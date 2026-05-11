package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ebfe/scard"
)

func TestH(t *testing.T) {
	got := h("D27600000102")
	want := []byte{0xD2, 0x76, 0x00, 0x00, 0x01, 0x02}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: %d vs %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("byte %d: %02X vs %02X", i, got[i], want[i])
		}
	}
}

func TestHPanicsOnBadHex(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic")
		}
	}()
	_ = h("not hex")
}

func TestParseICCSNTLV(t *testing.T) {
	// 5A 0A <10 bytes ICCSN> — TLV form.
	tlv := []byte{0x5A, 0x0A, 0x80, 0x27, 0x66, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05}
	got := parseICCSN(tlv)
	if got != "802766000001020304" + "05" {
		t.Errorf("got %q", got)
	}
}

func TestParseICCSNRaw10(t *testing.T) {
	raw := []byte{0x80, 0x27, 0x66, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05}
	got := parseICCSN(raw)
	if got != "80276600000102030405" {
		t.Errorf("got %q", got)
	}
}

func TestParseICCSNMalformed(t *testing.T) {
	// Too short.
	if got := parseICCSN([]byte{0x5A, 0x0A, 0x00}); got != "" {
		t.Errorf("short TLV: got %q, want empty", got)
	}
	// Wrong length tag and not 10 bytes.
	if got := parseICCSN([]byte{0x01, 0x02, 0x03}); got != "" {
		t.Errorf("wrong shape: got %q, want empty", got)
	}
	// Empty.
	if got := parseICCSN(nil); got != "" {
		t.Errorf("nil: got %q", got)
	}
}

// fakeProbeCard implements probeCard with scripted responses.
type fakeProbeCard struct {
	resp     []byte
	transErr error
}

func (f *fakeProbeCard) Transmit(_ []byte) ([]byte, error) {
	if f.transErr != nil {
		return nil, f.transErr
	}
	return f.resp, nil
}

func (f *fakeProbeCard) Status() (*scard.CardStatus, error) {
	return &scard.CardStatus{}, nil
}

func (f *fakeProbeCard) Disconnect(_ scard.Disposition) error { return nil }

// fakeProbeCtx implements probeContext.
type fakeProbeCtx struct {
	connectErr error
}

func (f *fakeProbeCtx) Connect(_ string, _ scard.ShareMode, _ scard.Protocol) (*scard.Card, error) {
	// Returning (nil, err) is sufficient for the Connect-fails branch.
	return nil, f.connectErr
}

func TestTransmitHappy(t *testing.T) {
	// Canned response: data bytes + SW1SW2 (0x9000).
	card := &fakeProbeCard{resp: []byte{0xAA, 0xBB, 0xCC, 0x90, 0x00}}
	sw, data := transmit(card, []byte{0x00, 0xA4, 0x00, 0x0C})
	if sw != 0x9000 {
		t.Errorf("sw = %04X, want 9000", sw)
	}
	if !bytes.Equal(data, []byte{0xAA, 0xBB, 0xCC}) {
		t.Errorf("data = %x, want AABBCC", data)
	}
}

func TestTransmitShortResponse(t *testing.T) {
	// 1 byte is shorter than the 2-byte SW.
	card := &fakeProbeCard{resp: []byte{0x9F}}
	sw, data := transmit(card, []byte{0x00})
	if sw != 0 || data != nil {
		t.Errorf("got (sw=%04X, data=%x), want (0, nil)", sw, data)
	}
}

func TestTransmitError(t *testing.T) {
	card := &fakeProbeCard{transErr: fmt.Errorf("io down")}
	sw, data := transmit(card, []byte{0x00})
	if sw != 0 || data != nil {
		t.Errorf("got (sw=%04X, data=%x), want (0, nil)", sw, data)
	}
}

func TestProbeReaderConnectFails(t *testing.T) {
	// Capture stdout while probeReader runs.
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		done <- buf.Bytes()
	}()

	ctx := &fakeProbeCtx{connectErr: fmt.Errorf("no card")}
	probeReader(ctx, "reader-0")

	w.Close()
	os.Stdout = orig
	got := string(<-done)
	if !strings.Contains(got, "Connect failed") {
		t.Errorf("expected 'Connect failed' in output, got %q", got)
	}
	if !strings.Contains(got, "no card") {
		t.Errorf("expected underlying error in output, got %q", got)
	}
}

func TestGuessFromATR(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{[]byte{0x80, 0x70, 0x70, 0x02}, "eGK G2.x"},
		{[]byte{0x3B, 0xDD, 0xAA}, "gematik-style"},
		{[]byte{0x3B, 0xD3, 0xAA}, "gematik-style"},
		{[]byte{0x3B, 0xFF, 0xAA}, "SMC-B Atos / TCOS"},
		{[]byte{0x3B, 0xAA, 0xBB}, "T=0/T=1"},
		{[]byte{0x3F, 0xAA, 0xBB}, "inverse-convention"},
		{[]byte{0xAB, 0xCD}, "unknown"},
	}
	for _, c := range cases {
		got := guessFromATR(c.in)
		if !strings.Contains(got, c.want) {
			t.Errorf("guessFromATR(%X) = %q, want substring %q", c.in, got, c.want)
		}
	}
}
