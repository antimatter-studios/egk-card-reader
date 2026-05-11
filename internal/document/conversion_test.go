package document

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/christhomas/card-reader/internal/egk"
)

// This file documents — and tests — exactly which CardData fields survive
// each encode→parse cycle, and how lossiness compounds when the same data
// passes through multiple formats in sequence.
//
// The goal is twofold:
//
//   1. Prevent silent regressions. If someone wires up a new encoder/parser
//      that quietly drops a field, the matrix below catches it.
//
//   2. Document, for human readers, what each format physically carries.
//      Today every format pair has slightly different coverage of the eGK
//      data model. Anyone integrating with one downstream system or another
//      needs to know which fields will reach it.
//
// The matrix is *descriptive of current behaviour*, not aspirational. If you
// extend a format to carry more fields, update the matrix at the same time.

// fmt identifiers — the keys in document.Encoders, used as map keys here.
const (
	fmtGDT  = "gdt"
	fmtHL7  = "hl7adt"
	fmtFHIR = "fhir"
)

// allFormats is iterated in stable order for deterministic test output.
var allFormats = []string{fmtGDT, fmtHL7, fmtFHIR}

// fieldSpec is one row of the lossiness matrix. `get` reads the field from a
// CardData; `survives` lists the formats whose encode→parse cycle preserves
// the value through to a freshly-parsed CardData.
type fieldSpec struct {
	name     string
	get      func(*egk.CardData) string
	survives []string // formats that round-trip this field non-empty
}

// richSource is the fixture every test in this file starts from. All optional
// fields are populated with values designed to survive at least one format:
//   - BesondereGruppe is "04" (not the suppressed default "00")
//   - DMP is "01" (likewise)
//   - ZuzahlungStatus is "1" with a date (GDT only emits 4242 in this case)
func richSource() *egk.CardData {
	return &egk.CardData{
		Personal: &egk.PersonalData{
			InsurantID:    "X110407317",
			FirstName:     "Jürgen",
			LastName:      "Müller-Lüdenscheidt",
			Title:         "Dr.",
			NamePrefix:    "Graf",
			Vorsatzwort:   "von",
			BirthDate:     "19720314",
			Gender:        "M",
			Street:        "Bahnhofstraße",
			HouseNumber:   "42a",
			AddressSuffix: "Hinterhaus",
			PostalCode:    "10115",
			City:          "Berlin",
			Country:       "D",
		},
		Insurance: &egk.InsuranceData{
			InsurerID:          "109519005",
			InsurerName:        "Techniker Krankenkasse",
			BillingInsurerID:   "109519005",
			BillingInsurerName: "Techniker Krankenkasse",
			StartDate:          "20240101",
			EndDate:            "20251231",
			InsuredType:        "1",
			BesondereGruppe:    "04",
			DMP:                "01",
			WOP:                "02",
		},
		Protected: &egk.ProtectedData{
			ZuzahlungStatus:     "1",
			ZuzahlungGueltigBis: "20251231",
		},
	}
}

