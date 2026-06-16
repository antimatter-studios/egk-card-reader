// Package usb abstracts USB device discovery across operating systems.
//
// Each OS has its own enumeration interface:
//
//   - macOS: ioreg(8) / IOKit
//   - Linux: /sys/bus/usb/devices (sysfs)
//   - Windows: SetupAPI (SetupDiGetClassDevs) / WMI
//
// The build-tagged files in this package provide one Probe implementation
// per supported OS, selected at compile time. Callers obtain the right
// implementation by calling Default(), which never returns nil — on
// unsupported platforms it returns a Probe whose FindDevices fails with
// ErrUnsupported, so consumers fail loudly rather than silently report
// "no devices".
//
// Package consumers should treat this as the single source of truth for
// "what USB hardware is plugged in" — no other code should glob /dev or
// shell out to platform-specific tools directly.
package usb

import "errors"

// Device describes one USB device discovered by Probe.FindDevices. All
// string fields are best-effort: the underlying OS may not expose them,
// in which case they are empty.
type Device struct {
	// VendorID, ProductID are the USB descriptor's idVendor / idProduct
	// values. Always populated for a match.
	VendorID  uint16
	ProductID uint16

	// Manufacturer / Product / Serial are the USB string descriptors
	// (iManufacturer / iProduct / iSerial). Best-effort: may be empty.
	Manufacturer string
	Product      string
	Serial       string

	// DevicePath is the OS device node the host uses to communicate with
	// this device's primary serial/CDC interface, e.g. /dev/cu.usbmodem11301
	// on macOS, /dev/ttyACM0 on Linux, "COM3" on Windows. Empty if no
	// serial interface is bound.
	DevicePath string
}

// Probe enumerates USB devices visible to the host. Implementations are
// per-OS; consumers should call Default() to get the appropriate one.
type Probe interface {
	// FindDevices returns all USB devices whose vendor and product IDs
	// match the given values. Returns ErrUnsupported (wrapped) if the
	// current OS has no enumeration implementation, or another error if
	// the OS-specific probe couldn't run (missing tool, permission
	// denied, etc.).
	FindDevices(vendorID, productID uint16) ([]Device, error)
}

// ErrUnsupported signals that USB enumeration is not yet implemented for
// the build's target OS. Wrap-aware: use errors.Is(err, usb.ErrUnsupported).
var ErrUnsupported = errors.New("usb: hardware enumeration not supported on this platform")

// Default returns the Probe implementation for the OS this binary was
// built for. Never returns nil.
func Default() Probe { return defaultProbe() }
