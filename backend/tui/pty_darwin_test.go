//go:build darwin

package tui

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

func openTestPTY() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	fd := int(master.Fd())
	if err = ioctlPtr(fd, unix.TIOCPTYGRANT, 0); err != nil {
		_ = master.Close()
		return nil, nil, err
	}
	if err = ioctlPtr(fd, unix.TIOCPTYUNLK, 0); err != nil {
		_ = master.Close()
		return nil, nil, err
	}
	buf := make([]byte, 128)
	if err = ioctlPtr(fd, unix.TIOCPTYGNAME, uintptr(unsafe.Pointer(&buf[0]))); err != nil {
		_ = master.Close()
		return nil, nil, err
	}
	name := cString(buf)
	slave, err = os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		_ = master.Close()
		return nil, nil, err
	}
	ws := &unix.Winsize{Row: 32, Col: 100}
	if err = unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, ws); err != nil {
		_ = slave.Close()
		_ = master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}

func ioctlPtr(fd int, req uint, arg uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), arg)
	if errno != 0 {
		return errno
	}
	return nil
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
