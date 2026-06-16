package orga

import (
	"strings"
	"testing"
)

func TestCheckAPDUSafe_OverrideAllowsAll(t *testing.T) {
	for ins := range dangerousISO {
		apdu := []byte{0x00, ins, 0x00, 0x00}
		if err := checkAPDUSafe(apdu, true); err != nil {
			t.Errorf("override should allow INS=%02X, got %v", ins, err)
		}
	}
	for ins := range dangerousCTBCS {
		apdu := []byte{0x20, ins, 0x00, 0x00}
		if err := checkAPDUSafe(apdu, true); err != nil {
			t.Errorf("override should allow CT-BCS INS=%02X, got %v", ins, err)
		}
	}
}

func TestCheckAPDUSafe_ShortAPDUAccepted(t *testing.T) {
	for _, apdu := range [][]byte{nil, {}, {0x00}} {
		if err := checkAPDUSafe(apdu, false); err != nil {
			t.Errorf("short APDU rejected: %v", err)
		}
	}
}

func TestCheckAPDUSafe_DangerousISORefused(t *testing.T) {
	for ins, reason := range dangerousISO {
		apdu := []byte{0x00, ins, 0x00, 0x00}
		err := checkAPDUSafe(apdu, false)
		if err == nil {
			t.Errorf("INS=%02X should be refused", ins)
			continue
		}
		de, ok := err.(*ErrDangerousAPDU)
		if !ok {
			t.Errorf("INS=%02X err type %T; want *ErrDangerousAPDU", ins, err)
			continue
		}
		if de.INS != ins {
			t.Errorf("INS field=%02X; want %02X", de.INS, ins)
		}
		if !strings.Contains(de.Error(), reason) {
			t.Errorf("Error()=%q missing reason %q", de.Error(), reason)
		}
		if !strings.Contains(de.Error(), "AllowPINWrite") {
			t.Errorf("Error() missing AllowPINWrite hint: %q", de.Error())
		}
	}
}

func TestCheckAPDUSafe_DangerousCTBCSRefused(t *testing.T) {
	for ins := range dangerousCTBCS {
		apdu := []byte{0x20, ins, 0x00, 0x00}
		err := checkAPDUSafe(apdu, false)
		if err == nil {
			t.Errorf("CT-BCS INS=%02X should be refused", ins)
		}
	}
}

func TestCheckAPDUSafe_CTBCS_SafeINSAccepted(t *testing.T) {
	// CT-BCS RESET (0x11), REQUEST ICC (0x12), GET STATUS (0x13), EJECT (0x15)
	for _, ins := range []byte{0x11, 0x12, 0x13, 0x15} {
		apdu := []byte{0x20, ins, 0x00, 0x00}
		if err := checkAPDUSafe(apdu, false); err != nil {
			t.Errorf("CT-BCS INS=%02X should be accepted, got %v", ins, err)
		}
	}
}

func TestCheckAPDUSafe_ISO_SafeINSAccepted(t *testing.T) {
	// SELECT (0xA4), READ BINARY (0xB0), READ RECORD (0xB2)
	for _, ins := range []byte{0xA4, 0xB0, 0xB2} {
		apdu := []byte{0x00, ins, 0x00, 0x00}
		if err := checkAPDUSafe(apdu, false); err != nil {
			t.Errorf("INS=%02X should be accepted, got %v", ins, err)
		}
	}
}

func TestErrDangerousAPDU_Error(t *testing.T) {
	e := &ErrDangerousAPDU{INS: 0x20, Reason: "VERIFY foo"}
	msg := e.Error()
	for _, want := range []string{"INS=0x20", "VERIFY foo", "AllowPINWrite"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error()=%q missing %q", msg, want)
		}
	}
}
