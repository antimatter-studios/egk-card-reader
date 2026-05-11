//go:build linux

package usb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxProbe_FakeSysfsTree(t *testing.T) {
	// Construct a minimal sysfs-shaped tree in t.TempDir() with two USB
	// devices: an Arduino and an ORGA. Only the ORGA should match.
	root := t.TempDir()

	mkDev := func(name, vid, pid, manuf, prod, serial, ttyName string) {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		write := func(fname, content string) {
			if err := os.WriteFile(filepath.Join(dir, fname), []byte(content+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("idVendor", vid)
		write("idProduct", pid)
		write("manufacturer", manuf)
		write("product", prod)
		write("serial", serial)
		if ttyName != "" {
			ttyDir := filepath.Join(dir, name+":1.0", "tty", ttyName)
			if err := os.MkdirAll(ttyDir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}

	mkDev("1-1", "2341", "0043", "Arduino LLC", "Uno", "ARDUINO-001", "ttyACM1")
	mkDev("1-2", "0780", "1202", "Ingenico Healthcare GmbH", "ORGA 900", "ORGA-FAKE-001", "ttyACM0")

	p := &linuxProbe{sysfsRoot: root}
	devs, err := p.FindDevices(0x0780, 0x1202)
	if err != nil {
		t.Fatalf("FindDevices: %v", err)
	}
	if len(devs) != 1 {
		t.Fatalf("got %d devices, want 1: %+v", len(devs), devs)
	}
	d := devs[0]
	if d.VendorID != 0x0780 || d.ProductID != 0x1202 {
		t.Errorf("VID/PID = %04X/%04X", d.VendorID, d.ProductID)
	}
	if d.DevicePath != "/dev/ttyACM0" {
		t.Errorf("DevicePath = %q, want /dev/ttyACM0", d.DevicePath)
	}
	if d.Manufacturer != "Ingenico Healthcare GmbH" {
		t.Errorf("Manufacturer = %q", d.Manufacturer)
	}
	if d.Product != "ORGA 900" {
		t.Errorf("Product = %q", d.Product)
	}
	if d.Serial != "ORGA-FAKE-001" {
		t.Errorf("Serial = %q", d.Serial)
	}
}

func TestLinuxProbe_NonexistentRoot_ReturnsError(t *testing.T) {
	p := &linuxProbe{sysfsRoot: "/does/not/exist"}
	_, err := p.FindDevices(0x0780, 0x1202)
	if err == nil {
		t.Fatal("expected error for missing sysfs root")
	}
}

func TestLinuxProbe_NoMatch_ReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "1-1")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "idVendor"), []byte("2341\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "idProduct"), []byte("0043\n"), 0o644)

	p := &linuxProbe{sysfsRoot: root}
	devs, err := p.FindDevices(0x0780, 0x1202)
	if err != nil {
		t.Fatalf("FindDevices: %v", err)
	}
	if len(devs) != 0 {
		t.Errorf("got %d devices, want 0", len(devs))
	}
}

func TestLinuxProbe_SkipsInterfaceNodes(t *testing.T) {
	// Sysfs has both device nodes (e.g. "1-1") and interface nodes
	// (e.g. "1-1:1.0"). The probe must skip the latter at the top level.
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "1-1:1.0"), 0o755)
	// (no idVendor file in the interface node, which would cause read errors
	// if the probe didn't skip these)
	p := &linuxProbe{sysfsRoot: root}
	if _, err := p.FindDevices(0x0780, 0x1202); err != nil {
		t.Fatalf("FindDevices on root with only interface nodes: %v", err)
	}
}

func TestDefaultProbe_IsLinuxProbe(t *testing.T) {
	if _, ok := defaultProbe().(*linuxProbe); !ok {
		t.Errorf("defaultProbe() did not return *linuxProbe on linux build")
	}
}
