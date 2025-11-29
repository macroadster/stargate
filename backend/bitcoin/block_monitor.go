package bitcoin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"stargate-backend/core"
)

// BlockMonitor handles comprehensive Bitcoin block monitoring and data extraction
type BlockMonitor struct {
	bitcoinClient *BitcoinNodeClient
	rawClient     *RawBlockClient
	currentHeight int64
	lastChecked   time.Time
	isRunning     bool
	stopChan      chan bool
	mu            sync.RWMutex
	dataStorage   DataStorageInterface

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
		rawClient:     NewRawBlockClient(),
		checkInterval: 5 * time.Minute, // Check every 5 minutes
		blocksDir:     "blocks",
		maxRetries:    3,
		retryDelay:    10 * time.Second,
		lastChecked:   time.Now(),
	}
}

// NewBlockMonitorWithStorage creates a new block monitor with data storage
func NewBlockMonitorWithStorage(client *BitcoinNodeClient, dataStorage DataStorageInterface) *BlockMonitor {
	return &BlockMonitor{
		bitcoinClient: client,
		rawClient:     NewRawBlockClient(),
		dataStorage:   dataStorage,
		checkInterval: 5 * time.Minute, // Check every 5 minutes
		blocksDir:     "blocks",
		maxRetries:    3,
		retryDelay:    10 * time.Second,
		lastChecked:   time.Now(),
	}
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

	log.Printf("Starting block monitor with %s interval", bm.checkInterval)

	go bm.monitorLoop()

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

// updateRecentBlocksSummary creates a recent blocks summary file for frontend
func (bm *BlockMonitor) updateRecentBlocksSummary() error {
	// Get recent block directories
	blocksDir := "blocks"
	files, err := os.ReadDir(blocksDir)
	if err != nil {
		return fmt.Errorf("failed to read blocks directory: %w", err)
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
		if currentHeight-startHeight > maxBlocksPerCycle {
			startHeight = currentHeight - maxBlocksPerCycle + 1
		}

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

// getCurrentHeightFromBlockchainInfo gets current height from blockchain.info
func (bm *BlockMonitor) getCurrentHeightFromBlockchainInfo() (int64, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.Get("https://blockchain.info/q/getblockcount")
	if err != nil {
		return 0, fmt.Errorf("failed to get block count: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("blockchain.info returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	height, err := strconv.ParseInt(strings.TrimSpace(string(body)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse height: %w", err)
	}

	return height, nil
}

// ProcessBlock downloads and processes a single block using raw block parser (exported for external use)
func (bm *BlockMonitor) ProcessBlock(height int64) error {
	startTime := time.Now()

	log.Printf("Processing block %d", height)

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

	// Scan images for steganography using the centralized API
	scanResults, err := bm.scanBlockViaAPI(height)
	if err != nil {
		log.Printf("Failed to scan block via API: %v", err)
		// Fallback: create empty scan results
		scanResults = []map[string]any{}
	} else {
		stegoCount := bm.countStegoImagesFromAPIResponse(scanResults)
		log.Printf("Steganography scan completed via API: %d images scanned, %d with stego detected",
			len(scanResults), stegoCount)
	}

	// Create inscriptions data
	inscriptions := bm.createInscriptionsFromImages(parsedBlock.Images)

	// Save block summary JSON for frontend API with scan results
	if err := bm.saveBlockSummaryWithScanResults(blockDir, parsedBlock, inscriptions, scanResults, height); err != nil {
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
		SmartContracts:    bm.createSmartContractsFromScanResults(scanResults),
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
			SourceFile:     fmt.Sprintf("block_%d.hex", parsedBlock.Header.Hash),
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
		imageFile := filepath.Join(imagesDir, image.FileName)
		// Save the actual image data
		if err := os.WriteFile(imageFile, image.Data, 0644); err != nil {
			log.Printf("Failed to save image %s: %v", image.FileName, err)
		} else {
			log.Printf("Successfully saved image %s (%d bytes)", image.FileName, len(image.Data))
		}
	}

	return nil
}

// createInscriptionsFromImages creates inscription data from extracted images
func (bm *BlockMonitor) createInscriptionsFromImages(images []ExtractedImageData) []InscriptionData {
	var inscriptions []InscriptionData

	for i, image := range images {
		inscription := InscriptionData{
			TxID:        image.TxID,
			InputIndex:  i,
			ContentType: fmt.Sprintf("image/%s", image.Format),
			Content:     fmt.Sprintf("Extracted from transaction %s", image.TxID),
			SizeBytes:   image.SizeBytes,
			FileName:    image.FileName,
			FilePath:    image.FilePath,
		}
		inscriptions = append(inscriptions, inscription)
	}

	return inscriptions
}

// saveBlockSummary saves a comprehensive block summary for frontend API
func (bm *BlockMonitor) saveBlockSummary(blockDir string, parsedBlock *ParsedBlock, inscriptions []InscriptionData, blockHeight int64) error {
	summary := BlockInscriptionsResponse{
		BlockHeight:       blockHeight,
		BlockHash:         parsedBlock.Header.Hash,
		Timestamp:         int64(parsedBlock.Header.Timestamp),
		TotalTransactions: len(parsedBlock.Transactions),
		Inscriptions:      inscriptions,
		Images:            parsedBlock.Images,
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

// convertTransactions converts parsed transactions to transaction data format
func (bm *BlockMonitor) convertTransactions(transactions []Transaction) []TransactionData {
	var txData []TransactionData

	for _, tx := range transactions {
		data := TransactionData{
			TxID:       tx.TxID,
			Height:     0, // Will be set by caller
			Time:       int64(tx.Locktime),
			Status:     "confirmed",
			HasImages:  len(tx.Witness) > 0,
			ImageCount: len(tx.Witness),
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

	// Make HTTP request to local API
	client := &http.Client{Timeout: 300 * time.Second} // 5 minute timeout for large blocks
	resp, err := client.Post("http://localhost:3001/bitcoin/v1/scan/block", "application/json", bytes.NewBuffer(requestBody))
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
	for _, txResult := range scanResponse.Results {
		if txResult.StegoDetected {
			result := map[string]any{
				"tx_id":             txResult.TransactionID,
				"image_index":       0, // We don't have individual image info from API
				"file_name":         fmt.Sprintf("tx_%s", txResult.TransactionID[:16]),
				"size_bytes":        0,
				"format":            "unknown",
				"scanned_at":        time.Now().Unix(),
				"is_stego":          txResult.StegoDetected,
				"confidence":        0.0,
				"stego_type":        "detected_via_api",
				"extracted_message": txResult.ExtractedMessage,
				"scan_error":        "",
				"stego_details":     txResult.StegoDetails,
			}
			results = append(results, result)
		}
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

// saveBlockSummaryWithScanResults saves block summary including steganography scan results
func (bm *BlockMonitor) saveBlockSummaryWithScanResults(blockDir string, parsedBlock *ParsedBlock, inscriptions []InscriptionData, scanResults []map[string]any, blockHeight int64) error {
	// Count stego detections
	stegoCount := bm.countStegoImages(scanResults)

	// Create optimized smart contracts from scan results (only for stego images)
	smartContracts := []SmartContractData{}
	if stegoCount > 0 {
		smartContracts = bm.createSmartContractsFromScanResults(scanResults)
	}

	// Create optimized summary with minimal data
	summary := BlockInscriptionsResponse{
		BlockHeight:       blockHeight,
		BlockHash:         parsedBlock.Header.Hash,
		Timestamp:         int64(parsedBlock.Header.Timestamp),
		TotalTransactions: len(parsedBlock.Transactions),
		Inscriptions:      inscriptions,
		Images:            parsedBlock.Images,
		SmartContracts:    smartContracts,
		ProcessingTime:    time.Now().Unix(),
		Success:           true,
	}

	// Create compact steganography summary (only once)
	steganographySummary := map[string]any{
		"total_images":   len(parsedBlock.Images),
		"stego_detected": stegoCount > 0,
		"stego_count":    stegoCount,
		"scan_timestamp": time.Now().Unix(),
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
			"images":             summary.Images,
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
			"images":             summary.Images,
			"smart_contracts":    []SmartContractData{},
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
