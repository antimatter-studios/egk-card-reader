package reader

import (
	"errors"
	"fmt"

	"github.com/christhomas/card-reader/internal/reader/generic"
	"github.com/christhomas/card-reader/internal/reader/usb"
)

// ORGAUSBVendorID / ORGAUSBProductID identify the ORGA 9xx terminal family
// (covers 900, 920, 930, 930 M). Used by the platform-specific USB-descriptor
// probe to confirm a /dev/cu.usbmodem* is actually an ORGA before claiming it.
//
// VID 0x0780 = Ingenico Healthcare GmbH (ex-Sagem Monetel).
// PID 0x1202 = ORGA 900 Smart Card Terminal Virtual Com Port family.
const (
	ORGAUSBVendorID  = 0x0780
	ORGAUSBProductID = 0x1202
)

// Driver identifies one detected reader by kind and device handle.
// Kind is "orga" or "pcsc". Device is the /dev/cu.usbmodem* path for ORGA,
// or the PC/SC reader name (as reported by the daemon) for PC/SC.
type Driver struct {
	Kind   string
	Device string
}

func (d Driver) String() string {
	if d.Device == "" {
		return d.Kind
	}
	return d.Kind + ":" + d.Device
}

// Probe is a snapshot of the smart-card-reader hardware visible to the host
// at the moment Detect was called. Cheap — queries USB enumeration via the
// usb subpackage and PC/SC reader-list; does NOT open any device or wait
// for card insertion.
type Probe struct {
	ORGADevices []string     // device paths of detected ORGA terminals
	USBDevices  []usb.Device // full USB descriptors parallel to ORGADevices
	PCSCReaders []string     // PC/SC reader names (pcscd's view)
}

// Detect inspects the system for available reader hardware. Always returns
// a non-nil *Probe; check the slices for emptiness or call Empty.
//
// ORGA detection consults the platform-specific usb.Probe (ioreg on macOS,
// sysfs on Linux, stub on other OSes) to enumerate USB devices matching the
// ORGA VID/PID. Unrelated CDC-ACM devices (Arduinos, MCUs, foreign
// terminals) are intentionally NOT reported, so we never ask the ORGA
// driver to talk to something that isn't an ORGA.
//
// USBDevices retains the full descriptor metadata (manufacturer / product /
// serial) for each detected ORGA so callers can present rich identification
// before opening.
func Detect() *Probe {
	p := &Probe{}
	if devs, err := usb.Default().FindDevices(ORGAUSBVendorID, ORGAUSBProductID); err == nil {
		for _, d := range devs {
			p.ORGADevices = append(p.ORGADevices, d.DevicePath)
			p.USBDevices = append(p.USBDevices, d)
		}
	}
	if r, err := generic.Open(); err == nil {
		p.PCSCReaders = r.Readers()
		_ = r.Close()
	}
	return p
}

// Empty reports whether no readers were detected.
func (p *Probe) Empty() bool {
	return len(p.ORGADevices) == 0 && len(p.PCSCReaders) == 0
}

// Drivers returns one Driver per detected reader, in pick-priority order
// (ORGA first, then PC/SC). Useful for showing the user what's available
// before deciding.
func (p *Probe) Drivers() []Driver {
	out := make([]Driver, 0, len(p.ORGADevices)+len(p.PCSCReaders))
	for _, d := range p.ORGADevices {
		out = append(out, Driver{Kind: "orga", Device: d})
	}
	for _, n := range p.PCSCReaders {
		out = append(out, Driver{Kind: "pcsc", Device: n})
	}
	return out
}

// Pick returns the highest-priority Driver from this probe (ORGA > PC/SC).
// Returns an error when Empty.
func (p *Probe) Pick() (Driver, error) {
	if len(p.ORGADevices) > 0 {
		return Driver{Kind: "orga", Device: p.ORGADevices[0]}, nil
	}
	if len(p.PCSCReaders) > 0 {
		return Driver{Kind: "pcsc", Device: p.PCSCReaders[0]}, nil
	}
	return Driver{}, errors.New("reader: no card readers detected (no /dev/cu.usbmodem* device, no PC/SC readers)")
}

// OpenDriver opens the session for a specific Driver returned by Probe.
// Use this when you've already chosen a driver (e.g. from Probe.Drivers());
// for auto-detection use Open instead.
func OpenDriver(d Driver, opts Options) (Session, error) {
	switch d.Kind {
	case "orga":
		opts.Force = "orga"
		opts.ORGADevNode = d.Device
	case "pcsc", "generic":
		opts.Force = "generic"
		opts.PCSCReaderName = d.Device
	default:
		return nil, fmt.Errorf("reader: unknown driver kind %q", d.Kind)
	}
	return Open(opts)
}
