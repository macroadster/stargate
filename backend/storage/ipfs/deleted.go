package ipfs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Mirror tombstones are persisted under UPLOADS_DIR by Mirror.UnpinPath (and
// by the pruning scripts). They are the authoritative, cross-process record of
// content that must not be imported again. The ingest-sync path stages blobs
// straight from peer manifests, bypassing Mirror.downloadEntry's in-memory
// isDeleted check, so it needs to consult these files directly.
var deletedIndex = struct {
	sync.Mutex
	files map[string]deletedIndexEntry
}{files: make(map[string]deletedIndexEntry)}

type deletedIndexEntry struct {
	modTime int64
	size    int64
	paths   map[string]bool
}

var mirrorDeletedFiles = []string{
	".ipfs-mirror-deleted.json",
	".ipfs-wish-mirror-deleted.json",
}

// IsPathDeleted reports whether path is tombstoned in either the durable
// uploads mirror or the wish mirror. Logical hash paths, partitioned on-disk
// paths (ab/cd/ef/<hash>), and wish- prefixed ids are all accepted.
//
// A fresh read is keyed by file mtime/size, so tombstones added while the
// process is running (or by an external script) take effect without a restart.
func IsPathDeleted(path string) bool {
	logical := logicalMirrorPath(normalizeWishHash(path))
	if logical == "" || logical == "." {
		return false
	}
	for _, name := range mirrorDeletedFiles {
		paths, ok := loadDeletedPaths(name)
		if !ok {
			continue
		}
		if paths[logical] {
			return true
		}
	}
	return false
}

func loadDeletedPaths(name string) (map[string]bool, bool) {
	uploadsDir := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
	if uploadsDir == "" {
		return nil, false
	}
	path := filepath.Join(uploadsDir, name)
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}

	deletedIndex.Lock()
	defer deletedIndex.Unlock()
	if entry, ok := deletedIndex.files[path]; ok &&
		entry.modTime == info.ModTime().UnixNano() && entry.size == info.Size() {
		return entry.paths, true
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var payload struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, false
	}

	paths := make(map[string]bool, len(payload.Paths))
	for _, rel := range payload.Paths {
		logical := logicalMirrorPath(normalizeWishHash(rel))
		if logical == "" || logical == "." {
			continue
		}
		paths[logical] = true
	}
	deletedIndex.files[path] = deletedIndexEntry{
		modTime: info.ModTime().UnixNano(),
		size:    info.Size(),
		paths:   paths,
	}
	return paths, true
}
