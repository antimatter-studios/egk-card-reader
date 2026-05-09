package ktda

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// Store is the compiled, deduplicated lookup table the app uses at runtime.
// Multiple Entry records can share an IK (different validity periods, multiple
// KE0 files); we keep the most recently valid one keyed by IK.
type Store struct {
	GeneratedAt string           `json:"generated_at"`
	Source      string           `json:"source"`
	Sources     []string         `json:"sources"`
	ByIK        map[string]Entry `json:"by_ik"`
}

// Compile merges entries from multiple KE0 parses into a single keyed map.
// When multiple entries share an IK, prefer the one with the latest VDT
// validity, then prefer one that has a non-empty VKNR.
func Compile(allEntries [][]Entry, sources []string) *Store {
	merged := make(map[string]Entry)
	for _, entries := range allEntries {
		for _, e := range entries {
			if e.IK == "" {
				continue
			}
			prev, ok := merged[e.IK]
			if !ok {
				merged[e.IK] = e
				continue
			}
			merged[e.IK] = bestOf(prev, e)
		}
	}
	return &Store{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Source:      "GKV-Datenaustausch (Kostenträgerdateien Sonstige Leistungserbringer, Anhang 3 zur Anlage 1)",
		Sources:     sources,
		ByIK:        merged,
	}
}

func bestOf(a, b Entry) Entry {
	// Prefer the one with VKNR set if only one has it.
	if a.VKNR == "" && b.VKNR != "" {
		return b
	}
	if b.VKNR == "" && a.VKNR != "" {
		return a
	}
	// Prefer the later ValidFrom.
	if b.ValidFrom > a.ValidFrom {
		return b
	}
	return a
}

// Save writes the store as pretty-printed JSON.
func (s *Store) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

// Load reads a previously-compiled store from disk.
func Load(path string) (*Store, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadStore(f)
}

func ReadStore(r io.Reader) (*Store, error) {
	var s Store
	if err := json.NewDecoder(r).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Lookup returns the entry for the given IK, or nil if not found.
func (s *Store) Lookup(ik string) *Entry {
	if s == nil {
		return nil
	}
	if e, ok := s.ByIK[ik]; ok {
		return &e
	}
	return nil
}

// Kostenträgergruppe returns the 2-digit KBV code for a given Kassenart prefix.
// These map the SGB V Kassenart families to the codes used on practice billing
// forms (Anlage 6 BMV-Ä).
func Kostentraegergruppe(kassenart string) string {
	switch strings.ToUpper(kassenart) {
	case "AO":
		return "01" // AOK
	case "BK":
		return "02" // BKK
	case "IK":
		return "03" // IKK
	case "BN":
		return "05" // Knappschaft
	case "LK":
		return "07" // Landwirtschaftliche KK / SVLFG
	case "EK":
		return "06" // Ersatzkassen (vdek)
	}
	return ""
}

// Stats returns a one-line summary for CLI output.
func (s *Store) Stats() string {
	if s == nil {
		return "(empty)"
	}
	withVKNR := 0
	for _, e := range s.ByIK {
		if e.VKNR != "" {
			withVKNR++
		}
	}
	return fmt.Sprintf("%d institutions (%d with VKNR), generated %s", len(s.ByIK), withVKNR, s.GeneratedAt)
}

// SortedIKs is handy for stable test output / debug listings.
func (s *Store) SortedIKs() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.ByIK))
	for k := range s.ByIK {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