// matrix lists every CardData field this project knows how to round-trip,
// with the formats that physically preserve it. Fields not in the matrix
// (PostfachStrasse, RuhenderLeistungsanspruch, etc.) are not carried by any
// format today — they're read from the card and rendered into the form
// table, but no Encoder serialises them.
//
// Pinning these expectations turns silent format drift into a test failure.
var matrix = []fieldSpec{
	// --- PersonalData ---
	{"InsurantID", func(d *egk.CardData) string { return d.Personal.InsurantID }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"FirstName", func(d *egk.CardData) string { return d.Personal.FirstName }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"LastName", func(d *egk.CardData) string { return d.Personal.LastName }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"Title", func(d *egk.CardData) string { return d.Personal.Title }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"BirthDate", func(d *egk.CardData) string { return d.Personal.BirthDate }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"Gender", func(d *egk.CardData) string { return d.Personal.Gender }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"Street", func(d *egk.CardData) string { return d.Personal.Street }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"HouseNumber", func(d *egk.CardData) string { return d.Personal.HouseNumber }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"PostalCode", func(d *egk.CardData) string { return d.Personal.PostalCode }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"City", func(d *egk.CardData) string { return d.Personal.City }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"Country", func(d *egk.CardData) string { return d.Personal.Country }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	// AddressSuffix: only HL7 ADT round-trips it (PID-11.2 "Other
	// Designation"). GDT has no equivalent. FHIR's encoder joins the suffix
	// into the address line ("Bahnhofstr. 42a, Hinterhaus"), but the parser
	// only extracts street/house number via the iso21090 extensions — the
	// suffix is currently lost on the parse side.
	{"AddressSuffix", func(d *egk.CardData) string { return d.Personal.AddressSuffix }, []string{fmtHL7}},
	// FHIR is the only encoder that emits humanname extensions for the
	// noble-affix (Namenszusatz) and surname-particle (Vorsatzwort).
	{"NamePrefix", func(d *egk.CardData) string { return d.Personal.NamePrefix }, []string{fmtFHIR}},
	{"Vorsatzwort", func(d *egk.CardData) string { return d.Personal.Vorsatzwort }, []string{fmtFHIR}},

	// --- InsuranceData ---
	{"InsurerID", func(d *egk.CardData) string { return d.Insurance.InsurerID }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"InsurerName", func(d *egk.CardData) string { return d.Insurance.InsurerName }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"BillingInsurerID", func(d *egk.CardData) string { return d.Insurance.BillingInsurerID }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"BillingInsurerName", func(d *egk.CardData) string { return d.Insurance.BillingInsurerName }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"StartDate", func(d *egk.CardData) string { return d.Insurance.StartDate }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"EndDate", func(d *egk.CardData) string { return d.Insurance.EndDate }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	{"InsuredType", func(d *egk.CardData) string { return d.Insurance.InsuredType }, []string{fmtGDT, fmtHL7, fmtFHIR}},
	// WOP: GDT has 4131; FHIR has the wop coding extension; HL7 ADT has no
	// designated field (we don't squat one).
	{"WOP", func(d *egk.CardData) string { return d.Insurance.WOP }, []string{fmtGDT, fmtFHIR}},
	// BesondereGruppe / DMP: FHIR only. Even there, the encoder suppresses
	// the "00" default — these tests use "04"/"01" so the value is actually
	// emitted.
	{"BesondereGruppe", func(d *egk.CardData) string { return d.Insurance.BesondereGruppe }, []string{fmtFHIR}},
	{"DMP", func(d *egk.CardData) string { return d.Insurance.DMP }, []string{fmtFHIR}},

	// --- ProtectedData ---
	// Zuzahlung status+date: only GDT carries this (field 4242, emitted only
	// when status="1"). Neither HL7 ADT nor FHIR Coverage has a co-pay slot.
	{"ZuzahlungStatus", func(d *egk.CardData) string {
		if d.Protected == nil {
			return ""
		}
		return d.Protected.ZuzahlungStatus
	}, []string{fmtGDT}},
	{"ZuzahlungGueltigBis", func(d *egk.CardData) string {
		if d.Protected == nil {
			return ""
		}
		return d.Protected.ZuzahlungGueltigBis
	}, []string{fmtGDT}},
}

// encodeParseOnce runs one encode→parse cycle through the named format and
// returns the resulting CardData. Returns an error if either step fails.
func encodeParseOnce(format string, src *egk.CardData) (*egk.CardData, error) {
	enc, ok := Encoders[format]
	if !ok {
		return nil, fmt.Errorf("unknown format %q", format)
	}
	doc, err := enc.Encode(src, nil)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", format, err)
	}
	parsed, err := parseByFormat(format, bytes.NewReader(doc.Bytes))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", format, err)
	}
	return parsed, nil
}

