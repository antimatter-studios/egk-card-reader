package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/christhomas/card-reader/internal/egk"
	"github.com/christhomas/card-reader/internal/ktda"
)

// KtdaCmd groups the insurer-table subcommands. The KE0 files are republished
// quarterly by gkv-datenaustausch.de — re-run `update` then to refresh.
type KtdaCmd struct {
	Update KtdaUpdateCmd `cmd:"" help:"Download + compile the 6 KE0 files from gkv-datenaustausch.de into ktda.json."`
	Lookup KtdaLookupCmd `cmd:"" help:"Print one IK's full record from ktda.json."`
	Info   KtdaInfoCmd   `cmd:"" help:"Show ktda.json path, file count, generation timestamp."`
}

type KtdaUpdateCmd struct {
	Dir string `arg:"" optional:"" default:"ktda-files/raw" help:"Directory where raw KE0 files are kept (will be created if missing)."`
}

func (k *KtdaUpdateCmd) Run() error {
	return runKTDAUpdate(k.Dir, os.Stdout)
}

// runKTDAUpdate downloads + parses + compiles the KE0 files into ktda.json.
// Status messages go to w so callers can route them to stderr (auto-fetch
// during a card read) or stdout (explicit `ktda update`).
func runKTDAUpdate(dir string, w *os.File) error {
	fmt.Fprintf(w, "Discovering current KE0 files at %s...\n", ktda.IndexURL)
	urls, err := ktda.DiscoverFiles()
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}
	fmt.Fprintf(w, "Found %d files:\n", len(urls))
	for _, u := range urls {
		fmt.Fprintf(w, "  - %s\n", filepath.Base(u))
	}

	fmt.Fprintf(w, "\nDownloading into %s/ ...\n", dir)
	paths, err := ktda.Download(urls, dir)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	fmt.Fprintln(w, "\nParsing...")
	var allEntries [][]ktda.Entry
	var sources []string
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "open %s: %v\n", p, err)
			continue
		}
		entries, err := ktda.Parse(f, ktda.KassenartFromFilename(p))
		f.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse %s: %v\n", p, err)
			continue
		}
		fmt.Fprintf(w, "  %-15s %d entries\n", filepath.Base(p), len(entries))
		allEntries = append(allEntries, entries)
		sources = append(sources, filepath.Base(p))
	}

	store := ktda.Compile(allEntries, sources)
	out := defaultKTDAPath()
	if err := store.Save(out); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	fmt.Fprintf(w, "\nWrote %s — %s\n", out, store.Stats())
	return nil
}

type KtdaLookupCmd struct {
	IK string `arg:"" required:"" help:"9-digit Institutionskennzeichen to look up (e.g. 109519005)."`
}

func (k *KtdaLookupCmd) Run() error {
	store, err := ktda.Load(defaultKTDAPath())
	if err != nil {
		return fmt.Errorf("load %s: %w (run `card-reader ktda update` first)", defaultKTDAPath(), err)
	}
	e := store.Lookup(k.IK)
	if e == nil {
		return fmt.Errorf("IK %s not found in %s", k.IK, defaultKTDAPath())
	}
	fmt.Printf("IK:                 %s\n", e.IK)
	fmt.Printf("Name:               %s\n", e.Name)
	fmt.Printf("Short name:         %s\n", e.ShortName)
	fmt.Printf("VKNR:               %s\n", orDash(e.VKNR))
	fmt.Printf("Kassenart:          %s\n", e.Kassenart)
	fmt.Printf("Kostenträgergruppe: %s\n", orDash(ktda.Kostentraegergruppe(e.Kassenart)))
	fmt.Printf("Valid from / to:    %s / %s\n", orDash(e.ValidFrom), orDash(e.ValidTo))
	if len(e.Links) > 0 {
		fmt.Println("Links:")
		for _, l := range e.Links {
			fmt.Printf("  - art=%s ik=%s leGruppe=%s stelle=%s\n", l.Art, l.IK, l.LEGruppe, l.StelleIK)
		}
	}
	return nil
}

type KtdaInfoCmd struct{}

