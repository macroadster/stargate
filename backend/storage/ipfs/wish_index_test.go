package ipfs

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWishIndexTrackUntrack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ipfs_wishes.json")
	resetWishIndexForTest(path)

	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if IsTrackedWish(hash) {
		t.Fatal("expected untracked")
	}
	TrackWish(hash, "bafytestcid", time.Unix(1_700_000_000, 0))
	if !IsTrackedWish(hash) {
		t.Fatal("expected tracked")
	}
	if !IsTrackedWish("wish-" + hash) {
		t.Fatal("wish- prefix should normalize")
	}
	rec, ok := LookupWish(hash)
	if !ok || rec.CID != "bafytestcid" || rec.CreatedAt != 1_700_000_000 {
		t.Fatalf("lookup: %+v ok=%v", rec, ok)
	}

	// Reloading from disk should keep the record.
	resetWishIndexForTest(path)
	if !IsTrackedWish(hash) {
		t.Fatal("expected persisted track")
	}

	UntrackWish(hash)
	if IsTrackedWish(hash) {
		t.Fatal("expected untracked after UntrackWish")
	}
}
