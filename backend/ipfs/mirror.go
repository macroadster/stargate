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
}

type Mirror struct {
	cfg            MirrorConfig
	client         *http.Client
	streamClient   *http.Client
	peerID         string
	lastPublished  string
	lastPublishAt  time.Time
	lastSeenRemote string
	mu             sync.Mutex
	knownFiles     map[string]fileState
}

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
	Enabled           bool   `json:"enabled"`
	PeerID            string `json:"peer_id,omitempty"`
	Topic             string `json:"topic,omitempty"`
	UploadsDir        string `json:"uploads_dir,omitempty"`
	LastPublishedCID  string `json:"last_published_cid,omitempty"`
	LastPublishAt     int64  `json:"last_publish_at,omitempty"`
	LastSeenRemoteCID string `json:"last_seen_remote_cid,omitempty"`
	KnownFiles        int    `json:"known_files,omitempty"`
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

	return MirrorStatus{
		Enabled:           m.cfg.Enabled,
		PeerID:            m.peerID,
		Topic:             m.cfg.Topic,
		UploadsDir:        m.cfg.UploadsDir,
		LastPublishedCID:  m.lastPublished,
		LastPublishAt:     m.lastPublishAt.Unix(),
		LastSeenRemoteCID: m.lastSeenRemote,
		KnownFiles:        len(m.knownFiles),
	}
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
	rel = filepath.ToSlash(filepath.Clean(rel))
	m.mu.Lock()
	state, ok := m.knownFiles[rel]
	if ok {
		delete(m.knownFiles, rel)
	}
	m.mu.Unlock()
	if !ok || state.CID == "" {
		return nil
	}
	return m.unpinCID(ctx, state.CID)
}

func LoadMirrorConfig() MirrorConfig {
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}

	return MirrorConfig{
		Enabled:           envBool("IPFS_MIRROR_ENABLED", false),
		UploadEnabled:     envBool("IPFS_MIRROR_UPLOAD_ENABLED", true),
		DownloadEnabled:   envBool("IPFS_MIRROR_DOWNLOAD_ENABLED", true),
		APIURL:            envString("IPFS_API_URL", "http://127.0.0.1:5001"),
		Topic:             envString("IPFS_MIRROR_TOPIC", "stargate-uploads"),
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

	m := &Mirror{
		cfg:          cfg,
		client:       &http.Client{Timeout: cfg.HTTPTimeout},
		streamClient: &http.Client{},
		knownFiles:   make(map[string]fileState),
	}

	peerID, err := m.fetchPeerID(ctx)
	if err != nil {
		return nil, err
	}
	m.peerID = peerID

	if err := m.ensurePubsubReady(ctx); err != nil {
		return nil, err
	}

	log.Printf("IPFS mirror enabled (peer=%s topic=%s uploads=%s)", m.peerID, m.cfg.Topic, m.cfg.UploadsDir)

	if cfg.UploadEnabled {
		go m.publishLoop(ctx)
	}
	if cfg.DownloadEnabled {
		go m.subscribeLoop(ctx)
	}

	return m, nil
}

func (m *Mirror) ensurePubsubReady(ctx context.Context) error {
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

			manifestCID, err := m.publishManifest(ctx)
			if err != nil {
				log.Printf("IPFS mirror publish failed: %v", err)
			} else if manifestCID != "" {
				m.lastPublished = manifestCID
				m.lastPublishAt = time.Now()
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
		if entry.IsDir() {
			return nil
		}
		if m.cfg.MaxFiles > 0 && count >= m.cfg.MaxFiles {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}

		rel, err := filepath.Rel(m.cfg.UploadsDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		info, err := entry.Info()
		if err != nil {
			return nil
		}

		state := fileState{
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
		}

		m.mu.Lock()
		prev, ok := m.knownFiles[rel]
		m.mu.Unlock()

		if ok && prev.Size == state.Size && prev.ModTime == state.ModTime {
			count++
			return nil
		}

		cid, err := m.addFile(ctx, path, entry.Name())
		if err != nil {
			log.Printf("IPFS mirror add failed for %s: %v", rel, err)
			return nil
		}

		state.CID = cid
		m.mu.Lock()
		m.knownFiles[rel] = state
		m.mu.Unlock()

		changed = true
		count++
		return nil
	})

	if err != nil {
		return changed, err
	}

	return changed, nil
}

