// Package reader exposes a uniform smart-card reader contract and a factory
// that picks an available driver at runtime.
//
// Two drivers are bundled:
//
//   - internal/reader/orga    — Ingenico/Worldline ORGA 9xx over CDC-ACM/T=1.
//     Multi-slot. Discovered via /dev/cu.usbmodem*.
//   - internal/reader/generic — Any CCID/PC/SC reader (Cherry ST-2100,
//     OMNIKEY 3121, REINER cyberJack, …). Single-slot per reader. Routed
//     through PC/SC.
//
// Higher-level protocol code (internal/egk, the future C2C handshake) takes
// reader.Card and does not know or care which driver is underneath.
package reader

import (
	"errors"
	"fmt"
	"time"

	"github.com/christhomas/card-reader/internal/reader/generic"
	"github.com/christhomas/card-reader/internal/reader/orga"
)

// Card is one logical smart-card session — APDU in, response (data + SW1SW2) out.
// Both *orga.Slot and *generic.Card satisfy this structurally.
type Card interface {
	Transmit(apdu []byte) ([]byte, error)
}

// Session is the reader-driver-agnostic handle. Slot(n) returns the Card in
// the requested slot — single-slot drivers accept only n=1.
type Session interface {
	// Slot returns the card in slot n (1 = primary / eGK / front).
	// Multi-slot drivers also accept n=2 (SMC-B / back). Single-slot
	// drivers return an error for any n other than 1.
	Slot(n int) (Card, error)

	// Kind identifies the driver in use for logging/diagnostics
	// (e.g. "orga", "pcsc:Cherry Smart Terminal ST-2xxx 00 00").
	Kind() string

	// Close releases the underlying handles. After Close, Slot/Transmit
	// calls return errors.
	Close() error
}

// Options tweak the factory behaviour. Zero value is fine for typical use.
type Options struct {
	// Force selects a specific driver. Empty (default) auto-detects:
	// try ORGA first, fall back to PC/SC.
	// Valid: "" | "orga" | "generic" | "pcsc" (alias for generic).
	Force string

	// ORGADevNode pins a specific serial device for the ORGA driver
	// (e.g. "/dev/cu.usbmodem11301"). Empty = auto-pick first match.
	// Ignored for non-ORGA drivers.
	ORGADevNode string

	// PCSCReaderName pins a specific reader name for the PC/SC driver
	// (substring match). Empty = first reader with a present card.
	// Ignored for non-PC/SC drivers.
	PCSCReaderName string

	// WaitForCard is how long the PC/SC driver waits for card insertion.
	// Ignored for ORGA (the card is either in a slot or not).
	WaitForCard time.Duration

	// AllowPINWrite removes the safety guardrail on dangerous APDUs
	// (VERIFY, CHANGE REFERENCE DATA, UPDATE, ERASE, PUT DATA, CT-BCS
	// PERFORM VERIFICATION). Default false. Enable only in code paths
	// that intentionally write to the card.
	AllowPINWrite bool
}

// Open opens a reader session. Behaviour depends on Options.Force:
//
//   - Force == "":               auto-detect via Detect().Pick() (ORGA > PC/SC)
//   - Force == "orga":           open ORGA at ORGADevNode (or first match)
//   - Force == "generic"|"pcsc": open PC/SC at PCSCReaderName (or first ready)
//
// For introspecting what's available without opening, call Detect() directly.
// For opening a specific Driver returned by Probe.Drivers(), use OpenDriver.
func Open(opts Options) (Session, error) {
	switch opts.Force {
	case "orga":
		return openORGA(opts)
	case "generic", "pcsc":
		return openGeneric(opts)
	case "":
		d, err := Detect().Pick()
		if err != nil {
			return nil, err
		}
		switch d.Kind {
		case "orga":
			opts.ORGADevNode = d.Device
			return openORGA(opts)
		case "pcsc":
			opts.PCSCReaderName = d.Device
			return openGeneric(opts)
		}
		return nil, fmt.Errorf("reader: probe returned unknown kind %q", d.Kind)
	default:
		return nil, fmt.Errorf("reader: unknown force=%q (valid: orga, generic, pcsc)", opts.Force)
	}
}

func openORGA(opts Options) (Session, error) {
	t, err := orga.Open(orga.Options{
		DevNode:       opts.ORGADevNode,
		AllowPINWrite: opts.AllowPINWrite,
	})
	if err != nil {
		return nil, err
	}
	if _, err := t.ActivateSlot(1); err != nil {
		_ = t.Close()
		return nil, fmt.Errorf("reader/orga: activate slot 1: %w", err)
	}
	return &orgaSession{t: t}, nil
}

func openGeneric(opts Options) (Session, error) {
	r, err := generic.Open()
	if err != nil {
		return nil, err
	}
	timeout := opts.WaitForCard
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	name, err := pickReader(r, opts.PCSCReaderName, timeout)
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	c, err := r.Connect(name)
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	return &genericSession{r: r, c: c, name: name}, nil
}

func pickReader(r *generic.Reader, want string, timeout time.Duration) (string, error) {
	if want != "" {
		for _, name := range r.Readers() {
			if name == want || containsFold(name, want) {
				return name, nil
			}
		}
		return "", fmt.Errorf("reader/generic: no PC/SC reader matching %q (have: %v)", want, r.Readers())
	}
	return r.WaitForCard(timeout)
}

func containsFold(haystack, needle string) bool {
	// Tiny ASCII-fold contains; reader names are ASCII in practice.
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		ok := true
		for j := 0; j < len(needle); j++ {
			a, b := haystack[i+j], needle[j]
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if b >= 'A' && b <= 'Z' {
				b += 32
			}
			if a != b {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// ErrNoSlot is returned by Session.Slot when the underlying driver doesn't
// have the requested slot index.
var ErrNoSlot = errors.New("reader: requested slot not available on this driver")

type orgaSession struct {
	t *orga.Terminal
}

func (s *orgaSession) Slot(n int) (Card, error) {
	if n != 1 && n != 2 {
		return nil, fmt.Errorf("%w: orga supports slots 1 and 2, got %d", ErrNoSlot, n)
	}
	return s.t.Slot(n), nil
}
func (s *orgaSession) Kind() string  { return "orga" }
func (s *orgaSession) Close() error  { return s.t.Close() }

// Terminal exposes the underlying *orga.Terminal for callers that need
// driver-specific operations (TerminalInfo, slot activate/eject). Returns
// nil if this Session is not ORGA-backed.
func (s *orgaSession) Terminal() *orga.Terminal { return s.t }

type genericSession struct {
	r    *generic.Reader
	c    *generic.Card
	name string
}

func (s *genericSession) Slot(n int) (Card, error) {
	if n != 1 {
		return nil, fmt.Errorf("%w: PC/SC drivers expose one card per reader (slot 1), got %d", ErrNoSlot, n)
	}
	return s.c, nil
}
func (s *genericSession) Kind() string { return "pcsc:" + s.name }
func (s *genericSession) Close() error {
	cerr := s.c.Close()
	rerr := s.r.Close()
	if cerr != nil {
		return cerr
	}
	return rerr
}
