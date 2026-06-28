package bitcoin

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
	// OP_RETURN donation flow: the funding transaction IS the final transaction.
	// No sweep or recommitment is needed, so the periodic reconcile loop is disabled.
	log.Printf("reconcile sweep loop disabled — OP_RETURN donation flow supersedes hashlock sweep")
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

	// Scan each image individually using the native Go AlphaScanner via ScanImage.
	// This replaces the former ScanBlock call which required the Python proxy.
	var scanResults []map[string]any
	if len(parsedBlock.Images) > 0 {
		log.Printf("Scanning %d images from block %d using per-image scanner", len(parsedBlock.Images), height)
		var err error
		scanResults, err = bm.scanImagesDirectly(parsedBlock.Images)
		if err != nil {
			log.Printf("Failed to scan images for block %d: %v", height, err)
			scanResults = bm.createEmptyScanResults(len(parsedBlock.Images))
		}
	} else {
		scanResults = bm.createEmptyScanResults(0)
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

	log.Printf("Successfully processed block %d in %v: %d txs, %d images, %d inscriptions, %d stego detected, %d smart contracts",
		height, processingTime, len(parsedBlock.Transactions), len(parsedBlock.Images), len(inscriptions), bm.countStegoImages(scanResults), len(smartContracts))

	for _, fn := range bm.onBlockProcessed {
		fn(height)
	}

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

// fetchTxStatus fetches a transaction from the blockchain API and returns the
// raw JSON map, block height, and whether the tx is confirmed.
func (bm *BlockMonitor) fetchTxStatus(txid string) (map[string]any, int64, bool, error) {
	if bm.bitcoinClient == nil {
		return nil, 0, false, fmt.Errorf("bitcoin client not configured")
	}
	url := fmt.Sprintf("%s/tx/%s", strings.TrimSpace(bm.bitcoinClient.baseURL), txid)
	resp, err := bm.bitcoinClient.httpClient.Get(url)
	if err != nil {
		return nil, 0, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, 0, false, nil // tx not found — unconfirmed or invalid
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, false, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, false, err
	}
	var txData map[string]any
	if err := json.Unmarshal(body, &txData); err != nil {
		return nil, 0, false, err
	}
	statusMap, _ := txData["status"].(map[string]any)
	if statusMap == nil {
		return txData, 0, false, nil
	}
	confirmed, _ := statusMap["confirmed"].(bool)
	if !confirmed {
		return txData, 0, false, nil
	}
	height, _ := statusMap["block_height"].(float64)
	return txData, int64(height), true, nil
}

// parseTxOutputsFromJSON builds a minimal Transaction from the Esplora JSON,
// containing only TxID and Outputs (ScriptPubKey + Value).  This is sufficient
// for updateTaskFundingProofsFromTx and confirmContractTasks.
func (bm *BlockMonitor) parseTxOutputsFromJSON(txid string, txJSON map[string]any) Transaction {
	tx := Transaction{TxID: txid}
	vouts, _ := txJSON["vout"].([]any)
	for _, v := range vouts {
		vout, _ := v.(map[string]any)
		if vout == nil {
			continue
		}
		scriptHex, _ := vout["scriptpubkey"].(string)
		scriptBytes, _ := hex.DecodeString(scriptHex)
		value, _ := vout["value"].(float64)
		tx.Outputs = append(tx.Outputs, TxOutput{
			ScriptPubKey: scriptBytes,
			Value:        int64(value),
		})
	}
	return tx
}
