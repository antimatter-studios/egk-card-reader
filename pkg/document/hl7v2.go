package document

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/antimatter-studios/egk-card-reader/pkg/egk"
)

// hl7v2ADTEncoder is the Encoder registered as "hl7adt".
type hl7v2ADTEncoder struct{}

func (hl7v2ADTEncoder) Format() string    { return "hl7adt" }
func (hl7v2ADTEncoder) Extension() string { return ".hl7" }
func (e hl7v2ADTEncoder) Encode(d *egk.CardData, ik *egk.IKInfo) (*Document, error) {
	return captureBytes("hl7adt", ".hl7", func(w io.Writer) error {
		return encodeADTA04(d, ik, w)
	})
}

// encodeADTA04 writes an HL7 v2.5 ADT^A04 (Register a Patient) message to w.
//
// Segments emitted: MSH, EVN, PID, PV1, IN1. Sender / receiver fields are
// placeholders that should be overridden in deployment — see
// docs/output-formats.md.
//
// Segment terminator is CR LF (\r\n). The strict HL7 v2 spec mandates CR
// only, but every mainstream parser (HAPI, Mirth, Cerner, EPIC) accepts
// CR LF — and CR-only renders as one overwritten line in a terminal, which
// makes the output look empty when piped to stdout. If a strict-CR-only
// receiver complains, strip the LFs with `tr -d '\n'` before transmission
// (or wire up a future `--hl7-adt=strict` variant). Field separator is "|"
// with encoding chars "^~\&". MSH-18 declares UNICODE UTF-8 so German
// umlauts travel literally.
func encodeADTA04(d *egk.CardData, ik *egk.IKInfo, w io.Writer) error {
	if d == nil {
		return fmt.Errorf("nil card data")
	}
	var (
		pd egk.PersonalData
		vd egk.InsuranceData
	)
	if d.Personal != nil {
		pd = *d.Personal
	}
	if d.Insurance != nil {
		vd = *d.Insurance
	}

	now := time.Now().Format("20060102150405")
	msgID := "CR" + now

	msh := "MSH|^~\\&|" + strings.Join([]string{
		"CARD-READER",
		"PRACTICE",
		"PVS",
		"PRACTICE",
		now,
		"",
		"ADT^A04^ADT_A01",
		msgID,
		"P",
		"2.5",
		"",
		"",
		"",
		"",
		"",
		"",
		"UNICODE UTF-8",
	}, "|")

	evn := segment("EVN", "A04", now)

	pidIDList := ""
	if pd.InsurantID != "" {
		assigner := "GKV"
		if iknr := billingIK(vd); iknr != "" {
			assigner = "GKV&" + hl7Escape(iknr) + "&IKNR"
		}
		pidIDList = hl7Escape(pd.InsurantID) + "^^^" + assigner + "^MR"
	}

	pid := segment("PID",
		"1",                    // 1: Set ID
		"",                     // 2: deprecated
		pidIDList,              // 3: Patient Identifier List
		"",                     // 4: deprecated
		hl7Name(&pd),           // 5: Name
		"",                     // 6: Mother's maiden name
		condDate(pd.BirthDate), // 7: DOB (YYYYMMDD)
		hl7Sex(pd.Gender),      // 8: Sex
		"",                     // 9: Patient alias
		"",                     // 10: Race
		hl7Address(&pd),        // 11: Address
	)

	pv1 := segment("PV1",
		"1", // 1: Set ID
		"O", // 2: Patient Class — O = Outpatient
	)

	in1 := segment("IN1",
		"1",                                        // 1: Set ID
		"GKV",                                      // 2: Insurance Plan ID
		hl7Escape(billingIK(vd))+"^^^"+"DE-IK^XX", // 3: Insurance Company ID
		hl7Escape(billingName(vd)),                 // 4: Insurance Company Name
		"",                                         // 5: Insurance Company Address
		"",                                         // 6: Contact person
		"",                                         // 7: Phone
		hl7VKNR(ik),                                // 8: Group Number — VKNR
		"",                                         // 9: Group Name
		"",                                         // 10: Insured's Group Emp ID
		"",                                         // 11: Insured's Group Emp Name
		condDate(vd.StartDate),                     // 12: Plan Effective Date
		condDate(vd.EndDate),                       // 13: Plan Expiration Date
		"",                                         // 14: Authorization
		hl7Escape(vd.InsuredType),                  // 15: Plan Type — repurposed for Versichertenart
		hl7Name(&pd),                               // 16: Name of Insured
		"SEL",                                      // 17: Relationship to Patient (SEL = Self)
	)

	segs := []string{msh, evn, pid, pv1, in1}
	_, err := io.WriteString(w, strings.Join(segs, "\r\n")+"\r\n")
	return err
}

func segment(name string, fields ...string) string {
	return name + "|" + strings.Join(fields, "|")
}

// hl7Escape escapes the five HL7 v2 delimiter characters using the standard
// formal sequences. Order matters: backslash MUST be replaced first so we
// don't double-escape the escape character we introduce.
func hl7Escape(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, `\`, `\E\`)
	s = strings.ReplaceAll(s, "|", `\F\`)
	s = strings.ReplaceAll(s, "^", `\S\`)
	s = strings.ReplaceAll(s, "~", `\R\`)
	s = strings.ReplaceAll(s, "&", `\T\`)
	return s
}

func hl7Name(pd *egk.PersonalData) string {
	parts := []string{
		hl7Escape(pd.LastName),
		hl7Escape(pd.FirstName),
		"",
		"",
		hl7Escape(pd.Title),
	}
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "^")
}

func hl7Address(pd *egk.PersonalData) string {
	street := strings.TrimSpace(pd.Street + " " + pd.HouseNumber)
	parts := []string{
		hl7Escape(street),
		hl7Escape(pd.AddressSuffix),
		hl7Escape(pd.City),
		"",
		hl7Escape(pd.PostalCode),
		hl7Escape(pd.Country),
	}
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "^")
}

func hl7Sex(s string) string {
	switch strings.ToUpper(s) {
	case "M":
		return "M"
	case "W", "F":
		return "F"
	case "X":
		return "U"
	case "D":
		return "A"
	}
	return ""
}

func hl7VKNR(ik *egk.IKInfo) string {
	if ik == nil {
		return ""
	}
	return hl7Escape(ik.VKNR)
}

// condDate returns YYYYMMDD or "" if the input isn't an 8-digit numeric date.
func condDate(s string) string {
	if len(s) != 8 {
		return ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s
}
