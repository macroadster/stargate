package ipfs

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"stargate-backend/storage/datadir"
)

const testHash = "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

func TestLogicalMirrorPath(t *testing.T) {
	partRel := filepath.ToSlash(filepath.Join("ab", "cd", "ef", testHash))
	tests := []struct {
		in   string
		want string
	}{
		{testHash, testHash},
		{partRel, testHash},
		{"plain-file.png", "plain-file.png"},
		{"nested/plain-file.png", "nested/plain-file.png"},
		{"./" + testHash, testHash},
	}
	for _, tt := range tests {
		got := logicalMirrorPath(tt.in)
		if got != tt.want {
			t.Errorf("logicalMirrorPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsPartitionDirRel(t *testing.T) {
	tests := []struct {
		rel  string
		want bool
	}{
		{"ab", true},
		{"ab/cd", true},
		{"ab/cd/ef", true},
		{"ab/cd/ef/extra", false},
		{"results", false},
		{"results/ab", false},
		{"abc", false},
		{"", false},
		{".", false},
	}
	for _, tt := range tests {
		got := isPartitionDirRel(tt.rel)
		if got != tt.want {
			t.Errorf("isPartitionDirRel(%q) = %v, want %v", tt.rel, got, tt.want)
		}
	}
}

func TestResolveMirrorWriteTarget_Partitioned(t *testing.T) {
	base := t.TempDir()
	target, ok := resolveMirrorWriteTarget(base, testHash)
	if !ok {
		t.Fatal("expected ok")
	}
	want := datadir.PartPath(base, testHash)
	if target != want {
		t.Fatalf("write target = %q, want %q", target, want)
	}
}

func TestResolveMirrorTarget_ExistingFlat(t *testing.T) {
	base := t.TempDir()
	flat := filepath.Join(base, testHash)
	if err := os.WriteFile(flat, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	target, ok := resolveMirrorTarget(base, testHash)
	if !ok {
		t.Fatal("expected ok")
	}
	if target != flat {
		t.Fatalf("resolve existing flat = %q, want %q", target, flat)
	}
}

func TestScanAndAdd_FindsPartitionedHashFiles(t *testing.T) {
	uploads := t.TempDir()
	storage := t.TempDir()

	// Partitioned hash file (the post-migration layout).
	partPath := datadir.PartPath(uploads, testHash)
	if err := os.MkdirAll(filepath.Dir(partPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partPath, []byte("stego-image-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	// Nested results tree must be skipped.
	resultsFile := filepath.Join(uploads, "results", testHash, "out.txt")
	if err := os.MkdirAll(filepath.Dir(resultsFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resultsFile, []byte("sandbox"), 0644); err != nil {
		t.Fatal(err)
	}

	// Root-level non-hash file should still be mirrored.
	rootFile := filepath.Join(uploads, "inscription-123.png")
	if err := os.WriteFile(rootFile, []byte("root-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	m := &Mirror{
		cfg: MirrorConfig{
			UploadsDir: uploads,
			MaxFiles:   100,
		},
		client:       &http.Client{Timeout: 2 * time.Second},
		knownFiles:   make(map[string]fileState),
		deletedFiles: make(map[string]bool),
		ipfsClient: &Client{
			apiURL:     "http://127.0.0.1:9", // unreachable → local fallback
			client:     &http.Client{Timeout: 200 * time.Millisecond},
			storageDir: storage,
		},
	}

	changed, err := m.scanAndAdd(context.Background())
	if err != nil {
		t.Fatalf("scanAndAdd: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true when new files are added")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.knownFiles[testHash]; !ok {
		t.Fatalf("expected partitioned hash file under logical key %q; known=%v", testHash, keysOf(m.knownFiles))
	}
	// Must NOT use the on-disk partition prefix as the wire path.
	partKey := filepath.ToSlash(filepath.Join("ab", "cd", "ef", testHash))
	if _, ok := m.knownFiles[partKey]; ok {
		t.Fatalf("knownFiles must not use partition prefix key %q", partKey)
	}
	if _, ok := m.knownFiles["inscription-123.png"]; !ok {
		t.Fatalf("expected root-level file; known=%v", keysOf(m.knownFiles))
	}
	// results/ tree must not appear.
	for k := range m.knownFiles {
		if filepath.Base(k) == "out.txt" || len(k) > 20 && k[:7] == "results" {
			t.Fatalf("results tree leaked into knownFiles: %q", k)
		}
	}
	if len(m.knownFiles) != 2 {
		t.Fatalf("knownFiles count = %d, want 2; known=%v", len(m.knownFiles), keysOf(m.knownFiles))
	}
}

func TestDownloadEntry_WritesPartitionedPath(t *testing.T) {
	uploads := t.TempDir()
	storage := t.TempDir()

	payload := []byte("mirrored-stego")
	// Use AddBytes local fallback so cat works without a live IPFS node.
	client := &Client{
		apiURL:     "http://127.0.0.1:9",
		client:     &http.Client{Timeout: 200 * time.Millisecond},
		storageDir: storage,
	}
	cid, err := client.AddBytes(context.Background(), "blob", payload)
	if err != nil {
		t.Fatal(err)
	}

	m := &Mirror{
		cfg: MirrorConfig{
			UploadsDir: uploads,
		},
		client:       &http.Client{Timeout: 2 * time.Second},
		knownFiles:   make(map[string]fileState),
		deletedFiles: make(map[string]bool),
		ipfsClient:   client,
	}

	applied, err := m.downloadEntry(context.Background(), manifestEntry{
		Path:    testHash, // logical flat path as published by peers
		CID:     cid,
		Size:    int64(len(payload)),
		ModTime: time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("downloadEntry: %v", err)
	}
	if !applied {
		t.Fatal("expected applied=true")
	}

	wantPath := datadir.PartPath(uploads, testHash)
	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("expected partitioned file at %s: %v", wantPath, err)
	}
	if string(got) != string(payload) {
		t.Fatalf("content mismatch: %q", got)
	}

	// Second download should skip (same size at partitioned location).
	applied, err = m.downloadEntry(context.Background(), manifestEntry{
		Path: testHash,
		CID:  cid,
		Size: int64(len(payload)),
	})
	if err != nil {
		t.Fatalf("second downloadEntry: %v", err)
	}
	if applied {
		t.Fatal("expected skip when partitioned file already exists")
	}
}

func TestDownloadEntry_SkipsExistingFlat(t *testing.T) {
	uploads := t.TempDir()
	storage := t.TempDir()
	payload := []byte("already-flat")

	// Legacy flat location still present (pre-migration peer / partial migrate).
	flat := filepath.Join(uploads, testHash)
	if err := os.WriteFile(flat, payload, 0644); err != nil {
		t.Fatal(err)
	}

	client := &Client{
		apiURL:     "http://127.0.0.1:9",
		client:     &http.Client{Timeout: 200 * time.Millisecond},
		storageDir: storage,
	}
	cid, err := client.AddBytes(context.Background(), "blob", payload)
	if err != nil {
		t.Fatal(err)
	}

	m := &Mirror{
		cfg:          MirrorConfig{UploadsDir: uploads},
		client:       &http.Client{Timeout: 2 * time.Second},
		knownFiles:   make(map[string]fileState),
		deletedFiles: make(map[string]bool),
		ipfsClient:   client,
	}

	applied, err := m.downloadEntry(context.Background(), manifestEntry{
		Path: testHash,
		CID:  cid,
		Size: int64(len(payload)),
	})
	if err != nil {
		t.Fatalf("downloadEntry: %v", err)
	}
	if applied {
		t.Fatal("expected skip when flat file already exists with matching size")
	}
}

func TestMirrorStatusIncludesWishTopic(t *testing.T) {
	t.Setenv("IPFS_MIRROR_TOPIC", DefaultMirrorTopic)
	t.Setenv("IPFS_WISH_TOPIC", DefaultWishTopic)

	m := &Mirror{
		cfg: MirrorConfig{
			Enabled:    true,
			Topic:      DefaultMirrorTopic,
			UploadsDir: t.TempDir(),
		},
		peerID: "12D3KooWtestpeer",
	}
	st := m.Status()
	if st.Topic != DefaultMirrorTopic {
		t.Fatalf("topic=%q want %q", st.Topic, DefaultMirrorTopic)
	}
	if st.WishTopic != DefaultWishTopic {
		t.Fatalf("wish_topic=%q want %q", st.WishTopic, DefaultWishTopic)
	}
	if len(st.Topics) != 2 {
		t.Fatalf("topics=%v want both allowlisted names", st.Topics)
	}
	if st.Topics[0] != DefaultMirrorTopic || st.Topics[1] != DefaultWishTopic {
		t.Fatalf("topics=%v want [%q %q]", st.Topics, DefaultMirrorTopic, DefaultWishTopic)
	}
}

func TestLoadWishMirrorConfig(t *testing.T) {
	t.Setenv("IPFS_MIRROR_TOPIC", DefaultMirrorTopic)
	t.Setenv("IPFS_WISH_TOPIC", DefaultWishTopic)
	t.Setenv("UPLOADS_DIR", t.TempDir())

	uploads := LoadMirrorConfig()
	if uploads.WishOnly || uploads.Topic != DefaultMirrorTopic {
		t.Fatalf("uploads cfg: WishOnly=%v topic=%q", uploads.WishOnly, uploads.Topic)
	}
	if uploads.ManifestFileName != "stargate-uploads-manifest.json" {
		t.Fatalf("uploads manifest=%q", uploads.ManifestFileName)
	}

	wishes := LoadWishMirrorConfig()
	if !wishes.WishOnly || wishes.Topic != DefaultWishTopic {
		t.Fatalf("wish cfg: WishOnly=%v topic=%q", wishes.WishOnly, wishes.Topic)
	}
	if wishes.ManifestFileName != "stargate-wishes-manifest.json" {
		t.Fatalf("wish manifest=%q", wishes.ManifestFileName)
	}
	if wishes.UploadsDir != uploads.UploadsDir {
		t.Fatalf("wish uploads dir %q != uploads %q", wishes.UploadsDir, uploads.UploadsDir)
	}
}

func TestScanAndAdd_WishOnlyIncludesTrackedWishes(t *testing.T) {
	uploads := t.TempDir()
	storage := t.TempDir()
	idx := filepath.Join(t.TempDir(), "wishes.json")
	ResetWishIndexForTest(idx)
	t.Cleanup(func() { ResetWishIndexForTest("") })

	wishHash := testHash
	durableHash := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	for _, hash := range []string{wishHash, durableHash} {
		p := datadir.PartPath(uploads, hash)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("img-"+hash[:8]), 0644); err != nil {
			t.Fatal(err)
		}
	}
	TrackWish(wishHash, "", time.Unix(1_700_000_000, 0))

	newMirror := func(wishOnly bool) *Mirror {
		return &Mirror{
			cfg: MirrorConfig{
				UploadsDir: uploads,
				MaxFiles:   100,
				WishOnly:   wishOnly,
			},
			client:       &http.Client{Timeout: 2 * time.Second},
			knownFiles:   make(map[string]fileState),
			deletedFiles: make(map[string]bool),
			ipfsClient: &Client{
				apiURL:     "http://127.0.0.1:9",
				client:     &http.Client{Timeout: 200 * time.Millisecond},
				storageDir: storage,
			},
		}
	}

	wishMirror := newMirror(true)
	if _, err := wishMirror.scanAndAdd(context.Background()); err != nil {
		t.Fatalf("wish scanAndAdd: %v", err)
	}
	if _, ok := wishMirror.knownFiles[wishHash]; !ok {
		t.Fatalf("wish mirror missing tracked wish; known=%v", keysOf(wishMirror.knownFiles))
	}
	if _, ok := wishMirror.knownFiles[durableHash]; ok {
		t.Fatalf("wish mirror must not include durable file; known=%v", keysOf(wishMirror.knownFiles))
	}

	uploadsMirror := newMirror(false)
	if _, err := uploadsMirror.scanAndAdd(context.Background()); err != nil {
		t.Fatalf("uploads scanAndAdd: %v", err)
	}
	if _, ok := uploadsMirror.knownFiles[durableHash]; !ok {
		t.Fatalf("uploads mirror missing durable file; known=%v", keysOf(uploadsMirror.knownFiles))
	}
	if _, ok := uploadsMirror.knownFiles[wishHash]; ok {
		t.Fatalf("uploads mirror must skip tracked wish; known=%v", keysOf(uploadsMirror.knownFiles))
	}

	// After promotion, the wish leaves the wish inventory and joins uploads.
	UntrackWish(wishHash)
	if _, err := wishMirror.scanAndAdd(context.Background()); err != nil {
		t.Fatalf("wish rescan: %v", err)
	}
	if _, ok := wishMirror.knownFiles[wishHash]; ok {
		t.Fatalf("promoted wish still on wish mirror; known=%v", keysOf(wishMirror.knownFiles))
	}
	if _, err := uploadsMirror.scanAndAdd(context.Background()); err != nil {
		t.Fatalf("uploads rescan: %v", err)
	}
	if _, ok := uploadsMirror.knownFiles[wishHash]; !ok {
		t.Fatalf("promoted wish missing from uploads mirror; known=%v", keysOf(uploadsMirror.knownFiles))
	}
}

func TestMergeMirrorStatus(t *testing.T) {
	t.Setenv("IPFS_MIRROR_TOPIC", DefaultMirrorTopic)
	t.Setenv("IPFS_WISH_TOPIC", DefaultWishTopic)

	uploads := &Mirror{
		cfg: MirrorConfig{
			Enabled:    true,
			Topic:      DefaultMirrorTopic,
			UploadsDir: "/data/uploads",
		},
		peerID:        "12D3KooWuploads",
		lastPublished: "bafyuploads",
		knownFiles:    map[string]fileState{"aaa": {}},
	}
	wishes := &Mirror{
		cfg: MirrorConfig{
			Enabled:    true,
			WishOnly:   true,
			Topic:      DefaultWishTopic,
			UploadsDir: "/data/uploads",
		},
		peerID:        "12D3KooWwishes",
		lastPublished: "bafywishes",
		knownFiles:    map[string]fileState{"bbb": {}, "ccc": {}},
	}

	st := MergeMirrorStatus(uploads, wishes)
	if !st.Enabled || !st.WishEnabled {
		t.Fatalf("enabled=%v wish_enabled=%v", st.Enabled, st.WishEnabled)
	}
	if st.Topic != DefaultMirrorTopic || st.WishTopic != DefaultWishTopic {
		t.Fatalf("topics topic=%q wish_topic=%q", st.Topic, st.WishTopic)
	}
	if st.LastPublishedCID != "bafyuploads" || st.WishLastPublishedCID != "bafywishes" {
		t.Fatalf("cids uploads=%q wishes=%q", st.LastPublishedCID, st.WishLastPublishedCID)
	}
	if st.KnownFiles != 1 || st.WishKnownFiles != 2 {
		t.Fatalf("counts uploads=%d wishes=%d", st.KnownFiles, st.WishKnownFiles)
	}
	if st.PeerID != "12D3KooWuploads" {
		t.Fatalf("peer_id=%q", st.PeerID)
	}
}

func keysOf(m map[string]fileState) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestManifestSnapshotStableAcrossWallClock(t *testing.T) {
	m := &Mirror{
		cfg:    MirrorConfig{ManifestVersion: 1},
		peerID: "12D3KooWtest",
		knownFiles: map[string]fileState{
			"bbb": {CID: "bafybbb", Size: 20, ModTime: 200},
			"aaa": {CID: "bafyaaa", Size: 10, ModTime: 100},
		},
	}

	first := m.manifestSnapshot()
	time.Sleep(15 * time.Millisecond)
	second := m.manifestSnapshot()

	if first.CreatedAt != 200 {
		t.Fatalf("CreatedAt=%d want newest file mtime 200", first.CreatedAt)
	}
	if first.CreatedAt != second.CreatedAt {
		t.Fatalf("CreatedAt rotated %d -> %d", first.CreatedAt, second.CreatedAt)
	}
	if len(first.Files) != 2 || first.Files[0].Path != "aaa" || first.Files[1].Path != "bbb" {
		t.Fatalf("files not sorted by path: %+v", first.Files)
	}

	a, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("snapshot not content-addressed:\n%s\n%s", a, b)
	}
}

func TestManifestSnapshotChangesWithInventory(t *testing.T) {
	m := &Mirror{
		cfg:    MirrorConfig{ManifestVersion: 1},
		peerID: "12D3KooWtest",
		knownFiles: map[string]fileState{
			"aaa": {CID: "bafyaaa", Size: 10, ModTime: 100},
		},
	}
	before, err := json.Marshal(m.manifestSnapshot())
	if err != nil {
		t.Fatal(err)
	}

	m.mu.Lock()
	m.knownFiles["aaa"] = fileState{CID: "bafyaaa-new", Size: 11, ModTime: 150}
	m.mu.Unlock()

	after, err := json.Marshal(m.manifestSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) == string(after) {
		t.Fatal("snapshot ignored inventory change")
	}
	got := m.manifestSnapshot()
	if got.CreatedAt != 150 {
		t.Fatalf("CreatedAt=%d want 150 after mtime bump", got.CreatedAt)
	}
}
