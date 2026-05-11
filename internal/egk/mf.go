package egk

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// MFData is read at the Master File level, before any application is selected.
// These fields identify the card itself (serial / OS version) independently of
// the eGK Healthcare Application contents.
type MFData struct {
	ICCSN    string   // 20-hex-char card serial number (from EF.GDO tag 0x5A)
	GDO      []byte   // raw EF.GDO bytes (TLV)
	Version2 *Version // gemSpec_eGK_ObjSys EF.Version2 (nil if unread or absent)
}

// Version captures EF.Version2's BER-TLV children, indexed by tag number. The
// exact label per tag varies between gemSpec_eGK_ObjSys versions, so we keep
// neutral names (C0..C3) and let callers map them per their spec revision.
type Version struct {
	FID  uint16 // FID where Version2 was found (D080 standard, 2F11 legacy)
	Raw  []byte // raw EF.Version2 bytes
	TagC0 string
	TagC1 string
	TagC2 string
	TagC3 string
}

// EF.GDO at MF holds the Integrated Circuit Card Serial Number as a BER-TLV:
// tag 0x5A, length 0x0A, value = 10 bytes. Per ISO 7816-4 / gemSpec_eGK_ObjSys.
const efGDO = 0x2F02 // FID at MF — collides with EF.VD inside DF.HCA, so order matters.

// EF.Version2 candidate FIDs at MF, in priority order.
//   - 0xD080: gemSpec_eGK_ObjSys §3.4.7 canonical location.
//   - 0x2F11: legacy / pre-G2.x cards.
var efVersion2FIDs = []uint16{0xD080, 0x2F11}

// readMF reads the EF.GDO at MF and returns the ICCSN + raw bytes.
// Returns (nil, nil) if the card has no GDO or it can't be read — callers
// treat this as "diagnostic info unavailable" rather than a fatal error.
func readMF(card Card) (*MFData, error) {
	if err := selectEF(card, efGDO); err != nil {
		return nil, err
	}
	// EF.GDO is at most 18 bytes (TLV 5A 0A + 10 + a few wrapper bytes); 32 is plenty.
	raw, err := readBinary(card, 0, 0, 32)
	if err != nil {
		return nil, err
	}
	return &MFData{
		ICCSN: parseICCSN(raw),
		GDO:   raw,
	}, nil
}

// readVersion2 tries each candidate FID for EF.Version2 and returns the first
// hit. Returns nil, nil if no candidate selects (which is normal — gemSpec
// makes Version2 optional on G1 cards). Errors from the read itself are
// surfaced; SELECT failures are treated as "not present".
func readVersion2(card Card) (*Version, error) {
	for _, fid := range efVersion2FIDs {
		if err := selectEF(card, fid); err != nil {
			continue // SELECT failed → try next candidate
		}
		raw, err := readBinary(card, 0, 0, 64)
		if err != nil {
			return nil, err
		}
		v := parseVersion2(raw)
		v.FID = fid
		v.Raw = raw
		return v, nil
	}
	return nil, nil
}

// parseVersion2 decodes the EF.Version2 BER-TLV structure. Layout per
// gemSpec_eGK_ObjSys §3.4.7: outer constructed tag 0xEF, four primitive
// children 0xC0..0xC3 each holding 3 BCD bytes. Unknown layouts return a
// Version with empty fields but the raw bytes preserved.
func parseVersion2(b []byte) *Version {
	v := &Version{}
	// Skip the outer tag/length if present (0xEF <len>).
	i := 0
	if len(b) >= 2 && b[0] == 0xEF {
		// length form: short (1 byte) or long (81/82 + len). For Version2,
		// payload is <= 16 bytes so short form is universal — but be defensive.
		l := int(b[1])
		switch {
		case l < 0x80:
			i = 2
		case l == 0x81 && len(b) >= 3:
			i = 3
		case l == 0x82 && len(b) >= 4:
			i = 4
		}
	}
	for i+1 < len(b) {
		tag := b[i]
		l := int(b[i+1])
		if i+2+l > len(b) {
			break
		}
		val := b[i+2 : i+2+l]
		s := hexDotted(val)
		// Long mixed-ASCII fields (C2 Personalisation block) read nicer with
		// the manufacturer prefix surfaced.
		if a := asciiPart(val); a != "" && len(a) >= 4 {
			s = a + " (" + s + ")"
		}
		switch tag {
		case 0xC0:
			v.TagC0 = s
		case 0xC1:
			v.TagC1 = s
		case 0xC2:
			v.TagC2 = s
		case 0xC3:
			v.TagC3 = s
		}
		i += 2 + l
	}
	return v
}

// hexDotted formats a byte slice as "HH.HH.HH..." — each byte rendered as two
// hex digits joined by dots. For 3-byte BCD-style version fields this reads
// as "MAJOR.MINOR.PATCH"; for longer mixed-ASCII fields it surfaces the raw
// bytes for the caller to decode further.
func hexDotted(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	parts := make([]string, 0, len(b))
	for _, x := range b {
		parts = append(parts, fmt.Sprintf("%02X", x))
	}
	return strings.Join(parts, ".")
}

// asciiPart returns the leading ASCII-printable run of b (until the first
// non-printable byte), empty if none. Used to surface the manufacturer text
// embedded in EF.Version2 tag C2.
func asciiPart(b []byte) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c < 0x20 || c >= 0x7F {
			break
		}
		out = append(out, c)
	}
	return string(out)
}

// parseICCSN extracts the 20-hex ICCSN from EF.GDO content. Accepts both the
// canonical TLV form (5A 0A <10 bytes>) and the bare 10-byte form some test
// cards return.
func parseICCSN(b []byte) string {
	if len(b) >= 12 && b[0] == 0x5A {
		n := int(b[1])
		if n == 10 && len(b) >= 2+n {
			return strings.ToUpper(hex.EncodeToString(b[2 : 2+n]))
		}
	}
	if len(b) == 10 {
		return strings.ToUpper(hex.EncodeToString(b))
	}
	return ""
}
