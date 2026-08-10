package bitcoin

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	"stargate-backend/storage/ipfs"
)

// BlockMonitor handles comprehensive Bitcoin block monitoring and data extraction
type BlockMonitor struct {
	bitcoinClient   *BitcoinNodeClient
	rawClient       *RawBlockClient
	chain           ChainBackend
	bitcoinAPI      *BitcoinAPI
	currentHeight   int64
	lastChecked     time.Time
	isRunning       bool
	stopChan        chan bool
	mu              sync.RWMutex
	dataStorage     DataStorageInterface
	ingestion       *services.IngestionService
	sweepStore      SweepTaskStore
	sweepMempool    UTXOClient
	stegoReconciler StegoReconciler
	unpinPath       func(context.Context, string) error
	ipfsClient      *ipfs.Client
	reconcileMu     sync.Mutex

	// Configuration
	checkInterval time.Duration
	blocksDir     string
	maxRetries    int
	retryDelay    time.Duration

	// Callbacks
	onBlockProcessed []func(height int64)

	// Statistics
	blocksProcessed     int64
	totalTransactions   int64
	totalImages         int64
	totalStegoContracts int64
	totalInscriptions   int64
	lastProcessTime     time.Duration
}

// reconcileSweepInterval / reconcileSweepBlocks control the periodic safety-net
// rescan.  With OP_RETURN-based matching, the block monitor discovers contracts
// during normal forward processing.  This loop is a low-frequency fallback for
// edge cases (node restart mid-block, reorgs).

// BlockData represents comprehensive block data stored to disk
type BlockData struct {
	BlockHeader     BlockHeader          `json:"block_header"`
	Transactions    []TransactionData    `json:"transactions"`
	WitnessData     []WitnessData        `json:"witness_data"`
	ExtractedImages []ExtractedImageData `json:"extracted_images"`
	Inscriptions    []InscriptionData    `json:"inscriptions"`
	SmartContracts  []SmartContractData  `json:"smart_contracts"`
	Metadata        BlockMetadata        `json:"metadata"`
	ProcessingInfo  ProcessingInfo       `json:"processing_info"`
}

// TransactionData represents transaction information
type TransactionData struct {
	TxID        string     `json:"tx_id"`
	Height      int        `json:"height"`
	Time        int64      `json:"time"`
	Status      string     `json:"status"`
	VOut        []VOut     `json:"vout"`
	VIn         []Vin      `json:"vin"`
	WitnessSize int        `json:"witness_size"`
	Inputs      []TxInput  `json:"inputs"`
	Outputs     []TxOutput `json:"outputs"`
	HasImages   bool       `json:"has_images"`
	ImageCount  int        `json:"image_count"`
	TextContent []string   `json:"text_content"`
	HexData     []string   `json:"hex_data"`
}

// WitnessData represents extracted witness data
type WitnessData struct {
	TxID        string   `json:"tx_id"`
	InputIndex  int      `json:"input_index"`
	WitnessData []string `json:"witness_data"`
	TotalSize   int      `json:"total_size"`
	HasImages   bool     `json:"has_images"`
	ImageCount  int      `json:"image_count"`
	TextContent []string `json:"text_content"`
	HexData     []string `json:"hex_data"`
}

// InscriptionData represents inscription information
type InscriptionData struct {
	TxID        string `json:"tx_id"`
	InputIndex  int    `json:"input_index"`
	ContentType string `json:"content_type"`
	Content     string `json:"content"`
	SizeBytes   int    `json:"size_bytes"`
	FileName    string `json:"file_name"`
	FilePath    string `json:"file_path"`
}

// SmartContractData represents smart contract information
type SmartContractData struct {
	ContractID  string         `json:"contract_id"`
	BlockHeight int64          `json:"block_height"`
	ImagePath   string         `json:"image_path"`
	Confidence  float64        `json:"confidence"`
	Metadata    map[string]any `json:"metadata"`
}

// StegoReconciler is the seam from block confirmation into the stego/contract app layer.
// Implemented by app/smart_contract.Server.ReconcileStego — bitcoin must not decode
// manifests or write proposals itself. See docs/arch/DOMAIN_SEAMS.md.
type StegoReconciler interface {
	ReconcileStego(ctx context.Context, stegoCID, expectedHash string) error
}

// StegoReconcilerFunc adapts a function to the StegoReconciler interface.
type StegoReconcilerFunc func(ctx context.Context, stegoCID, expectedHash string) error

// BlockMetadata contains processing metadata
type BlockMetadata struct {
	SourceFile          string `json:"source_file"`
	FileSize            int64  `json:"file_size"`
	ParserVersion       string `json:"parser_version"`
	ProcessingTime      int64  `json:"processing_time"`
	ImageExtractionTime int64  `json:"image_extraction_time"`
	InscriptionTime     int64  `json:"inscription_time"`
	SmartContractTime   int64  `json:"smart_contract_time"`
}

// ProcessingInfo contains processing information
type ProcessingInfo struct {
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	Duration    int64     `json:"duration"`
	Version     string    `json:"version"`
	APISources  []string  `json:"api_sources"`
	Success     bool      `json:"success"`
}

