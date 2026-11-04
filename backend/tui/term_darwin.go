//go:build darwin

package tui

import "golang.org/x/sys/unix"

const (
	termiosRead  = unix.TIOCGETA
	termiosWrite = unix.TIOCSETA
)
