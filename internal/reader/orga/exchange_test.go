package orga

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func newTermWithFake(fake *fakeSerialIO) *Terminal {
	return &Terminal{
		io:      fake,
		ns:      map[byte]byte{},
		timeout: 200 * time.Millisecond,
	}
}

func TestResync_Success(t *testing.T) {
	// S(RESYNCH req): NAD=0x12 PCB=0xC0
	reqRaw := buildBlock(0x12, 0xC0, nil)
	// S(RESYNCH resp): NAD=0x21 PCB=0xE0
	respRaw := buildBlock(0x21, 0xE0, nil)
	fake := newFakeSerialIO(t, exchange{wantWrite: reqRaw, reply: respRaw})
	term := newTermWithFake(fake)
	term.ns[icc1Addr] = 1 // dirty value, must be reset
	term.ifsNegotiated = true

	if err := term.resync(); err != nil {
		t.Fatalf("resync: %v", err)
	}
	if term.ifsNegotiated {
		t.Error("ifsNegotiated not cleared")
	}
	if len(term.ns) != 0 {
		t.Errorf("ns not cleared: %v", term.ns)
	}
	fake.assertDrained()
}

func TestResync_WrongPCB(t *testing.T) {
	reqRaw := buildBlock(0x12, 0xC0, nil)
	// Reply with an I-block instead of S(RESYNCH resp).
	wrongRaw := buildBlock(0x21, 0x00, []byte{0x90, 0x00})
	fake := newFakeSerialIO(t, exchange{wantWrite: reqRaw, reply: wrongRaw})
	term := newTermWithFake(fake)
	err := term.resync()
	if err == nil || !contains(err.Error(), "expected S(RESYNCH response)") {
		t.Errorf("got %v; want wrong PCB error", err)
	}
}

func TestResync_WriteError(t *testing.T) {
	fake := newFakeSerialIO(t)
	fake.setWriteErr(errSentinel)
	term := newTermWithFake(fake)
	if err := term.resync(); !errors.Is(err, errSentinel) {
		t.Errorf("got %v; want sentinel", err)
	}
}

func TestResync_ReadTimeout(t *testing.T) {
	reqRaw := buildBlock(0x12, 0xC0, nil)
	// No reply scripted → readOneBlock will EOF-loop until deadline.
	fake := newFakeSerialIO(t, exchange{wantWrite: reqRaw})
	term := newTermWithFake(fake)
	term.timeout = 20 * time.Millisecond
	if err := term.resync(); err == nil || !contains(err.Error(), "timeout") {
		t.Errorf("got %v; want timeout", err)
	}
}

func TestTransactWithNAD_SimpleIBlock(t *testing.T) {
	// Host → ICC1: NAD = (icc1Addr<<4)|hostAddr = 0x02
	outNAD := byte(0x02)
	apdu := []byte{0x00, 0xA4, 0x04, 0x0C, 0x07, 0xD2, 0x76, 0x00, 0x01, 0x44, 0x80, 0x00}
	req := buildBlock(outNAD, 0x00, apdu) // N(S)=0

	// ICC1 → host: NAD swapped = 0x20
	respINF := []byte{0x90, 0x00}
	resp := buildBlock(0x20, 0x00, respINF)

	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermWithFake(fake)

	got, err := term.transactWithNAD(icc1Addr, apdu)
	if err != nil {
		t.Fatalf("transactWithNAD: %v", err)
	}
	if !bytes.Equal(got, respINF) {
		t.Errorf("response=%X; want %X", got, respINF)
	}
	if term.ns[icc1Addr] != 1 {
		t.Errorf("N(S) not toggled: %d", term.ns[icc1Addr])
	}
	fake.assertDrained()
}

func TestTransactWithNAD_NSToggling(t *testing.T) {
	apdu := []byte{0x00, 0xB0, 0, 0, 4}
	outNAD := byte(0x02)
	resp := buildBlock(0x20, 0x00, []byte{0x90, 0x00})

	// First transaction: N(S)=0 → PCB=0x00
	req0 := buildBlock(outNAD, 0x00, apdu)
	// Second transaction: N(S)=1 → PCB=0x40
	req1 := buildBlock(outNAD, 0x40, apdu)
	resp1 := buildBlock(0x20, 0x40, []byte{0x90, 0x00})

	fake := newFakeSerialIO(t,
		exchange{wantWrite: req0, reply: resp},
		exchange{wantWrite: req1, reply: resp1},
	)
	term := newTermWithFake(fake)

	if _, err := term.transactWithNAD(icc1Addr, apdu); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := term.transactWithNAD(icc1Addr, apdu); err != nil {
		t.Fatalf("second: %v", err)
	}
	if term.ns[icc1Addr] != 0 {
		t.Errorf("N(S) didn't toggle back to 0: %d", term.ns[icc1Addr])
	}
	fake.assertDrained()
}

