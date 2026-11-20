package smart_contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stargate-backend/storage/datadir"
	"stargate-backend/storage/ipfs"
)

type stubCatClient struct {
	objects map[string][]byte
}

func (s stubCatClient) Cat(_ context.Context, cid string) ([]byte, error) {
	blob, ok := s.objects[cid]
	if !ok {
		return nil, fmt.Errorf("missing object %s", cid)
	}
	return blob, nil
}

func writeWishTombstone(t *testing.T, uploadsDir string, paths []string) {
	t.Helper()
	payload, err := json.Marshal(struct {
		Paths []string `json:"paths"`
	}{Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploadsDir, ".ipfs-wish-mirror-deleted.json"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A deleted wish may still appear in a peer's stargate-wishes manifest. The
// staging path must refuse it, otherwise TrackWish re-tracks it and the
// contract is recreated on the next ingest.
func TestIngestProcessManifestSkipsTombstonedWish(t *testing.T) {
	isolateWishIndex(t)
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)

	hash := strings.Repeat("a", 64)
	writeWishTombstone(t, uploadsDir, []string{hash})

	blob := []byte("deleted wish payload")
	sum := sha256.Sum256(blob)
	stegoHash := hex.EncodeToString(sum[:])
	manifest, err := json.Marshal(ipfsIngestManifest{
		Version: 1,
		Origin:  "peer",
		Files: []ipfsIngestManifestEntry{{
			Path:    hash,
			CID:     "cid-wish-blob",
			Size:    int64(len(blob)),
			ModTime: 123,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := stubCatClient{objects: map[string][]byte{
		"cid-manifest":  manifest,
		"cid-wish-blob": blob,
	}}

	stage := func() {
		t.Helper()
		state := &ipfsIngestSyncState{
			lastSeen:      make(map[string]int64),
			repairChecked: make(map[string]bool),
		}
		if err := ipfsIngestProcessManifest(context.Background(), nil, nil, ipfsIngestSyncConfig{}, state, client, ipfs.WishTopic(), "cid-manifest"); err != nil {
			t.Fatalf("stage manifest: %v", err)
		}
	}

	stage()
	if got := datadir.PartPath(uploadsDir, stegoHash); fileExists(got) {
		t.Errorf("tombstoned wish was staged at %s", got)
	}
	if ipfs.IsTrackedWish(hash) {
		t.Error("tombstoned wish was re-tracked")
	}

	// Once the tombstone is gone the same manifest is ingested normally.
	writeWishTombstone(t, uploadsDir, nil)
	stage()
	if got := datadir.PartPath(uploadsDir, stegoHash); !fileExists(got) {
		t.Errorf("non-tombstoned wish was not staged at %s", got)
	}
	if !ipfs.IsTrackedWish(hash) {
		t.Error("non-tombstoned wish was not tracked")
	}
}

// A peer can omit or mislabel the manifest path while serving the exact
// payload that was deleted. The content-hash guard must still catch it.
func TestIngestProcessManifestSkipsTombstonedPayload(t *testing.T) {
	isolateWishIndex(t)
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)

	blob := []byte("deleted payload")
	sum := sha256.Sum256(blob)
	stegoHash := hex.EncodeToString(sum[:])
	mislabeledPath := strings.Repeat("b", 64)
	writeWishTombstone(t, uploadsDir, []string{stegoHash})

	manifest, err := json.Marshal(ipfsIngestManifest{
		Version: 1,
		Files: []ipfsIngestManifestEntry{{
			Path:    mislabeledPath,
			CID:     "cid-mislabeled",
			Size:    int64(len(blob)),
			ModTime: 123,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := stubCatClient{objects: map[string][]byte{
		"cid-manifest":   manifest,
		"cid-mislabeled": blob,
	}}
	state := &ipfsIngestSyncState{
		lastSeen:      make(map[string]int64),
		repairChecked: make(map[string]bool),
	}
	if err := ipfsIngestProcessManifest(context.Background(), nil, nil, ipfsIngestSyncConfig{}, state, client, ipfs.WishTopic(), "cid-manifest"); err != nil {
		t.Fatalf("stage manifest: %v", err)
	}
	if got := datadir.PartPath(uploadsDir, stegoHash); fileExists(got) {
		t.Errorf("tombstoned payload was staged at %s", got)
	}
	if ipfs.IsTrackedWish(mislabeledPath) {
		t.Error("wish serving a tombstoned payload was re-tracked")
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
