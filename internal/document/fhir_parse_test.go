package document

import (
	"bytes"
	"testing"

	"github.com/christhomas/card-reader/internal/egk"
)

func TestFHIRRoundTrip(t *testing.T) {
	original := &egk.CardData{
		Personal: &egk.PersonalData{
			InsurantID:  "X110407317",
			FirstName:   "Jürgen",
			LastName:    "Müller-Lüdenscheidt",
			Title:       "Dr.",
			NamePrefix:  "Graf",
			Vorsatzwort: "von",
			BirthDate:   "19720314",
			Gender:      "M",
			Street:      "Bahnhofstraße",
			HouseNumber: "42a",
			PostalCode:  "10115",
			City:        "Berlin",
			Country:     "D",
		},
		Insurance: &egk.InsuranceData{
			InsurerID:          "109519005",
			InsurerName:        "Techniker Krankenkasse",
			BillingInsurerID:   "109519005",
			BillingInsurerName: "Techniker Krankenkasse",
			StartDate:          "20240101",
			EndDate:            "20251231",
			InsuredType:        "1",
			WOP:                "02",
			BesondereGruppe:    "04",
			DMP:                "01",
		},
	}

	doc, err := fhirEncoder{}.Encode(original, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(doc.Bytes) == 0 {
		t.Fatal("encoder produced no output")
	}

	got, err := ParseFHIR(bytes.NewReader(doc.Bytes))
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
		{"NamePrefix", op.NamePrefix, gp.NamePrefix},
		{"Vorsatzwort", op.Vorsatzwort, gp.Vorsatzwort},
		{"BirthDate", op.BirthDate, gp.BirthDate},
		{"Gender", op.Gender, gp.Gender},
		{"Street", op.Street, gp.Street},
		{"HouseNumber", op.HouseNumber, gp.HouseNumber},
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
		{"WOP", oi.WOP, gi.WOP},
		{"BesondereGruppe", oi.BesondereGruppe, gi.BesondereGruppe},
		{"DMP", oi.DMP, gi.DMP},
	} {
		if c.want != c.got {
			t.Errorf("Insurance.%s: want %q, got %q", c.name, c.want, c.got)
		}
	}
}

func TestFHIRParseAddressFallbackSplit(t *testing.T) {
	// Address line with no extensions — parser should fall back to last-space split.
	src := []byte(`{
  "resourceType": "Bundle",
  "type": "collection",
  "entry": [
    {
      "resource": {
        "resourceType": "Patient",
        "name": [{"family": "Schmidt"}],
        "address": [{"line": ["Unter den Linden 5"], "city": "Berlin", "postalCode": "10117", "country": "D"}]
      }
    }
  ]
}`)
	got, err := ParseFHIR(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Personal == nil {
		t.Fatal("Personal nil")
	}
	if got.Personal.Street != "Unter den Linden" {
		t.Errorf("Street: want %q, got %q", "Unter den Linden", got.Personal.Street)
	}
	if got.Personal.HouseNumber != "5" {
		t.Errorf("HouseNumber: want %q, got %q", "5", got.Personal.HouseNumber)
	}
}
