package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"stargate-backend/bitcoin"
	"stargate-backend/security"
)

// DataStorage handles centralized storage and retrieval of block monitoring data
type DataStorage struct {
	dataDir      string
	mu           sync.RWMutex
	cache        map[int64]*BlockDataCache
	cacheTimeout time.Duration
}

// ExtendedDataStorage includes the core interface plus helper methods used by APIs.
type ExtendedDataStorage interface {
	bitcoin.DataStorageInterface
	CreateRealtimeUpdate(updateType string, blockHeight int64, data interface{}) *RealtimeUpdate
	ReadTextContent(height int64, filePath string) (string, error)
}

// BlockDataCache represents cached block data with metadata
type BlockDataCache struct {
	BlockHeight          int64                         `json:"block_height"`
	BlockHash            string                        `json:"block_hash"`
	Timestamp            int64                         `json:"timestamp"`
	TxCount              int                           `json:"tx_count"`
	Inscriptions         []bitcoin.InscriptionData     `json:"inscriptions"`
	Images               []bitcoin.ExtractedImageData  `json:"images"`
	SmartContracts       []bitcoin.SmartContractData   `json:"smart_contracts"`
	ScanResults          []map[string]interface{}      `json:"scan_results"`
	ProcessingTime       int64                         `json:"processing_time_ms"`
	Success              bool                          `json:"success"`
	CacheTimestamp       time.Time                     `json:"cache_timestamp"`
	SteganographySummary *bitcoin.SteganographySummary `json:"steganography_summary"`
}

// RealtimeUpdate represents a real-time update message
type RealtimeUpdate struct {
	Type        string      `json:"type"` // "new_block", "scan_complete", "stego_detected"
	Timestamp   int64       `json:"timestamp"`
	BlockHeight int64       `json:"block_height,omitempty"`
	Data        interface{} `json:"data"`
}

// NewDataStorage creates a new data storage instance
func NewDataStorage(dataDir string) *DataStorage {
	storage := &DataStorage{
		dataDir:      dataDir,
		cache:        make(map[int64]*BlockDataCache),
		cacheTimeout: 30 * time.Minute,
	}

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Printf("Failed to create data directory: %v", err)
	}

	// Migrate legacy flat block layout to partitioned early, *before* loading cache.
	// This ensures the in-memory cache and any subsequent FS walks see the new layout.
	bitcoin.MigrateOldBlockLayoutIfNeeded(dataDir)

	// Load existing cache (now from the correct layout)
	storage.loadCache()

	return storage
}

// StoreBlockData stores block monitoring results with caching
func (ds *DataStorage) StoreBlockData(blockResponse *bitcoin.BlockInscriptionsResponse, scanResults []map[string]interface{}) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Create steganography summary
	stegoSummary := ds.createSteganographySummary(blockResponse.Images, scanResults)

	// Create cache entry
	cacheEntry := &BlockDataCache{
		BlockHeight:          blockResponse.BlockHeight,
		BlockHash:            blockResponse.BlockHash,
		Timestamp:            blockResponse.Timestamp,
		TxCount:              blockResponse.TotalTransactions,
		Inscriptions:         blockResponse.Inscriptions,
		Images:               blockResponse.Images,
		SmartContracts:       blockResponse.SmartContracts,
		ScanResults:          scanResults,
		ProcessingTime:       blockResponse.ProcessingTime,
		Success:              blockResponse.Success,
		CacheTimestamp:       time.Now(),
		SteganographySummary: stegoSummary,
	}

	// Update cache
	ds.cache[blockResponse.BlockHeight] = cacheEntry

	// Save to file
	if err := ds.saveBlockDataToFile(cacheEntry); err != nil {
		log.Printf("Failed to save block data to file: %v", err)
		return err
	}

	// Clean old cache entries
	ds.cleanOldCache()

	log.Printf("Stored block data for height %d with %d images, %d stego detected",
		blockResponse.BlockHeight, len(blockResponse.Images), stegoSummary.StegoCount)

	return nil
}

