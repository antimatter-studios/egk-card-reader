package reader

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/antimatter-studios/egk-card-reader/pkg/reader/generic"
	"github.com/antimatter-studios/egk-card-reader/pkg/reader/orga"
	"github.com/antimatter-studios/egk-card-reader/pkg/reader/usb"
)

// Watcher holds a reader open across many presence polls so a long-running
// consumer (e.g. a monitoring daemon) can cheaply answer "is a card present?"
// and only pay the cost of activating/connecting + reading when one actually
// is.
//
// It exists because the one-shot Open → Slot → Read flow (Session) deliberately
// bundles wait-for-card with connect: a PC/SC session cannot exist without a
// card, and re-opening the ORGA serial port on every poll churns the terminal.
// Watcher instead keeps the underlying handle (orga.Terminal or
// generic.Reader) open for its whole lifetime and answers presence via CT-BCS
// GET STATUS (ORGA) or a non-blocking SCardGetStatusChange (PC/SC).
//
// A Watcher is NOT safe for concurrent use — drive it from a single goroutine.
type Watcher struct {
	kind string
	info DeviceInfo

	// orga-backed
	term *orga.Terminal

	// pcsc-backed
	gr       *generic.Reader
	pcscName string        // reader name observed holding a card (set by Present)
	card     *generic.Card // currently-acquired PC/SC card, released by Release/Close
}

// OpenWatcher detects the best available reader (ORGA > PC/SC, unless
// opts.Force pins one) and opens it WITHOUT requiring a card to be present.
// The pin fields (ORGADevNode / PCSCReaderName) behave as in Open. The caller
// must Close the Watcher when done.
func OpenWatcher(opts Options) (*Watcher, error) {
	force := opts.Force
	if force == "" {
		d, err := Detect().Pick()
		if err != nil {
			return nil, err
		}
		force = d.Kind
		switch d.Kind {
		case "orga":
			opts.ORGADevNode = d.Device
		case "pcsc":
			opts.PCSCReaderName = d.Device
		}
	}
	switch force {
	case "orga":
		return openORGAWatcher(opts)
	case "generic", "pcsc":
		return openGenericWatcher(opts)
	default:
		return nil, fmt.Errorf("reader: unknown force=%q (valid: orga, generic, pcsc)", force)
	}
}

func openORGAWatcher(opts Options) (*Watcher, error) {
	var picked usb.Device
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
		if probeErr != nil && !errors.Is(probeErr, usb.ErrUnsupported) {
			return nil, fmt.Errorf("reader/orga: USB probe failed: %w", probeErr)
		}
		return nil, fmt.Errorf("reader/orga: no ORGA terminal detected (VID 0x%04X / PID 0x%04X) — check that it's plugged in and not in DFU mode (PID 0xDF55)",
			ORGAUSBVendorID, ORGAUSBProductID)
	}
	t, err := orga.Open(orga.Options{DevNode: devPath, AllowPINWrite: opts.AllowPINWrite})
	if err != nil {
		return nil, err
	}
	return &Watcher{kind: "orga", term: t, info: orgaDeviceInfo(picked, t)}, nil
}

func openGenericWatcher(opts Options) (*Watcher, error) {
	r, err := generic.Open()
	if err != nil {
		return nil, err
	}
	name := opts.PCSCReaderName
	if name != "" {
		matched := ""
		for _, n := range r.Readers() {
			if n == name || containsFold(n, name) {
				matched = n
				break
			}
		}
		if matched == "" {
			_ = r.Close()
			return nil, fmt.Errorf("reader/generic: no PC/SC reader matching %q (have: %v)", name, r.Readers())
		}
		name = matched
	}
	info := DeviceInfo{
		Driver:          "pcsc",
		Product:         name,
		Device:          name,
		SelectionReason: "PC/SC daemon enumerated this reader; no higher-priority ORGA device detected",
	}
	return &Watcher{kind: "pcsc", gr: r, pcscName: name, info: info}, nil
}

// Kind reports the driver backing this watcher ("orga" or "pcsc").
func (w *Watcher) Kind() string { return w.kind }

// Identify returns the descriptor for the watched reader. For PC/SC the
// FirmwareInfo (ATR) is only populated once a card has been acquired.
func (w *Watcher) Identify() DeviceInfo { return w.info }

