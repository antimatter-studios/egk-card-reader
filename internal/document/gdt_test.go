package document

import (
	"strings"
	"testing"

	"github.com/christhomas/card-reader/internal/egk"
)

func TestGDTLineISO885915Length(t *testing.T) {
	// "Müller" is 6 chars but 6 bytes in ISO-8859-15 (ü → 0xFC) — line length
	// is computed against encoded bytes, so total = 3 + 4 + 6 + 2 = 15.
	got, err := gdtLine("3101", "Müller")
	if err != nil {
		t.Fatalf("gdtLine: %v", err)
	}
	if !strings.HasPrefix(string(got[:3]), "015") {
		t.Errorf("length prefix wrong: %q", got[:3])
	}
	if got[len(got)-2] != '\r' || got[len(got)-1] != '\n' {
		t.Error("missing CR LF terminator")
	}
}

func TestGDTLineUnencodableChar(t *testing.T) {
	// Emoji isn't in ISO-8859-15 → encoder errors.
	if _, err := gdtLine("3101", "Hello 🌍"); err == nil {
		t.Error("expected encoder error for non-Latin-9 char")
	}
}

func TestGdtDate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"19720314", "14031972"},
		{"20240101", "01012024"},
		{"", ""},
		{"abc", ""},
		{"1972031", ""},      // 7 chars
		{"197203144", ""},    // 9 chars
		{"1972A314", ""},     // non-numeric
	}
	for _, c := range cases {
		if got := gdtDate(c.in); got != c.want {
			t.Errorf("gdtDate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGdtSex(t *testing.T) {
	cases := []struct{ in, want string }{
		{"M", "1"}, {"m", "1"},
		{"W", "2"}, {"F", "2"},
		{"X", "3"}, {"D", "4"},
		{"", ""}, {"Q", ""},
	}
	for _, c := range cases {
		if got := gdtSex(c.in); got != c.want {
			t.Errorf("gdtSex(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGdtVKNR(t *testing.T) {
	if got := gdtVKNR(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
	if got := gdtVKNR(&egk.IKInfo{VKNR: "12345"}); got != "12345" {
		t.Errorf("set = %q", got)
	}
	if got := gdtVKNR(&egk.IKInfo{}); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestParseGDTDate(t *testing.T) {
	if got := parseGDTDate("14031972"); got != "19720314" {
		t.Errorf("date = %q", got)
	}
	if got := parseGDTDate(""); got != "" {
		t.Errorf("empty = %q", got)
	}
	if got := parseGDTDate("abc"); got != "" {
		t.Errorf("bad = %q", got)
	}
	if got := parseGDTDate("123"); got != "" {
		t.Errorf("short = %q", got)
	}
	if got := parseGDTDate("14X31972"); got != "" {
		t.Errorf("non-numeric = %q", got)
	}
}

func TestParseGDTSex(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1", "M"}, {"2", "W"}, {"3", "X"}, {"4", "D"},
		{"", ""}, {"9", ""},
	}
	for _, c := range cases {
		if got := parseGDTSex(c.in); got != c.want {
			t.Errorf("parseGDTSex(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseGDTFile(t *testing.T) {
	// Round-trip via tempfile: write a GDT doc, parse it back via the file API.
	src := &egk.CardData{
		Personal:  &egk.PersonalData{LastName: "Müller", InsurantID: "X1"},
		Insurance: &egk.InsuranceData{InsurerID: "1"},
	}
	doc, err := gdtEncoder{}.Encode(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := writeTempFile(t, doc.Bytes, "in.gdt")
	got, err := ParseGDTFile(path)
	if err != nil {
		t.Fatalf("ParseGDTFile: %v", err)
	}
	if got.Personal == nil || got.Personal.LastName != "Müller" {
		t.Errorf("got %+v", got.Personal)
	}
}

func TestParseGDTFileMissing(t *testing.T) {
	if _, err := ParseGDTFile("/does/not/exist.gdt"); err == nil {
		t.Error("expected error")
	}
}

func TestParseGDTNilReader(t *testing.T) {
	if _, err := ParseGDT(nil); err == nil {
		t.Error("expected nil reader error")
	}
}

func TestParseGDTSkipsUnknownFields(t *testing.T) {
	// A line with an unknown 4-digit field code is silently dropped.
	src := &egk.CardData{
		Personal:  &egk.PersonalData{LastName: "Müller", InsurantID: "X1"},
		Insurance: &egk.InsuranceData{InsurerID: "1"},
	}
	doc, err := gdtEncoder{}.Encode(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Inject an unknown field "9999" mid-stream.
	extra, err := gdtLine("9999", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	mixed := append(doc.Bytes, extra...)

	got, err := ParseGDT(byteReader(mixed))
	if err != nil {
		t.Fatalf("ParseGDT: %v", err)
	}
	if got.Personal == nil {
		t.Error("Personal should still parse")
	}
}

func TestRenderGDTSmoke(t *testing.T) {
	d := &egk.CardData{
		Personal: &egk.PersonalData{
			InsurantID:  "X110407317",
			FirstName:   "Hans",
			LastName:    "Müller",
			BirthDate:   "19720314",
			Street:      "Bahnhofstr.",
			HouseNumber: "42",
			PostalCode:  "10115",
			City:        "Berlin",
		},
		Insurance: &egk.InsuranceData{
			InsurerID:   "109519005",
			InsurerName: "TK",
			StartDate:   "20240101",
			InsuredType: "1",
		},
		Protected: &egk.ProtectedData{ZuzahlungStatus: "1", ZuzahlungGueltigBis: "20251231"},
	}
	ik := &egk.IKInfo{VKNR: "12345"}
	out := RenderGDT(d, ik)
	for _, want := range []string{
		"6301", "8000", "8100", "Müller", "Hans",
		"109519005", "Total record length", "Stammdaten",
		"12345", // VKNR row
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in rendered output", want)
		}
	}
}

func TestRenderGDTNilData(t *testing.T) {
	// Nil/empty CardData should still render a table (everything blank).
	out := RenderGDT(nil, nil)
	if !strings.Contains(out, "6301") {
		t.Error("table should still render with nil data")
	}
}

func TestEncodeGDTPatchesLengthCorrectly(t *testing.T) {
	d := &egk.CardData{
		Personal:  &egk.PersonalData{LastName: "X", InsurantID: "Y"},
		Insurance: &egk.InsuranceData{InsurerID: "1"},
	}
	doc, err := gdtEncoder{}.Encode(d, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// 8100 holds the total record length. Find the line.
	lines := strings.Split(string(doc.Bytes), "\r\n")
	var found bool
	for _, l := range lines {
		if len(l) >= 7 && l[3:7] == "8100" {
			value := l[7:]
			if value == "00000" {
				t.Error("8100 placeholder not patched")
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("8100 line not present")
	}
}
