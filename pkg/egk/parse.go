package egk

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

// xmlCharsetReader resolves the charset names eGK XML files declare. The
// payloads are ISO-8859-15 in practice; some older cards use ISO-8859-1.
func xmlCharsetReader(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(charset) {
	case "iso-8859-15", "latin-9", "latin9":
		return charmap.ISO8859_15.NewDecoder().Reader(input), nil
	case "iso-8859-1", "latin-1", "latin1":
		return charmap.ISO8859_1.NewDecoder().Reader(input), nil
	case "utf-8", "utf8", "":
		return input, nil
	}
	return nil, fmt.Errorf("unsupported XML charset: %s", charset)
}

func unmarshalXML(data []byte, v any) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = xmlCharsetReader
	return dec.Decode(v)
}

// PersonalData mirrors UC_PersoenlicheVersichertendatenXML (gemSpec_eGK_Fach v5.2).
type PersonalData struct {
	XMLName          xml.Name `xml:"UC_PersoenlicheVersichertendatenXML"`
	InsurantID       string   `xml:"Versicherter>Versicherten_ID"`
	FirstName        string   `xml:"Versicherter>Person>Vorname"`
	LastName         string   `xml:"Versicherter>Person>Nachname"`
	Title            string   `xml:"Versicherter>Person>Titel"`
	NamePrefix       string   `xml:"Versicherter>Person>Namenszusatz"`
	Vorsatzwort      string   `xml:"Versicherter>Person>Vorsatzwort"`
	BirthDate        string   `xml:"Versicherter>Person>Geburtsdatum"`
	Gender           string   `xml:"Versicherter>Person>Geschlecht"`
	Street           string   `xml:"Versicherter>Person>StrassenAdresse>Strasse"`
	HouseNumber      string   `xml:"Versicherter>Person>StrassenAdresse>Hausnummer"`
	PostalCode       string   `xml:"Versicherter>Person>StrassenAdresse>Postleitzahl"`
	City             string   `xml:"Versicherter>Person>StrassenAdresse>Ort"`
	Country          string   `xml:"Versicherter>Person>StrassenAdresse>Land>Wohnsitzlaendercode"`
	AddressSuffix    string   `xml:"Versicherter>Person>StrassenAdresse>Anschriftenzusatz"`
	PostfachStrasse  string   `xml:"Versicherter>Person>PostfachAdresse>Postfach"`
	PostfachPLZ      string   `xml:"Versicherter>Person>PostfachAdresse>Postleitzahl"`
	PostfachOrt      string   `xml:"Versicherter>Person>PostfachAdresse>Ort"`
}

// InsuranceData mirrors UC_AllgemeineVersicherungsdatenXML.
//
// Note: Kostentraeger.Kostentraegerkennung is the original/issuing insurer IK.
// AbrechnenderKostentraeger.Kostentraegerkennung is the IK that actually
// settles billing — this is what goes into the IKNR field on most German
// practice-management forms. They differ when a regional TK office issues the
// card but the headquarters bills.
type InsuranceData struct {
	XMLName               xml.Name `xml:"UC_AllgemeineVersicherungsdatenXML"`
	InsurerID             string   `xml:"Versicherter>Versicherungsschutz>Kostentraeger>Kostentraegerkennung"`
	InsurerName           string   `xml:"Versicherter>Versicherungsschutz>Kostentraeger>Name"`
	InsurerCountry        string   `xml:"Versicherter>Versicherungsschutz>Kostentraeger>Kostentraegerlaendercode"`
	BillingInsurerID      string   `xml:"Versicherter>Versicherungsschutz>Kostentraeger>AbrechnenderKostentraeger>Kostentraegerkennung"`
	BillingInsurerName    string   `xml:"Versicherter>Versicherungsschutz>Kostentraeger>AbrechnenderKostentraeger>Name"`
	BillingInsurerCountry string   `xml:"Versicherter>Versicherungsschutz>Kostentraeger>AbrechnenderKostentraeger>Kostentraegerlaendercode"`
	StartDate             string   `xml:"Versicherter>Versicherungsschutz>Beginn"`
	EndDate               string   `xml:"Versicherter>Versicherungsschutz>Ende"`
	InsuredType           string   `xml:"Versicherter>Zusatzinfos>ZusatzinfosGKV>Versichertenart"`
	BesondereGruppe       string   `xml:"Versicherter>Zusatzinfos>ZusatzinfosGKV>Zusatzinfos_Abrechnung_GKV>Besondere_Personengruppe"`
	DMP                   string   `xml:"Versicherter>Zusatzinfos>ZusatzinfosGKV>Zusatzinfos_Abrechnung_GKV>DMP_Kennzeichnung"`
	WOP                   string   `xml:"Versicherter>Zusatzinfos>ZusatzinfosGKV>Zusatzinfos_Abrechnung_GKV>WOP"`
	RuhenderLeistungsanspruch string `xml:"Versicherter>Zusatzinfos>ZusatzinfosGKV>Zusatzinfos_Abrechnung_GKV>Ruhender_Leistungsanspruch"`
	Rechtskreis           string   `xml:"Versicherter>Zusatzinfos>ZusatzinfosGKV>Zusatzinfos_Abrechnung_GKV>Rechtskreis"`
}

