package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"stargate-backend/agents"
	"stargate-backend/bitcoin"
	"stargate-backend/starlight"
	"stargate-backend/tui"
)

// consoleControl is the in-process sample the status screen and
// GET /api/console/status both read. Collectors publish into hub.
type consoleControl struct {
	hub *tui.Hub

	mu      sync.RWMutex
	chain   *bitcoin.ChainRuntime
	monitor *bitcoin.BlockMonitor
	mirror  *mirrorState

	pub atomic.Bool
}

func newConsoleControl() *consoleControl {
	c := &consoleControl{hub: tui.NewHub()}
	c.hub.SetVersion(versionString())
	c.hub.SetLineFormatter(prettifyLogLine)
	c.hub.SetPhase(tui.PhaseStarting, "preparing data directories")
	c.hub.ApplyStatus(c.build())
	return c
}

// Start refreshes the sample until ctx is canceled. The constructor already
// published once, so the screen has URLs before the first tick.
func (c *consoleControl) Start(ctx context.Context) {
	if c == nil {
		return
	}
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.publish()
			}
		}
	}()
}

func (c *consoleControl) SetPhase(p tui.Phase, detail string) {
	if c == nil {
		return
	}
	c.hub.SetPhase(p, detail)
}

func (c *consoleControl) SetChain(rt *bitcoin.ChainRuntime) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.chain = rt
	c.mu.Unlock()
	c.publish()
}

func (c *consoleControl) SetMonitor(m *bitcoin.BlockMonitor) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.monitor = m
	c.mu.Unlock()
	c.publish()
}

func (c *consoleControl) SetMirror(m *mirrorState) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.mirror = m
	c.mu.Unlock()
	c.publish()
}

func (c *consoleControl) publish() {
	if c == nil || c.hub == nil {
		return
	}
	if !c.pub.CompareAndSwap(false, true) {
		return
	}
	defer c.pub.Store(false)
	c.hub.ApplyStatus(c.build())
}

func (c *consoleControl) build() tui.Snapshot {
	c.mu.RLock()
	chain := c.chain
	mon := c.monitor
	mirror := c.mirror
	c.mu.RUnlock()
	return buildConsoleSnapshot(chain, mon, mirror, c.hub.Phase())
}

func (c *consoleControl) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if c == nil || c.hub == nil {
		http.Error(w, "console unavailable", http.StatusServiceUnavailable)
		return
	}
	snap := c.hub.Snapshot()
	if !requestIsLoopback(r) {
		// Logs and the on-disk data path stay on the machine. Chain height,
		// health, and the browser URL are the same kind of fact as /api/health.
		snap.Logs = nil
		snap.DataDir = ""
	} else if len(snap.Logs) > 40 {
		snap.Logs = append([]string(nil), snap.Logs[len(snap.Logs)-40:]...)
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(snap)
}

func buildConsoleSnapshot(chain *bitcoin.ChainRuntime, mon *bitcoin.BlockMonitor, mirror *mirrorState, phase tui.Phase) tui.Snapshot {
	port := envOr("STARGATE_HTTP_PORT", "3001")
	base := "http://127.0.0.1:" + port
	s := tui.Snapshot{
		HTTPPort:   port,
		UIURL:      base,
		DocsURL:    base + "/docs",
		APIURL:     base + "/api/",
		MCPURL:     base + "/mcp/tools",
		HealthURL:  base + "/api/health",
		Storage:    envOr("STARGATE_STORAGE", "sqlite"),
		DataDir:    envOr("STARGATE_DATA_DIR", "data"),
		Network:    bitcoin.GetCurrentNetwork(),
		ChainMode:  envOr("BTCD_MODE", "managed"),
		Goroutines: runtime.NumGoroutine(),
		Reachable:  true,
	}
	if chain != nil {
		if chain.Mode != "" {
			s.ChainMode = chain.Mode
		}
		if chain.Backend != nil {
			if n := strings.TrimSpace(chain.Backend.Network()); n != "" {
				s.Network = n
			}
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			st, err := chain.Backend.NodeStatus(ctx)
			cancel()
			if err != nil {
				s.ChainError = err.Error()
				if errors.Is(err, context.DeadlineExceeded) {
					s.ChainError = "bitcoin RPC timed out"
				}
			} else {
				applyNodeStatus(&s, st)
			}
		}
	}
	lag := bitcoin.GetTipLagStatus()
	if !lag.CheckedAt.IsZero() {
		s.TipLocal = lag.LocalTip
		s.TipExternal = lag.ExternalTip
		s.TipLagBlocks = lag.LagBlocks
		s.TipLagging = lag.Lagging
		s.TipHashMismatch = lag.HashMismatch
	}
	if mon != nil {
		st := mon.SnapshotStats()
		s.MonitorRunning = st.Running
		s.MonitorHeight = st.CurrentHeight
		s.BlocksProcessed = st.BlocksProcessed
		s.InscriptionsSeen = st.TotalInscriptions
		s.ContractsSeen = st.TotalStegoContracts
		s.ImagesSeen = st.TotalImages
		s.LastProcessMS = st.LastProcess.Milliseconds()
		s.DiscoverEnabled = st.DiscoverEnabled
		s.DiscoverNext = st.DiscoverNext
		s.DiscoverPending = st.DiscoverPending
		s.DiscoverWaiting = st.DiscoverWaiting
	}
	if mirror != nil {
		ms := mirror.Status()
		s.MirrorEnabled = ms.Enabled
		s.WishMirrorOn = ms.WishEnabled
		s.MirrorPeerID = ms.PeerID
		s.MirrorUploads = ms.KnownFiles
		s.MirrorWishes = ms.WishKnownFiles
		peers := len(ms.SwarmPeers)
		if peers == 0 {
			peers = len(ms.UploadsMeshPeers)
		}
		if peers == 0 {
			peers = len(ms.WishMeshPeers)
		}
		s.MirrorPeers = peers
	}
	if sm := starlight.GetScannerManager(); sm != nil {
		s.ScannerType = sm.GetScannerType()
		info := sm.GetScannerInfo()
		s.ScannerVersion = info.ModelVersion
		s.ScannerReady = sm.IsInitialized()
	}
	// Same switch the server uses when it decides to start the orchestrator.
	cfg := agents.LoadConfig()
	s.AgentEnabled = cfg.Enabled
	s.AgentWatcher = cfg.Enabled && cfg.WatcherEnabled
	s.AgentWorker = cfg.Enabled && cfg.WorkerEnabled
	s.AgentTool = cfg.ExecutorTool
	if s.AgentEnabled && s.AgentTool == "" {
		s.AgentTool = "auto"
	}
	if s.AgentEnabled {
		s.AgentID = cfg.AIIdentifier
	}

	switch {
	case s.SyncNote != "":
		s.Detail = s.SyncNote
	case s.ChainError != "":
		s.Detail = s.ChainError
	case phase == tui.PhaseReady || phase == "":
		s.Detail = "listening on :" + port
	}
	tui.DeriveHealth(&s, phase)
	return s
}

