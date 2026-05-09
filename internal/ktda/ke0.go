// Package ktda parses Kostenträgerdatei files (UN/EDIFACT KE0 format,
// Anhang 3 zur Anlage 1 to the GKV-Spitzenverband data-exchange agreement).
//
// The lookup goal is: given an IKNR from an eGK, return the insurer name,
// the VKNR (Vertragskassennummer), the Kassenart (KE0 file prefix), and the
// chain of related IKs (card-IK → billing-IK / data-acceptance-points).
package ktda

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

// Entry is the de-duplicated data extracted for one Institutionskennzeichen.
type Entry struct {
	IK        string  // 9-digit Institutionskennzeichen
	Name      string  // composed Name-1..Name-4
	ShortName string  // Kurzbezeichnung from IDK
	VKNR      string  // 5-digit Vertragskassennummer (often empty in KE0)
	Kassenart string  // 2-letter prefix from filename (AO/EK/BK/IK/BN/LK)
	ValidFrom string  // earliest YYYYMMDD seen
	ValidTo   string  // latest YYYYMMDD seen, "" if open
	Links     []Link  // VKG references from this entry
}

// Link mirrors a VKG segment: this IK delegates billing/paper handling to
// another IK with the given Verknüpfungsart (01..09 etc.).
type Link struct {
	Art       string // Art der Verknüpfung (01 = card→Kostenträger, 02/03 = Datenannahme, 09 = Papier)
	IK        string // partner IK
	LEGruppe  string // Leistungserbringergruppe
	StelleIK  string // IK der Abrechnungsstelle
}

// Parse reads a KE0 stream and emits one Entry per UNH...UNT message. The
// kassenart parameter records which KE0 file the entries came from; pass the
// 2-letter prefix from the filename (AO/EK/BK/IK/BN/LK).
//
// KE0 files are EDIFACT-style: each segment starts with a 3-letter tag, fields
// are separated by '+', sub-fields by ':', and segments end with "'".
// Encoding is ISO-8859-1 (Latin-1).
func Parse(r io.Reader, kassenart string) ([]Entry, error) {
	dec := charmap.ISO8859_1.NewDecoder().Reader(r)
	br := bufio.NewReader(dec)

	var out []Entry
	var cur *Entry

	// Read until apostrophe, allow embedded newlines/CR.
	var seg strings.Builder
	flush := func() error {
		s := strings.TrimSpace(seg.String())
		seg.Reset()
		if s == "" {
			return nil
		}
		return handleSegment(s, kassenart, &cur, &out)
	}

	for {
		ch, err := br.ReadByte()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch ch {
		case '\'':
			if err := flush(); err != nil {
				return nil, err
			}
		case '\r', '\n':
			// Line breaks are visual only; ignore.
		default:
			seg.WriteByte(ch)
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out, nil
}

func handleSegment(seg, kassenart string, cur **Entry, out *[]Entry) error {
	if len(seg) < 3 {
		return nil
	}
	tag := seg[:3]
	fields := splitEDIFACT(seg)
	// fields[0] is the tag itself.

	switch tag {
	case "UNH":
		// Start of a new message. Flush any pending entry just in case.
		if *cur != nil {
			*out = append(*out, **cur)
		}
		*cur = &Entry{Kassenart: kassenart}

	case "UNT":
		if *cur != nil {
			*out = append(*out, **cur)
			*cur = nil
		}

	case "IDK":
		if *cur == nil {
			*cur = &Entry{Kassenart: kassenart}
		}
		// IDK + IK + Art + Kurzbezeichnung [+ VKNR]
		if len(fields) > 1 {
			(*cur).IK = fields[1]
		}
		if len(fields) > 3 {
			(*cur).ShortName = fields[3]
		}
		if len(fields) > 4 {
			(*cur).VKNR = strings.TrimSpace(fields[4])
		}

	case "VDT":
		// VDT + ValidFrom (YYYYMMDD) + ValidTo (YYYYMMDD)
		if *cur == nil {
			return nil
		}
		if len(fields) > 1 && (*cur).ValidFrom == "" {
			(*cur).ValidFrom = fields[1]
		}
		if len(fields) > 2 {
			(*cur).ValidTo = fields[2]
		}

	case "NAM":
		if *cur == nil {
			return nil
		}
		// NAM + LfdNr + Name1 [+ Name2 + Name3 + Name4]
		var parts []string
		for i := 2; i < len(fields); i++ {
			if v := strings.TrimSpace(fields[i]); v != "" {
				parts = append(parts, v)
			}
		}
		if name := strings.Join(parts, " "); name != "" {
			if (*cur).Name == "" {
				(*cur).Name = name
			}
		}

	case "VKG":
		if *cur == nil {
			return nil
		}
		// VKG + Art + IK + LEGruppe + IKAbrechnungsstelle + ...
		l := Link{}
		if len(fields) > 1 {
			l.Art = fields[1]
		}
		if len(fields) > 2 {
			l.IK = fields[2]
		}
		if len(fields) > 3 {
			l.LEGruppe = fields[3]
		}
		if len(fields) > 4 {
			l.StelleIK = fields[4]
		}
		(*cur).Links = append((*cur).Links, l)
	}
	return nil
}

// splitEDIFACT splits on '+' but respects the '?' release character which
// escapes the next character (so "?+" is a literal '+' inside a field).
func splitEDIFACT(s string) []string {
	var parts []string
	var cur strings.Builder
	escape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape {
			cur.WriteByte(c)
			escape = false
			continue
		}
		switch c {
		case '?':
			escape = true
		case '+':
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	parts = append(parts, cur.String())
	return parts
}

// ParseError is returned when the file isn't a recognizable KE0.
type ParseError struct{ Reason string }

func (e *ParseError) Error() string { return fmt.Sprintf("not a KE0 file: %s", e.Reason) }
