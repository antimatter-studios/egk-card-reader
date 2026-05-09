package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/ebfe/scard"
)

// Known gematik / German healthcare AIDs.
type aidEntry struct {
	name string
	aid  []byte
}

var probes = []aidEntry{
	{"DF.HCA (eGK Health Card App)", h("D27600000102")},
	{"DF.ESIGN (HBA/SMC-B signature)", h("A000000167455349474E")},
	{"DF.HPA (HBA Heilberufsausweis)", h("D27600006601")},
	{"DF.AUTO (HBA/SMC-B auth)", h("D27600006602")},
	{"DF.QES (qualified signature)", h("D27600006603")},
	{"DF.SMA (SMC-B Anwendungs)", h("D2760001448000")},
	{"MF.EF.GDO (global ICCSN)", nil}, // sentinel, handled below
	{"MF.EF.Version2 (gematik)", nil},
	{"MF.EF.ATR", nil},
}

func h(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func main() {
	ctx, err := scard.EstablishContext()
	if err != nil {
		die("EstablishContext: %v", err)
	}
	defer ctx.Release()

	readers, err := ctx.ListReaders()
	if err != nil {
		die("ListReaders: %v", err)
	}
	if len(readers) == 0 {
		fmt.Println("No PC/SC readers found.")
		return
	}

	for i, r := range readers {
		fmt.Printf("\n==== Reader %d: %s ====\n", i, r)
		probeReader(ctx, r)
	}
}

func probeReader(ctx *scard.Context, reader string) {
	card, err := ctx.Connect(reader, scard.ShareShared, scard.ProtocolAny)
	if err != nil {
		fmt.Printf("  Connect failed: %v (no card present?)\n", err)
		return
	}
	defer card.Disconnect(scard.LeaveCard)

	st, err := card.Status()
	if err != nil {
		fmt.Printf("  Status failed: %v\n", err)
		return
	}
	fmt.Printf("  ATR  : %s\n", strings.ToUpper(hex.EncodeToString(st.Atr)))
	fmt.Printf("  Proto: T=%d\n", st.ActiveProtocol-1)
	fmt.Printf("  Guess: %s\n", guessFromATR(st.Atr))

	// 1. Reset to MF.
	sw, _ := transmit(card, h("00A4000C023F00"))
	fmt.Printf("  SELECT MF                                  -> SW=%04X\n", sw)

	// 2. Try EF.GDO (FID 2F02 at MF) — should hold ICCSN on gematik cards.
	sw, data := transmit(card, h("00A4020C022F02"))
	fmt.Printf("  SELECT MF/EF.GDO (2F02)                    -> SW=%04X\n", sw)
	if sw == 0x9000 {
		sw2, gdo := transmit(card, h("00B0000020"))
		if sw2 == 0x9000 || sw2&0xFF00 == 0x6C00 {
			if sw2&0xFF00 == 0x6C00 {
				apdu := append([]byte{0x00, 0xB0, 0x00, 0x00}, byte(sw2&0xFF))
				sw2, gdo = transmit(card, apdu)
			}
			fmt.Printf("    EF.GDO bytes: %s\n", strings.ToUpper(hex.EncodeToString(gdo)))
			if iccsn := parseICCSN(gdo); iccsn != "" {
				fmt.Printf("    ICCSN       : %s\n", iccsn)
			}
		}
		_ = data
	}

	// 3. Try Version2 (gematik standard EF) — different FID per card type.
	for _, fid := range []string{"2F11", "D001", "D002"} {
		apdu := h("00A4020C02" + fid)
		sw, _ := transmit(card, apdu)
		if sw == 0x9000 {
			fmt.Printf("  SELECT MF/EF (%s) found                   -> SW=%04X\n", fid, sw)
		}
	}

	// 4. Probe the application AIDs.
	fmt.Println("  --- application probe ---")
	for _, p := range probes {
		if p.aid == nil {
			continue
		}
		// reset to MF before each select
		_, _ = transmit(card, h("00A4000C023F00"))
		apdu := append([]byte{0x00, 0xA4, 0x04, 0x0C, byte(len(p.aid))}, p.aid...)
		sw, _ := transmit(card, apdu)
		mark := "—"
		switch {
		case sw == 0x9000:
			mark = "FOUND"
		case sw == 0x6A82:
			mark = "not present"
		case sw == 0x6982:
			mark = "present, auth required"
		default:
			mark = fmt.Sprintf("SW=%04X", sw)
		}
		fmt.Printf("    %-40s %s\n", p.name, mark)
	}
}

func transmit(card *scard.Card, apdu []byte) (uint16, []byte) {
	resp, err := card.Transmit(apdu)
	if err != nil || len(resp) < 2 {
		return 0, nil
	}
	sw := uint16(resp[len(resp)-2])<<8 | uint16(resp[len(resp)-1])
	return sw, resp[:len(resp)-2]
}

// parseICCSN expects either a raw 10-byte ICCSN or a TLV (5A 0A …).
func parseICCSN(b []byte) string {
	if len(b) >= 12 && b[0] == 0x5A {
		n := int(b[1])
		if len(b) >= 2+n {
			return strings.ToUpper(hex.EncodeToString(b[2 : 2+n]))
		}
	}
	if len(b) == 10 {
		return strings.ToUpper(hex.EncodeToString(b))
	}
	return ""
}

func guessFromATR(atr []byte) string {
	hexStr := strings.ToUpper(hex.EncodeToString(atr))
	switch {
	case strings.Contains(hexStr, "80707002"):
		return "likely eGK G2.x (gematik historical bytes)"
	case strings.HasPrefix(hexStr, "3BDD") || strings.HasPrefix(hexStr, "3BD3"):
		return "ISO 7816 card, gematik-style (eGK / HBA / SMC-B)"
	case strings.HasPrefix(hexStr, "3BFF"):
		return "ISO 7816 card (often SMC-B Atos / TCOS)"
	case strings.HasPrefix(hexStr, "3B"):
		return "ISO 7816 T=0/T=1 card"
	case strings.HasPrefix(hexStr, "3F"):
		return "inverse-convention card (rare in healthcare)"
	default:
		return "unknown"
	}
}

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}
