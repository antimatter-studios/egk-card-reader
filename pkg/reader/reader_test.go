package reader

import (
	"errors"
	"strings"
	"testing"

	"github.com/antimatter-studios/egk-card-reader/pkg/reader/usb"
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

func TestContainsFold(t *testing.T) {
	cases := []struct {
		hay, needle string
		want        bool
	}{
		{"Cherry Smart Terminal ST-2xxx 00 00", "cherry", true},
		{"Cherry Smart Terminal ST-2xxx 00 00", "CHERRY", true},
		{"Cherry Smart Terminal ST-2xxx 00 00", "ST-2xxx", true},
		{"Cherry Smart Terminal ST-2xxx 00 00", "st-2XXX", true},
		{"Cherry Smart Terminal ST-2xxx 00 00", "OMNIKEY", false},
		{"short", "muchlongerneedle", false},
		{"", "", true},
		{"abc", "", true},
		{"abc", "abc", true},
		{"abc", "abcd", false},
		{"AbCdEf", "cde", true},
	}
	for _, c := range cases {
		got := containsFold(c.hay, c.needle)
		if got != c.want {
			t.Errorf("containsFold(%q,%q)=%v, want %v", c.hay, c.needle, got, c.want)
		}
	}
}

func TestDriver_String(t *testing.T) {
	cases := []struct {
		d    Driver
		want string
	}{
		{Driver{Kind: "orga", Device: "/dev/cu.usbmodem11301"}, "orga:/dev/cu.usbmodem11301"},
		{Driver{Kind: "pcsc", Device: "Cherry ST-2100"}, "pcsc:Cherry ST-2100"},
		{Driver{Kind: "orga"}, "orga"},
		{Driver{}, ""},
	}
	for _, c := range cases {
		if got := c.d.String(); got != c.want {
			t.Errorf("Driver%+v.String()=%q want %q", c.d, got, c.want)
		}
	}
}

func TestProbe_Empty(t *testing.T) {
	if !(&Probe{}).Empty() {
		t.Errorf("zero Probe should be empty")
	}
	if (&Probe{ORGADevices: []string{"/dev/x"}}).Empty() {
		t.Errorf("probe with ORGA device should not be empty")
	}
	if (&Probe{PCSCReaders: []string{"R1"}}).Empty() {
		t.Errorf("probe with PC/SC reader should not be empty")
	}
	if (&Probe{ORGADevices: []string{"/dev/x"}, PCSCReaders: []string{"R1"}}).Empty() {
		t.Errorf("probe with both should not be empty")
	}
}

func TestProbe_Drivers(t *testing.T) {
	p := &Probe{
		ORGADevices: []string{"/dev/cu.usbmodem11301", "/dev/cu.usbmodem11401"},
		PCSCReaders: []string{"Cherry ST-2100", "OMNIKEY 3121"},
	}
	got := p.Drivers()
	want := []Driver{
		{Kind: "orga", Device: "/dev/cu.usbmodem11301"},
		{Kind: "orga", Device: "/dev/cu.usbmodem11401"},
		{Kind: "pcsc", Device: "Cherry ST-2100"},
		{Kind: "pcsc", Device: "OMNIKEY 3121"},
	}
	if len(got) != len(want) {
		t.Fatalf("Drivers() len=%d want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Drivers()[%d]=%v want %v", i, got[i], want[i])
		}
	}
	if drivers := (&Probe{}).Drivers(); len(drivers) != 0 {
		t.Errorf("empty Probe.Drivers() should be empty, got %v", drivers)
	}
}

func TestProbe_Pick(t *testing.T) {
	// ORGA wins over PC/SC.
	p := &Probe{
		ORGADevices: []string{"/dev/cu.usbmodem11301"},
		PCSCReaders: []string{"Cherry ST-2100"},
	}
	d, err := p.Pick()
	if err != nil {
		t.Fatalf("Pick() err=%v", err)
	}
	if d.Kind != "orga" || d.Device != "/dev/cu.usbmodem11301" {
		t.Errorf("Pick() prioritized wrong driver: %+v", d)
	}

	// PC/SC when no ORGA.
	p = &Probe{PCSCReaders: []string{"Cherry ST-2100", "OMNIKEY 3121"}}
	d, err = p.Pick()
	if err != nil {
		t.Fatalf("Pick() err=%v", err)
	}
	if d.Kind != "pcsc" || d.Device != "Cherry ST-2100" {
		t.Errorf("Pick() picked wrong PC/SC reader: %+v", d)
	}

	// Empty.
	if _, err := (&Probe{}).Pick(); err == nil {
		t.Errorf("empty Pick() should error")
	}
}

func TestDetect_AlwaysReturnsNonNil(t *testing.T) {
	// Detect() consults real OS USB enumeration and PC/SC daemon. On CI
	// neither is expected; we only assert it doesn't crash and returns a
	// non-nil pointer with consistent slice lengths.
	p := Detect()
	if p == nil {
		t.Fatalf("Detect() returned nil")
	}
	if len(p.ORGADevices) != len(p.USBDevices) {
		t.Errorf("ORGADevices/USBDevices length mismatch: %d vs %d", len(p.ORGADevices), len(p.USBDevices))
	}
}

func TestOpen_InvalidForce(t *testing.T) {
	_, err := Open(Options{Force: "bogus"})
	if err == nil {
		t.Fatalf("Open(Force=bogus) should error")
	}
	if !strings.Contains(err.Error(), "unknown force") {
		t.Errorf("expected 'unknown force' in error, got: %v", err)
	}
}

