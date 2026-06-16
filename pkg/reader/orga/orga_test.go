package orga

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestTerminal_Close(t *testing.T) {
	fake := newFakeSerialIO(t)
	term := &Terminal{io: fake, ns: map[byte]byte{}}
	if err := term.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if !fake.closed {
		t.Error("fake not closed")
	}
}

func TestTerminal_Close_PropagatesError(t *testing.T) {
	ec := &errCloser{ReadWriter: &bytes.Buffer{}, closeErr: errSentinel}
	term := &Terminal{io: ec, ns: map[byte]byte{}}
	if err := term.Close(); !errors.Is(err, errSentinel) {
		t.Errorf("got %v; want sentinel", err)
	}
}

func TestTerminal_Slot1And2(t *testing.T) {
	term := &Terminal{io: newFakeSerialIO(t), ns: map[byte]byte{}}
	s1 := term.Slot(1)
	if s1.Number() != 1 || s1.peer != icc1Addr {
		t.Errorf("slot1=%+v", s1)
	}
	s2 := term.Slot(2)
	if s2.Number() != 2 || s2.peer != icc2Addr {
		t.Errorf("slot2=%+v", s2)
	}
}

func TestTerminal_Slot_InvalidPanics(t *testing.T) {
	term := &Terminal{io: newFakeSerialIO(t), ns: map[byte]byte{}}
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on invalid slot")
		}
	}()
	_ = term.Slot(3)
}

func TestSlot_Transmit_DangerousRefused(t *testing.T) {
	term := &Terminal{io: newFakeSerialIO(t), ns: map[byte]byte{}, timeout: time.Second}
	slot := term.Slot(1)
	// ISO VERIFY: CLA=00 INS=20
	_, err := slot.Transmit([]byte{0x00, 0x20, 0x00, 0x82, 0x06, 1, 2, 3, 4, 5, 6})
	if err == nil {
		t.Fatal("expected dangerous APDU refusal")
	}
	if _, ok := err.(*ErrDangerousAPDU); !ok {
		t.Errorf("err type %T; want *ErrDangerousAPDU", err)
	}
}

func TestSlot_Transmit_NADRoutingSlot1(t *testing.T) {
	apdu := []byte{0x00, 0xA4, 0x04, 0x0C, 0x07, 0xD2, 0x76, 0x00, 0x01, 0x44, 0x80, 0x00}
	// host→ICC1: outNAD = (0<<4)|2 = 0x02
	req := buildBlock(0x02, 0x00, apdu)
	resp := buildBlock(0x20, 0x00, []byte{0x90, 0x00})
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := &Terminal{io: fake, ns: map[byte]byte{}, timeout: time.Second}
	slot := term.Slot(1)
	got, err := slot.Transmit(apdu)
	if err != nil {
		t.Fatalf("Transmit: %v", err)
	}
	if !bytes.Equal(got, []byte{0x90, 0x00}) {
		t.Errorf("resp=%X", got)
	}
}

func TestSlot_Transmit_NADRoutingSlot2(t *testing.T) {
	apdu := []byte{0x00, 0xA4, 0x00, 0x00, 0x02, 0x3F, 0x00}
	// host→ICC2: outNAD = (2<<4)|2 = 0x22
	req := buildBlock(0x22, 0x00, apdu)
	resp := buildBlock(0x22, 0x00, []byte{0x90, 0x00})
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := &Terminal{io: fake, ns: map[byte]byte{}, timeout: time.Second}
	slot := term.Slot(2)
	if _, err := slot.Transmit(apdu); err != nil {
		t.Fatalf("Transmit: %v", err)
	}
}

func TestSlot_Transmit_AllowWriteSendsDangerous(t *testing.T) {
	apdu := []byte{0x00, 0x20, 0x00, 0x82, 0x06, 1, 2, 3, 4, 5, 6}
	req := buildBlock(0x02, 0x00, apdu)
	resp := buildBlock(0x20, 0x00, []byte{0x90, 0x00})
	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := &Terminal{io: fake, ns: map[byte]byte{}, timeout: time.Second, allowWrite: true}
	slot := term.Slot(1)
	got, err := slot.Transmit(apdu)
	if err != nil {
		t.Fatalf("Transmit: %v", err)
	}
	if !bytes.Equal(got, []byte{0x90, 0x00}) {
		t.Errorf("resp=%X", got)
	}
}

func TestOpen_NoDevice(t *testing.T) {
	// Open with an explicit non-existent device path — fails at openSerial.
	// (Skipping glob-based discovery to avoid host-state dependence.)
	_, err := Open(Options{DevNode: "/dev/null-does-not-exist-orga-test"})
	if err == nil {
		t.Fatal("expected error on missing device")
	}
}
