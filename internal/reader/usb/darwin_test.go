//go:build darwin

package usb

import (
	"errors"
	"os"
	"testing"
)

const (
	orgaVID = 0x0780
	orgaPID = 0x1202
)

func TestDarwinProbe_LiveFixture_FindsORGA(t *testing.T) {
	out, err := os.ReadFile("testdata/ioreg_with_orga.txt")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	p := &darwinProbe{ioregOutput: func() ([]byte, error) { return out, nil }}
	devs, err := p.FindDevices(orgaVID, orgaPID)
	if err != nil {
		t.Fatalf("FindDevices: %v", err)
	}
	if len(devs) != 1 {
		t.Fatalf("got %d devices, want 1: %+v", len(devs), devs)
	}
	d := devs[0]
	if d.VendorID != orgaVID || d.ProductID != orgaPID {
		t.Errorf("VID/PID = %04X/%04X, want %04X/%04X", d.VendorID, d.ProductID, orgaVID, orgaPID)
	}
	if d.DevicePath != "/dev/cu.usbmodem11301" {
		t.Errorf("DevicePath = %q, want /dev/cu.usbmodem11301", d.DevicePath)
	}
	if d.Manufacturer != "Ingenico Healthcare GmbH" {
		t.Errorf("Manufacturer = %q", d.Manufacturer)
	}
	if d.Product == "" {
		t.Errorf("Product is empty; expected ORGA product string")
	}
	if d.Serial == "" {
		t.Errorf("Serial is empty; expected ORGA serial string")
	}
}

func TestDarwinProbe_NoMatch_ReturnsEmpty(t *testing.T) {
	out, err := os.ReadFile("testdata/ioreg_with_orga.txt")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	p := &darwinProbe{ioregOutput: func() ([]byte, error) { return out, nil }}
	devs, err := p.FindDevices(0xDEAD, 0xBEEF)
	if err != nil {
		t.Fatalf("FindDevices: %v", err)
	}
	if len(devs) != 0 {
		t.Errorf("got %d devices for unknown VID/PID, want 0: %+v", len(devs), devs)
	}
}

func TestDarwinProbe_IORegError_Propagates(t *testing.T) {
	wantErr := errors.New("command not found")
	p := &darwinProbe{ioregOutput: func() ([]byte, error) { return nil, wantErr }}
	_, err := p.FindDevices(orgaVID, orgaPID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err %v doesn't wrap %v", err, wantErr)
	}
}

func TestParseIORegDevices_Empty(t *testing.T) {
	if got := parseIORegDevices("", orgaVID, orgaPID); got != nil {
		t.Errorf("empty input returned %+v, want nil", got)
	}
}

func TestParseIORegDevices_RejectsNonORGAOnSameDevPath(t *testing.T) {
	// Synthetic ioreg block for an Arduino (VID 0x2341 = 9025, PID 0x0043 = 67)
	// with /dev/cu.usbmodem* shape. MUST NOT match ORGA filter.
	fake := `+-o Arduino Uno@01130000  <class IOUSBHostDevice, id 0x1, registered, matched, active, busy 0 (0 ms), retain 30>
  | {
  |   "idProduct" = 67
  |   "idVendor" = 9025
  | }
  | +-o IOSerialBSDClient  <class IOSerialBSDClient, id 0x2, registered, matched, active, busy 0 (0 ms), retain 5>
  |       "IOCalloutDevice" = "/dev/cu.usbmodem99991"
`
	if got := parseIORegDevices(fake, orgaVID, orgaPID); len(got) != 0 {
		t.Errorf("Arduino wrongly matched ORGA filter: %+v", got)
	}
}

func TestParseIORegDevices_MixedDevices_FiltersByVIDPID(t *testing.T) {
	fake := `+-o Arduino Uno@01130000  <class IOUSBHostDevice, id 0x1, registered, matched, active, busy 0 (0 ms), retain 30>
  | {
  |   "idProduct" = 67
  |   "idVendor" = 9025
  | }
  | +-o IOSerialBSDClient  <class IOSerialBSDClient, id 0x2, registered, matched, active, busy 0 (0 ms), retain 5>
  |       "IOCalloutDevice" = "/dev/cu.usbmodem99991"
  |
  +-o ORGA 900@01200000  <class IOUSBHostDevice, id 0x3, registered, matched, active, busy 0 (0 ms), retain 30>
    | {
    |   "idProduct" = 4610
    |   "idVendor" = 1920
    |   "kUSBVendorString" = "Ingenico Healthcare GmbH"
    |   "kUSBProductString" = "ORGA 900"
    |   "kUSBSerialNumberString" = "FAKESERIAL"
    | }
    | +-o IOSerialBSDClient  <class IOSerialBSDClient, id 0x4, registered, matched, active, busy 0 (0 ms), retain 5>
    |       "IOCalloutDevice" = "/dev/cu.usbmodem88881"
`
	got := parseIORegDevices(fake, orgaVID, orgaPID)
	if len(got) != 1 || got[0].DevicePath != "/dev/cu.usbmodem88881" {
		t.Fatalf("got %+v, want exactly one ORGA at /dev/cu.usbmodem88881", got)
	}
	if got[0].Serial != "FAKESERIAL" {
		t.Errorf("Serial = %q, want FAKESERIAL", got[0].Serial)
	}
}

func TestParseIORegDevices_NoDevPath_NotReported(t *testing.T) {
	// ORGA-VID/PID match but no IOSerialBSDClient child — we can't address it,
	// so it must NOT appear in results.
	fake := `+-o ORGA 900@01200000  <class IOUSBHostDevice, id 0x3, registered, matched, active, busy 0 (0 ms), retain 30>
    {
      "idProduct" = 4610
      "idVendor" = 1920
    }
`
	if got := parseIORegDevices(fake, orgaVID, orgaPID); len(got) != 0 {
		t.Errorf("ORGA without device-node wrongly reported: %+v", got)
	}
}

func TestDefaultProbe_IsDarwinProbe(t *testing.T) {
	if _, ok := defaultProbe().(*darwinProbe); !ok {
		t.Errorf("defaultProbe() did not return *darwinProbe on darwin build")
	}
}
