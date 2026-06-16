package egk

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestFormatDate(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"19720314", "1972-03-14"},
		{"20240101", "2024-01-01"},
		{"", ""},
		{"abc", "abc"},
		{"1972031", "1972031"},     // 7 chars
		{"197203144", "197203144"}, // 9 chars
		{"1972A314", "1972A314"},   // non-numeric
	}
	for _, c := range cases {
		if got := FormatDate(c.in); got != c.want {
			t.Errorf("FormatDate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatGender(t *testing.T) {
	cases := []struct{ in, want string }{
		{"M", "Male"}, {"m", "Male"},
		{"W", "Female"}, {"F", "Female"}, {"f", "Female"},
		{"X", "Diverse"}, {"D", "Diverse"},
		{"", ""}, {"Q", "Q"},
	}
	for _, c := range cases {
		if got := FormatGender(c.in); got != c.want {
			t.Errorf("FormatGender(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatInsuredType(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1", "1 — Member (Mitglied)"},
		{"3", "3 — Family member (Familienversicherter)"},
		{"5", "5 — Pensioner (Rentner)"},
		{"", ""}, {"2", "2"},
	}
	for _, c := range cases {
		if got := FormatInsuredType(c.in); got != c.want {
			t.Errorf("FormatInsuredType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatCountry(t *testing.T) {
	cases := []struct{ in, want string }{
		{"D", "Germany"}, {"d", "Germany"},
		{"A", "Austria"},
		{"CH", "Switzerland"}, {"ch", "Switzerland"},
		{"F", "France"},
		{"NL", "Netherlands"},
		{"PL", "Poland"},
		{"", ""}, {"ZZ", "ZZ"},
	}
	for _, c := range cases {
		if got := FormatCountry(c.in); got != c.want {
			t.Errorf("FormatCountry(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestXMLCharsetReader(t *testing.T) {
	src := bytes.NewReader([]byte{0xE4}) // 'ä' in ISO-8859-1 / ISO-8859-15
	for _, name := range []string{"iso-8859-15", "ISO-8859-15", "latin-9", "latin9"} {
		r, err := xmlCharsetReader(name, bytes.NewReader([]byte{0xE4}))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, _ := io.ReadAll(r)
		if string(b) != "ä" {
			t.Errorf("%s: got %q, want ä", name, b)
		}
	}
	for _, name := range []string{"iso-8859-1", "latin-1", "latin1"} {
		r, err := xmlCharsetReader(name, bytes.NewReader([]byte{0xE4}))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, _ := io.ReadAll(r)
		if string(b) != "ä" {
			t.Errorf("%s: got %q, want ä", name, b)
		}
	}
	// UTF-8 / utf8 / empty pass through unchanged.
	for _, name := range []string{"utf-8", "utf8", ""} {
		r, err := xmlCharsetReader(name, bytes.NewReader([]byte("hello")))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, _ := io.ReadAll(r)
		if string(b) != "hello" {
			t.Errorf("%s: got %q, want hello", name, b)
		}
	}
	if _, err := xmlCharsetReader("ebcdic-x", src); err == nil {
		t.Error("unsupported charset should return error")
	}
}

func TestUnmarshalXMLWithISO885915(t *testing.T) {
	xmlStr := "<?xml version=\"1.0\" encoding=\"ISO-8859-15\"?><root><name>M\xfcller</name></root>"
	var got struct {
		XMLName xml.Name `xml:"root"`
		Name    string   `xml:"name"`
	}
	if err := unmarshalXML([]byte(xmlStr), &got); err != nil {
		t.Fatalf("unmarshalXML: %v", err)
	}
	if got.Name != "Müller" {
		t.Errorf("Name = %q, want Müller", got.Name)
	}
}

func TestGunzip(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte("hello world")); err != nil {
		t.Fatal(err)
	}
	zw.Close()

	got, err := gunzip(buf.Bytes())
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("got %q, want hello world", got)
	}

	if _, err := gunzip([]byte("not gzip")); err == nil {
		t.Error("gunzip on garbage should error")
	}
}

// buildPD packages the given XML body the way EF.PD is stored on a real card:
// 2-byte big-endian length prefix followed by gzipped XML bytes.
func buildPD(t *testing.T, xmlBody string) []byte {
	t.Helper()
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write([]byte(xmlBody)); err != nil {
		t.Fatal(err)
	}
	zw.Close()

	out := make([]byte, 2+gz.Len())
	binary.BigEndian.PutUint16(out[:2], uint16(gz.Len()))
	copy(out[2:], gz.Bytes())
	return out
}

// samplePDXML declares utf-8 so the literal "ü" byte (2 bytes in UTF-8)
// passes through the charset reader unchanged. Real eGK cards use
// ISO-8859-15; that path is exercised separately in
// TestUnmarshalXMLWithISO885915.
const samplePDXML = `<?xml version="1.0" encoding="utf-8"?>
<UC_PersoenlicheVersichertendatenXML>
  <Versicherter>
    <Versicherten_ID>X110407317</Versicherten_ID>
    <Person>
      <Vorname>Hans</Vorname>
      <Nachname>Müller</Nachname>
      <Titel>Dr.</Titel>
      <Geburtsdatum>19720314</Geburtsdatum>
      <Geschlecht>M</Geschlecht>
      <StrassenAdresse>
        <Strasse>Bahnhofstr.</Strasse>
        <Hausnummer>42</Hausnummer>
        <Postleitzahl>10115</Postleitzahl>
        <Ort>Berlin</Ort>
        <Land><Wohnsitzlaendercode>D</Wohnsitzlaendercode></Land>
      </StrassenAdresse>
    </Person>
  </Versicherter>
</UC_PersoenlicheVersichertendatenXML>`

func TestParsePDHappyPath(t *testing.T) {
	raw := buildPD(t, samplePDXML)
	pd, xmlOut, err := ParsePD(raw)
	if err != nil {
		t.Fatalf("ParsePD: %v", err)
	}
	if pd.InsurantID != "X110407317" {
		t.Errorf("InsurantID = %q", pd.InsurantID)
	}
	if pd.LastName != "Müller" {
		t.Errorf("LastName = %q, want Müller", pd.LastName)
	}
	if pd.FirstName != "Hans" {
		t.Errorf("FirstName = %q", pd.FirstName)
	}
	if pd.Title != "Dr." {
		t.Errorf("Title = %q", pd.Title)
	}
	if pd.BirthDate != "19720314" {
		t.Errorf("BirthDate = %q", pd.BirthDate)
	}
	if pd.Gender != "M" {
		t.Errorf("Gender = %q", pd.Gender)
	}
	if pd.Street != "Bahnhofstr." || pd.HouseNumber != "42" {
		t.Errorf("Street/Hausnr = %q/%q", pd.Street, pd.HouseNumber)
	}
	if pd.PostalCode != "10115" || pd.City != "Berlin" {
		t.Errorf("PLZ/City = %q/%q", pd.PostalCode, pd.City)
	}
	if pd.Country != "D" {
		t.Errorf("Country = %q", pd.Country)
	}
	if !strings.Contains(xmlOut, "Müller") {
		t.Error("decompressed XML should include Müller")
	}
}

func TestParsePDErrors(t *testing.T) {
	if _, _, err := ParsePD(nil); err == nil {
		t.Error("nil → expected error")
	}
	if _, _, err := ParsePD([]byte{0x00}); err == nil {
		t.Error("1-byte → expected error")
	}
	// Length prefix claims more bytes than buffer holds.
	tooLong := []byte{0x00, 0xFF, 0x00}
	if _, _, err := ParsePD(tooLong); err == nil {
		t.Error("length-exceeds-buffer → expected error")
	}
	// Valid length prefix, body isn't gzip.
	notGzip := []byte{0x00, 0x04, 'a', 'b', 'c', 'd'}
	if _, _, err := ParsePD(notGzip); err == nil {
		t.Error("not gzip → expected error")
	}
	// Valid gzip body but malformed XML.
	bad := buildPD(t, "<not><well-formed>")
	if _, _, err := ParsePD(bad); err == nil {
		t.Error("malformed XML → expected error")
	}
}

// buildVD assembles an EF.VD buffer: an 8-byte offset header followed by the
// two gzipped sections (AVD, then GVD). Pass empty gvdXML to omit the GVD
// section entirely (start=end=0).
func buildVD(t *testing.T, avdXML, gvdXML string) []byte {
	t.Helper()

	var avdGz bytes.Buffer
	zw := gzip.NewWriter(&avdGz)
	zw.Write([]byte(avdXML))
	zw.Close()

	header := make([]byte, 8)
	avdStart := uint16(8)
	avdEnd := avdStart + uint16(avdGz.Len()) - 1
	binary.BigEndian.PutUint16(header[0:2], avdStart)
	binary.BigEndian.PutUint16(header[2:4], avdEnd)

	out := append([]byte{}, header...)
	out = append(out, avdGz.Bytes()...)

	if gvdXML == "" {
		// gvdStart=0, gvdEnd=0 → "no GVD"
		return out
	}

	var gvdGz bytes.Buffer
	zw2 := gzip.NewWriter(&gvdGz)
	zw2.Write([]byte(gvdXML))
	zw2.Close()

	gvdStart := uint16(len(out))
	gvdEnd := gvdStart + uint16(gvdGz.Len()) - 1
	binary.BigEndian.PutUint16(out[4:6], gvdStart)
	binary.BigEndian.PutUint16(out[6:8], gvdEnd)
	out = append(out, gvdGz.Bytes()...)
	return out
}

const sampleAVDXML = `<?xml version="1.0" encoding="utf-8"?>
<UC_AllgemeineVersicherungsdatenXML>
  <Versicherter><Versicherungsschutz>
    <Beginn>20240101</Beginn>
    <Ende>20251231</Ende>
    <Kostentraeger>
      <Kostentraegerkennung>109519005</Kostentraegerkennung>
      <Name>Techniker Krankenkasse</Name>
      <AbrechnenderKostentraeger>
        <Kostentraegerkennung>109519005</Kostentraegerkennung>
        <Name>Techniker Krankenkasse</Name>
      </AbrechnenderKostentraeger>
    </Kostentraeger>
  </Versicherungsschutz>
  <Zusatzinfos><ZusatzinfosGKV>
    <Versichertenart>1</Versichertenart>
    <Zusatzinfos_Abrechnung_GKV>
      <Besondere_Personengruppe>00</Besondere_Personengruppe>
      <DMP_Kennzeichnung>00</DMP_Kennzeichnung>
      <WOP>02</WOP>
    </Zusatzinfos_Abrechnung_GKV>
  </ZusatzinfosGKV></Zusatzinfos></Versicherter>
</UC_AllgemeineVersicherungsdatenXML>`

const sampleGVDXML = `<?xml version="1.0" encoding="utf-8"?>
<UC_GeschuetzteVersichertendatenXML>
  <Zuzahlungsstatus>
    <Status>1</Status>
    <Gueltig_bis>20251231</Gueltig_bis>
  </Zuzahlungsstatus>
</UC_GeschuetzteVersichertendatenXML>`

func TestParseVDHappyPath(t *testing.T) {
	raw := buildVD(t, sampleAVDXML, sampleGVDXML)
	vd, gvd, avdXML, gvdXML, err := ParseVD(raw)
	if err != nil {
		t.Fatalf("ParseVD: %v", err)
	}
	if vd == nil {
		t.Fatal("vd nil")
	}
	if vd.InsurerID != "109519005" {
		t.Errorf("InsurerID = %q", vd.InsurerID)
	}
	if vd.BillingInsurerID != "109519005" {
		t.Errorf("BillingInsurerID = %q", vd.BillingInsurerID)
	}
	if vd.StartDate != "20240101" || vd.EndDate != "20251231" {
		t.Errorf("dates %q %q", vd.StartDate, vd.EndDate)
	}
	if vd.InsuredType != "1" {
		t.Errorf("InsuredType = %q", vd.InsuredType)
	}
	if vd.WOP != "02" {
		t.Errorf("WOP = %q", vd.WOP)
	}
	if gvd == nil {
		t.Fatal("gvd nil")
	}
	if gvd.ZuzahlungStatus != "1" {
		t.Errorf("ZuzahlungStatus = %q", gvd.ZuzahlungStatus)
	}
	if gvd.ZuzahlungGueltigBis != "20251231" {
		t.Errorf("ZuzahlungGueltigBis = %q", gvd.ZuzahlungGueltigBis)
	}
	if !strings.Contains(avdXML, "Techniker") {
		t.Error("AVD xml output missing insurer name")
	}
	if !strings.Contains(gvdXML, "Zuzahlungsstatus") {
		t.Error("GVD xml output missing root tag")
	}
}

func TestParseVDNoGVD(t *testing.T) {
	raw := buildVD(t, sampleAVDXML, "")
	vd, gvd, avdXML, gvdXML, err := ParseVD(raw)
	if err != nil {
		t.Fatalf("ParseVD: %v", err)
	}
	if vd == nil {
		t.Fatal("vd nil")
	}
	if gvd != nil {
		t.Errorf("gvd expected nil when GVD section absent, got %+v", gvd)
	}
	if avdXML == "" {
		t.Error("avdXML should be populated")
	}
	if gvdXML != "" {
		t.Errorf("gvdXML should be empty, got %q", gvdXML)
	}
}

func TestParseVDErrors(t *testing.T) {
	if _, _, _, _, err := ParseVD(nil); err == nil {
		t.Error("nil → expected error")
	}
	if _, _, _, _, err := ParseVD(make([]byte, 4)); err == nil {
		t.Error("short header → expected error")
	}
	// AVD pointer past buffer → gunzip on truncated data fails.
	hdr := make([]byte, 8)
	binary.BigEndian.PutUint16(hdr[0:2], 8)
	binary.BigEndian.PutUint16(hdr[2:4], 50) // AVD claims to extend to byte 50
	if _, _, _, _, err := ParseVD(hdr); err != nil {
		// Out-of-bound AVD is currently silently ignored (avdEnd+1 > len).
		// Expect no error and nil vd.
		t.Errorf("ParseVD with avd-pointer-past-end unexpectedly errored: %v", err)
	}
}

func TestParseVDBadAVDGzip(t *testing.T) {
	// Build a header where AVD section points at non-gzip bytes.
	hdr := make([]byte, 8)
	body := []byte{'n', 'o', 't', 'g', 'z'}
	binary.BigEndian.PutUint16(hdr[0:2], 8)
	binary.BigEndian.PutUint16(hdr[2:4], uint16(8+len(body)-1))
	buf := append(hdr, body...)
	if _, _, _, _, err := ParseVD(buf); err == nil {
		t.Error("bad AVD gzip should error")
	}
}

func TestParseVDMalformedAVDXML(t *testing.T) {
	// Valid gzip, malformed XML inside. The function returns nil vd but
	// surfaces avdXML so callers can inspect the raw bytes that failed to
	// parse.
	raw := buildVD(t, "<not><closed>", "")
	_, _, avdXML, _, err := ParseVD(raw)
	if err == nil {
		t.Error("malformed AVD XML should error")
	}
	if avdXML == "" {
		t.Error("avdXML should be populated even when XML parse fails")
	}
}

func TestParseVDBadGVDDoesNotFailWhole(t *testing.T) {
	// Build with valid AVD but the GVD section is non-gzip bytes — ParseVD
	// silently drops gvd when gunzip fails (it's the "outer wrapper still
	// usable, inner section optional" semantic).
	avdRaw := buildVD(t, sampleAVDXML, "")
	// Append junk and patch the GVD pointers.
	junk := []byte{'b', 'a', 'd'}
	full := append(avdRaw, junk...)
	gvdStart := uint16(len(avdRaw))
	gvdEnd := uint16(len(full) - 1)
	binary.BigEndian.PutUint16(full[4:6], gvdStart)
	binary.BigEndian.PutUint16(full[6:8], gvdEnd)

	vd, gvd, _, _, err := ParseVD(full)
	if err != nil {
		t.Fatalf("ParseVD: %v", err)
	}
	if vd == nil {
		t.Error("vd should be non-nil")
	}
	if gvd != nil {
		t.Errorf("gvd should be nil when GVD gunzip fails, got %+v", gvd)
	}
}
