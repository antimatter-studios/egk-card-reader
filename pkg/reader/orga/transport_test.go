package orga

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestLRC(t *testing.T) {
	for _, tc := range []struct {
		in   []byte
		want byte
	}{
		{nil, 0x00},
		{[]byte{}, 0x00},
		{[]byte{0x00}, 0x00},
		{[]byte{0xFF}, 0xFF},
		{[]byte{0x12, 0x00, 0x00}, 0x12},
		{[]byte{0x12, 0xC0, 0x00}, 0xD2}, // S(RESYNCH req)
		{[]byte{0x21, 0xE0, 0x00}, 0xC1}, // S(RESYNCH resp)
	} {
		if got := lrc(tc.in); got != tc.want {
			t.Errorf("lrc(% X) = %02X; want %02X", tc.in, got, tc.want)
		}
	}
}

func TestBuildBlock(t *testing.T) {
	// S(RESYNCH req): NAD=0x12 PCB=0xC0 INF=∅
	got := buildBlock(0x12, 0xC0, nil)
	want := []byte{0x12, 0xC0, 0x00, 0xD2}
	if !bytes.Equal(got, want) {
		t.Errorf("buildBlock RESYNCH = %X; want %X", got, want)
	}

	// I-block with payload
	got = buildBlock(0x02, 0x00, []byte{0x20, 0x11, 0x00, 0x00, 0x00})
	// LRC = XOR of all preceding bytes
	want = []byte{0x02, 0x00, 0x05, 0x20, 0x11, 0x00, 0x00, 0x00}
	want = append(want, lrc(want))
	if !bytes.Equal(got, want) {
		t.Errorf("buildBlock I-block = %X; want %X", got, want)
	}
}

