package ktda

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKassenartFromFilename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"AO05Q226.ke0", "AO"},
		{"ek05q226.ke0", "EK"},
		{"/var/data/BN05Q226.ke0", "BN"},
		{"X", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := KassenartFromFilename(c.in); got != c.want {
			t.Errorf("KassenartFromFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// withTestServer points IndexURL and baseHost at a temporary httptest server
// for the duration of t. The variables are package-level vars (not const) so
// tests can override them safely.
func withTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	prevIndex, prevBase := IndexURL, baseHost
	IndexURL = srv.URL + "/index"
	baseHost = srv.URL
	t.Cleanup(func() {
		IndexURL = prevIndex
		baseHost = prevBase
		srv.Close()
	})
	return srv
}

func TestDiscoverFilesHappyPath(t *testing.T) {
	body := `<html>
<a href="/media/AO05Q226.ke0">AOK</a>
<a href="/media/EK05Q226.ke0">EK</a>
<a href="https://elsewhere.example/BK05Q226.ke0">BK absolute</a>
<a href="/something/else.txt">ignore me</a>
<a href="/dup/AO05Q226.ke0">duplicate AO</a>
</html>`
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	})

	urls, err := DiscoverFiles()
	if err != nil {
		t.Fatalf("DiscoverFiles: %v", err)
	}
	// Expect AO (relative), EK (relative), BK (absolute), duplicate AO under different path.
	// The kassenartRe filters by basename so duplicates with different paths still dedupe in the URL set.
	wants := []string{
		baseHost + "/media/AO05Q226.ke0",
		baseHost + "/media/EK05Q226.ke0",
		"https://elsewhere.example/BK05Q226.ke0",
		baseHost + "/dup/AO05Q226.ke0",
	}
	if len(urls) != len(wants) {
		t.Fatalf("got %d urls, want %d: %v", len(urls), len(wants), urls)
	}
	for i, w := range wants {
		if urls[i] != w {
			t.Errorf("urls[%d] = %q, want %q", i, urls[i], w)
		}
	}
}

func TestDiscoverFilesNoMatches(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<a href="/elsewhere/nothing.txt">x</a>`))
	})
	if _, err := DiscoverFiles(); err == nil {
		t.Error("expected no-matches error")
	}
}

func TestDiscoverFilesServerError(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	if _, err := DiscoverFiles(); err == nil {
		t.Error("expected 500 error")
	}
}

func TestDiscoverFilesUnreachable(t *testing.T) {
	prev := IndexURL
	IndexURL = "http://127.0.0.1:1/will-not-listen"
	t.Cleanup(func() { IndexURL = prev })
	if _, err := DiscoverFiles(); err == nil {
		t.Error("expected connection error")
	}
}

func TestDownload(t *testing.T) {
	body1 := []byte("file-1-bytes")
	body2 := []byte("file-2-bytes")
	srv := withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.ke0":
			_, _ = w.Write(body1)
		case "/b.ke0":
			_, _ = w.Write(body2)
		default:
			http.NotFound(w, r)
		}
	})
	dir := t.TempDir()
	paths, err := Download([]string{srv.URL + "/a.ke0", srv.URL + "/b.ke0"}, dir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("got %d paths", len(paths))
	}
	got1, _ := os.ReadFile(paths[0])
	got2, _ := os.ReadFile(paths[1])
	if string(got1) != string(body1) {
		t.Errorf("body1 mismatch: %q", got1)
	}
	if string(got2) != string(body2) {
		t.Errorf("body2 mismatch: %q", got2)
	}
	if filepath.Base(paths[0]) != "a.ke0" {
		t.Errorf("basename = %q", filepath.Base(paths[0]))
	}
	// No .tmp file leftover.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("stale .tmp file: %s", e.Name())
		}
	}
}

func TestDownloadServerError(t *testing.T) {
	srv := withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	})
	dir := t.TempDir()
	if _, err := Download([]string{srv.URL + "/missing.ke0"}, dir); err == nil {
		t.Error("expected error on 404")
	}
}

func TestDownloadMkdirFailure(t *testing.T) {
	// Try to download into a path that exists but as a regular file (not dir).
	dir := t.TempDir()
	regularFile := filepath.Join(dir, "notadir")
	if err := os.WriteFile(regularFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Download(nil, filepath.Join(regularFile, "child")); err == nil {
		t.Error("expected mkdir error")
	}
}

func TestDownloadInvalidURL(t *testing.T) {
	dir := t.TempDir()
	if _, err := Download([]string{"http://127.0.0.1:1/will-fail"}, dir); err == nil {
		t.Error("expected error from unreachable URL")
	}
}
