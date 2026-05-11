package document

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/christhomas/card-reader/internal/egk"
)

func TestFhirPatientID(t *testing.T) {
	if got := fhirPatientID(""); got != "patient-unknown" {
		t.Errorf("empty = %q", got)
	}
	if got := fhirPatientID("X1"); got != "patient-X1" {
		t.Errorf("set = %q", got)
	}
}

func TestFhirCoverageID(t *testing.T) {
	if got := fhirCoverageID("X1", "I1"); got != "coverage-X1-I1" {
		t.Errorf("both = %q", got)
	}
	if got := fhirCoverageID("X1", ""); got != "coverage-X1" {
		t.Errorf("kvnr-only = %q", got)
	}
	if got := fhirCoverageID("", ""); got != "coverage-unknown" {
		t.Errorf("neither = %q", got)
	}
	if got := fhirCoverageID("", "I1"); got != "coverage-unknown" {
		t.Errorf("iknr-only = %q", got)
	}
}

func TestFhirGender(t *testing.T) {
	cases := []struct{ in, want string }{
		{"M", "male"}, {"m", "male"},
		{"W", "female"}, {"F", "female"},
		{"X", "unknown"}, {"D", "other"},
		{"", ""}, {"Q", ""},
	}
	for _, c := range cases {
		if got := fhirGender(c.in); got != c.want {
			t.Errorf("fhirGender(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFhirAddress(t *testing.T) {
	// Empty → nil.
	if got := fhirAddress(&egk.PersonalData{}); got != nil {
		t.Errorf("empty should be nil, got %+v", got)
	}
	// Minimal.
	pd := &egk.PersonalData{
		Street:      "Hauptstr.",
		HouseNumber: "5",
		City:        "Berlin",
		PostalCode:  "10115",
		Country:     "D",
	}
	got := fhirAddress(pd)
	if got["city"] != "Berlin" {
		t.Errorf("city = %v", got["city"])
	}
	if got["postalCode"] != "10115" {
		t.Errorf("plz = %v", got["postalCode"])
	}
	lines := got["line"].([]any)
	if lines[0] != "Hauptstr. 5" {
		t.Errorf("line = %v", lines[0])
	}
	// With suffix.
	pd.AddressSuffix = "Hinterhaus"
	got = fhirAddress(pd)
	lines = got["line"].([]any)
	if !strings.Contains(lines[0].(string), "Hinterhaus") {
		t.Errorf("suffix missing: %v", lines[0])
	}
	// Only PLZ + city (no street).
	got = fhirAddress(&egk.PersonalData{PostalCode: "10115", City: "Berlin"})
	if got == nil {
		t.Error("PLZ-only should not be nil")
	}
	if _, hasLine := got["line"]; hasLine {
		t.Error("address with no street should not have line")
	}
}

func TestFhirCodingExtension(t *testing.T) {
	ext := fhirCodingExtension("http://url", "http://sys", "X")
	if ext["url"] != "http://url" {
		t.Errorf("url = %v", ext["url"])
	}
	vc := ext["valueCoding"].(map[string]any)
	if vc["system"] != "http://sys" || vc["code"] != "X" {
		t.Errorf("valueCoding = %v", vc)
	}
}

func TestParseFHIRGender(t *testing.T) {
	cases := []struct{ in, want string }{
		{"male", "M"}, {"Male", "M"},
		{"female", "W"},
		{"other", "D"},
		{"unknown", "X"},
		{"", ""}, {"weird", ""},
	}
	for _, c := range cases {
		if got := parseFHIRGender(c.in); got != c.want {
			t.Errorf("parseFHIRGender(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStripDashes(t *testing.T) {
	if got := stripDashes("2024-01-15"); got != "20240115" {
		t.Errorf("got %q", got)
	}
	if got := stripDashes(""); got != "" {
		t.Errorf("empty = %q", got)
	}
	if got := stripDashes("not a date"); got != "not a date" {
		t.Errorf("non-date = %q", got)
	}
	if got := stripDashes("2024/01/15"); got != "2024/01/15" {
		t.Errorf("bad separator = %q", got)
	}
}

func TestExtensionCode(t *testing.T) {
	// valueCoding present.
	ext := fhirExtension{ValueCoding: &fhirCoding{Code: "X"}, ValueString: "ignored"}
	if got := extensionCode(ext); got != "X" {
		t.Errorf("coding = %q", got)
	}
	// valueCoding present but empty code → falls back to valueString.
	ext = fhirExtension{ValueCoding: &fhirCoding{}, ValueString: "fallback"}
	if got := extensionCode(ext); got != "fallback" {
		t.Errorf("fallback = %q", got)
	}
	// No coding at all.
	ext = fhirExtension{ValueString: "plain"}
	if got := extensionCode(ext); got != "plain" {
		t.Errorf("plain = %q", got)
	}
	// Completely empty.
	if got := extensionCode(fhirExtension{}); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestParseFHIRFile(t *testing.T) {
	src := &egk.CardData{
		Personal:  &egk.PersonalData{LastName: "Müller", InsurantID: "X1"},
		Insurance: &egk.InsuranceData{InsurerID: "1", InsurerName: "TK"},
	}
	doc, err := fhirEncoder{}.Encode(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := writeTempFile(t, doc.Bytes, "in.fhir.json")
	got, err := ParseFHIRFile(path)
	if err != nil {
		t.Fatalf("ParseFHIRFile: %v", err)
	}
	if got.Personal == nil || got.Personal.LastName != "Müller" {
		t.Errorf("got %+v", got.Personal)
	}
}

func TestParseFHIRFileMissing(t *testing.T) {
	if _, err := ParseFHIRFile("/does/not/exist.fhir.json"); err == nil {
		t.Error("expected error")
	}
}

func TestParseFHIRNilReader(t *testing.T) {
	if _, err := ParseFHIR(nil); err == nil {
		t.Error("expected nil reader error")
	}
}

func TestParseFHIRBadJSON(t *testing.T) {
	if _, err := ParseFHIR(bytes.NewReader([]byte("not json"))); err == nil {
		t.Error("expected JSON parse error")
	}
}

func TestParseFHIRUnknownResource(t *testing.T) {
	// Unknown resourceType should be skipped silently.
	body := `{"resourceType":"Bundle","type":"collection","entry":[
{"resource":{"resourceType":"Observation","id":"x"}}
]}`
	cd, err := ParseFHIR(bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("ParseFHIR: %v", err)
	}
	if cd.Personal != nil || cd.Insurance != nil {
		t.Errorf("unknown resource leaked: %+v", cd)
	}
}

func TestParseFHIRCoverageExtensions(t *testing.T) {
	body := `{
  "resourceType":"Bundle","type":"collection",
  "entry":[{"resource":{
    "resourceType":"Coverage","id":"c",
    "payor":[{"identifier":{"system":"http://fhir.de/sid/arge-ik/iknr","value":"123"},"display":"X"}],
    "extension":[
      {"url":"http://fhir.de/StructureDefinition/gkv/wop","valueCoding":{"code":"02"}},
      {"url":"http://fhir.de/StructureDefinition/gkv/besondere-personengruppe","valueCoding":{"code":"04"}},
      {"url":"http://fhir.de/StructureDefinition/gkv/dmp-kennzeichen","valueCoding":{"code":"01"}},
      {"url":"http://fhir.de/StructureDefinition/gkv/kostentraeger-gruppe","valueCoding":{"code":"06"}},
      {"url":"http://other.example/random","valueCoding":{"code":"ignored"}},
      {"url":"http://broken","valueString":""}
    ]
  }}]
}`
	cd, err := ParseFHIR(bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	if cd.Insurance == nil {
		t.Fatal("Insurance nil")
	}
	if cd.Insurance.WOP != "02" {
		t.Errorf("WOP = %q", cd.Insurance.WOP)
	}
	if cd.Insurance.BesondereGruppe != "04" {
		t.Errorf("BesondereGruppe = %q", cd.Insurance.BesondereGruppe)
	}
	if cd.Insurance.DMP != "01" {
		t.Errorf("DMP = %q", cd.Insurance.DMP)
	}
}

func TestParseFHIRPatientWithoutIdentifier(t *testing.T) {
	// Patient without KVNR identifier — pd.InsurantID stays empty but the
	// rest still parses.
	body := `{"resourceType":"Bundle","type":"collection","entry":[
{"resource":{"resourceType":"Patient","name":[{"family":"X"}]}}
]}`
	cd, err := ParseFHIR(bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	if cd.Personal == nil || cd.Personal.LastName != "X" {
		t.Errorf("got %+v", cd.Personal)
	}
	if cd.Personal.InsurantID != "" {
		t.Errorf("InsurantID should be empty, got %q", cd.Personal.InsurantID)
	}
}

func TestParseFHIRPatientWithFamilyExtensions(t *testing.T) {
	// Name with _family.extension carrying namenszusatz + own-prefix.
	body := `{"resourceType":"Bundle","type":"collection","entry":[
{"resource":{"resourceType":"Patient","name":[{
  "family":"Bismarck",
  "_family":{"extension":[
    {"url":"http://fhir.de/StructureDefinition/humanname-namenszusatz","valueString":"Fürst"},
    {"url":"http://hl7.org/fhir/StructureDefinition/humanname-own-prefix","valueString":"von"}
  ]}
}]}}
]}`
	cd, err := ParseFHIR(bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	if cd.Personal.NamePrefix != "Fürst" {
		t.Errorf("NamePrefix = %q", cd.Personal.NamePrefix)
	}
	if cd.Personal.Vorsatzwort != "von" {
		t.Errorf("Vorsatzwort = %q", cd.Personal.Vorsatzwort)
	}
}

func TestRenderFHIRSmoke(t *testing.T) {
	d := &egk.CardData{
		Personal: &egk.PersonalData{
			InsurantID: "X110407317",
			LastName:   "Müller",
			FirstName:  "Hans",
			BirthDate:  "19720314",
			Street:     "Hauptstr.",
			City:       "Berlin",
			PostalCode: "10115",
		},
		Insurance: &egk.InsuranceData{
			InsurerID:       "109519005",
			InsurerName:     "TK",
			StartDate:       "20240101",
			BesondereGruppe: "00", // suppressed
			DMP:             "00", // suppressed
			WOP:             "02",
		},
	}
	out := RenderFHIR(d, &egk.IKInfo{VKNR: "12345", KostentraegerGruppe: "06"})
	for _, want := range []string{
		"Patient.identifier",
		"X110407317",
		"Müller",
		"109519005",
		"12345",   // VKNR
		"06",      // KTG
		"Bundle",  // caption
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	// BesondereGruppe / DMP "00" should appear as the em-dash (suppressed).
	// We don't try to parse ANSI here — just check the row labels are present.
	if !strings.Contains(out, "besondere-personengruppe") {
		t.Error("BesondereGruppe row should still be present")
	}
}

func TestRenderFHIRNilData(t *testing.T) {
	out := RenderFHIR(nil, nil)
	if !strings.Contains(out, "Patient.identifier") {
		t.Error("table should render with nil data")
	}
}

func TestAddrUseAddrType(t *testing.T) {
	// Empty PD → empty strings.
	pd := egk.PersonalData{}
	if got := addrUse(pd); got != "" {
		t.Errorf("addrUse empty = %q", got)
	}
	if got := addrType(pd); got != "" {
		t.Errorf("addrType empty = %q", got)
	}
	// Any one of Street/City/PostalCode → home/both.
	pd = egk.PersonalData{City: "Berlin"}
	if got := addrUse(pd); got != "home" {
		t.Errorf("addrUse = %q", got)
	}
	if got := addrType(pd); got != "both" {
		t.Errorf("addrType = %q", got)
	}
}

func TestPatientRef(t *testing.T) {
	if got := patientRef(""); got != "Patient/patient-unknown" {
		t.Errorf("empty = %q", got)
	}
	if got := patientRef("X1"); got != "Patient/patient-X1" {
		t.Errorf("set = %q", got)
	}
}

func TestSystemOrDash(t *testing.T) {
	if got := systemOrDash("", "sys"); got != "" {
		t.Errorf("empty value = %q", got)
	}
	if got := systemOrDash("v", "sys"); got != "sys" {
		t.Errorf("value = %q", got)
	}
}

func TestFhirWrap(t *testing.T) {
	// Short string — single line.
	if got := fhirWrap("hello world", 40); got != "hello world" {
		t.Errorf("short = %q", got)
	}
	// Wrap on word boundary.
	got := fhirWrap("one two three four five six seven", 10)
	if !strings.Contains(got, "\n") {
		t.Errorf("expected wrap newline: %q", got)
	}
	// Empty input → empty output.
	if got := fhirWrap("", 40); got != "" {
		t.Errorf("empty = %q", got)
	}
	// Whitespace-only input → empty after Fields.
	if got := fhirWrap("   ", 40); got != "   " {
		t.Errorf("whitespace = %q", got)
	}
}

// Sanity check: encoded FHIR Bundle has valid JSON shape.
func TestEncodedFHIRIsValidJSON(t *testing.T) {
	d := &egk.CardData{
		Personal:  &egk.PersonalData{LastName: "X", InsurantID: "Y"},
		Insurance: &egk.InsuranceData{InsurerID: "1"},
	}
	doc, err := fhirEncoder{}.Encode(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	var anyOut map[string]any
	if err := json.Unmarshal(doc.Bytes, &anyOut); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if anyOut["resourceType"] != "Bundle" {
		t.Errorf("resourceType = %v", anyOut["resourceType"])
	}
}

func TestFormMappingJSONEncode(t *testing.T) {
	enc := formMappingJSON{}
	if enc.Format() != "json" {
		t.Error("Format")
	}
	if enc.Extension() != ".json" {
		t.Error("Extension")
	}
	d := &egk.CardData{
		Personal:  &egk.PersonalData{InsurantID: "X1"},
		Insurance: &egk.InsuranceData{InsurerID: "1"},
	}
	doc, err := enc.Encode(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]string
	if err := json.Unmarshal(doc.Bytes, &rows); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(rows) != 23 {
		t.Errorf("expected 23 rows, got %d", len(rows))
	}
	for _, r := range rows {
		for _, k := range []string{"label", "value", "source", "note"} {
			if _, ok := r[k]; !ok {
				t.Errorf("row missing key %q: %+v", k, r)
			}
		}
	}
}