// GetBlockData retrieves block data with caching.
// On cache miss or stale, loads from disk and promotes into the in-memory cache
// (so that GetRecentBlocks and scrollers can see recently accessed historical blocks).
func (ds *DataStorage) GetBlockData(height int64) (interface{}, error) {
	ds.mu.RLock()
	if cached, exists := ds.cache[height]; exists {
		if time.Since(cached.CacheTimestamp) < ds.cacheTimeout {
			cpy := *cached
			ds.mu.RUnlock()
			return &cpy, nil
		}
	}
	ds.mu.RUnlock()

	// Load from disk (outside lock)
	loaded, err := ds.loadBlockDataFromFile(height)
	if err != nil {
		return nil, err
	}

	// Promote into cache under write lock (refresh timestamp already set by loader)
	if entry, ok := loaded.(*BlockDataCache); ok {
		ds.mu.Lock()
		ds.cache[height] = entry
		ds.mu.Unlock()
	}

	return loaded, nil
}

// GetRecentBlocks retrieves recent blocks with steganography data
func (ds *DataStorage) GetRecentBlocks(limit int) ([]interface{}, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var cacheList []*BlockDataCache
	for _, cached := range ds.cache {
		cacheList = append(cacheList, cached)
	}

	sort.Slice(cacheList, func(i, j int) bool {
		return cacheList[i].BlockHeight > cacheList[j].BlockHeight
	})

	if limit > 0 && len(cacheList) > limit {
		cacheList = cacheList[:limit]
	}

	result := make([]interface{}, 0, len(cacheList))
	for _, c := range cacheList {
		result = append(result, c)
	}

	return result, nil
}

// GetSteganographyStats returns overall steganography statistics
func (ds *DataStorage) GetSteganographyStats() map[string]interface{} {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	totalBlocks := len(ds.cache)
	totalImages := 0
	totalStego := 0
	stegoTypes := make(map[string]int)

	for _, cache := range ds.cache {
		if cache.SteganographySummary != nil {
			totalImages += cache.SteganographySummary.TotalImages
			totalStego += cache.SteganographySummary.StegoCount
			for _, stegoType := range cache.SteganographySummary.StegoTypes {
				stegoTypes[stegoType]++
			}
		}
	}

	stegoDetectionRate := float64(0)
	if totalImages > 0 {
		stegoDetectionRate = float64(totalStego) / float64(totalImages) * 100
	}

	return map[string]interface{}{
		"total_blocks":         totalBlocks,
		"total_images":         totalImages,
		"total_stego_detected": totalStego,
		"stego_detection_rate": stegoDetectionRate,
		"stego_types":          stegoTypes,
		"last_updated":         time.Now().Unix(),
	}
}

// ValidateDataIntegrity checks data integrity and consistency
func (ds *DataStorage) ValidateDataIntegrity(height int64) error {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	cached, exists := ds.cache[height]
	if !exists {
		return fmt.Errorf("block data not found for height %d", height)
	}

	// Validate basic data consistency
	if cached.BlockHeight != height {
		return fmt.Errorf("height mismatch: expected %d, got %d", height, cached.BlockHeight)
	}

	if cached.BlockHash == "" {
		return fmt.Errorf("empty block hash for height %d", height)
	}

	// Validate image data consistency
	if cached.SteganographySummary != nil {
		if cached.SteganographySummary.TotalImages != len(cached.Images) {
			return fmt.Errorf("image count mismatch: summary says %d, actual %d",
				cached.SteganographySummary.TotalImages, len(cached.Images))
		}
	}

	return nil
}

// createSteganographySummary creates summary from scan results
func (ds *DataStorage) createSteganographySummary(images []bitcoin.ExtractedImageData, scanResults []map[string]interface{}) *bitcoin.SteganographySummary {
	summary := &bitcoin.SteganographySummary{
		TotalImages:   len(images),
		ScanTimestamp: time.Now().Unix(),
		StegoTypes:    []string{},
	}

	stegoCount := 0
	totalConfidence := 0.0
	stegoTypeSet := make(map[string]bool)

	for _, result := range scanResults {
		if isStego, ok := result["is_stego"].(bool); ok && isStego {
			stegoCount++
			if confidence, ok := result["confidence"].(float64); ok {
				totalConfidence += confidence
			}
			if stegoType, ok := result["stego_type"].(string); ok && stegoType != "" {
				stegoTypeSet[stegoType] = true
			}
		}
	}

	summary.StegoDetected = stegoCount > 0
	summary.StegoCount = stegoCount

	if stegoCount > 0 {
		summary.AvgConfidence = totalConfidence / float64(stegoCount)
		for stegoType := range stegoTypeSet {
			summary.StegoTypes = append(summary.StegoTypes, stegoType)
		}
	}

	return summary
}

