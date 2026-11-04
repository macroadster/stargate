package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ResolveConsoleURLs accepts a base URL, a full /api/console/status URL, or a
// /api/health URL and returns the origin plus the status URL.
func ResolveConsoleURLs(base string) (root, status string, err error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", "", errors.New("empty URL")
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", "", fmt.Errorf("invalid URL %q", base)
	}
	u.RawQuery = ""
	u.Fragment = ""
	path := strings.TrimRight(u.Path, "/")
	switch {
	case strings.HasSuffix(path, "/api/console/status"):
		u.Path = strings.TrimSuffix(path, "/api/console/status")
	case strings.HasSuffix(path, "/api/health"):
		u.Path = strings.TrimSuffix(path, "/api/health")
	case strings.HasSuffix(path, "/bitcoin/v1/health"):
		u.Path = strings.TrimSuffix(path, "/bitcoin/v1/health")
	default:
		u.Path = path
	}
	root = strings.TrimRight(u.String(), "/")
	return root, root + "/api/console/status", nil
}

// FillURLs sets empty UI links from the node origin.
func FillURLs(s *Snapshot, root string) {
	if s == nil || root == "" {
		return
	}
	if s.UIURL == "" {
		s.UIURL = root
	}
	if s.DocsURL == "" {
		s.DocsURL = root + "/docs"
	}
	if s.APIURL == "" {
		s.APIURL = root + "/api/"
	}
	if s.MCPURL == "" {
		s.MCPURL = root + "/mcp/tools"
	}
	if s.HealthURL == "" {
		s.HealthURL = root + "/api/health"
	}
}

// Unreachable is the sample drawn when the attached console cannot reach a node.
func Unreachable(base string, err error) Snapshot {
	root, _, rerr := ResolveConsoleURLs(base)
	if rerr != nil {
		root = strings.TrimSpace(base)
	}
	s := Snapshot{
		Phase:        "down",
		HealthStatus: "down",
		Detail:       "node is not responding",
		Reachable:    false,
		SampledAt:    time.Now(),
	}
	if err != nil {
		s.FetchError = err.Error()
	}
	FillURLs(&s, root)
	return s
}

// Fetch reads GET /api/console/status. A node that predates that route is
// read from /api/health, /bitcoin/v1/health, and /api/ipfs-mirror/status.
func Fetch(base string, timeout time.Duration) (Snapshot, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	root, statusURL, err := ResolveConsoleURLs(base)
	if err != nil {
		return Snapshot{}, err
	}
	client := &http.Client{Timeout: timeout}
	body, code, err := getJSON(client, statusURL)
	if err != nil {
		return Snapshot{}, err
	}
	if code == http.StatusOK {
		var snap Snapshot
		if err := json.Unmarshal(body, &snap); err != nil {
			return Snapshot{}, fmt.Errorf("decode %s: %w", statusURL, err)
		}
		snap.Reachable = true
		FillURLs(&snap, root)
		if snap.HealthStatus == "" {
			DeriveHealth(&snap, snap.Phase)
		}
		return snap, nil
	}
	if code != http.StatusNotFound {
		return Snapshot{}, fmt.Errorf("GET %s: HTTP %d", statusURL, code)
	}

	snap := Snapshot{
		Reachable: true,
		Phase:     PhaseReady,
		Detail:    "partial status from /api/health",
	}
	FillURLs(&snap, root)
	if body, code, err := getJSON(client, root+"/api/health"); err == nil && code == http.StatusOK {
		applyHealthJSON(&snap, body)
	}
	if body, code, err := getJSON(client, root+"/bitcoin/v1/health"); err == nil && code == http.StatusOK {
		applyBitcoinHealth(&snap, body)
	}
	if body, code, err := getJSON(client, root+"/api/ipfs-mirror/status"); err == nil && code == http.StatusOK {
		applyMirrorJSON(&snap, body)
	}
	DeriveHealth(&snap, PhaseReady)
	snap.SampledAt = time.Now()
	return snap, nil
}

func getJSON(client *http.Client, rawURL string) ([]byte, int, error) {
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

type healthBody struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Scanner struct {
		ModelLoaded  bool   `json:"model_loaded"`
		ModelVersion string `json:"model_version"`
	} `json:"scanner"`
	Bitcoin struct {
		NodeConnected bool   `json:"node_connected"`
		NodeURL       string `json:"node_url"`
		BlockHeight   int64  `json:"block_height"`
	} `json:"bitcoin"`
	Network string `json:"network"`
	TipLag  *struct {
		LocalTip     int64 `json:"local_tip"`
		ExternalTip  int64 `json:"external_tip"`
		LagBlocks    int64 `json:"lag_blocks"`
		Lagging      bool  `json:"lagging"`
		HashMismatch bool  `json:"hash_mismatch"`
	} `json:"tip_lag"`
}

