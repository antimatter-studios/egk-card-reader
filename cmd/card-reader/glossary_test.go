package main

import (
	"strings"
	"testing"
)

func TestRenderGlossarySections(t *testing.T) {
	out := renderGlossary()
	for _, want := range []string{
		"Source codes",
		"Form labels",
		"KTAB codes",
		"Healthcare acronyms",
		"GKV",   // acronym table entry
		"AOK",
		"PLZ",
		"Primärabrechnung", // KTAB 00
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in glossary output", want)
		}
	}
}

func TestGlossaryTableEmpty(t *testing.T) {
	out := glossaryTable("EmptyCaption", nil)
	if !strings.Contains(out, "EmptyCaption") {
		t.Errorf("caption missing: %q", out)
	}
}