// saveBlockDataToFile saves block data to JSON file
func (ds *DataStorage) saveBlockDataToFile(data interface{}) error {
	cacheData, ok := data.(*BlockDataCache)
	if !ok {
		return fmt.Errorf("invalid data type, expected *BlockDataCache")
	}
	// Write inside the (partitioned) block dir to avoid polluting the root.
	// Falls back gracefully if dir not found.
	dir, _ := bitcoin.FindBlockDir(ds.dataDir, cacheData.BlockHeight)
	if dir == "" {
		dir = ds.dataDir // very old fallback
	}
	filename := filepath.Join(dir, "block.json")

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal block data: %w", err)
	}

	if err := os.WriteFile(filename, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write block data file: %w", err)
	}

	return nil
}

// loadBlockDataFromFile loads block data from JSON file
func (ds *DataStorage) loadBlockDataFromFile(height int64) (interface{}, error) {
	// Use centralized finder that understands partitioned + legacy layout.
	dir, err := bitcoin.FindBlockDir(ds.dataDir, height)
	if err != nil {
		return nil, fmt.Errorf("no block data for height %d: %w", height, err)
	}

	// inscriptions.json is the summary (written with total_transactions), block.json is raw.
	candidates := []string{
		filepath.Join(dir, "inscriptions.json"),
		filepath.Join(dir, "block.json"),
	}
	var data []byte
	for _, p := range candidates {
		if b, e := os.ReadFile(p); e == nil {
			data = b
			break
		}
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("no block json found for height %d", height)
	}

	var cacheEntry BlockDataCache
	if err := json.Unmarshal(data, &cacheEntry); err != nil {
		log.Printf("Failed to unmarshal block data for height %d: %v", height, err)
		return nil, fmt.Errorf("failed to unmarshal block data: %w", err)
	}

	// The summary file uses "total_transactions"; ensure TxCount is populated.
	if cacheEntry.TxCount == 0 {
		// Try secondary unmarshal for the common key.
		var aux struct {
			TotalTransactions int `json:"total_transactions"`
		}
		if uerr := json.Unmarshal(data, &aux); uerr == nil && aux.TotalTransactions > 0 {
			cacheEntry.TxCount = aux.TotalTransactions
		}
	}

	// Set cache timestamp
	cacheEntry.CacheTimestamp = time.Now()

	return &cacheEntry, nil
}

// loadCache loads block data from blocks/[height]_[hash]/block.json files into cache
func (ds *DataStorage) loadCache() {
	// Check if blocks directory exists
	if _, err := os.Stat(ds.dataDir); err != nil {
		if os.IsNotExist(err) {
			log.Printf("Blocks directory does not exist, cache will be empty")
			return
		}
		log.Printf("Failed to check blocks directory: %v", err)
		return
	}

	// Walk the (possibly partitioned) tree to find any <height>_* dirs that have inscriptions.json.
	// This supports both legacy flat layout and the new 3-level partitioned layout.
	loadedCount := 0
	_ = filepath.WalkDir(ds.dataDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || !d.IsDir() {
			return nil
		}
		// Look for a directory whose name starts with digits_ (the leaf block dir)
		name := d.Name()
		underscore := strings.Index(name, "_")
		if underscore <= 0 {
			return nil
		}
		if _, err := strconv.ParseInt(name[:underscore], 10, 64); err != nil {
			return nil
		}

		insPath := filepath.Join(path, "inscriptions.json")
		if _, err := os.Stat(insPath); err != nil {
			return nil // not a block dir with data, or skip deeper?
		}

		blockData, err := os.ReadFile(insPath)
		if err != nil {
			log.Printf("Warning: failed to read inscriptions file %s: %v", insPath, err)
			return nil
		}

		var blockInfo struct {
			BlockHash      string `json:"block_hash"`
			BlockHeight    int64  `json:"block_height"`
			Images         []struct {
				TxID      string `json:"tx_id"`
				Format    string `json:"format"`
				SizeBytes int    `json:"size_bytes"`
				FileName  string `json:"file_name"`
				FilePath  string `json:"file_path"`
			} `json:"images"`
			SmartContracts []bitcoin.SmartContractData `json:"smart_contracts"`
		}

		if err := json.Unmarshal(blockData, &blockInfo); err != nil {
			log.Printf("Warning: failed to parse inscriptions file %s: %v", insPath, err)
			return nil
		}

		// Convert to our cache format
		contracts := blockInfo.SmartContracts
		if contracts == nil {
			contracts = make([]bitcoin.SmartContractData, 0)
		}
		cacheEntry := &BlockDataCache{
			BlockHeight:    blockInfo.BlockHeight,
			BlockHash:      blockInfo.BlockHash,
			Timestamp:      0, // Not in inscriptions.json, using 0
			Inscriptions:   make([]bitcoin.InscriptionData, len(blockInfo.Images)),
			Images:         make([]bitcoin.ExtractedImageData, len(blockInfo.Images)),
			SmartContracts: contracts,
			ScanResults:    make([]map[string]interface{}, 0),
			ProcessingTime: 0,
			Success:        true,
			CacheTimestamp: time.Now(),
			SteganographySummary: &bitcoin.SteganographySummary{
				TotalImages:   len(blockInfo.Images),
				StegoDetected: false,
				StegoCount:    0,
				ScanTimestamp: time.Now().Unix(),
				AvgConfidence: 0,
				StegoTypes:    []string{},
			},
		}

		// Convert images to inscriptions
		for i, img := range blockInfo.Images {
			cacheEntry.Inscriptions[i] = bitcoin.InscriptionData{
				TxID:        img.TxID,
				Content:     "", // Content served via /api/block-image/ endpoint
				ContentType: img.Format,
				FileName:    img.FileName,
				FilePath:    img.FilePath,
				SizeBytes:   img.SizeBytes,
			}

			cacheEntry.Images[i] = bitcoin.ExtractedImageData{
				TxID:      img.TxID,
				Format:    img.Format,
				FileName:  img.FileName,
				FilePath:  img.FilePath,
				SizeBytes: img.SizeBytes,
			}
		}

		// Store in cache
		ds.cache[blockInfo.BlockHeight] = cacheEntry
		loadedCount++
		return nil
	})

	log.Printf("Loaded %d blocks into cache from blocks/ directory", loadedCount)
}

