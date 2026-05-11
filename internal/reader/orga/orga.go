// Package orga drives an Ingenico/Worldline ORGA 9xx card terminal directly
// over its USB-CDC-ACM serial endpoint on macOS / Linux. It speaks plain
// ISO 7816-3 T=1 framing — no vendor driver, no PC/SC daemon.
//
// The Slot type implements the egk.Card interface, so eGK reads work
// identically whether the underlying transport is PC/SC (Cherry, OMNIKEY) or
// the ORGA via this package.
//
// Safety: dangerous APDUs (VERIFY, CHANGE REFERENCE DATA, UPDATE BINARY,
// PUT DATA, ERASE, CT-BCS PERFORM VERIFICATION, …) are refused unless the
// caller passes AllowPINWrite. See safety.go.
package orga

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

const (
	hostAddr     byte = 0x02
	terminalAddr byte = 0x01
	icc1Addr     byte = 0x00
	icc2Addr     byte = 0x02
)

const DefaultTimeout = 8 * time.Second

// Terminal is a session with an ORGA card terminal. Safe for concurrent use —
// all transactions are serialized through an internal mutex.
type Terminal struct {
	io        serialIO
	mu        sync.Mutex
	timeout   time.Duration
	allowWrite bool
	// T=1 N(S) tracking. Key = peer NAD (terminal, ICC1, ICC2 — the high nibble
	// of the wire NAD). After RESYNCH both sides reset to 0.
	ns map[byte]byte
	// Set after the first successful exchange so we know the terminal has
	// negotiated IFSC; informational only.
	ifsNegotiated bool
}

// Options configures Open. Zero value is fine for typical use.
type Options struct {
	// DevNode is the serial device path (e.g. "/dev/cu.usbmodem11301"). If
	// empty, Open will pick the first /dev/cu.usbmodem* node.
	DevNode string
	// Baud overrides the default 9600 used by ORGA over USB. Leave 0 unless
	// you're using the RS-232 cable variant (which wants 115200).
	Baud int
	// Timeout caps each T=1 transaction, including S(WTX)-extended waits.
	// Zero means DefaultTimeout.
	Timeout time.Duration
	// AllowPINWrite removes the mechanical block on VERIFY / UPDATE / etc.
	// Default false. Setting true means accidental misuse can permanently
	// lock the card's PIN — only enable in code paths that explicitly
	// require it.
	AllowPINWrite bool
}

// Open connects to an ORGA terminal and resets T=1 state with a RESYNCH.
// Caller must Close when done. DevNode is required — callers should
// discover the path via the parent reader package's USB probe rather than
// relying on a /dev glob, which could return a stale node when the
// terminal has just disconnected.
func Open(opts Options) (*Terminal, error) {
	dev := opts.DevNode
	if dev == "" {
		// Last-resort glob — kept for orga-probe-style direct callers, but
		// emit a warning-shaped error rather than picking blindly: a stale
		// /dev/cu.usbmodem* node can outlive its USB device for several
		// seconds on macOS, and opening one ends in ENXIO on T=1 resync.
		matches, err := filepath.Glob("/dev/cu.usbmodem*")
		if err != nil || len(matches) == 0 {
			return nil, fmt.Errorf("orga: no /dev/cu.usbmodem* device found; is the ORGA plugged in and out of DFU mode?")
		}
		dev = matches[0]
	}
	baud := opts.Baud
	if baud == 0 {
		baud = 9600
	}
	io, err := openSerial(dev, baud)
	if err != nil {
		return nil, fmt.Errorf("orga: open %s: %w", dev, friendlySerialError(err))
	}
	t := &Terminal{
		io:         io,
		timeout:    opts.Timeout,
		allowWrite: opts.AllowPINWrite,
		ns:         map[byte]byte{},
	}
	if t.timeout == 0 {
		t.timeout = DefaultTimeout
	}
	if err := t.resync(); err != nil {
		io.Close()
		return nil, fmt.Errorf("orga: T=1 resync on %s: %w", dev, friendlySerialError(err))
	}
	return t, nil
}

// Close releases the serial port. Does not eject any inserted cards — call
// Slot.Eject explicitly first if that's what you want.
func (t *Terminal) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.io.Close()
}

// Slot returns a handle for slot n (1=front/ICC1, 2=back/ICC2). The returned
// *Slot is cheap and may be reused; APDUs sent through different slots are
// serialized through the same underlying T=1 link.
func (t *Terminal) Slot(n int) *Slot {
	var dad byte
	switch n {
	case 1:
		dad = icc1Addr
	case 2:
		dad = icc2Addr
	default:
		panic(fmt.Sprintf("orga: invalid slot %d (must be 1 or 2)", n))
	}
	return &Slot{term: t, n: n, peer: dad}
}

// Slot is a CT-API logical slot. Implements egk.Card via Transmit.
type Slot struct {
	term *Terminal
	n    int
	peer byte // peer address nibble (0=ICC1, 2=ICC2)
}

// Number returns the slot index (1 or 2).
func (s *Slot) Number() int { return s.n }

// Transmit sends a single ISO 7816-4 APDU to the card in this slot and
// returns the full response including SW1SW2 trailer. Satisfies
// egk.Card.Transmit. Refused if the APDU is on the dangerous-INS list and
// Options.AllowPINWrite was not set.
func (s *Slot) Transmit(apdu []byte) ([]byte, error) {
	if err := checkAPDUSafe(apdu, s.term.allowWrite); err != nil {
		return nil, err
	}
	return s.term.transactWithNAD(s.peer, apdu)
}
