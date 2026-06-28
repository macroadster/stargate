package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"stargate-backend/bitcoin"
	"stargate-backend/security"
	"stargate-backend/storage"
)

// resolveBlocksDir returns the directory that holds block JSON artifacts.
func (api *DataAPI) resolveBlocksDir() string {
	if dir := os.Getenv("BLOCKS_DIR"); dir != "" {
		return dir
	}
	return "blocks"
}

// loadBlockFromDisk reads a single block JSON file into a cache struct.
func (api *DataAPI) loadBlockFromDisk(height int64) (*storage.BlockDataCache, error) {
	baseDir := strings.TrimRight(api.resolveBlocksDir(), "/")
	filePath := fmt.Sprintf("%s/block_%d.json", baseDir, height)

	data, err := os.ReadFile(filePath)
	if err != nil {
		// Try directory-based layout: <height>_00000000/inscriptions.json
		dirPath := fmt.Sprintf("%s/%d_00000000/inscriptions.json", baseDir, height)
		if data2, err2 := os.ReadFile(dirPath); err2 == nil {
			data = data2
		} else {
			return nil, fmt.Errorf("read block file: %w", err)
		}
	}

	var raw struct {
		BlockHeight  int64                        `json:"block_height"`
		BlockHash    string                       `json:"block_hash"`
		Timestamp    int64                        `json:"timestamp"`
		TxCount      int                          `json:"tx_count"`
		Inscriptions []bitcoin.InscriptionData    `json:"inscriptions"`
		Images       []bitcoin.ExtractedImageData `json:"images"`
		Smart        []bitcoin.SmartContractData  `json:"smart_contracts"`
		ScanResults  []map[string]interface{}     `json:"scan_results"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode block file: %w", err)
	}

	cache := &storage.BlockDataCache{
		BlockHeight:    raw.BlockHeight,
		BlockHash:      raw.BlockHash,
		Timestamp:      raw.Timestamp,
		TxCount:        raw.TxCount,
		Inscriptions:   raw.Inscriptions,
		Images:         raw.Images,
		SmartContracts: raw.Smart,
		ScanResults:    raw.ScanResults,
		ProcessingTime: 0,
		Success:        true,
		CacheTimestamp: time.Now(),
		SteganographySummary: &bitcoin.SteganographySummary{
			TotalImages:   len(raw.Images),
			StegoDetected: false,
			StegoCount:    0,
			ScanTimestamp: time.Now().Unix(),
			AvgConfidence: 0,
			StegoTypes:    []string{},
		},
	}

	if cache.SmartContracts == nil {
		cache.SmartContracts = []bitcoin.SmartContractData{}
	}

	return cache, nil
}

// loadBlock tries storage first, then disk.
func (api *DataAPI) loadBlock(height int64) (*storage.BlockDataCache, error) {
	if existing, err := api.dataStorage.GetBlockData(height); err == nil {
		if cache, ok := existing.(*storage.BlockDataCache); ok {
			return cache, nil
		}
	}
	return api.loadBlockFromDisk(height)
}

// listAvailableBlockHeights discovers block files and returns heights sorted desc.
func (api *DataAPI) listAvailableBlockHeights() []int64 {
	baseDir := api.resolveBlocksDir()
	entries, err := os.ReadDir(baseDir)
	var heights []int64
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				// Support directory naming like 926464_00000000
				if idx := strings.Index(name, "_"); idx > 0 {
					if h, err := strconv.ParseInt(name[:idx], 10, 64); err == nil {
						heights = append(heights, h)
					}
				}
				continue
			}
			if strings.HasPrefix(name, "block_") && strings.HasSuffix(name, ".json") {
				raw := strings.TrimSuffix(strings.TrimPrefix(name, "block_"), ".json")
				if h, err := strconv.ParseInt(raw, 10, 64); err == nil {
					heights = append(heights, h)
				}
			}
		}
	}

	// Fallback to data storage if filesystem is empty (e.g., Postgres mode).
	if len(heights) == 0 {
		if cached, err := api.dataStorage.GetRecentBlocks(200); err == nil {
			for _, b := range cached {
				if block, ok := b.(*storage.BlockDataCache); ok {
					heights = append(heights, block.BlockHeight)
				}
			}
		}
	}

	sort.Slice(heights, func(i, j int) bool { return heights[i] > heights[j] })
	return heights
}

// EnableCORS enables CORS headers
func (api *DataAPI) EnableCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// HandleGetBlockData handles getting comprehensive block data
func (api *DataAPI) HandleGetBlockData(w http.ResponseWriter, r *http.Request) {
	api.EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract block height from URL - use simpler approach
	path := r.URL.Path
	log.Printf("Request path: %s", path)

	// Handle both /api/data/block/0 and /api/data/block/0/ formats
	var heightStr string
	if strings.HasSuffix(path, "/") {
		// /api/data/block/0/ -> extract after last slash
		parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
		heightStr = parts[len(parts)-1]
	} else {
		// /api/data/block/0 -> extract after last slash
		parts := strings.Split(path, "/")
		heightStr = parts[len(parts)-1]
	}

	log.Printf("Extracted height string: %s", heightStr)
	height, err := strconv.ParseInt(heightStr, 10, 64)
	if err != nil {
		log.Printf("ParseInt error: %v", err)
		http.Error(w, "Invalid block height", http.StatusBadRequest)
		return
	}

	// Get block data from storage
	blockData, err := api.dataStorage.GetBlockData(height)
	if err != nil {
		// Trigger on-demand scan for historical blocks
		log.Printf("Block %d not in local storage, triggering on-demand scan", height)

		scanErr := api.blockMonitor.ProcessBlock(height)
		if scanErr != nil {
			log.Printf("Failed to scan block %d: %v", height, scanErr)
			http.Error(w, "Failed to scan block", http.StatusInternalServerError)
			return
		}

		// Try getting data again after scan
		blockData, err = api.dataStorage.GetBlockData(height)
		if err != nil {
			// Try filesystem cache as a fallback if DB upsert failed.
			if cache, diskErr := api.loadBlockFromDisk(height); diskErr == nil {
				blockData = cache
				log.Printf("Served block %d from disk fallback after DB miss", height)
			} else {
				log.Printf("Block %d still not found after scan: %v (disk fallback error: %v)", height, err, diskErr)
				http.Error(w, "Block data not found", http.StatusNotFound)
				return
			}
		}
	}

	// Validate data integrity
	if validationErr := api.dataStorage.ValidateDataIntegrity(height); validationErr != nil {
		log.Printf("Data integrity validation failed for block %d: %v", height, validationErr)
		// Still return data but note the validation error
		if cacheData, ok := blockData.(*bitcoin.BlockDataCache); ok {
			cacheData.Success = false
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(blockData)
}

// HandleGetRecentBlocks handles getting recent blocks with steganography data
func (api *DataAPI) HandleGetRecentBlocks(w http.ResponseWriter, r *http.Request) {
	api.EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	log.Printf("HandleGetRecentBlocks called for URL: %s", r.URL.String())

	// Parse limit parameter
	limitStr := r.URL.Query().Get("limit")
	limit := 10 // default
	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	// Get recent blocks
	recentBlocks, err := api.dataStorage.GetRecentBlocks(limit)
	if err != nil {
		log.Printf("Failed to get recent blocks: %v", err)
		http.Error(w, "Failed to get recent blocks", http.StatusInternalServerError)
		return
	}

	log.Printf("Got %d recent blocks from storage", len(recentBlocks))

	// Always return something, even if empty, to avoid 404s
	if len(recentBlocks) == 0 {
		log.Printf("No blocks in storage, returning empty array")
		recentBlocks = []interface{}{}
	}

	// Enrich with transaction counts from block data
	enrichedBlocks := make([]interface{}, len(recentBlocks))
	for i, block := range recentBlocks {
		// Type assert to access block data
		blockData, ok := block.(*storage.BlockDataCache)
		if ok && blockData.BlockHeight > 0 {
			// Get actual transaction count from block data
			log.Printf("Block %d: loaded %d inscriptions", blockData.BlockHeight, len(blockData.Inscriptions))
			txCount := api.getTransactionCount(blockData)
			log.Printf("Block %d: %d inscriptions, %d transactions", blockData.BlockHeight, len(blockData.Inscriptions), txCount)

			inscriptions := blockData.Inscriptions
			smartContracts := filterSmartContractsForUI(blockData.SmartContracts)
			if len(inscriptions) == 0 && len(smartContracts) > 0 {
				inscriptions, _, _ = buildContractInscriptions(smartContracts, blockData.BlockHeight)
			}
			if len(smartContracts) == 0 {
				smartContracts = nil
			}
			log.Printf("Mapping %d inscriptions to smart contracts for block %d", len(inscriptions), blockData.BlockHeight)

			// Create enriched block data
			enrichedBlock := map[string]interface{}{
				"block_height":          blockData.BlockHeight,
				"block_hash":            blockData.BlockHash,
				"timestamp":             blockData.Timestamp,
				"inscriptions":          inscriptions,
				"images":                blockData.Images,
				"smart_contracts":       smartContracts,
				"scan_results":          blockData.ScanResults,
				"processing_time_ms":    blockData.ProcessingTime,
				"success":               blockData.Success,
				"steganography_summary": blockData.SteganographySummary,
				"cache_timestamp":       blockData.CacheTimestamp,
				"tx_count":              txCount, // Add actual transaction count
			}
			enrichedBlocks[i] = enrichedBlock
		} else {
			log.Printf("Block %d: no block data or invalid type", i)
			// Create fallback block with empty smart contracts
			fallbackBlock := map[string]interface{}{
				"smart_contracts": []interface{}{},
			}
			enrichedBlocks[i] = fallbackBlock
		}
	}

	// Create response
	response := map[string]interface{}{
		"blocks":       enrichedBlocks,
		"total":        len(enrichedBlocks),
		"limit":        limit,
		"last_updated": time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleGetBlockSummaries provides cursor-based block summaries for horizontal scrolling.
func (api *DataAPI) HandleGetBlockSummaries(w http.ResponseWriter, r *http.Request) {
	api.EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 20
	if lim := r.URL.Query().Get("limit"); lim != "" {
		if parsed, err := strconv.Atoi(lim); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	cursor := r.URL.Query().Get("cursor_height")
	var cursorHeight int64
	if cursor != "" {
		cursorHeight, _ = strconv.ParseInt(cursor, 10, 64)
	}

	heights := api.listAvailableBlockHeights()
	if len(heights) == 0 {
		http.Error(w, "No blocks available", http.StatusNotFound)
		return
	}

	start := 0
	if cursorHeight > 0 {
		for idx, h := range heights {
			if h < cursorHeight {
				start = idx
				break
			}
		}
	}

	end := start + limit
	if end > len(heights) {
		end = len(heights)
	}

	selected := heights[start:end]
	var summaries []map[string]interface{}
	for _, h := range selected {
		block, err := api.loadBlock(h)
		if err != nil {
			log.Printf("Failed to load block %d: %v", h, err)
			continue
		}
		inscriptions := block.Inscriptions
		smartContracts := filterSmartContractsForUI(block.SmartContracts)
		if len(inscriptions) == 0 && len(smartContracts) > 0 {
			inscriptions, _, _ = buildContractInscriptions(smartContracts, block.BlockHeight)
		}
		preview := []string{}
		for i, ins := range inscriptions {
			if i >= 3 {
				break
			}
			preview = append(preview, ins.FileName)
		}
		inscriptionCount := len(block.Inscriptions)
		contractCount := len(smartContracts)
		hasImages := len(block.Images) > 0 || inscriptionCount > 0 || contractCount > 0

		// Compute a representative thumbnail URL for the block card.
		// Prefer the first real smart contract image (served via block-image), else first inscription.
		thumbnailURL := ""
		for _, c := range smartContracts {
			if isSyntheticStegoContract(c) {
				continue
			}
			meta := c.Metadata
			fileName := strings.TrimSpace(stringFromAny(meta["image_file"]))
			if fileName == "" {
				fileName = filepath.Base(strings.TrimSpace(c.ImagePath))
			}
			if fileName != "" {
				thumbnailURL = fmt.Sprintf("/api/block-image/%d/%s", block.BlockHeight, fileName)
				break
			}
		}
		if thumbnailURL == "" && len(inscriptions) > 0 {
			// Prefer an actual image inscription for the card thumbnail (by content_type
			// or filename). Many blocks mix text + images; always taking [0] often
			// picked a .txt and caused the <img> to error → fallback pickaxe emoji.
			if chosen := pickImageLikeInscription(inscriptions); chosen != nil {
				thumbnailURL = fmt.Sprintf("/content/%s%s", chosen.TxID, func() string {
					if chosen.InputIndex >= 0 {
						return fmt.Sprintf("?witness=%d", chosen.InputIndex)
					}
					return ""
				}())
			}
		}

		// Fallback for blocks where the persisted Inscriptions list is (currently) empty
		// but we know via the tx index (from prior content access or startup scan) that
		// the block has servable inscription content. This makes the BlockCard show the
		// actual thumbnail image (e.g. for blocks like 95785 that have witness images
		// resolvable via /content/ even if the block cache list is empty).
		if thumbnailURL == "" {
			api.txMu.RLock()
			if txs, ok := api.heightIndex[block.BlockHeight]; ok && len(txs) > 0 {
				// /content/{tx} (no witness) serves the primary inscription for the tx.
				// This is the same pattern used for recent blocks and will render a real
				// image thumbnail in the scroller instead of the emoji.
				thumbnailURL = fmt.Sprintf("/content/%s", txs[0])
			}
			api.txMu.RUnlock()
		}

		// Last resort on-disk scan: if there are payload files in the block dir's images/
		// subdir (even without tx metadata), use the block-image serving route so the
		// card can still show a thumbnail.
		if thumbnailURL == "" {
			if t := api.findFirstBlockImageThumbnail(block.BlockHeight); t != "" {
				thumbnailURL = t
			}
		}

		summaries = append(summaries, map[string]interface{}{
			"block_height":          block.BlockHeight,
			"block_hash":            block.BlockHash,
			"timestamp":             block.Timestamp,
			"tx_count":              block.TxCount,
			"inscription_count":     inscriptionCount,
			"smart_contract_count":  contractCount,
			"steganography_summary": block.SteganographySummary,
			"preview_inscriptions":  preview,
			"has_images":            hasImages,
			"thumbnail_url":         thumbnailURL,
		})
	}

	var nextCursor string
	hasMore := false
	if end < len(heights) {
		nextCursor = fmt.Sprintf("%d", heights[end-1])
		hasMore = true
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"blocks":      summaries,
		"limit":       limit,
		"next_cursor": nextCursor,
		"has_more":    hasMore,
		"total":       len(heights),
	})
}

// HandleGetSteganographyStats handles getting overall steganography statistics
func (api *DataAPI) HandleGetSteganographyStats(w http.ResponseWriter, r *http.Request) {
	api.EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get statistics
	stats := api.dataStorage.GetSteganographyStats()

	// Add block monitor statistics
	monitorStats := api.blockMonitor.GetStatistics()
	stats["block_monitor"] = monitorStats

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// HandleRealtimeUpdates handles real-time updates via Server-Sent Events
func (api *DataAPI) HandleRealtimeUpdates(w http.ResponseWriter, r *http.Request) {
	api.EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create a channel for this client
	updates := make(chan *storage.RealtimeUpdate, 10)

	// Send initial connection message
	initialUpdate := api.dataStorage.CreateRealtimeUpdate("connected", 0, map[string]interface{}{
		"message":   "Connected to real-time updates",
		"timestamp": time.Now().Unix(),
	})
	api.sendSSEUpdate(w, initialUpdate)

	// Start monitoring for updates
	go api.monitorUpdates(updates)

	// Handle client connection
	defer close(updates)

	// Send updates to client
	for update := range updates {
		api.sendSSEUpdate(w, update)
	}
}

// HandleScanBlockOnDemand handles on-demand block scanning
func (api *DataAPI) HandleScanBlockOnDemand(w http.ResponseWriter, r *http.Request) {
	api.EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		BlockHeight int64 `json:"block_height"`
		StartHeight int64 `json:"start_height"`
		EndHeight   int64 `json:"end_height"`
		ForceScan   bool  `json:"force_scan"`
		Force       bool  `json:"force"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON request", http.StatusBadRequest)
		return
	}

	forceScan := request.ForceScan || request.Force
	startHeight := request.BlockHeight
	if startHeight == 0 && request.StartHeight > 0 {
		startHeight = request.StartHeight
	}
	endHeight := request.EndHeight
	if endHeight == 0 {
		endHeight = startHeight
	}

	if startHeight == 0 {
		http.Error(w, "block_height or start_height required", http.StatusBadRequest)
		return
	}
	if endHeight < startHeight {
		http.Error(w, "end_height must be >= start_height", http.StatusBadRequest)
		return
	}

	// Check if we already have data for this block
	if !forceScan && startHeight == endHeight {
		if existingData, err := api.dataStorage.GetBlockData(startHeight); err == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":    true,
				"cached":     true,
				"block_data": existingData,
				"message":    "Returned cached data",
			})
			return
		}
	}

	// Process the block
	log.Printf("On-demand scan requested for block %d-%d, force_scan=%v", startHeight, endHeight, forceScan)
	for height := startHeight; height <= endHeight; height++ {
		if err := api.blockMonitor.ProcessBlock(height); err != nil {
			http.Error(w, fmt.Sprintf("Failed to scan block %d: %v", height, err), http.StatusInternalServerError)
			return
		}
	}

	// Get the processed data
	blockData, err := api.dataStorage.GetBlockData(endHeight)
	if err != nil {
		http.Error(w, "Failed to retrieve processed block data", http.StatusInternalServerError)
		return
	}

	// Create real-time update
	update := api.dataStorage.CreateRealtimeUpdate("scan_complete", endHeight, blockData)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"cached":     false,
		"block_data": blockData,
		"message":    "Block scanned successfully",
		"scanned":    []int64{startHeight, endHeight},
		"update":     update,
	})
}

