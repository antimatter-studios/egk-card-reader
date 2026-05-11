package egk

import (
	"strings"
	"testing"
	"time"
)

func TestFormFieldFilled(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"", false},
		{"   ", false},
		{"\t\n", false},
		{"value", true},
		{"  v  ", true},
	}
	for _, c := range cases {
		f := FormField{Value: c.v}
		if got := f.Filled(); got != c.want {
			t.Errorf("Filled(%q) = %v, want %v", c.v, got, c.want)
		}
	}
}

func fieldByLabel(fields []FormField, label string) (FormField, bool) {
	for _, f := range fields {
		if f.Label == label {
			return f, true
		}
	}
	return FormField{}, false
}

func TestFormMappingFullCard(t *testing.T) {
	d := &CardData{
		Personal: &PersonalData{
			InsurantID:  "X110407317",
			FirstName:   "Hans",
			LastName:    "Müller",
			BirthDate:   "19720314",
			Street:      "Bahnhofstr.",
			HouseNumber: "42",
			PostalCode:  "10115",
			City:        "Berlin",
		},
		Insurance: &InsuranceData{
			InsurerID:          "109519005",
			InsurerName:        "Issuing TK",
			BillingInsurerID:   "109519999",
			BillingInsurerName: "Billing TK",
			StartDate:          "20240101",
			EndDate:            "20251231",
			InsuredType:        "1",
			BesondereGruppe:    "04",
			DMP:                "01",
			WOP:                "02",
		},
		Protected: &ProtectedData{
			ZuzahlungStatus:     "1",
			ZuzahlungGueltigBis: "20251231",
		},
	}
	ik := &IKInfo{
		Name:                "Techniker Krankenkasse",
		VKNR:                "12345",
		Kassenart:           "EK",
		KostentraegerGruppe: "06",
	}
	fields := FormMapping(d, ik)
	if len(fields) != 23 {
		t.Fatalf("expected 23 fields, got %d", len(fields))
	}

	checks := map[string]string{
		"Abrechnung":                 "GKV",
		"Kasse":                      "Billing TK",
		"IKNR":                       "109519999",
		"KTAB":                       "00",
		"Kostenträgergruppe":         "06",
		"Versicherungsschutz Beginn": "2024-01-01",
		"Besondere Personengruppe":   "04",
		"Adresse Teil 1":             "Bahnhofstr. 42",
		"Adresse Teil 2":             "10115 Berlin",
		"Versicherten-Nr.":           "X110407317",
		"VKNR":                       "12345",
		"Bedruckungsname":            "Müller, Hans",
		"Versichertenart":            "1",
		"WOP":                        "02",
		"DMP-Kennzeichen":            "01",
		"Versicherungsschutz Ende":   "2025-12-31",
		"gebührenbefreit bis Datum":  "2025-12-31",
	}
	for label, want := range checks {
		f, ok := fieldByLabel(fields, label)
		if !ok {
			t.Errorf("missing field %q", label)
			continue
		}
		if f.Value != want {
			t.Errorf("%s: got %q, want %q", label, f.Value, want)
		}
	}

	// "Karte gelesen" is today, format YYYY-MM-DD.
	if f, _ := fieldByLabel(fields, "Karte gelesen"); f.Value != time.Now().Format("2006-01-02") {
		t.Errorf("Karte gelesen got %q", f.Value)
	}

	// Practice-config fields stay empty by design.
	for _, label := range []string{"Gebührenordnung", "Betriebsstätte", "Arzt"} {
		f, _ := fieldByLabel(fields, label)
		if f.Value != "" {
			t.Errorf("%s should be empty, got %q", label, f.Value)
		}
		if f.Source != "practice" {
			t.Errorf("%s source = %q, want practice", label, f.Source)
		}
	}
}

func TestFormMappingFallsBackToIssuingIK(t *testing.T) {
	// Card without AbrechnenderKostentraeger — issuer IK/name should win.
	d := &CardData{
		Personal: &PersonalData{InsurantID: "X1"},
		Insurance: &InsuranceData{
			InsurerID:   "109519005",
			InsurerName: "Issuing Only",
		},
	}
	fields := FormMapping(d, nil)
	iknr, _ := fieldByLabel(fields, "IKNR")
	kasse, _ := fieldByLabel(fields, "Kasse")
	if iknr.Value != "109519005" {
		t.Errorf("IKNR fallback failed: %q", iknr.Value)
	}
	if kasse.Value != "Issuing Only" {
		t.Errorf("Kasse fallback failed: %q", kasse.Value)
	}
}

func TestFormMappingDefaultsBlankCodes(t *testing.T) {
	// BesondereGruppe / DMP empty → form fills "00".
	d := &CardData{
		Insurance: &InsuranceData{
			InsurerID: "109519005",
		},
	}
	fields := FormMapping(d, nil)
	bp, _ := fieldByLabel(fields, "Besondere Personengruppe")
	dmp, _ := fieldByLabel(fields, "DMP-Kennzeichen")
	if bp.Value != "00" {
		t.Errorf("BesondereGruppe default = %q", bp.Value)
	}
	if dmp.Value != "00" {
		t.Errorf("DMP default = %q", dmp.Value)
	}
}

