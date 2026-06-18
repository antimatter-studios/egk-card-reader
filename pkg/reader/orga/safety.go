package orga

import "fmt"

// dangerousISO maps INS bytes that mutate card state or decrement counters
// when the CLA is any non-CT-BCS value.
var dangerousISO = map[byte]string{
	0x20: "VERIFY (decrements PIN retry counter on failure — 3 wrong tries blocks the card)",
	0x24: "CHANGE REFERENCE DATA (changes PIN)",
	0x26: "DISABLE VERIFICATION REQUIREMENT",
	0x28: "ENABLE VERIFICATION REQUIREMENT",
	0x2C: "RESET RETRY COUNTER (needs PUK; wrong PUK decrements PUK counter)",
	0xD6: "UPDATE BINARY (writes to EF data)",
	0xDC: "UPDATE RECORD (writes to record)",
	0xDA: "PUT DATA (writes data object)",
	0xE0: "ERASE BINARY",
	0x0E: "ERASE BINARY (alternative encoding)",
	0xEE: "ERASE RECORD",
}

// dangerousCTBCS maps INS bytes that drive the terminal keypad — they can be
// used to collect or verify a PIN, which decrements the card's retry counter.
var dangerousCTBCS = map[byte]string{
	0x16: "CT-BCS INPUT (terminal keypad)",
	0x18: "CT-BCS PERFORM VERIFICATION (secure PIN entry → VERIFY on card)",
	0x19: "CT-BCS MODIFY VERIFICATION DATA (PIN change via keypad)",
}

// ErrDangerousAPDU is returned by Transmit / CTBCS when the APDU is on the
// dangerous-INS list and the Terminal was opened without Options.AllowPINWrite.
type ErrDangerousAPDU struct {
	INS    byte
	Reason string
}

func (e *ErrDangerousAPDU) Error() string {
	return fmt.Sprintf("orga: REFUSED INS=0x%02X — %s. Set Options.AllowPINWrite=true to override", e.INS, e.Reason)
}

func checkAPDUSafe(apdu []byte, override bool) error {
	if override || len(apdu) < 2 {
		return nil
	}
	cla, ins := apdu[0], apdu[1]
	if cla == 0x20 {
		if reason, bad := dangerousCTBCS[ins]; bad {
			return &ErrDangerousAPDU{INS: ins, Reason: reason}
		}
		return nil
	}
	if reason, bad := dangerousISO[ins]; bad {
		return &ErrDangerousAPDU{INS: ins, Reason: reason}
	}
	return nil
}
