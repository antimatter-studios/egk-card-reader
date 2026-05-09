package document

import (
	"bytes"
	"testing"

	"github.com/christhomas/card-reader/internal/egk"
)

func TestHL7ADTRoundTrip(t *testing.T) {
	original := &egk.CardData{
		Personal: &egk.PersonalData{
			InsurantID:    "X110407317",
			FirstName:     "Jürgen",
			LastName:      "Müller-Lüdenscheidt",
			Title:         "Dr.",
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
		},
	}

	doc, err := hl7v2ADTEncoder{}.Encode(original, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(doc.Bytes) == 0 {
		t.Fatal("encoder produced no output")
	}

	got, err := ParseHL7ADT(bytes.NewReader(doc.Bytes))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Personal == nil {
		t.Fatal("Personal nil after round-trip")
	}
	if got.Insurance == nil {
		t.Fatal("Insurance nil after round-trip")
	}

	op := original.Personal
	gp := got.Personal
	for _, c := range []struct {
		name, want, got string
	}{
		{"InsurantID", op.InsurantID, gp.InsurantID},
		{"FirstName", op.FirstName, gp.FirstName},
		{"LastName", op.LastName, gp.LastName},
		{"Title", op.Title, gp.Title},
		{"BirthDate", op.BirthDate, gp.BirthDate},
		{"Gender", op.Gender, gp.Gender},
		{"Street", op.Street, gp.Street},
		{"HouseNumber", op.HouseNumber, gp.HouseNumber},
		{"AddressSuffix", op.AddressSuffix, gp.AddressSuffix},
		{"PostalCode", op.PostalCode, gp.PostalCode},
		{"City", op.City, gp.City},
		{"Country", op.Country, gp.Country},
	} {
		if c.want != c.got {
			t.Errorf("Personal.%s: want %q, got %q", c.name, c.want, c.got)
		}
	}

	oi := original.Insurance
	gi := got.Insurance
	for _, c := range []struct {
		name, want, got string
	}{
		{"InsurerID", oi.InsurerID, gi.InsurerID},
		{"InsurerName", oi.InsurerName, gi.InsurerName},
		{"BillingInsurerID", oi.BillingInsurerID, gi.BillingInsurerID},
		{"BillingInsurerName", oi.BillingInsurerName, gi.BillingInsurerName},
		{"StartDate", oi.StartDate, gi.StartDate},
		{"EndDate", oi.EndDate, gi.EndDate},
		{"InsuredType", oi.InsuredType, gi.InsuredType},
	} {
		if c.want != c.got {
			t.Errorf("Insurance.%s: want %q, got %q", c.name, c.want, c.got)
		}
	}
}

func TestHL7ADTParseToleratesBareLF(t *testing.T) {
	src := &egk.CardData{
		Personal: &egk.PersonalData{
			InsurantID: "X110407317",
			LastName:   "Müller",
			FirstName:  "Anna",
			BirthDate:  "19800505",
			Gender:     "W",
		},
		Insurance: &egk.InsuranceData{
			InsurerID:   "109519005",
			InsurerName: "Techniker Krankenkasse",
		},
	}
	doc, err := hl7v2ADTEncoder{}.Encode(src, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	bareLF := bytes.ReplaceAll(doc.Bytes, []byte("\r\n"), []byte("\n"))
	got, err := ParseHL7ADT(bytes.NewReader(bareLF))
	if err != nil {
		t.Fatalf("parse bare-LF: %v", err)
	}
	if got.Personal == nil || got.Personal.LastName != "Müller" {
		t.Errorf("expected Müller, got %+v", got.Personal)
	}
	if got.Personal.Gender != "W" {
		t.Errorf("expected W, got %q", got.Personal.Gender)
	}
}

func TestHL7ADTParseEmptyReturnsError(t *testing.T) {
	_, err := ParseHL7ADT(bytes.NewReader([]byte{}))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestHL7ADTParseToleratesBOM(t *testing.T) {
	src := &egk.CardData{
		Personal: &egk.PersonalData{
			InsurantID: "X110407317",
			LastName:   "Schmidt",
			FirstName:  "Eva",
			BirthDate:  "19900101",
			Gender:     "W",
		},
		Insurance: &egk.InsuranceData{
			InsurerID:   "109519005",
			InsurerName: "TK",
		},
	}
	doc, err := hl7v2ADTEncoder{}.Encode(src, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, doc.Bytes...)
	got, err := ParseHL7ADT(bytes.NewReader(withBOM))
	if err != nil {
		t.Fatalf("parse BOM: %v", err)
	}
	if got.Personal == nil || got.Personal.LastName != "Schmidt" {
		t.Errorf("expected Schmidt, got %+v", got.Personal)
	}
}
