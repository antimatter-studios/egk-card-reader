// orga-probe is a thin CLI wrapper around internal/orga for one-shot
// experimentation against the ORGA 9xx terminal.
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/antimatter-studios/egk-card-reader/pkg/reader/orga"
)

func main() {
	var (
		dev    = flag.String("dev", "", "serial device (default: first /dev/cu.usbmodem*)")
		apdu   = flag.String("apdu", "", "APDU bytes (hex, whitespace ignored)")
		slot   = flag.Int("slot", 0, "0=terminal (CT-BCS), 1=ICC1, 2=ICC2")
		tmoMs  = flag.Int("timeout-ms", 8000, "per-transaction timeout (ms)")
		unsafe = flag.Bool("UNSAFE-allow-pin-write", false, "DANGER: permit VERIFY / CHANGE REFERENCE DATA / UPDATE / ERASE / CT-BCS PERFORM VERIFICATION. Wrong VERIFYs decrement the eGK PIN counter (3 strikes = locked card)")
		info     = flag.Bool("info", false, "fetch terminal info (GET STATUS P2=46) and exit")
		act      = flag.Int("activate", 0, "activate slot n (1 or 2), print ATR, then exit")
		status   = flag.Int("status", 0, "GET STATUS for slot n (1 or 2), print status byte, then exit")
		ident    = flag.Int("identify", 0, "full identity probe on slot n (1 or 2) — emits structured markdown")
		identOut = flag.String("identify-out", "", "with -identify: also write markdown to this path (default: stdout only)")
		redact   = flag.Bool("redact", false, "with -identify: hash ICCSN to an opaque id-* instead of printing raw")
		readCert  = flag.Int("readcert", 0, "slot n: read an X.509 cert from the EF at -fid (optionally after -aid), parse, summarize")
		certAID   = flag.String("aid", "", "with -readcert: AID to SELECT before the EF (hex). Empty = stay at MF.")
		certFID   = flag.String("fid", "", "with -readcert: 2-byte EF file identifier (hex)")
		certOut   = flag.String("out", "", "with -readcert: write raw DER + matching PEM to this path")
		certParse = flag.Bool("parse", true, "with -readcert: also crypto/x509-parse and summarize")
		c2cSlot   = flag.Int("c2c", 0, "slot n: drive C2C Discover + Validate phases, print structured report")
		c2cOut    = flag.String("c2c-out", "", "with -c2c: also write markdown report to this path")
		c2cTest   = flag.Bool("c2c-test-roots", true, "with -c2c: validate against gematik TEST roots (true) or production roots (false)")
	)
	flag.Parse()

	t, err := orga.Open(orga.Options{
		DevNode:       *dev,
		Timeout:       time.Duration(*tmoMs) * time.Millisecond,
		AllowPINWrite: *unsafe,
	})
	if err != nil {
		fail(err)
	}
	defer t.Close()

	switch {
	case *info:
		b, err := t.TerminalInfo()
		if err != nil {
			fail(err)
		}
		fmt.Printf("%s  | %q\n", hex.EncodeToString(b), printableASCII(b))

	case *act != 0:
		atr, err := t.ActivateSlot(*act)
		if err != nil {
			fail(err)
		}
		fmt.Printf("slot %d ATR: %X\n", *act, atr)

	case *status != 0:
		s, err := t.SlotStatus(*status)
		if err != nil {
			fail(err)
		}
		fmt.Printf("slot %d status: 0x%02X (%s)\n", *status, s, decodeSlotStatus(s))

	case *ident != 0:
		if err := runIdentify(t, *ident, *identOut, *redact); err != nil {
			fail(err)
		}

	case *readCert != 0:
		if err := runReadCert(t, *readCert, *certAID, *certFID, *certOut, *certParse); err != nil {
			fail(err)
		}

	case *c2cSlot != 0:
		if err := runC2C(t, *c2cSlot, *c2cOut, *c2cTest); err != nil {
			fail(err)
		}

	case *apdu != "":
		buf, err := decodeHex(*apdu)
		if err != nil {
			fail(err)
		}
		ts := time.Now()
		var resp []byte
		switch *slot {
		case 0:
			resp, err = t.CTBCS(buf)
		case 1, 2:
			resp, err = t.Slot(*slot).Transmit(buf)
		default:
			fail(fmt.Errorf("invalid -slot %d", *slot))
		}
		if err != nil {
			fail(err)
		}
		fmt.Printf("%s TX: %s  | slot=%d\n", ts.Format("15:04:05.000"), hex.EncodeToString(buf), *slot)
		if len(resp) >= 2 {
			data, sw := resp[:len(resp)-2], resp[len(resp)-2:]
			fmt.Printf("%s RX data (%d): %s\n%s RX SW: %X\n",
				time.Now().Format("15:04:05.000"), len(data), hex.EncodeToString(data),
				time.Now().Format("15:04:05.000"), sw)
		} else {
			fmt.Printf("%s RX (%d): %s\n", time.Now().Format("15:04:05.000"), len(resp), hex.EncodeToString(resp))
		}

	default:
		flag.Usage()
		os.Exit(2)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func decodeHex(s string) ([]byte, error) {
	clean := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ',' || r == ':' {
			return -1
		}
		return r
	}, s)
	return hex.DecodeString(clean)
}

func decodeSlotStatus(s byte) string {
	if s == 0x00 {
		return "no card"
	}
	parts := []string{}
	if s&0x01 != 0 {
		parts = append(parts, "present")
	}
	if s&0x02 != 0 {
		parts = append(parts, "active")
	}
	if s&0x04 != 0 {
		parts = append(parts, "processed")
	}
	if len(parts) == 0 {
		return fmt.Sprintf("unknown 0x%02X", s)
	}
	return strings.Join(parts, "|")
}
