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
	"sort"
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
	// Lightweight periodic healer. Re-processing the most recent N blocks is
	// cheap (idempotent upserts + rate limiter will naturally throttle) and
	// heals gaps caused by restarts, partial failures, or brief API blips.
	// It also helps with short reorgs in addition to reconcileCanonicalTip.
	interval := monitorReconcileInterval()
	window := monitorRecentReconcileWindow()
	log.Printf("Starting periodic recent-block reconciler every %s (window=%d)", interval, window)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial small delay so startup forward scan + first reconcileCanonicalTip settle first
	time.Sleep(15 * time.Second)

	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			if err := bm.ReconcileRecentBlocks(ctx, window); err != nil {
				log.Printf("periodic reconcile: %v", err)
			}
			cancel()
		case <-bm.stopChan:
			log.Println("reconcile sweep loop stopped")
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

	// Ensure the directory exists
	if err := os.MkdirAll(blocksDir, 0755); err != nil {
		return fmt.Errorf("failed to ensure blocks directory: %w", err)
	}

	var recentBlocks []map[string]interface{}

	// Prefer the storage (SQLite / Postgres / in-memory cache) which is authoritative
	// and avoids expensive full FS directory walks. This is a major IO/CPU win.
	if bm.dataStorage != nil {
		if items, err := bm.dataStorage.GetRecentBlocks(12); err == nil {
			for _, item := range items {
				switch v := item.(type) {
				case map[string]interface{}:
					recentBlocks = append(recentBlocks, v)
				case *BlockDataCache:
					// convert struct to map for the legacy recent-blocks.json shape
					b, _ := json.Marshal(v)
					var m map[string]interface{}
					if json.Unmarshal(b, &m) == nil {
						recentBlocks = append(recentBlocks, m)
					}
				default:
					// best effort
					if b, err := json.Marshal(item); err == nil {
						var m map[string]interface{}
						if json.Unmarshal(b, &m) == nil {
							recentBlocks = append(recentBlocks, m)
						}
					}
				}
			}
		}
	}

	// Fallback only if storage gave nothing: do not do expensive scans.
	// The monitor will populate storage on next successful ProcessBlock.
	if len(recentBlocks) == 0 {
		// nothing — we avoid linear/expensive fallback here to honor IO reduction goals
	}

	// Sort by height descending (use efficient sort, not bubble)
	sort.Slice(recentBlocks, func(i, j int) bool {
		hi, _ := recentBlocks[i]["block_height"].(float64)
		hj, _ := recentBlocks[j]["block_height"].(float64)
		return hi > hj
	})

	if len(recentBlocks) > 10 {
		recentBlocks = recentBlocks[:10]
	}

	summary := map[string]interface{}{
		"blocks":       recentBlocks,
		"total":        len(recentBlocks),
		"last_updated": time.Now().Unix(),
	}

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
	// Get current blockchain height from the configured source
	tip, err := bm.getCurrentHeightFromBlockchainInfo()
	if err != nil {
		return fmt.Errorf("failed to get current height: %w", err)
	}

	log.Printf("Current blockchain height: %d, monitor height: %d", tip, bm.currentHeight)
	if err := bm.reconcileCanonicalTip(tip, 6); err != nil {
		log.Printf("Failed to reconcile canonical tip: %v", err)
	}

	// === Reliability improvement ===
	// Use the *maximum* of:
	//   - our in-memory high-water mark (what this instance has sequentially advanced)
	//   - the max height persisted in storage (from prior runs, on-demand scans, etc.)
	// This prevents "only last 3 on restart" and allows jumping forward when
	// storage was populated by other means.
	lastGood := bm.currentHeight
	if stored := bm.getMaxStoredHeight(); stored > lastGood {
		lastGood = stored
	}
	if lastGood > bm.currentHeight {
		bm.currentHeight = lastGood
	}

	startHeight := lastGood + 1
	maxPerCycle := monitorMaxBlocksPerCycle()
	delayBetween := 6 * time.Second // slightly higher base delay to stay kind to explorers

	// Always do a bounded recent heal pass first. This is the main defense
	// against gaps after restarts: the most recent N blocks around the live
	// tip get (re)processed regardless of our high-water mark.
	healWindow := monitorRecentReconcileWindow()
	if healWindow > 0 {
		_ = bm.ReconcileRecentBlocks(context.Background(), healWindow)
	}

	// If we were at 0 (fresh or long downtime) and tip is high, treat the
	// recent window as "caught" for the purpose of the sequential pointer.
	// We do *not* want to try crawling thousands of blocks from 1 on the next
	// cycle (that would be slow and API-unfriendly). Future blocks will be
	// picked up incrementally. Deep historical gaps can be filled on-demand.
	if bm.currentHeight == 0 && tip > 100 {
		bm.currentHeight = tip - 1
		startHeight = bm.currentHeight + 1
	}

	// Forward incremental scan with safe error handling:
	// - On any ProcessBlock failure we *break* (do not advance the pointer).
	//   Next cycle (plus the healer) will retry. Eliminates silent skips.
	log.Printf("Processing new blocks from %d to %d (max %d per cycle)", startHeight, tip, maxPerCycle)

	processedThisCycle := int64(0)
	for height := startHeight; height <= tip && height < startHeight+maxPerCycle; height++ {
		if err := bm.ProcessBlock(height); err != nil {
			log.Printf("Error processing block %d (will retry next cycle): %v", height, err)
			// Do NOT advance currentHeight past the failure.
			break
		}
		bm.currentHeight = height
		bm.blocksProcessed++
		processedThisCycle++

		if height < tip && height < startHeight+maxPerCycle-1 {
			log.Printf("Waiting %v before processing next block...", delayBetween)
			time.Sleep(delayBetween)
		}
	}

	remaining := tip - (lastGood + processedThisCycle)
	if remaining > 0 {
		log.Printf("Processed %d blocks this cycle, ~%d more remaining (will continue next cycle or via healer)", processedThisCycle, remaining)
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

	// Use partitioned 3-level directory layout (000/144/144001_xxxx) to keep
	// directory scans (reorg, recent summary, Find) from being O(total blocks).
	blockDir := BlockDir(bm.blocksDir, height, parsedBlock.Hash[:8])
	if err := os.MkdirAll(blockDir, 0755); err != nil {
		return fmt.Errorf("failed to create block directory: %w", err)
	}

	// Heavy data (raw hex + full tx list in block.json) is only written for blocks
	// that actually contain images. Boring blocks get only the lightweight
	// inscriptions.json metadata (plus DB row). This is goal #4.
	if len(parsedBlock.Images) > 0 {
		if err := bm.saveBlockData(blockDir, parsedBlock, hexData); err != nil {
			return fmt.Errorf("failed to save block data: %w", err)
		}
	}

	// Extract and save images (only when present)
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
