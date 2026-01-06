package bitcoin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"

	"stargate-backend/core"
	"stargate-backend/core/smart_contract"
	"stargate-backend/ipfs"
	"stargate-backend/services"
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

	// Statistics
	blocksProcessed     int64
	totalTransactions   int64
	totalImages         int64
	totalStegoContracts int64
	totalInscriptions   int64
	lastProcessTime     time.Duration
}

const reconcileSweepInterval = 20 * time.Minute
const reconcileSweepBlocks = 6

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

// StegoReconciler runs a stego reconcile given a CID + expected hash.
type StegoReconciler interface {
	ReconcileStego(ctx context.Context, stegoCID, expectedHash string) error
}

// StegoReconcilerFunc adapts a function to the StegoReconciler interface.
type StegoReconcilerFunc func(ctx context.Context, stegoCID, expectedHash string) error

func (fn StegoReconcilerFunc) ReconcileStego(ctx context.Context, stegoCID, expectedHash string) error {
	return fn(ctx, stegoCID, expectedHash)
}

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

func blocksDirFromEnv() string {
	if v := os.Getenv("BLOCKS_DIR"); v != "" {
		return v
	}
	return "blocks"
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

// monitorLoop runs the main monitoring loop
func (bm *BlockMonitor) monitorLoop() {
	ticker := time.NewTicker(bm.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := bm.checkForNewBlocks(); err != nil {
				log.Printf("Error checking for new blocks: %v", err)
			}
		case <-bm.stopChan:
			log.Println("Block monitor stopped")
			return
		}
	}
}

func (bm *BlockMonitor) reconcileSweepLoop() {
	if reconcileSweepBlocks <= 0 || reconcileSweepInterval <= 0 {
		return
	}
	ticker := time.NewTicker(reconcileSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if bm.ingestion == nil {
				continue
			}
			if err := bm.ReconcileRecentBlocks(context.Background(), reconcileSweepBlocks); err != nil {
				log.Printf("reconcile sweep failed: %v", err)
			}
		case <-bm.stopChan:
			return
		}
	}
}

// updateRecentBlocksSummary creates a recent blocks summary file for frontend
func (bm *BlockMonitor) updateRecentBlocksSummary() error {
	blocksDir := bm.blocksDir
	if blocksDir == "" {
		blocksDir = blocksDirFromEnv()
	}

	// Ensure the directory exists so we don't fail with a missing relative path when running in a container.
	if err := os.MkdirAll(blocksDir, 0755); err != nil {
		return fmt.Errorf("failed to ensure blocks directory: %w", err)
	}

	files, err := os.ReadDir(blocksDir)
	if err != nil {
		return fmt.Errorf("failed to read blocks directory %s: %w", blocksDir, err)
	}

	var recentBlocks []map[string]interface{}

	// Collect recent blocks (up to 10 most recent)
	for _, file := range files {
		if file.IsDir() && len(file.Name()) > 8 {
			var height int64
			if _, err := fmt.Sscanf(file.Name(), "%d_", &height); err == nil {
				// Try to read inscriptions.json
				blockDirPath := filepath.Join(blocksDir, file.Name())
				inscriptionsPath := filepath.Join(blockDirPath, "inscriptions.json")

				if data, err := os.ReadFile(inscriptionsPath); err == nil {
					var blockData map[string]interface{}
					if err := json.Unmarshal(data, &blockData); err == nil {
						// Add to recent blocks
						recentBlocks = append(recentBlocks, blockData)
					}
				}
			}
		}
	}

	// Sort by height (descending)
	for i := 0; i < len(recentBlocks); i++ {
		for j := i + 1; j < len(recentBlocks); j++ {
			height1, _ := recentBlocks[i]["block_height"].(float64)
			height2, _ := recentBlocks[j]["block_height"].(float64)
			if height1 < height2 {
				recentBlocks[i], recentBlocks[j] = recentBlocks[j], recentBlocks[i]
			}
		}
	}

	// Take only top 10
	if len(recentBlocks) > 10 {
		recentBlocks = recentBlocks[:10]
	}

	// Create summary
	summary := map[string]interface{}{
		"blocks":       recentBlocks,
		"total":        len(recentBlocks),
		"last_updated": time.Now().Unix(),
	}

	// Save to blocks/recent-blocks.json
	summaryJSON, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal recent blocks summary: %w", err)
	}

	summaryPath := filepath.Join(blocksDir, "recent-blocks.json")
	if err := os.WriteFile(summaryPath, summaryJSON, 0644); err != nil {
		return fmt.Errorf("failed to write recent blocks summary: %w", err)
	}

	log.Printf("Updated recent blocks summary with %d blocks", len(recentBlocks))
	return nil
}

// checkForNewBlocks checks for and processes new blocks more efficiently
func (bm *BlockMonitor) checkForNewBlocks() error {
	// Get current blockchain height from blockchain.info
	currentHeight, err := bm.getCurrentHeightFromBlockchainInfo()
	if err != nil {
		return fmt.Errorf("failed to get current height: %w", err)
	}

	log.Printf("Current blockchain height: %d, monitor height: %d", currentHeight, bm.currentHeight)
	if err := bm.reconcileCanonicalTip(currentHeight, 6); err != nil {
		log.Printf("Failed to reconcile canonical tip: %v", err)
	}

	var startHeight int64
	var maxBlocksPerCycle int64 = 2            // Very conservative: only 2 blocks per cycle
	var delayBetweenRequests = 5 * time.Second // 5 second delay between requests

	// If this is first run, process some recent blocks
	if bm.currentHeight == 0 {
		// Process last 3 blocks as initial seed (reduced from 5)
		startHeight = currentHeight - 2
		if startHeight < 1 {
			startHeight = 1
		}
		log.Printf("First run - processing blocks from %d to %d with %v delay between requests", startHeight, currentHeight, delayBetweenRequests)

		for height := startHeight; height <= currentHeight; height++ {
			if err := bm.ProcessBlock(height); err != nil {
				log.Printf("Error processing block %d: %v", height, err)
				continue
			}
			bm.currentHeight = height
			bm.blocksProcessed++

			// Add delay between requests to avoid rate limiting
			if height < currentHeight {
				log.Printf("Waiting %v before processing next block...", delayBetweenRequests)
				time.Sleep(delayBetweenRequests)
			}
		}
	} else {
		// Process new blocks in batches with throttling
		startHeight = bm.currentHeight + 1

		log.Printf("Processing new blocks from %d to %d (max %d per cycle) with %v delay between requests", startHeight, currentHeight, maxBlocksPerCycle, delayBetweenRequests)

		for height := startHeight; height <= currentHeight && height < startHeight+maxBlocksPerCycle; height++ {
			if err := bm.ProcessBlock(height); err != nil {
				log.Printf("Error processing block %d: %v", height, err)
				continue
			}
			bm.currentHeight = height
			bm.blocksProcessed++

			// Add delay between requests to avoid rate limiting
			if height < currentHeight && height < startHeight+maxBlocksPerCycle-1 {
				log.Printf("Waiting %v before processing next block...", delayBetweenRequests)
				time.Sleep(delayBetweenRequests)
			}
		}
	}

	// If we still have more blocks to process, continue in next cycle
	if currentHeight > startHeight+maxBlocksPerCycle-1 {
		log.Printf("Processed %d blocks this cycle, %d more blocks remaining for next cycle", maxBlocksPerCycle, currentHeight-startHeight+1)
	}

	bm.lastChecked = time.Now()

	// Update recent blocks summary for frontend
	if err := bm.updateRecentBlocksSummary(); err != nil {
		log.Printf("Failed to update recent blocks summary: %v", err)
	}

	return nil
}

func (bm *BlockMonitor) reconcileCanonicalTip(currentHeight int64, depth int) error {
	if depth <= 0 || bm.rawClient == nil || bm.bitcoinClient == nil {
		return nil
	}
	for i := 0; i < depth; i++ {
		height := currentHeight - int64(i)
		if height < 0 {
			break
		}
		canonicalHash, err := bm.getCanonicalBlockHash(height)
		if err != nil {
			return err
		}
		if canonicalHash == "" {
			continue
		}
		removed, err := bm.pruneBlockDirsForHeight(height, canonicalHash)
		if err != nil {
			return err
		}
		if removed {
			if err := bm.ProcessBlock(height); err != nil {
				log.Printf("Failed to reprocess block %d after reorg: %v", height, err)
			}
		}
	}
	return nil
}

func (bm *BlockMonitor) getCanonicalBlockHash(height int64) (string, error) {
	baseURL := strings.TrimSpace(bm.bitcoinClient.baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("bitcoin client baseURL missing")
	}
	url := fmt.Sprintf("%s/block-height/%d", baseURL, height)
	resp, err := bm.bitcoinClient.httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("block hash status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func (bm *BlockMonitor) pruneBlockDirsForHeight(height int64, canonicalHash string) (bool, error) {
	blocksDir := bm.blocksDir
	if blocksDir == "" {
		blocksDir = blocksDirFromEnv()
	}
	if blocksDir == "" {
		return false, nil
	}
	entries, err := os.ReadDir(blocksDir)
	if err != nil {
		return false, err
	}
	var removed bool
	var hasCanonical bool
	heightPrefix := fmt.Sprintf("%d_", height)
	reorgDir := filepath.Join(blocksDir, "reorgs")
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), heightPrefix) {
			continue
		}
		dirPath := filepath.Join(blocksDir, entry.Name())
		hash, err := readBlockHeaderHash(filepath.Join(dirPath, "block.json"))
		if err != nil || hash == "" {
			continue
		}
		if hash == canonicalHash {
			hasCanonical = true
			continue
		}
		log.Printf("Reorg cleanup: moving stale block dir %s to reorgs (hash=%s canonical=%s)", entry.Name(), hash, canonicalHash)
		if err := os.MkdirAll(reorgDir, 0755); err != nil {
			return removed, err
		}
		dest := filepath.Join(reorgDir, entry.Name())
		if err := os.Rename(dirPath, dest); err != nil {
			if err := copyDir(dirPath, dest); err != nil {
				return removed, err
			}
			if err := os.RemoveAll(dirPath); err != nil {
				return removed, err
			}
		}
		removed = true
	}
	if removed && !hasCanonical {
		return true, nil
	}
	return false, nil
}

func copyDir(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if err := copyFile(path, target); err != nil {
			return err
		}
		return os.Chmod(target, info.Mode())
	})
}

func readBlockHeaderHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var payload struct {
		BlockHeader struct {
			Hash string `json:"Hash"`
		} `json:"block_header"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.BlockHeader.Hash), nil
}

// getCurrentHeightFromBlockchainInfo gets current height from the configured Bitcoin network
func (bm *BlockMonitor) getCurrentHeightFromBlockchainInfo() (int64, error) {
	return bm.bitcoinClient.GetCurrentHeight()
}

// ProcessBlock downloads and processes a single block using raw block parser (exported for external use)
func (bm *BlockMonitor) ProcessBlock(height int64) error {
	startTime := time.Now()

	log.Printf("Processing block %d, bitcoinAPI set: %v", height, bm.bitcoinAPI != nil)

	// Get raw block hex from blockchain.info
	hexData, err := bm.rawClient.GetRawBlockHex(height)
	if err != nil {
		return fmt.Errorf("failed to get raw block hex: %w", err)
	}

	// Parse the block
	parsedBlock, err := bm.rawClient.ParseBlock(hexData)
	if err != nil {
		return fmt.Errorf("failed to parse block: %w", err)
	}

	// Set the height in parsed block (this was missing!)
	parsedBlock.Height = height

	log.Printf("Parsed block %d: %d transactions, %d images found", height, len(parsedBlock.Transactions), len(parsedBlock.Images))

	// Create block directory
	blockDir := filepath.Join(bm.blocksDir, fmt.Sprintf("%d_%s", height, parsedBlock.Hash[:8]))
	if err := os.MkdirAll(blockDir, 0755); err != nil {
		return fmt.Errorf("failed to create block directory: %w", err)
	}

	// Save raw block data
	if err := bm.saveBlockData(blockDir, parsedBlock, hexData); err != nil {
		return fmt.Errorf("failed to save block data: %w", err)
	}

	// Extract and save images
	if err := bm.saveImages(blockDir, parsedBlock.Images); err != nil {
		log.Printf("Failed to save images: %v", err)
	}

	// Scan block for steganography using the scanner manager
	// NOTE: We use the Go backend's /bitcoin/v1/scan/block endpoint via scannerManager.ScanBlock()
	// instead of calling the Python API's /scan/block endpoint directly. While the Python API
	// has an efficient /scan/block endpoint that scans entire blocks in one request and
	// automatically updates inscriptions.json, we use the Go implementation for consistency
	// with the existing architecture. The Python /scan/block endpoint requires filesystem access to
	// the shared blocks directory.
	var blockScanResponse *core.BlockScanResponse

	if bm.bitcoinAPI != nil && bm.bitcoinAPI.scannerManager != nil {
		log.Printf("Using scanner manager to scan block %d", height)
		var err error
		blockScanResponse, err = bm.bitcoinAPI.scannerManager.ScanBlock(height, core.ScanOptions{
			ExtractMessage:      true,
			ConfidenceThreshold: 0.5,
			IncludeMetadata:     true,
		})
		if err != nil {
			log.Printf("Failed to scan block via scanner manager: %v", err)
			blockScanResponse = nil
		}
	}

	// Convert block scan response to the format expected by the rest of the code
	var scanResults []map[string]any
	if blockScanResponse != nil {
		// Use results from block scan
		scanResults = make([]map[string]any, len(blockScanResponse.Inscriptions))
		for i, inscription := range blockScanResponse.Inscriptions {
			result := map[string]any{
				"tx_id":             inscription.TxID,
				"image_index":       i,
				"file_name":         inscription.FileName,
				"size_bytes":        inscription.SizeBytes,
				"format":            "unknown", // Could extract from content_type
				"scanned_at":        time.Now().Unix(),
				"is_stego":          false,
				"confidence":        0.0,
				"stego_type":        "",
				"extracted_message": "",
				"scan_error":        "",
				"stego_details":     nil,
			}

			if inscription.ScanResult != nil {
				result["is_stego"] = inscription.ScanResult.IsStego
				result["confidence"] = inscription.ScanResult.Confidence
				if inscription.ScanResult.StegoType != "" {
					result["stego_type"] = inscription.ScanResult.StegoType
				}
				if inscription.ScanResult.ExtractedMessage != "" {
					result["extracted_message"] = inscription.ScanResult.ExtractedMessage
				}
				if inscription.ScanResult.ExtractionError != "" {
					result["scan_error"] = inscription.ScanResult.ExtractionError
				}
			}

			scanResults[i] = result
		}
		log.Printf("Block scan completed: %d inscriptions scanned, %d stego detected", blockScanResponse.TotalInscriptions, blockScanResponse.StegoDetected)
	} else {
		// Fallback to empty results
		log.Printf("No block scan results available, using empty results for %d images", len(parsedBlock.Images))
		scanResults = bm.createEmptyScanResults(len(parsedBlock.Images))
	}

	stegoCount := bm.countStegoImagesFromAPIResponse(scanResults)
	log.Printf("Steganography scan completed: %d images scanned, %d with stego detected",
		len(scanResults), stegoCount)

	// Create inscriptions data
	inscriptions := bm.createInscriptionsFromImages(parsedBlock.Images)

	// Create smart contracts and reconcile with ingested uploads when possible
	smartContracts := bm.createSmartContractsFromScanResults(scanResults)
	smartContracts = bm.reconcileIngestionContracts(blockDir, parsedBlock, scanResults, smartContracts, height)
	smartContracts = bm.reconcileOracleIngestions(blockDir, parsedBlock, smartContracts, height)

	// Save block summary JSON for frontend API with scan results
	if err := bm.saveBlockSummaryWithScanResults(blockDir, parsedBlock, inscriptions, scanResults, height, smartContracts); err != nil {
		log.Printf("Failed to save block summary: %v", err)
	}

	processingTime := time.Since(startTime)
	bm.lastProcessTime = processingTime

	// Create block response for storage
	blockResponse := &BlockInscriptionsResponse{
		BlockHeight:       height,
		BlockHash:         parsedBlock.Header.Hash,
		Timestamp:         int64(parsedBlock.Header.Timestamp),
		TotalTransactions: len(parsedBlock.Transactions),
		Inscriptions:      inscriptions,
		Images:            parsedBlock.Images,
		SmartContracts:    smartContracts,
		ProcessingTime:    processingTime.Milliseconds(),
		Success:           true,
	}

	// Store in data storage if available
	if bm.dataStorage != nil {
		if err := bm.dataStorage.StoreBlockData(blockResponse, scanResults); err != nil {
			log.Printf("Failed to store block data in storage: %v", err)
		} else {
			log.Printf("Successfully stored block %d data in storage", height)
		}
	}

	// Update statistics
	bm.totalTransactions += int64(len(parsedBlock.Transactions))
	bm.totalImages += int64(len(parsedBlock.Images))
	bm.totalInscriptions += int64(len(inscriptions))
	bm.totalStegoContracts += int64(bm.countStegoImages(scanResults))

	log.Printf("Successfully processed block %d in %v: %d txs, %d images, %d inscriptions, %d stego detected",
		height, processingTime, len(parsedBlock.Transactions), len(parsedBlock.Images), len(inscriptions), bm.countStegoImages(scanResults))

	return nil
}

// ReconcileRecentBlocks forces a reprocess of the most recent N blocks.
func (bm *BlockMonitor) ReconcileRecentBlocks(ctx context.Context, count int) error {
	if count <= 0 {
		return nil
	}
	bm.reconcileMu.Lock()
	defer bm.reconcileMu.Unlock()

	height, err := bm.getCurrentHeightFromBlockchainInfo()
	if err != nil {
		return fmt.Errorf("get current height: %w", err)
	}
	for i := 0; i < count; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		h := height - int64(i)
		if h < 0 {
			break
		}
		if err := bm.ProcessBlock(h); err != nil {
			log.Printf("reconcile recent blocks: failed to process block %d: %v", h, err)
		}
	}
	return nil
}

// saveBlockData saves raw block data to files
func (bm *BlockMonitor) saveBlockData(blockDir string, parsedBlock *ParsedBlock, hexData string) error {
	// Save raw hex data
	hexFile := filepath.Join(blockDir, "block.hex")
	if err := os.WriteFile(hexFile, []byte(hexData), 0644); err != nil {
		return fmt.Errorf("failed to write hex file: %w", err)
	}

	// Save parsed block data as JSON
	blockData := BlockData{
		BlockHeader: BlockHeader{
			Version:    parsedBlock.Header.Version,
			PrevBlock:  parsedBlock.Header.PrevBlock,
			MerkleRoot: parsedBlock.Header.MerkleRoot,
			Timestamp:  parsedBlock.Header.Timestamp,
			Bits:       parsedBlock.Header.Bits,
			Nonce:      parsedBlock.Header.Nonce,
			Hash:       parsedBlock.Header.Hash,
		},
		Transactions:    bm.convertTransactions(parsedBlock.Transactions),
		ExtractedImages: parsedBlock.Images,
		Metadata: BlockMetadata{
			SourceFile:     fmt.Sprintf("block_%s.hex", parsedBlock.Header.Hash),
			FileSize:       int64(len(hexData)),
			ParserVersion:  "1.0.0",
			ProcessingTime: time.Now().Unix(),
		},
		ProcessingInfo: ProcessingInfo{
			StartedAt:   time.Now(),
			CompletedAt: time.Now(),
			Version:     "1.0.0",
			APISources:  []string{"blockchain.info", "raw_parser"},
			Success:     true,
		},
	}

	blockJSON, err := json.MarshalIndent(blockData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal block data: %w", err)
	}

	blockFile := filepath.Join(blockDir, "block.json")
	if err := os.WriteFile(blockFile, blockJSON, 0644); err != nil {
		return fmt.Errorf("failed to write block JSON: %w", err)
	}

	return nil
}

// saveImages saves extracted images to files
func (bm *BlockMonitor) saveImages(blockDir string, images []ExtractedImageData) error {
	if len(images) == 0 {
		return nil
	}

	imagesDir := filepath.Join(blockDir, "images")
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return fmt.Errorf("failed to create images directory: %w", err)
	}

	for _, image := range images {
		cleaned := sanitizeExtractedImage(image)
		imageFile := filepath.Join(imagesDir, cleaned.FileName)
		// Save the actual image data
		if err := os.WriteFile(imageFile, cleaned.Data, 0644); err != nil {
			log.Printf("Failed to save image %s: %v", cleaned.FileName, err)
		} else {
			log.Printf("Successfully saved image %s (%d bytes)", cleaned.FileName, len(cleaned.Data))
		}
	}

	return nil
}

// createInscriptionsFromImages creates inscription data from extracted images
func (bm *BlockMonitor) createInscriptionsFromImages(images []ExtractedImageData) []InscriptionData {
	var inscriptions []InscriptionData

	for i, image := range images {
		cleaned := sanitizeExtractedImage(image)
		contentType := image.ContentType
		if contentType == "" {
			if strings.HasPrefix(image.Format, "text") || image.Format == "txt" {
				contentType = "text/plain"
			} else {
				contentType = fmt.Sprintf("image/%s", image.Format)
			}
		}
		content := ""
		if strings.HasPrefix(contentType, "text/") {
			content = string(cleaned.Data)
		} else {
			// Avoid storing binary blobs in the DB payload; rely on disk + /content API for retrieval.
			content = ""
		}

		inscription := InscriptionData{
			TxID:        image.TxID,
			InputIndex:  i,
			ContentType: contentType,
			Content:     content,
			SizeBytes:   cleaned.SizeBytes,
			FileName:    cleaned.FileName,
			FilePath:    cleaned.FilePath,
		}
		inscriptions = append(inscriptions, inscription)
	}

	return inscriptions
}

// saveBlockSummary saves a comprehensive block summary for frontend API
func (bm *BlockMonitor) saveBlockSummary(blockDir string, parsedBlock *ParsedBlock, inscriptions []InscriptionData, blockHeight int64) error {
	cleanedImages := make([]ExtractedImageData, len(parsedBlock.Images))
	for i, img := range parsedBlock.Images {
		cleanedImages[i] = sanitizeExtractedImage(img)
	}
	cleanedInscriptions := sanitizeInscriptionsForDisk(inscriptions)

	summary := BlockInscriptionsResponse{
		BlockHeight:       blockHeight,
		BlockHash:         parsedBlock.Header.Hash,
		Timestamp:         int64(parsedBlock.Header.Timestamp),
		TotalTransactions: len(parsedBlock.Transactions),
		Inscriptions:      cleanedInscriptions,
		Images:            cleanedImages,
		SmartContracts:    []SmartContractData{},
		ProcessingTime:    time.Now().Unix(),
		Success:           true,
	}

	summaryJSON, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal summary: %w", err)
	}

	summaryFile := filepath.Join(blockDir, "inscriptions.json")
	if err := os.WriteFile(summaryFile, summaryJSON, 0644); err != nil {
		return fmt.Errorf("failed to write summary file: %w", err)
	}

	return nil
}

// calculateTransactionSize calculates the size of a transaction in bytes
func (bm *BlockMonitor) calculateTransactionSize(tx Transaction) int {
	size := 4 // Version (4 bytes)

	// Input count (varint)
	size += encodeVarIntSize(len(tx.Inputs))

	// Inputs
	for _, input := range tx.Inputs {
		size += 32 // Previous txid
		size += 4  // Previous index
		size += encodeVarIntSize(len(input.ScriptSig)) + len(input.ScriptSig)
		size += 4 // Sequence
	}

	// Output count (varint)
	size += encodeVarIntSize(len(tx.Outputs))

	// Outputs
	for _, output := range tx.Outputs {
		size += 8 // Value
		size += encodeVarIntSize(len(output.ScriptPubKey)) + len(output.ScriptPubKey)
	}

	// Witness data (if present)
	if tx.HasWitness {
		size += 1 // Marker
		size += 1 // Flag
		for _, witness := range tx.Witness {
			size += encodeVarIntSize(len(witness)) + len(witness)
		}
	}

	size += 4 // Locktime (4 bytes)
	return size
}

// encodeVarIntSize returns the size of a varint encoding for the given value
func encodeVarIntSize(value int) int {
	if value < 0xfd {
		return 1
	} else if value <= 0xffff {
		return 3
	} else if value <= 0xffffffff {
		return 5
	} else {
		return 9
	}
}

// sanitizeExtractedImage removes opcode prefixes and stray metadata from payloads before persisting to disk.
func sanitizeExtractedImage(img ExtractedImageData) ExtractedImageData {
	cleaned := img
	data := img.Data
	mime := strings.ToLower(strings.TrimSpace(img.ContentType))
	if mime == "" && img.Format != "" {
		if strings.HasPrefix(img.Format, "text") || img.Format == "txt" {
			mime = "text/plain"
		} else {
			mime = fmt.Sprintf("image/%s", img.Format)
		}
	}

	if strings.HasPrefix(mime, "image/") {
		if strings.HasPrefix(mime, "image/svg") {
			// Treat SVG as text-like for cleanup.
			if cleanedData := stripPushdataPrefixLocal(data); len(cleanedData) > 0 {
				data = cleanedData
			}
			if cleanedData := stripNonPrintablePrefixLocal(data); len(cleanedData) > 0 {
				data = cleanedData
			}
			if idx := bytes.IndexByte(data, '<'); idx >= 0 {
				data = data[idx:]
			}
		} else if trimmed := trimToImageSignatureLocal(data); len(trimmed) > 0 {
			data = trimmed
		}
	} else {
		if cleanedData := stripPushdataPrefixLocal(data); len(cleanedData) > 0 {
			data = cleanedData
		}
		if cleanedData := stripNonPrintablePrefixLocal(data); len(cleanedData) > 0 {
			data = cleanedData
		}
		// HTML bodies may have leading metadata/prefix bytes before the first tag.
		if strings.HasPrefix(mime, "text/html") || strings.HasSuffix(strings.ToLower(img.FileName), ".html") {
			if idx := bytes.IndexByte(data, '<'); idx >= 0 {
				data = data[idx:]
			}
		}
	}

	cleaned.Data = data
	cleaned.SizeBytes = len(data)
	return cleaned
}

// sanitizeInscriptionsForDisk cleans inscription content before writing JSON summaries.
func sanitizeInscriptionsForDisk(inscriptions []InscriptionData) []InscriptionData {
	out := make([]InscriptionData, len(inscriptions))
	for i, ins := range inscriptions {
		cleaned := ins
		data := []byte(ins.Content)
		mime := strings.ToLower(strings.TrimSpace(ins.ContentType))

		if strings.HasPrefix(mime, "image/") {
			if trimmed := trimToImageSignatureLocal(data); len(trimmed) > 0 {
				data = trimmed
			}
		} else if strings.HasPrefix(mime, "image/svg") {
			// Treat SVG as text-ish.
			if cleanedData := stripPushdataPrefixLocal(data); len(cleanedData) > 0 {
				data = cleanedData
			}
			if cleanedData := stripNonPrintablePrefixLocal(data); len(cleanedData) > 0 {
				data = cleanedData
			}
			if idx := bytes.IndexByte(data, '<'); idx >= 0 {
				data = data[idx:]
			}
		} else {
			if cleanedData := stripPushdataPrefixLocal(data); len(cleanedData) > 0 {
				data = cleanedData
			}
			if cleanedData := stripNonPrintablePrefixLocal(data); len(cleanedData) > 0 {
				data = cleanedData
			}
			if strings.HasPrefix(mime, "text/html") || strings.HasSuffix(strings.ToLower(ins.FileName), ".html") {
				if idx := bytes.IndexByte(data, '<'); idx >= 0 {
					data = data[idx:]
				}
			}
		}

		cleaned.Content = string(data)
		cleaned.SizeBytes = len(data)
		out[i] = cleaned
	}
	return out
}

// stripPushdataPrefixLocal removes a leading push opcode (OP_PUSH, OP_PUSHDATA1/2/4) from a payload when present.
func stripPushdataPrefixLocal(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	op := b[0]
	switch {
	case op <= 75:
		if len(b) > int(op) {
			return b[1:]
		}
	case op == 0x4c && len(b) > 1: // OP_PUSHDATA1
		l := int(b[1])
		if len(b) >= 2+l {
			return b[2:]
		}
	case op == 0x4d && len(b) > 2: // OP_PUSHDATA2
		l := int(b[1]) | int(b[2])<<8
		if len(b) >= 3+l {
			return b[3:]
		}
	case op == 0x4e && len(b) > 4: // OP_PUSHDATA4
		l := int(b[1]) | int(b[2])<<8 | int(b[3])<<16 | int(b[4])<<24
		if len(b) >= 5+l {
			return b[5:]
		}
	}
	return b
}

// stripNonPrintablePrefixLocal trims leading control bytes to get to the printable payload.
func stripNonPrintablePrefixLocal(b []byte) []byte {
	i := 0
	for i < len(b) {
		c := b[i]
		if c == '\n' || c == '\r' || c == '\t' || (c >= 32 && c < 127) {
			break
		}
		i++
	}
	if i > 0 && i < len(b) {
		return b[i:]
	}
	return b
}

// convertTransactions converts parsed transactions to transaction data format
func (bm *BlockMonitor) convertTransactions(transactions []Transaction) []TransactionData {
	var txData []TransactionData

	for _, tx := range transactions {
		witnessCount := 0
		for _, stack := range tx.InputWitnesses {
			witnessCount += len(stack)
		}
		data := TransactionData{
			TxID:       tx.TxID,
			Height:     0, // Will be set by caller
			Time:       int64(tx.Locktime),
			Status:     "confirmed",
			HasImages:  witnessCount > 0,
			ImageCount: witnessCount,
		}
		txData = append(txData, data)
	}

	return txData
}

// scanBlockViaAPI calls the /scan/block API endpoint to scan a block
func (bm *BlockMonitor) scanBlockViaAPI(height int64) ([]map[string]any, error) {
	// Create the request for the /scan/block API
	request := core.BlockScanRequest{
		BlockHeight: int(height),
		ScanOptions: core.ScanOptions{
			ExtractMessage:      true,
			ConfidenceThreshold: 0.5,
			IncludeMetadata:     true,
		},
	}

	// Marshal the request
	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal scan request: %w", err)
	}

	// Make HTTP request to the Go backend's Bitcoin API
	client := &http.Client{Timeout: 300 * time.Second} // 5 minute timeout for large blocks
	req, err := http.NewRequest("POST", "http://localhost:3001/bitcoin/v1/scan/block", bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call scan API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("scan API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var scanResponse core.BlockScanResponse
	if err := json.NewDecoder(resp.Body).Decode(&scanResponse); err != nil {
		return nil, fmt.Errorf("failed to decode scan response: %w", err)
	}

	// Convert the API response to the expected format for block monitor
	var results []map[string]any
	for i, inscription := range scanResponse.Inscriptions {
		result := map[string]any{
			"tx_id":             inscription.TxID,
			"image_index":       i,
			"file_name":         inscription.FileName,
			"size_bytes":        inscription.SizeBytes,
			"format":            "unknown",
			"scanned_at":        time.Now().Unix(),
			"is_stego":          false,
			"confidence":        0.0,
			"stego_type":        "",
			"extracted_message": "",
			"scan_error":        "",
			"stego_details":     nil,
		}

		if inscription.ScanResult != nil {
			result["is_stego"] = inscription.ScanResult.IsStego
			result["confidence"] = inscription.ScanResult.Confidence
			if inscription.ScanResult.StegoType != "" {
				result["stego_type"] = inscription.ScanResult.StegoType
			}
			if inscription.ScanResult.ExtractedMessage != "" {
				result["extracted_message"] = inscription.ScanResult.ExtractedMessage
			}
			if inscription.ScanResult.ExtractionError != "" {
				result["scan_error"] = inscription.ScanResult.ExtractionError
			}
		}

		results = append(results, result)
	}

	return results, nil
}

// countStegoImagesFromAPIResponse counts stego detections from API response
func (bm *BlockMonitor) countStegoImagesFromAPIResponse(scanResults []map[string]any) int {
	count := 0
	for _, result := range scanResults {
		if isStego, ok := result["is_stego"].(bool); ok && isStego {
			count++
		}
	}
	return count
}

// countStegoImages counts how many images have steganography detected
func (bm *BlockMonitor) countStegoImages(scanResults []map[string]any) int {
	return bm.countStegoImagesFromAPIResponse(scanResults)
}

// scanImagesDirectly scans images using the BitcoinAPI directly
func (bm *BlockMonitor) scanImagesDirectly(images []ExtractedImageData) ([]map[string]any, error) {
	log.Printf("scanImagesDirectly called with %d images", len(images))
	var results []map[string]any

	for i, image := range images {
		// Create scan result for this image
		result := map[string]any{
			"tx_id":             image.TxID,
			"image_index":       i,
			"file_name":         image.FileName,
			"size_bytes":        image.SizeBytes,
			"format":            image.Format,
			"scanned_at":        time.Now().Unix(),
			"is_stego":          false,
			"confidence":        0.0,
			"stego_type":        "",
			"extracted_message": "",
			"scan_error":        "",
			"stego_details":     nil,
		}

		// Try to scan the image using the scanner manager
		if bm.bitcoinAPI != nil && bm.bitcoinAPI.scannerManager != nil {
			log.Printf("Scanning image %d: %s (%d bytes)", i, image.FileName, len(image.Data))
			scanResult, err := bm.bitcoinAPI.scannerManager.ScanImage(image.Data, core.ScanOptions{
				ExtractMessage:      true,
				ConfidenceThreshold: 0.5,
				IncludeMetadata:     true,
			})
			if err != nil {
				log.Printf("Failed to scan image %s: %v", image.FileName, err)
				result["scan_error"] = err.Error()
			} else {
				log.Printf("Scanned image %s: is_stego=%v, confidence=%.2f", image.FileName, scanResult.IsStego, scanResult.Confidence)
				result["is_stego"] = scanResult.IsStego
				result["confidence"] = scanResult.Confidence
				if scanResult.StegoType != "" {
					result["stego_type"] = scanResult.StegoType
				}
				if scanResult.ExtractedMessage != "" {
					result["extracted_message"] = scanResult.ExtractedMessage
				}
				if scanResult.ExtractionError != "" {
					result["scan_error"] = scanResult.ExtractionError
				}
			}
		} else {
			log.Printf("Scanner not available for image %s", image.FileName)
			result["scan_error"] = "Scanner not available"
		}

		results = append(results, result)
	}

	log.Printf("scanImagesDirectly completed, scanned %d images", len(results))
	return results, nil
}

// createEmptyScanResults creates empty scan results for all images
func (bm *BlockMonitor) createEmptyScanResults(count int) []map[string]any {
	results := make([]map[string]any, count)
	for i := 0; i < count; i++ {
		results[i] = map[string]any{
			"tx_id":             "",
			"image_index":       i,
			"file_name":         "",
			"size_bytes":        0,
			"format":            "",
			"scanned_at":        time.Now().Unix(),
			"is_stego":          false,
			"confidence":        0.0,
			"stego_type":        "",
			"extracted_message": "",
			"scan_error":        "not_scanned",
			"stego_details":     nil,
		}
	}
	return results
}

// saveBlockSummaryWithScanResults saves block summary including steganography scan results
func (bm *BlockMonitor) saveBlockSummaryWithScanResults(blockDir string, parsedBlock *ParsedBlock, inscriptions []InscriptionData, scanResults []map[string]any, blockHeight int64, smartContracts []SmartContractData) error {
	// Count stego detections
	stegoCount := bm.countStegoImages(scanResults)

	// Clean payloads before persisting to disk so downstream consumers don't see opcode metadata.
	cleanedImages := make([]ExtractedImageData, len(parsedBlock.Images))
	for i, img := range parsedBlock.Images {
		cleanedImages[i] = sanitizeExtractedImage(img)
	}
	cleanedInscriptions := sanitizeInscriptionsForDisk(inscriptions)

	// Create optimized summary with minimal data
	summary := BlockInscriptionsResponse{
		BlockHeight:       blockHeight,
		BlockHash:         parsedBlock.Header.Hash,
		Timestamp:         int64(parsedBlock.Header.Timestamp),
		TotalTransactions: len(parsedBlock.Transactions),
		Inscriptions:      cleanedInscriptions,
		Images:            cleanedImages,
		SmartContracts:    smartContracts,
		ProcessingTime:    time.Now().Unix(),
		Success:           true,
	}

	// Create compact steganography summary (only once)
	steganographySummary := map[string]any{
		"total_images":   len(cleanedImages),
		"stego_detected": stegoCount > 0,
		"stego_count":    stegoCount,
		"scan_timestamp": time.Now().Unix(),
	}

	// Create enhanced images with scan results
	enhancedImages := make([]map[string]any, len(cleanedImages))
	for i, image := range cleanedImages {
		enhancedImage := map[string]any{
			"tx_id":      image.TxID,
			"format":     image.Format,
			"size_bytes": image.SizeBytes,
			"file_name":  image.FileName,
			"file_path":  image.FilePath,
		}

		// Add scan result if available
		if len(scanResults) > i {
			scanResult := scanResults[i]
			if scanResult != nil {
				enhancedImage["scan_result"] = scanResult
			} else {
				// Default scan result for unscanned images
				enhancedImage["scan_result"] = map[string]any{
					"is_stego":          false,
					"stego_probability": 0.0,
					"confidence":        0.0,
					"prediction":        "not_scanned",
				}
			}
		} else {
			// Default scan result for unscanned images
			enhancedImage["scan_result"] = map[string]any{
				"is_stego":          false,
				"stego_probability": 0.0,
				"confidence":        0.0,
				"prediction":        "not_scanned",
			}
		}

		enhancedImages[i] = enhancedImage
	}

	// Only include steganography data if detections exist
	var enhancedSummary map[string]any
	if stegoCount > 0 {
		enhancedSummary = map[string]any{
			"block_height":       summary.BlockHeight,
			"block_hash":         summary.BlockHash,
			"timestamp":          summary.Timestamp,
			"total_transactions": summary.TotalTransactions,
			"inscriptions":       summary.Inscriptions,
			"images":             enhancedImages,
			"smart_contracts":    summary.SmartContracts,
			"processing_time_ms": summary.ProcessingTime,
			"success":            summary.Success,
			"steganography_scan": steganographySummary,
		}
	} else {
		enhancedSummary = map[string]any{
			"block_height":       summary.BlockHeight,
			"block_hash":         summary.BlockHash,
			"timestamp":          summary.Timestamp,
			"total_transactions": summary.TotalTransactions,
			"inscriptions":       summary.Inscriptions,
			"images":             enhancedImages,
			"smart_contracts":    summary.SmartContracts,
			"processing_time_ms": summary.ProcessingTime,
			"success":            summary.Success,
		}
	}

	summaryJSON, err := json.MarshalIndent(enhancedSummary, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal enhanced summary: %w", err)
	}

	summaryFile := filepath.Join(blockDir, "inscriptions.json")
	if err := os.WriteFile(summaryFile, summaryJSON, 0644); err != nil {
		return fmt.Errorf("failed to write summary file: %w", err)
	}

	return nil
}

// createSmartContractsFromScanResults creates smart contract data from steganography scan results
func (bm *BlockMonitor) createSmartContractsFromScanResults(scanResults []map[string]any) []SmartContractData {
	var contracts []SmartContractData

	for _, result := range scanResults {
		if isStego, ok := result["is_stego"].(bool); ok && isStego {
			contract := SmartContractData{
				ContractID:  fmt.Sprintf("stego_%v_%d", result["image_index"], time.Now().Unix()),
				BlockHeight: 0, // Will be set by caller
				ImagePath:   fmt.Sprintf("%v", result["file_name"]),
				Confidence:  0.0,
				Metadata: map[string]any{
					"tx_id":             result["tx_id"],
					"image_index":       result["image_index"],
					"stego_type":        result["stego_type"],
					"extracted_message": result["extracted_message"],
					"scan_confidence":   result["confidence"],
					"scan_timestamp":    result["scanned_at"],
					"format":            result["format"],
					"size_bytes":        result["size_bytes"],
				},
			}

			if conf, ok := result["confidence"].(float64); ok {
				contract.Confidence = conf
			}

			contracts = append(contracts, contract)
		}
	}

	return contracts
}

type scanPayload struct {
	message          string
	payoutAddress    string
	payoutScript     string
	payoutScriptHash string
}

func (bm *BlockMonitor) reconcileIngestionContracts(blockDir string, parsedBlock *ParsedBlock, scanResults []map[string]any, smartContracts []SmartContractData, blockHeight int64) []SmartContractData {
	if bm.ingestion == nil || len(scanResults) == 0 {
		return smartContracts
	}

	txByID := make(map[string]Transaction, len(parsedBlock.Transactions))
	for _, tx := range parsedBlock.Transactions {
		if tx.TxID != "" {
			txByID[tx.TxID] = tx
		}
	}

	for i := range smartContracts {
		smartContracts[i].BlockHeight = blockHeight
	}

	for _, result := range scanResults {
		isStego, _ := result["is_stego"].(bool)
		if !isStego {
			continue
		}

		txID := stringFromAny(result["tx_id"])
		if txID == "" {
			continue
		}

		tx, ok := txByID[txID]
		if !ok {
			continue
		}

		image := bm.findImageForScanResult(parsedBlock.Images, result)
		if image == nil || len(image.Data) == 0 {
			continue
		}

		payload := parseScanPayload(result)
		if payload.message == "" {
			continue
		}

		cleanedImage := sanitizeExtractedImage(*image)
		visibleHash := visiblePixelHash(cleanedImage.Data, payload.message)
		if visibleHash == "" {
			continue
		}

		rec, err := bm.ingestion.Get(visibleHash)
		if err != nil {
			continue
		}

		matchedScript, ok := bm.matchPayoutScript(tx, payload)
		if !ok {
			continue
		}

		destPath, err := bm.moveIngestionImage(blockDir, rec)
		if err != nil {
			log.Printf("Failed to move ingestion image for %s: %v", visibleHash, err)
			bm.maybeReconcileStego(rec)
			continue
		}
		bm.maybeReconcileStego(rec)

		imageFile := filepath.Base(destPath)
		imagePath := filepath.Join("images", imageFile)
		contractMeta := buildContractMetadata(result)
		contractMeta["visible_pixel_hash"] = visibleHash
		if payload.payoutAddress != "" {
			contractMeta["payout_address"] = payload.payoutAddress
		}
		if payload.payoutScript != "" {
			contractMeta["payout_script"] = payload.payoutScript
		}
		if payload.payoutScriptHash != "" {
			contractMeta["payout_script_hash"] = payload.payoutScriptHash
		}
		if len(matchedScript) > 0 && payload.payoutScriptHash == "" {
			contractMeta["payout_script_hash_sha256"] = scriptHashHex(matchedScript)
			contractMeta["payout_script_hash160"] = scriptHash160Hex(matchedScript)
		}
		contractMeta["ingestion_id"] = rec.ID
		contractMeta["image_file"] = imageFile
		contractMeta["image_path"] = imagePath

		if updated := updateContractEntry(smartContracts, result, SmartContractData{
			ContractID:  visibleHash,
			BlockHeight: blockHeight,
			ImagePath:   imagePath,
			Confidence:  confidenceFromAny(result["confidence"]),
			Metadata:    contractMeta,
		}); !updated {
			smartContracts = append(smartContracts, SmartContractData{
				ContractID:  visibleHash,
				BlockHeight: blockHeight,
				ImagePath:   imagePath,
				Confidence:  confidenceFromAny(result["confidence"]),
				Metadata:    contractMeta,
			})
		}
	}

	return smartContracts
}

func (bm *BlockMonitor) reconcileOracleIngestions(blockDir string, parsedBlock *ParsedBlock, smartContracts []SmartContractData, blockHeight int64) []SmartContractData {
	if bm.ingestion == nil || len(parsedBlock.Transactions) == 0 {
		return smartContracts
	}

	recs, err := bm.ingestion.ListRecent("", 500)
	if err != nil {
		log.Printf("oracle reconcile: failed to list ingestions: %v", err)
		return smartContracts
	}

	primaryCandidates := make(map[string]*services.IngestionRecord, len(recs))
	fallbackCandidates := make(map[string]*services.IngestionRecord, len(recs))
	candidatesByID := make(map[string][]string, len(recs))
	txidMatches := make(map[string]*services.IngestionRecord, len(recs))
	matchedTxIDs := make(map[string]string)
	for _, rec := range recs {
		recCopy := rec
		primaryList, fallbackList := ingestionCandidateBuckets(recCopy, bm.networkParams())
		for _, candidate := range primaryList {
			primaryCandidates[candidate] = &recCopy
			candidatesByID[recCopy.ID] = append(candidatesByID[recCopy.ID], candidate)
		}
		for _, candidate := range fallbackList {
			fallbackCandidates[candidate] = &recCopy
			candidatesByID[recCopy.ID] = append(candidatesByID[recCopy.ID], candidate)
		}
		for _, txid := range fundingTxIDsFromMeta(recCopy.Metadata) {
			txidMatches[txid] = &recCopy
		}
	}
	if len(primaryCandidates) == 0 && len(fallbackCandidates) == 0 && len(txidMatches) == 0 {
		return smartContracts
	}
	log.Printf("oracle reconcile: %d primary hashes, %d fallback hashes, %d funding txids across %d ingestions", len(primaryCandidates), len(fallbackCandidates), len(txidMatches), len(recs))

	for _, tx := range parsedBlock.Transactions {
		if match, ok := txidMatches[tx.TxID]; ok && match != nil {
			destPath, err := bm.moveIngestionImageWithFilename(blockDir, match, blockImageFilename(match, tx.TxID))
			if err != nil {
				log.Printf("oracle reconcile: failed to move ingestion image for %s: %v", match.ID, err)
				bm.maybeReconcileStego(match)
			} else {
				bm.maybeReconcileStego(match)
				log.Printf("oracle reconcile: matched ingestion %s via funding_txid=%s", match.ID, tx.TxID)
				imageFile := filepath.Base(destPath)
				imagePath := filepath.Join("images", imageFile)
				contractMeta := map[string]any{
					"tx_id":              tx.TxID,
					"output_index":       0,
					"block_height":       blockHeight,
					"match_type":         "funding_txid",
					"match_hash":         tx.TxID,
					"image_file":         imageFile,
					"image_path":         imagePath,
					"ingestion_id":       match.ID,
					"visible_pixel_hash": stringFromAny(match.Metadata["visible_pixel_hash"]),
				}
				mergeIngestionMetadata(contractMeta, match.Metadata)
				applyStegoMetadata(contractMeta, match.Metadata)
				smartContracts = upsertContractByID(smartContracts, SmartContractData{
					ContractID:  match.ID,
					BlockHeight: blockHeight,
					ImagePath:   imagePath,
					Confidence:  0,
					Metadata:    contractMeta,
				})
				bm.markIngestionConfirmed(match, tx.TxID, blockHeight, imageFile, imagePath)
				bm.updateTaskFundingProofsFromTx(match.ID, tx, blockHeight)
				bm.confirmAndSweepContractTasks(match.ID, tx.TxID, blockHeight)
				for _, candidate := range candidatesByID[match.ID] {
					delete(primaryCandidates, candidate)
					delete(fallbackCandidates, candidate)
				}
				delete(txidMatches, tx.TxID)
				matchedTxIDs[tx.TxID] = match.ID
			}
		}

		if match, matchType, matchedHash := matchWitnessHash(tx, primaryCandidates, fallbackCandidates); match != nil {
			if existingID, ok := matchedTxIDs[tx.TxID]; ok && existingID != match.ID {
				log.Printf("oracle reconcile: skipping %s match for %s (tx %s already matched by funding_txid)", matchType, match.ID, tx.TxID)
			} else {
				destPath, err := bm.moveIngestionImageWithFilename(blockDir, match, blockImageFilename(match, tx.TxID))
				if err != nil {
					log.Printf("oracle reconcile: failed to move ingestion image for %s: %v", match.ID, err)
					bm.maybeReconcileStego(match)
				} else {
					bm.maybeReconcileStego(match)
					log.Printf("oracle reconcile: matched ingestion %s via %s=%s in tx %s witness", match.ID, matchType, matchedHash, tx.TxID)
					imageFile := filepath.Base(destPath)
					imagePath := filepath.Join("images", imageFile)
					contractMeta := map[string]any{
						"tx_id":              tx.TxID,
						"block_height":       blockHeight,
						"match_type":         matchType,
						"match_hash":         matchedHash,
						"image_file":         imageFile,
						"image_path":         imagePath,
						"ingestion_id":       match.ID,
						"visible_pixel_hash": stringFromAny(match.Metadata["visible_pixel_hash"]),
					}
					mergeIngestionMetadata(contractMeta, match.Metadata)
					applyStegoMetadata(contractMeta, match.Metadata)
					smartContracts = upsertContractByID(smartContracts, SmartContractData{
						ContractID:  match.ID,
						BlockHeight: blockHeight,
						ImagePath:   imagePath,
						Confidence:  0,
						Metadata:    contractMeta,
					})
					bm.markIngestionConfirmed(match, tx.TxID, blockHeight, imageFile, imagePath)
					bm.updateTaskFundingProofsFromTx(match.ID, tx, blockHeight)
					bm.confirmAndSweepContractTasks(match.ID, tx.TxID, blockHeight)
					for _, candidate := range candidatesByID[match.ID] {
						delete(primaryCandidates, candidate)
						delete(fallbackCandidates, candidate)
					}
				}
			}
		}

		for outIdx, output := range tx.Outputs {
			match, matchType, matchedHash := matchOracleOutput(output.ScriptPubKey, bm.networkParams(), primaryCandidates)
			if match == nil {
				match, matchType, matchedHash = matchOracleOutput(output.ScriptPubKey, bm.networkParams(), fallbackCandidates)
			}
			if match == nil {
				continue
			}
			if existingID, ok := matchedTxIDs[tx.TxID]; ok && existingID != match.ID {
				log.Printf("oracle reconcile: skipping %s match for %s (tx %s already matched by funding_txid)", matchType, match.ID, tx.TxID)
				continue
			}

			destPath, err := bm.moveIngestionImageWithFilename(blockDir, match, blockImageFilename(match, tx.TxID))
			if err != nil {
				log.Printf("oracle reconcile: failed to move ingestion image for %s: %v", match.ID, err)
				bm.maybeReconcileStego(match)
				continue
			}
			bm.maybeReconcileStego(match)
			log.Printf("oracle reconcile: matched ingestion %s via %s=%s in tx %s output %d", match.ID, matchType, matchedHash, tx.TxID, outIdx)

			imageFile := filepath.Base(destPath)
			imagePath := filepath.Join("images", imageFile)
			contractMeta := map[string]any{
				"tx_id":              tx.TxID,
				"output_index":       outIdx,
				"block_height":       blockHeight,
				"match_type":         matchType,
				"match_hash":         matchedHash,
				"payout_script":      hex.EncodeToString(output.ScriptPubKey),
				"image_file":         imageFile,
				"image_path":         imagePath,
				"ingestion_id":       match.ID,
				"visible_pixel_hash": stringFromAny(match.Metadata["visible_pixel_hash"]),
			}
			mergeIngestionMetadata(contractMeta, match.Metadata)
			applyStegoMetadata(contractMeta, match.Metadata)

			smartContracts = upsertContractByID(smartContracts, SmartContractData{
				ContractID:  match.ID,
				BlockHeight: blockHeight,
				ImagePath:   imagePath,
				Confidence:  0,
				Metadata:    contractMeta,
			})
			bm.markIngestionConfirmed(match, tx.TxID, blockHeight, imageFile, imagePath)
			bm.updateTaskFundingProofsFromTx(match.ID, tx, blockHeight)
			bm.confirmAndSweepContractTasks(match.ID, tx.TxID, blockHeight)

			for _, candidate := range candidatesByID[match.ID] {
				delete(primaryCandidates, candidate)
				delete(fallbackCandidates, candidate)
			}
		}
	}

	return smartContracts
}

func fundingTxIDsFromMeta(meta map[string]any) []string {
	var txids []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range txids {
			if existing == value {
				return
			}
		}
		txids = append(txids, value)
	}
	if meta == nil {
		return txids
	}
	if txid, ok := meta["funding_txid"].(string); ok {
		add(txid)
	}
	switch v := meta["funding_txids"].(type) {
	case []string:
		for _, txid := range v {
			add(txid)
		}
	case []any:
		for _, item := range v {
			if txid, ok := item.(string); ok {
				add(txid)
			}
		}
	case string:
		for _, part := range strings.Split(v, ",") {
			add(part)
		}
	}
	return txids
}

// SetSweepDependencies wires commitment sweep support for oracle reconcile.
func (bm *BlockMonitor) SetSweepDependencies(store SweepTaskStore, mempool *MempoolClient) {
	bm.sweepStore = store
	bm.sweepMempool = mempool
}

func (bm *BlockMonitor) confirmAndSweepContractTasks(contractID, txid string, blockHeight int64) {
	if bm.sweepStore == nil || bm.sweepMempool == nil || strings.TrimSpace(contractID) == "" || strings.TrimSpace(txid) == "" {
		return
	}
	tasks, err := bm.sweepStore.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
	if err != nil {
		log.Printf("oracle reconcile: failed to list tasks for %s: %v", contractID, err)
		return
	}
	log.Printf("oracle reconcile DEBUG: found %d tasks for contract %s", len(tasks), contractID)
	log.Printf("oracle reconcile DEBUG: flushing logs")
	for i, task := range tasks {
		proof := task.MerkleProof
		log.Printf("oracle reconcile DEBUG: task %d - ID: %s, proof: %v", i, task.TaskID, proof != nil)
		log.Printf("oracle reconcile DEBUG: flushing logs")
		log.Println() // Force flush
		if proof == nil {
			log.Printf("oracle reconcile DEBUG: skipping task %s - no proof", task.TaskID)
			continue
		}
		log.Printf("oracle reconcile DEBUG: checking task %s proof.TxID='%s' against txid='%s'", task.TaskID, proof.TxID, txid)
		if proof.TxID == "" || strings.TrimSpace(proof.TxID) != strings.TrimSpace(txid) {
			log.Printf("oracle reconcile DEBUG: skipping task %s - txid mismatch (proof='%s' vs txid='%s')", task.TaskID, proof.TxID, txid)
			continue
		}
		log.Printf("oracle reconcile DEBUG: task %s confirmation_status=%s", task.TaskID, proof.ConfirmationStatus)
		log.Printf("oracle reconcile DEBUG: flushing logs")
		log.Println() // Force flush
		if proof.ConfirmationStatus != "confirmed" {
			now := time.Now()
			proof.ConfirmationStatus = "confirmed"
			proof.ConfirmedAt = &now
			proof.BlockHeight = blockHeight
			log.Printf("oracle reconcile DEBUG: confirming proof for task %s at block %d", task.TaskID, blockHeight)
			if err := bm.sweepStore.UpdateTaskProof(context.Background(), task.TaskID, proof); err != nil {
				log.Printf("oracle reconcile: failed to confirm proof for %s: %v", task.TaskID, err)
			}
		}
		log.Printf("oracle reconcile DEBUG: attempting commitment sweep for task %s", task.TaskID)
		if err := SweepCommitmentIfReady(context.Background(), bm.sweepStore, bm.sweepMempool, task, proof); err != nil {
			log.Printf("oracle reconcile: sweep error for %s: %v", task.TaskID, err)
		} else {
			log.Printf("oracle reconcile DEBUG: commitment sweep successful for task %s", task.TaskID)
		}
	}
}

func (bm *BlockMonitor) updateTaskFundingProofsFromTx(contractID string, tx Transaction, blockHeight int64) {
	if bm.sweepStore == nil || strings.TrimSpace(contractID) == "" {
		return
	}
	tasks, err := bm.sweepStore.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
	if err != nil {
		log.Printf("oracle reconcile: failed to list tasks for funding update %s: %v", contractID, err)
		return
	}
	taskByWallet := make(map[string][]smart_contract.Task)
	for _, task := range tasks {
		wallet := strings.TrimSpace(task.ContractorWallet)
		if wallet == "" && task.MerkleProof != nil {
			wallet = strings.TrimSpace(task.MerkleProof.ContractorWallet)
		}
		if wallet == "" {
			continue
		}
		taskByWallet[wallet] = append(taskByWallet[wallet], task)
	}
	if len(taskByWallet) == 0 {
		return
	}
	now := time.Now()
	for _, output := range tx.Outputs {
		for _, addr := range outputAddresses(output.ScriptPubKey, bm.networkParams()) {
			candidates := taskByWallet[addr]
			if len(candidates) == 0 {
				continue
			}
			bestIdx := -1
			for i, task := range candidates {
				proof := task.MerkleProof
				if proof != nil && strings.TrimSpace(proof.TxID) != "" && strings.TrimSpace(proof.TxID) != strings.TrimSpace(tx.TxID) {
					continue
				}
				if task.BudgetSats > 0 && task.BudgetSats == output.Value {
					bestIdx = i
					break
				}
				if bestIdx == -1 {
					bestIdx = i
				}
			}
			if bestIdx < 0 {
				continue
			}
			task := candidates[bestIdx]
			taskByWallet[addr] = append(candidates[:bestIdx], candidates[bestIdx+1:]...)

			proof := task.MerkleProof
			if proof == nil {
				proof = &smart_contract.MerkleProof{}
			}
			proof.TxID = tx.TxID
			proof.BlockHeight = blockHeight
			proof.FundingAddress = addr
			proof.FundedAmountSats = output.Value
			if proof.ConfirmationStatus == "" || proof.ConfirmationStatus == "provisional" {
				proof.ConfirmationStatus = "confirmed"
			}
			if proof.ConfirmedAt == nil {
				proof.ConfirmedAt = &now
			}
			if proof.SeenAt.IsZero() {
				proof.SeenAt = now
			}
			if proof.ContractorWallet == "" {
				proof.ContractorWallet = addr
			}
			if err := bm.sweepStore.UpdateTaskProof(context.Background(), task.TaskID, proof); err != nil {
				log.Printf("oracle reconcile: failed to update funding proof for %s: %v", task.TaskID, err)
				continue
			}
			if err := SweepCommitmentIfReady(context.Background(), bm.sweepStore, bm.sweepMempool, task, proof); err != nil {
				log.Printf("oracle reconcile: sweep error for %s: %v", task.TaskID, err)
			}
		}
	}
}

func outputAddresses(script []byte, params *chaincfg.Params) []string {
	class, addrs, _, err := txscript.ExtractPkScriptAddrs(script, params)
	if err != nil || class == txscript.NonStandardTy {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if addr != nil {
			out = append(out, addr.EncodeAddress())
		}
	}
	return out
}

func (bm *BlockMonitor) findImageForScanResult(images []ExtractedImageData, result map[string]any) *ExtractedImageData {
	fileName := stringFromAny(result["file_name"])
	if fileName != "" {
		for i := range images {
			if images[i].FileName == fileName {
				return &images[i]
			}
		}
	}
	txID := stringFromAny(result["tx_id"])
	if txID != "" {
		for i := range images {
			if images[i].TxID == txID {
				return &images[i]
			}
		}
	}
	return nil
}

func ingestionCandidateBuckets(rec services.IngestionRecord, params *chaincfg.Params) ([]string, []string) {
	var primary []string
	var fallback []string

	appendPrimary := func(value string) {
		value = normalizeHex(value)
		if len(value) != 40 && len(value) != 64 {
			return
		}
		primary = append(primary, value)
	}
	appendFallback := func(value string) {
		value = normalizeHex(value)
		if len(value) != 40 && len(value) != 64 {
			return
		}
		fallback = append(fallback, value)
	}

	if rec.ID != "" {
		appendFallback(rec.ID)
	}
	if prefix := hashPrefixFromFilename(rec.Filename); prefix != "" {
		appendPrimary(prefix)
	}
	if v := stringFromAny(rec.Metadata["visible_pixel_hash"]); v != "" {
		appendPrimary(v)
	}
	if v := commitmentScriptHashFromMeta(rec, params); v != "" {
		appendPrimary(v)
	}
	if v := stringFromAny(rec.Metadata["pixel_hash"]); v != "" {
		appendPrimary(v)
	}
	if v := stringFromAny(rec.Metadata["payout_script_hash"]); v != "" {
		appendFallback(v)
	}
	switch hashes := rec.Metadata["payout_script_hashes"].(type) {
	case []string:
		for _, hash := range hashes {
			appendFallback(hash)
		}
	case []any:
		for _, hash := range hashes {
			appendFallback(fmt.Sprintf("%v", hash))
		}
	case string:
		for _, hash := range strings.Split(hashes, ",") {
			appendFallback(hash)
		}
	}
	if v := stringFromAny(rec.Metadata["payout_script_hash160"]); v != "" {
		appendFallback(v)
	}
	switch hashes := rec.Metadata["payout_script_hash160s"].(type) {
	case []string:
		for _, hash := range hashes {
			appendFallback(hash)
		}
	case []any:
		for _, hash := range hashes {
			appendFallback(fmt.Sprintf("%v", hash))
		}
	case string:
		for _, hash := range strings.Split(hashes, ",") {
			appendFallback(hash)
		}
	}

	if rec.ImageBase64 != "" {
		if data, err := base64.StdEncoding.DecodeString(rec.ImageBase64); err == nil {
			sum := sha256.Sum256(data)
			appendFallback(hex.EncodeToString(sum[:]))
		}
	}

	return primary, fallback
}

func hashPrefixFromFilename(filename string) string {
	if strings.TrimSpace(filename) == "" {
		return ""
	}
	base := filepath.Base(filename)
	sep := strings.Index(base, "_")
	if sep <= 0 {
		return ""
	}
	prefix := normalizeHex(base[:sep])
	if len(prefix) != 40 && len(prefix) != 64 {
		return ""
	}
	return prefix
}

func commitmentScriptHashFromMeta(rec services.IngestionRecord, params *chaincfg.Params) string {
	if params == nil || rec.Metadata == nil {
		return ""
	}
	visible := normalizeHex(stringFromAny(rec.Metadata["visible_pixel_hash"]))
	if len(visible) != 64 {
		return ""
	}
	lockAddr := strings.TrimSpace(stringFromAny(rec.Metadata["commitment_lock_address"]))
	if lockAddr == "" {
		return ""
	}
	pixelBytes, err := hex.DecodeString(visible)
	if err != nil {
		return ""
	}
	addr, err := btcutil.DecodeAddress(lockAddr, params)
	if err != nil {
		return ""
	}
	_, _, redeemScriptHash, _, err := buildCommitmentScript(params, pixelBytes, addr)
	if err != nil || len(redeemScriptHash) == 0 {
		return ""
	}
	return hex.EncodeToString(redeemScriptHash)
}

func matchOracleOutput(script []byte, params *chaincfg.Params, candidates map[string]*services.IngestionRecord) (*services.IngestionRecord, string, string) {
	if len(script) == 0 {
		return nil, "", ""
	}

	for _, hash := range []string{scriptHashHex(script), scriptHash160Hex(script)} {
		if match, ok := candidates[hash]; ok {
			return match, "script_hash", hash
		}
	}

	if len(candidates) > 0 {
		for _, addrHash := range scriptAddressHashes(script, params) {
			if match, ok := candidates[addrHash]; ok {
				return match, "script_address", addrHash
			}
		}
	}

	return nil, "", ""
}

func matchWitnessHash(tx Transaction, primaryCandidates, fallbackCandidates map[string]*services.IngestionRecord) (*services.IngestionRecord, string, string) {
	if len(tx.InputWitnesses) == 0 {
		return nil, "", ""
	}
	if match, matchType, matched := matchWitnessCandidates(tx.InputWitnesses, primaryCandidates); match != nil {
		return match, matchType, matched
	}
	return matchWitnessCandidates(tx.InputWitnesses, fallbackCandidates)
}

func matchWitnessCandidates(inputWitnesses [][][]byte, candidates map[string]*services.IngestionRecord) (*services.IngestionRecord, string, string) {
	if len(candidates) == 0 {
		return nil, "", ""
	}
	for _, stack := range inputWitnesses {
		for _, item := range stack {
			for _, candidate := range witnessHashes(item) {
				if match, ok := candidates[candidate]; ok {
					return match, "witness_hash", candidate
				}
			}
		}
	}
	return nil, "", ""
}

func witnessHashes(item []byte) []string {
	if len(item) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, 6)
	add := func(value string) {
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
	}

	if len(item) == 32 || len(item) == 20 {
		add(hex.EncodeToString(item))
	}
	sum := sha256.Sum256(item)
	add(hex.EncodeToString(sum[:]))
	add(hex.EncodeToString(btcutil.Hash160(item)))

	asText := normalizeHex(strings.TrimSpace(string(item)))
	if asText != "" && (len(asText) == 64 || len(asText) == 40) {
		if decoded, err := hex.DecodeString(asText); err == nil {
			add(asText)
			if len(decoded) == 32 || len(decoded) == 20 {
				add(hex.EncodeToString(decoded))
			}
			decodedSum := sha256.Sum256(decoded)
			add(hex.EncodeToString(decodedSum[:]))
			add(hex.EncodeToString(btcutil.Hash160(decoded)))
		}
	}

	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	return out
}

func scriptAddressHashes(script []byte, params *chaincfg.Params) []string {
	class, addrs, _, err := txscript.ExtractPkScriptAddrs(script, params)
	if err != nil {
		return nil
	}
	if class != txscript.ScriptHashTy && class != txscript.WitnessV0ScriptHashTy {
		return nil
	}
	var out []string
	for _, addr := range addrs {
		hash := hex.EncodeToString(addr.ScriptAddress())
		if hash != "" {
			out = append(out, hash)
		}
	}
	return out
}

func parseScanPayload(result map[string]any) scanPayload {
	payload := scanPayload{
		message:          strings.TrimSpace(stringFromAny(result["extracted_message"])),
		payoutAddress:    strings.TrimSpace(stringFromAny(result["payout_address"])),
		payoutScript:     normalizeHex(stringFromAny(result["payout_script"])),
		payoutScriptHash: normalizeHex(stringFromAny(result["payout_script_hash"])),
	}
	if payload.payoutAddress == "" {
		payload.payoutAddress = strings.TrimSpace(stringFromAny(result["address"]))
	}

	raw := payload.message
	if raw == "" {
		raw = strings.TrimSpace(stringFromAny(result["embedded_message"]))
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		if payload.message == "" {
			payload.message = raw
		}
		return payload
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		if payload.message == "" {
			payload.message = raw
		}
		return payload
	}

	if msg := strings.TrimSpace(stringFromAny(parsed["message"])); msg != "" {
		payload.message = msg
	}
	if payload.message == "" {
		if msg := strings.TrimSpace(stringFromAny(parsed["embedded_message"])); msg != "" {
			payload.message = msg
		}
	}
	if payload.payoutAddress == "" {
		payload.payoutAddress = strings.TrimSpace(stringFromAny(parsed["address"]))
	}
	if payload.payoutAddress == "" {
		payload.payoutAddress = strings.TrimSpace(stringFromAny(parsed["payout_address"]))
	}
	if payload.payoutScript == "" {
		payload.payoutScript = normalizeHex(stringFromAny(parsed["payout_script"]))
	}
	if payload.payoutScriptHash == "" {
		payload.payoutScriptHash = normalizeHex(stringFromAny(parsed["payout_script_hash"]))
	}

	return payload
}

func (bm *BlockMonitor) matchPayoutScript(tx Transaction, payload scanPayload) ([]byte, bool) {
	if payload.payoutScript == "" && payload.payoutScriptHash == "" && payload.payoutAddress == "" {
		return nil, false
	}

	if payload.payoutScript != "" {
		if script, err := hex.DecodeString(payload.payoutScript); err == nil {
			for _, output := range tx.Outputs {
				if bytes.Equal(output.ScriptPubKey, script) {
					return script, true
				}
			}
		}
	}

	if payload.payoutAddress != "" {
		if script := bm.scriptForAddress(payload.payoutAddress); len(script) > 0 {
			for _, output := range tx.Outputs {
				if bytes.Equal(output.ScriptPubKey, script) {
					return script, true
				}
			}
		}
	}

	if payload.payoutScriptHash != "" {
		for _, output := range tx.Outputs {
			if scriptHashHex(output.ScriptPubKey) == payload.payoutScriptHash {
				return output.ScriptPubKey, true
			}
			if scriptHash160Hex(output.ScriptPubKey) == payload.payoutScriptHash {
				return output.ScriptPubKey, true
			}
		}
	}

	return nil, false
}

func (bm *BlockMonitor) scriptForAddress(address string) []byte {
	params := bm.networkParams()
	addr, err := btcutil.DecodeAddress(address, params)
	if err != nil {
		return nil
	}
	script, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil
	}
	return script
}

func (bm *BlockMonitor) networkParams() *chaincfg.Params {
	switch bm.bitcoinClient.GetNetwork() {
	case "mainnet":
		return &chaincfg.MainNetParams
	case "signet":
		return &chaincfg.SigNetParams
	case "testnet":
		return &chaincfg.TestNet3Params
	case "testnet4":
		return &chaincfg.TestNet4Params
	default:
		return &chaincfg.TestNet4Params
	}
}

func (bm *BlockMonitor) moveIngestionImage(blockDir string, rec *services.IngestionRecord) (string, error) {
	return bm.moveIngestionImageWithFilename(blockDir, rec, "")
}

func (bm *BlockMonitor) moveIngestionImageWithFilename(blockDir string, rec *services.IngestionRecord, destFilename string) (string, error) {
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}
	filename := strings.TrimSpace(rec.Filename)
	if filename == "" {
		filename = "inscription.png"
	}
	if strings.TrimSpace(destFilename) == "" {
		destFilename = blockImageFilename(rec, "")
		if destFilename == "" {
			destFilename = filename
		}
	}

	destDir := filepath.Join(blockDir, "images")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create images dir: %w", err)
	}
	destPath := filepath.Join(destDir, destFilename)
	if _, err := os.Stat(destPath); err == nil {
		bm.cleanupUploadArtifacts(rec)
		return destPath, nil
	}

	if stegoPath, ok := bm.stegoImagePath(rec); ok {
		if err := bm.copyStegoToBlock(stegoPath, destPath); err == nil {
			bm.unpinUploadPath(stegoPath)
			bm.cleanupUploadArtifacts(rec)
			return destPath, nil
		}
	}
	if bm.writeStegoToBlock(rec, destPath) {
		bm.cleanupUploadArtifacts(rec)
		return destPath, nil
	}

	sourcePath := ""
	var candidates []string
	if filename != "" {
		candidates = append(candidates, filename)
	}
	if rec.ID != "" && filename != "" {
		candidates = append(candidates, fmt.Sprintf("%s_%s", rec.ID, filename))
	}
	if rec.ID != "" && filepath.Base(filename) != filename {
		candidates = append(candidates, fmt.Sprintf("%s_%s", rec.ID, filepath.Base(filename)))
	}
	for _, candidate := range candidates {
		path := filepath.Join(uploadsDir, candidate)
		if _, err := os.Stat(path); err == nil {
			sourcePath = path
			break
		}
	}
	if sourcePath == "" && rec.ID != "" {
		matches, _ := filepath.Glob(filepath.Join(uploadsDir, rec.ID+"_*"))
		if len(matches) > 0 {
			sort.Strings(matches)
			sourcePath = matches[0]
		}
	}
	if sourcePath == "" {
		if len(rec.ImageBase64) == 0 {
			return "", fmt.Errorf("missing ingestion image for %s", rec.ID)
		}
		data, err := base64.StdEncoding.DecodeString(rec.ImageBase64)
		if err != nil {
			return "", fmt.Errorf("decode ingestion image: %w", err)
		}
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			return "", fmt.Errorf("write ingestion image: %w", err)
		}
		bm.cleanupUploadArtifacts(rec)
		return destPath, nil
	}

	if err := os.Rename(sourcePath, destPath); err != nil {
		if err := copyFile(sourcePath, destPath); err != nil {
			return "", err
		}
		_ = os.Remove(sourcePath)
	}
	bm.unpinUploadPath(sourcePath)
	bm.cleanupUploadArtifacts(rec)
	return destPath, nil
}

func (bm *BlockMonitor) stegoImagePath(rec *services.IngestionRecord) (string, bool) {
	if bm == nil || rec == nil || rec.Metadata == nil {
		return "", false
	}
	stegoCID := stegoCIDFromRecord(rec)
	if stegoCID == "" {
		return "", false
	}
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}
	if matches, _ := filepath.Glob(filepath.Join(uploadsDir, stegoCID+"*")); len(matches) > 0 {
		sort.Strings(matches)
		return matches[0], true
	}
	return "", false
}

func stegoCIDFromRecord(rec *services.IngestionRecord) string {
	if rec == nil || rec.Metadata == nil {
		return ""
	}
	stegoCID := strings.TrimSpace(stringFromAny(rec.Metadata["stego_image_cid"]))
	if stegoCID == "" {
		stegoCID = strings.TrimSpace(stringFromAny(rec.Metadata["stego_cid"]))
	}
	return stegoCID
}

func (bm *BlockMonitor) writeStegoToBlock(rec *services.IngestionRecord, destPath string) bool {
	if bm == nil || bm.ipfsClient == nil {
		return false
	}
	stegoCID := stegoCIDFromRecord(rec)
	if stegoCID == "" {
		return false
	}
	if destPath == "" {
		return false
	}
	stegoBytes, err := bm.ipfsClient.Cat(context.Background(), stegoCID)
	if err != nil || len(stegoBytes) == 0 {
		return false
	}
	if err := os.WriteFile(destPath, stegoBytes, 0644); err != nil {
		log.Printf("failed to write stego image to block dir: %v", err)
		return false
	}
	return true
}

func (bm *BlockMonitor) copyStegoToBlock(src, dest string) error {
	if err := copyFile(src, dest); err != nil {
		return err
	}
	return nil
}

func (bm *BlockMonitor) unpinUploadPath(path string) {
	if bm == nil || strings.TrimSpace(path) == "" {
		return
	}
	if bm.unpinPath != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := bm.unpinPath(ctx, path); err != nil {
			log.Printf("ipfs unpin failed for %s: %v", path, err)
		}
	}
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}
	if rel, err := filepath.Rel(uploadsDir, path); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".." {
		_ = os.Remove(path)
	}
}

func (bm *BlockMonitor) cleanupUploadArtifacts(rec *services.IngestionRecord) {
	if bm == nil || rec == nil {
		return
	}
	id := strings.TrimSpace(rec.ID)
	if id == "" {
		return
	}
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}
	pattern := filepath.Join(uploadsDir, id+"_*")
	matches, _ := filepath.Glob(pattern)
	for _, match := range matches {
		bm.unpinUploadPath(match)
	}
}

func (bm *BlockMonitor) maybeReconcileStego(rec *services.IngestionRecord) {
	if bm.stegoReconciler == nil || rec == nil {
		return
	}
	meta := rec.Metadata
	if meta == nil {
		return
	}
	stegoCID := strings.TrimSpace(stringFromAny(meta["stego_image_cid"]))
	if stegoCID == "" {
		stegoCID = strings.TrimSpace(stringFromAny(meta["stego_cid"]))
	}
	if stegoCID == "" {
		return
	}
	if strings.TrimSpace(stringFromAny(meta["stego_reconciled_at"])) != "" {
		return
	}
	expected := strings.TrimSpace(stringFromAny(meta["stego_contract_id"]))
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := bm.stegoReconciler.ReconcileStego(ctx, stegoCID, expected); err != nil {
		log.Printf("stego reconcile failed for %s (cid=%s): %v", rec.ID, stegoCID, err)
		return
	}
	if bm.ingestion != nil {
		now := strconv.FormatInt(time.Now().Unix(), 10)
		if err := bm.ingestion.UpdateMetadata(rec.ID, map[string]interface{}{
			"stego_reconciled_at": now,
		}); err != nil {
			log.Printf("stego reconcile: failed to mark ingestion %s reconciled: %v", rec.ID, err)
		}
	}
}

func txidImageFilename(txid, fallback string) string {
	ext := filepath.Ext(fallback)
	if ext == "" {
		ext = ".png"
	}
	return fmt.Sprintf("%s%s", txid, ext)
}

func blockImageFilename(rec *services.IngestionRecord, fallbackTxid string) string {
	if rec != nil && rec.Metadata != nil {
		cid := strings.TrimSpace(stringFromAny(rec.Metadata["stego_image_cid"]))
		if cid == "" {
			cid = strings.TrimSpace(stringFromAny(rec.Metadata["ipfs_image_cid"]))
		}
		if cid == "" {
			cid = strings.TrimSpace(stringFromAny(rec.Metadata["stego_cid"]))
		}
		if cid != "" {
			return cid
		}
	}
	if strings.TrimSpace(fallbackTxid) != "" {
		return txidImageFilename(fallbackTxid, rec.Filename)
	}
	return strings.TrimSpace(rec.Filename)
}

func mergeIngestionMetadata(target map[string]any, meta map[string]interface{}) {
	if len(meta) == 0 {
		return
	}
	for key, value := range meta {
		if _, exists := target[key]; exists {
			continue
		}
		target[key] = value
	}
}

func applyStegoMetadata(target map[string]any, meta map[string]interface{}) {
	if len(target) == 0 || len(meta) == 0 {
		return
	}
	if !hasStegoMetadata(meta) {
		return
	}
	if _, exists := target["is_stego"]; !exists {
		target["is_stego"] = true
	}
	if _, exists := target["stego_probability"]; !exists {
		target["stego_probability"] = 1.0
	}
	if _, exists := target["stego_type"]; !exists {
		if method := strings.TrimSpace(stringFromAny(meta["stego_method"])); method != "" {
			target["stego_type"] = method
		} else {
			target["stego_type"] = "stego"
		}
	}
	if _, exists := target["extracted_message"]; !exists {
		if manifest := strings.TrimSpace(stringFromAny(meta["stego_manifest_yaml"])); manifest != "" {
			target["extracted_message"] = manifest
		}
	}
}

func hasStegoMetadata(meta map[string]interface{}) bool {
	if meta == nil {
		return false
	}
	keys := []string{
		"stego_image_cid",
		"stego_payload_cid",
		"stego_manifest_yaml",
		"stego_manifest_proposal_id",
		"stego_contract_id",
	}
	for _, key := range keys {
		if strings.TrimSpace(stringFromAny(meta[key])) != "" {
			return true
		}
	}
	return false
}

func (bm *BlockMonitor) markIngestionConfirmed(rec *services.IngestionRecord, txid string, height int64, imageFile, imagePath string) {
	if bm.ingestion == nil || rec == nil {
		return
	}
	updates := map[string]interface{}{
		"confirmed_txid":   txid,
		"confirmed_height": height,
		"image_file":       imageFile,
		"image_path":       imagePath,
	}
	if meta := rec.Metadata; meta != nil {
		if prevHeight, ok := meta["confirmed_height"].(float64); ok && int64(prevHeight) != height {
			updates["reorg_from_height"] = int64(prevHeight)
		} else if prevHeightStr, ok := meta["confirmed_height"].(string); ok {
			if prevHeightInt, err := strconv.ParseInt(strings.TrimSpace(prevHeightStr), 10, 64); err == nil && prevHeightInt != height {
				updates["reorg_from_height"] = prevHeightInt
			}
		}
		if prevTxid, ok := meta["confirmed_txid"].(string); ok && strings.TrimSpace(prevTxid) != "" && strings.TrimSpace(prevTxid) != txid {
			updates["reorg_from_txid"] = prevTxid
		}
	}
	if err := bm.ingestion.UpdateMetadata(rec.ID, updates); err != nil {
		log.Printf("oracle reconcile: failed to update ingestion metadata for %s: %v", rec.ID, err)
	}
	if err := bm.ingestion.UpdateStatusWithNote(rec.ID, "confirmed", fmt.Sprintf("confirmed in block %d", height)); err != nil {
		log.Printf("oracle reconcile: failed to update ingestion status for %s: %v", rec.ID, err)
	}
	if bm.sweepStore != nil {
		contractID := contractIDFromIngestion(rec)
		if contractID != "" {
			if err := bm.sweepStore.UpdateContractStatus(context.Background(), contractID, "confirmed"); err != nil {
				log.Printf("oracle reconcile: failed to update contract status for %s: %v", contractID, err)
			}
		}
	}
}

func contractIDFromIngestion(rec *services.IngestionRecord) string {
	if rec == nil {
		return ""
	}
	if meta := rec.Metadata; meta != nil {
		if contractID, ok := meta["contract_id"].(string); ok {
			if trimmed := strings.TrimSpace(contractID); trimmed != "" {
				return trimmed
			}
		}
		if visibleHash, ok := meta["visible_pixel_hash"].(string); ok {
			if trimmed := strings.TrimSpace(visibleHash); trimmed != "" {
				return trimmed
			}
		}
	}
	return strings.TrimSpace(rec.ID)
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func visiblePixelHash(imageBytes []byte, message string) string {
	if len(imageBytes) == 0 || message == "" {
		return ""
	}
	sum := sha256.Sum256(append(imageBytes, []byte(message)...))
	return fmt.Sprintf("%x", sum[:8])
}

func normalizeHex(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "0x")
	return strings.ToLower(value)
}

func scriptHashHex(script []byte) string {
	if len(script) == 0 {
		return ""
	}
	sum := sha256.Sum256(script)
	return hex.EncodeToString(sum[:])
}

func scriptHash160Hex(script []byte) string {
	if len(script) == 0 {
		return ""
	}
	return hex.EncodeToString(btcutil.Hash160(script))
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}

func intFromAny(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return int(parsed), true
		}
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func confidenceFromAny(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case json.Number:
		if parsed, err := v.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return parsed
		}
	}
	return 0.0
}

func buildContractMetadata(result map[string]any) map[string]any {
	return map[string]any{
		"tx_id":             result["tx_id"],
		"image_index":       result["image_index"],
		"stego_type":        result["stego_type"],
		"extracted_message": result["extracted_message"],
		"scan_confidence":   result["confidence"],
		"scan_timestamp":    result["scanned_at"],
		"format":            result["format"],
		"size_bytes":        result["size_bytes"],
	}
}

func updateContractEntry(contracts []SmartContractData, result map[string]any, updated SmartContractData) bool {
	txID := stringFromAny(result["tx_id"])
	imageIndex, hasIndex := intFromAny(result["image_index"])
	for i := range contracts {
		if contracts[i].Metadata == nil {
			continue
		}
		metaTx := stringFromAny(contracts[i].Metadata["tx_id"])
		if metaTx == "" || metaTx != txID {
			continue
		}
		if hasIndex {
			if metaIndex, ok := intFromAny(contracts[i].Metadata["image_index"]); ok && metaIndex != imageIndex {
				continue
			}
		}
		if contracts[i].Metadata == nil {
			contracts[i].Metadata = map[string]any{}
		}
		if updated.Metadata != nil {
			contracts[i].Metadata = updated.Metadata
		}
		contracts[i].ContractID = updated.ContractID
		contracts[i].BlockHeight = updated.BlockHeight
		contracts[i].ImagePath = updated.ImagePath
		if updated.Confidence > 0 {
			contracts[i].Confidence = updated.Confidence
		}
		return true
	}
	return false
}

func upsertContractByID(contracts []SmartContractData, updated SmartContractData) []SmartContractData {
	for i := range contracts {
		if contracts[i].ContractID == updated.ContractID {
			contracts[i].BlockHeight = updated.BlockHeight
			contracts[i].ImagePath = updated.ImagePath
			if updated.Metadata != nil {
				contracts[i].Metadata = updated.Metadata
			}
			if updated.Confidence > 0 {
				contracts[i].Confidence = updated.Confidence
			}
			return contracts
		}
	}
	return append(contracts, updated)
}

// GetBlockInscriptions retrieves inscriptions for a specific block height
func (bm *BlockMonitor) GetBlockInscriptions(height int64) (*BlockInscriptionsResponse, error) {
	// First, try to find existing block data
	blockDir, err := bm.findBlockDirectory(height)
	if err != nil {
		return &BlockInscriptionsResponse{
			BlockHeight: height,
			Success:     false,
			Error:       "Block not found",
		}, nil
	}

	// Read inscriptions.json
	inscriptionsFile := filepath.Join(blockDir, "inscriptions.json")
	data, err := os.ReadFile(inscriptionsFile)
	if err != nil {
		return &BlockInscriptionsResponse{
			BlockHeight: height,
			Success:     false,
			Error:       "Inscriptions data not found",
		}, nil
	}

	var response BlockInscriptionsResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return &BlockInscriptionsResponse{
			BlockHeight: height,
			Success:     false,
			Error:       "Failed to parse inscriptions data",
		}, nil
	}

	return &response, nil
}

// findBlockDirectory finds the directory for a given block height
func (bm *BlockMonitor) findBlockDirectory(height int64) (string, error) {
	entries, err := os.ReadDir(bm.blocksDir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Extract height from directory name (format: height_hash)
			parts := strings.Split(entry.Name(), "_")
			if len(parts) >= 1 {
				if dirHeight, err := strconv.ParseInt(parts[0], 10, 64); err == nil && dirHeight == height {
					return filepath.Join(bm.blocksDir, entry.Name()), nil
				}
			}
		}
	}

	return "", fmt.Errorf("block directory not found for height %d", height)
}
