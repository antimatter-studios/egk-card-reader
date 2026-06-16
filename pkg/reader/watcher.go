package reader

import (
	"errors"
	"fmt"

	"github.com/christhomas/card-reader/pkg/reader/generic"
	"github.com/christhomas/card-reader/pkg/reader/orga"
	"github.com/christhomas/card-reader/pkg/reader/usb"
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
		// 1 = present-inactive, 3 = present-active; 0 = no card.
		return st == 1 || st == 3, nil
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
		}
	}
	return info
}
