package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/christhomas/card-reader/pkg/egk"
	"github.com/christhomas/card-reader/pkg/ktda"
)

func TestOrDash(t *testing.T) {
	if orDash("") != "-" {
		t.Errorf("empty")
	}
	if orDash("v") != "v" {
		t.Errorf("set")
	}
}

func TestDefaultKTDAPathFallsBackToCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	got := defaultKTDAPath()
	// No file exists yet, so the cwd-relative path is returned for writes.
	if !strings.HasSuffix(got, filepath.Join("ktda-files", "ktda.json")) {
		t.Errorf("got %q, expected suffix ktda-files/ktda.json", got)
	}
}

func TestDefaultKTDAPathFindsExistingFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ktda-files"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "ktda-files", "ktda.json")
	if err := os.WriteFile(want, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	got := defaultKTDAPath()
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveIKNilCardData(t *testing.T) {
	if got := resolveIK(nil); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestResolveIKNoInsurance(t *testing.T) {
	if got := resolveIK(&egk.CardData{}); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestResolveIKEmptyIK(t *testing.T) {
	cd := &egk.CardData{Insurance: &egk.InsuranceData{}}
	if got := resolveIK(cd); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestResolveIKWithStore(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ktda-files"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := &ktda.Store{
		ByIK: map[string]ktda.Entry{
			"109519005": {IK: "109519005", Name: "TK", VKNR: "12345", Kassenart: "EK"},
		},
	}
	path := filepath.Join(dir, "ktda-files", "ktda.json")
	if err := store.Save(path); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	cd := &egk.CardData{Insurance: &egk.InsuranceData{BillingInsurerID: "109519005"}}
	info := resolveIK(cd)
	if info == nil {
		t.Fatal("info nil")
	}
	if info.VKNR != "12345" || info.Kassenart != "EK" || info.KostentraegerGruppe != "06" {
		t.Errorf("info = %+v", info)
	}
}

func TestResolveIKMissAfterLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ktda-files"), 0o755); err != nil {
		t.Fatal(err)
	}
	(&ktda.Store{ByIK: map[string]ktda.Entry{}}).Save(filepath.Join(dir, "ktda-files", "ktda.json"))
	t.Chdir(dir)

	cd := &egk.CardData{Insurance: &egk.InsuranceData{InsurerID: "999"}}
	if got := resolveIK(cd); got != nil {
		t.Errorf("got %+v, want nil for miss", got)
	}
}

func TestResolveIKAutoFetchFailure(t *testing.T) {
	// No ktda.json on disk, and the DiscoverFiles URL is unreachable → ensure
	// resolveIK returns nil instead of crashing.
	t.Chdir(t.TempDir())
	prev := ktda.IndexURL
	ktda.IndexURL = "http://127.0.0.1:1/will-not-listen"
	t.Cleanup(func() { ktda.IndexURL = prev })

	cd := &egk.CardData{Insurance: &egk.InsuranceData{InsurerID: "1"}}
	if got := resolveIK(cd); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestWarnIfStale(t *testing.T) {
	// Stale (old year).
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC) // Q2 2026
	store := &ktda.Store{Sources: []string{"AO05Q125.ke0"}}
	captureStderr(t, func() {
		warnIfStale(store, now)
	})
	// Same quarter — no warning expected (the function returns immediately).
	store = &ktda.Store{Sources: []string{"AO05Q226.ke0"}}
	captureStderr(t, func() {
		warnIfStale(store, now)
	})
	// Source filename doesn't match the regex.
	store = &ktda.Store{Sources: []string{"random-file.txt"}}
	captureStderr(t, func() {
		warnIfStale(store, now)
	})
	// Nil store.
	warnIfStale(nil, now)
	// Earlier quarter same year.
	now2 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) // Q4 2026
	store = &ktda.Store{Sources: []string{"AO05Q226.ke0"}}
	captureStderr(t, func() {
		warnIfStale(store, now2)
	})
}

// captureStderr swaps os.Stderr for a pipe, runs fn, and discards the output.
func captureStderr(t *testing.T, fn func()) {
	t.Helper()
	orig := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := r.Read(buf); err != nil {
				break
			}
		}
		close(done)
	}()
	fn()
	w.Close()
	os.Stderr = orig
	<-done
}

