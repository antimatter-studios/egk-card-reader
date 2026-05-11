//go:build linux

// On Linux we read the kernel's sysfs export directly rather than shelling
// out to lsusb(8). lsusb is a thin wrapper around libusb/sysfs and is not
// always installed; sysfs is part of every modern kernel, requires no
// extra dependency, and avoids exec overhead. The file layout we rely on
// has been stable since the 2.6 series.
package usb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type linuxProbe struct {
	// sysfsRoot is the path to enumerate (default /sys/bus/usb/devices).
	// Overridable for tests via a fake directory tree.
	sysfsRoot string
}

func defaultProbe() Probe {
	return &linuxProbe{sysfsRoot: "/sys/bus/usb/devices"}
}

func (p *linuxProbe) FindDevices(vendorID, productID uint16) ([]Device, error) {
	entries, err := os.ReadDir(p.sysfsRoot)
	if err != nil {
		return nil, fmt.Errorf("usb/linux: read %s: %w", p.sysfsRoot, err)
	}
	wantVID := fmt.Sprintf("%04x", vendorID)
	wantPID := fmt.Sprintf("%04x", productID)
	var out []Device
	for _, e := range entries {
		// Sysfs has both device nodes (busnum-portnum, like "1-2") and
		// interface nodes (with a colon, like "1-2:1.0"). Devices are at
		// the top level; skip interface nodes here.
		if strings.Contains(e.Name(), ":") {
			continue
		}
		devDir := filepath.Join(p.sysfsRoot, e.Name())
		if !strings.EqualFold(readSysfsFile(devDir, "idVendor"), wantVID) ||
			!strings.EqualFold(readSysfsFile(devDir, "idProduct"), wantPID) {
			continue
		}
		d := Device{
			VendorID:     vendorID,
			ProductID:    productID,
			Manufacturer: readSysfsFile(devDir, "manufacturer"),
			Product:      readSysfsFile(devDir, "product"),
			Serial:       readSysfsFile(devDir, "serial"),
			DevicePath:   findTTYPath(devDir),
		}
		out = append(out, d)
	}
	return out, nil
}

// readSysfsFile reads <dir>/<name> and returns the trimmed string, or "" on error.
func readSysfsFile(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// findTTYPath looks under each USB interface directory for a "tty" subdir
// containing a single tty node (e.g. ttyACM0 or ttyUSB0) and returns the
// corresponding /dev path. Returns "" if no tty child is bound.
//
// Sysfs layout for a CDC-ACM device:
//
//	/sys/bus/usb/devices/<bus>-<port>/<bus>-<port>:1.0/tty/ttyACM0
func findTTYPath(devDir string) string {
	ifaces, err := os.ReadDir(devDir)
	if err != nil {
		return ""
	}
	for _, e := range ifaces {
		if !strings.Contains(e.Name(), ":") {
			continue
		}
		ttyDir := filepath.Join(devDir, e.Name(), "tty")
		ents, err := os.ReadDir(ttyDir)
		if err != nil || len(ents) == 0 {
			continue
		}
		return "/dev/" + ents[0].Name()
	}
	return ""
}