func applyNodeStatus(s *tui.Snapshot, st map[string]any) {
	if st == nil {
		s.ChainError = "bitcoin status unavailable"
		return
	}
	if e := asString(st["error"]); e != "" {
		s.ChainError = e
		s.BitcoinConnected = false
	} else {
		s.BitcoinConnected = true
	}
	if n := asString(st["network"]); n != "" {
		s.Network = n
	}
	s.BlockHeight = asInt64(st["blocks"])
	s.HeaderHeight = asInt64(st["headers"])
	s.BestBlockHash = asString(st["best_block_hash"])
	s.InitialDownload = asBool(st["initial_block_download"])
	s.Synced = asBool(st["synced"])
	s.PeerCount = int(asInt64(st["peers"]))
	s.MinPeers = int(asInt64(st["min_peers"]))
	s.SyncNote = asString(st["sync_note"])
	if asBool(st["tip_lagging"]) {
		s.TipLagging = true
	}
	if asBool(st["tip_hash_mismatch"]) {
		s.TipHashMismatch = true
	}
	if n := asInt64(st["tip_lag_blocks"]); n > 0 {
		s.TipLagBlocks = n
	}
	if n := asInt64(st["external_tip"]); n > 0 {
		s.TipExternal = n
	}
}

func asInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case uint32:
		return int64(n)
	case uint64:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return i
	default:
		return 0
	}
}

func asBool(v any) bool {
	switch n := v.(type) {
	case bool:
		return n
	case string:
		switch strings.ToLower(strings.TrimSpace(n)) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func requestIsLoopback(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// prettifyLogLine turns the HTTP access JSON into one operator line and drops
// the console poll so the log panel is not a mirror of itself.
func prettifyLogLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	if !strings.HasPrefix(line, "{") {
		return line
	}
	var m struct {
		Method   string `json:"method"`
		Path     string `json:"path"`
		Status   int    `json:"status"`
		Duration string `json:"duration"`
	}
	if json.Unmarshal([]byte(line), &m) != nil || m.Path == "" {
		return line
	}
	if m.Path == "/api/console/status" {
		return ""
	}
	return m.Method + " " + m.Path + " " + strconv.Itoa(m.Status) + " " + m.Duration
}

// logFan copies process logs into the hub and, when extra is set, to the
// terminal. The status screen clears extra while it owns the display.
type logFan struct {
	hub   *tui.Hub
	mu    sync.Mutex
	extra io.Writer
}

func (f *logFan) setExtra(w io.Writer) {
	if f == nil {
		return
	}
	f.mu.Lock()
	f.extra = w
	f.mu.Unlock()
}

func (f *logFan) Write(p []byte) (int, error) {
	if f != nil && f.hub != nil {
		_, _ = f.hub.Write(p)
	}
	var extra io.Writer
	if f != nil {
		f.mu.Lock()
		extra = f.extra
		f.mu.Unlock()
	}
	if extra != nil {
		_, _ = extra.Write(p)
	}
	return len(p), nil
}
