//go:build !linux && !darwin && !windows

package tui

import (
	"errors"
	"os"
)

func enableRaw(*os.File) (func(), error) {
	return nil, errors.New("status console is not supported on this OS")
}

// IsTerminal is false on platforms without a console implementation.
func IsTerminal(*os.File) bool { return false }

func terminalSize(*os.File) (int, int) { return 80, 24 }
