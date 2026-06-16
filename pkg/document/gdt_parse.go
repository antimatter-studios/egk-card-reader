package document

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/text/encoding/charmap"

	"github.com/christhomas/card-reader/pkg/egk"
)

// ParseGDT reads a GDT 2.10 Satzart 6301 record (ISO-8859-15, CRLF-terminated
// "LLLFFFFvalue" lines) from r and returns a populated *egk.CardData. Header
// fields (8000, 8100, 9218, 0201, 0203, 0205) and field 4239 ("Karte gelesen
// am") are tolerated but not stored. Unknown field codes are skipped.
//
// ParseGDT is the inverse of encodeGDT6301; it does not populate RawPD/RawVD
// or any XML fields, since those aren't carried in a GDT record.
func ParseGDT(r io.Reader) (*egk.CardData, error) {
	if r == nil {
		return nil, fmt.Errorf("nil reader")
	}
	// Decode ISO-8859-15 → UTF-8 on the way in.
	dec := charmap.ISO8859_15.NewDecoder().Reader(r)
	br := bufio.NewReader(dec)

	pd := &egk.PersonalData{}
	vd := &egk.InsuranceData{}
	gv := &egk.ProtectedData{}

	var (
		havePD       bool
		haveVD       bool
		haveGV       bool
		streetField  string // 3114 raw value — split into Street/HouseNumber on assembly
	)

	for {
		line, err := br.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read line: %w", err)
		}
		// Tolerate both CRLF and bare LF; trim either.
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			if err == io.EOF {
				break
			}
			continue
		}
		if len(trimmed) < 7 {
			// Too short to contain LLL + FFFF; skip.
			if err == io.EOF {
				break
			}
			continue
		}
		field := trimmed[3:7]
		value := trimmed[7:]

		switch field {
		// Header / metadata — silently ignore.
		case "8000", "8100", "9218", "0201", "0203", "0205":
			// no-op
		case "4239":
			// "Karte gelesen am" — has nowhere to live in CardData.

		// Personal data
		case "3000":
			pd.InsurantID = value
			havePD = true
		case "3101":
			pd.LastName = value
			havePD = true
		case "3102":
			pd.FirstName = value
			havePD = true
		case "3103":
			pd.BirthDate = parseGDTDate(value)
			havePD = true
		case "3104":
			pd.Title = value
			havePD = true
		case "3105":
			// Duplicate InsurantID; keep first non-empty so 3000 wins.
			if pd.InsurantID == "" {
				pd.InsurantID = value
			}
			havePD = true
		case "3106":
			vd.InsuredType = value
			haveVD = true
		case "3110":
			pd.Gender = parseGDTSex(value)
			havePD = true
		case "3112":
			pd.PostalCode = value
			havePD = true
		case "3113":
			pd.City = value
			havePD = true
		case "3114":
			streetField = value
			havePD = true
		case "3116":
			pd.Country = value
			havePD = true

		// Insurance
		case "4101":
			vd.InsurerName = value
			vd.BillingInsurerName = value
			haveVD = true
		case "4104":
			vd.InsurerID = value
			vd.BillingInsurerID = value
			haveVD = true
		case "4108":
			// VKNR — comes from IKInfo on encode; no slot in CardData. Skip.
		case "4131":
			vd.WOP = value
			haveVD = true
		case "4133":
			vd.StartDate = parseGDTDate(value)
			haveVD = true
		case "4202":
			vd.EndDate = parseGDTDate(value)
			haveVD = true
		case "4242":
			gv.ZuzahlungGueltigBis = parseGDTDate(value)
			gv.ZuzahlungStatus = "1"
			haveGV = true

		default:
			// Unknown field — skip silently.
		}

		if err == io.EOF {
			break
		}
	}

	// Split the assembled street + house-number back out. The encoder joins
	// them with a single space; we split on the last space so multi-word
	// street names round-trip correctly ("Bahnhofstr." vs "Unter den Linden").
	if streetField != "" {
		if idx := strings.LastIndex(streetField, " "); idx >= 0 {
			pd.Street = streetField[:idx]
			pd.HouseNumber = streetField[idx+1:]
		} else {
			pd.Street = streetField
		}
	}

	cd := &egk.CardData{}
	if havePD {
		cd.Personal = pd
	}
	if haveVD {
		cd.Insurance = vd
	}
	if haveGV {
		cd.Protected = gv
	}
	return cd, nil
}

// ParseGDTFile opens path and delegates to ParseGDT.
func ParseGDTFile(path string) (*egk.CardData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return ParseGDT(f)
}

// parseGDTDate converts TTMMJJJJ (DDMMYYYY) → YYYYMMDD. Returns "" on
// malformed input so callers don't pollute CardData with garbage.
func parseGDTDate(s string) string {
	if len(s) != 8 {
		return ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s[4:8] + s[2:4] + s[0:2]
}

// parseGDTSex maps xDT field 3110 (1/2/3/4) back to eGK gender (M/W/X/D).
func parseGDTSex(s string) string {
	switch s {
	case "1":
		return "M"
	case "2":
		return "W"
	case "3":
		return "X"
	case "4":
		return "D"
	}
	return ""
}
