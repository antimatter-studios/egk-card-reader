package document

import (
	"strings"
	"testing"

	"github.com/christhomas/card-reader/pkg/egk"
)

func TestADTA04Smoke(t *testing.T) {
	data := &egk.CardData{
		Personal: &egk.PersonalData{
			InsurantID:  "X110407317",
			FirstName:   "Hans",
			LastName:    "Mustermann",
			BirthDate:   "19500101",
			Gender:      "M",
			Street:      "Hauptstr.",
			HouseNumber: "5",
			PostalCode:  "12345",
			City:        "Berlin",
			Country:     "D",
		},
		Insurance: &egk.InsuranceData{
			InsurerID:   "109519005",
			InsurerName: "Techniker Krankenkasse",
			StartDate:   "20240101",
			InsuredType: "1",
			WOP:         "02",
		},
	}
	doc, err := hl7v2ADTEncoder{}.Encode(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Bytes) == 0 {
		t.Fatal("empty output")
	}
	out := string(doc.Bytes)
	for _, want := range []string{"MSH|", "EVN|A04", "PID|1|", "PV1|1|O", "IN1|1|GKV", "Mustermann", "X110407317", "109519005"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output", want)
		}
	}
	if !strings.Contains(out, "\r\n") {
		t.Error("expected \\r\\n segment terminator")
	}
	t.Logf("len=%d", len(doc.Bytes))
	t.Logf("output:\n%s", out)
}
