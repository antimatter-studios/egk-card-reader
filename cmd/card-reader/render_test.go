package main

import (
	"strings"
	"testing"

	"github.com/antimatter-studios/egk-card-reader/pkg/egk"
)

func TestWrap(t *testing.T) {
	// Empty / whitespace.
	if got := wrap("", 10); got != "" {
		t.Errorf("empty = %q", got)
	}
	// Short string fits one line.
	if got := wrap("hello", 80); got != "hello" {
		t.Errorf("short = %q", got)
	}
	// Long string wraps on word boundaries.
	got := wrap("one two three four five six seven eight", 12)
	if !strings.Contains(got, "\n") {
		t.Errorf("expected wrap: %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		// No single output line should massively exceed the width (allow some
		// slack because wrap measures full words).
		if len(line) > 25 {
			t.Errorf("line too long: %q", line)
		}
	}
}

func TestRenderForm(t *testing.T) {
	fields := []egk.FormField{
		{Label: "A", Value: "yes", Source: "EF.PD"},
		{Label: "B", Value: "", Source: "practice", Note: "missing on card"},
	}
	out := renderForm(fields)
	if !strings.Contains(out, "1 filled") {
		t.Errorf("summary count wrong: %q", out)
	}
	if !strings.Contains(out, "1 missing") {
		t.Errorf("missing count wrong: %q", out)
	}
}

func TestChrome(t *testing.T) {
	out := chrome()
	if !strings.Contains(out, "eGK Card Reader") {
		t.Errorf("title missing: %q", out)
	}
	if !strings.Contains(out, "Read at") {
		t.Errorf("subtitle missing: %q", out)
	}
}

func TestRenderTableEachFormat(t *testing.T) {
	d := &egk.CardData{
		Personal:  &egk.PersonalData{InsurantID: "X1", LastName: "Y"},
		Insurance: &egk.InsuranceData{InsurerID: "1", InsurerName: "TK"},
	}
	for _, fmt := range []string{"form", "json", "gdt", "hl7-fhir", "hl7-adt"} {
		out, err := renderTable(fmt, d, nil, false)
		if err != nil {
			t.Errorf("%s: %v", fmt, err)
		}
		if out == "" {
			t.Errorf("%s: empty output", fmt)
		}
	}
}

func TestRenderTableWithGlossary(t *testing.T) {
	d := &egk.CardData{Personal: &egk.PersonalData{InsurantID: "X1"}}
	out, err := renderTable("form", d, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Healthcare acronyms") {
		t.Error("glossary not appended")
	}
}

func TestRenderTableUnknownFormat(t *testing.T) {
	if _, err := renderTable("xyz", nil, nil, false); err == nil {
		t.Error("expected error")
	}
}
