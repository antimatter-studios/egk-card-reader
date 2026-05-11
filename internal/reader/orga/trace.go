package orga

import (
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"
)

// traceEnabled is set once at init time from the ORGA_TRACE env var. Setting
// ORGA_TRACE=1 (or any non-empty, non-"0" value) logs every T=1 block sent
// and received to stderr with a millisecond-resolution timestamp, plus a
// short PCB classification for I/R/S blocks.
//
// The trace is line-oriented and append-only — safe to redirect to a file
// for post-mortem analysis after a terminal fault. We keep the format
// deterministic so diffing two traces highlights divergences.
var traceEnabled = func() bool {
	v := os.Getenv("ORGA_TRACE")
	return v != "" && v != "0" && v != "false"
}()

var traceMu sync.Mutex

func traceTX(b []byte) {
	if !traceEnabled {
		return
	}
	traceMu.Lock()
	defer traceMu.Unlock()
	fmt.Fprintf(os.Stderr, "%s ORGA TX %s  %s\n",
		time.Now().Format("15:04:05.000"), hex.EncodeToString(b), classifyBlock(b))
}

func traceRX(b []byte) {
	if !traceEnabled {
		return
	}
	traceMu.Lock()
	defer traceMu.Unlock()
	fmt.Fprintf(os.Stderr, "%s ORGA RX %s  %s\n",
		time.Now().Format("15:04:05.000"), hex.EncodeToString(b), classifyBlock(b))
}

// classifyBlock returns a short tag for the block's PCB classification.
// Returns "?" if the input is too short to be a valid T=1 block.
func classifyBlock(b []byte) string {
	if len(b) < 4 {
		return "?"
	}
	pcb := b[1]
	switch {
	case pcb&0x80 == 0:
		return fmt.Sprintf("I-block N(S)=%d M=%d len=%d", (pcb>>6)&1, (pcb>>5)&1, b[2])
	case pcb&0xC0 == 0x80:
		return fmt.Sprintf("R-block N(R)=%d err=%d", (pcb>>4)&1, pcb&0x0F)
	default:
		code := pcb & 0x1F
		dir := "req"
		if pcb&0x20 != 0 {
			dir = "resp"
		}
		names := map[byte]string{0: "RESYNCH", 1: "IFS", 2: "ABORT", 3: "WTX", 4: "VPP"}
		name, ok := names[code]
		if !ok {
			name = fmt.Sprintf("S?%d", code)
		}
		return fmt.Sprintf("S-block %s %s", name, dir)
	}
}