func TestTransactWithNAD_UnexpectedNAD(t *testing.T) {
	apdu := []byte{0x00, 0xA4, 0x00, 0x00, 0x02, 0x3F, 0x00}
	outNAD := byte(0x02)
	req := buildBlock(outNAD, 0x00, apdu)
	// Reply with wrong NAD (not the expected 0x20 and not 0x00).
	resp := buildBlock(0x10, 0x00, []byte{0x90, 0x00})

	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermWithFake(fake)

	_, err := term.transactWithNAD(icc1Addr, apdu)
	if err == nil || !contains(err.Error(), "unexpected NAD") {
		t.Errorf("got %v; want unexpected NAD", err)
	}
}

func TestTransactWithNAD_RBlockError(t *testing.T) {
	apdu := []byte{0x00, 0xC0, 0, 0, 0}
	outNAD := byte(0x02)
	req := buildBlock(outNAD, 0x00, apdu)
	// Peer R-block with err nibble 2 (other error)
	resp := buildBlock(0x20, 0x82, nil)

	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermWithFake(fake)
	_, err := term.transactWithNAD(icc1Addr, apdu)
	if err == nil || !contains(err.Error(), "R-block") {
		t.Errorf("got %v; want R-block error", err)
	}
}

func TestTransactWithNAD_SBlockWTX(t *testing.T) {
	// Card sends S(WTX request) before its real I-block response.
	// We must auto-ack with S(WTX response) and continue reading.
	apdu := []byte{0x00, 0xB0, 0, 0, 4}
	outNAD := byte(0x02)
	req := buildBlock(outNAD, 0x00, apdu)

	// S(WTX req) from ICC1: PCB=0xC3, INF=01 (BWT multiplier)
	wtxReq := buildBlock(0x20, 0xC3, []byte{0x01})
	// Expected ack from host: PCB=0xE3 (response bit set), echo INF.
	wtxAck := buildBlock(outNAD, 0xE3, []byte{0x01})

	// Final I-block response from ICC1
	finalResp := buildBlock(0x20, 0x00, []byte{0xDE, 0xAD, 0x90, 0x00})

	fake := newFakeSerialIO(t,
		exchange{wantWrite: req, reply: wtxReq},
		exchange{wantWrite: wtxAck, reply: finalResp},
	)
	term := newTermWithFake(fake)
	got, err := term.transactWithNAD(icc1Addr, apdu)
	if err != nil {
		t.Fatalf("transact: %v", err)
	}
	if !bytes.Equal(got, []byte{0xDE, 0xAD, 0x90, 0x00}) {
		t.Errorf("response=%X", got)
	}
	fake.assertDrained()
}

func TestTransactWithNAD_SBlockIFSSetsFlag(t *testing.T) {
	// Terminal sends S(IFS request) before card response. We ack it and the
	// terminal then sends the I-block.
	apdu := []byte{0x00, 0xA4, 0, 0, 2, 0x3F, 0x00}
	outNAD := byte(0x02)
	req := buildBlock(outNAD, 0x00, apdu)

	ifsReq := buildBlock(0x20, 0xC1, []byte{0xFE})
	ifsAck := buildBlock(outNAD, 0xE1, []byte{0xFE})
	finalResp := buildBlock(0x20, 0x00, []byte{0x90, 0x00})

	fake := newFakeSerialIO(t,
		exchange{wantWrite: req, reply: ifsReq},
		exchange{wantWrite: ifsAck, reply: finalResp},
	)
	term := newTermWithFake(fake)
	if _, err := term.transactWithNAD(icc1Addr, apdu); err != nil {
		t.Fatalf("transact: %v", err)
	}
	if !term.ifsNegotiated {
		t.Error("ifsNegotiated not set after IFS S-block")
	}
}

