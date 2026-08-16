package ipfs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"stargate-backend/storage/datadir"
)

const wishIndexFile = "ipfs_wishes.json"

// WishRecord tracks an inscribed wish that has not yet been promoted to the
// durable uploads topic (PSBT built / engagement).
type WishRecord struct {
	Hash      string `json:"hash"`
	CID       string `json:"cid,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

type wishIndexFilePayload struct {
	Wishes map[string]WishRecord `json:"wishes"`
}

type wishIndex struct {
	mu    sync.Mutex
	path  string
	items map[string]WishRecord
}

var (
	globalWishIndex   *wishIndex
	wishIndexOnce     sync.Once
	wishIndexTestPath string
	wishIndexTestMu   sync.Mutex
)

func wishIndexPath() string {
	wishIndexTestMu.Lock()
	defer wishIndexTestMu.Unlock()
	if wishIndexTestPath != "" {
		return wishIndexTestPath
	}
	return datadir.Path(wishIndexFile)
}

// ResetWishIndexForTest points the in-process wish index at path.
func ResetWishIndexForTest(path string) {
	resetWishIndexForTest(path)
}

func resetWishIndexForTest(path string) {
	wishIndexTestMu.Lock()
	wishIndexTestPath = path
	wishIndexTestMu.Unlock()
	globalWishIndex = nil
	wishIndexOnce = sync.Once{}
}

func getWishIndex() *wishIndex {
	wishIndexOnce.Do(func() {
		idx := &wishIndex{
			path:  wishIndexPath(),
			items: make(map[string]WishRecord),
		}
		idx.load()
		globalWishIndex = idx
	})
	return globalWishIndex
}

func (idx *wishIndex) load() {
	data, err := os.ReadFile(idx.path)
	if err != nil {
		return
	}
	var payload wishIndexFilePayload
	if err := json.Unmarshal(data, &payload); err != nil || payload.Wishes == nil {
		return
	}
	idx.items = payload.Wishes
}

func (idx *wishIndex) persistLocked() {
	if idx.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(idx.path), 0o755); err != nil {
		return
	}
	payload := wishIndexFilePayload{Wishes: idx.items}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	tmp := idx.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, idx.path)
}

// TrackWish records an inscribed wish so the uploads mirror skips it and the
// 7-day GC can expire it if nothing engages.
func TrackWish(hash, cid string, createdAt time.Time) {
	hash = normalizeWishHash(hash)
	if hash == "" {
		return
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	idx := getWishIndex()
	idx.mu.Lock()
	defer idx.mu.Unlock()
	rec := idx.items[hash]
	rec.Hash = hash
	if cid != "" {
		rec.CID = cid
	}
	if rec.CreatedAt == 0 {
		rec.CreatedAt = createdAt.Unix()
	}
	idx.items[hash] = rec
	idx.persistLocked()
}

// UntrackWish promotes a wish off the ephemeral topic (PSBT built or engaged).
func UntrackWish(hash string) {
	hash = normalizeWishHash(hash)
	if hash == "" {
		return
	}
	idx := getWishIndex()
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if _, ok := idx.items[hash]; !ok {
		return
	}
	delete(idx.items, hash)
	idx.persistLocked()
}

// IsTrackedWish reports whether hash is still an ephemeral inscribed wish.
func IsTrackedWish(hash string) bool {
	hash = normalizeWishHash(hash)
	if hash == "" {
		return false
	}
	idx := getWishIndex()
	idx.mu.Lock()
	defer idx.mu.Unlock()
	_, ok := idx.items[hash]
	return ok
}

// LookupWish returns the tracked record for hash, if any.
func LookupWish(hash string) (WishRecord, bool) {
	hash = normalizeWishHash(hash)
	if hash == "" {
		return WishRecord{}, false
	}
	idx := getWishIndex()
	idx.mu.Lock()
	defer idx.mu.Unlock()
	rec, ok := idx.items[hash]
	return rec, ok
}

// ListTrackedWishes returns a snapshot of ephemeral inscribed wishes.
func ListTrackedWishes() []WishRecord {
	idx := getWishIndex()
	idx.mu.Lock()
	defer idx.mu.Unlock()
	out := make([]WishRecord, 0, len(idx.items))
	for _, rec := range idx.items {
		out = append(out, rec)
	}
	return out
}

func normalizeWishHash(hash string) string {
	hash = strings.TrimSpace(hash)
	hash = strings.TrimPrefix(hash, "wish-")
	return hash
}