func applyHealthJSON(s *Snapshot, body []byte) {
	var h healthBody
	if json.Unmarshal(body, &h) != nil {
		return
	}
	if h.Status != "" {
		s.HealthStatus = h.Status
	}
	if s.Version == "" {
		s.Version = h.Version
	}
	s.ScannerReady = h.Scanner.ModelLoaded
	s.ScannerVersion = h.Scanner.ModelVersion
	s.BitcoinConnected = h.Bitcoin.NodeConnected
	// /api/health has no IBD bit. A connected node is treated as synced
	// until a richer payload says otherwise.
	if h.Bitcoin.NodeConnected {
		s.Synced = true
	}
	if h.Bitcoin.BlockHeight > 0 {
		s.BlockHeight = h.Bitcoin.BlockHeight
	}
	if h.Network != "" {
		s.Network = h.Network
	}
}

func applyBitcoinHealth(s *Snapshot, body []byte) {
	var h healthBody
	if json.Unmarshal(body, &h) != nil {
		return
	}
	applyHealthJSON(s, body)
	if h.Network != "" {
		s.Network = h.Network
	}
	if h.TipLag != nil {
		s.TipLocal = h.TipLag.LocalTip
		s.TipExternal = h.TipLag.ExternalTip
		s.TipLagBlocks = h.TipLag.LagBlocks
		s.TipLagging = h.TipLag.Lagging
		s.TipHashMismatch = h.TipLag.HashMismatch
	}
	// /bitcoin/v1/health nests scanner_manager.scanner_type. Read it loosely.
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return
	}
	if mgr, ok := raw["scanner_manager"].(map[string]any); ok {
		if t, ok := mgr["scanner_type"].(string); ok && t != "" {
			s.ScannerType = t
		}
	}
}

func applyMirrorJSON(s *Snapshot, body []byte) {
	var m struct {
		Enabled        bool     `json:"enabled"`
		PeerID         string   `json:"peer_id"`
		KnownFiles     int      `json:"known_files"`
		WishEnabled    bool     `json:"wish_enabled"`
		WishKnownFiles int      `json:"wish_known_files"`
		SwarmPeers     []string `json:"swarm_peers"`
		UploadsMesh    []string `json:"uploads_mesh_peers"`
		WishMesh       []string `json:"wish_mesh_peers"`
	}
	if json.Unmarshal(body, &m) != nil {
		return
	}
	s.MirrorEnabled = m.Enabled
	s.WishMirrorOn = m.WishEnabled
	s.MirrorPeerID = m.PeerID
	s.MirrorUploads = m.KnownFiles
	s.MirrorWishes = m.WishKnownFiles
	s.MirrorPeers = len(m.SwarmPeers)
	if s.MirrorPeers == 0 {
		s.MirrorPeers = len(m.UploadsMesh)
	}
	if s.MirrorPeers == 0 {
		s.MirrorPeers = len(m.WishMesh)
	}
}

// PollSource fetches on an interval and serves the last sample to the screen.
type PollSource struct {
	Interval time.Duration
	Fetch    func() Snapshot

	mu      sync.RWMutex
	last    Snapshot
	started bool
}

// Start begins polling until ctx is done. The first fetch runs immediately.
func (p *PollSource) Start(ctx context.Context) {
	if p == nil {
		return
	}
	if p.Interval <= 0 {
		p.Interval = 2 * time.Second
	}
	go func() {
		p.pull()
		t := time.NewTicker(p.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.pull()
			}
		}
	}()
}

func (p *PollSource) pull() {
	if p.Fetch == nil {
		return
	}
	s := p.Fetch()
	p.mu.Lock()
	p.last = s
	p.started = true
	p.mu.Unlock()
}

// Snapshot returns the last fetched sample, or a starting sample before the
// first fetch completes.
func (p *PollSource) Snapshot() Snapshot {
	if p == nil {
		return Snapshot{HealthStatus: "starting", Phase: PhaseStarting}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.started {
		return Snapshot{HealthStatus: "starting", Phase: PhaseStarting, Detail: "contacting node"}
	}
	return p.last
}
