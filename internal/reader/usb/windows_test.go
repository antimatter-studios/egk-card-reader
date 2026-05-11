//go:build windows

package usb

import (
	"errors"
	"testing"
)

func TestWindowsProbe_ReturnsUnsupported(t *testing.T) {
	_, err := defaultProbe().FindDevices(0x0780, 0x1202)
	if err == nil {
		t.Fatal("expected ErrUnsupported, got nil")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err %v doesn't wrap ErrUnsupported", err)
	}
}
