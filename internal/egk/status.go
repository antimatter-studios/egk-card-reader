package egk

import (
	"encoding/hex"
	"strings"
)

// StatusVD captures the contents of EF.StatusVD inside DF.HCA. The file
// tracks the freshness of EF.VD: when the insurer last refreshed the
// Versicherungsdaten and what its current state is.
//
// Layout observed on G2.1 cards (FID D00C, 25 bytes):
//
//	byte 0     : '0' version prefix (ASCII)
//	bytes 1-14 : update timestamp YYYYMMDDhhmmss (ASCII, 14 chars)
//	byte 15    : 0x00 padding
//	bytes 16+  : status indicators (binary, layout per gemSpec_eGK_ObjSys)
type StatusVD struct {
	FID       uint16 // FID where the EF was read (D00C on G2.1)
	Raw       []byte // raw EF bytes
	Timestamp string // parsed ISO-8601 update time, empty if unparseable
	StatusHex string // hex dump of the trailing status block (bytes 16+)
}

// readStatusVD selects EF.StatusVD and reads up to 32 bytes. Returns nil if
// the file isn't present (best-effort; not all cards expose it).
func readStatusVD(card Card) (*StatusVD, error) {
	if err := selectEF(card, fidStatusVD); err != nil {
		return nil, err
	}
	raw, err := readBinary(card, 0, 0, 32)
	if err != nil {
		return nil, err
	}
	return parseStatusVD(raw, fidStatusVD), nil
}

func parseStatusVD(raw []byte, fid uint16) *StatusVD {
	s := &StatusVD{FID: fid, Raw: raw}
	if len(raw) >= 15 {
		// Take bytes 1..14 as 14-char ASCII timestamp YYYYMMDDhhmmss.
		ts := string(raw[1:15])
		if isAllDigits(ts) {
			// 2023-04-21T17:16:32
			s.Timestamp = ts[0:4] + "-" + ts[4:6] + "-" + ts[6:8] + "T" +
				ts[8:10] + ":" + ts[10:12] + ":" + ts[12:14]
		}
	}
	if len(raw) > 16 {
		s.StatusHex = strings.ToUpper(hex.EncodeToString(raw[16:]))
	}
	return s
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
