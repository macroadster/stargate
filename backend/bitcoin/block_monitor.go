package bitcoin

import (
	"context"
	"fmt"
	"log"
	"os"
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
	bitcoinAPI      *BitcoinAPI
	currentHeight   int64
	lastChecked     time.Time
	isRunning       bool
	stopChan        chan bool
	mu              sync.RWMutex
	dataStorage     DataStorageInterface
	ingestion       *services.IngestionService
	sweepStore      SweepTaskStore
	sweepMempool    *MempoolClient
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
const reconcileSweepInterval = 60 * time.Minute

const reconcileSweepBlocks = 3

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

// NewBlockMonitor creates a new block monitor
func NewBlockMonitor(client *BitcoinNodeClient) *BlockMonitor {
	return &BlockMonitor{
		bitcoinClient: client,
		rawClient:     NewRawBlockClient(client.GetNetwork()),
		checkInterval: 5 * time.Minute, // Check every 5 minutes
		blocksDir:     blocksDirFromEnv(),
		maxRetries:    3,
		retryDelay:    10 * time.Second,
		lastChecked:   time.Now(),
		ipfsClient:    ipfs.NewClientFromEnv(),
	}
}

// NewBlockMonitorWithStorage creates a new block monitor with data storage
func NewBlockMonitorWithStorage(client *BitcoinNodeClient, dataStorage DataStorageInterface) *BlockMonitor {
	return &BlockMonitor{
		bitcoinClient: client,
		rawClient:     NewRawBlockClient(client.GetNetwork()),
		dataStorage:   dataStorage,
		checkInterval: 5 * time.Minute, // Check every 5 minutes
		blocksDir:     blocksDirFromEnv(),
		maxRetries:    3,
		retryDelay:    10 * time.Second,
		lastChecked:   time.Now(),
		ipfsClient:    ipfs.NewClientFromEnv(),
	}
}

// NewBlockMonitorWithAPI creates a new block monitor with Bitcoin API
func NewBlockMonitorWithAPI(client *BitcoinNodeClient, bitcoinAPI *BitcoinAPI) *BlockMonitor {
	return &BlockMonitor{
		bitcoinClient: client,
		rawClient:     NewRawBlockClient(client.GetNetwork()),
		bitcoinAPI:    bitcoinAPI,
		checkInterval: 5 * time.Minute, // Check every 5 minutes
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
	return &BlockMonitor{
		bitcoinClient: client,
		rawClient:     NewRawBlockClient(client.GetNetwork()),
		dataStorage:   dataStorage,
		bitcoinAPI:    bitcoinAPI,
		checkInterval: 5 * time.Minute, // Check every 5 minutes
		blocksDir:     blocksDirFromEnv(),
		maxRetries:    3,
		retryDelay:    10 * time.Second,
		lastChecked:   time.Now(),
		ipfsClient:    ipfs.NewClientFromEnv(),
	}
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

	log.Printf("Starting block monitor with %s interval, bitcoinAPI set: %v", bm.checkInterval, bm.bitcoinAPI != nil)

	go bm.monitorLoop()
	go bm.reconcileSweepLoop()

	return nil
}

// Stop stops the block monitoring process
func (bm *BlockMonitor) Stop() error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if !bm.isRunning {
		return fmt.Errorf("block monitor is not running")
	}

	log.Println("Stopping block monitor")
	bm.isRunning = false
	close(bm.stopChan)

	return nil
}

// IsRunning returns whether the monitor is currently running
func (bm *BlockMonitor) IsRunning() bool {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.isRunning
}

// GetStatistics returns current monitoring statistics
func (bm *BlockMonitor) GetStatistics() map[string]any {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	return map[string]any{
		"blocks_processed":      bm.blocksProcessed,
		"total_transactions":    bm.totalTransactions,
		"total_images":          bm.totalImages,
		"total_stego_contracts": bm.totalStegoContracts,
		"total_inscriptions":    bm.totalInscriptions,
		"current_height":        bm.currentHeight,
		"last_process_time":     bm.lastProcessTime.Milliseconds(),
		"is_running":            bm.isRunning,
		"check_interval":        bm.checkInterval.Milliseconds(),
	}
}

type scanPayload struct {
	message          string
	payoutAddress    string
	payoutScript     string
	payoutScriptHash string
}

// SetSweepDependencies wires commitment sweep support for oracle reconcile.
func (bm *BlockMonitor) SetSweepDependencies(store SweepTaskStore, mempool *MempoolClient) {
	bm.sweepStore = store
	bm.sweepMempool = mempool
}

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