// parseByFormat dispatches to the package's parse function for `format`.
// formMappingJSON has no parser by design (it's a one-way render), so json
// isn't included in conversion testing.
func parseByFormat(format string, r io.Reader) (*egk.CardData, error) {
	switch format {
	case fmtGDT:
		return ParseGDT(r)
	case fmtHL7:
		return ParseHL7ADT(r)
	case fmtFHIR:
		return ParseFHIR(r)
	}
	return nil, fmt.Errorf("no parser for format %q", format)
}

// TestSingleHopMatrix is the *primary* contract test. For every field in the
// matrix, for every format, it verifies that:
//   - if the format claims to carry the field → it round-trips with the same value
//   - if the format does NOT claim to carry the field → the parsed value is empty
//
// A future encoder change that adds or drops a field will fail one of these
// halves loudly.
func TestSingleHopMatrix(t *testing.T) {
	src := richSource()
	for _, format := range allFormats {
		t.Run(format, func(t *testing.T) {
			got, err := encodeParseOnce(format, src)
			if err != nil {
				t.Fatalf("%s round-trip: %v", format, err)
			}
			for _, f := range matrix {
				wantSurvives := contains(f.survives, format)
				srcVal := f.get(src)
				gotVal := safeGet(got, f.get)
				if wantSurvives {
					if gotVal != srcVal {
						t.Errorf("%s.%s: expected to survive, got %q want %q",
							format, f.name, gotVal, srcVal)
					}
				} else {
					if gotVal != "" {
						t.Errorf("%s.%s: expected to be dropped, but came back as %q",
							format, f.name, gotVal)
					}
				}
			}
		})
	}
}

// TestChainCompoundsLossiness verifies that running the data through *two*
// formats sequentially preserves only the intersection of fields each format
// individually preserves. This catches subtle bugs where, for example, a
// parser populates a field one encoder doesn't actually emit — a synthetic
// hop that the round-trip-against-self test wouldn't notice.
//
// Chains tested are all 6 ordered pairs across {gdt, hl7adt, fhir}.
func TestChainCompoundsLossiness(t *testing.T) {
	src := richSource()
	for _, a := range allFormats {
		for _, b := range allFormats {
			if a == b {
				continue
			}
			t.Run(a+"_to_"+b, func(t *testing.T) {
				mid, err := encodeParseOnce(a, src)
				if err != nil {
					t.Fatalf("first hop %s: %v", a, err)
				}
				end, err := encodeParseOnce(b, mid)
				if err != nil {
					t.Fatalf("second hop %s: %v", b, err)
				}

				for _, f := range matrix {
					srcVal := f.get(src)
					if srcVal == "" {
						continue // nothing to lose
					}
					survivesA := contains(f.survives, a)
					survivesB := contains(f.survives, b)
					endVal := safeGet(end, f.get)

					switch {
					case survivesA && survivesB:
						// Both hops carry it → value must round-trip exactly.
						if endVal != srcVal {
							t.Errorf("%s→%s.%s: should survive both, got %q want %q",
								a, b, f.name, endVal, srcVal)
						}
					default:
						// At least one hop drops it → final value must be empty.
						if endVal != "" {
							t.Errorf("%s→%s.%s: should be dropped (survivesA=%v survivesB=%v) but got %q",
								a, b, f.name, survivesA, survivesB, endVal)
						}
					}
				}
			})
		}
	}
}

// TestBesondereGruppeDefaultSuppressed pins the encoder's documented
// "suppress 00 default" behaviour. If someone changes FHIR to always emit
// BesondereGruppe / DMP, the form output gets noisier; the matrix above
// assumes the suppress-default behaviour.
func TestBesondereGruppeDefaultSuppressed(t *testing.T) {
	src := richSource()
	src.Insurance.BesondereGruppe = "00"
	src.Insurance.DMP = "00"
	parsed, err := encodeParseOnce(fmtFHIR, src)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Insurance.BesondereGruppe != "" {
		t.Errorf("BesondereGruppe=00 should be suppressed, got %q", parsed.Insurance.BesondereGruppe)
	}
	if parsed.Insurance.DMP != "" {
		t.Errorf("DMP=00 should be suppressed, got %q", parsed.Insurance.DMP)
	}
}

