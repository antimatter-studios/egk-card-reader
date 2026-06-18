package orga

import (
	"bytes"
	"testing"
	"time"
)

// ctbcsExchange builds the host→terminal request block + the canned response
// for a CT-BCS command (NAD high nibble = terminal=1, host=2 → outNAD=0x12).
func ctbcsExchange(t *testing.T, apdu []byte, respINF []byte, ns byte) (req, resp []byte) {
	t.Helper()
	out := byte(0x12)
	in := byte(0x21)
	req = buildBlock(out, ns<<6, apdu)
	resp = buildBlock(in, ns<<6, respINF)
	return req, resp
}

func newTermCTBCS(fake *fakeSerialIO) *Terminal {
	return &Terminal{io: fake, ns: map[byte]byte{}, timeout: 200 * time.Millisecond}
}

func TestCTBCS_DangerousAPDURefused(t *testing.T) {
	fake := newFakeSerialIO(t)
	term := newTermCTBCS(fake)
	// CT-BCS PERFORM VERIFICATION (CLA=20 INS=18) → dangerous.
	_, err := term.CTBCS([]byte{0x20, 0x18, 0x00, 0x00})
	if err == nil {
		t.Fatal("expected refusal")
	}
	if _, ok := err.(*ErrDangerousAPDU); !ok {
		t.Errorf("err type %T; want *ErrDangerousAPDU", err)
	}
}

