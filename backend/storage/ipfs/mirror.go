package ipfs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"stargate-backend/storage/datadir"
)

type MirrorConfig struct {
	Enabled           bool
	UploadEnabled     bool
	DownloadEnabled   bool
	APIURL            string
	Topic             string
	UploadsDir        string
	PollInterval      time.Duration
	PublishInterval   time.Duration
	MaxFiles          int
	HTTPTimeout       time.Duration
	ManifestVersion   int
	ManifestFileName  string
	AnnouncementLabel string
	// WishOnly publishes only tracked inscribed wishes (stargate-wishes).
	// The durable uploads mirror leaves those files off its manifest so
	// NAT/relay peers still get a periodic inventory on the wish topic.
	WishOnly bool
	// OnFileDownloaded is copied to the Mirror before goroutines start,
	// so there is no race between the subscribe loop and callback setup.
	OnFileDownloaded func(ctx context.Context, ev FileDownloadedEvent)
}

// FileDownloadedEvent is passed to the OnFileDownloaded callback when the
// mirror finishes downloading a new file from a remote peer.
type FileDownloadedEvent struct {
	Path     string // relative path within UploadsDir
	CID      string // IPFS CID
	FilePath string // absolute path on disk
	Size     int64
}

type Mirror struct {
	cfg              MirrorConfig
	client           *http.Client
	streamClient     *http.Client
	ipfsClient       *Client
	peerID           string
	lastPublished    string
	lastPublishAt    time.Time
	lastSeenRemote   string
	mu               sync.Mutex
	knownFiles       map[string]fileState
	deletedFiles     map[string]bool
	ingestByCID      map[string]*remoteIngest
	onFileDownloaded func(ctx context.Context, ev FileDownloadedEvent)
}

// remoteIngest tracks one peer catalog. A heartbeat of the same CID must
// retry missing files; "seen" is not "complete".
type remoteIngest struct {
	complete bool
	missing  []manifestEntry
	files    []manifestEntry
}

const maxRemoteIngest = 64

// OnFileDownloaded registers a callback invoked every time the mirror
// downloads a new file.  Use this to trigger ingestion without a separate
// IPFS pubsub subscription.

type fileState struct {
	Size    int64
	ModTime int64
	CID     string
}

type manifest struct {
	Version   int             `json:"version"`
	Origin    string          `json:"origin"`
	CreatedAt int64           `json:"created_at"`
	Files     []manifestEntry `json:"files"`
}

