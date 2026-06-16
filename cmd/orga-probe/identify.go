package main

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/antimatter-studios/egk-card-reader/pkg/reader/orga"
)

// knownEF is one EF we probe at the MF level. Each card type uses a specific
// subset; we report which were readable.
type knownEF struct {
	FID  uint16
	Name string
	Note string
}

var mfEFs = []knownEF{
	{0x2F02, "EF.GDO", "global card data object — ICCSN at tag 5A"},
	{0x2F01, "EF.ATR", "SICCT card-info TLV — chip / OS identification"},
	{0x2F00, "EF.DIR", "application directory"},
	{0xD080, "EF.Version2", "gemSpec G2 card version block"},
	{0x2F11, "EF.Version", "legacy version block"},
}

// knownAID is one application we try to SELECT.
type knownAID struct {
	AID  []byte
	Name string
	Note string
}

var probedAIDs = []knownAID{
	{[]byte{0xD2, 0x76, 0x00, 0x00, 0x01, 0x02}, "DF.HCA", "eGK healthcare app (patient master + insurance + protected EFs)"},
	{[]byte{0xA0, 0x00, 0x00, 0x01, 0x67, 0x45, 0x53, 0x49, 0x47, 0x4E}, "DF.ESIGN", "ISO/IEC 7816-15 electronic-signature app (HBA / SMC-B / nPA)"},
	{[]byte{0xD2, 0x76, 0x00, 0x00, 0x66, 0x01}, "DF.HPA", "HBA Heilberufsausweis profile"},
	{[]byte{0xD2, 0x76, 0x00, 0x00, 0x66, 0x02}, "DF.AUTO", "HBA / SMC-B authentication app"},
	{[]byte{0xD2, 0x76, 0x00, 0x00, 0x66, 0x03}, "DF.QES", "HBA qualified electronic signature"},
	{[]byte{0xD2, 0x76, 0x00, 0x00, 0x01, 0x44, 0x80, 0x00}, "DF.SMA", "SMC-B application DF"},
	{[]byte{0xD2, 0x76, 0x00, 0x00, 0x14, 0x80, 0x02}, "DF.SAK", "SMC-K Konnektor app (legacy)"},
	{[]byte{0xE8, 0x28, 0xBD, 0x08, 0x0F, 0xA0, 0x00, 0x00, 0x01, 0x67, 0x45, 0x53, 0x49, 0x47, 0x4E}, "DF.CIA.ESIGN", "Cryptographic Information Application for ESIGN"},
	{[]byte{0xD2, 0x76, 0x00, 0x00, 0x01, 0x44, 0x84, 0x80, 0x00}, "DF.NFD", "eGK notfalldaten / emergency-data app (protected)"},
	{[]byte{0xD2, 0x76, 0x00, 0x00, 0x01, 0x44, 0x83, 0x00, 0x00}, "DF.DPE", "eGK Datenmanagement persönliche Erklärungen (protected)"},
}

// IdentifyOptions controls what the identify command emits.
type IdentifyOptions struct {
	Slot   int
	Out    io.Writer
	Redact bool // hash ICCSN to a stable opaque ID instead of printing raw
}