func TestParseBlock(t *testing.T) {
	t.Run("incomplete header", func(t *testing.T) {
		_, _, err := parseBlock([]byte{0x12, 0xC0})
		if !errors.Is(err, errIncomplete) {
			t.Errorf("got %v; want errIncomplete", err)
		}
	})
	t.Run("incomplete body", func(t *testing.T) {
		_, _, err := parseBlock([]byte{0x12, 0x00, 0x05, 0x01, 0x02})
		if !errors.Is(err, errIncomplete) {
			t.Errorf("got %v; want errIncomplete", err)
		}
	})
	t.Run("good block", func(t *testing.T) {
		raw := buildBlock(0x21, 0xE0, nil)
		blk, total, err := parseBlock(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if total != len(raw) {
			t.Errorf("total=%d; want %d", total, len(raw))
		}
		if blk.NAD != 0x21 || blk.PCB != 0xE0 || len(blk.INF) != 0 {
			t.Errorf("blk=%+v", blk)
		}
	})
	t.Run("good block with INF", func(t *testing.T) {
		raw := buildBlock(0x21, 0x00, []byte{0xDE, 0xAD, 0xBE, 0xEF})
		blk, total, err := parseBlock(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if total != len(raw) {
			t.Errorf("total=%d", total)
		}
		if !bytes.Equal(blk.INF, []byte{0xDE, 0xAD, 0xBE, 0xEF}) {
			t.Errorf("INF=%X", blk.INF)
		}
	})
	t.Run("LRC mismatch", func(t *testing.T) {
		bad := []byte{0x12, 0xC0, 0x00, 0xFF}
		_, _, err := parseBlock(bad)
		if err == nil || !contains(err.Error(), "LRC mismatch") {
			t.Errorf("got %v; want LRC mismatch", err)
		}
	})
}

func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }

func TestSwapNAD(t *testing.T) {
	for _, tc := range []struct {
		in, want byte
	}{
		{0x12, 0x21},
		{0x21, 0x12},
		{0x02, 0x20}, // host→ICC1: SAD=0 DAD=2
		{0x20, 0x02},
		{0x00, 0x00},
		{0xFF, 0xFF},
	} {
		if got := swapNAD(tc.in); got != tc.want {
			t.Errorf("swapNAD(%02X) = %02X; want %02X", tc.in, got, tc.want)
		}
	}
}

func TestBlockPredicates(t *testing.T) {
	// I-block: bit 7 = 0
	for _, pcb := range []byte{0x00, 0x40, 0x20, 0x60} {
		if !isIBlock(pcb) {
			t.Errorf("isIBlock(%02X) false", pcb)
		}
		if isRBlock(pcb) {
			t.Errorf("isRBlock(%02X) true", pcb)
		}
		if isSBlock(pcb) {
			t.Errorf("isSBlock(%02X) true", pcb)
		}
	}
	// R-block: bits 7..6 = 10
	for _, pcb := range []byte{0x80, 0x90, 0x82, 0xB0} {
		if isIBlock(pcb) {
			t.Errorf("isIBlock(%02X) true", pcb)
		}
		if !isRBlock(pcb) {
			t.Errorf("isRBlock(%02X) false", pcb)
		}
		if isSBlock(pcb) {
			t.Errorf("isSBlock(%02X) true", pcb)
		}
	}
	// S-block: bits 7..6 = 11
	for _, pcb := range []byte{0xC0, 0xE0, 0xC3, 0xE3} {
		if isIBlock(pcb) {
			t.Errorf("isIBlock(%02X) true", pcb)
		}
		if isRBlock(pcb) {
			t.Errorf("isRBlock(%02X) true", pcb)
		}
		if !isSBlock(pcb) {
			t.Errorf("isSBlock(%02X) false", pcb)
		}
	}
}

func TestIBlockNSAndMore(t *testing.T) {
	if iBlockNS(0x00) != 0 {
		t.Error("N(S)=0 case")
	}
	if iBlockNS(0x40) != 1 {
		t.Error("N(S)=1 case")
	}
	if iBlockMore(0x00) {
		t.Error("M=0 case")
	}
	if !iBlockMore(0x20) {
		t.Error("M=1 case")
	}
}

func TestReadOneBlock_SingleRead(t *testing.T) {
	raw := buildBlock(0x21, 0xE0, nil)
	fake := newFakeSerialIO(t)
	fake.preload(raw)
	blk, gotRaw, err := readOneBlock(fake, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if blk.PCB != 0xE0 {
		t.Errorf("PCB=%02X", blk.PCB)
	}
	if !bytes.Equal(gotRaw, raw) {
		t.Errorf("raw=%X want %X", gotRaw, raw)
	}
}

func TestReadOneBlock_Chunked(t *testing.T) {
	raw := buildBlock(0x21, 0x00, []byte{0x90, 0x00})
	fake := newFakeSerialIO(t)
	fake.preload(raw)
	chunked := &chunkedReader{inner: fake, cap: 2}
	blk, gotRaw, err := readOneBlock(chunked, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !bytes.Equal(gotRaw, raw) {
		t.Errorf("raw=%X want %X", gotRaw, raw)
	}
	if !bytes.Equal(blk.INF, []byte{0x90, 0x00}) {
		t.Errorf("INF=%X", blk.INF)
	}
}

func TestReadOneBlock_Timeout(t *testing.T) {
	fake := newFakeSerialIO(t) // empty: every Read returns EOF immediately
	deadline := time.Now().Add(20 * time.Millisecond)
	_, _, err := readOneBlock(fake, deadline)
	if err == nil || !contains(err.Error(), "timeout") {
		t.Errorf("got %v; want timeout", err)
	}
}

func TestReadOneBlock_LRCMismatch(t *testing.T) {
	bad := []byte{0x12, 0xC0, 0x00, 0xFF}
	fake := newFakeSerialIO(t)
	fake.preload(bad)
	_, _, err := readOneBlock(fake, time.Now().Add(time.Second))
	if err == nil || !contains(err.Error(), "LRC mismatch") {
		t.Errorf("got %v; want LRC mismatch", err)
	}
}

func TestReadOneBlock_ReadError(t *testing.T) {
	r := &errAfterReader{reads: nil, err: errSentinel}
	_, _, err := readOneBlock(r, time.Now().Add(time.Second))
	if !errors.Is(err, errSentinel) {
		t.Errorf("got %v; want sentinel", err)
	}
}
