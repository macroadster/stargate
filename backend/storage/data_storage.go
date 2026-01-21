package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
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

	// Load existing cache
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

// GetBlockData retrieves block data with caching
func (ds *DataStorage) GetBlockData(height int64) (interface{}, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	// Check cache first
	if cached, exists := ds.cache[height]; exists {
		if time.Since(cached.CacheTimestamp) < ds.cacheTimeout {
			return cached, nil
		}
		// Cache expired, remove it
		delete(ds.cache, height)
	}

	// Load from file
	return ds.loadBlockDataFromFile(height)
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
	filename := filepath.Join(ds.dataDir, fmt.Sprintf("block_%d.json", cacheData.BlockHeight))

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
	// Try blocks directory structure (height_hash/inscriptions.json)
	blockDirPattern := filepath.Join(ds.dataDir, fmt.Sprintf("%d_00000000", height))
	inscriptionsPath := filepath.Join(blockDirPattern, "inscriptions.json")

	data, err := os.ReadFile(inscriptionsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read block data file: %w", err)
	}

	var cacheEntry BlockDataCache
	if err := json.Unmarshal(data, &cacheEntry); err != nil {
		log.Printf("Failed to unmarshal block data for height %d: %v", height, err)
		return nil, fmt.Errorf("failed to unmarshal block data: %w", err)
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

	blockEntries, err := os.ReadDir(ds.dataDir)
	if err != nil {
		log.Printf("Failed to read blocks directory: %v", err)
		return
	}

	loadedCount := 0
	for _, blockEntry := range blockEntries {
		if !blockEntry.IsDir() {
			continue
		}

		// Try to load inscriptions.json from this directory
		inscriptionsJsonPath := filepath.Join(ds.dataDir, blockEntry.Name(), "inscriptions.json")
		if _, err := os.Stat(inscriptionsJsonPath); err != nil {
			continue // Skip if inscriptions.json doesn't exist
		}

		// Read and parse the inscriptions.json file
		blockData, err := os.ReadFile(inscriptionsJsonPath)
		if err != nil {
			log.Printf("Warning: failed to read inscriptions file %s: %v", inscriptionsJsonPath, err)
			continue
		}

		var blockInfo struct {
			BlockHash   string `json:"block_hash"`
			BlockHeight int64  `json:"block_height"`
			Images      []struct {
				TxID      string `json:"tx_id"`
				Format    string `json:"format"`
				SizeBytes int    `json:"size_bytes"`
				FileName  string `json:"file_name"`
				FilePath  string `json:"file_path"`
			} `json:"images"`
		}

		if err := json.Unmarshal(blockData, &blockInfo); err != nil {
			log.Printf("Warning: failed to parse inscriptions file %s: %v", inscriptionsJsonPath, err)
			continue
		}

		// Convert to our cache format
		cacheEntry := &BlockDataCache{
			BlockHeight:    blockInfo.BlockHeight,
			BlockHash:      blockInfo.BlockHash,
			Timestamp:      0, // Not in inscriptions.json, using 0
			Inscriptions:   make([]bitcoin.InscriptionData, len(blockInfo.Images)),
			Images:         make([]bitcoin.ExtractedImageData, len(blockInfo.Images)),
			SmartContracts: make([]bitcoin.SmartContractData, 0),
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
	}

	log.Printf("Loaded %d blocks into cache from blocks/ directory", loadedCount)
}

// cleanOldCache removes expired cache entries but keeps at least 10 recent blocks
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

	// Keep at least 10 most recent blocks, remove expired ones beyond that
	keepCount := 10
	if len(entries) < keepCount {
		keepCount = len(entries)
	}

	// Remove expired entries beyond the keep count
	for i := keepCount; i < len(entries); i++ {
		if now.Sub(entries[i].timestamp) > ds.cacheTimeout {
			delete(ds.cache, entries[i].height)
			log.Printf("Removed expired block %d from cache (older than %v minutes)", entries[i].height, ds.cacheTimeout.Minutes())
		}
	}

	// Keep recent blocks even if expired (if we have less than 10 total)
	for i := 0; i < keepCount; i++ {
		if now.Sub(entries[i].timestamp) > ds.cacheTimeout {
			// Refresh expired but keep it if we don't have enough blocks
			log.Printf("Block %d expired but keeping in cache (only %d blocks cached)", entries[i].height, len(entries))
		} else {
			// Keep non-expired blocks
			log.Printf("Keeping block %d in cache (age: %v minutes)", entries[i].height, now.Sub(entries[i].timestamp).Minutes())
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
