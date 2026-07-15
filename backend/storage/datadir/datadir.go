package datadir

import (
       "fmt"
       "log"
	"os"
	"path/filepath"
       "strings"
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

// ---------------------------------------------------------------------------
// Three-level partition helpers
// ---------------------------------------------------------------------------
//
// For a 64-char hex key "abcdef1234…" the partition prefix is "ab/cd/ef",
// producing filesystem paths like:
//
//     $UPLOADS_DIR/ab/cd/ef/abcdef1234…          (flat hash files)
//     $UPLOADS_DIR/results/ab/cd/ef/abcdef1234…/ (result directories)
//
// Public URL paths (e.g. /uploads/results/<hash>/<file>) stay unchanged;
// the HTTP handlers resolve them to the partitioned on-disk location.

// IsHexHash reports whether s is a 64-char hex string (SHA-256 hex key).
func IsHexHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// isHexHash is kept as an unexported alias for call sites inside this package.
func isHexHash(s string) bool { return IsHexHash(s) }

// PartPrefix returns the three-level directory prefix for a key,
// e.g. "ab/cd/ef" for key "abcdef…". Returns ("", false) when key
// is shorter than 6 characters.
func PartPrefix(key string) (string, bool) {
       key = strings.TrimSpace(key)
       if len(key) < 6 {
               return "", false
       }
       return filepath.Join(key[0:2], key[2:4], key[4:6]), true
}

// PartPath returns the partitioned filesystem path: base/ab/cd/ef/key.
// Falls back to base/key when key is too short for partitioning.
func PartPath(base, key string) string {
       prefix, ok := PartPrefix(key)
       if !ok {
               return filepath.Join(base, key)
       }
       return filepath.Join(base, prefix, key)
}

// PartResolve looks up key under base, checking the partitioned location
// first (base/ab/cd/ef/key) then the legacy flat location (base/key).
// Returns the first path that exists on disk. If neither exists it returns
// the partitioned path so callers that create new entries get the new layout.
func PartResolve(base, key string) string {
       partitioned := PartPath(base, key)
       if _, err := os.Stat(partitioned); err == nil {
               return partitioned
       }
       flat := filepath.Join(base, key)
       if _, err := os.Stat(flat); err == nil {
               return flat
       }
       return partitioned
}

// PartMkdirAll creates the partitioned directory base/ab/cd/ef/key with
// intermediate parents and returns its path.
func PartMkdirAll(base, key string, perm os.FileMode) (string, error) {
       p := PartPath(base, key)
       return p, os.MkdirAll(p, perm)
}

// ResolveUploadRelPath translates a URL-relative path (the portion after
// "/uploads/") into an absolute filesystem path, honoring the partition
// layout. It handles two shapes:
//
//     "<hash>"                       → uploadsDir/ab/cd/ef/<hash>
//     "results/<hash>/…"             → uploadsDir/results/ab/cd/ef/<hash>/…
//
// For non-hash names (e.g. timestamp-prefixed inscriptions) it falls back
// to the flat path.
func ResolveUploadRelPath(uploadsDir, relPath string) string {
       // results/<key>[/rest…]
       if strings.HasPrefix(relPath, "results/") {
               after := strings.TrimPrefix(relPath, "results/")
               slash := strings.IndexByte(after, '/')
               var key, rest string
               if slash >= 0 {
                       key = after[:slash]
                       rest = after[slash+1:]
               } else {
                       key = after
               }
               if isHexHash(key) {
                       resolved := PartResolve(filepath.Join(uploadsDir, "results"), key)
                       if rest != "" {
                               return filepath.Join(resolved, rest)
                       }
                       return resolved
               }
       }

       // Top-level hash file
       slash := strings.IndexByte(relPath, '/')
       var key string
       if slash >= 0 {
               key = relPath[:slash]
       } else {
               key = relPath
       }
       if isHexHash(key) {
               resolved := PartResolve(uploadsDir, key)
               if slash >= 0 {
                       return filepath.Join(resolved, relPath[slash+1:])
               }
               return resolved
       }

       // Non-hash fallback (timestamp-prefixed etc.)
       return filepath.Join(uploadsDir, relPath)
}

// ---------------------------------------------------------------------------
// Migration
// ---------------------------------------------------------------------------

const migrateMarker = ".partition-v1"

// MigrateUploads moves flat hash-named files under base/ and hash-named
// directories under base/results/ into three-level partitioned sub-trees.
// It is idempotent: once the marker file exists, subsequent calls are no-ops.
func MigrateUploads(base string) error {
       base = filepath.Clean(base)
       if base == "" {
               return nil
       }
       marker := filepath.Join(base, migrateMarker)
       if _, err := os.Stat(marker); err == nil {
               return nil // already migrated
       }

       // --- flat hash files in base/ ----------------------------------------
       entries, err := os.ReadDir(base)
       if err != nil {
               if os.IsNotExist(err) {
                       return nil
               }
               return fmt.Errorf("read uploads dir: %w", err)
       }
       moved := 0
       for _, e := range entries {
               name := e.Name()
               if e.IsDir() || !isHexHash(name) {
                       continue
               }
               dst := PartPath(base, name)
               if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
                       return fmt.Errorf("mkdir partition for %s: %w", name, err)
               }
               if err := os.Rename(filepath.Join(base, name), dst); err != nil {
                       return fmt.Errorf("rename %s: %w", name, err)
               }
               moved++
       }

       // --- hash-named directories in base/results/ -------------------------
       resultsDir := filepath.Join(base, "results")
       entries, err = os.ReadDir(resultsDir)
       if err != nil {
               if os.IsNotExist(err) {
                       return os.WriteFile(marker, []byte("migrated\n"), 0644)
               }
               return fmt.Errorf("read results dir: %w", err)
       }
       for _, e := range entries {
               name := e.Name()
               if !e.IsDir() || !isHexHash(name) {
                       continue
               }
               dst := PartPath(resultsDir, name)
               if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
                       return fmt.Errorf("mkdir partition for results/%s: %w", name, err)
               }
               if err := os.Rename(filepath.Join(resultsDir, name), dst); err != nil {
                       return fmt.Errorf("rename results/%s: %w", name, err)
               }
               moved++
       }

       log.Printf("partition: migrated %d entries under %s", moved, base)
       return os.WriteFile(marker, []byte("migrated\n"), 0644)
}