func TestTransactWithNAD_UnexpectedSBlockResponse(t *testing.T) {
	apdu := []byte{0x00, 0xB0, 0, 0, 4}
	outNAD := byte(0x02)
	req := buildBlock(outNAD, 0x00, apdu)
	// S-block with response bit already set — should be flagged.
	resp := buildBlock(0x20, 0xE3, []byte{0x01})

	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermWithFake(fake)
	_, err := term.transactWithNAD(icc1Addr, apdu)
	if err == nil || !contains(err.Error(), "unexpected S-block response") {
		t.Errorf("got %v; want unexpected S-block response", err)
	}
}

func TestTransactWithNAD_ChainedIBlock(t *testing.T) {
	// Card sends two chained I-blocks; transactWithNAD acks the first with
	// an R-block and reassembles both INF segments.
	apdu := []byte{0x00, 0xB0, 0, 0, 8}
	outNAD := byte(0x02)
	req := buildBlock(outNAD, 0x00, apdu)

	// First I-block from card: N(S)=0, M=1, INF=AA BB CC DD
	part1 := buildBlock(0x20, 0x20, []byte{0xAA, 0xBB, 0xCC, 0xDD})
	// R-block ack from host: N(R) = ~N(S) of first chained part = 1 → PCB = 0x90
	rAck := buildBlock(outNAD, 0x90, nil)
	// Second I-block from card: N(S)=1, M=0, INF=EE FF 90 00
	part2 := buildBlock(0x20, 0x40, []byte{0xEE, 0xFF, 0x90, 0x00})

	fake := newFakeSerialIO(t,
		exchange{wantWrite: req, reply: part1},
		exchange{wantWrite: rAck, reply: part2},
	)
	term := newTermWithFake(fake)
	got, err := term.transactWithNAD(icc1Addr, apdu)
	if err != nil {
		t.Fatalf("transact: %v", err)
	}
	want := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x90, 0x00}
	if !bytes.Equal(got, want) {
		t.Errorf("got %X; want %X", got, want)
	}
	fake.assertDrained()
}

func TestTransactWithNAD_NAD00Accepted(t *testing.T) {
	// Some ORGA firmware replies with NAD=0x00. transactWithNAD treats it as
	// a wildcard match.
	apdu := []byte{0x00, 0xA4, 0, 0, 0}
	outNAD := byte(0x02)
	req := buildBlock(outNAD, 0x00, apdu)
	resp := buildBlock(0x00, 0x00, []byte{0x90, 0x00})

	fake := newFakeSerialIO(t, exchange{wantWrite: req, reply: resp})
	term := newTermWithFake(fake)
	if _, err := term.transactWithNAD(icc1Addr, apdu); err != nil {
		t.Fatalf("got %v; want NAD=00 to be accepted", err)
	}
}

func TestTransactWithNAD_WriteError(t *testing.T) {
	fake := newFakeSerialIO(t)
	fake.setWriteErr(errSentinel)
	term := newTermWithFake(fake)
	_, err := term.transactWithNAD(icc1Addr, []byte{0x00, 0xA4, 0, 0, 0})
	if !errors.Is(err, errSentinel) {
		t.Errorf("got %v; want sentinel", err)
	}
}

func TestTransactWithNAD_ReadTimeout(t *testing.T) {
	apdu := []byte{0x00, 0xA4, 0, 0, 0}
	outNAD := byte(0x02)
	req := buildBlock(outNAD, 0x00, apdu)
	// No reply — readOneBlock will hit deadline.
	fake := newFakeSerialIO(t, exchange{wantWrite: req})
	term := newTermWithFake(fake)
	term.timeout = 20 * time.Millisecond
	_, err := term.transactWithNAD(icc1Addr, apdu)
	if err == nil || !contains(err.Error(), "timeout") {
		t.Errorf("got %v; want timeout", err)
	}
}

func TestWriteBlock_WriteError(t *testing.T) {
	fake := newFakeSerialIO(t)
	fake.setWriteErr(errSentinel)
	term := newTermWithFake(fake)
	err := term.writeBlock(0x02, 0x00, []byte{0x00})
	if !errors.Is(err, errSentinel) {
		t.Errorf("got %v; want sentinel", err)
	}
}