// identify runs the full identity probe on a slot and writes structured
// markdown to opts.Out. Returns the inferred card class as a short string
// (eGK / HBA / SMC-B / TCOS-test / unknown).
func identify(t *orga.Terminal, opts IdentifyOptions) (string, error) {
	w := opts.Out
	now := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(w, "# Card identity — slot %d — %s\n\n", opts.Slot, now)

	// --- Slot status / power-up
	st, err := t.SlotStatus(opts.Slot)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(w, "## Slot status\n\n- status byte: `0x%02X` (%s)\n\n", st, decodeSlotStatus(st))

	// --- ATR
	atr, err := t.ActivateSlot(opts.Slot)
	if err != nil {
		return "", fmt.Errorf("activate: %w", err)
	}
	// ATR returned by REQUEST ICC has trailing SW already stripped by the
	// library helper, but some terminals append warning bytes after the
	// ATR proper. Trim trailing 62xx / 90xx warnings.
	atr = trimATRWarnings(atr)
	fmt.Fprintf(w, "## ATR\n\n")
	if info, err := decodeATR(atr); err == nil {
		fmt.Fprint(w, info.markdown())
	} else {
		fmt.Fprintf(w, "- raw: `%X`\n- decode error: %v\n", atr, err)
	}
	fmt.Fprintln(w)

	slot := t.Slot(opts.Slot)

	// --- MF-level EFs
	fmt.Fprintf(w, "## MF-level EFs\n\n")
	fmt.Fprintf(w, "| FID  | Name        | SW   | Bytes | Notes |\n")
	fmt.Fprintf(w, "|------|-------------|------|-------|-------|\n")
	if err := apduSW(slot, []byte{0x00, 0xA4, 0x00, 0x0C, 0x02, 0x3F, 0x00}); err != nil {
		fmt.Fprintf(w, "_(SELECT MF failed: %v)_\n\n", err)
	}
	var efContents = map[uint16][]byte{}
	for _, ef := range mfEFs {
		sw, data := selectAndRead(slot, ef.FID)
		fmt.Fprintf(w, "| %04X | %-11s | %04X | %5d | %s |\n", ef.FID, ef.Name, sw, len(data), ef.Note)
		if sw == 0x9000 {
			efContents[ef.FID] = data
		}
	}
	fmt.Fprintln(w)

	// --- EF.GDO breakdown
	if data, ok := efContents[0x2F02]; ok {
		fmt.Fprintf(w, "## EF.GDO\n\n- raw: `%X`\n", data)
		if iccsn := extractICCSN(data); iccsn != nil {
			tag := "ICCSN"
			val := hex.EncodeToString(iccsn)
			if opts.Redact {
				val = redactID(iccsn)
				tag = "ICCSN (redacted)"
			}
			fmt.Fprintf(w, "- %s (10 bytes): `%s`\n", tag, val)
			if !opts.Redact {
				fmt.Fprintf(w, "- MII: %d, country/issuer prefix: %X\n", iccsn[0]>>4, iccsn[:3])
			}
		}
		fmt.Fprintln(w)
	}

	// --- EF.ATR breakdown
	if data, ok := efContents[0x2F01]; ok {
		fmt.Fprintf(w, "## EF.ATR (TLV)\n\n")
		fmt.Fprint(w, efATRDecode(data))
		fmt.Fprintln(w)
	}

	// --- AID probe
	fmt.Fprintf(w, "## Application directory probe\n\n")
	fmt.Fprintf(w, "| AID                                | Name         | P2=04 SW | P2=0C SW | Notes |\n")
	fmt.Fprintf(w, "|------------------------------------|--------------|----------|----------|-------|\n")
	// reset to MF before each AID try
	var presentAIDs []knownAID
	for _, a := range probedAIDs {
		_ = apduSW(slot, []byte{0x00, 0xA4, 0x00, 0x0C, 0x02, 0x3F, 0x00})
		sw04 := tryAID(slot, a.AID, 0x04)
		_ = apduSW(slot, []byte{0x00, 0xA4, 0x00, 0x0C, 0x02, 0x3F, 0x00})
		sw0C := tryAID(slot, a.AID, 0x0C)
		fmt.Fprintf(w, "| %-34X | %-12s | %04X     | %04X     | %s |\n",
			a.AID, a.Name, sw04, sw0C, a.Note)
		if sw04 == 0x9000 || sw0C == 0x9000 {
			presentAIDs = append(presentAIDs, a)
		}
	}
	fmt.Fprintln(w)

	// --- Card class heuristic
	class := classify(atr, efContents, presentAIDs)
	fmt.Fprintf(w, "## Inferred card class\n\n%s\n", class)

	return class, nil
}

// apduSW sends an APDU and returns the SW (data is ignored). Used for SELECTs
// where we only care about success/failure.
func apduSW(slot *orga.Slot, apdu []byte) error {
	resp, err := slot.Transmit(apdu)
	if err != nil {
		return err
	}
	if len(resp) < 2 {
		return fmt.Errorf("short response: %X", resp)
	}
	return nil
}

func selectAndRead(slot *orga.Slot, fid uint16) (uint16, []byte) {
	sel := []byte{0x00, 0xA4, 0x02, 0x0C, 0x02, byte(fid >> 8), byte(fid & 0xFF)}
	resp, err := slot.Transmit(sel)
	if err != nil || len(resp) < 2 {
		return 0, nil
	}
	sw := uint16(resp[len(resp)-2])<<8 | uint16(resp[len(resp)-1])
	if sw != 0x9000 {
		return sw, nil
	}
	read, err := slot.Transmit([]byte{0x00, 0xB0, 0x00, 0x00, 0x00})
	if err != nil || len(read) < 2 {
		return sw, nil
	}
	rsw := uint16(read[len(read)-2])<<8 | uint16(read[len(read)-1])
	return rsw, read[:len(read)-2]
}

