package orga

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// openSerial is the Linux backend. The termios ioctl numbers differ from
// darwin (TCGETS/TCSETS vs TIOCGETA/TIOCSETA), the Termios struct is uint32
// rather than uint64, and standard termios sets the baud rate via bits in
// Cflag (the Bxxx constants) rather than via Ispeed/Ospeed. We support the
// usual rate ladder; arbitrary rates would need termios2/BOTHER which the
// stdlib unix package doesn't expose. The orga driver defaults to 9600 so
// this covers the common case.
func openSerial(dev string, baud int) (serialIO, error) {
	fd, err := unix.Open(dev, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dev, err)
	}
	f := os.NewFile(uintptr(fd), dev)
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFL, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("clear O_NONBLOCK: %w", err)
	}

	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("tcgetattr: %w", err)
	}

	speed, err := linuxBaud(baud)
	if err != nil {
		f.Close()
		return nil, err
	}

	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON | unix.IXOFF | unix.IXANY
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB | unix.PARODD | unix.CSTOPB | unix.CRTSCTS | unix.CBAUD
	t.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL | speed
	t.Ispeed = speed
	t.Ospeed = speed
	t.Cc[unix.VMIN] = 0
	t.Cc[unix.VTIME] = 1

	if err := unix.IoctlSetTermios(fd, unix.TCSETS, t); err != nil {
		f.Close()
		return nil, fmt.Errorf("tcsetattr: %w", err)
	}
	return f, nil
}

// linuxBaud maps a numeric baud rate to the Bxxx constant that termios
// expects in Cflag. Returns an error for rates outside the standard ladder.
func linuxBaud(rate int) (uint32, error) {
	switch rate {
	case 1200:
		return unix.B1200, nil
	case 2400:
		return unix.B2400, nil
	case 4800:
		return unix.B4800, nil
	case 9600:
		return unix.B9600, nil
	case 19200:
		return unix.B19200, nil
	case 38400:
		return unix.B38400, nil
	case 57600:
		return unix.B57600, nil
	case 115200:
		return unix.B115200, nil
	case 230400:
		return unix.B230400, nil
	case 460800:
		return unix.B460800, nil
	case 921600:
		return unix.B921600, nil
	}
	return 0, fmt.Errorf("unsupported baud %d on linux (need one of 1200/2400/4800/9600/19200/38400/57600/115200/230400/460800/921600)", rate)
}
