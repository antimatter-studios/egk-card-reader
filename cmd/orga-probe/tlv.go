package main

import (
	"fmt"
	"strings"
)

// tlvRecord is a flat BER-TLV record. We don't recurse — just walk top-level.
type tlvRecord struct {
	Tag    uint32
	Length int
	Value  []byte
}

func parseTLV(buf []byte) []tlvRecord {
	var out []tlvRecord
	for i := 0; i < len(buf); {
		if buf[i] == 0x00 || buf[i] == 0xFF { // padding
			i++
			continue
		}
		tag := uint32(buf[i])
		i++
		if tag&0x1F == 0x1F {
			// multi-byte tag
			for i < len(buf) {
				tag = (tag << 8) | uint32(buf[i])
				more := buf[i]&0x80 != 0
				i++
				if !more {
					break
				}
			}
		}
		if i >= len(buf) {
			break
		}
		l := int(buf[i])
		i++
		if l == 0x81 {
			if i >= len(buf) {
				break
			}
			l = int(buf[i])
			i++
		} else if l == 0x82 {
			if i+1 >= len(buf) {
				break
			}
			l = int(buf[i])<<8 | int(buf[i+1])
			i += 2
		} else if l >= 0x80 {
			break // unsupported length encoding
		}
		if i+l > len(buf) {
			break
		}
		out = append(out, tlvRecord{Tag: tag, Length: l, Value: append([]byte(nil), buf[i:i+l]...)})
		i += l
	}
	return out
}

// efATRDecode walks the EF.ATR / EF.Version TLV records and annotates known
// vendor tags. Tags D0..DF are used by SICCT/CIO for issuer-specific info;
// gematik-issued cards typically populate D2 (chip type), D3 (COS), D4
// (Service Box), D6 (init data).
func efATRDecode(buf []byte) string {
	var b strings.Builder
	recs := parseTLV(buf)
	if len(recs) == 0 {
		fmt.Fprintf(&b, "- (no TLV records parsed; raw `%X`)\n", buf)
		return b.String()
	}
	for _, r := range recs {
		hint := vendorTagHint(r.Tag, r.Value)
		ascii := printableASCII(r.Value)
		fmt.Fprintf(&b, "- tag `%X` (%s) len=%d value=`%X`", r.Tag, hint, r.Length, r.Value)
		if strings.TrimSpace(strings.ReplaceAll(ascii, ".", "")) != "" {
			fmt.Fprintf(&b, " — %q", ascii)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func vendorTagHint(tag uint32, val []byte) string {
	switch tag {
	case 0xE0:
		return "ISO 7816-4 application template"
	case 0xD0:
		return "issuer data"
	case 0xD1:
		return "card profile"
	case 0xD2:
		return "chip / hardware"
	case 0xD3:
		return "COS / operating system"
	case 0xD4:
		return "service box / config"
	case 0xD5:
		return "patch level"
	case 0xD6:
		return "initialization data"
	case 0xD7:
		return "personalizer data"
	case 0xCF:
		return "padding / reserved"
	case 0x5F52:
		return "SICCT card capability info"
	case 0x4F:
		return "application identifier (AID)"
	case 0x50:
		return "application label"
	case 0x84:
		return "DF name (AID)"
	}
	return fmt.Sprintf("unknown 0x%X", tag)
}

func printableASCII(b []byte) string {
	out := make([]rune, len(b))
	for i, c := range b {
		if c >= 0x20 && c < 0x7f {
			out[i] = rune(c)
		} else {
			out[i] = '.'
		}
	}
	return string(out)
}
