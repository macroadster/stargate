package api

import (
	"bytes"
	"crypto/hmac"
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

	"stargate-backend/bitcoin"
	"stargate-backend/security"
	"stargate-backend/storage"
)

// DataAPI handles enhanced API endpoints for block monitoring data
type DataAPI struct {
	dataStorage  storage.ExtendedDataStorage
	blockMonitor *bitcoin.BlockMonitor
	bitcoinAPI   *bitcoin.BitcoinAPI
	// simple in-memory index of tx -> block height for manifest/content lookup
	txIndex map[string]int64
	txMu    sync.RWMutex
}

// NewDataAPI creates a new data API instance
func NewDataAPI(dataStorage storage.ExtendedDataStorage, blockMonitor *bitcoin.BlockMonitor, bitcoinAPI *bitcoin.BitcoinAPI) *DataAPI {
	api := &DataAPI{
		dataStorage:  dataStorage,
		blockMonitor: blockMonitor,
		bitcoinAPI:   bitcoinAPI,
		txIndex:      make(map[string]int64),
	}
	api.buildTxIndex()
	return api
}

// resolveBlocksDir returns the directory that holds block JSON artifacts.
func (api *DataAPI) resolveBlocksDir() string {
	if dir := os.Getenv("BLOCKS_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("DATA_DIR"); dir != "" {
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
		for _, ins := range inscriptions {
			if strings.HasPrefix(strings.ToLower(ins.ContentType), "text/") || ins.Content != "" {
				filtered = append(filtered, ins)
			}
		}
		inscriptions = filtered
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

func (api *DataAPI) sniffContentType(height int64, filePath string) string {
	if strings.TrimSpace(filePath) == "" {
		return ""
	}
	base := strings.TrimRight(api.resolveBlocksDir(), "/")
	fsPath := filepath.Join(fmt.Sprintf("%s/%d_00000000", base, height), filePath)
	file, err := os.Open(fsPath)
	if err != nil {
		return ""
	}
	defer file.Close()
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	if n <= 0 {
		return ""
	}
	if detected := http.DetectContentType(buf[:n]); detected != "" {
		return detected
	}
	return ""
}

func buildContractInscriptions(contracts []bitcoin.SmartContractData, height int64) ([]bitcoin.InscriptionData, map[int]string, map[int]map[string]any) {
	var out []bitcoin.InscriptionData
	imageURLs := map[int]string{}
	metadata := map[int]map[string]any{}
	firstFundingTxID := func(meta map[string]any) string {
		if meta == nil {
			return ""
		}
		if v := strings.TrimSpace(stringFromAny(meta["funding_txid"])); v != "" {
			return v
		}
		switch values := meta["funding_txids"].(type) {
		case []string:
			if len(values) > 0 {
				return strings.TrimSpace(values[0])
			}
		case []any:
			for _, item := range values {
				if txid, ok := item.(string); ok && strings.TrimSpace(txid) != "" {
					return strings.TrimSpace(txid)
				}
			}
		}
		return ""
	}

	for _, contract := range contracts {
		if isSyntheticStegoContract(contract) {
			continue
		}
		meta := contract.Metadata
		fileName := strings.TrimSpace(stringFromAny(meta["image_file"]))
		if fileName == "" {
			fileName = filepath.Base(strings.TrimSpace(contract.ImagePath))
		}
		if fileName == "" {
			continue
		}
		filePath := strings.TrimSpace(contract.ImagePath)
		if filePath == "" {
			filePath = filepath.Join("images", fileName)
		}

		txID := strings.TrimSpace(stringFromAny(meta["tx_id"]))
		if !isLikelyTxID(txID) {
			if candidate := strings.TrimSpace(stringFromAny(meta["confirmed_txid"])); isLikelyTxID(candidate) {
				txID = candidate
			} else if candidate := strings.TrimSpace(stringFromAny(meta["funding_txid"])); isLikelyTxID(candidate) {
				txID = candidate
			} else if candidate := firstFundingTxID(meta); isLikelyTxID(candidate) {
				txID = candidate
			} else if candidate := strings.TrimSpace(stringFromAny(meta["match_hash"])); isLikelyTxID(candidate) {
				txID = candidate
			} else if isLikelyTxID(contract.ContractID) {
				txID = strings.TrimSpace(contract.ContractID)
			}
		}
		if !isLikelyTxID(txID) {
			lowerFile := strings.ToLower(fileName)
			lowerTx := strings.ToLower(txID)
			if strings.HasPrefix(lowerFile, "unknown_") || strings.HasSuffix(lowerFile, ".png") || strings.Contains(lowerTx, ".png") {
				continue
			}
		}
		inputIndex := 0
		if idx, ok := intFromAny(meta["input_index"]); ok {
			inputIndex = idx
		} else if idx, ok := intFromAny(meta["output_index"]); ok {
			inputIndex = idx
		}

		contentType := inferMime("", nil, fileName)
		out = append(out, bitcoin.InscriptionData{
			TxID:        txID,
			InputIndex:  inputIndex,
			FileName:    fileName,
			FilePath:    filePath,
			ContentType: contentType,
			SizeBytes:   0,
			Content:     "",
		})

		idx := len(out) - 1
		imageURLs[idx] = fmt.Sprintf("/api/block-image/%d/%s", height, fileName)
		if meta != nil {
			metadata[idx] = meta
		}
	}

	return out, imageURLs, metadata
}

func filterSmartContractsForUI(contracts []bitcoin.SmartContractData) []bitcoin.SmartContractData {
	if len(contracts) == 0 {
		return contracts
	}
	out := make([]bitcoin.SmartContractData, 0, len(contracts))
	for _, contract := range contracts {
		if isSyntheticStegoContract(contract) {
			continue
		}
		out = append(out, contract)
	}
	return out
}

func isSyntheticStegoContract(contract bitcoin.SmartContractData) bool {
	contractID := strings.ToLower(strings.TrimSpace(contract.ContractID))
	if strings.HasPrefix(contractID, "stego_") {
		return true
	}
	if contract.Metadata == nil {
		return false
	}
	txID := strings.ToLower(strings.TrimSpace(stringFromAny(contract.Metadata["tx_id"])))
	if strings.HasPrefix(txID, "unknown_") {
		return true
	}
	return false
}

// HandleStegoCallback ingests scan results from the Python scanner instead of filesystem writes.
func (api *DataAPI) HandleStegoCallback(w http.ResponseWriter, r *http.Request) {
	api.EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != http.MethodPost {
		log.Printf("stego-callback: invalid method %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("stego-callback: read body error: %v", err)
		http.Error(w, "unable to read body", http.StatusBadRequest)
		return
	}

	secret := os.Getenv("STARLIGHT_CALLBACK_SECRET")
	if secret != "" {
		if !api.verifySignature(secret, body, r.Header.Get("X-Starlight-Signature")) {
			log.Printf("stego-callback: signature verification failed")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Detect batch payload (block-level with inscriptions array)
	var batchProbe struct {
		Inscriptions []map[string]interface{} `json:"inscriptions"`
		BlockHeight  int64                    `json:"block_height"`
	}
	if err := json.Unmarshal(body, &batchProbe); err == nil && len(batchProbe.Inscriptions) > 0 && batchProbe.BlockHeight > 0 {
		log.Printf("stego-callback: batch payload height=%d count=%d", batchProbe.BlockHeight, len(batchProbe.Inscriptions))
		if err := api.handleStegoBatch(batchProbe.BlockHeight, body, w); err != nil {
			log.Printf("stego-callback: batch error height=%d: %v", batchProbe.BlockHeight, err)
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	var payload struct {
		RequestID     string                 `json:"request_id"`
		BlockHeight   int64                  `json:"block_height"`
		TxID          string                 `json:"tx_id"`
		FileName      string                 `json:"file_name"`
		ContentType   string                 `json:"content_type"`
		SizeBytes     int                    `json:"size_bytes"`
		ScanResult    map[string]interface{} `json:"scan_result"`
		Metadata      map[string]interface{} `json:"metadata"`
		ImageBytesB64 string                 `json:"image_bytes_b64"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("stego-callback: invalid JSON: %v", err)
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if payload.BlockHeight == 0 {
		log.Printf("stego-callback: missing block height")
		http.Error(w, "missing block height", http.StatusBadRequest)
		return
	}

	block, err := api.loadBlock(payload.BlockHeight)
	if err != nil {
		log.Printf("stego-callback: block %d not found: %v", payload.BlockHeight, err)
		http.Error(w, "block not found", http.StatusNotFound)
		return
	}

	idx := -1
	for i, ins := range block.Inscriptions {
		if payload.FileName != "" && ins.FileName == payload.FileName {
			idx = i
			break
		}
		if payload.TxID != "" && ins.TxID == payload.TxID {
			idx = i
			break
		}
	}

	if idx == -1 {
		// Append new inscription entry for completeness
		block.Inscriptions = append(block.Inscriptions, bitcoin.InscriptionData{
			TxID:        payload.TxID,
			ContentType: payload.ContentType,
			FileName:    payload.FileName,
			FilePath:    fmt.Sprintf("images/%s", payload.FileName),
			SizeBytes:   payload.SizeBytes,
			Content:     "",
		})
		idx = len(block.Inscriptions) - 1
	}

	for len(block.ScanResults) < len(block.Inscriptions) {
		block.ScanResults = append(block.ScanResults, map[string]interface{}{})
	}
	block.ScanResults[idx] = payload.ScanResult

	// Persist updated block data
	resp := &bitcoin.BlockInscriptionsResponse{
		BlockHeight:       block.BlockHeight,
		BlockHash:         block.BlockHash,
		Timestamp:         block.Timestamp,
		TotalTransactions: block.TxCount,
		Inscriptions:      block.Inscriptions,
		Images:            block.Images,
		SmartContracts:    block.SmartContracts,
		ProcessingTime:    block.ProcessingTime,
		Success:           block.Success,
	}
	if err := api.dataStorage.StoreBlockData(resp, block.ScanResults); err != nil {
		log.Printf("Failed to persist stego callback update: %v", err)
		http.Error(w, "failed to persist update", http.StatusInternalServerError)
		return
	}

	update := api.dataStorage.CreateRealtimeUpdate("scan_complete", block.BlockHeight, payload)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "accepted",
		"request": payload.RequestID,
		"update":  update,
	})
}

func (api *DataAPI) handleStegoBatch(blockHeight int64, body []byte, w http.ResponseWriter) error {
	var payload struct {
		RequestID    string `json:"request_id"`
		BlockHeight  int64  `json:"block_height"`
		BlockHash    string `json:"block_hash"`
		Timestamp    int64  `json:"timestamp"`
		Inscriptions []struct {
			TxID        string                 `json:"tx_id"`
			InputIndex  int                    `json:"input_index"`
			FileName    string                 `json:"file_name"`
			FilePath    string                 `json:"file_path"`
			ContentType string                 `json:"content_type"`
			SizeBytes   int                    `json:"size_bytes"`
			ScanResult  map[string]interface{} `json:"scan_result"`
		} `json:"inscriptions"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if payload.BlockHeight == 0 {
		return fmt.Errorf("missing block height")
	}

	block, err := api.loadBlock(payload.BlockHeight)
	if err != nil {
		block = &storage.BlockDataCache{
			BlockHeight:    payload.BlockHeight,
			BlockHash:      payload.BlockHash,
			Timestamp:      payload.Timestamp,
			Inscriptions:   []bitcoin.InscriptionData{},
			Images:         []bitcoin.ExtractedImageData{},
			SmartContracts: []bitcoin.SmartContractData{},
			ScanResults:    []map[string]interface{}{},
			ProcessingTime: 0,
			Success:        true,
			CacheTimestamp: time.Now(),
			SteganographySummary: &bitcoin.SteganographySummary{
				TotalImages:   len(payload.Inscriptions),
				StegoDetected: false,
				StegoCount:    0,
				ScanTimestamp: time.Now().Unix(),
				AvgConfidence: 0,
				StegoTypes:    []string{},
			},
		}
	}

	if block.BlockHash == "" {
		block.BlockHash = payload.BlockHash
	}
	if block.Timestamp == 0 {
		block.Timestamp = payload.Timestamp
	}

	for _, ins := range payload.Inscriptions {
		idx := -1
		for i, existing := range block.Inscriptions {
			if ins.FileName != "" && existing.FileName == ins.FileName {
				idx = i
				break
			}
			if ins.TxID != "" && existing.TxID == ins.TxID {
				idx = i
				break
			}
		}

		if idx == -1 {
			block.Inscriptions = append(block.Inscriptions, bitcoin.InscriptionData{
				TxID:        ins.TxID,
				InputIndex:  ins.InputIndex,
				ContentType: ins.ContentType,
				Content:     "",
				SizeBytes:   ins.SizeBytes,
				FileName:    ins.FileName,
				FilePath:    ins.FilePath,
			})
			idx = len(block.Inscriptions) - 1
		} else {
			block.Inscriptions[idx].ContentType = ins.ContentType
			block.Inscriptions[idx].SizeBytes = ins.SizeBytes
			if block.Inscriptions[idx].FileName == "" {
				block.Inscriptions[idx].FileName = ins.FileName
			}
			if block.Inscriptions[idx].FilePath == "" {
				block.Inscriptions[idx].FilePath = ins.FilePath
			}
		}

		for len(block.ScanResults) < len(block.Inscriptions) {
			block.ScanResults = append(block.ScanResults, map[string]interface{}{})
		}
		block.ScanResults[idx] = ins.ScanResult
	}

	resp := &bitcoin.BlockInscriptionsResponse{
		BlockHeight:       block.BlockHeight,
		BlockHash:         block.BlockHash,
		Timestamp:         block.Timestamp,
		TotalTransactions: block.TxCount,
		Inscriptions:      block.Inscriptions,
		Images:            block.Images,
		SmartContracts:    block.SmartContracts,
		ProcessingTime:    block.ProcessingTime,
		Success:           true,
	}
	if resp.TotalTransactions == 0 {
		resp.TotalTransactions = len(block.Inscriptions)
	}

	if err := api.dataStorage.StoreBlockData(resp, block.ScanResults); err != nil {
		return fmt.Errorf("persist batch update: %w", err)
	}

	update := api.dataStorage.CreateRealtimeUpdate("scan_complete", block.BlockHeight, map[string]interface{}{
		"mode":         "batch",
		"inscriptions": len(payload.Inscriptions),
		"request_id":   payload.RequestID,
		"block_height": block.BlockHeight,
		"block_hash":   block.BlockHash,
		"updated_at":   time.Now().Unix(),
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "accepted",
		"request": payload.RequestID,
		"update":  update,
	})

	return nil
}

// HandleContent routes content requests to raw or manifest responses.
func (api *DataAPI) HandleContent(w http.ResponseWriter, r *http.Request) {
	api.EnableCORS(w, r)
	if r.Method == http.MethodOptions {
		return
	}
	// Rebuild the tx index on each request to pick up newly processed blocks.
	api.buildTxIndex()
	path := strings.TrimPrefix(r.URL.Path, "/content/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "missing txid", http.StatusBadRequest)
		return
	}
	txid := normalizeTxID(parts[0])
	isManifest := len(parts) > 1 && parts[1] == "manifest"

	if isManifest {
		api.handleContentManifest(w, r, txid)
		return
	}
	api.handleContentRaw(w, r, txid)
}

// handleContentRaw returns raw payload for a txid (optionally by witness).
func (api *DataAPI) handleContentRaw(w http.ResponseWriter, r *http.Request, txid string) {
	witnessParam := r.URL.Query().Get("witness")
	var witnessIndex *int
	if witnessParam != "" {
		if wi, err := strconv.Atoi(witnessParam); err == nil {
			witnessIndex = &wi
		}
	}

	height, insList, err := api.findInscriptionsByTx(txid)
	if err != nil || len(insList) == 0 {
		if height, filePath, ok := api.findContractImageByTx(txid); ok {
			api.serveBlockImage(w, height, filePath)
			return
		}
		http.Error(w, "inscription not found", http.StatusNotFound)
		return
	}

	inscription := insList[0]
	if witnessIndex != nil {
		for _, ins := range insList {
			if ins.InputIndex == *witnessIndex {
				inscription = ins
				break
			}
		}
	}

	content, mimeType := api.loadInscriptionContent(height, inscription)
	log.Printf("content served tx=%s len=%d first8=%x", txid, len(content), func() []byte {
		if len(content) >= 8 {
			return content[:8]
		}
		return content
	}())
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
	w.Header().Set("X-Inscription-Mime", mimeType)
	w.Header().Set("X-Inscription-Size", fmt.Sprintf("%d", len(content)))
	w.Header().Set("X-Inscription-Hash", sha256Hex(content))
	w.Write(content)
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

// handleContentManifest returns a JSON manifest of all inscription parts for a txid.
func (api *DataAPI) handleContentManifest(w http.ResponseWriter, r *http.Request, txid string) {
	height, insList, err := api.findInscriptionsByTx(txid)
	if err != nil {
		http.Error(w, "inscription not found", http.StatusNotFound)
		return
	}

	parts := []map[string]interface{}{}
	for _, ins := range insList {
		content, mimeType := api.loadInscriptionContent(height, ins)
		parts = append(parts, map[string]interface{}{
			"witness_index": ins.InputIndex,
			"size_bytes":    len(content),
			"mime_type":     mimeType,
			"hash":          sha256Hex(content),
			"primary":       ins.InputIndex == insList[0].InputIndex,
			"url":           fmt.Sprintf("/content/%s?witness=%d", txid, ins.InputIndex),
		})
	}

	resp := map[string]interface{}{
		"tx_id":        txid,
		"block_height": height,
		"parts":        parts,
		"stitch_hint":  "unknown",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// findInscriptionsByTx scans known blocks to locate a txid.
func (api *DataAPI) findInscriptionsByTx(txid string) (int64, []bitcoin.InscriptionData, error) {
	if height, ok := api.lookupTxHeight(txid); ok {
		if block, err := api.loadBlock(height); err == nil {
			inscriptions := block.Inscriptions
			if len(inscriptions) == 0 && len(block.SmartContracts) > 0 {
				inscriptions, _, _ = buildContractInscriptions(block.SmartContracts, block.BlockHeight)
			}
			var hits []bitcoin.InscriptionData
			for _, ins := range inscriptions {
				if normalizeTxID(ins.TxID) == txid {
					hits = append(hits, ins)
				}
			}
			if len(hits) > 0 {
				return height, hits, nil
			}
		}
	}

	heights := api.listAvailableBlockHeights()
	for _, h := range heights {
		block, err := api.loadBlock(h)
		if err != nil {
			continue
		}
		inscriptions := block.Inscriptions
		if len(inscriptions) == 0 && len(block.SmartContracts) > 0 {
			inscriptions, _, _ = buildContractInscriptions(block.SmartContracts, block.BlockHeight)
		}
		var hits []bitcoin.InscriptionData
		for _, ins := range inscriptions {
			if normalizeTxID(ins.TxID) == txid {
				hits = append(hits, ins)
			}
		}
		if len(hits) > 0 {
			api.txMu.Lock()
			api.txIndex[txid] = h
			api.txMu.Unlock()
			return h, hits, nil
		}
	}
	return 0, nil, fmt.Errorf("not found")
}

func (api *DataAPI) findContractImageByTx(txid string) (int64, string, bool) {
	txid = normalizeTxID(txid)
	if txid == "" {
		return 0, "", false
	}
	heights := api.listAvailableBlockHeights()
	for _, height := range heights {
		block, err := api.loadBlock(height)
		if err != nil {
			continue
		}
		for _, contract := range block.SmartContracts {
			meta := contract.Metadata
			if !contractMatchesTx(meta, txid) {
				continue
			}
			filePath := strings.TrimSpace(contract.ImagePath)
			if filePath == "" {
				filePath = filepath.Join("images", filepath.Base(strings.TrimSpace(stringFromAny(meta["image_file"]))))
			}
			if strings.TrimSpace(filePath) != "" {
				return height, filePath, true
			}
		}
	}
	return 0, "", false
}

func contractMatchesTx(meta map[string]any, txid string) bool {
	if meta == nil || txid == "" {
		return false
	}
	candidates := []string{
		stringFromAny(meta["tx_id"]),
		stringFromAny(meta["confirmed_txid"]),
		stringFromAny(meta["funding_txid"]),
		stringFromAny(meta["match_hash"]),
	}
	switch values := meta["funding_txids"].(type) {
	case []string:
		candidates = append(candidates, values...)
	case []any:
		for _, item := range values {
			if v, ok := item.(string); ok {
				candidates = append(candidates, v)
			}
		}
	case string:
		candidates = append(candidates, strings.Split(values, ",")...)
	}
	for _, candidate := range candidates {
		if normalizeTxID(candidate) == txid {
			return true
		}
	}
	return false
}

// loadInscriptionContent fetches inscription payload and inferred MIME.
func (api *DataAPI) loadInscriptionContent(height int64, ins bitcoin.InscriptionData) ([]byte, string) {
	content := []byte(ins.Content)
	mimeType := inferMime(ins.ContentType, content, ins.FileName)

	base := strings.TrimRight(api.resolveBlocksDir(), "/")
	blockDir := fmt.Sprintf("%s/%d_00000000", base, height)
	safePath, err := security.SanitizePath(blockDir, ins.FilePath)
	if err == nil {
		if data, err := os.ReadFile(safePath); err == nil {
			// Prefer filesystem copy whenever it exists; it's the source of truth.
			content = data
			mimeType = inferMime(ins.ContentType, content, ins.FileName)
		}
	}

	// If content is base64-encoded (used for binary payloads in DB), decode it.
	if strings.HasPrefix(mimeType, "image/") && len(ins.Content) > 0 && looksBase64(ins.Content) && len(content) == len(ins.Content) {
		if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(ins.Content)); err == nil && len(decoded) > 0 {
			content = decoded
			mimeType = inferMime(ins.ContentType, content, ins.FileName)
		}
	}

	// Attempt to rebuild from pushdatas for text-like payloads only.
	if !strings.HasPrefix(mimeType, "image/") {
		if rebuilt, ok := extractPushPayload(content); ok {
			content = rebuilt
			mimeType = inferMime(ins.ContentType, content, ins.FileName)
		}
	}

	// If this is an image but the payload has leading garbage, trim to the first valid signature.
	if strings.HasPrefix(mimeType, "image/") {
		if trimmed := trimToImageSignature(content); len(trimmed) > 0 {
			content = trimmed
		}
		// Re-evaluate MIME after trimming.
		mimeType = inferMime(ins.ContentType, content, ins.FileName)
	}

	// Trim trailing single 'h' artifact on text payloads.
	if strings.HasPrefix(mimeType, "text/") && len(content) > 0 && content[len(content)-1] == 'h' {
		content = content[:len(content)-1]
	}

	// Strip leading pushdata opcodes that may have been preserved in extraction.
	if strings.HasPrefix(mimeType, "text/") {
		if cleaned := stripPushdataPrefix(content); len(cleaned) > 0 {
			content = cleaned
		}
		if cleaned := stripNonPrintablePrefix(content); len(cleaned) > 0 {
			content = cleaned
		}
	}

	// For HTML payloads, trim any stray metadata bytes that appear before the first tag.
	if strings.HasPrefix(mimeType, "text/html") {
		if idx := bytes.IndexByte(content, '<'); idx > 0 {
			content = content[idx:]
		}
	}

	return content, mimeType
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// inferMime attempts to produce a better content type when missing or generic.
func inferMime(current string, content []byte, fileName string) string {
	m := strings.ToLower(strings.TrimSpace(current))
	if m == "image/txt" {
		m = "text/plain"
	}
	if m == "image/avif" {
		return "image/avif"
	}
	if m == "image/svg" {
		m = "image/svg+xml"
	}

	// Enhanced content detection for files without extensions
	if len(content) > 0 && (m == "" || m == "application/octet-stream") {
		sample := content
		if len(sample) > 512 {
			sample = sample[:512]
		}
		if detected := http.DetectContentType(sample); detected != "" && detected != "application/octet-stream" {
			m = detected
		}
	}

	// Prefer explicit image types by filename if type is missing or generic.
	if m == "" || m == "application/octet-stream" {
		lowerName := strings.ToLower(fileName)
		switch {
		case strings.HasSuffix(lowerName, ".avif"):
			m = "image/avif"
		case strings.HasSuffix(lowerName, ".jpg"), strings.HasSuffix(lowerName, ".jpeg"):
			m = "image/jpeg"
		case strings.HasSuffix(lowerName, ".png"):
			m = "image/png"
		case strings.HasSuffix(lowerName, ".gif"):
			m = "image/gif"
		case strings.HasSuffix(lowerName, ".webp"):
			m = "image/webp"
		case strings.HasSuffix(lowerName, ".svg"):
			m = "image/svg+xml"
		case strings.HasSuffix(lowerName, ".bmp"):
			m = "image/bmp"
		case strings.HasSuffix(lowerName, ".html"), strings.HasSuffix(lowerName, ".htm"):
			m = "text/html"
		case strings.HasSuffix(lowerName, ".json"):
			m = "application/json"
		}
	}

	if m == "" {
		trim := strings.TrimSpace(string(content))
		lower := strings.ToLower(trim)
		if strings.HasPrefix(lower, "<!doctype") || strings.HasPrefix(lower, "<html") {
			m = "text/html"
		} else if strings.HasPrefix(lower, "<svg") {
			m = "image/svg+xml"
		} else if json.Valid(content) {
			m = "application/json"
		} else if isMostlyPrintable(trim) {
			m = "text/plain"
		} else {
			m = "application/octet-stream"
		}
	}
	return m
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		return v.String()
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
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
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func isAVIF(b []byte) bool {
	if len(b) < 12 {
		return false
	}
	// ISO BMFF: size(4) 'ftyp' then brand.
	if b[4] == 'f' && b[5] == 't' && b[6] == 'y' && b[7] == 'p' {
		brand := string(b[8:12])
		if brand == "avif" || brand == "avis" {
			return true
		}
	}
	// Search for ftypavif within the first 64 bytes.
	if idx := bytes.Index(b, []byte("ftypavif")); idx >= 0 && idx < 64 {
		return true
	}
	return false
}

// extractPushPayload rebuilds a script blob by concatenating its pushdatas in order.
// Non-push opcodes are discarded; if parsing fails, the original buffer is returned.
func extractPushPayload(script []byte) ([]byte, bool) {
	ops, ok := parseScriptOpsLocal(script)
	if !ok || len(ops) == 0 {
		return script, false
	}
	var out bytes.Buffer
	pushes := 0
	for _, op := range ops {
		if op.isPush {
			out.Write(op.data)
			pushes++
		}
	}
	if pushes == 0 {
		return script, false
	}

	total := 0
	for _, op := range ops {
		if op.isPush {
			total += len(op.data)
		}
	}
	// Require that total pushed bytes are at least as large as the script; otherwise
	// we likely mis-parsed plain text and should preserve the original.
	if total < len(script) && total < 256 { // tiny total suggests bad parse
		return script, false
	}

	return out.Bytes(), true
}

// looksBase64 returns true if the string is plausibly base64-encoded.
func looksBase64(s string) bool {
	trim := strings.TrimSpace(s)
	if len(trim) == 0 || len(trim)%4 != 0 {
		return false
	}
	for i := 0; i < len(trim); i++ {
		c := trim[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=') {
			return false
		}
	}
	return true
}

// trimToImageSignature scans for known image headers and slices from the first match.
func trimToImageSignature(b []byte) []byte {
	if len(b) < 8 {
		return b
	}
	sigs := [][]byte{
		{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, // PNG
		{0xFF, 0xD8, 0xFF},       // JPEG
		[]byte("RIFF"),           // WEBP (we also check WEBP marker)
		{0x47, 0x49, 0x46, 0x38}, // GIF
		[]byte("ftypavif"),       // AVIF (ISO BMFF)
	}
	for _, sig := range sigs {
		if idx := bytes.Index(b, sig); idx >= 0 {
			// For RIFF/WEBP, ensure WEBP marker exists.
			if sig[0] == 'R' && len(b) >= idx+12 {
				if !(b[idx+8] == 'W' && b[idx+9] == 'E' && b[idx+10] == 'B' && b[idx+11] == 'P') {
					continue
				}
			}
			if string(sig) == "ftypavif" {
				// Ensure we return from the start of the ftyp box.
				if idx >= 4 {
					return b[idx-4:]
				}
			}
			return b[idx:]
		}
	}
	return b
}

type scriptOpLocal struct {
	opcode byte
	data   []byte
	isPush bool
}

// Minimal script parser (duplicate of bitcoin.parseScriptOps) to avoid dependency loops.
// Returns ops and ok==true if the entire script was consumed successfully.
func parseScriptOpsLocal(script []byte) ([]scriptOpLocal, bool) {
	var ops []scriptOpLocal
	i := 0
	for i < len(script) {
		op := script[i]
		i++

		switch {
		case op <= 75:
			l := int(op)
			if l < 0 || i+l > len(script) {
				return ops, false
			}
			ops = append(ops, scriptOpLocal{opcode: op, data: script[i : i+l], isPush: true})
			i += l
		case op == 0x4c: // OP_PUSHDATA1
			if i >= len(script) {
				return ops, false
			}
			l := int(script[i])
			i++
			if l < 0 || i+l > len(script) {
				return ops, false
			}
			ops = append(ops, scriptOpLocal{opcode: op, data: script[i : i+l], isPush: true})
			i += l
		case op == 0x4d: // OP_PUSHDATA2
			if i+1 >= len(script) {
				return ops, false
			}
			l := int(script[i]) | int(script[i+1])<<8
			i += 2
			if l < 0 || i+l > len(script) {
				return ops, false
			}
			ops = append(ops, scriptOpLocal{opcode: op, data: script[i : i+l], isPush: true})
			i += l
		case op == 0x4e: // OP_PUSHDATA4
			if i+3 >= len(script) {
				return ops, false
			}
			l := int(script[i]) | int(script[i+1])<<8 | int(script[i+2])<<16 | int(script[i+3])<<24
			i += 4
			if l < 0 || i+l > len(script) {
				return ops, false
			}
			ops = append(ops, scriptOpLocal{opcode: op, data: script[i : i+l], isPush: true})
			i += l
		default:
			ops = append(ops, scriptOpLocal{opcode: op, isPush: false})
		}
	}
	return ops, true
}

func isMostlyPrintable(s string) bool {
	if s == "" {
		return false
	}
	printable := 0
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			printable++
			continue
		}
		if r >= 32 && r < 127 {
			printable++
		}
	}
	return printable >= len(s)/2
}

// normalizeTxID trims ordinal-style suffixes (e.g., i0) to compare canonical txids.
func normalizeTxID(txid string) string {
	if idx := strings.Index(txid, "i"); idx > 0 {
		if len(txid)-idx <= 4 { // common patterns like i0, i00
			return txid[:idx]
		}
	}
	return txid
}

func isLikelyTxID(txid string) bool {
	txid = strings.TrimSpace(normalizeTxID(strings.ToLower(txid)))
	if len(txid) != 64 {
		return false
	}
	_, err := hex.DecodeString(txid)
	return err == nil
}

// stripPushdataPrefix removes a leading push opcode (OP_PUSH, OP_PUSHDATA1/2/4) from a payload when present.
func stripPushdataPrefix(b []byte) []byte {
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

// stripNonPrintablePrefix trims leading control bytes to get to the printable payload.
func stripNonPrintablePrefix(b []byte) []byte {
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

// buildTxIndex creates a simple in-memory map from txid to block height for faster lookup.
func (api *DataAPI) buildTxIndex() {
	heights := api.listAvailableBlockHeights()
	newIndex := make(map[string]int64)
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
				newIndex[normalizeTxID(ins.TxID)] = h
			}
		}
	}
	api.txMu.Lock()
	api.txIndex = newIndex
	api.txMu.Unlock()
}

func (api *DataAPI) lookupTxHeight(txid string) (int64, bool) {
	api.txMu.RLock()
	h, ok := api.txIndex[txid]
	api.txMu.RUnlock()
	return h, ok
}

// Helper functions

func (api *DataAPI) verifySignature(secret string, body []byte, signature string) bool {
	if signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(signature)), []byte(strings.ToLower(expected)))
}

func splitPath(path string) []string {
	path = path[1:] // Remove leading slash
	if path == "" {
		return []string{}
	}
	var parts []string
	for _, part := range strings.Split(path, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func (api *DataAPI) sendSSEUpdate(w http.ResponseWriter, update *storage.RealtimeUpdate) {
	data, err := json.Marshal(update)
	if err != nil {
		log.Printf("Failed to marshal SSE update: %v", err)
		return
	}

	fmt.Fprintf(w, "data: %s\n\n", data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func (api *DataAPI) monitorUpdates(updates chan *storage.RealtimeUpdate) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Send periodic statistics update
			stats := api.dataStorage.GetSteganographyStats()
			update := api.dataStorage.CreateRealtimeUpdate("stats_update", 0, stats)
			select {
			case updates <- update:
			default:
				// Channel full, skip this update
			}
		}
	}
}

// getTransactionCount returns a best-effort transaction count.
func (api *DataAPI) getTransactionCount(blockData *storage.BlockDataCache) int {
	if blockData == nil {
		return 0
	}
	if blockData.TxCount > 0 {
		return blockData.TxCount
	}
	if len(blockData.Images) > 0 {
		return len(blockData.Images)
	}
	if len(blockData.Inscriptions) > 0 {
		return len(blockData.Inscriptions)
	}
	return 0
}
