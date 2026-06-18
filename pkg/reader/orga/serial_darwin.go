package orga

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

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

	t, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("tcgetattr: %w", err)
	}

	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON | unix.IXOFF | unix.IXANY
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB | unix.PARODD | unix.CSTOPB | unix.CRTSCTS
	t.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL
	t.Ispeed = uint64(baud)
	t.Ospeed = uint64(baud)
	t.Cc[unix.VMIN] = 0
	t.Cc[unix.VTIME] = 1

	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, t); err != nil {
		f.Close()
		return nil, fmt.Errorf("tcsetattr: %w", err)
	}
	return f, nil
}