// Present reports whether a card is currently in the given slot (PC/SC ignores
// slot — it has one card per reader). Cheap; intended to be called on a poll
// interval. An error means the reader became unreachable (unplugged, terminal
// rebooting) — the caller should Close and re-open.
func (w *Watcher) Present(slot int) (bool, error) {
	switch w.kind {
	case "orga":
		st, err := w.term.SlotStatus(slot)
		if err != nil {
			return false, err
		}
		// CT-BCS ICC status: bit0 (0x01) = card present. Higher bits vary by
		// firmware (ORGA 930 M reports 1/3; ORGA 930 care reports 5 = bit0+bit2),
		// so test the present bit rather than exact values. 0 = no card.
		return st&0x01 != 0, nil
	case "pcsc":
		name, present, err := w.gr.Present()
		if err != nil {
			return false, err
		}
		if present {
			w.pcscName = name
		}
		return present, nil
	}
	return false, errors.New("reader: watcher has no driver")
}

// Acquire powers up / connects to the card in the given slot and returns a
// Card ready for egk.Read. Call Release (or Close) when the read is done. For
// PC/SC the slot is ignored. Acquire assumes a card is present (gate it behind
// Present); for PC/SC it re-checks and errors if none is found.
func (w *Watcher) Acquire(slot int) (Card, error) {
	switch w.kind {
	case "orga":
		if _, err := w.term.ActivateSlot(slot); err != nil {
			return nil, fmt.Errorf("reader/orga: activate slot %d: %w", slot, err)
		}
		return w.term.Slot(slot), nil
	case "pcsc":
		name := w.pcscName
		if name == "" {
			n, present, err := w.gr.Present()
			if err != nil {
				return nil, err
			}
			if !present {
				return nil, errors.New("reader/generic: no card present to acquire")
			}
			name = n
			w.pcscName = n
		}
		c, err := w.gr.Connect(name)
		if err != nil {
			return nil, err
		}
		w.card = c
		if atr, err := c.ATR(); err == nil && len(atr) > 0 {
			w.info.FirmwareInfo = fmt.Sprintf("ATR=%X", atr)
		}
		return c, nil
	}
	return nil, errors.New("reader: watcher has no driver")
}

// Release frees the most recently Acquired card while keeping the reader open
// for further polling. For PC/SC it disconnects (leaving the card powered);
// for ORGA it is a no-op (the card stays powered in the slot — the watcher
// owner decides whether to Eject).
func (w *Watcher) Release() {
	if w.card != nil {
		_ = w.card.Close()
		w.card = nil
	}
}

// Close releases any acquired card and the underlying reader handle.
func (w *Watcher) Close() error {
	w.Release()
	switch {
	case w.term != nil:
		return w.term.Close()
	case w.gr != nil:
		return w.gr.Close()
	}
	return nil
}

// orgaDeviceInfo builds the cross-driver DeviceInfo for an ORGA terminal from
// its USB descriptor plus a best-effort CT-BCS GET STATUS firmware summary.
// Shared by orgaSession.Identify and the Watcher.
func orgaDeviceInfo(dev usb.Device, t *orga.Terminal) DeviceInfo {
	info := DeviceInfo{
		Driver:          "orga",
		Manufacturer:    dev.Manufacturer,
		Product:         dev.Product,
		SerialNumber:    dev.Serial,
		Device:          dev.DevicePath,
		VendorID:        ORGAUSBVendorID,
		ProductID:       ORGAUSBProductID,
		SelectionReason: fmt.Sprintf("USB descriptor matched ORGA 9xx family (VID 0x%04X / PID 0x%04X)", ORGAUSBVendorID, ORGAUSBProductID),
	}
	if info.Manufacturer == "" {
		info.Manufacturer = "Ingenico Healthcare GmbH"
	}
	if t != nil {
		if fw, err := t.TerminalInfo(); err == nil {
			info.FirmwareInfo = summarizeTerminalInfo(fw)
			// The terminal self-reports its real model (e.g. "ORGA 930 care") in
			// GET STATUS; prefer it over the USB iProduct, which can read as a
			// generic "ORGA 900 ... RTM" and misidentify the device.
			if model := parseORGAModel(fw); model != "" {
				info.Product = model
			}
		}
	}
	return info
}

// parseORGAModel extracts the friendly terminal model (e.g. "ORGA 930 care")
// from the CT-BCS GET STATUS FH payload. Returns "" if not present. The run is
// taken from "ORGA" up to a control byte or a 2+ space padding boundary.
func parseORGAModel(b []byte) string {
	i := bytes.Index(b, []byte("ORGA"))
	if i < 0 {
		return ""
	}
	end, spaces := i, 0
	for end < len(b) {
		c := b[end]
		if c < 0x20 || c > 0x7e {
			break
		}
		if c == ' ' {
			if spaces++; spaces >= 2 {
				break
			}
		} else {
			spaces = 0
		}
		end++
	}
	return strings.TrimSpace(string(b[i:end]))
}
