package document

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"

	"github.com/christhomas/card-reader/pkg/egk"
)

// gdtEncoder is the Encoder registered as "gdt".
type gdtEncoder struct{}

func (gdtEncoder) Format() string    { return "gdt" }
func (gdtEncoder) Extension() string { return ".gdt" }
func (e gdtEncoder) Encode(d *egk.CardData, ik *egk.IKInfo) (*Document, error) {
	return captureBytes("gdt", ".gdt", func(w io.Writer) error {
		return encodeGDT6301(d, ik, w)
	})
}

// encodeGDT6301 writes a GDT 2.10 Satzart 6301 (Stammdaten übergeben) record
// to w as ISO-8859-15 with CR LF line terminators.
//
// Each line is "LLLFFFFvalue\r\n" where LLL is the *byte* length of the line
// after ISO-8859-15 encoding (including LLL itself and the CR LF), and FFFF
// is the 4-digit field code. The 8100 field at line 2 holds the byte length
// of the entire record. See docs/output-formats.md for the field mapping.
func encodeGDT6301(d *egk.CardData, ik *egk.IKInfo, w io.Writer) error {
	if d == nil {
		return fmt.Errorf("nil card data")
	}
	var (
		pd egk.PersonalData
		vd egk.InsuranceData
		gv egk.ProtectedData
	)
	if d.Personal != nil {
		pd = *d.Personal
	}
	if d.Insurance != nil {
		vd = *d.Insurance
	}
	if d.Protected != nil {
		gv = *d.Protected
	}

	var lines [][]byte
	add := func(field, value string) error {
		if value == "" {
			return nil
		}
		line, err := gdtLine(field, value)
		if err != nil {
			return err
		}
		lines = append(lines, line)
		return nil
	}

	// Header — 8100 (Satzlänge) is patched after we know the total byte count.
	if err := add("8000", "6301"); err != nil {
		return err
	}
	placeholder, err := gdtLine("8100", "00000")
	if err != nil {
		return err
	}
	lines = append(lines, placeholder)
	placeholderIdx := len(lines) - 1
	for _, kv := range []struct{ k, v string }{
		{"9218", "02.10"},
		{"0201", "EMPF"},        // Empfänger-ID — placeholder
		{"0203", "CRDR"},        // Sender-ID — placeholder
		{"0205", "card-reader"}, // Software-Bezeichnung
	} {
		if err := add(kv.k, kv.v); err != nil {
			return err
		}
	}

	// Patient
	addrLine := strings.TrimSpace(pd.Street + " " + pd.HouseNumber)
	for _, kv := range []struct{ k, v string }{
		{"3000", pd.InsurantID},
		{"3101", pd.LastName},
		{"3102", pd.FirstName},
		{"3103", gdtDate(pd.BirthDate)},
		{"3104", pd.Title},
		{"3105", pd.InsurantID},
		{"3106", vd.InsuredType},
		{"3110", gdtSex(pd.Gender)},
		{"3112", pd.PostalCode},
		{"3113", pd.City},
		{"3114", addrLine},
		{"3116", pd.Country},
	} {
		if err := add(kv.k, kv.v); err != nil {
			return err
		}
	}

	// Insurance
	for _, kv := range []struct{ k, v string }{
		{"4101", billingName(vd)},
		{"4104", billingIK(vd)},
		{"4108", gdtVKNR(ik)},
		{"4131", vd.WOP},
		{"4133", gdtDate(vd.StartDate)},
		{"4202", gdtDate(vd.EndDate)},
		{"4239", time.Now().Format("02012006")}, // Karte gelesen am — TTMMJJJJ
	} {
		if err := add(kv.k, kv.v); err != nil {
			return err
		}
	}
	if gv.ZuzahlungStatus == "1" {
		if err := add("4242", gdtDate(gv.ZuzahlungGueltigBis)); err != nil {
			return err
		}
	}

	// Patch 8100 with the now-known total byte length.
	total := 0
	for _, l := range lines {
		total += len(l)
	}
	patched, err := gdtLine("8100", fmt.Sprintf("%05d", total))
	if err != nil {
		return err
	}
	if len(patched) != len(lines[placeholderIdx]) {
		return fmt.Errorf("gdt: 8100 placeholder length drifted (%d vs %d)", len(patched), len(lines[placeholderIdx]))
	}
	lines[placeholderIdx] = patched

	for _, l := range lines {
		if _, err := w.Write(l); err != nil {
			return err
		}
	}
	return nil
}

// gdtLine builds one GDT line as ISO-8859-15 bytes. Line length is computed
// against the encoded bytes — encoding before measuring is essential because
// German umlauts are 1 byte in ISO-8859-15 but 2 bytes in UTF-8.
func gdtLine(field, value string) ([]byte, error) {
	enc := charmap.ISO8859_15.NewEncoder()
	encValue, err := enc.Bytes([]byte(value))
	if err != nil {
		return nil, fmt.Errorf("encode %s value: %w", field, err)
	}
	length := 3 + len(field) + len(encValue) + 2 // LLL + FFFF + value + CRLF
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%03d%s", length, field)
	buf.Write(encValue)
	buf.WriteString("\r\n")
	return buf.Bytes(), nil
}

// gdtDate converts YYYYMMDD → TTMMJJJJ (DDMMYYYY) for xDT date fields.
func gdtDate(s string) string {
	if len(s) != 8 {
		return ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s[6:8] + s[4:6] + s[0:4]
}

// gdtSex maps eGK gender (M/W/X/D) to xDT field 3110 (1/2/3/4).
func gdtSex(s string) string {
	switch strings.ToUpper(s) {
	case "M":
		return "1"
	case "W", "F":
		return "2"
	case "X":
		return "3"
	case "D":
		return "4"
	}
	return ""
}

func gdtVKNR(ik *egk.IKInfo) string {
	if ik == nil {
		return ""
	}
	return ik.VKNR
}
