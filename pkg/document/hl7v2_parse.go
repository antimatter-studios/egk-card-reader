package document

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/antimatter-studios/egk-card-reader/pkg/egk"
)

// ParseHL7ADT reads an HL7 v2.5 ADT^A04 message (as produced by encodeADTA04)
// from r and returns a populated *egk.CardData. It is the inverse of
// hl7v2ADTEncoder. Tolerates CRLF or bare LF segment terminators and a
// leading UTF-8 BOM. Reads the encoding characters from MSH-2 and falls back
// to the standard "^~\&" set.
//
// Mapping mirrors what encodeADTA04 emits — see hl7v2.go for the authoritative
// outbound side. RawPD/RawVD/XMLPD/XMLAVD/XMLGVD/HCAFCP are NOT populated as
// HL7 v2 carries no equivalent.
//
// If the input contains no recognisable segments, an error is returned.
// Otherwise a partial CardData is returned even when individual segments
// (PID, IN1, PV1) are missing.
func ParseHL7ADT(r io.Reader) (*egk.CardData, error) {
	if r == nil {
		return nil, fmt.Errorf("nil reader")
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read HL7: %w", err)
	}
	// Strip UTF-8 BOM if present.
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})

	// Normalise CRLF / bare CR to LF so a single split works for all senders.
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	raw = bytes.ReplaceAll(raw, []byte("\r"), []byte("\n"))

	lines := strings.Split(string(raw), "\n")

	// Default delimiters match the HL7 v2 standard set (and what our encoder
	// always uses). MSH-2 may override.
	fieldSep := byte('|')
	compSep := byte('^')
	repSep := byte('~')
	escChar := byte('\\')
	subSep := byte('&')

	pd := &egk.PersonalData{}
	vd := &egk.InsuranceData{}

	var (
		havePD       bool
		haveVD       bool
		seenAny      bool
		streetField  string // PID-11.1 — split into Street / HouseNumber on assembly.
	)

	for _, line := range lines {
		seg := strings.TrimSpace(line)
		if seg == "" {
			continue
		}
		// MSH is special: the field separator is the 4th byte and the
		// encoding characters are MSH-2 (the 5th..8th bytes).
		if strings.HasPrefix(seg, "MSH") && len(seg) >= 8 {
			fieldSep = seg[3]
			compSep = seg[4]
			repSep = seg[5]
			escChar = seg[6]
			subSep = seg[7]
			seenAny = true
			continue
		}
		// Need at least "XYZ|" to be a real segment.
		if len(seg) < 4 || seg[3] != fieldSep {
			continue
		}
		segName := seg[:3]
		// Split on the field separator. For MSH the encoding chars live in
		// MSH-2 which complicates indexing; we already handled MSH above so
		// every other segment indexes naturally: fields[0] is Set ID etc.
		fields := strings.Split(seg[4:], string(fieldSep))

		switch segName {
		case "PID":
			seenAny = true
			havePD = true
			// PID-3 = fields[2]: take the first repetition.
			if pid3 := fieldAt(fields, 2); pid3 != "" {
				rep := firstRep(pid3, repSep)
				comps := splitComps(rep, compSep)
				if id := compAt(comps, 0); id != "" {
					pd.InsurantID = hl7Unescape(id, escChar, fieldSep, compSep, repSep, subSep)
				}
				// PID-3.4 = Assigning Authority "GKV&IK&IKNR".
				if assigner := compAt(comps, 3); assigner != "" {
					sub := strings.Split(assigner, string(subSep))
					if len(sub) >= 2 {
						ik := hl7Unescape(sub[1], escChar, fieldSep, compSep, repSep, subSep)
						if ik != "" {
							vd.InsurerID = ik
							haveVD = true
						}
					}
				}
			}
			// PID-5 = fields[4]: Name (LastName^FirstName^^^Title).
			if pid5 := fieldAt(fields, 4); pid5 != "" {
				rep := firstRep(pid5, repSep)
				comps := splitComps(rep, compSep)
				pd.LastName = hl7Unescape(compAt(comps, 0), escChar, fieldSep, compSep, repSep, subSep)
				pd.FirstName = hl7Unescape(compAt(comps, 1), escChar, fieldSep, compSep, repSep, subSep)
				pd.Title = hl7Unescape(compAt(comps, 4), escChar, fieldSep, compSep, repSep, subSep)
			}
			pd.BirthDate = condDateValue(fieldAt(fields, 6))
			pd.Gender = parseHL7Sex(fieldAt(fields, 7))
			// PID-11 = fields[10]: Address.
			if pid11 := fieldAt(fields, 10); pid11 != "" {
				rep := firstRep(pid11, repSep)
				comps := splitComps(rep, compSep)
				streetField = hl7Unescape(compAt(comps, 0), escChar, fieldSep, compSep, repSep, subSep)
				pd.AddressSuffix = hl7Unescape(compAt(comps, 1), escChar, fieldSep, compSep, repSep, subSep)
				pd.City = hl7Unescape(compAt(comps, 2), escChar, fieldSep, compSep, repSep, subSep)
				pd.PostalCode = hl7Unescape(compAt(comps, 4), escChar, fieldSep, compSep, repSep, subSep)
				pd.Country = hl7Unescape(compAt(comps, 5), escChar, fieldSep, compSep, repSep, subSep)
			}

		case "IN1":
			seenAny = true
			haveVD = true
			// IN1-3 = fields[2]: Insurance Company ID (IK^^^DE-IK^XX).
			if in13 := fieldAt(fields, 2); in13 != "" {
				rep := firstRep(in13, repSep)
				comps := splitComps(rep, compSep)
				if id := hl7Unescape(compAt(comps, 0), escChar, fieldSep, compSep, repSep, subSep); id != "" {
					vd.InsurerID = id
					vd.BillingInsurerID = id
				}
			}
			// IN1-4 = fields[3]: Insurance Company Name.
			if in14 := fieldAt(fields, 3); in14 != "" {
				rep := firstRep(in14, repSep)
				comps := splitComps(rep, compSep)
				if nm := hl7Unescape(compAt(comps, 0), escChar, fieldSep, compSep, repSep, subSep); nm != "" {
					vd.InsurerName = nm
					vd.BillingInsurerName = nm
				}
			}
			vd.StartDate = condDateValue(fieldAt(fields, 11))
			vd.EndDate = condDateValue(fieldAt(fields, 12))
			if it := fieldAt(fields, 14); it != "" {
				vd.InsuredType = hl7Unescape(it, escChar, fieldSep, compSep, repSep, subSep)
			}
			// IN1-8 (VKNR), IN1-16/17 (subscriber name / relationship): ignored.

		case "EVN", "PV1":
			seenAny = true
			// Envelope-only; nothing to extract.

		default:
			// Unknown segments — skip silently.
		}
	}

	if !seenAny {
		return nil, fmt.Errorf("no HL7 segments found")
	}

	// Split the assembled street + house-number back out using the same
	// last-space heuristic as gdt_parse.go so multi-word streets round-trip.
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
	return cd, nil
}

