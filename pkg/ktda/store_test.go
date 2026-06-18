package ktda

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileMergesByIK(t *testing.T) {
	a := Entry{IK: "1", Name: "A old", VKNR: "", ValidFrom: "20240101", Kassenart: "EK"}
	b := Entry{IK: "1", Name: "A new", VKNR: "12345", ValidFrom: "20250101", Kassenart: "EK"}
	c := Entry{IK: "2", Name: "B", Kassenart: "AO"}

	store := Compile([][]Entry{{a}, {b, c}}, []string{"f1", "f2"})
	if len(store.ByIK) != 2 {
		t.Fatalf("expected 2 IKs, got %d", len(store.ByIK))
	}
	// IK=1 should pick b (has VKNR).
	if store.ByIK["1"].Name != "A new" {
		t.Errorf("merge by VKNR failed: %+v", store.ByIK["1"])
	}
	if store.Sources[0] != "f1" || store.Sources[1] != "f2" {
		t.Errorf("sources = %v", store.Sources)
	}
	if !strings.Contains(store.Source, "GKV-Datenaustausch") {
		t.Errorf("source header = %q", store.Source)
	}
}

func TestCompileSkipsEmptyIK(t *testing.T) {
	store := Compile([][]Entry{{{IK: ""}, {IK: "1", Name: "A"}}}, nil)
	if _, ok := store.ByIK[""]; ok {
		t.Error("empty IK should be skipped")
	}
	if len(store.ByIK) != 1 {
		t.Errorf("got %d entries", len(store.ByIK))
	}
}

func TestBestOf(t *testing.T) {
	// Only b has VKNR → b wins.
	a := Entry{IK: "1", ValidFrom: "20250101"}
	b := Entry{IK: "1", VKNR: "55555", ValidFrom: "20240101"}
	if got := bestOf(a, b); got.VKNR != "55555" {
		t.Errorf("b should win when only b has VKNR; got %+v", got)
	}
	if got := bestOf(b, a); got.VKNR != "55555" {
		t.Errorf("a-vs-b independent of order: %+v", got)
	}
	// Both have VKNR — later ValidFrom wins.
	a2 := Entry{IK: "1", VKNR: "1", ValidFrom: "20230101"}
	b2 := Entry{IK: "1", VKNR: "2", ValidFrom: "20250101"}
	if got := bestOf(a2, b2); got.ValidFrom != "20250101" {
		t.Errorf("later ValidFrom should win; got %+v", got)
	}
	// Both have VKNR, same ValidFrom — a wins (stable).
	a3 := Entry{IK: "1", VKNR: "1", ValidFrom: "20250101", Name: "first"}
	b3 := Entry{IK: "1", VKNR: "2", ValidFrom: "20250101", Name: "second"}
	if got := bestOf(a3, b3); got.Name != "first" {
		t.Errorf("equal-ValidFrom: a should win; got %+v", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ktda.json")

	original := &Store{
		GeneratedAt: "2026-01-01T00:00:00Z",
		Source:      "test",
		Sources:     []string{"AO05Q126.ke0"},
		ByIK: map[string]Entry{
			"109519005": {IK: "109519005", Name: "TK", VKNR: "12345", Kassenart: "EK"},
		},
	}
	if err := original.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.GeneratedAt != original.GeneratedAt {
		t.Errorf("GeneratedAt drift")
	}
	if loaded.ByIK["109519005"].Name != "TK" {
		t.Errorf("entry drift")
	}
}

func TestSaveInvalidDir(t *testing.T) {
	// Save into a path under a non-existent dir that can't be created.
	if err := (&Store{}).Save("/dev/null/cannot/exist.json"); err == nil {
		t.Error("expected save error on bad path")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("expected load error on missing file")
	}
}

func TestReadStoreFromBytes(t *testing.T) {
	good := []byte(`{"generated_at":"x","source":"y","sources":["z"],"by_ik":{"1":{"IK":"1"}}}`)
	s, err := ReadStore(bytes.NewReader(good))
	if err != nil {
		t.Fatalf("ReadStore: %v", err)
	}
	if s.ByIK["1"].IK != "1" {
		t.Errorf("ByIK = %+v", s.ByIK)
	}
	if _, err := ReadStore(bytes.NewReader([]byte("not json"))); err == nil {
		t.Error("expected error for bad JSON")
	}
}

func TestLookup(t *testing.T) {
	s := &Store{ByIK: map[string]Entry{"1": {IK: "1", Name: "A"}}}
	if e := s.Lookup("1"); e == nil || e.Name != "A" {
		t.Errorf("Lookup(1) = %+v", e)
	}
	if e := s.Lookup("missing"); e != nil {
		t.Errorf("Lookup(missing) should be nil")
	}
	var nilStore *Store
	if e := nilStore.Lookup("1"); e != nil {
		t.Errorf("Lookup on nil store should be nil")
	}
}

func TestKostentraegergruppe(t *testing.T) {
	cases := []struct{ in, want string }{
		{"AO", "01"}, {"ao", "01"},
		{"BK", "02"}, {"IK", "03"},
		{"BN", "05"}, {"EK", "06"}, {"LK", "07"},
		{"", ""}, {"XX", ""},
	}
	for _, c := range cases {
		if got := Kostentraegergruppe(c.in); got != c.want {
			t.Errorf("Kostentraegergruppe(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStats(t *testing.T) {
	if got := (*Store)(nil).Stats(); got != "(empty)" {
		t.Errorf("nil store stats = %q", got)
	}
	s := &Store{
		GeneratedAt: "2026-01-01",
		ByIK: map[string]Entry{
			"1": {VKNR: "11111"},
			"2": {},
			"3": {VKNR: "33333"},
		},
	}
	got := s.Stats()
	if !strings.Contains(got, "3 institutions") {
		t.Errorf("Stats missing total: %q", got)
	}
	if !strings.Contains(got, "2 with VKNR") {
		t.Errorf("Stats missing VKNR count: %q", got)
	}
}

func TestSortedIKs(t *testing.T) {
	s := &Store{ByIK: map[string]Entry{"b": {}, "a": {}, "c": {}}}
	got := s.SortedIKs()
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("SortedIKs = %v", got)
	}
	if (*Store)(nil).SortedIKs() != nil {
		t.Error("nil store should return nil")
	}
}