func (m *Mirror) publishManifest(ctx context.Context) (string, error) {
	manifestCID, err := m.createManifest(ctx)
	if err != nil {
		return "", err
	}
	if manifestCID == "" || manifestCID == m.lastPublished {
		return manifestCID, nil
	}

	msg := announcement{
		Type:        m.cfg.AnnouncementLabel,
		ManifestCID: manifestCID,
		Origin:      m.peerID,
		Timestamp:   time.Now().Unix(),
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}

	if err := m.pubsubPublish(ctx, payload); err != nil {
		return "", err
	}

	return manifestCID, nil
}

func (m *Mirror) createManifest(ctx context.Context) (string, error) {
	m.mu.Lock()
	entries := make([]manifestEntry, 0, len(m.knownFiles))
	for path, state := range m.knownFiles {
		entries = append(entries, manifestEntry{
			Path:    path,
			CID:     state.CID,
			Size:    state.Size,
			ModTime: state.ModTime,
		})
	}
	m.mu.Unlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	payload, err := json.Marshal(manifest{
		Version:   m.cfg.ManifestVersion,
		Origin:    m.peerID,
		CreatedAt: time.Now().Unix(),
		Files:     entries,
	})
	if err != nil {
		return "", err
	}

	return m.addBytes(ctx, m.cfg.ManifestFileName, payload)
}

func (m *Mirror) subscribeOnce(ctx context.Context) error {
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
		if manifestCID == "" {
			continue
		}
		if manifestCID == "" || manifestCID == m.lastSeenRemote {
			continue
		}
		if err := m.processManifest(ctx, manifestCID); err != nil {
			log.Printf("IPFS mirror sync failed: %v", err)
		} else {
			m.lastSeenRemote = manifestCID
		}
	}
}

func (m *Mirror) processManifest(ctx context.Context, manifestCID string) error {
	data, err := m.cat(ctx, manifestCID)
	if err != nil {
		return err
	}

	var incoming manifest
	if err := json.Unmarshal(data, &incoming); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	for _, entry := range incoming.Files {
		if entry.Path == "" || entry.CID == "" {
			continue
		}
		if err := m.downloadEntry(ctx, entry); err != nil {
			log.Printf("IPFS mirror download failed for %s: %v", entry.Path, err)
		}
	}

	return nil
}

func (m *Mirror) downloadEntry(ctx context.Context, entry manifestEntry) error {
	target, ok := safeJoin(m.cfg.UploadsDir, entry.Path)
	if !ok {
		return fmt.Errorf("invalid path: %s", entry.Path)
	}

	if info, err := os.Stat(target); err == nil {
		if info.Size() == entry.Size {
			return nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".ipfs-mirror-*")
	if err != nil {
		return err
	}

	catErr := m.catToWriter(ctx, entry.CID, tmp)
	closeErr := tmp.Close()
	if catErr != nil {
		_ = os.Remove(tmp.Name())
		return catErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp.Name())
		return closeErr
	}

	if err := os.Rename(tmp.Name(), target); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if entry.ModTime > 0 {
		modTime := time.Unix(entry.ModTime, 0)
		_ = os.Chtimes(target, modTime, modTime)
	}

	m.mu.Lock()
	m.knownFiles[entry.Path] = fileState{
		Size:    entry.Size,
		ModTime: entry.ModTime,
		CID:     entry.CID,
	}
	m.mu.Unlock()

	return nil
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
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	return m.addStream(ctx, name, file)
}

func (m *Mirror) addBytes(ctx context.Context, name string, data []byte) (string, error) {
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
