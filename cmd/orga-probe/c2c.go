package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/christhomas/card-reader/internal/c2c"
	"github.com/christhomas/card-reader/internal/c2c/keys"
	"github.com/christhomas/card-reader/internal/reader/orga"
)

// runC2C drives the C2C handshake's Discover + Validate phases against the
// real card in slot n, printing a structured report to stdout (and
// optionally a markdown file).
//
// Later handshake phases (PresentToVerifier / MutualAuth / OpenSecureChannel)
// are scaffolded but not implemented; they emit "not yet implemented"
// errors and are skipped here.
func runC2C(t *orga.Terminal, slot int, outPath string, useTest bool) error {
	if slot != 1 && slot != 2 {
		return fmt.Errorf("c2c: slot must be 1 or 2, got %d", slot)
	}
	peer := t.Slot(slot)
	roots := keys.ProductionCVCRoots()
	rootKind := "production CVC"
	if useTest {
		roots = keys.TestCVCRoots()
		rootKind = "test CVC"
	}

	var w io.Writer = os.Stdout
	if outPath != "" {
		f, err := os.Create(outPath)
		if err != nil {
			return err
		}
		defer f.Close()
		w = io.MultiWriter(os.Stdout, f)
	}

	fmt.Fprintf(w, "# C2C probe — slot %d — %s\n\n", slot, time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "Trust anchors: gematik **%s** roots (%d configured)\n\n", rootKind, len(roots))

	// Handshake.Run would stop at the first phase error. For an interactive
	// probe we want to surface each phase result separately, so we call the
	// phase methods directly.
	h, err := c2c.New(c2c.Options{
		EGK:   peer, // probing one card; the EGK slot doesn't matter here
		SMCB:  peer,
		Roots: roots,
	})
	if err != nil {
		return err
	}

	// Phase 1: discover
	fmt.Fprintf(w, "## Phase 1 — DiscoverPeerCerts\n\n")
	if err := h.DiscoverPeerCerts(); err != nil {
		fmt.Fprintf(w, "FAILED: %v\n\n", err)
		return nil // a failed discover isn't a tool-level error; we report and continue
	}
	chain := h.SMCBChain()
	fmt.Fprintf(w, "Found **%d** CV-cert(s) on the card:\n\n", len(chain))
	fmt.Fprintln(w, "| FID  | Label | KeyAlg | CAR | CHR | NotBefore | NotAfter |")
	fmt.Fprintln(w, "|------|-------|--------|-----|-----|-----------|----------|")
	for _, d := range chain {
		fmt.Fprintf(w, "| %04X | %s | %s | %s | %s | %s | %s |\n",
			d.Slot.FID, d.Slot.Label, d.Cert.KeyAlg, d.Cert.CAR, d.Cert.CHR,
			d.Cert.NotBefore.Format("2006-01-02"), d.Cert.NotAfter.Format("2006-01-02"))
	}
	fmt.Fprintln(w)

	// Phase 2: validate
	fmt.Fprintf(w, "## Phase 2 — ValidatePeerChain\n\n")
	if err := h.ValidatePeerChain(); err != nil {
		fmt.Fprintf(w, "FAILED: %v\n\n", err)
		fmt.Fprintln(w, "Expected outcomes for the slot-2 test SMC-B:")
		fmt.Fprintln(w, "- against `keys.ProductionRoots()`: certs chain to gematik TEST root, no production match → \"untrusted root\"")
		fmt.Fprintln(w, "- against `keys.TestRoots()`: chain matches test root but the certs expired 2024-12-11 → \"chain expired\"")
		fmt.Fprintln(w)
	} else if r := h.MatchedRoot(); r != nil {
		fmt.Fprintf(w, "Chain validated against root **%s**.\n\n", r.Name)
	}

	// Phases 3-5: skipped (scaffolded only; require CVC-Root pubkey).
	fmt.Fprintf(w, "## Phases 3–5 — not yet implemented\n\n")
	fmt.Fprintln(w, "`PresentToVerifier`, `MutualAuthenticate`, `OpenSecureChannel` are")
	fmt.Fprintln(w, "scaffolded in `internal/c2c/handshake.go`. They will fail with")
	fmt.Fprintln(w, "\"not yet implemented\" until the gematik CVC-Root pubkey is sourced")
	fmt.Fprintln(w, "(see `docs/c2c/plan.md` and the open TODO in `internal/c2c/keys/`).")
	return nil
}