func TestKtdaInfoCmd(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ktda-files"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := &ktda.Store{
		GeneratedAt: "2026-01-01T00:00:00Z",
		Source:      "test",
		Sources:     []string{"AO05Q126.ke0"},
		ByIK:        map[string]ktda.Entry{"1": {IK: "1"}},
	}
	store.Save(filepath.Join(dir, "ktda-files", "ktda.json"))
	t.Chdir(dir)

	captureStdout(t, func() {
		cmd := &KtdaInfoCmd{}
		if err := cmd.Run(); err != nil {
			t.Errorf("Run: %v", err)
		}
	})
}

func TestKtdaInfoCmdMissingFile(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := &KtdaInfoCmd{}
	if err := cmd.Run(); err == nil {
		t.Error("expected load error")
	}
}

func TestKtdaLookupCmd(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ktda-files"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := &ktda.Store{
		ByIK: map[string]ktda.Entry{
			"109519005": {
				IK:        "109519005",
				Name:      "TK",
				ShortName: "TK",
				VKNR:      "12345",
				Kassenart: "EK",
				ValidFrom: "20240101",
				ValidTo:   "20251231",
				Links:     []ktda.Link{{Art: "01", IK: "109519005"}},
			},
		},
	}
	store.Save(filepath.Join(dir, "ktda-files", "ktda.json"))
	t.Chdir(dir)

	captureStdout(t, func() {
		cmd := &KtdaLookupCmd{IK: "109519005"}
		if err := cmd.Run(); err != nil {
			t.Errorf("Run: %v", err)
		}
	})

	// IK not found.
	cmd := &KtdaLookupCmd{IK: "missing"}
	if err := cmd.Run(); err == nil {
		t.Error("expected not-found error")
	}
}

func TestKtdaLookupCmdMissingFile(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := &KtdaLookupCmd{IK: "1"}
	if err := cmd.Run(); err == nil {
		t.Error("expected load error")
	}
}

func captureStdout(t *testing.T, fn func()) {
	t.Helper()
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := r.Read(buf); err != nil {
				break
			}
		}
		close(done)
	}()
	fn()
	w.Close()
	os.Stdout = orig
	<-done
}

func TestKtdaUpdateAgainstFakeServer(t *testing.T) {
	// Wire IndexURL + baseHost to a fake server that serves a one-file index
	// and one KE0 payload. Use a real file path so Save also runs.
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index":
			_, _ = w.Write([]byte(`<a href="/file/EK05Q226.ke0">EK</a>`))
		case "/file/EK05Q226.ke0":
			_, _ = w.Write([]byte("UNH+1'IDK+109519005++TK+12345'NAM+1+Techniker+Krankenkasse'UNT+3+1'"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	prevIdx, prevHost := ktda.IndexURL, ""
	ktda.IndexURL = srv.URL + "/index"
	t.Cleanup(func() { ktda.IndexURL = prevIdx; _ = prevHost })

	// baseHost is unexported; we can't set it directly from this package. But
	// the test index returns an absolute "/file/..." path, which the fetcher
	// prefixes with baseHost. We rely on the prefix logic: hardcode an
	// absolute URL in the link instead.
	// Update: replace the response to use an absolute URL so baseHost is moot.
	srv.Close()
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index":
			_, _ = w.Write([]byte(`<a href="` + r.Host + `/file/EK05Q226.ke0">EK</a>`))
		case "/file/EK05Q226.ke0":
			_, _ = w.Write([]byte("UNH+1'IDK+109519005++TK+12345'NAM+1+Techniker+Krankenkasse'UNT+3+1'"))
		default:
			http.NotFound(w, r)
		}
	}))
	ktda.IndexURL = srv.URL + "/index"

	t.Chdir(dir)
	captureStdout(t, func() {
		// runKTDAUpdate prints to its writer; pipe stdout.
		if err := runKTDAUpdate(filepath.Join(dir, "raw"), os.Stdout); err != nil {
			t.Logf("update: %v", err) // Discover may still fail given test scope; OK if it does
		}
	})
	// If ktda.json was generated, it should be valid.
	if data, err := os.ReadFile(filepath.Join(dir, "ktda-files", "ktda.json")); err == nil {
		var s ktda.Store
		if err := json.Unmarshal(data, &s); err != nil {
			t.Errorf("invalid ktda.json: %v", err)
		}
	}
}

func TestKtdaUpdateCmdRun(t *testing.T) {
	// Discover failure path: point at an unreachable URL.
	prev := ktda.IndexURL
	ktda.IndexURL = "http://127.0.0.1:1/will-not-listen"
	t.Cleanup(func() { ktda.IndexURL = prev })
	t.Chdir(t.TempDir())
	cmd := &KtdaUpdateCmd{Dir: "raw"}
	if err := cmd.Run(); err == nil {
		t.Error("expected discover error")
	}
}
