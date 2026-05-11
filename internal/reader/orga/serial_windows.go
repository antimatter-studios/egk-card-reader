package orga

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// openSerial is the Windows backend. The CDC-ACM ORGA reader appears as a
// COM port (e.g. \\.\COM3). Configuration goes through the Win32 Comm API
// (DCB + CommTimeouts) rather than POSIX termios — same end result, very
// different bookkeeping.
//
// Read timeout policy mirrors the unix VMIN=0/VTIME=1 setup the darwin/linux
// backends use: return whatever is already in the buffer, otherwise wait
// briefly and return empty. That keeps T=1 layer's framing/timeout logic
// portable across platforms.
func openSerial(dev string, baud int) (serialIO, error) {
	devUTF16, err := windows.UTF16PtrFromString(dev)
	if err != nil {
		return nil, fmt.Errorf("device path %q: %w", dev, err)
	}

	h, err := windows.CreateFile(
		devUTF16,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, // no sharing
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dev, err)
	}

	var dcb windows.DCB
	dcb.DCBlength = uint32(unsafe.Sizeof(dcb))
	if err := windows.GetCommState(h, &dcb); err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("GetCommState: %w", err)
	}

	dcb.BaudRate = uint32(baud)
	dcb.ByteSize = 8
	dcb.Parity = 0   // NOPARITY
	dcb.StopBits = 0 // ONESTOPBIT
	// DCB flags field: only fBinary (bit 0) is required to be 1. Clearing all
	// other bits disables fParity, fOutxCtsFlow, fOutxDsrFlow, RTS/DTR
	// control, XON/XOFF, etc.
	dcb.Flags = 0x01

	if err := windows.SetCommState(h, &dcb); err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("SetCommState: %w", err)
	}

	timeouts := windows.CommTimeouts{
		ReadIntervalTimeout:         0xFFFFFFFF, // MAXDWORD — return immediately with whatever's buffered
		ReadTotalTimeoutMultiplier:  0,
		ReadTotalTimeoutConstant:    100, // ...but cap any wait at 100ms total
		WriteTotalTimeoutMultiplier: 0,
		WriteTotalTimeoutConstant:   0,
	}
	if err := windows.SetCommTimeouts(h, &timeouts); err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("SetCommTimeouts: %w", err)
	}

	return os.NewFile(uintptr(h), dev), nil
}