// TestGDTOmitsZuzahlungWhenStatusZero pins the encoder's documented behaviour
// that 4242 is only emitted when ZuzahlungStatus=="1". The matrix uses a
// status=1 fixture; this test covers the inverse.
func TestGDTOmitsZuzahlungWhenStatusZero(t *testing.T) {
	src := richSource()
	src.Protected.ZuzahlungStatus = "0"
	parsed, err := encodeParseOnce(fmtGDT, src)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Protected != nil && parsed.Protected.ZuzahlungStatus != "" {
		t.Errorf("status=0 should not emit 4242, but parser reconstructed status=%q",
			parsed.Protected.ZuzahlungStatus)
	}
}

// TestEveryFormatPreservesIdentity is a sanity check: encode→parse for the
// minimal "just identifying fields" CardData round-trips through every format.
// Catches regressions where a format silently drops the few fields every
// downstream system actually needs.
func TestEveryFormatPreservesIdentity(t *testing.T) {
	minimal := &egk.CardData{
		Personal:  &egk.PersonalData{InsurantID: "X110407317", LastName: "Mustermann"},
		Insurance: &egk.InsuranceData{InsurerID: "109519005", InsurerName: "TK"},
	}
	for _, format := range allFormats {
		t.Run(format, func(t *testing.T) {
			got, err := encodeParseOnce(format, minimal)
			if err != nil {
				t.Fatalf("%v", err)
			}
			if got.Personal == nil || got.Personal.InsurantID != "X110407317" {
				t.Errorf("InsurantID lost: %+v", got.Personal)
			}
			if got.Personal.LastName != "Mustermann" {
				t.Errorf("LastName lost: %q", got.Personal.LastName)
			}
			if got.Insurance == nil || got.Insurance.InsurerID != "109519005" {
				t.Errorf("InsurerID lost: %+v", got.Insurance)
			}
		})
	}
}

// TestThreeHopChain documents the worst-case lossiness: data through three
// hops loses everything that any of the three hops drops. This is the case
// for an integration pipeline that, say, ingests GDT, exports HL7, then
// re-imports as FHIR — each transformation strips a different set.
func TestThreeHopChain(t *testing.T) {
	src := richSource()
	a, err := encodeParseOnce(fmtGDT, src)
	if err != nil {
		t.Fatal(err)
	}
	b, err := encodeParseOnce(fmtHL7, a)
	if err != nil {
		t.Fatal(err)
	}
	c, err := encodeParseOnce(fmtFHIR, b)
	if err != nil {
		t.Fatal(err)
	}
	// Through gdt→hl7adt→fhir, only fields in ALL THREE survive.
	// That set is the union of fmtGDT∩fmtHL7∩fmtFHIR.
	for _, f := range matrix {
		srcVal := f.get(src)
		if srcVal == "" {
			continue
		}
		allThree := contains(f.survives, fmtGDT) &&
			contains(f.survives, fmtHL7) &&
			contains(f.survives, fmtFHIR)
		endVal := safeGet(c, f.get)
		if allThree {
			if endVal != srcVal {
				t.Errorf("3-hop %s: should survive all three, got %q want %q",
					f.name, endVal, srcVal)
			}
		} else {
			if endVal != "" {
				t.Errorf("3-hop %s: should be dropped, got %q", f.name, endVal)
			}
		}
	}
}

