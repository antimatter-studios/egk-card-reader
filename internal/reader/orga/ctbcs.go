package orga

import "fmt"

// CT-BCS terminal-level commands. CLA = 0x20, addressed via NAD high-nibble=1.

// CTBCS sends a CT-BCS APDU (CLA=0x20) to the terminal itself and returns the
// data + SW. The safety guardrail still applies for CT-BCS PERFORM
// VERIFICATION etc.
func (t *Terminal) CTBCS(apdu []byte) ([]byte, error) {
	if err := checkAPDUSafe(apdu, t.allowWrite); err != nil {
		return nil, err
	}
	return t.transactWithNAD(terminalAddr, apdu)
}

// Reset issues CT-BCS RESET CT with no card power-cycle (P1=0x00).
// Returns SW1SW2; no informational data unless P2 != 0 is requested via
// the lower-level CTBCS call.
func (t *Terminal) Reset() error {
	resp, err := t.CTBCS([]byte{0x20, 0x11, 0x00, 0x00, 0x00})
	if err != nil {
		return err
	}
	return swCheck("RESET CT", resp)
}

// ActivateSlot powers up the card in slot n. Returns the ATR if P2=0x01 was
// honored by the terminal; otherwise returns nil with SW=9000.
func (t *Terminal) ActivateSlot(n int) (atr []byte, err error) {
	if n != 1 && n != 2 {
		return nil, fmt.Errorf("orga: invalid slot %d", n)
	}
	resp, err := t.CTBCS([]byte{0x20, 0x12, byte(n), 0x01, 0x00})
	if err != nil {
		return nil, err
	}
	if len(resp) < 2 {
		return nil, fmt.Errorf("orga: REQUEST ICC short response: %X", resp)
	}
	sw := uint16(resp[len(resp)-2])<<8 | uint16(resp[len(resp)-1])
	// 9000     = success, ATR follows (or no data if P2=0)
	// 9001     = "card was already activated"
	// 62xx     = warnings: card present but specific state ("part of returned data may be corrupted", "no card / inactive", etc.) — still accept and return data
	// 63xx     = warnings with retry counter — accept too
	hi := sw >> 8
	if sw != 0x9000 && sw != 0x9001 && hi != 0x62 && hi != 0x63 {
		return nil, fmt.Errorf("orga: REQUEST ICC slot %d: SW=%04X", n, sw)
	}
	return resp[:len(resp)-2], nil
}

// SlotStatus returns the raw terminal status byte for slot n.
// 0 = no card. 1 = card present but inactive. 3 = card present and active.
// (Encoding follows the observed wire format; spec uses bitmasks but the
// terminal returns a single byte under tag 0x80.)
func (t *Terminal) SlotStatus(n int) (byte, error) {
	if n != 1 && n != 2 {
		return 0, fmt.Errorf("orga: invalid slot %d", n)
	}
	resp, err := t.CTBCS([]byte{0x20, 0x13, byte(n), 0x80, 0x00})
	if err != nil {
		return 0, err
	}
	if err := swCheck("GET STATUS", resp); err != nil {
		return 0, err
	}
	body := resp[:len(resp)-2]
	// Expected: tag=80, len=01, value=ss
	if len(body) == 3 && body[0] == 0x80 && body[1] == 0x01 {
		return body[2], nil
	}
	return 0, fmt.Errorf("orga: unexpected GET STATUS body %X", body)
}

// TerminalInfo returns the raw bytes of GET STATUS P1=00 P2=46. The payload
// contains the manufacturer code, hardware/firmware version, RTC, friendly
// product name, and supported card profile names. Parsing is left to the
// caller — the wire format is vendor-defined despite living under a
// standardized CT-BCS call.
func (t *Terminal) TerminalInfo() ([]byte, error) {
	resp, err := t.CTBCS([]byte{0x20, 0x13, 0x00, 0x46, 0x00})
	if err != nil {
		return nil, err
	}
	if err := swCheck("GET STATUS (FH)", resp); err != nil {
		return nil, err
	}
	return resp[:len(resp)-2], nil
}

// Eject powers down the card in slot n.
func (t *Terminal) Eject(n int) error {
	if n != 1 && n != 2 {
		return fmt.Errorf("orga: invalid slot %d", n)
	}
	resp, err := t.CTBCS([]byte{0x20, 0x15, byte(n), 0x00, 0x00})
	if err != nil {
		return err
	}
	return swCheck("EJECT", resp)
}

func swCheck(label string, resp []byte) error {
	if len(resp) < 2 {
		return fmt.Errorf("orga: %s short response: %X", label, resp)
	}
	sw := uint16(resp[len(resp)-2])<<8 | uint16(resp[len(resp)-1])
	if sw == 0x9000 {
		return nil
	}
	return fmt.Errorf("orga: %s SW=%04X", label, sw)
}