// HandleGetBlockImages handles getting images for a specific block with enhanced metadata
func (api *DataAPI) HandleGetBlockImages(w http.ResponseWriter, r *http.Request) {
	api.EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	heightStr := r.URL.Query().Get("height")
	if heightStr == "" {
		http.Error(w, "height parameter required", http.StatusBadRequest)
		return
	}

	height, err := strconv.ParseInt(heightStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid height parameter", http.StatusBadRequest)
		return
	}

	// Get block data
	blockData, err := api.dataStorage.GetBlockData(height)
	if err != nil {
		http.Error(w, "Block data not found", http.StatusNotFound)
		return
	}

	// Type assert to get the concrete data
	cacheData, ok := blockData.(*storage.BlockDataCache)
	if !ok {
		log.Printf("Invalid block data type. Expected *storage.BlockDataCache, got %T", blockData)
		http.Error(w, "Invalid block data type", http.StatusInternalServerError)
		return
	}

	// Enhance image data with scan results
	var enhancedImages []map[string]interface{}
	for i, image := range cacheData.Images {
		// Determine proper content type based on format
		contentType := "image/" + image.Format
		if image.Format == "txt" || image.Format == "text" {
			contentType = "text/plain"
		} else if image.Format == "json" {
			contentType = "application/json"
		} else if image.Format == "html" {
			contentType = "text/html"
		}

		enhancedImage := map[string]interface{}{
			"tx_id":        image.TxID,
			"file_name":    image.FileName,
			"file_path":    image.FilePath,
			"size_bytes":   image.SizeBytes,
			"format":       image.Format,
			"content_type": contentType,
			"input_index":  i,
		}

		// Read text content for text files
		if image.Format == "txt" || image.Format == "text" {
			if content, err := api.dataStorage.ReadTextContent(height, image.FilePath); err == nil {
				enhancedImage["content"] = content
			} else {
				// Storage backends like Postgres don't expose filesystem reads; skip gracefully.
				log.Printf("Skipping text content for %s: %v", image.FilePath, err)
				enhancedImage["content"] = ""
			}
		}

		// Add scan results if available
		if len(cacheData.ScanResults) > i {
			scanResult := cacheData.ScanResults[i]
			enhancedImage["scan_result"] = scanResult
			enhancedImage["is_stego"] = scanResult["is_stego"]
			enhancedImage["confidence"] = scanResult["confidence"]
			enhancedImage["stego_type"] = scanResult["stego_type"]
			enhancedImage["extracted_message"] = scanResult["extracted_message"]
		} else {
			enhancedImage["scan_result"] = map[string]interface{}{
				"is_stego":          false,
				"confidence":        0.0,
				"stego_type":        "",
				"extracted_message": "",
				"scan_error":        "Not scanned",
			}
		}

		enhancedImages = append(enhancedImages, enhancedImage)
	}

	response := map[string]interface{}{
		"block_height":          height,
		"block_hash":            cacheData.BlockHash,
		"images":                enhancedImages,
		"total":                 len(enhancedImages),
		"steganography_summary": cacheData.SteganographySummary,
		"timestamp":             time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleGetBlockInscriptionsPaginated returns inscriptions with pagination to keep UI lightweight.
func (api *DataAPI) HandleGetBlockInscriptionsPaginated(w http.ResponseWriter, r *http.Request) {
	api.EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != http.MethodGet {
		log.Printf("block-inscriptions: invalid method %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := splitPath(r.URL.Path)
	if len(parts) < 4 {
		log.Printf("block-inscriptions: invalid path %s", r.URL.Path)
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	// Expected: /api/data/block-inscriptions/{height}
	heightStr := parts[3]
	height, err := strconv.ParseInt(heightStr, 10, 64)
	if err != nil {
		log.Printf("block-inscriptions: invalid height %q", heightStr)
		http.Error(w, "invalid height", http.StatusBadRequest)
		return
	}

	limit := 20
	if lim := r.URL.Query().Get("limit"); lim != "" {
		if parsed, err := strconv.Atoi(lim); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	cursorStr := r.URL.Query().Get("cursor")
	filter := r.URL.Query().Get("filter")
	fields := r.URL.Query().Get("fields")
	if fields == "" {
		fields = "summary"
	}

	cursor := 0
	if cursorStr != "" {
		if parsed, err := strconv.Atoi(cursorStr); err == nil && parsed >= 0 {
			cursor = parsed
		}
	}

	log.Printf("block-inscriptions: height=%d cursor=%d limit=%d filter=%s fields=%s", height, cursor, limit, filter, fields)

	block, err := api.loadBlock(height)
	if err != nil {
		log.Printf("block-inscriptions: block %d not found: %v", height, err)
		http.Error(w, "block not found", http.StatusNotFound)
		return
	}

	inscriptions := block.Inscriptions
	imageURLOverrides := map[int]string{}
	metadataOverrides := map[int]map[string]any{}
	if len(block.SmartContracts) > 0 {
		contractInscriptions, contractImageOverrides, contractMetadataOverrides := buildContractInscriptions(filterSmartContractsForUI(block.SmartContracts), height)
		if len(inscriptions) == 0 {
			inscriptions = contractInscriptions
			imageURLOverrides = contractImageOverrides
			metadataOverrides = contractMetadataOverrides
		} else {
			offset := len(inscriptions)
			inscriptions = append(inscriptions, contractInscriptions...)
			for idx, url := range contractImageOverrides {
				imageURLOverrides[offset+idx] = url
			}
			for idx, meta := range contractMetadataOverrides {
				metadataOverrides[offset+idx] = meta
			}
		}
	}
	if filter == "text" {
		var filtered []bitcoin.InscriptionData
		newImageOverrides := map[int]string{}
		newMetaOverrides := map[int]map[string]any{}
		for i, ins := range inscriptions {
			if strings.HasPrefix(strings.ToLower(ins.ContentType), "text/") || ins.Content != "" {
				idx := len(filtered)
				filtered = append(filtered, ins)
				if v, ok := imageURLOverrides[i]; ok {
					newImageOverrides[idx] = v
				}
				if v, ok := metadataOverrides[i]; ok {
					newMetaOverrides[idx] = v
				}
			}
		}
		inscriptions = filtered
		imageURLOverrides = newImageOverrides
		metadataOverrides = newMetaOverrides
	} else if filter == "image" {
		var filtered []bitcoin.InscriptionData
		newImageOverrides := map[int]string{}
		newMetaOverrides := map[int]map[string]any{}
		for i, ins := range inscriptions {
			// An inscription is image-like if:
			// 1. It has an image content type or image file extension, OR
			// 2. It has a block-image URL override (smart contract images), OR
			// 3. Sniffing the actual file on disk reveals an image type
			isImage := isImageLikeInscription(ins)
			if !isImage {
				if override, ok := imageURLOverrides[i]; ok && strings.Contains(override, "/block-image/") {
					isImage = true
				}
			}
			if !isImage && ins.FilePath != "" {
				if sniffed := api.sniffContentType(height, ins.FilePath); strings.HasPrefix(sniffed, "image/") {
					isImage = true
				}
			}
			if isImage {
				idx := len(filtered)
				filtered = append(filtered, ins)
				if v, ok := imageURLOverrides[i]; ok {
					newImageOverrides[idx] = v
				}
				if v, ok := metadataOverrides[i]; ok {
					newMetaOverrides[idx] = v
				}
			}
		}
		inscriptions = filtered
		imageURLOverrides = newImageOverrides
		metadataOverrides = newMetaOverrides
	}

	start := cursor
	if start > len(inscriptions) {
		start = len(inscriptions)
	}

	end := start + limit
	if end > len(inscriptions) {
		end = len(inscriptions)
	}

	selected := inscriptions[start:end]
	var nextCursor interface{}
	hasMore := end < len(inscriptions)
	if hasMore {
		nextCursor = strconv.Itoa(end)
	} else {
		nextCursor = nil
	}

	var responseItems []map[string]interface{}
	for i, ins := range selected {
		// Derive a safe content type; some historical entries may miss it.
		contentType := ins.ContentType
		contentType = inferMime(contentType, nil, ins.FileName)
		if contentType == "" || contentType == "application/octet-stream" {
			if sniffed := api.sniffContentType(height, ins.FilePath); sniffed != "" {
				contentType = sniffed
			}
		}

		imageURL := fmt.Sprintf("/content/%s%s", ins.TxID, func() string {
			if ins.InputIndex >= 0 {
				return fmt.Sprintf("?witness=%d", ins.InputIndex)
			}
			return ""
		}())
		if override, ok := imageURLOverrides[start+i]; ok && override != "" {
			imageURL = override
		}

		entry := map[string]interface{}{
			"id":                   fmt.Sprintf("%s_%d", ins.TxID, ins.InputIndex),
			"tx_id":                ins.TxID,
			"input_index":          ins.InputIndex,
			"file_name":            ins.FileName,
			"file_path":            ins.FilePath,
			"content_type":         contentType,
			"size_bytes":           ins.SizeBytes,
			"genesis_block_height": height,
			"number":               height,
			"address":              "bc1p...",
			"image_url":            imageURL,
		}
		if meta, ok := metadataOverrides[start+i]; ok {
			entry["metadata"] = meta
		}

		// If this looks like a text inscription, try to hydrate content from disk when missing or placeholder.
		isTextType := strings.HasPrefix(strings.ToLower(contentType), "text/") || strings.HasSuffix(strings.ToLower(ins.FileName), ".txt")
		inscriptionContent := ins.Content
		if isTextType {
			// Detect placeholder content and attempt to read the actual file.
			looksPlaceholder := inscriptionContent == "" || strings.HasPrefix(inscriptionContent, "Extracted from transaction")
			if looksPlaceholder {
				blockDir := fmt.Sprintf("%s/%d_00000000", strings.TrimRight(api.resolveBlocksDir(), "/"), height)
				safePath, err := security.SanitizePath(blockDir, ins.FilePath)
				if err == nil {
					if data, err := os.ReadFile(safePath); err == nil {
						inscriptionContent = string(data)
					}
				}
			}
		}

		// Always include textual content so the UI can render inscriptions without extra fetches.
		if fields == "full" || isTextType || inscriptionContent != "" {
			entry["content"] = inscriptionContent
		}

		if len(block.ScanResults) > i {
			entry["scan_result"] = block.ScanResults[i]
		}

		responseItems = append(responseItems, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"inscriptions": responseItems,
		"has_more":     hasMore,
		"next_cursor":  nextCursor,
	})
}

func (api *DataAPI) serveBlockImage(w http.ResponseWriter, height int64, filePath string) {
	if strings.TrimSpace(filePath) == "" {
		http.Error(w, "inscription not found", http.StatusNotFound)
		return
	}
	base := strings.TrimRight(api.resolveBlocksDir(), "/")
	baseDir := fmt.Sprintf("%s/%d_00000000", base, height)
	safePath, err := security.SanitizePath(baseDir, filePath)
	if err != nil {
		http.Error(w, "inscription not found", http.StatusNotFound)
		return
	}
	data, err := os.ReadFile(safePath)
	if err != nil {
		http.Error(w, "inscription not found", http.StatusNotFound)
		return
	}
	mimeType := inferMime("", data, filepath.Base(filePath))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Header().Set("X-Inscription-Mime", mimeType)
	w.Header().Set("X-Inscription-Size", fmt.Sprintf("%d", len(data)))
	w.Header().Set("X-Inscription-Hash", sha256Hex(data))
	w.Write(data)
}

// buildTxIndex creates a simple in-memory map from txid to block height for faster lookup.
func (api *DataAPI) buildTxIndex() {
	heights := api.listAvailableBlockHeights()
	newIndex := make(map[string]int64)
	newHeightIndex := make(map[int64][]string)
	for _, h := range heights {
		block, err := api.loadBlock(h)
		if err != nil {
			continue
		}
		inscriptions := block.Inscriptions
		if len(inscriptions) == 0 && len(block.SmartContracts) > 0 {
			inscriptions, _, _ = buildContractInscriptions(block.SmartContracts, block.BlockHeight)
		}
		for _, ins := range inscriptions {
			if ins.TxID != "" {
				ntx := normalizeTxID(ins.TxID)
				newIndex[ntx] = h
				newHeightIndex[h] = appendIfNotPresent(newHeightIndex[h], ntx)
			}
		}
	}
	api.txMu.Lock()
	api.txIndex = newIndex
	api.heightIndex = newHeightIndex
	api.txMu.Unlock()
}

// IndexBlock incrementally adds a single block's inscriptions to the tx index.
// Designed to be called from BlockMonitor.OnBlockProcessed.
func (api *DataAPI) IndexBlock(height int64) {
	block, err := api.loadBlock(height)
	if err != nil {
		return
	}
	inscriptions := block.Inscriptions
	if len(inscriptions) == 0 && len(block.SmartContracts) > 0 {
		inscriptions, _, _ = buildContractInscriptions(block.SmartContracts, block.BlockHeight)
	}
	api.txMu.Lock()
	for _, ins := range inscriptions {
		if ins.TxID != "" {
			ntx := normalizeTxID(ins.TxID)
			api.txIndex[ntx] = height
			api.heightIndex[height] = appendIfNotPresent(api.heightIndex[height], ntx)
		}
	}
	api.txMu.Unlock()
}

func (api *DataAPI) lookupTxHeight(txid string) (int64, bool) {
	api.txMu.RLock()
	h, ok := api.txIndex[txid]
	api.txMu.RUnlock()
	return h, ok
}

// appendIfNotPresent is a tiny helper for maintaining the heightIndex without duplicates.
func appendIfNotPresent(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// findFirstBlockImageThumbnail scans the on-disk layout for a block and returns
// a /api/block-image/... URL for the first file found under its images/ subdir
// (if any). This acts as a last-resort so BlockCards can show a real thumbnail
// for heights that have payload files on disk even when the Inscriptions list
// in the loaded BlockDataCache is empty.
func (api *DataAPI) findFirstBlockImageThumbnail(height int64) string {
	base := strings.TrimRight(api.resolveBlocksDir(), "/")
	// Try the two common directory shapes used by the system.
	candidates := []string{
		filepath.Join(base, fmt.Sprintf("%d_00000000", height), "images"),
	}
	// Also support the hashed suffix form <height>_<hash>
	if matches, err := filepath.Glob(filepath.Join(base, fmt.Sprintf("%d_*", height), "images")); err == nil {
		candidates = append(candidates, matches...)
	}

	for _, imgDir := range candidates {
		entries, err := os.ReadDir(imgDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			// Return the first file we find; the block-image handler will serve it
			// and set an appropriate Content-Type.
			return fmt.Sprintf("/api/block-image/%d/%s", height, name)
		}
	}
	return ""
}