type manifestEntry struct {
	Path    string `json:"path"`
	CID     string `json:"cid"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
}

type announcement struct {
	Type        string `json:"type"`
	ManifestCID string `json:"manifest_cid"`
	Origin      string `json:"origin"`
	Timestamp   int64  `json:"timestamp"`
}

type MirrorStatus struct {
	Enabled           bool     `json:"enabled"`
	PeerID            string   `json:"peer_id,omitempty"`
	Topic             string   `json:"topic,omitempty"`
	WishTopic         string   `json:"wish_topic,omitempty"`
	Topics            []string `json:"topics,omitempty"`
	UploadsDir        string   `json:"uploads_dir,omitempty"`
	LastPublishedCID  string   `json:"last_published_cid,omitempty"`
	LastPublishAt     int64    `json:"last_publish_at,omitempty"`
	LastSeenRemoteCID string   `json:"last_seen_remote_cid,omitempty"`
	KnownFiles        int      `json:"known_files,omitempty"`
	// Wish* fields are filled by the combined /api/ipfs-mirror/status
	// handler when the wish file-mirror is running.
	WishEnabled           bool   `json:"wish_enabled,omitempty"`
	WishLastPublishedCID  string `json:"wish_last_published_cid,omitempty"`
	WishLastPublishAt     int64  `json:"wish_last_publish_at,omitempty"`
	WishLastSeenRemoteCID string `json:"wish_last_seen_remote_cid,omitempty"`
	WishKnownFiles        int    `json:"wish_known_files,omitempty"`
}

type pubsubMessage struct {
	From string `json:"from"`
	Data string `json:"data"`
}

func (m *Mirror) Status() MirrorStatus {
	if m == nil {
		return MirrorStatus{Enabled: false}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	st := MirrorStatus{
		Enabled:           m.cfg.Enabled,
		PeerID:            m.peerID,
		UploadsDir:        m.cfg.UploadsDir,
		Topics:            AllowedPubsubTopics(),
		LastPublishedCID:  m.lastPublished,
		LastPublishAt:     m.lastPublishAt.Unix(),
		LastSeenRemoteCID: m.lastSeenRemote,
		KnownFiles:        len(m.knownFiles),
	}
	if m.cfg.WishOnly {
		st.WishTopic = m.cfg.Topic
		if st.WishTopic == "" {
			st.WishTopic = WishTopic()
		}
		st.Topic = MirrorTopic()
		st.WishEnabled = m.cfg.Enabled
		st.WishLastPublishedCID = m.lastPublished
		st.WishLastPublishAt = m.lastPublishAt.Unix()
		st.WishLastSeenRemoteCID = m.lastSeenRemote
		st.WishKnownFiles = len(m.knownFiles)
		// Per-mirror callers should not treat wish inventory as uploads.
		st.LastPublishedCID = ""
		st.LastPublishAt = 0
		st.LastSeenRemoteCID = ""
		st.KnownFiles = 0
	} else {
		st.Topic = m.cfg.Topic
		st.WishTopic = WishTopic()
	}
	return st
}

// MergeMirrorStatus combines the durable uploads mirror and the wish
// file-mirror into the single /api/ipfs-mirror/status payload.
func MergeMirrorStatus(uploads, wishes *Mirror) MirrorStatus {
	if uploads == nil && wishes == nil {
		return MirrorStatus{Enabled: false, Topics: AllowedPubsubTopics(), Topic: MirrorTopic(), WishTopic: WishTopic()}
	}
	st := MirrorStatus{Topic: MirrorTopic(), WishTopic: WishTopic(), Topics: AllowedPubsubTopics()}
	if uploads != nil {
		st = uploads.Status()
	}
	if wishes == nil {
		return st
	}
	ws := wishes.Status()
	if !st.Enabled {
		st.Enabled = ws.Enabled
		if st.PeerID == "" {
			st.PeerID = ws.PeerID
		}
		if st.UploadsDir == "" {
			st.UploadsDir = ws.UploadsDir
		}
	}
	st.WishTopic = ws.WishTopic
	if st.WishTopic == "" {
		st.WishTopic = WishTopic()
	}
	st.WishEnabled = ws.WishEnabled || ws.Enabled
	st.WishLastPublishedCID = ws.WishLastPublishedCID
	st.WishLastPublishAt = ws.WishLastPublishAt
	st.WishLastSeenRemoteCID = ws.WishLastSeenRemoteCID
	st.WishKnownFiles = ws.WishKnownFiles
	if len(st.Topics) == 0 {
		st.Topics = AllowedPubsubTopics()
	}
	return st
}

func (m *Mirror) UnpinPath(ctx context.Context, path string) error {
	if m == nil {
		return nil
	}
	rel := strings.TrimSpace(path)
	if rel == "" {
		return nil
	}
	if filepath.IsAbs(rel) {
		if m.cfg.UploadsDir == "" {
			return nil
		}
		if r, err := filepath.Rel(m.cfg.UploadsDir, rel); err == nil {
			rel = r
		}
	}
	// Normalize to the logical mirror path (hash basename, not ab/cd/ef/hash).
	rel = logicalMirrorPath(filepath.ToSlash(filepath.Clean(rel)))
	m.mu.Lock()
	state, ok := m.knownFiles[rel]
	if ok {
		delete(m.knownFiles, rel)
	}
	m.deletedFiles[rel] = true
	m.persistDeletedLocked()
	m.mu.Unlock()
	if !ok || state.CID == "" {
		return nil
	}
	return m.unpinCID(ctx, state.CID)
}

func (m *Mirror) isDeleted(path string) bool {
	if m == nil {
		return false
	}
	rel := filepath.ToSlash(filepath.Clean(path))
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deletedFiles[rel]
}

func (m *Mirror) deletedFilePath() string {
	if m == nil || m.cfg.UploadsDir == "" {
		return ""
	}
	name := ".ipfs-mirror-deleted.json"
	if m.cfg.WishOnly {
		// Separate tombstone file so expiring a wish does not prevent
		// the durable uploads mirror from adopting it after promotion,
		// and so upload deletes do not hide live wishes.
		name = ".ipfs-wish-mirror-deleted.json"
	}
	return filepath.Join(m.cfg.UploadsDir, name)
}

func (m *Mirror) loadDeleted() {
	path := m.deletedFilePath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var payload struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}
	m.mu.Lock()
	for _, rel := range payload.Paths {
		rel = filepath.ToSlash(filepath.Clean(rel))
		if rel == "" || rel == "." {
			continue
		}
		m.deletedFiles[rel] = true
	}
	m.mu.Unlock()
}

func (m *Mirror) persistDeletedLocked() {
	path := m.deletedFilePath()
	if path == "" {
		return
	}
	paths := make([]string, 0, len(m.deletedFiles))
	for rel := range m.deletedFiles {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	payload, err := json.Marshal(struct {
		Paths []string `json:"paths"`
	}{Paths: paths})
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ipfs-mirror-deleted-*")
	if err != nil {
		return
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	_ = os.Rename(tmp.Name(), path)
}

func LoadMirrorConfig() MirrorConfig {
	uploadsDir := os.Getenv("UPLOADS_DIR")

	return MirrorConfig{
		Enabled:           envBool("IPFS_MIRROR_ENABLED", true),
		UploadEnabled:     envBool("IPFS_MIRROR_UPLOAD_ENABLED", true),
		DownloadEnabled:   envBool("IPFS_MIRROR_DOWNLOAD_ENABLED", true),
		APIURL:            envString("IPFS_API_URL", "http://127.0.0.1:5001"),
		Topic:             MirrorTopic(),
		UploadsDir:        uploadsDir,
		PollInterval:      envDurationSeconds("IPFS_MIRROR_POLL_INTERVAL_SEC", 10),
		PublishInterval:   envDurationSeconds("IPFS_MIRROR_PUBLISH_INTERVAL_SEC", 30),
		MaxFiles:          envInt("IPFS_MIRROR_MAX_FILES", 2000),
		HTTPTimeout:       envDurationSeconds("IPFS_HTTP_TIMEOUT_SEC", 30),
		ManifestVersion:   1,
		ManifestFileName:  "stargate-uploads-manifest.json",
		AnnouncementLabel: "stargate-uploads",
	}
}

// LoadWishMirrorConfig is the ephemeral inscribed-wish file mirror. Same
// enable/timing flags as the uploads mirror; different topic, manifest,
// and file filter (tracked wishes only). NAT/relay peers need this
// periodic inventory — a one-shot CID announce is not enough.
func LoadWishMirrorConfig() MirrorConfig {
	cfg := LoadMirrorConfig()
	cfg.Topic = WishTopic()
	cfg.WishOnly = true
	cfg.ManifestFileName = "stargate-wishes-manifest.json"
	cfg.AnnouncementLabel = "stargate-wishes"
	return cfg
}

func StartMirror(ctx context.Context, cfg MirrorConfig) (*Mirror, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	if cfg.UploadsDir == "" {
		return nil, fmt.Errorf("uploads dir is required")
	}
	if err := os.MkdirAll(cfg.UploadsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create uploads dir: %w", err)
	}

	if cfg.APIURL == "" {
		cfg.APIURL = "http://127.0.0.1:5001"
	}

	ipfsClient := NewClientFromEnv()

	m := &Mirror{
		cfg:              cfg,
		client:           &http.Client{Timeout: cfg.HTTPTimeout},
		streamClient:     &http.Client{},
		ipfsClient:       ipfsClient,
		knownFiles:       make(map[string]fileState),
		deletedFiles:     make(map[string]bool),
		ingestByCID:      make(map[string]*remoteIngest),
		onFileDownloaded: cfg.OnFileDownloaded,
	}
	m.loadDeleted()

	peerID, err := m.fetchPeerID(ctx)
	if err != nil {
		log.Printf("IPFS mirror: local node not reachable (%v), mirror will be in standby/fallback mode", err)
		m.cfg.UploadEnabled = false
		m.cfg.DownloadEnabled = false
		return m, nil
	}
	m.peerID = peerID

	if err := m.ensurePubsubReady(ctx); err != nil {
		log.Printf("IPFS mirror: pubsub not supported or ready (%v), pubsub features disabled", err)
		m.cfg.UploadEnabled = false
		m.cfg.DownloadEnabled = false
	}

	kind := "uploads"
	if m.cfg.WishOnly {
		kind = "wishes"
	}
	log.Printf("IPFS mirror enabled (kind=%s peer=%s topic=%s uploads=%s)", kind, m.peerID, m.cfg.Topic, m.cfg.UploadsDir)

	if cfg.UploadEnabled {
		go m.publishLoop(ctx)
	}
	if cfg.DownloadEnabled {
		go m.subscribeLoop(ctx)
	}

	return m, nil
}

func (m *Mirror) ensurePubsubReady(ctx context.Context) error {
	// Embedded node always has pubsub available
	if m.ipfsClient != nil && m.ipfsClient.embedded != nil {
		return nil
	}

	reqURL := fmt.Sprintf("%s/api/v0/pubsub/ls", strings.TrimRight(m.cfg.APIURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ipfs pubsub not ready: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (m *Mirror) publishLoop(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed, err := m.scanAndAdd(ctx)
			if err != nil {
				log.Printf("IPFS mirror scan failed: %v", err)
				continue
			}

			if !changed && time.Since(m.lastPublishAt) < m.cfg.PublishInterval {
				continue
			}

			// Unchanged inventory: re-announce the existing CID so NAT/relay
			// peers can catch up. Do not remint — CreatedAt used to be
			// time.Now(), which minted a unique object every 30s.
			if !changed && m.lastPublished != "" {
				if err := m.announceManifest(ctx, m.lastPublished); err != nil {
					log.Printf("IPFS mirror announce failed: %v", err)
					continue
				}
				m.lastPublishAt = time.Now()
				log.Printf("IPFS mirror announced manifest: %s (changed=false files=%d)", m.lastPublished, m.knownFileCount())
				continue
			}

			manifestCID, err := m.publishManifest(ctx)
			if err != nil {
				log.Printf("IPFS mirror publish failed: %v", err)
			} else if manifestCID != "" {
				m.lastPublished = manifestCID
				m.lastPublishAt = time.Now()
				log.Printf("IPFS mirror published manifest: %s (changed=%t files=%d)", manifestCID, changed, m.knownFileCount())
			}
		}
	}
}

func (m *Mirror) subscribeLoop(ctx context.Context) {
	for {
		if err := m.subscribeOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("IPFS mirror subscribe error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
	}
}

func (m *Mirror) scanAndAdd(ctx context.Context) (bool, error) {
	changed := false
	count := 0

	err := filepath.WalkDir(m.cfg.UploadsDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(m.cfg.UploadsDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		// Directory policy after the three-level partition layout:
		//   - walk into ab/cd/ef hash partitions (so hash files are found)
		//   - skip results/ and every other non-partition tree (sandboxes,
		//     nested execution output, etc. must not be mirrored)
		if entry.IsDir() {
			if path == m.cfg.UploadsDir {
				return nil
			}
			if isPartitionDirRel(rel) {
				return nil
			}
			return filepath.SkipDir
		}

		if m.cfg.MaxFiles > 0 && count >= m.cfg.MaxFiles {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}

		// Manifest/wire path stays the logical name (hash basename), not the
		// on-disk partition prefix. Peers still advertise path="<hash>" so
		// public URLs remain /uploads/<hash> and older nodes stay compatible.
		logical := logicalMirrorPath(rel)

		// Uploads mirror: skip tracked wishes (they live on the wish
		// file-mirror). Wish mirror: skip everything else.
		if !m.includeLogical(logical) {
			m.mu.Lock()
			if _, ok := m.knownFiles[logical]; ok {
				delete(m.knownFiles, logical)
				changed = true
			}
			m.mu.Unlock()
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return nil
		}

		state := fileState{
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
		}

		m.mu.Lock()
		prev, ok := m.knownFiles[logical]
		m.mu.Unlock()

		if ok && prev.Size == state.Size && prev.ModTime == state.ModTime {
			count++
			return nil
		}

		cid, err := m.addFile(ctx, path, entry.Name())
		if err != nil {
			log.Printf("IPFS mirror add failed for %s: %v", logical, err)
			return nil
		}

		state.CID = cid
		m.mu.Lock()
		m.knownFiles[logical] = state
		m.mu.Unlock()
		if m.cfg.WishOnly {
			TrackWish(logical, cid, time.Unix(state.ModTime, 0))
		}

		changed = true
		count++
		return nil
	})

	if err != nil {
		return changed, err
	}

	return changed, nil
}

func (m *Mirror) includeLogical(logical string) bool {
	if m == nil {
		return false
	}
	isWish := IsTrackedWish(logical)
	if m.cfg.WishOnly {
		return isWish
	}
	return !isWish
}

// isPartitionDirRel reports whether rel (slash-separated, relative to
// UploadsDir) is a directory that is part of the three-level ab/cd/ef
// partition layout and should be walked into.
func isPartitionDirRel(rel string) bool {
	if rel == "" || rel == "." {
		return false
	}
	parts := strings.Split(rel, "/")
	if len(parts) == 0 || len(parts) > 3 {
		return false
	}
	for _, p := range parts {
		if !isTwoHex(p) {
			return false
		}
	}
	return true
}

func isTwoHex(s string) bool {
	if len(s) != 2 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// logicalMirrorPath normalizes an on-disk relative path to the logical
// mirror path used in manifests and knownFiles.
//
//	ab/cd/ef/<64-hex>  →  <64-hex>
//	<64-hex>           →  <64-hex>
//	other/file         →  other/file (unchanged)
func logicalMirrorPath(rel string) string {
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." || rel == "" {
		return rel
	}
	base := filepath.Base(rel)
	if datadir.IsHexHash(base) {
		// Accept both flat and ab/cd/ef/<hash> (or accidental deeper paths).
		return base
	}
	return rel
}

// resolveMirrorTarget maps a logical (or legacy) mirror path to the on-disk
// write/read location under UploadsDir, honoring the partition layout for
// hash-keyed files.
func resolveMirrorTarget(uploadsDir, rel string) (string, bool) {
	logical := logicalMirrorPath(rel)
	if logical == "" || logical == "." {
		return "", false
	}
	if datadir.IsHexHash(logical) {
		// Prefer existing location (partitioned or flat), default to partitioned.
		return datadir.PartResolve(uploadsDir, logical), true
	}
	return safeJoin(uploadsDir, logical)
}

// resolveMirrorWriteTarget is like resolveMirrorTarget but always returns the
// partitioned path for new hash files (never the flat legacy path).
func resolveMirrorWriteTarget(uploadsDir, rel string) (string, bool) {
	logical := logicalMirrorPath(rel)
	if logical == "" || logical == "." {
		return "", false
	}
	if datadir.IsHexHash(logical) {
		return datadir.PartPath(uploadsDir, logical), true
	}
	return safeJoin(uploadsDir, logical)
}

func (m *Mirror) publishManifest(ctx context.Context) (string, error) {
	manifestCID, err := m.createManifest(ctx)
	if err != nil {
		return "", err
	}
	if manifestCID == "" {
		return "", nil
	}
	if err := m.announceManifest(ctx, manifestCID); err != nil {
		return "", err
	}
	return manifestCID, nil
}

// announceManifest publishes a small pubsub pointer at ManifestCID.
// Timestamp lives here (not in the IPFS object) so heartbeats stay cheap.
func (m *Mirror) announceManifest(ctx context.Context, manifestCID string) error {
	if manifestCID == "" {
		return nil
	}
	payload, err := json.Marshal(announcement{
		Type:        m.cfg.AnnouncementLabel,
		ManifestCID: manifestCID,
		Origin:      m.peerID,
		Timestamp:   time.Now().Unix(),
	})
	if err != nil {
		return err
	}
	return m.pubsubPublish(ctx, payload)
}

func (m *Mirror) createManifest(ctx context.Context) (string, error) {
	payload, err := json.Marshal(m.manifestSnapshot())
	if err != nil {
		return "", err
	}
	return m.addBytes(ctx, m.cfg.ManifestFileName, payload)
}

// manifestSnapshot is content-addressed: same inventory ⇒ same bytes ⇒ same CID.
// CreatedAt is the newest file mtime, not wall-clock, so periodic heartbeats
// do not mint a new object.
func (m *Mirror) manifestSnapshot() manifest {
	m.mu.Lock()
	entries := make([]manifestEntry, 0, len(m.knownFiles))
	var createdAt int64
	for path, state := range m.knownFiles {
		entries = append(entries, manifestEntry{
			Path:    path,
			CID:     state.CID,
			Size:    state.Size,
			ModTime: state.ModTime,
		})
		if state.ModTime > createdAt {
			createdAt = state.ModTime
		}
	}
	peerID := m.peerID
	version := m.cfg.ManifestVersion
	m.mu.Unlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	return manifest{
		Version:   version,
		Origin:    peerID,
		CreatedAt: createdAt,
		Files:     entries,
	}
}

func (m *Mirror) subscribeOnce(ctx context.Context) error {
	// Use the global IPFS client for pubsub when embedded node is available
	if m.ipfsClient != nil && m.ipfsClient.embedded != nil {
		ch, err := m.ipfsClient.PubsubSubscribe(ctx, m.cfg.Topic)
		if err != nil {
			return err
		}
		for data := range ch {
			var msg pubsubMessage
			if json.Unmarshal(data, &msg) == nil && msg.From == m.peerID {
				continue
			}
			// Try as raw announcement first, then as pubsub message wrapper
			encoded := string(data)
			if msg.Data != "" {
				encoded = msg.Data
			}
			manifestCID, err := m.extractManifestCID(encoded)
			if err != nil {
				log.Printf("IPFS mirror message decode failed: %v", err)
				continue
			}
			m.ingestRemoteManifest(ctx, manifestCID)
		}
		return nil
	}

	// Fallback to direct HTTP API
	reqURL := fmt.Sprintf("%s/api/v0/pubsub/sub?arg=%s", strings.TrimRight(m.cfg.APIURL, "/"), url.QueryEscape(multibaseEncodeString(m.cfg.Topic)))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return err
	}

	client := m.streamClient
	if client == nil {
		client = m.client
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if len(body) == 0 {
			return fmt.Errorf("pubsub subscribe failed: %s", resp.Status)
		}
		return fmt.Errorf("pubsub subscribe failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	decoder := json.NewDecoder(resp.Body)
	for {
		var msg pubsubMessage
		if err := decoder.Decode(&msg); err != nil {
			return err
		}
		if msg.From == m.peerID {
			continue
		}
		manifestCID, err := m.extractManifestCID(msg.Data)
		if err != nil {
			log.Printf("IPFS mirror message decode failed: %v", err)
			continue
		}
		m.ingestRemoteManifest(ctx, manifestCID)
	}
}

// ingestRemoteManifest pulls a peer catalog. Same-CID heartbeats retry
// only when the previous pass left missing files.
func (m *Mirror) ingestRemoteManifest(ctx context.Context, manifestCID string) {
	if m == nil || manifestCID == "" {
		return
	}
	m.lastSeenRemote = manifestCID
	if m.remoteIngestComplete(manifestCID) {
		return
	}
	if err := m.processManifest(ctx, manifestCID); err != nil {
		log.Printf("IPFS mirror sync failed: %v", err)
	}
}

func (m *Mirror) remoteIngestComplete(manifestCID string) bool {
	if m == nil || manifestCID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.ingestByCID[manifestCID]
	return ok && st.complete
}

func (m *Mirror) processManifest(ctx context.Context, manifestCID string) error {
	all, todo, err := m.remoteManifestWork(ctx, manifestCID)
	if err != nil {
		return err
	}

	downloaded := 0
	skipped := 0
	var missing []manifestEntry
	for _, entry := range todo {
		if entry.Path == "" || entry.CID == "" {
			continue
		}
		applied, err := m.downloadEntry(ctx, entry)
		if err != nil {
			missing = append(missing, entry)
			log.Printf("IPFS mirror download failed for %s: %v", entry.Path, err)
			continue
		}
		if applied {
			downloaded++
		} else {
			skipped++
		}
	}

	failed := len(missing)
	m.rememberIngest(manifestCID, all, missing)
	log.Printf("IPFS mirror synced manifest: %s (downloaded=%d skipped=%d failed=%d)", manifestCID, downloaded, skipped, failed)
	if failed > 0 {
		return fmt.Errorf("incomplete ingest %s: failed=%d", manifestCID, failed)
	}
	return nil
}

func (m *Mirror) remoteManifestWork(ctx context.Context, manifestCID string) (all, todo []manifestEntry, err error) {
	m.mu.Lock()
	if st, ok := m.ingestByCID[manifestCID]; ok && len(st.files) > 0 {
		all = append([]manifestEntry(nil), st.files...)
		if len(st.missing) > 0 {
			todo = append([]manifestEntry(nil), st.missing...)
		} else {
			todo = append([]manifestEntry(nil), st.files...)
		}
		m.mu.Unlock()
		return all, todo, nil
	}
	m.mu.Unlock()

	data, err := m.cat(ctx, manifestCID)
	if err != nil {
		return nil, nil, err
	}
	var incoming manifest
	if err := json.Unmarshal(data, &incoming); err != nil {
		return nil, nil, fmt.Errorf("invalid manifest: %w", err)
	}
	return incoming.Files, incoming.Files, nil
}

func (m *Mirror) rememberIngest(manifestCID string, files, missing []manifestEntry) {
	if m == nil || manifestCID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ingestByCID == nil {
		m.ingestByCID = make(map[string]*remoteIngest)
	}
	m.ingestByCID[manifestCID] = &remoteIngest{
		complete: len(missing) == 0,
		missing:  append([]manifestEntry(nil), missing...),
		files:    append([]manifestEntry(nil), files...),
	}
	if len(m.ingestByCID) <= maxRemoteIngest {
		return
	}
	for cid, st := range m.ingestByCID {
		if st.complete && cid != manifestCID {
			delete(m.ingestByCID, cid)
			if len(m.ingestByCID) <= maxRemoteIngest {
				return
			}
		}
	}
	for cid := range m.ingestByCID {
		if cid != manifestCID {
			delete(m.ingestByCID, cid)
			if len(m.ingestByCID) <= maxRemoteIngest {
				return
			}
		}
	}
}

func (m *Mirror) downloadEntry(ctx context.Context, entry manifestEntry) (bool, error) {
	logical := logicalMirrorPath(entry.Path)
	if m.isDeleted(logical) || m.isDeleted(entry.Path) {
		return false, nil
	}

	// Skip if the file already exists at partitioned or flat location.
	if existing, ok := resolveMirrorTarget(m.cfg.UploadsDir, logical); ok {
		if info, err := os.Stat(existing); err == nil && info.Size() == entry.Size {
			// Track under the logical key so later scans/publishes stay consistent.
			m.mu.Lock()
			m.knownFiles[logical] = fileState{
				Size:    entry.Size,
				ModTime: entry.ModTime,
				CID:     entry.CID,
			}
			m.mu.Unlock()
			if m.cfg.WishOnly {
				created := time.Now()
				if entry.ModTime > 0 {
					created = time.Unix(entry.ModTime, 0)
				}
				TrackWish(logical, entry.CID, created)
			}
			return false, nil
		}
	}

	// New files go into the partitioned layout for hash keys.
	target, ok := resolveMirrorWriteTarget(m.cfg.UploadsDir, logical)
	if !ok {
		return false, fmt.Errorf("invalid path: %s", entry.Path)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return false, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".ipfs-mirror-*")
	if err != nil {
		return false, err
	}

	catErr := m.catToWriter(ctx, entry.CID, tmp)
	closeErr := tmp.Close()
	if catErr != nil {
		_ = os.Remove(tmp.Name())
		return false, catErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp.Name())
		return false, closeErr
	}

	if err := os.Rename(tmp.Name(), target); err != nil {
		_ = os.Remove(tmp.Name())
		return false, err
	}
	if entry.ModTime > 0 {
		modTime := time.Unix(entry.ModTime, 0)
		_ = os.Chtimes(target, modTime, modTime)
	}

	m.mu.Lock()
	m.knownFiles[logical] = fileState{
		Size:    entry.Size,
		ModTime: entry.ModTime,
		CID:     entry.CID,
	}
	fn := m.onFileDownloaded
	m.mu.Unlock()
	if m.cfg.WishOnly {
		created := time.Now()
		if entry.ModTime > 0 {
			created = time.Unix(entry.ModTime, 0)
		}
		TrackWish(logical, entry.CID, created)
	}

	if fn != nil {
		fn(ctx, FileDownloadedEvent{
			Path:     logical,
			CID:      entry.CID,
			FilePath: target,
			Size:     entry.Size,
		})
	}

	return true, nil
}

func (m *Mirror) knownFileCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.knownFiles)
}

func (m *Mirror) extractManifestCID(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}

	candidates := make([][]byte, 0, 4)
	candidates = append(candidates, []byte(encoded))
	if decoded := decodeMultibasePayload([]byte(encoded)); len(decoded) > 0 {
		candidates = append(candidates, decoded)
	}
	if decoded := decodeBase64Payload(encoded); len(decoded) > 0 {
		candidates = append(candidates, decoded)
		if decoded2 := decodeMultibasePayload(decoded); len(decoded2) > 0 {
			candidates = append(candidates, decoded2)
		}
	}

	for _, payload := range candidates {
		if cid := parseAnnouncementPayload(payload); cid != "" {
			return cid, nil
		}
	}

	return "", nil
}

func parseAnnouncementPayload(payload []byte) string {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return ""
	}

	var ann announcement
	if err := json.Unmarshal(payload, &ann); err == nil && ann.ManifestCID != "" {
		return ann.ManifestCID
	}

	return ""
}

func multibaseEncodeString(value string) string {
	return multibaseEncodeBytes([]byte(value))
}

func multibaseEncodeBytes(value []byte) string {
	encoded := base64.RawURLEncoding.EncodeToString(value)
	return "u" + encoded
}

func decodeMultibasePayload(raw []byte) []byte {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != 'u' {
		return nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(string(raw[1:]))
	if err != nil {
		return nil
	}
	return decoded
}

func decodeBase64Payload(encoded string) []byte {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, enc := range encodings {
		if decoded, err := enc.DecodeString(encoded); err == nil {
			return decoded
		}
	}
	return nil
}

func (m *Mirror) fetchPeerID(ctx context.Context) (string, error) {
	// Try the global IPFS client first (uses embedded node when available)
	if m.ipfsClient != nil {
		if id := m.ipfsClient.PeerID(ctx); id != "" {
			return id, nil
		}
	}

	// Fallback to direct HTTP API
	reqURL := fmt.Sprintf("%s/api/v0/id", strings.TrimRight(m.cfg.APIURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var payload struct {
		ID string `json:"ID"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.ID == "" {
		return "", fmt.Errorf("ipfs id missing")
	}
	return payload.ID, nil
}