func (k *KtdaInfoCmd) Run() error {
	store, err := ktda.Load(defaultKTDAPath())
	if err != nil {
		return fmt.Errorf("load %s: %w", defaultKTDAPath(), err)
	}
	fmt.Printf("Path:    %s\n", defaultKTDAPath())
	fmt.Printf("Stats:   %s\n", store.Stats())
	fmt.Printf("Source:  %s\n", store.Source)
	fmt.Println("Files:")
	for _, s := range store.Sources {
		fmt.Printf("  - %s\n", s)
	}
	return nil
}

// defaultKTDAPath returns the path to use for ktda.json. It looks at
// ktda-files/ktda.json relative to the binary's directory and the current
// working directory, returns the first one that exists, and falls back to
// "<cwd>/ktda-files/ktda.json" for write paths.
//
// The binary path is unreliable under `go run` (it points into a temp build
// dir), so the cwd fallback is what makes that case work.
func defaultKTDAPath() string {
	rel := filepath.Join("ktda-files", "ktda.json")
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		if dir, err := filepath.Abs(filepath.Dir(exe)); err == nil {
			if !strings.Contains(dir, "go-build") {
				candidates = append(candidates, filepath.Join(dir, rel))
			}
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, rel))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if len(candidates) > 0 {
		return candidates[len(candidates)-1] // cwd, for writes
	}
	return rel
}

// resolveIK loads the cached KTDA (auto-downloading on first run) and
// resolves the card's AbrechnenderKostentraeger IK (preferred) or its main
// IK into the bits we need to fill VKNR / Kassenart / Kostenträgergruppe on
// the form. Returns nil only if the card carries no IK or the auto-download
// failed (network down etc.) — the form mapping then degrades to "run `ktda
// update`" notes for those fields.
func resolveIK(d *egk.CardData) *egk.IKInfo {
	if d == nil || d.Insurance == nil {
		return nil
	}
	ik := d.Insurance.BillingInsurerID
	if ik == "" {
		ik = d.Insurance.InsurerID
	}
	if ik == "" {
		return nil
	}
	store, err := ensureKTDA()
	if err != nil || store == nil {
		return nil
	}
	warnIfStale(store, time.Now())
	e := store.Lookup(ik)
	if e == nil {
		return nil
	}
	return &egk.IKInfo{
		Name:                e.Name,
		VKNR:                e.VKNR,
		Kassenart:           e.Kassenart,
		KostentraegerGruppe: ktda.Kostentraegergruppe(e.Kassenart),
	}
}

// ensureKTDA returns the loaded KTDA store, downloading the KE0 files on
// first use so a fresh checkout doesn't need a manual `ktda update` step.
// Status goes to stderr so a 10–30s fetch isn't silent. Subsequent runs
// hit the on-disk ktda.json and return immediately.
func ensureKTDA() (*ktda.Store, error) {
	path := defaultKTDAPath()
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(os.Stderr, "ktda.json not found at %s — fetching insurer table (one-time, ~10–30s)\n", path)
		if err := runKTDAUpdate(filepath.Join("ktda-files", "raw"), os.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "ktda auto-fetch failed: %v (continuing without KTDA enrichment)\n", err)
			return nil, err
		}
	}
	return ktda.Load(path)
}

// quarterYearRe extracts the quarter and 2-digit year from a KE0 filename
// like "EK05Q226.ke0" — group 1 is "2" (Q2), group 2 is "26" (2026).
var quarterYearRe = regexp.MustCompile(`(?i)Q(\d)(\d{2})\.ke0$`)

// warnIfStale prints a notice on stderr when the source files are from a
// quarter older than the current calendar quarter. Doesn't block — stale
// data still resolves most IKs, and forcing a re-download would be hostile
// to scripted use.
func warnIfStale(store *ktda.Store, now time.Time) {
	if store == nil {
		return
	}
	curQ := (int(now.Month())-1)/3 + 1
	curY := now.Year()
	for _, src := range store.Sources {
		m := quarterYearRe.FindStringSubmatch(src)
		if m == nil {
			continue
		}
		q, _ := strconv.Atoi(m[1])
		yy, _ := strconv.Atoi(m[2])
		y := 2000 + yy
		if y < curY || (y == curY && q < curQ) {
			fmt.Fprintf(os.Stderr, "warning: KTDA files are from Q%d %d, current is Q%d %d — run `card-reader ktda update` to refresh\n", q, y, curQ, curY)
			return
		}
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