// ParseHL7ADTFile opens path and delegates to ParseHL7ADT.
func ParseHL7ADTFile(path string) (*egk.CardData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return ParseHL7ADT(f)
}

// fieldAt returns fields[i] or "" if i is out of range.
func fieldAt(fields []string, i int) string {
	if i < 0 || i >= len(fields) {
		return ""
	}
	return fields[i]
}

// compAt returns comps[i] or "" if out of range.
func compAt(comps []string, i int) string {
	if i < 0 || i >= len(comps) {
		return ""
	}
	return comps[i]
}

// firstRep returns the first repetition of a field value.
func firstRep(s string, repSep byte) string {
	if idx := strings.IndexByte(s, repSep); idx >= 0 {
		return s[:idx]
	}
	return s
}

// splitComps splits a field on the component separator.
func splitComps(s string, compSep byte) []string {
	return strings.Split(s, string(compSep))
}

// hl7Unescape reverses the formal escape sequences emitted by hl7Escape. The
// only sequences our encoder produces are \F\ \S\ \R\ \T\ \E\ for the five
// delimiters; we handle those plus a literal \\ pass-through. Escape char is
// dynamic (per MSH-2) but the sequence letters are fixed by the standard.
func hl7Unescape(s string, escChar, fieldSep, compSep, repSep, subSep byte) string {
	if s == "" || strings.IndexByte(s, escChar) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != escChar {
			b.WriteByte(s[i])
			i++
			continue
		}
		// Look for the closing escape char.
		end := -1
		for j := i + 1; j < len(s); j++ {
			if s[j] == escChar {
				end = j
				break
			}
		}
		if end < 0 {
			// Malformed — pass remainder through verbatim.
			b.WriteString(s[i:])
			break
		}
		seq := s[i+1 : end]
		switch seq {
		case "F":
			b.WriteByte(fieldSep)
		case "S":
			b.WriteByte(compSep)
		case "R":
			b.WriteByte(repSep)
		case "T":
			b.WriteByte(subSep)
		case "E":
			b.WriteByte(escChar)
		default:
			// Unknown sequence (e.g. \X20\ hex, \H\ highlight) — pass through
			// verbatim so callers can decide what to do.
			b.WriteByte(escChar)
			b.WriteString(seq)
			b.WriteByte(escChar)
		}
		i = end + 1
	}
	return b.String()
}

// parseHL7Sex inverts hl7Sex: M→M, F→W, U→X, A→D.
func parseHL7Sex(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "M":
		return "M"
	case "F":
		return "W"
	case "U":
		return "X"
	case "A":
		return "D"
	}
	return ""
}

// condDateValue returns s if it is an 8-digit numeric date, else "".
// Mirrors condDate on the encoder side so empty / malformed values round-trip
// to "".
func condDateValue(s string) string {
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
