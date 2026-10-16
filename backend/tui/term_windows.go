//go:build windows

package tui

import (
	"os"

	"golang.org/x/sys/windows"
)

func enableRaw(in *os.File) (func(), error) {
	h := windows.Handle(in.Fd())
	var inMode uint32
	if err := windows.GetConsoleMode(h, &inMode); err != nil {
		return nil, err
	}
	raw := inMode
	raw &^= windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_MOUSE_INPUT
	// Keep processed input so Ctrl-C still raises a console control event.
	raw |= windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err := windows.SetConsoleMode(h, raw); err != nil {
		return nil, err
	}

	var outMode uint32
	var out windows.Handle
	var haveOut bool
	if stdout := os.Stdout; stdout != nil {
		out = windows.Handle(stdout.Fd())
		if windows.GetConsoleMode(out, &outMode) == nil {
			haveOut = true
			_ = windows.SetConsoleMode(out, outMode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
		}
	}
	return func() {
		_ = windows.SetConsoleMode(h, inMode)
		if haveOut {
			_ = windows.SetConsoleMode(out, outMode)
		}
	}, nil
}

// IsTerminal reports whether f is a console handle.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(f.Fd()), &mode) == nil
}

func terminalSize(f *os.File) (int, int) {
	if f == nil {
		return 80, 24
	}
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(f.Fd()), &info); err != nil {
		return 80, 24
	}
	w := int(info.Window.Right - info.Window.Left + 1)
	h := int(info.Window.Bottom - info.Window.Top + 1)
	if w <= 0 || h <= 0 {
		return 80, 24
	}
	return w, h
}