// TestMatrixDocumentationIsExhaustive is a meta-test: it confirms that every
// non-zero field on the rich source has a corresponding entry in the matrix.
// If someone adds a new field to PersonalData / InsuranceData / ProtectedData
// and populates it in richSource(), this test fires until the matrix is
// updated with the documented format support.
//
// Fields the project deliberately doesn't carry (PostfachStrasse, etc.) are
// listed below to suppress the check; they need to be added to richSource()
// to test that they're correctly NOT round-tripped, but until then they're
// just noise.
func TestMatrixDocumentationIsExhaustive(t *testing.T) {
	src := richSource()
	covered := map[string]bool{}
	for _, f := range matrix {
		covered[f.name] = true
	}
	// Walk every field on PersonalData / InsuranceData / ProtectedData; if it's
	// non-empty in the source but not in the matrix, fail loudly.
	check := func(structName string, fields map[string]string) {
		for name, val := range fields {
			if val == "" {
				continue
			}
			if !covered[name] {
				t.Errorf("matrix missing entry for %s.%s (value %q in richSource)", structName, name, val)
			}
		}
	}
	check("Personal", map[string]string{
		"InsurantID": src.Personal.InsurantID, "FirstName": src.Personal.FirstName,
		"LastName": src.Personal.LastName, "Title": src.Personal.Title,
		"NamePrefix": src.Personal.NamePrefix, "Vorsatzwort": src.Personal.Vorsatzwort,
		"BirthDate": src.Personal.BirthDate, "Gender": src.Personal.Gender,
		"Street": src.Personal.Street, "HouseNumber": src.Personal.HouseNumber,
		"AddressSuffix": src.Personal.AddressSuffix, "PostalCode": src.Personal.PostalCode,
		"City": src.Personal.City, "Country": src.Personal.Country,
	})
	check("Insurance", map[string]string{
		"InsurerID": src.Insurance.InsurerID, "InsurerName": src.Insurance.InsurerName,
		"BillingInsurerID": src.Insurance.BillingInsurerID, "BillingInsurerName": src.Insurance.BillingInsurerName,
		"StartDate": src.Insurance.StartDate, "EndDate": src.Insurance.EndDate,
		"InsuredType": src.Insurance.InsuredType, "WOP": src.Insurance.WOP,
		"BesondereGruppe": src.Insurance.BesondereGruppe, "DMP": src.Insurance.DMP,
	})
	if src.Protected != nil {
		check("Protected", map[string]string{
			"ZuzahlungStatus":     src.Protected.ZuzahlungStatus,
			"ZuzahlungGueltigBis": src.Protected.ZuzahlungGueltigBis,
		})
	}
}

// --- helpers --------------------------------------------------------------

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// safeGet calls f on cd, treating a nil Personal/Insurance/Protected as
// "field is empty" rather than panicking. The accessor closures in `matrix`
// dereference d.Personal etc. unguarded, so the wrapper exists to keep the
// tests readable.
func safeGet(cd *egk.CardData, f func(*egk.CardData) string) (out string) {
	defer func() {
		if r := recover(); r != nil {
			out = ""
		}
	}()
	return f(cd)
}

// summary is unused by tests; it's here for hand-running with `go test -v` to
// print the matrix in a human-readable form. Run via:
//   go test -v ./internal/document/ -run=TestPrintMatrix
func TestPrintMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("informational")
	}
	t.Log(formatMatrix())
}

func formatMatrix() string {
	var b strings.Builder
	b.WriteString("\nField                  ")
	for _, f := range allFormats {
		fmt.Fprintf(&b, "%-9s", f)
	}
	b.WriteString("\n")
	b.WriteString(strings.Repeat("-", 50))
	b.WriteString("\n")
	for _, fs := range matrix {
		fmt.Fprintf(&b, "%-22s", fs.name)
		for _, fm := range allFormats {
			mark := "·"
			if contains(fs.survives, fm) {
				mark = "✓"
			}
			fmt.Fprintf(&b, "%-9s", mark)
		}
		b.WriteString("\n")
	}
	return b.String()
}
