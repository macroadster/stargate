// Package tui draws the Stargate status console.
//
// The welcome view tells a new operator to open the browser UI. The monitor
// and log views are the live node. This package renders and polls. The server
// process fills a Hub. It does not own chain, storage, or HTTP routing.
package tui

import "time"

// Phase is the process lifecycle shown in the console header.
type Phase string

const (
	PhaseStarting Phase = "starting"
	PhaseReady    Phase = "ready"
	PhaseStopping Phase = "stopping"
)

// View is the console screen the operator is looking at.
type View string

const (
	ViewWelcome View = "welcome"
	ViewMonitor View = "monitor"
	ViewLogs    View = "logs"
)

// Snapshot is one sample of node status. It is safe to copy and to send as
// JSON from GET /api/console/status. Secrets (DSN, RPC password, API keys)
// do not belong here.
type Snapshot struct {
	SampledAt time.Time `json:"sampled_at"`
	Version   string    `json:"version,omitempty"`
	Phase     Phase     `json:"phase,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`

	UIURL     string `json:"ui_url,omitempty"`
	DocsURL   string `json:"docs_url,omitempty"`
	APIURL    string `json:"api_url,omitempty"`
	MCPURL    string `json:"mcp_url,omitempty"`
	HealthURL string `json:"health_url,omitempty"`
	HTTPPort  string `json:"http_port,omitempty"`

	Storage   string `json:"storage,omitempty"`
	DataDir   string `json:"data_dir,omitempty"`
	Network   string `json:"network,omitempty"`
	ChainMode string `json:"chain_mode,omitempty"`

	// HealthStatus is the pill: starting, healthy, syncing, degraded, down, stopping.
	HealthStatus string `json:"health_status,omitempty"`

	ScannerType    string `json:"scanner_type,omitempty"`
	ScannerVersion string `json:"scanner_version,omitempty"`
	ScannerReady   bool   `json:"scanner_ready"`

	BitcoinConnected bool   `json:"bitcoin_connected"`
	BlockHeight      int64  `json:"block_height"`
	HeaderHeight     int64  `json:"header_height"`
	PeerCount        int    `json:"peer_count"`
	MinPeers         int    `json:"min_peers"`
	InitialDownload  bool   `json:"initial_block_download"`
	Synced           bool   `json:"synced"`
	BestBlockHash    string `json:"best_block_hash,omitempty"`
	SyncNote         string `json:"sync_note,omitempty"`
	ChainError       string `json:"chain_error,omitempty"`

	TipLocal        int64 `json:"tip_local,omitempty"`
	TipExternal     int64 `json:"tip_external,omitempty"`
	TipLagBlocks    int64 `json:"tip_lag_blocks,omitempty"`
	TipLagging      bool  `json:"tip_lagging"`
	TipHashMismatch bool  `json:"tip_hash_mismatch"`

	MonitorRunning   bool  `json:"monitor_running"`
	MonitorHeight    int64 `json:"monitor_height"`
	BlocksProcessed  int64 `json:"blocks_processed"`
	InscriptionsSeen int64 `json:"inscriptions_seen"`
	ContractsSeen    int64 `json:"contracts_seen"`
	ImagesSeen       int64 `json:"images_seen"`
	LastProcessMS    int64 `json:"last_process_ms"`

	MirrorEnabled bool   `json:"mirror_enabled"`
	MirrorPeerID  string `json:"mirror_peer_id,omitempty"`
	MirrorUploads int    `json:"mirror_uploads"`
	MirrorWishes  int    `json:"mirror_wishes"`
	MirrorPeers   int    `json:"mirror_peers"`
	WishMirrorOn  bool   `json:"wish_mirror_on"`

	AgentEnabled bool   `json:"agent_enabled"`
	AgentWatcher bool   `json:"agent_watcher"`
	AgentWorker  bool   `json:"agent_worker"`
	AgentTool    string `json:"agent_tool,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`

	Goroutines int `json:"goroutines,omitempty"`

	Logs []string `json:"logs,omitempty"`

	Reachable  bool   `json:"reachable"`
	FetchError string `json:"fetch_error,omitempty"`
}

// Source supplies snapshots to the screen. Hub and the remote poller both implement it.
type Source interface {
	Snapshot() Snapshot
}

// DeriveHealth sets HealthStatus from phase and chain fields.
// FetchError and an explicit stopping phase win. A node that is still
// starting stays "starting" unless the chain is already known to be wrong.
func DeriveHealth(s *Snapshot, phase Phase) {
	if s == nil {
		return
	}
	switch {
	case s.FetchError != "":
		s.HealthStatus = "down"
	case phase == PhaseStopping:
		s.HealthStatus = "stopping"
	case s.ChainError != "" || s.TipLagging || s.TipHashMismatch:
		s.HealthStatus = "degraded"
	case phase == PhaseStarting || phase == "":
		s.HealthStatus = "starting"
	case s.InitialDownload || (s.BitcoinConnected && !s.Synced):
		s.HealthStatus = "syncing"
	case !s.BitcoinConnected:
		s.HealthStatus = "degraded"
	default:
		s.HealthStatus = "healthy"
	}
}
