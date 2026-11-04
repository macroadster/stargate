//go:build linux

package tui

import "golang.org/x/sys/unix"

const (
	termiosRead  = unix.TCGETS
	termiosWrite = unix.TCSETS
)
