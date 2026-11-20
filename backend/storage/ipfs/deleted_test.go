package ipfs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDeletedFile(t *testing.T, uploadsDir, name string, paths []string) {
	t.Helper()
	payload, err := json.Marshal(struct {
		Paths []string `json:"paths"`
	}{Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploadsDir, name), payload, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIsPathDeleted(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)

	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	other := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	writeDeletedFile(t, uploadsDir, ".ipfs-wish-mirror-deleted.json", []string{hash})

	partitioned := filepath.ToSlash(filepath.Join("ab", "cd", "ef", hash))
	for _, tc := range []struct {
		name string
		path string
		want bool
	}{
		{"bare hash", hash, true},
		{"partitioned path", partitioned, true},
		{"wish- prefixed", "wish-" + hash, true},
		{"unknown hash", other, false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPathDeleted(tc.path); got != tc.want {
				t.Errorf("IsPathDeleted(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}

	// The durable uploads tombstone is consulted too.
	writeDeletedFile(t, uploadsDir, ".ipfs-mirror-deleted.json", []string{other})
	if !IsPathDeleted(other) {
		t.Errorf("hash in uploads tombstone not reported deleted")
	}
}

func TestIsPathDeletedPicksUpNewTombstonesWithoutRestart(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)

	first := strings.Repeat("a", 64)
	second := strings.Repeat("b", 64)
	path := filepath.Join(uploadsDir, ".ipfs-wish-mirror-deleted.json")
	writeDeletedFile(t, uploadsDir, filepath.Base(path), []string{first})
	if !IsPathDeleted(first) {
		t.Fatal("initial tombstone not loaded")
	}

	// Append a second path. mtime+size cache must be invalidated.
	writeDeletedFile(t, uploadsDir, filepath.Base(path), []string{first, second})
	if !IsPathDeleted(second) {
		t.Error("newly added tombstone not picked up (cache not invalidated)")
	}
}

func TestIsPathDeletedIgnoresMalformedTombstone(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)
	if err := os.WriteFile(filepath.Join(uploadsDir, ".ipfs-mirror-deleted.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsPathDeleted(strings.Repeat("c", 64)) {
		t.Error("malformed tombstone must not report everything deleted")
	}
}
