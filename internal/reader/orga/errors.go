package orga

import (
	"errors"
	"fmt"
	"syscall"
)

// friendlySerialError rewraps low-level macOS serial errors that have
// unhelpful default messages ("device not configured" for ENXIO, etc.)
// with guidance the user can act on. Pass-through if the input is nil or
// doesn't match a known errno.
func friendlySerialError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, syscall.ENXIO):
		return fmt.Errorf("%w — the kernel sees the /dev node but the USB endpoint isn't responding. "+
			"If the terminal just rebooted (e.g. after a Fatal Error), wait ~5s for re-enumeration. "+
			"If the device went into DFU mode (PID 0xDF55), power-cycle it. "+
			"Otherwise unplug and replug the USB cable", err)
	case errors.Is(err, syscall.ENOENT):
		return fmt.Errorf("%w — the device node has disappeared. The terminal was likely disconnected mid-session", err)
	case errors.Is(err, syscall.EACCES):
		return fmt.Errorf("%w — permission denied opening the serial device. On Linux, add yourself to the 'dialout' group; on macOS, this usually means another process holds the port", err)
	case errors.Is(err, syscall.EBUSY):
		return fmt.Errorf("%w — another process holds the serial port. Check for stray orga-probe / card-reader instances and screen/minicom sessions", err)
	}
	return err
}
