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
	for fid := uint16(0xD001); fid <= 0xD010; fid++ {
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
}
