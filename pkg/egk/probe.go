package egk

import (
	"encoding/hex"
	"fmt"
	"os"
)

// ProbeDFHCA selects DF.HCA and walks a range of candidate FIDs / SFIs to
// discover which admin EFs respond on the live card. Output goes to stderr.
// Invoked only when EGK_PROBE_HCA=1 is set in the environment, so production
// reads are unaffected.
func ProbeDFHCA(card Card) {
	if os.Getenv("EGK_PROBE_HCA") != "1" {
		return
	}
	fmt.Fprintln(os.Stderr, "[probe] --- DF.HCA EF scan ---")
	if _, err := selectByAID(card, aidHCA); err != nil {
		fmt.Fprintf(os.Stderr, "[probe] SELECT DF.HCA failed: %v\n", err)
		return
	}
	// Sweep the D000..D010 range used by gemSpec_eGK_ObjSys for DF.HCA EFs.
	for fid := fidPD; fid <= 0xD010; fid++ {
		err := selectEF(card, fid)
		if err != nil {
			continue
		}
		raw, rerr := readBinary(card, 0, 0, 32)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "[probe] FID %04X: SELECT ok, READ err: %v\n", fid, rerr)
			continue
		}
		fmt.Fprintf(os.Stderr, "[probe] FID %04X: %d bytes  %s\n", fid, len(raw), hex.EncodeToString(raw))
	}
	// Also SFI-based reads — some EFs respond by SFI even when FID-select fails.
	fmt.Fprintln(os.Stderr, "[probe] --- SFI scan (DF.HCA) ---")
	for sfi := byte(1); sfi <= 0x1E; sfi++ {
		raw, err := readBinary(card, sfi, 0, 16)
		if err != nil {
			continue
		}
		fmt.Fprintf(os.Stderr, "[probe] SFI %02X: %d bytes  %s\n", sfi, len(raw), hex.EncodeToString(raw))
	}
	// Certificate FIDs in DF.HCA — C500..C50F range. X.509 certs are large
	// (~1 KB), so try a long READ to spot them.
	fmt.Fprintln(os.Stderr, "[probe] --- cert FID scan (DF.HCA) ---")
	for fid := fidCertRangeStart; fid <= fidCertRangeEnd; fid++ {
		if err := selectEF(card, fid); err != nil {
			continue
		}
		raw, err := readBinary(card, 0, 0, 32)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[probe] FID %04X: SELECT ok, READ err: %v\n", fid, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "[probe] FID %04X: %d bytes  %s\n", fid, len(raw), hex.EncodeToString(raw))
	}
	// Probe DF.ESIGN inside the eGK (different AID).
	fmt.Fprintln(os.Stderr, "[probe] --- DF.ESIGN scan ---")
	if _, err := selectByAID(card, aidESIGN); err != nil {
		fmt.Fprintf(os.Stderr, "[probe] SELECT DF.ESIGN failed: %v\n", err)
	} else {
		for fid := fidCertRangeStart; fid <= fidCertRangeEnd; fid++ {
			if err := selectEF(card, fid); err != nil {
				continue
			}
			raw, rerr := readBinary(card, 0, 0, 32)
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "[probe] ESIGN FID %04X: SELECT ok, READ err: %v\n", fid, rerr)
				continue
			}
			fmt.Fprintf(os.Stderr, "[probe] ESIGN FID %04X: %d bytes  %s\n",
				fid, len(raw), hex.EncodeToString(raw))
		}
	}
	// Restore DF.HCA selection so subsequent reads still hit the right context.
	_, _ = selectByAID(card, aidHCA)
	// Record-structured EFs — READ RECORD (INS B2). 6981 from READ BINARY
	// usually means "use READ RECORD instead". Print the SW per attempt so
	// we can distinguish empty records (6A83) from PIN-required (6982) from
	// successful empty payloads (9000 with len=0).
	fmt.Fprintln(os.Stderr, "[probe] --- READ RECORD scan (DF.HCA) ---")
	// P2 = 0x04 means "current EF, by record number" per ISO 7816-4 §7.3.3.
	const readRecordCurrentEF byte = 0x04
	for _, fid := range []uint16{0xD003, 0xD005, 0xD006, 0xD007, 0xD008, 0xD009, 0xD00A, 0xD00B} {
		if err := selectEF(card, fid); err != nil {
			continue
		}
		for rec := byte(1); rec <= 3; rec++ {
			apdu := []byte{claISO, insReadRecord, rec, readRecordCurrentEF, 0x00}
			data, sw, err := transmit(card, apdu)
			if err != nil {
				continue
			}
			if sw&0xFF00 == sw6Cxx {
				apdu[4] = byte(sw & 0xFF)
				data, sw, _ = transmit(card, apdu)
			}
			fmt.Fprintf(os.Stderr, "[probe] FID %04X rec %d: SW=%04X len=%d  %s\n",
				fid, rec, sw, len(data), hex.EncodeToString(data))
		}
	}
}
