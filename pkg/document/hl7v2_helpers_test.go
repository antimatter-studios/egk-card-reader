package document

import (
	"strings"
	"testing"

	"github.com/christhomas/card-reader/pkg/egk"
)

func TestHL7Escape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"plain text", "plain text"},
		{"a|b", `a\F\b`},
		{"a^b", `a\S\b`},
		{"a~b", `a\R\b`},
		{`a\b`, `a\E\b`},
		{"a&b", `a\T\b`},
		// Combined.
		{"a|b^c~d&e", `a\F\b\S\c\R\d\T\e`},
		// Backslash must be replaced FIRST so we don't double-escape.
		{`\|`, `\E\\F\`},
	}
	for _, c := range cases {
		if got := hl7Escape(c.in); got != c.want {
			t.Errorf("hl7Escape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHL7Name(t *testing.T) {
	// Full name: last^first^^^title.
	pd := &egk.PersonalData{LastName: "Müller", FirstName: "Hans", Title: "Dr."}
	if got := hl7Name(pd); got != "Müller^Hans^^^Dr." {
		t.Errorf("full = %q", got)
	}
	// No title — trailing empty parts trimmed.
	pd = &egk.PersonalData{LastName: "Müller", FirstName: "Hans"}
	if got := hl7Name(pd); got != "Müller^Hans" {
		t.Errorf("no-title = %q", got)
	}
	// Only last name.
	pd = &egk.PersonalData{LastName: "Müller"}
	if got := hl7Name(pd); got != "Müller" {
		t.Errorf("last-only = %q", got)
	}
	// Empty.
	pd = &egk.PersonalData{}
	if got := hl7Name(pd); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestHL7Address(t *testing.T) {
	pd := &egk.PersonalData{
		Street:      "Hauptstr.",
		HouseNumber: "5",
		City:        "Berlin",
		PostalCode:  "10115",
		Country:     "D",
	}
	got := hl7Address(pd)
	if !strings.Contains(got, "Hauptstr. 5") || !strings.Contains(got, "Berlin") || !strings.Contains(got, "10115") {
		t.Errorf("addr = %q", got)
	}
	// AddressSuffix included.
	pd.AddressSuffix = "Hinterhaus"
	got = hl7Address(pd)
	if !strings.Contains(got, "Hinterhaus") {
		t.Errorf("suffix missing: %q", got)
	}
	// Empty.
	if got := hl7Address(&egk.PersonalData{}); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestHL7Sex(t *testing.T) {
	cases := []struct{ in, want string }{
		{"M", "M"}, {"m", "M"},
		{"W", "F"}, {"F", "F"},
		{"X", "U"}, {"D", "A"},
		{"", ""}, {"Q", ""},
	}
	for _, c := range cases {
		if got := hl7Sex(c.in); got != c.want {
			t.Errorf("hl7Sex(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHL7VKNR(t *testing.T) {
	if got := hl7VKNR(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
	if got := hl7VKNR(&egk.IKInfo{VKNR: "12345"}); got != "12345" {
		t.Errorf("set = %q", got)
	}
	// Verify hl7Escape applied to VKNR.
	if got := hl7VKNR(&egk.IKInfo{VKNR: "1|2"}); got != `1\F\2` {
		t.Errorf("escape = %q", got)
	}
}

func TestCondDate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"19720314", "19720314"},
		{"", ""},
		{"abc", ""},
		{"1972031", ""},   // short
		{"197203140", ""}, // long
		{"1972A314", ""},  // non-numeric
	}
	for _, c := range cases {
		if got := condDate(c.in); got != c.want {
			t.Errorf("condDate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSegment(t *testing.T) {
	if got := segment("EVN", "A04", "20240101"); got != "EVN|A04|20240101" {
		t.Errorf("segment = %q", got)
	}
	if got := segment("PV1"); got != "PV1|" {
		t.Errorf("empty fields = %q", got)
	}
}

func TestFieldAt(t *testing.T) {
	f := []string{"a", "b", "c"}
	if got := fieldAt(f, 0); got != "a" {
		t.Errorf("0 = %q", got)
	}
	if got := fieldAt(f, 2); got != "c" {
		t.Errorf("2 = %q", got)
	}
	if got := fieldAt(f, 3); got != "" {
		t.Errorf("out-of-range = %q", got)
	}
	if got := fieldAt(f, -1); got != "" {
		t.Errorf("negative = %q", got)
	}
}

func TestCompAt(t *testing.T) {
	c := []string{"a", "b"}
	if got := compAt(c, 0); got != "a" {
		t.Errorf("0 = %q", got)
	}
	if got := compAt(c, 5); got != "" {
		t.Errorf("out-of-range = %q", got)
	}
	if got := compAt(c, -1); got != "" {
		t.Errorf("negative = %q", got)
	}
}

func TestFirstRep(t *testing.T) {
	if got := firstRep("a~b~c", '~'); got != "a" {
		t.Errorf("got %q", got)
	}
	if got := firstRep("a", '~'); got != "a" {
		t.Errorf("no-sep = %q", got)
	}
	if got := firstRep("", '~'); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestSplitComps(t *testing.T) {
	got := splitComps("a^b^c", '^')
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("comps = %v", got)
	}
	got = splitComps("", '^')
	if len(got) != 1 || got[0] != "" {
		t.Errorf("empty = %v", got)
	}
}

func TestHL7Unescape(t *testing.T) {
	// Each escape sequence individually.
	for _, c := range []struct{ in, want string }{
		{`a\F\b`, "a|b"},
		{`a\S\b`, "a^b"},
		{`a\R\b`, "a~b"},
		{`a\T\b`, "a&b"},
		{`a\E\b`, `a\b`},
	} {
		got := hl7Unescape(c.in, '\\', '|', '^', '~', '&')
		if got != c.want {
			t.Errorf("hl7Unescape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Unknown escape passes through verbatim.
	if got := hl7Unescape(`pre\X20\post`, '\\', '|', '^', '~', '&'); got != `pre\X20\post` {
		t.Errorf("unknown passthrough = %q", got)
	}
	// Malformed (no closing escape).
	if got := hl7Unescape(`prefix\F`, '\\', '|', '^', '~', '&'); got != `prefix\F` {
		t.Errorf("malformed = %q", got)
	}
	// String without escapes → short-circuit return.
	if got := hl7Unescape("plain", '\\', '|', '^', '~', '&'); got != "plain" {
		t.Errorf("plain = %q", got)
	}
	// Empty.
	if got := hl7Unescape("", '\\', '|', '^', '~', '&'); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestParseHL7Sex(t *testing.T) {
	cases := []struct{ in, want string }{
		{"M", "M"}, {"m", "M"}, {" m ", "M"},
		{"F", "W"}, {"U", "X"}, {"A", "D"},
		{"", ""}, {"Q", ""},
	}
	for _, c := range cases {
		if got := parseHL7Sex(c.in); got != c.want {
			t.Errorf("parseHL7Sex(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCondDateValue(t *testing.T) {
	if got := condDateValue("19720314"); got != "19720314" {
		t.Errorf("valid = %q", got)
	}
	if got := condDateValue(""); got != "" {
		t.Errorf("empty = %q", got)
	}
	if got := condDateValue("abcd1234"); got != "" {
		t.Errorf("non-numeric = %q", got)
	}
}

func TestParseHL7ADTFile(t *testing.T) {
	src := &egk.CardData{
		Personal:  &egk.PersonalData{LastName: "X", InsurantID: "Y"},
		Insurance: &egk.InsuranceData{InsurerID: "1", InsurerName: "Z"},
	}
	doc, err := hl7v2ADTEncoder{}.Encode(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := writeTempFile(t, doc.Bytes, "in.hl7")
	got, err := ParseHL7ADTFile(path)
	if err != nil {
		t.Fatalf("ParseHL7ADTFile: %v", err)
	}
	if got.Personal == nil || got.Personal.LastName != "X" {
		t.Errorf("got %+v", got.Personal)
	}
}

func TestParseHL7ADTFileMissing(t *testing.T) {
	if _, err := ParseHL7ADTFile("/does/not/exist.hl7"); err == nil {
		t.Error("expected error")
	}
}

func TestParseHL7ADTNilReader(t *testing.T) {
	if _, err := ParseHL7ADT(nil); err == nil {
		t.Error("expected nil reader error")
	}
}

func TestRenderHL7ADTSmoke(t *testing.T) {
	d := &egk.CardData{
		Personal: &egk.PersonalData{
			InsurantID:  "X110407317",
			LastName:    "Müller",
			FirstName:   "Hans",
			BirthDate:   "19720314",
			Gender:      "M",
			Street:      "Hauptstr.",
			HouseNumber: "5",
			City:        "Berlin",
			PostalCode:  "10115",
			Country:     "D",
		},
		Insurance: &egk.InsuranceData{
			InsurerID:   "109519005",
			InsurerName: "TK",
			StartDate:   "20240101",
			InsuredType: "1",
		},
	}
	out := RenderHL7ADT(d, &egk.IKInfo{VKNR: "12345"})
	for _, want := range []string{"MSH-1", "PID-3.1", "X110407317", "Müller", "109519005", "12345", "ADT^A04"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestRenderHL7ADTNilData(t *testing.T) {
	out := RenderHL7ADT(nil, nil)
	if !strings.Contains(out, "MSH-1") {
		t.Error("table should still render with nil data")
	}
}
