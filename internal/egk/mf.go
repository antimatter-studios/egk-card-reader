package egk

import (
	"encoding/hex"
	"strings"
)

// MFData is read at the Master File level, before any application is selected.
// These fields identify the card itself (serial / OS version) independently of
// the eGK Healthcare Application contents.
type MFData struct {
	ICCSN string // 20-hex-char card serial number (from EF.GDO tag 0x5A)
	GDO   []byte // raw EF.GDO bytes (TLV)
}

// EF.GDO at MF holds the Integrated Circuit Card Serial Number as a BER-TLV:
// tag 0x5A, length 0x0A, value = 10 bytes. Per ISO 7816-4 / gemSpec_eGK_ObjSys.
const efGDO = 0x2F02 // FID at MF — collides with EF.VD inside DF.HCA, so order matters.

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
