package main

import (
	"fmt"
	"strings"
)

// atrInfo decodes an ISO 7816-3 ATR. Output is human-readable; for full
// detail consult ISO 7816-3 §8 and gemSpec_COS Anhang B.
type atrInfo struct {
	raw          []byte
	convention   string // "direct" or "inverse"
	protocols    []byte // T values seen in TDi
	tA1          *byte
	tB1, tC1     *byte
	historical   []byte
	tckOK        bool
	tck, wantTCK byte
	extra        []string // free-form notes per interface byte
}

func decodeATR(b []byte) (*atrInfo, error) {
	if len(b) < 2 {
		return nil, fmt.Errorf("ATR too short (%d bytes)", len(b))
	}
	info := &atrInfo{raw: append([]byte(nil), b...)}
	switch b[0] {
	case 0x3B:
		info.convention = "direct"
	case 0x3F:
		info.convention = "inverse"
	default:
		return nil, fmt.Errorf("invalid TS=0x%02X (want 3B or 3F)", b[0])
	}
	t0 := b[1]
	k := int(t0 & 0x0F)
	y := t0 >> 4
	tckRequired := false
	i := 2

	for y != 0 {
		if y&0x01 != 0 { // TAi
			if i >= len(b) {
				return nil, fmt.Errorf("truncated ATR at TA")
			}
			v := b[i]
			i++
			if info.tA1 == nil {
				info.tA1 = &v
				fi, di := decodeFiDi(v)
				info.extra = append(info.extra, fmt.Sprintf("TA1=%02X → Fi=%d, Di=%d", v, fi, di))
			} else {
				info.extra = append(info.extra, fmt.Sprintf("TAi=%02X", v))
			}
		}
		if y&0x02 != 0 { // TBi
			if i >= len(b) {
				return nil, fmt.Errorf("truncated ATR at TB")
			}
			v := b[i]
			i++
			if info.tB1 == nil {
				info.tB1 = &v
			}
			info.extra = append(info.extra, fmt.Sprintf("TBi=%02X", v))
		}
		if y&0x04 != 0 { // TCi
			if i >= len(b) {
				return nil, fmt.Errorf("truncated ATR at TC")
			}
			v := b[i]
			i++
			if info.tC1 == nil {
				info.tC1 = &v
				info.extra = append(info.extra, fmt.Sprintf("TC1=%02X → extra guard time N=%d", v, v))
			} else {
				info.extra = append(info.extra, fmt.Sprintf("TCi=%02X", v))
			}
		}
		if y&0x08 != 0 { // TDi
			if i >= len(b) {
				return nil, fmt.Errorf("truncated ATR at TD")
			}
			td := b[i]
			i++
			t := td & 0x0F
			info.protocols = append(info.protocols, t)
			if t != 0 {
				tckRequired = true
			}
			info.extra = append(info.extra, fmt.Sprintf("TDi=%02X → T=%d", td, t))
			y = td >> 4
			continue
		}
		y = 0
	}

	if i+k > len(b) {
		return nil, fmt.Errorf("truncated historical bytes (want %d, have %d)", k, len(b)-i)
	}
	info.historical = append([]byte(nil), b[i:i+k]...)
	i += k

	if tckRequired {
		if i >= len(b) {
			return nil, fmt.Errorf("missing TCK")
		}
		info.tck = b[i]
		var x byte
		for _, by := range b[1 : i+1] {
			x ^= by
		}
		info.wantTCK = x ^ b[i] // 0 if OK
		info.tckOK = info.wantTCK == 0
	}
	return info, nil
}

func decodeFiDi(ta1 byte) (fi, di int) {
	fiTable := [16]int{372, 372, 558, 744, 1116, 1488, 1860, 0, 0, 512, 768, 1024, 1536, 2048, 0, 0}
	diTable := [16]int{0, 1, 2, 4, 8, 16, 32, 64, 12, 20, 0, 0, 0, 0, 0, 0}
	return fiTable[ta1>>4], diTable[ta1&0x0F]
}

func (a *atrInfo) markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "- **raw**: `%X`\n", a.raw)
	fmt.Fprintf(&b, "- convention: %s\n", a.convention)
	if len(a.protocols) > 0 {
		ts := make([]string, len(a.protocols))
		for i, t := range a.protocols {
			ts[i] = fmt.Sprintf("T=%d", t)
		}
		fmt.Fprintf(&b, "- protocols: %s\n", strings.Join(ts, ", "))
	} else {
		fmt.Fprintf(&b, "- protocols: T=0 (default)\n")
	}
	for _, line := range a.extra {
		fmt.Fprintf(&b, "- %s\n", line)
	}
	if len(a.historical) > 0 {
		fmt.Fprintf(&b, "- historical bytes (%d): `%X`", len(a.historical), a.historical)
		if printable := printableASCII(a.historical); strings.TrimSpace(printable) != "" && strings.IndexByte(printable, '.') != len(printable) {
			fmt.Fprintf(&b, " — %q", printable)
		}
		b.WriteString("\n")
	} else {
		fmt.Fprintf(&b, "- historical bytes: none\n")
	}
	if a.tck != 0 || len(a.protocols) > 0 {
		ok := "✓"
		if !a.tckOK {
			ok = "✗"
		}
		fmt.Fprintf(&b, "- TCK: `%02X` %s\n", a.tck, ok)
	}
	return b.String()
}