func TestOpen_ForceORGA_NoHardware(t *testing.T) {
	// No real ORGA plugged in (CI) — openORGA should refuse rather than
	// guess at a stale /dev node. We accept either the "no ORGA detected"
	// path or, if USB probing is unsupported on this OS, a probe error.
	_, err := Open(Options{Force: "orga", ORGADevNode: "/dev/cu.does-not-exist"})
	if err == nil {
		t.Fatalf("Open(Force=orga, bogus dev) should error in no-hw environment")
	}
	// The error must come from reader/orga, not from "unknown force".
	if strings.Contains(err.Error(), "unknown force") {
		t.Errorf("unexpected unknown-force error: %v", err)
	}
}

func TestOpen_ForceGeneric_NoReaders(t *testing.T) {
	// In an environment with no PC/SC reader (or no pcscd), openGeneric
	// should propagate the underlying generic.Open / pickReader error.
	// We don't assert a specific error message because it varies between
	// macOS (no readers) and CI (no pcscd / wrapper missing).
	_, err := Open(Options{Force: "generic"})
	if err == nil {
		t.Logf("Open(Force=generic) succeeded — a live PC/SC reader appears attached; skipping negative assertion")
		return
	}
	// Make sure we don't mis-route as "unknown force".
	if strings.Contains(err.Error(), "unknown force") {
		t.Errorf("generic force was mis-routed as unknown: %v", err)
	}
}

func TestOpen_ForcePCSCAlias(t *testing.T) {
	// "pcsc" must be accepted as a synonym for "generic" — i.e. it should
	// NOT yield an unknown-force error. The actual outcome depends on
	// whether a PC/SC reader is attached.
	_, err := Open(Options{Force: "pcsc"})
	if err != nil && strings.Contains(err.Error(), "unknown force") {
		t.Errorf("'pcsc' alias rejected as unknown force: %v", err)
	}
}

func TestOpenDriver_UnknownKind(t *testing.T) {
	_, err := OpenDriver(Driver{Kind: "nfc", Device: "x"}, Options{})
	if err == nil {
		t.Fatalf("OpenDriver(nfc) should error")
	}
	if !strings.Contains(err.Error(), "unknown driver kind") {
		t.Errorf("expected 'unknown driver kind', got: %v", err)
	}
}

func TestOpenDriver_RoutesByKind(t *testing.T) {
	// We can't open real hardware in tests, but we can verify that
	// OpenDriver chooses the correct sub-factory by checking that an
	// unknown-force error never appears for known kinds.
	for _, kind := range []string{"orga", "pcsc", "generic"} {
		_, err := OpenDriver(Driver{Kind: kind, Device: "x"}, Options{})
		if err == nil {
			continue // hardware happened to be attached
		}
		if strings.Contains(err.Error(), "unknown force") {
			t.Errorf("OpenDriver(%q) mis-routed: %v", kind, err)
		}
	}
}

func TestOpen_AutoDetect_NoHardware(t *testing.T) {
	// On a host with neither ORGA nor any PC/SC reader, the auto-detect
	// path should surface Detect().Pick()'s "no card readers detected"
	// error. If hardware is present, we just skip the assertion.
	if !Detect().Empty() {
		t.Skip("hardware attached; cannot test empty auto-detect path")
	}
	_, err := Open(Options{})
	if err == nil {
		t.Fatalf("Open() with empty Force and no hardware should error")
	}
	if !strings.Contains(err.Error(), "no card readers detected") {
		t.Errorf("expected 'no card readers detected', got: %v", err)
	}
}

func TestOrgaSession_SlotRejectsOutOfRange(t *testing.T) {
	// Slot() validates n before touching s.t, so nil terminal is safe here.
	s := &orgaSession{}
	for _, n := range []int{0, 3, -1, 99} {
		_, err := s.Slot(n)
		if err == nil {
			t.Errorf("Slot(%d) should error", n)
			continue
		}
		if !errors.Is(err, ErrNoSlot) {
			t.Errorf("Slot(%d) err not ErrNoSlot: %v", n, err)
		}
	}
}

func TestOrgaSession_Kind(t *testing.T) {
	s := &orgaSession{}
	if got := s.Kind(); got != "orga" {
		t.Errorf("Kind()=%q want orga", got)
	}
}

func TestOrgaSession_Terminal_NilWhenUnset(t *testing.T) {
	// Terminal() is a trivial accessor; covers the getter.
	s := &orgaSession{}
	if s.Terminal() != nil {
		t.Errorf("Terminal() on zero session should be nil")
	}
}

func TestGenericSession_SlotRejectsOutOfRange(t *testing.T) {
	// genericSession.Slot validates n before touching s.c.
	s := &genericSession{name: "test"}
	for _, n := range []int{0, 2, -1, 99} {
		_, err := s.Slot(n)
		if err == nil {
			t.Errorf("Slot(%d) should error", n)
			continue
		}
		if !errors.Is(err, ErrNoSlot) {
			t.Errorf("Slot(%d) err not ErrNoSlot: %v", n, err)
		}
	}
}

func TestGenericSession_Kind(t *testing.T) {
	s := &genericSession{name: "Cherry ST-2100"}
	if got, want := s.Kind(), "pcsc:Cherry ST-2100"; got != want {
		t.Errorf("Kind()=%q want %q", got, want)
	}
}

// Compile-time sanity: usb.Device is the field type on orgaSession; making
// sure the test file's import is used and the types line up with what
// production code expects.
var _ = usb.Device{}
