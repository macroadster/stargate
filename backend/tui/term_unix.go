//go:build linux || darwin

package tui

import (
	"os"

	"golang.org/x/sys/unix"
)

func enableRaw(f *os.File) (func(), error) {
	fd := int(f.Fd())
	orig, err := unix.IoctlGetTermios(fd, termiosRead)
	if err != nil {
		return nil, err
	}
	raw := *orig
	// Keep ISIG so Ctrl-C still delivers SIGINT to the server.
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.IEXTEN
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag |= unix.OPOST | unix.ONLCR
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, termiosWrite, &raw); err != nil {
		return nil, err
	}
	return func() {
		_ = unix.IoctlSetTermios(fd, termiosWrite, orig)
	}, nil
}

// IsTerminal reports whether f is a terminal device.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	_, err := unix.IoctlGetTermios(int(f.Fd()), termiosRead)
	return err == nil
}

func terminalSize(f *os.File) (int, int) {
	if f == nil {
		return 80, 24
	}
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws == nil || ws.Col == 0 || ws.Row == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}
