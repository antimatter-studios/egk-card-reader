package orga

import (
	"errors"
	"strings"
	"syscall"
	"testing"
)

func TestFriendlySerialError_PassesNil(t *testing.T) {
	if got := friendlySerialError(nil); got != nil {
		t.Errorf("nil input → %v, want nil", got)
	}
}

func TestFriendlySerialError_PassesThroughUnknown(t *testing.T) {
	in := errors.New("some random failure")
	got := friendlySerialError(in)
	if got != in {
		t.Errorf("unknown error rewrapped: got %v, want pass-through %v", got, in)
	}
}

func TestFriendlySerialError_ENXIO_AddsGuidance(t *testing.T) {
	got := friendlySerialError(syscall.ENXIO)
	if got == nil {
		t.Fatal("ENXIO produced nil error")
	}
	msg := got.Error()
	for _, want := range []string{"USB endpoint", "rebooted", "DFU"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ENXIO message missing %q:\n%s", want, msg)
		}
	}
	if !errors.Is(got, syscall.ENXIO) {
		t.Errorf("ENXIO wrap lost errors.Is identity")
	}
}

func TestFriendlySerialError_ENOENT_AddsGuidance(t *testing.T) {
	got := friendlySerialError(syscall.ENOENT)
	if !errors.Is(got, syscall.ENOENT) || !strings.Contains(got.Error(), "disappeared") {
		t.Errorf("ENOENT wrap incorrect: %v", got)
	}
}

func TestFriendlySerialError_EACCES_AddsGuidance(t *testing.T) {
	got := friendlySerialError(syscall.EACCES)
	if !errors.Is(got, syscall.EACCES) || !strings.Contains(got.Error(), "dialout") {
		t.Errorf("EACCES wrap incorrect: %v", got)
	}
}

func TestFriendlySerialError_EBUSY_AddsGuidance(t *testing.T) {
	got := friendlySerialError(syscall.EBUSY)
	if !errors.Is(got, syscall.EBUSY) || !strings.Contains(got.Error(), "another process") {
		t.Errorf("EBUSY wrap incorrect: %v", got)
	}
}

func TestClassifyBlock(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      []byte
		wantSub string
	}{
		{"too short", []byte{0x12}, "?"},
		{"I-block N(S)=0", []byte{0x12, 0x00, 0x05, 0, 0, 0, 0, 0, 0}, "I-block N(S)=0"},
		{"I-block N(S)=1", []byte{0x12, 0x40, 0x05, 0, 0, 0, 0, 0, 0}, "I-block N(S)=1"},
		{"I-block M=1", []byte{0x12, 0x20, 0x05, 0, 0, 0, 0, 0, 0}, "M=1"},
		{"R-block err=2", []byte{0x21, 0x92, 0x00, 0xB3}, "R-block N(R)=1 err=2"},
		{"S-block RESYNCH req", []byte{0x12, 0xC0, 0x00, 0xD2}, "S-block RESYNCH req"},
		{"S-block RESYNCH resp", []byte{0x21, 0xE0, 0x00, 0xC1}, "S-block RESYNCH resp"},
		{"S-block WTX req", []byte{0x21, 0xC3, 0x01, 0x1E, 0xFD}, "S-block WTX req"},
		{"S-block IFS req", []byte{0x21, 0xC1, 0x01, 0xFE, 0x1F}, "S-block IFS req"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyBlock(tc.in)
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("classifyBlock(%X) = %q; want substring %q", tc.in, got, tc.wantSub)
			}
		})
	}
}