func TestFormMappingCopayStates(t *testing.T) {
	mk := func(status, until string) *CardData {
		return &CardData{
			Insurance: &InsuranceData{InsurerID: "1"},
			Protected: &ProtectedData{ZuzahlungStatus: status, ZuzahlungGueltigBis: until},
		}
	}
	// Status=1 with date → value populated.
	f := FormMapping(mk("1", "20251231"), nil)
	v, _ := fieldByLabel(f, "gebührenbefreit bis Datum")
	if v.Value != "2025-12-31" {
		t.Errorf("status=1 value = %q", v.Value)
	}
	if !strings.Contains(v.Note, "zuzahlungsbefreit") {
		t.Errorf("status=1 note = %q", v.Note)
	}
	// Status=1 missing date.
	f = FormMapping(mk("1", ""), nil)
	v, _ = fieldByLabel(f, "gebührenbefreit bis Datum")
	if v.Value != "" {
		t.Errorf("status=1 no-date value = %q, want empty", v.Value)
	}
	if !strings.Contains(v.Note, "end date missing") {
		t.Errorf("status=1 no-date note = %q", v.Note)
	}
	// Status=0.
	f = FormMapping(mk("0", ""), nil)
	v, _ = fieldByLabel(f, "gebührenbefreit bis Datum")
	if !strings.Contains(v.Note, "nicht zuzahlungsbefreit") {
		t.Errorf("status=0 note = %q", v.Note)
	}
	// Status empty.
	f = FormMapping(mk("", ""), nil)
	v, _ = fieldByLabel(f, "gebührenbefreit bis Datum")
	if !strings.Contains(v.Note, "not on card") {
		t.Errorf("empty status note = %q", v.Note)
	}
	// Status unknown.
	f = FormMapping(mk("9", ""), nil)
	v, _ = fieldByLabel(f, "gebührenbefreit bis Datum")
	if !strings.Contains(v.Note, "unknown") {
		t.Errorf("unknown status note = %q", v.Note)
	}
}

func TestFormMappingNilIKInfoNotes(t *testing.T) {
	d := &CardData{Insurance: &InsuranceData{InsurerID: "1"}}
	fields := FormMapping(d, nil)
	vknr, _ := fieldByLabel(fields, "VKNR")
	if vknr.Value != "" {
		t.Errorf("VKNR value should be empty when no IKInfo, got %q", vknr.Value)
	}
	if !strings.Contains(vknr.Note, "ktda update") {
		t.Errorf("VKNR note = %q", vknr.Note)
	}
	ktg, _ := fieldByLabel(fields, "Kostenträgergruppe")
	if !strings.Contains(ktg.Note, "ktda update") {
		t.Errorf("KTG note = %q", ktg.Note)
	}
}

func TestFormMappingIKInfoPresentNoVKNR(t *testing.T) {
	// IKInfo present but VKNR missing.
	d := &CardData{Insurance: &InsuranceData{InsurerID: "1"}}
	ik := &IKInfo{Name: "X", Kassenart: "EK", KostentraegerGruppe: "06"}
	fields := FormMapping(d, ik)
	v, _ := fieldByLabel(fields, "VKNR")
	if v.Value != "" {
		t.Errorf("VKNR = %q, want empty", v.Value)
	}
	if !strings.Contains(v.Note, "not present in KTDA") {
		t.Errorf("VKNR note = %q", v.Note)
	}
	if v.Source != "lookup" {
		t.Errorf("VKNR source = %q, want lookup", v.Source)
	}
}

func TestFormMappingIKInfoPresentNoKTG(t *testing.T) {
	d := &CardData{Insurance: &InsuranceData{InsurerID: "1"}}
	ik := &IKInfo{Name: "X", VKNR: "12345"}
	fields := FormMapping(d, ik)
	ktg, _ := fieldByLabel(fields, "Kostenträgergruppe")
	if ktg.Value != "" {
		t.Errorf("KTG value = %q, want empty", ktg.Value)
	}
	if !strings.Contains(ktg.Note, "not resolvable") {
		t.Errorf("KTG note = %q", ktg.Note)
	}
}

func TestFormMappingNilCardData(t *testing.T) {
	// Empty CardData should still produce 23 fields with default codes.
	fields := FormMapping(&CardData{}, nil)
	if len(fields) != 23 {
		t.Errorf("expected 23 fields from empty CardData, got %d", len(fields))
	}
	bp, _ := fieldByLabel(fields, "Besondere Personengruppe")
	if bp.Value != "00" {
		t.Errorf("BesondereGruppe default missing: %q", bp.Value)
	}
}

func TestFormMappingAddressSuffix(t *testing.T) {
	d := &CardData{
		Personal: &PersonalData{
			Street:        "Hauptstr.",
			HouseNumber:   "1",
			AddressSuffix: "Hinterhaus",
			PostalCode:    "10115",
			City:          "Berlin",
		},
		Insurance: &InsuranceData{InsurerID: "1"},
	}
	fields := FormMapping(d, nil)
	a1, _ := fieldByLabel(fields, "Adresse Teil 1")
	if !strings.Contains(a1.Value, "Hinterhaus") {
		t.Errorf("address suffix not joined: %q", a1.Value)
	}
}

