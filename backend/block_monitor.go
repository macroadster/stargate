package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// BlockMonitor handles comprehensive Bitcoin block monitoring and data extraction
type BlockMonitor struct {
	bitcoinClient *BitcoinNodeClient
	scanner       StarlightScannerInterface
	currentHeight int64
	lastChecked   time.Time
	isRunning     bool
	stopChan      chan bool
	mu            sync.RWMutex

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

// ExtractedImageData represents an image extracted from witness data
type ExtractedImageData struct {
	TxID      string `json:"tx_id"`
	Format    string `json:"format"`
	SizeBytes int    `json:"size_bytes"`
	FileName  string `json:"file_name"`
	FilePath  string `json:"file_path"`
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
	ContractID  string                 `json:"contract_id"`
	BlockHeight int64                  `json:"block_height"`
	ImagePath   string                 `json:"image_path"`
	Confidence  float64                `json:"confidence"`
	Metadata    map[string]interface{} `json:"metadata"`
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

// NewBlockMonitor creates a new block monitor
func NewBlockMonitor(client *BitcoinNodeClient, scanner StarlightScannerInterface) *BlockMonitor {
	return &BlockMonitor{
		bitcoinClient: client,
		scanner:       scanner,
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
func (bm *BlockMonitor) GetStatistics() map[string]interface{} {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	return map[string]interface{}{
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

// checkForNewBlocks checks for and processes new blocks
func (bm *BlockMonitor) checkForNewBlocks() error {
	// Get current blockchain height
	currentHeight, err := bm.bitcoinClient.GetCurrentHeight()
	if err != nil {
		return fmt.Errorf("failed to get current height: %w", err)
	}

	// Check if we have new blocks
	if currentHeight <= bm.currentHeight {
		return nil // No new blocks
	}

	// Process new blocks
	for height := bm.currentHeight + 1; height <= currentHeight; height++ {
		if err := bm.processBlock(height); err != nil {
			log.Printf("Error processing block %d: %v", height, err)
			continue
		}
		bm.currentHeight = height
		bm.blocksProcessed++
	}

	bm.lastChecked = time.Now()
	return nil
}

// processBlock downloads and processes a single block using raw block parser
func (bm *BlockMonitor) processBlock(height int64) error {
	startTime := time.Now()

	// Get block data as interface
	blockDataInterface, err := bm.bitcoinClient.GetBlockData(height)
	if err != nil {
		return fmt.Errorf("failed to get block data: %w", err)
	}

	// Create block directory
	blockDir := filepath.Join(bm.blocksDir, fmt.Sprintf("%d_%d", height, time.Now().Unix()))
	if err := os.MkdirAll(blockDir, 0755); err != nil {
		return fmt.Errorf("failed to create block directory: %w", err)
	}

	// Save raw block data
	if err := bm.saveBlockData(blockDir, blockDataInterface); err != nil {
		return fmt.Errorf("failed to save block data: %w", err)
	}

	// For now, use raw block parser for image extraction
	// TODO: Integrate with blockDataInterface properly
	log.Printf("Block %d processed - raw parser integration needed", height)

	bm.lastProcessTime = time.Since(startTime)
	log.Printf("Successfully processed block %d in %v", height, bm.lastProcessTime)

	return nil
}

// saveBlockData saves raw block data to files
func (bm *BlockMonitor) saveBlockData(blockDir string, blockData interface{}) error {
	// Save block header
	headerFile := filepath.Join(blockDir, "block_header.json")
	headerData, err := json.MarshalIndent(blockData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal header: %w", err)
	}

	if err := os.WriteFile(headerFile, headerData, 0644); err != nil {
		return fmt.Errorf("failed to write header file: %w", err)
	}

	// Save transactions
	// transactionsFile := filepath.Join(blockDir, "transactions.json")
	// Implementation would depend on actual structure of blockData
	// This is a placeholder for structure

	return nil
}

// extractImages extracts images from transaction witness data
func (bm *BlockMonitor) extractImages(transactions interface{}) ([]ExtractedImageData, error) {
	// This would integrate with the fixed raw_block_parser logic
	// For now, return empty slice
	return []ExtractedImageData{}, nil
}

// saveImages saves extracted images to files
func (bm *BlockMonitor) saveImages(blockDir string, images []ExtractedImageData) error {
	imagesDir := filepath.Join(blockDir, "images")
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return fmt.Errorf("failed to create images directory: %w", err)
	}

	for _, image := range images {
		imageFile := filepath.Join(imagesDir, image.FileName)
		if err := os.WriteFile(imageFile, []byte{}, 0644); err != nil {
			log.Printf("Failed to save image %s: %v", image.FileName, err)
		}
	}

	return nil
}

// scanForSteganography scans images for hidden content
func (bm *BlockMonitor) scanForSteganography(images []ExtractedImageData) ([]SmartContractData, error) {
	var contracts []SmartContractData

	for _, image := range images {
		// Read image file data
		fileData, err := os.ReadFile(image.FilePath)
		if err != nil {
			log.Printf("Error reading image file %s: %v", image.FilePath, err)
			continue
		}

		// Use scanner to analyze image
		options := ScanOptions{
			ExtractMessage:      true,
			ConfidenceThreshold: 0.5,
			IncludeMetadata:     true,
		}
		result, err := bm.scanner.ScanImage(fileData, options)
		if err != nil {
			log.Printf("Error scanning image %s: %v", image.FileName, err)
			continue
		}

		if result.IsStego {
			log.Printf("Steganography detected in image %s with confidence %.2f", image.FileName, result.Confidence)

			contract := SmartContractData{
				ContractID:  generateContractID(),
				BlockHeight: 0, // Would be set from context
				ImagePath:   image.FilePath,
				Confidence:  result.Confidence,
				Metadata: map[string]interface{}{
					"extracted_message": result.ExtractedMessage,
					"stego_type":        result.StegoType,
				},
			}
			contracts = append(contracts, contract)
		}
	}

	return contracts, nil
}

// saveSmartContracts saves smart contracts to file
func (bm *BlockMonitor) saveSmartContracts(blockDir string, contracts []SmartContractData) error {
	contractsFile := filepath.Join(blockDir, "smart_contracts.json")
	contractsData, err := json.MarshalIndent(contracts, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal contracts: %w", err)
	}

	return os.WriteFile(contractsFile, contractsData, 0644)
}

// updateStatistics updates monitoring statistics
func (bm *BlockMonitor) updateStatistics(blockData interface{}, images []ExtractedImageData, contracts []SmartContractData) {
	bm.totalTransactions += 10 // Placeholder
	bm.totalImages += int64(len(images))
	bm.totalStegoContracts += int64(len(contracts))
}

// saveBlockSummary saves a comprehensive block summary
func (bm *BlockMonitor) saveBlockSummary(blockDir string, blockData interface{}, images []ExtractedImageData, contracts []SmartContractData) error {
	summary := map[string]interface{}{
		"block_height":      bm.currentHeight,
		"transaction_count": bm.totalTransactions,
		"image_count":       len(images),
		"contract_count":    len(contracts),
		"processing_time":   bm.lastProcessTime.Milliseconds(),
		"timestamp":         time.Now().Unix(),
	}

	summaryFile := filepath.Join(blockDir, "metadata.json")
	summaryData, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal summary: %w", err)
	}

	return os.WriteFile(summaryFile, summaryData, 0644)
}

// Helper functions
func generateContractID() string {
	return fmt.Sprintf("contract_%d", time.Now().UnixNano())
}
