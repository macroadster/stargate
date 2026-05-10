package datadir

import (
	"os"
	"path/filepath"
)

// Default returns the root data directory.
// Respects STARGATE_DATA_DIR when set; otherwise "data".
// This is the canonical default used by all subsystems.
func Default() string {
	if d := os.Getenv("STARGATE_DATA_DIR"); d != "" {
		return d
	}
	return "data"
}

// Path returns a path under the default data directory.
func Path(subpath string) string {
	return filepath.Join(Default(), subpath)
}