// BlockInscriptionsResponse represents the response for block inscriptions API
type BlockInscriptionsResponse struct {
	BlockHeight       int64                `json:"block_height"`
	BlockHash         string               `json:"block_hash"`
	Timestamp         int64                `json:"timestamp"`
	TotalTransactions int                  `json:"total_transactions"`
	Inscriptions      []InscriptionData    `json:"inscriptions"`
	Images            []ExtractedImageData `json:"images"`
	SmartContracts    []SmartContractData  `json:"smart_contracts"`
	ProcessingTime    int64                `json:"processing_time_ms"`
	Success           bool                 `json:"success"`
	Error             string               `json:"error,omitempty"`
}

// monitor config helpers (overridable via env for K8s tuning without rebuild)
func monitorCheckInterval() time.Duration {
	if v := os.Getenv("BLOCK_MONITOR_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	if monitorTrackTipOnly() {
		// Tip-only tracking is cheap; poll more frequently so we notice
		// new blocks with lower latency.
		return 60 * time.Second
	}
	return 5 * time.Minute
}

func monitorMaxBlocksPerCycle() int64 {
	if v := os.Getenv("BLOCK_MONITOR_MAX_PER_CYCLE"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			if n > 50 {
				n = 50 // safety cap
			}
			return n
		}
	}
	return 3
}

func monitorRecentReconcileWindow() int {
	if v := os.Getenv("BLOCK_MONITOR_RECONCILE_WINDOW"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 100 {
				n = 100
			}
			return n
		}
	}
	if monitorTrackTipOnly() {
		// In tip-only mode we do not periodically reprocess recent blocks.
		// Explicit ReconcileRecentBlocks calls (e.g. after an ingestion) are still honored.
		return 0
	}
	return 1
}

func monitorReconcileInterval() time.Duration {
	if v := os.Getenv("BLOCK_MONITOR_RECONCILE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 20 * time.Minute
}

// monitorTrackTipOnly returns whether the block monitor should run in
// lightweight "just track the live tip" mode (the default). This avoids
// repeated re-processing of recent blocks and the associated CPU cost
// from parsing + oracle reconciliation on every ticker and heal window.
func monitorTrackTipOnly() bool {
	if v := os.Getenv("BLOCK_MONITOR_TRACK_TIP_ONLY"); v != "" {
		// Explicit opt-out supported.
		return v != "false" && v != "0" && v != "off"
	}
	return true
}

// NewBlockMonitor creates a new block monitor
func NewBlockMonitor(client *BitcoinNodeClient) *BlockMonitor {
	return &BlockMonitor{
		bitcoinClient: client,
		rawClient:     NewRawBlockClient(client.GetNetwork()),
		checkInterval: monitorCheckInterval(),
		blocksDir:     blocksDirFromEnv(),
		maxRetries:    3,
		retryDelay:    10 * time.Second,
		lastChecked:   time.Now(),
		ipfsClient:    ipfs.NewClientFromEnv(),
	}
}

// NewBlockMonitorWithStorageAndAPI creates a new block monitor with data storage and Bitcoin API
func NewBlockMonitorWithStorageAndAPI(client *BitcoinNodeClient, dataStorage DataStorageInterface, bitcoinAPI *BitcoinAPI) *BlockMonitor {
	log.Printf("Creating block monitor with bitcoinAPI set: %v", bitcoinAPI != nil)
	m := &BlockMonitor{
		bitcoinClient: client,
		rawClient:     NewRawBlockClient(client.GetNetwork()),
		dataStorage:   dataStorage,
		bitcoinAPI:    bitcoinAPI,
		checkInterval: monitorCheckInterval(),
		blocksDir:     blocksDirFromEnv(),
		maxRetries:    3,
		retryDelay:    10 * time.Second,
		lastChecked:   time.Now(),
		ipfsClient:    ipfs.NewClientFromEnv(),
	}
	// Bootstrap from persistent storage as early as possible so restarts do not
	// cause us to only look at the last 3 blocks.
	m.bootstrapCurrentHeightFromStorage()
	return m
}

// SetIngestionService enables ingestion-aware reconciliation (optional).
func (bm *BlockMonitor) SetIngestionService(ingestion *services.IngestionService) {
	bm.ingestion = ingestion
}

// SetStegoReconciler wires stego reconcile to run when ingestions are confirmed.
func (bm *BlockMonitor) SetStegoReconciler(reconciler StegoReconciler) {
	bm.stegoReconciler = reconciler
}

func (bm *BlockMonitor) SetIPFSUnpin(unpin func(context.Context, string) error) {
	bm.unpinPath = unpin
}

// OnBlockProcessed registers a callback invoked after a block is successfully processed.
func (bm *BlockMonitor) OnBlockProcessed(fn func(height int64)) {
	bm.onBlockProcessed = append(bm.onBlockProcessed, fn)
}

// Start begins the block monitoring process
func (bm *BlockMonitor) Start() error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.isRunning {
		return fmt.Errorf("block monitor is already running")
	}

	bm.isRunning = true
	bm.stopChan = make(chan bool)

	// Create blocks directory
	if err := os.MkdirAll(bm.blocksDir, 0755); err != nil {
		return fmt.Errorf("failed to create blocks directory: %w", err)
	}

	// One-time (idempotent) migration from old flat layout to 3-level partitioned layout.
	// This is the follow-up to the IO reduction changes. Safe to call repeatedly.
	if err := MigrateOldBlockLayoutIfNeeded(bm.blocksDir); err != nil {
		log.Printf("block layout migration warning (non-fatal): %v", err)
	}

	mode := "full"
	if monitorTrackTipOnly() {
		mode = "tip-only"
	}
	log.Printf("Starting block monitor (%s mode) with %s interval, bitcoinAPI set: %v", mode, bm.checkInterval, bm.bitcoinAPI != nil)

	go bm.monitorLoop()
	// Only start the reconcile sweep goroutine when we actually use periodic
	// recent-block healing (disabled in the default tip-only mode to avoid
	// parking an idle goroutine forever on stopChan).
	if !monitorTrackTipOnly() && monitorRecentReconcileWindow() > 0 {
		go bm.reconcileSweepLoop()
	} else {
		log.Printf("block monitor: periodic reconcile sweep disabled (tip-only tracking)")
	}

	return nil
}

