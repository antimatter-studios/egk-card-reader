package document

import (
	"bytes"
	"testing"

	"github.com/christhomas/card-reader/pkg/egk"
)

// fuzzSeedCardData is a small but populated CardData used to derive encoded
// seed bytes for each fuzz target.
func fuzzSeedCardData() *egk.CardData {
	return &egk.CardData{
		Personal: &egk.PersonalData{
			InsurantID:  "X110407317",
			FirstName:   "Anna",
			LastName:    "Müller",
			Title:       "Dr.",
			BirthDate:   "19800505",
			Gender:      "W",
			Street:      "Bahnhofstr.",
			HouseNumber: "42",
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
	}
}

func FuzzParseGDT(f *testing.F) {
	if doc, err := (gdtEncoder{}).Encode(fuzzSeedCardData(), nil); err == nil {
		f.Add(doc.Bytes)
	}
	f.Add([]byte(""))
	f.Add([]byte("invalid"))
	f.Add([]byte("01380008230\r\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseGDT(bytes.NewReader(data))
	})
}

func FuzzParseHL7ADT(f *testing.F) {
	if doc, err := (hl7v2ADTEncoder{}).Encode(fuzzSeedCardData(), nil); err == nil {
		f.Add(doc.Bytes)
	}
	f.Add([]byte(""))
	f.Add([]byte("MSH|^~\\&|\r"))
	f.Add([]byte("not an hl7 message"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseHL7ADT(bytes.NewReader(data))
	})
}

func FuzzParseFHIR(f *testing.F) {
	if doc, err := (fhirEncoder{}).Encode(fuzzSeedCardData(), nil); err == nil {
		f.Add(doc.Bytes)
	}
	f.Add([]byte(""))
	f.Add([]byte("{}"))
	f.Add([]byte(`{"resourceType":"Bundle","entry":[]}`))
	f.Add([]byte("not json"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseFHIR(bytes.NewReader(data))
	})
}
