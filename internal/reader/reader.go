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
	"strings"
	"time"

	"github.com/christhomas/card-reader/internal/reader/generic"
	"github.com/christhomas/card-reader/internal/reader/orga"
	"github.com/christhomas/card-reader/internal/reader/usb"
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

	// Identify returns descriptive information about the underlying reader
	// hardware (manufacturer, product, serial, device path, USB IDs when
	// applicable) plus a human-readable reason why this driver was chosen.
	// Best-effort: fields with no source data are left empty.
	Identify() DeviceInfo

	// Close releases the underlying handles. After Close, Slot/Transmit
	// calls return errors.
	Close() error
}

// DeviceInfo is the cross-driver descriptor returned by Session.Identify.
// Drivers populate as many fields as their underlying transport exposes;
// callers should accept empty strings for fields the driver can't surface.
type DeviceInfo struct {
	Driver          string // "orga" or "pcsc"
	Manufacturer    string // USB iManufacturer / PC/SC vendor prefix
	Product         string // USB iProduct / PC/SC reader name
	SerialNumber    string // USB iSerial; empty for PC/SC
	Device          string // /dev/cu.usbmodem* for orga; reader name for pcsc
	VendorID        uint16 // USB VID; 0 if not applicable
	ProductID       uint16 // USB PID; 0 if not applicable
	FirmwareInfo    string // ORGA GET STATUS terminal info; PC/SC ATR hex
	SelectionReason string // why this driver was chosen over alternatives
}

// String formats DeviceInfo as a multi-line block suitable for logging or
// CLI status output. Empty fields are omitted.
func (i DeviceInfo) String() string {
	lines := []string{fmt.Sprintf("  Driver:        %s", i.Driver)}
	if i.Manufacturer != "" {
		lines = append(lines, fmt.Sprintf("  Manufacturer:  %s", i.Manufacturer))
	}
	if i.Product != "" {
		lines = append(lines, fmt.Sprintf("  Product:       %s", i.Product))
	}
	if i.SerialNumber != "" {
		lines = append(lines, fmt.Sprintf("  Serial:        %s", i.SerialNumber))
	}
	if i.Device != "" {
		lines = append(lines, fmt.Sprintf("  Device:        %s", i.Device))
	}
	if i.VendorID != 0 || i.ProductID != 0 {
		lines = append(lines, fmt.Sprintf("  USB VID/PID:   0x%04X / 0x%04X", i.VendorID, i.ProductID))
	}
	if i.FirmwareInfo != "" {
		lines = append(lines, fmt.Sprintf("  Firmware:      %s", i.FirmwareInfo))
	}
	if i.SelectionReason != "" {
		lines = append(lines, fmt.Sprintf("  Reason:        %s", i.SelectionReason))
	}
	return strings.Join(lines, "\n")
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
	// Enumerate ORGA-matching USB devices first so we have the descriptor
	// strings ready for Identify() AND so we can fail fast if no real ORGA
	// is plugged in. Without this check, orga.Open would fall back to a
	// /dev/cu.usbmodem* glob and could pick a stale node (e.g. left over
	// from a terminal that just rebooted), producing a confusing ENXIO
	// downstream.
	var picked usb.Device
	probeErr := error(nil)
	devs, probeErr := usb.Default().FindDevices(ORGAUSBVendorID, ORGAUSBProductID)
	if probeErr == nil {
		switch {
		case opts.ORGADevNode == "" && len(devs) > 0:
			picked = devs[0]
		case opts.ORGADevNode != "":
			for _, d := range devs {
				if d.DevicePath == opts.ORGADevNode {
					picked = d
					break
				}
			}
		}
	}

	devPath := opts.ORGADevNode
	if devPath == "" {
		devPath = picked.DevicePath
	}

	if devPath == "" {
		// No USB descriptor match and no explicit path. Refuse rather than
		// pick a stale /dev node.
		if probeErr != nil && !errors.Is(probeErr, usb.ErrUnsupported) {
			return nil, fmt.Errorf("reader/orga: USB probe failed: %w", probeErr)
		}
		return nil, fmt.Errorf("reader/orga: no ORGA terminal detected (VID 0x%04X / PID 0x%04X) — check that it's plugged in and not in DFU mode (PID 0xDF55)",
			ORGAUSBVendorID, ORGAUSBProductID)
	}

	t, err := orga.Open(orga.Options{
		DevNode:       devPath,
		AllowPINWrite: opts.AllowPINWrite,
	})
	if err != nil {
		return nil, err
	}
	if _, err := t.ActivateSlot(1); err != nil {
		_ = t.Close()
		return nil, fmt.Errorf("reader/orga: activate slot 1: %w", err)
	}
	return &orgaSession{t: t, dev: picked}, nil
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
	t   *orga.Terminal
	dev usb.Device
}

func (s *orgaSession) Slot(n int) (Card, error) {
	if n != 1 && n != 2 {
		return nil, fmt.Errorf("%w: orga supports slots 1 and 2, got %d", ErrNoSlot, n)
	}
	return s.t.Slot(n), nil
}
func (s *orgaSession) Kind() string { return "orga" }
func (s *orgaSession) Close() error { return s.t.Close() }

func (s *orgaSession) Identify() DeviceInfo {
	info := DeviceInfo{
		Driver:          "orga",
		Manufacturer:    s.dev.Manufacturer,
		Product:         s.dev.Product,
		SerialNumber:    s.dev.Serial,
		Device:          s.dev.DevicePath,
		VendorID:        ORGAUSBVendorID,
		ProductID:       ORGAUSBProductID,
		SelectionReason: fmt.Sprintf("USB descriptor matched ORGA 9xx family (VID 0x%04X / PID 0x%04X)", ORGAUSBVendorID, ORGAUSBProductID),
	}
	if info.Manufacturer == "" {
		info.Manufacturer = "Ingenico Healthcare GmbH"
	}
	if fw, err := s.t.TerminalInfo(); err == nil {
		info.FirmwareInfo = summarizeTerminalInfo(fw)
	}
	return info
}

// summarizeTerminalInfo pulls the ASCII version tag out of the CT-BCS
// GET STATUS reply. The full body is a vendor-defined TLV blob; we only
// need a short human-readable summary for Identify().
func summarizeTerminalInfo(b []byte) string {
	const wantPrefix = "FHDE"
	idx := strings.Index(string(b), wantPrefix)
	if idx < 0 || len(b) < idx+22 {
		return ""
	}
	tag := string(b[idx : idx+22])
	tag = strings.TrimRight(tag, "\x00 ")
	return tag
}

// Terminal exposes the underlying *orga.Terminal for callers that need
// driver-specific operations (TerminalInfo, slot activate/eject). Returns
// nil if this Session is not ORGA-backed.
func (s *orgaSession) Terminal() *orga.Terminal { return s.t }

func (s *genericSession) Identify() DeviceInfo {
	info := DeviceInfo{
		Driver:          "pcsc",
		Product:         s.name,
		Device:          s.name,
		SelectionReason: "PC/SC daemon enumerated this reader; no higher-priority ORGA device detected",
	}
	if atr, err := s.c.ATR(); err == nil && len(atr) > 0 {
		info.FirmwareInfo = fmt.Sprintf("ATR=%X", atr)
	}
	return info
}

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