// cleanOldCache removes expired cache entries but keeps a large number of blocks
// (historical + recent) so that GetRecentBlocks and block scrollback continue to
// work after cacheTimeout. The on-disk artifacts are the source of truth; the map
// is just a hot cache. We keep up to 100k to avoid unbounded growth in extreme cases.
func (ds *DataStorage) cleanOldCache() {
	now := time.Now()

	// Collect all heights and sort by cache timestamp
	type cacheEntry struct {
		height    int64
		timestamp time.Time
	}
	var entries []cacheEntry
	for height, cached := range ds.cache {
		entries = append(entries, cacheEntry{height, cached.CacheTimestamp})
	}

	// Sort by timestamp (newest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].timestamp.After(entries[j].timestamp)
	})

	// Keep a large number (supports scrollback over historical scanned blocks).
	// Only evict beyond this if they are also expired.
	keepCount := 100000
	if len(entries) < keepCount {
		keepCount = len(entries)
	}

	removed := 0
	for i := keepCount; i < len(entries); i++ {
		if now.Sub(entries[i].timestamp) > ds.cacheTimeout {
			delete(ds.cache, entries[i].height)
			removed++
		}
	}
	if removed > 0 {
		log.Printf("cleanOldCache: removed %d expired blocks beyond keep window", removed)
	}

	// Light logging for the kept head (avoid spam)
	if len(entries) > 0 {
		kept := entries[:keepCount]
		// Only log occasionally or for the very newest
		if now.Sub(kept[0].timestamp) < ds.cacheTimeout {
			log.Printf("Keeping %d blocks in cache (newest age: %v min)", len(kept), now.Sub(kept[0].timestamp).Minutes())
		}
	}
}

// ReadTextContent reads the content of a text file
func (ds *DataStorage) ReadTextContent(height int64, filePath string) (string, error) {
	blockDir := filepath.Join(ds.dataDir, fmt.Sprintf("%d_00000000", height))
	safePath, err := security.SanitizePath(blockDir, filePath)
	if err != nil {
		return "", fmt.Errorf("invalid file path: %w", err)
	}

	content, err := os.ReadFile(safePath)
	if err != nil {
		return "", fmt.Errorf("failed to read text file %s: %w", safePath, err)
	}

	return string(content), nil
}

// CreateRealtimeUpdate creates a real-time update message
func (ds *DataStorage) CreateRealtimeUpdate(updateType string, blockHeight int64, data interface{}) *RealtimeUpdate {
	return &RealtimeUpdate{
		Type:        updateType,
		Timestamp:   time.Now().Unix(),
		BlockHeight: blockHeight,
		Data:        data,
	}
}
