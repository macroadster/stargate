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

	log.Printf("block monitor: monitorLoop started (check_interval=%v)", bm.checkInterval)

	for {
		select {
		case <-ticker.C:
			log.Printf("block monitor: ticker fired (interval=%v), running checkForNewBlocks", bm.checkInterval)
			start := time.Now()
			if err := bm.checkForNewBlocks(); err != nil {
				log.Printf("Error checking for new blocks: %v", err)
			}
			log.Printf("block monitor: checkForNewBlocks completed in %v", time.Since(start))
		case <-bm.stopChan:
			log.Println("Block monitor stopped")
			return
		}
	}
}

func (bm *BlockMonitor) reconcileSweepLoop() {
	window := monitorRecentReconcileWindow()
	interval := monitorReconcileInterval()

	// Legacy periodic healer path (only started by Start() when not in tip-only mode).
	log.Printf("block monitor: Starting periodic recent-block reconciler every %s (window=%d)", interval, window)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial small delay so startup forward scan + first reconcileCanonicalTip settle first
	time.Sleep(15 * time.Second)

	for {
		select {
		case <-ticker.C:
			log.Printf("block monitor: reconcileSweep tick (window=%d)", window)
			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			if err := bm.ReconcileRecentBlocks(ctx, window); err != nil {
				log.Printf("periodic reconcile: %v", err)
			}
			cancel()
			log.Printf("block monitor: ReconcileRecentBlocks (sweep) took %v", time.Since(start))
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

// checkForNewBlocks checks for and processes new blocks.
// In the default "track tip only" mode this is extremely lightweight:
// we only ever process the live tip when it advances. No repeated
// reprocessing of the last N blocks, no background catch-up crawls.
func (bm *BlockMonitor) checkForNewBlocks() error {
	// Get current blockchain height from the configured source
	tip, err := bm.getCurrentHeightFromBlockchainInfo()
	if err != nil {
		return fmt.Errorf("failed to get current height: %w", err)
	}

	log.Printf("block monitor: checkForNewBlocks tip=%d current=%d", tip, bm.currentHeight)

	// Light reorg safety (only re-processes on actual hash mismatch).
	// Depth tracks settlement confirmations so a shallow fake fork cannot
	// stay as the stored canonical block.
	if err := bm.reconcileCanonicalTip(tip, reorgWatchDepth()); err != nil {
		log.Printf("Failed to reconcile canonical tip: %v", err)
	}

	if monitorTrackTipOnly() {
		return bm.trackTip(tip)
	}

	// --- legacy / full monitoring path (opt-in via BLOCK_MONITOR_TRACK_TIP_ONLY=false) ---
	lastGood := bm.currentHeight
	if stored := bm.getMaxStoredHeight(); stored > lastGood {
		lastGood = stored
	}
	if lastGood > bm.currentHeight {
		bm.currentHeight = lastGood
	}

	startHeight := lastGood + 1
	maxPerCycle := monitorMaxBlocksPerCycle()
	delayBetween := 6 * time.Second

	healWindow := monitorRecentReconcileWindow()
	if healWindow > 0 {
		log.Printf("block monitor: running heal/reconcile window of %d recent blocks", healWindow)
		_ = bm.ReconcileRecentBlocks(context.Background(), healWindow)
	}

	if bm.currentHeight == 0 && tip > 100 {
		bm.currentHeight = tip - 1
		startHeight = bm.currentHeight + 1
	}

	log.Printf("Processing new blocks from %d to %d (max %d per cycle)", startHeight, tip, maxPerCycle)

	processedThisCycle := int64(0)
	for height := startHeight; height <= tip && height < startHeight+maxPerCycle; height++ {
		if err := bm.ProcessBlock(height); err != nil {
			log.Printf("Error processing block %d (will retry next cycle): %v", height, err)
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
	bm.promoteProvisionalProofs(tip)
	if err := bm.updateRecentBlocksSummary(); err != nil {
		log.Printf("Failed to update recent blocks summary: %v", err)
	}
	return nil
}

// trackTip implements lightweight tip tracking with sequential gap backfill.
//
// Unlike the old "jump to tip-N" behavior, we never skip intermediate heights
// once we have a known currentHeight. When behind, we process up to
// monitorCatchupBatch() blocks per cycle until we catch the local tip.
// Cold start (currentHeight==0, empty storage) still seeds near the tip so we
// do not scan the entire chain; historical gaps remain available via on-demand scan.
func (bm *BlockMonitor) trackTip(tip int64) error {
	maxPerCycle := monitorMaxBlocksPerCycle()
	catchupBatch := monitorCatchupBatch()
	// Cold-start window: process a small trailing set near tip on first run.
	coldWindow := maxPerCycle
	if coldWindow < 3 {
		coldWindow = 3
	}

	first, last, seededCurrent, seeded := tipCatchupWindow(
		bm.currentHeight, tip, maxPerCycle, catchupBatch, coldWindow,
	)
	if seeded {
		bm.currentHeight = seededCurrent
		log.Printf("block monitor: tip-only initialized; will track from near tip=%d (seeded current=%d)", tip, bm.currentHeight)
	}

	if first == 0 || last == 0 || first > last {
		bm.lastChecked = time.Now()
		return nil
	}

	behind := tip - bm.currentHeight
	if behind > maxPerCycle {
		log.Printf("block monitor: tip advanced to %d (sequential catch-up %d..%d, %d behind, batch=%d)",
			tip, first, last, behind, last-first+1)
	} else {
		log.Printf("block monitor: tip advanced to %d (processing %d..%d)", tip, first, last)
	}

	processed := int64(0)
	for h := first; h <= last; h++ {
		if err := bm.ProcessBlock(h); err != nil {
			log.Printf("Error processing block %d: %v (will retry on next tick)", h, err)
			// Stop advancing past the failure — preserves gap-free progress.
			break
		}
		bm.currentHeight = h
		bm.blocksProcessed++
		processed++
	}

	if tip > bm.currentHeight && processed > 0 {
		log.Printf("block monitor: catch-up progress current=%d tip=%d remaining=%d",
			bm.currentHeight, tip, tip-bm.currentHeight)
	}

	bm.lastChecked = time.Now()
	bm.promoteProvisionalProofs(tip)
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
	if bm.chain != nil {
		return bm.chain.GetTipHeight(context.Background())
	}
	if bm.bitcoinClient == nil {
		return 0, fmt.Errorf("no chain backend or bitcoin client configured")
	}
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

	canonicalHash, _ := bm.getCanonicalBlockHash(height)
	prevHash := ""
	if height > 0 {
		prevHash, _ = bm.getCanonicalBlockHash(height - 1)
	}
	if err := validateIngestedBlock(height, parsedBlock, canonicalHash, prevHash); err != nil {
		return fmt.Errorf("rejecting block %d (failed integrity check): %w", height, err)
	}

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
// This is intentionally not called by the background monitor loop in
// tip-only mode (the default). It is still used for:
//   - explicit on-demand scans
//   - IPFS ingestion sync (after a new upload is seen)
//   - manual / debugging use
func (bm *BlockMonitor) ReconcileRecentBlocks(ctx context.Context, count int) error {
	if count <= 0 {
		return nil
	}
	bm.reconcileMu.Lock()
	defer bm.reconcileMu.Unlock()

	start := time.Now()
	height, err := bm.getCurrentHeightFromBlockchainInfo()
	if err != nil {
		return fmt.Errorf("get current height: %w", err)
	}
	processed := 0
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
		} else {
			processed++
		}
	}
	dur := time.Since(start)
	if dur > 10*time.Second || processed > 0 {
		log.Printf("block monitor: ReconcileRecentBlocks(%d) reprocessed %d blocks in %v (tip=%d)", count, processed, dur, height)
	}
	return nil
}

// fetchTxStatus fetches a transaction and returns a JSON-compatible map,
// block height, and whether the tx is confirmed.
func (bm *BlockMonitor) fetchTxStatus(txid string) (map[string]any, int64, bool, error) {
	if bm.chain != nil {
		height, confirmed, err := bm.chain.GetTxStatus(context.Background(), txid)
		if err != nil {
			return nil, 0, false, err
		}
		txData := map[string]any{
			"txid": txid,
			"status": map[string]any{
				"confirmed":    confirmed,
				"block_height": height,
			},
		}
		if confirmed {
			// Attach outputs for downstream funding-proof helpers.
			if msg, err := bm.chain.GetRawTx(context.Background(), txid); err == nil && msg != nil {
				var vouts []any
				for _, out := range msg.TxOut {
					vouts = append(vouts, map[string]any{
						"scriptpubkey": hex.EncodeToString(out.PkScript),
						"value":        float64(out.Value),
					})
				}
				txData["vout"] = vouts
			}
		}
		return txData, height, confirmed, nil
	}
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