func TestFormMappingPrintedNameOnlyLast(t *testing.T) {
	// FirstName empty → no comma separator.
	d := &CardData{
		Personal:  &PersonalData{LastName: "Müller"},
		Insurance: &InsuranceData{InsurerID: "1"},
	}
	fields := FormMapping(d, nil)
	bn, _ := fieldByLabel(fields, "Bedruckungsname")
	if bn.Value != "Müller" {
		t.Errorf("Bedruckungsname = %q", bn.Value)
	}
}

func TestFormMappingSelektivContracts(t *testing.T) {
	mk := func(aer, zahn string) *CardData {
		return &CardData{
			Insurance: &InsuranceData{InsurerID: "1"},
			Protected: &ProtectedData{
				SelektivAerztlich:     aer,
				SelektivZahnaerztlich: zahn,
			},
		}
	}
	// Both participate (1/1).
	f := FormMapping(mk("1", "1"), nil)
	a, _ := fieldByLabel(f, "Selektivvertrag (ärztlich)")
	if a.Value != "1" {
		t.Errorf("ärztlich value = %q", a.Value)
	}
	if !strings.Contains(a.Note, "Teilnahme") {
		t.Errorf("ärztlich note = %q", a.Note)
	}
	z, _ := fieldByLabel(f, "Selektivvertrag (zahnärztlich)")
	if z.Value != "1" {
		t.Errorf("zahnärztlich value = %q", z.Value)
	}
	// Neither (0/0).
	f = FormMapping(mk("0", "0"), nil)
	a, _ = fieldByLabel(f, "Selektivvertrag (ärztlich)")
	if !strings.Contains(a.Note, "keine Teilnahme") {
		t.Errorf("0 note = %q", a.Note)
	}
	// Absent on card.
	f = FormMapping(mk("", ""), nil)
	a, _ = fieldByLabel(f, "Selektivvertrag (ärztlich)")
	if a.Value != "" {
		t.Errorf("absent value = %q", a.Value)
	}
	if !strings.Contains(a.Note, "not present") {
		t.Errorf("absent note = %q", a.Note)
	}
}

func TestExplainSelektiv(t *testing.T) {
	cases := []struct{ in, wantSub string }{
		{"", "not present"},
		{"0", "keine Teilnahme"},
		{"1", "Teilnahme"},
		{"9", ""},
	}
	for _, c := range cases {
		got := explainSelektiv(c.in)
		if c.wantSub == "" {
			if got != "" {
				t.Errorf("explainSelektiv(%q) = %q, want empty", c.in, got)
			}
			continue
		}
		if !strings.Contains(got, c.wantSub) {
			t.Errorf("explainSelektiv(%q) = %q, want substring %q", c.in, got, c.wantSub)
		}
	}
}

func TestExplainInsuredType(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1", "1 = Mitglied"},
		{"3", "3 = Familienversicherter"},
		{"5", "5 = Rentner"},
		{"", ""}, {"2", ""},
	}
	for _, c := range cases {
		if got := explainInsuredType(c.in); got != c.want {
			t.Errorf("explainInsuredType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExplainWOP(t *testing.T) {
	if got := explainWOP("02"); got != "02 = Hamburg" {
		t.Errorf("got %q", got)
	}
	if got := explainWOP(""); got != "" {
		t.Errorf("empty: got %q", got)
	}
	if got := explainWOP("99"); got != "" {
		t.Errorf("unknown: got %q", got)
	}
	// Spot-check every documented code resolves to something non-empty.
	for _, code := range []string{"01", "02", "03", "17", "20", "38", "46", "51", "52", "71", "72", "73", "78", "83", "88", "93", "98"} {
		if got := explainWOP(code); got == "" {
			t.Errorf("WOP %s should resolve to a name", code)
		}
	}
}

func TestKTABHelpers(t *testing.T) {
	if ktabFromIKInfo(nil) != "00" {
		t.Error("ktabFromIKInfo(nil) should be 00")
	}
	if ktabFromIKInfo(&IKInfo{}) != "00" {
		t.Error("ktabFromIKInfo non-nil should still be 00")
	}
	if ktabSource(nil) != "derived" {
		t.Error("ktabSource should be derived")
	}
	if !strings.Contains(ktabNote(nil), "Primärabrechnung") {
		t.Error("ktabNote should mention Primärabrechnung")
	}
}

func TestCopayNote(t *testing.T) {
	cases := []struct {
		status, until, want string
	}{
		{"", "", "not on card"},
		{"0", "", "nicht zuzahlungsbefreit"},
		{"1", "", "end date missing"},
		{"1", "20251231", "zuzahlungsbefreit until 2025-12-31"},
		{"9", "", "unknown"},
	}
	for _, c := range cases {
		got := copayNote(c.status, c.until)
		if !strings.Contains(got, c.want) {
			t.Errorf("copayNote(%q,%q) = %q, want substring %q", c.status, c.until, got, c.want)
		}
	}
}