func TestCTBCS_AllowWriteSendsDangerous(t *testing.T) {
	apdu := []byte{0x20, 0x18, 0x00, 0x00}
	req, resp := ctbcsExchange(t, apdu, []byte{0x90, 0x00}, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	term.allowWrite = true
	got, err := term.CTBCS(apdu)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !bytes.Equal(got, []byte{0x90, 0x00}) {
		t.Errorf("resp=%X", got)
	}
}

func TestReset_Success(t *testing.T) {
	apdu := []byte{0x20, 0x11, 0x00, 0x00, 0x00}
	req, resp := ctbcsExchange(t, apdu, []byte{0x90, 0x00}, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	if err := term.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	fake.assertDrained()
}

func TestReset_SWFailure(t *testing.T) {
	apdu := []byte{0x20, 0x11, 0x00, 0x00, 0x00}
	req, resp := ctbcsExchange(t, apdu, []byte{0x6A, 0x80}, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	if err := term.Reset(); err == nil || !contains(err.Error(), "RESET CT SW=6A80") {
		t.Errorf("got %v; want RESET CT SW=6A80", err)
	}
}

func TestActivateSlot_BadSlot(t *testing.T) {
	fake := newFakeSerialIO(t)
	term := newTermCTBCS(fake)
	if _, err := term.ActivateSlot(3); err == nil {
		t.Error("invalid slot accepted")
	}
}

func TestActivateSlot_SuccessWithATR(t *testing.T) {
	apdu := []byte{0x20, 0x12, 0x01, 0x01, 0x00}
	atr := []byte{0x3B, 0xD3, 0x96, 0xFF, 0x81, 0xB1, 0xFE, 0x45, 0x1F, 0x07}
	respINF := append([]byte{}, atr...)
	respINF = append(respINF, 0x90, 0x00)
	req, resp := ctbcsExchange(t, apdu, respINF, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	got, err := term.ActivateSlot(1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !bytes.Equal(got, atr) {
		t.Errorf("ATR=%X want %X", got, atr)
	}
}

func TestActivateSlot_9001AlreadyActive(t *testing.T) {
	apdu := []byte{0x20, 0x12, 0x02, 0x01, 0x00}
	req, resp := ctbcsExchange(t, apdu, []byte{0x90, 0x01}, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	got, err := term.ActivateSlot(2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty ATR, got %X", got)
	}
}

func TestActivateSlot_62Warning(t *testing.T) {
	apdu := []byte{0x20, 0x12, 0x01, 0x01, 0x00}
	req, resp := ctbcsExchange(t, apdu, []byte{0x62, 0x81}, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	if _, err := term.ActivateSlot(1); err != nil {
		t.Errorf("62xx should be tolerated: %v", err)
	}
}

func TestActivateSlot_63Warning(t *testing.T) {
	apdu := []byte{0x20, 0x12, 0x01, 0x01, 0x00}
	req, resp := ctbcsExchange(t, apdu, []byte{0x63, 0xC3}, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	if _, err := term.ActivateSlot(1); err != nil {
		t.Errorf("63xx should be tolerated: %v", err)
	}
}

func TestActivateSlot_HardFailure(t *testing.T) {
	apdu := []byte{0x20, 0x12, 0x01, 0x01, 0x00}
	req, resp := ctbcsExchange(t, apdu, []byte{0x64, 0x00}, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	if _, err := term.ActivateSlot(1); err == nil || !contains(err.Error(), "SW=6400") {
		t.Errorf("got %v; want SW=6400", err)
	}
}

func TestActivateSlot_ShortResponse(t *testing.T) {
	apdu := []byte{0x20, 0x12, 0x01, 0x01, 0x00}
	req, resp := ctbcsExchange(t, apdu, []byte{0x90}, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	if _, err := term.ActivateSlot(1); err == nil || !contains(err.Error(), "short response") {
		t.Errorf("got %v; want short response", err)
	}
}

func TestSlotStatus_BadSlot(t *testing.T) {
	term := newTermCTBCS(newFakeSerialIO(t))
	if _, err := term.SlotStatus(0); err == nil {
		t.Error("invalid slot accepted")
	}
}

func TestSlotStatus_OK(t *testing.T) {
	apdu := []byte{0x20, 0x13, 0x01, 0x80, 0x00}
	// Body: tag=80 len=01 val=03 (card present and active), then SW 9000.
	respINF := []byte{0x80, 0x01, 0x03, 0x90, 0x00}
	req, resp := ctbcsExchange(t, apdu, respINF, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	st, err := term.SlotStatus(1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if st != 0x03 {
		t.Errorf("status=%02X; want 03", st)
	}
}

func TestSlotStatus_SWNot9000(t *testing.T) {
	apdu := []byte{0x20, 0x13, 0x01, 0x80, 0x00}
	respINF := []byte{0x80, 0x01, 0x00, 0x6A, 0x86}
	req, resp := ctbcsExchange(t, apdu, respINF, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	if _, err := term.SlotStatus(1); err == nil || !contains(err.Error(), "SW=6A86") {
		t.Errorf("got %v; want SW=6A86", err)
	}
}

func TestSlotStatus_UnexpectedBody(t *testing.T) {
	apdu := []byte{0x20, 0x13, 0x01, 0x80, 0x00}
	respINF := []byte{0x81, 0x01, 0x03, 0x90, 0x00}
	req, resp := ctbcsExchange(t, apdu, respINF, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	if _, err := term.SlotStatus(1); err == nil || !contains(err.Error(), "unexpected GET STATUS body") {
		t.Errorf("got %v; want unexpected body", err)
	}
}

func TestTerminalInfo_Success(t *testing.T) {
	apdu := []byte{0x20, 0x13, 0x00, 0x46, 0x00}
	body := []byte{0x46, 0x05, 'O', 'R', 'G', 'A', '!'}
	respINF := append([]byte{}, body...)
	respINF = append(respINF, 0x90, 0x00)
	req, resp := ctbcsExchange(t, apdu, respINF, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	got, err := term.TerminalInfo()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("got %X; want %X", got, body)
	}
}

func TestTerminalInfo_SWFail(t *testing.T) {
	apdu := []byte{0x20, 0x13, 0x00, 0x46, 0x00}
	req, resp := ctbcsExchange(t, apdu, []byte{0x6D, 0x00}, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	if _, err := term.TerminalInfo(); err == nil || !contains(err.Error(), "SW=6D00") {
		t.Errorf("got %v", err)
	}
}

func TestEject_BadSlot(t *testing.T) {
	term := newTermCTBCS(newFakeSerialIO(t))
	if err := term.Eject(5); err == nil {
		t.Error("bad slot accepted")
	}
}

func TestEject_Success(t *testing.T) {
	apdu := []byte{0x20, 0x15, 0x02, 0x00, 0x00}
	req, resp := ctbcsExchange(t, apdu, []byte{0x90, 0x00}, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	if err := term.Eject(2); err != nil {
		t.Fatalf("eject: %v", err)
	}
}

func TestEject_SWFail(t *testing.T) {
	apdu := []byte{0x20, 0x15, 0x01, 0x00, 0x00}
	req, resp := ctbcsExchange(t, apdu, []byte{0x6A, 0x00}, 0)
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermCTBCS(fake)
	if err := term.Eject(1); err == nil || !contains(err.Error(), "SW=6A00") {
		t.Errorf("got %v", err)
	}
}

func TestSWCheck(t *testing.T) {
	if err := swCheck("X", []byte{0x90, 0x00}); err != nil {
		t.Errorf("9000: %v", err)
	}
	if err := swCheck("X", []byte{}); err == nil || !contains(err.Error(), "short") {
		t.Errorf("empty: %v", err)
	}
	if err := swCheck("X", []byte{0x6A, 0x80}); err == nil || !contains(err.Error(), "SW=6A80") {
		t.Errorf("6A80: %v", err)
	}
}

func TestCTBCS_TransportError(t *testing.T) {
	fake := newFakeSerialIO(t)
	fake.setWriteErr(errSentinel)
	term := newTermCTBCS(fake)
	_, err := term.CTBCS([]byte{0x20, 0x11, 0, 0, 0})
	if err == nil {
		t.Fatal("expected error")
	}
}
