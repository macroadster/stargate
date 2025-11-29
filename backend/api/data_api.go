package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"stargate-backend/bitcoin"
	"stargate-backend/storage"
)

// DataAPI handles enhanced API endpoints for block monitoring data
type DataAPI struct {
	dataStorage  *storage.DataStorage
	blockMonitor *bitcoin.BlockMonitor
	bitcoinAPI   *bitcoin.BitcoinAPI
}

// NewDataAPI creates a new data API instance
func NewDataAPI(dataStorage *storage.DataStorage, blockMonitor *bitcoin.BlockMonitor, bitcoinAPI *bitcoin.BitcoinAPI) *DataAPI {
	return &DataAPI{
		dataStorage:  dataStorage,
		blockMonitor: blockMonitor,
		bitcoinAPI:   bitcoinAPI,
	}
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

	// Extract block height from URL
	pathParts := splitPath(r.URL.Path)
	if len(pathParts) < 3 {
		http.Error(w, "Invalid URL format", http.StatusBadRequest)
		return
	}

	heightStr := pathParts[2]
	height, err := strconv.ParseInt(heightStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid block height", http.StatusBadRequest)
		return
	}

	// Get block data from storage
	blockData, err := api.dataStorage.GetBlockData(height)
	if err != nil {
		http.Error(w, "Block data not found", http.StatusNotFound)
		return
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
		http.Error(w, "Failed to get recent blocks", http.StatusInternalServerError)
		return
	}

	// Create response
	response := map[string]interface{}{
		"blocks":       recentBlocks,
		"total":        len(recentBlocks),
		"limit":        limit,
		"last_updated": time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
		ForceScan   bool  `json:"force_scan"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON request", http.StatusBadRequest)
		return
	}

	// Check if we already have data for this block
	if !request.ForceScan {
		if existingData, err := api.dataStorage.GetBlockData(request.BlockHeight); err == nil {
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
	log.Printf("On-demand scan requested for block %d", request.BlockHeight)
	err := api.blockMonitor.ProcessBlock(request.BlockHeight)
	if err != nil {
		http.Error(w, "Failed to scan block: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get the processed data
	blockData, err := api.dataStorage.GetBlockData(request.BlockHeight)
	if err != nil {
		http.Error(w, "Failed to retrieve processed block data", http.StatusInternalServerError)
		return
	}

	// Create real-time update
	update := api.dataStorage.CreateRealtimeUpdate("scan_complete", request.BlockHeight, blockData)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"cached":     false,
		"block_data": blockData,
		"message":    "Block scanned successfully",
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
	cacheData, ok := blockData.(*bitcoin.BlockDataCache)
	if !ok {
		http.Error(w, "Invalid block data type", http.StatusInternalServerError)
		return
	}

	// Enhance image data with scan results
	var enhancedImages []map[string]interface{}
	for i, image := range cacheData.Images {
		enhancedImage := map[string]interface{}{
			"tx_id":        image.TxID,
			"file_name":    image.FileName,
			"file_path":    image.FilePath,
			"size_bytes":   image.SizeBytes,
			"format":       image.Format,
			"content_type": "image/" + image.Format,
			"index":        i,
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

// Helper functions

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