// Stop stops the block monitoring process

// IsRunning returns whether the monitor is currently running

// GetStatistics returns current monitoring statistics
func (bm *BlockMonitor) GetStatistics() map[string]any {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	out := map[string]any{
		"blocks_processed":      bm.blocksProcessed,
		"total_transactions":    bm.totalTransactions,
		"total_images":          bm.totalImages,
		"total_stego_contracts": bm.totalStegoContracts,
		"total_inscriptions":    bm.totalInscriptions,
		"current_height":        bm.currentHeight,
		"max_stored_height":     bm.getMaxStoredHeight(),
		"last_process_time":     bm.lastProcessTime.Milliseconds(),
		"is_running":            bm.isRunning,
		"check_interval":        bm.checkInterval.Milliseconds(),
	}
	lag := GetTipLagStatus()
	if !lag.CheckedAt.IsZero() {
		out["tip_lag"] = lag
	}
	return out
}

// bootstrapCurrentHeightFromStorage sets currentHeight from the storage's
// persisted max if it is higher. This is the key fix for gaps after pod
// restarts: we resume from the last thing we successfully wrote instead of
// resetting to "only the last 3 blocks".
func (bm *BlockMonitor) bootstrapCurrentHeightFromStorage() {
	if bm.dataStorage == nil {
		return
	}
	if maxH, err := bm.dataStorage.GetMaxBlockHeight(); err == nil && maxH > bm.currentHeight {
		bm.currentHeight = maxH
		log.Printf("Block monitor bootstrapped currentHeight=%d from storage (resume across restart)", maxH)
	}
}

// getMaxStoredHeight returns the max height known to storage (best effort).
func (bm *BlockMonitor) getMaxStoredHeight() int64 {
	if bm.dataStorage == nil {
		return 0
	}
	if h, err := bm.dataStorage.GetMaxBlockHeight(); err == nil {
		return h
	}
	return 0
}

type scanPayload struct {
	message          string
	payoutAddress    string
	payoutScript     string
	payoutScriptHash string
}

// SetSweepDependencies wires commitment sweep support for oracle reconcile.
func (bm *BlockMonitor) SetSweepDependencies(store SweepTaskStore, mempool UTXOClient) {
	bm.sweepStore = store
	bm.sweepMempool = mempool
}

// SetChainBackend wires the local btcd (or esplora fallback) as the primary
// chain data source for tip tracking, raw blocks, reorg checks, and tx status.
func (bm *BlockMonitor) SetChainBackend(chain ChainBackend) {
	bm.chain = chain
	if bm.rawClient != nil {
		bm.rawClient.SetChainBackend(chain)
	}
	if bm.bitcoinClient != nil {
		bm.bitcoinClient.SetChainBackend(chain)
	}
}

// ChainBackend returns the configured chain backend (may be nil in tests).

// contractUpserter is an optional interface satisfied by MCP stores that can
// upsert contracts.  Used by persistDiscoveryContract to save on-chain
// OP_RETURN discoveries so they appear in /api/contracts immediately.
type contractUpserter interface {
	UpsertContractWithTasks(ctx context.Context, c smart_contract.Contract, t []smart_contract.Task) error
}

// contractLister is an optional interface satisfied by MCP stores that can
// list contracts (wishes).  Used to build OP_RETURN candidates from wish
// contracts whose contract_id encodes the visible_pixel_hash.
type contractLister interface {
	ListContracts(filter smart_contract.ContractFilter) ([]smart_contract.Contract, error)
}

// proposalLister is an optional interface satisfied by MCP stores that can
// list proposals.  Used to build OP_RETURN candidates from proposals when
// the peer's ingestion records are incomplete.
type proposalLister interface {
	ListProposals(ctx context.Context, filter smart_contract.ProposalFilter) ([]smart_contract.Proposal, error)
}