func (m *Mirror) pubsubPublish(ctx context.Context, message []byte) error {
	// Use the global IPFS client when embedded node is available
	if m.ipfsClient != nil {
		return m.ipfsClient.PubsubPublish(ctx, m.cfg.Topic, message)
	}

	// Fallback to direct HTTP API
	topic := url.QueryEscape(multibaseEncodeString(m.cfg.Topic))
	reqURL := fmt.Sprintf("%s/api/v0/pubsub/pub?arg=%s", strings.TrimRight(m.cfg.APIURL, "/"), topic)

	body, contentType, err := multipartBody("data", "data", message)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if len(body) == 0 {
			return fmt.Errorf("pubsub publish failed: %s", resp.Status)
		}
		return fmt.Errorf("pubsub publish failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func multipartBody(fieldName string, filename string, payload []byte) (io.Reader, string, error) {
	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)
	part, err := writer.CreateFormFile(fieldName, filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(payload); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buf, writer.FormDataContentType(), nil
}

func (m *Mirror) addFile(ctx context.Context, path string, name string) (string, error) {
	// Use the global IPFS client when available (tries embedded node first)
	if m.ipfsClient != nil {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return m.ipfsClient.AddBytes(ctx, name, data)
	}

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	return m.addStream(ctx, name, file)
}

func (m *Mirror) addBytes(ctx context.Context, name string, data []byte) (string, error) {
	// Use the global IPFS client when available (tries embedded node first)
	if m.ipfsClient != nil {
		return m.ipfsClient.AddBytes(ctx, name, data)
	}
	return m.addStream(ctx, name, bytes.NewReader(data))
}

func (m *Mirror) addStream(ctx context.Context, name string, reader io.Reader) (string, error) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()
		part, err := writer.CreateFormFile("file", name)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, reader); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = writer.Close()
	}()

	reqURL := fmt.Sprintf("%s/api/v0/add?pin=true&cid-version=1", strings.TrimRight(m.cfg.APIURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, pr)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := m.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var lastHash string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var entry struct {
			Hash string `json:"Hash"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err == nil && entry.Hash != "" {
			lastHash = entry.Hash
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if lastHash == "" {
		return "", fmt.Errorf("ipfs add returned empty hash")
	}
	return lastHash, nil
}

func (m *Mirror) unpinCID(ctx context.Context, cid string) error {
	reqURL := fmt.Sprintf("%s/api/v0/pin/rm?arg=%s", strings.TrimRight(m.cfg.APIURL, "/"), url.QueryEscape(cid))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ipfs unpin failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (m *Mirror) cat(ctx context.Context, cid string) ([]byte, error) {
	// Use the global IPFS client when available (tries embedded node first)
	if m.ipfsClient != nil {
		return m.ipfsClient.Cat(ctx, cid)
	}

	// Fallback to direct HTTP API
	reqURL := fmt.Sprintf("%s/api/v0/cat?arg=%s", strings.TrimRight(m.cfg.APIURL, "/"), url.QueryEscape(cid))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (m *Mirror) catToWriter(ctx context.Context, cid string, w io.Writer) error {
	// Use the global IPFS client when available (tries embedded node first)
	if m.ipfsClient != nil {
		data, err := m.ipfsClient.Cat(ctx, cid)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}

	// Fallback to direct HTTP API
	reqURL := fmt.Sprintf("%s/api/v0/cat?arg=%s", strings.TrimRight(m.cfg.APIURL, "/"), url.QueryEscape(cid))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if _, err := io.Copy(w, resp.Body); err != nil {
		return err
	}
	return nil
}

func safeJoin(baseDir string, relPath string) (string, bool) {
	clean := filepath.Clean(filepath.FromSlash(relPath))
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", false
	}
	joined := filepath.Join(baseDir, clean)
	if !strings.HasPrefix(joined, filepath.Clean(baseDir)+string(os.PathSeparator)) && filepath.Clean(baseDir) != joined {
		return "", false
	}
	return joined, true
}

func envString(key string, fallback string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(val)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDurationSeconds(key string, fallback int) time.Duration {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return time.Duration(fallback) * time.Second
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return time.Duration(fallback) * time.Second
	}
	if parsed <= 0 {
		return time.Duration(fallback) * time.Second
	}
	return time.Duration(parsed) * time.Second
}
