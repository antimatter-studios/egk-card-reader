package document

import (
	"bytes"
	"testing"

	"github.com/antimatter-studios/egk-card-reader/pkg/egk"
)

func TestGDTRoundTrip(t *testing.T) {
	original := &egk.CardData{
		Personal: &egk.PersonalData{
			InsurantID:  "X110407317",
			FirstName:   "Jürgen",
			LastName:    "Müller-Lüdenscheidt",
			Title:       "Dr.",
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
		},
		Protected: &egk.ProtectedData{
			ZuzahlungStatus:     "1",
			ZuzahlungGueltigBis: "20251231",
		},
	}

	doc, err := gdtEncoder{}.Encode(original, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(doc.Bytes) == 0 {
		t.Fatal("encoder produced no output")
	}

	got, err := ParseGDT(bytes.NewReader(doc.Bytes))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got.Personal == nil {
		t.Fatal("Personal nil after round-trip")
	}
	if got.Insurance == nil {
		t.Fatal("Insurance nil after round-trip")
	}
	if got.Protected == nil {
		t.Fatal("Protected nil after round-trip")
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
	} {
		if c.want != c.got {
			t.Errorf("Insurance.%s: want %q, got %q", c.name, c.want, c.got)
		}
	}

	og := original.Protected
	gg := got.Protected
	if og.ZuzahlungStatus != gg.ZuzahlungStatus {
		t.Errorf("Protected.ZuzahlungStatus: want %q, got %q", og.ZuzahlungStatus, gg.ZuzahlungStatus)
	}
	if og.ZuzahlungGueltigBis != gg.ZuzahlungGueltigBis {
		t.Errorf("Protected.ZuzahlungGueltigBis: want %q, got %q", og.ZuzahlungGueltigBis, gg.ZuzahlungGueltigBis)
	}
}

func TestGDTParseToleratesBareLF(t *testing.T) {
	// Same content as a normal GDT line but using bare LF instead of CRLF.
	// "01331023Müller" — but compute length carefully: 3(LLL)+4(FFFF)+6(value bytes ISO-8859-15)+2(CRLF)=15
	// We'll construct it using the encoder, then strip \r.
	src := &egk.CardData{
		Personal: &egk.PersonalData{
			InsurantID: "X110407317",
			LastName:   "Müller",
			FirstName:  "Anna",
			BirthDate:  "19800505",
			Gender:     "W",
		},
	}
	doc, err := gdtEncoder{}.Encode(src, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	bareLF := bytes.ReplaceAll(doc.Bytes, []byte("\r\n"), []byte("\n"))
	got, err := ParseGDT(bytes.NewReader(bareLF))
	if err != nil {
		t.Fatalf("parse bare-LF: %v", err)
	}
	if got.Personal == nil || got.Personal.LastName != "Müller" {
		t.Errorf("expected Müller, got %+v", got.Personal)
	}
}
