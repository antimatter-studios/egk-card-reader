package reader

import (
	"strings"
	"testing"
)

func TestDeviceInfo_StringOmitsEmptyFields(t *testing.T) {
	d := DeviceInfo{
		Driver:          "orga",
		Manufacturer:    "Ingenico Healthcare GmbH",
		Product:         "ORGA 900",
		Device:          "/dev/cu.usbmodem11301",
		VendorID:        0x0780,
		ProductID:       0x1202,
		SelectionReason: "VID/PID match",
	}
	out := d.String()
	for _, want := range []string{"Driver", "orga", "Ingenico Healthcare GmbH", "ORGA 900", "/dev/cu.usbmodem11301", "0x0780", "0x1202", "VID/PID match"} {
		if !strings.Contains(out, want) {
			t.Errorf("DeviceInfo.String() missing %q\ngot:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Serial:") {
		t.Errorf("DeviceInfo.String() included empty Serial field:\n%s", out)
	}
	if strings.Contains(out, "Firmware:") {
		t.Errorf("DeviceInfo.String() included empty Firmware field:\n%s", out)
	}
}

func TestDeviceInfo_StringIncludesAllSetFields(t *testing.T) {
	d := DeviceInfo{
		Driver:          "orga",
		Manufacturer:    "M",
		Product:         "P",
		SerialNumber:    "SN",
		Device:          "/dev/x",
		VendorID:        1,
		ProductID:       2,
		FirmwareInfo:    "FW",
		SelectionReason: "R",
	}
	out := d.String()
	for _, want := range []string{"Driver:", "Manufacturer:", "Product:", "Serial:", "Device:", "USB VID/PID:", "Firmware:", "Reason:"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing label %q:\n%s", want, out)
		}
	}
}

func TestSummarizeTerminalInfo_ExtractsFHDETag(t *testing.T) {
	// Real ORGA 930 care GET STATUS P2=46 reply starts with "FHDEORGMCT93V5.03 7.05".
	in := []byte("\xe0\x00FHDEORGMCT93V5.03 7.05\x00\x00\x00\x00\x00\x00trailing junk")
	got := summarizeTerminalInfo(in)
	if got != "FHDEORGMCT93V5.03 7.05" {
		t.Errorf("got %q, want %q", got, "FHDEORGMCT93V5.03 7.05")
	}
}

func TestSummarizeTerminalInfo_NoMatchReturnsEmpty(t *testing.T) {
	if got := summarizeTerminalInfo([]byte("no fhde tag here")); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