// ProtectedData mirrors UC_GeschuetzteVersichertendatenXML.
type ProtectedData struct {
	XMLName              xml.Name `xml:"UC_GeschuetzteVersichertendatenXML"`
	ZuzahlungStatus      string   `xml:"Zuzahlungsstatus>Status"`
	ZuzahlungGueltigBis  string   `xml:"Zuzahlungsstatus>Gueltig_bis"`
	SelektivAerztlich    string   `xml:"Selektivvertraege>Aerztlich"`
	SelektivZahnaerztlich string  `xml:"Selektivvertraege>Zahnaerztlich"`
}

// FormatDate converts YYYYMMDD to YYYY-MM-DD; returns input unchanged if not 8 digits.
func FormatDate(s string) string {
	if len(s) != 8 {
		return s
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return s
		}
	}
	return s[0:4] + "-" + s[4:6] + "-" + s[6:8]
}

// FormatGender maps M/W/X/D to human-readable English.
func FormatGender(s string) string {
	switch strings.ToUpper(s) {
	case "M":
		return "Male"
	case "W", "F":
		return "Female"
	case "X", "D":
		return "Diverse"
	}
	return s
}

// FormatInsuredType maps the Versichertenart code (gemSpec_eGK_Fach Tab.27).
func FormatInsuredType(s string) string {
	switch s {
	case "1":
		return "1 — Member (Mitglied)"
	case "3":
		return "3 — Family member (Familienversicherter)"
	case "5":
		return "5 — Pensioner (Rentner)"
	}
	return s
}

// FormatCountry maps the most common Wohnsitzlaendercode values.
func FormatCountry(s string) string {
	switch strings.ToUpper(s) {
	case "D":
		return "Germany"
	case "A":
		return "Austria"
	case "CH":
		return "Switzerland"
	case "F":
		return "France"
	case "NL":
		return "Netherlands"
	case "PL":
		return "Poland"
	}
	return s
}

// ParsePD decodes the EF.PD raw bytes into PersonalData. Also returns the
// decompressed XML so callers can inspect / dump raw fields.
func ParsePD(raw []byte) (*PersonalData, string, error) {
	if len(raw) < 2 {
		return nil, "", fmt.Errorf("PD too short")
	}
	n := binary.BigEndian.Uint16(raw[:2])
	if int(n)+2 > len(raw) {
		return nil, "", fmt.Errorf("PD length %d exceeds buffer %d", n, len(raw))
	}
	xmlBytes, err := gunzip(raw[2 : 2+int(n)])
	if err != nil {
		return nil, "", fmt.Errorf("gunzip PD: %w", err)
	}
	var pd PersonalData
	if err := unmarshalXML(xmlBytes, &pd); err != nil {
		return nil, "", fmt.Errorf("parse PD XML: %w", err)
	}
	return &pd, string(xmlBytes), nil
}

// ParseVD decodes the EF.VD raw bytes into InsuranceData (and optionally
// ProtectedData if a GVD section is present). The file starts with four
// big-endian offset pointers: VD-start, VD-end, GVD-start, GVD-end.
func ParseVD(raw []byte) (*InsuranceData, *ProtectedData, string, string, error) {
	if len(raw) < 8 {
		return nil, nil, "", "", fmt.Errorf("VD header too short")
	}
	avdStart := binary.BigEndian.Uint16(raw[0:2])
	avdEnd := binary.BigEndian.Uint16(raw[2:4])
	gvdStart := binary.BigEndian.Uint16(raw[4:6])
	gvdEnd := binary.BigEndian.Uint16(raw[6:8])

	var vd *InsuranceData
	var avdXML string
	if avdEnd > avdStart && int(avdEnd)+1 <= len(raw) {
		xmlBytes, err := gunzip(raw[avdStart : avdEnd+1])
		if err != nil {
			return nil, nil, "", "", fmt.Errorf("gunzip AVD: %w", err)
		}
		avdXML = string(xmlBytes)
		vd = &InsuranceData{}
		if err := unmarshalXML(xmlBytes, vd); err != nil {
			return nil, nil, avdXML, "", fmt.Errorf("parse AVD XML: %w", err)
		}
	}

	var gvd *ProtectedData
	var gvdXML string
	if gvdEnd > gvdStart && gvdStart > 0 && int(gvdEnd)+1 <= len(raw) {
		xmlBytes, err := gunzip(raw[gvdStart : gvdEnd+1])
		if err == nil {
			gvdXML = string(xmlBytes)
			gvd = &ProtectedData{}
			if err := unmarshalXML(xmlBytes, gvd); err != nil {
				gvd = nil
			}
		}
	}
	return vd, gvd, avdXML, gvdXML, nil
}

func gunzip(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}