func tryAID(slot *orga.Slot, aid []byte, p2 byte) uint16 {
	apdu := append([]byte{0x00, 0xA4, 0x04, p2, byte(len(aid))}, aid...)
	if p2 == 0x04 {
		apdu = append(apdu, 0x00) // Le=0
	}
	resp, err := slot.Transmit(apdu)
	if err != nil || len(resp) < 2 {
		return 0
	}
	return uint16(resp[len(resp)-2])<<8 | uint16(resp[len(resp)-1])
}

func trimATRWarnings(b []byte) []byte {
	// Drop a trailing 62xx / 9000 / 9001 if it looks like an appended SW.
	if len(b) < 4 {
		return b
	}
	last2 := b[len(b)-2:]
	if last2[0] == 0x62 || (last2[0] == 0x90 && (last2[1] == 0x00 || last2[1] == 0x01)) {
		return b[:len(b)-2]
	}
	return b
}

func extractICCSN(efgdo []byte) []byte {
	// EF.GDO format per gemSpec: BER-TLV with tag 5A, length 0A (10 bytes).
	recs := parseTLV(efgdo)
	for _, r := range recs {
		if r.Tag == 0x5A && r.Length == 10 {
			return r.Value
		}
	}
	return nil
}

func redactID(b []byte) string {
	// Trivial visible hash (first 4 bytes of a non-cryptographic mix) — good
	// enough to correlate across reports without leaking the actual ICCSN.
	var x uint32 = 0xC0FFEE
	for _, c := range b {
		x = x*1664525 + uint32(c) + 1013904223
	}
	return fmt.Sprintf("id-%08x", x)
}

// classify makes a best-effort guess at what kind of card we're holding.
func classify(atr []byte, efs map[uint16][]byte, aids []knownAID) string {
	hasHCA := false
	hasESIGN := false
	hasHPA := false
	hasAUTO := false
	hasQES := false
	hasSMA := false
	for _, a := range aids {
		switch a.Name {
		case "DF.HCA":
			hasHCA = true
		case "DF.ESIGN":
			hasESIGN = true
		case "DF.HPA":
			hasHPA = true
		case "DF.AUTO":
			hasAUTO = true
		case "DF.QES":
			hasQES = true
		case "DF.SMA":
			hasSMA = true
		}
	}

	// Look for T-Systems test-card fingerprint in EF.ATR.
	tcosTest := false
	if data, ok := efs[0x2F01]; ok {
		s := string(data)
		if strings.Contains(s, "TSYSITCOS") {
			tcosTest = true
		}
	}

	switch {
	case hasHCA:
		return "**eGK** (electronic Gesundheitskarte) — DF.HCA present. Use this slot for patient data reads."
	case hasHPA && hasQES:
		return "**HBA** (Heilberufsausweis) — DF.HPA + DF.QES present."
	case hasSMA:
		return "**SMC-B** (Security Module Card type B) — DF.SMA present. Suitable as the C2C peer for a real eGK."
	case hasESIGN && tcosTest:
		return "**T-Systems TCOS test/development card** — DF.ESIGN + TSYSITCOS fingerprint in EF.ATR. Not a production SMC-B; C2C against a real eGK will fail unless this card carries a gematik-chained CV-cert (verify before relying on it)."
	case hasESIGN || hasAUTO:
		return "**ESIGN-bearing card** (HBA / SMC-B / other) — has electronic-signature app but specific role unclear. Probe DF.ESIGN contents for CV-certs to refine."
	default:
		return "**unknown** — no recognized application DF. Could be a blank, a foreign-issuer card, or a non-healthcare smart card."
	}
}

func decodeSlotStatusReason(s byte) string { // kept for backwards compat; main uses decodeSlotStatus
	return decodeSlotStatus(s)
}

// runIdentify wires identify() to a Terminal opened by main(). Writes
// markdown to stdout and (when out != "") also to a file.
func runIdentify(t *orga.Terminal, slot int, outPath string, redact bool) error {
	var w io.Writer = os.Stdout
	var f *os.File
	if outPath != "" {
		var err error
		f, err = os.Create(outPath)
		if err != nil {
			return err
		}
		defer f.Close()
		w = io.MultiWriter(os.Stdout, f)
	}
	_, err := identify(t, IdentifyOptions{Slot: slot, Out: w, Redact: redact})
	return err
}
