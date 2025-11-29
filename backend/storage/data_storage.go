package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"stargate-backend/bitcoin"
)

// DataStorage handles centralized storage and retrieval of block monitoring data
type DataStorage struct {
	dataDir      string
	mu           sync.RWMutex
	cache        map[int64]*BlockDataCache
	cacheTimeout time.Duration
}

// BlockDataCache represents cached block data with metadata
type BlockDataCache struct {
	BlockHeight          int64                         `json:"block_height"`
	BlockHash            string                        `json:"block_hash"`
	Timestamp            int64                         `json:"timestamp"`
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

	var recentBlocks []interface{}

	// Get sorted block heights from cache
	var heights []int64
	for height := range ds.cache {
		heights = append(heights, height)
	}

	// Simple sort (could be optimized for large datasets)
	for i := 0; i < len(heights); i++ {
		for j := i + 1; j < len(heights); j++ {
			if heights[i] < heights[j] {
				heights[i], heights[j] = heights[j], heights[i]
			}
		}
	}

	// Take the most recent blocks
	count := len(heights)
	if count > limit {
		count = limit
	}

	for i := 0; i < count; i++ {
		if cached, exists := ds.cache[heights[i]]; exists {
			recentBlocks = append(recentBlocks, cached)
		}
	}

	return recentBlocks, nil
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
	filename := filepath.Join(ds.dataDir, fmt.Sprintf("block_%d.json", height))

	data, err := os.ReadFile(filename)
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

// loadCache loads existing cache from files
func (ds *DataStorage) loadCache() {
	files, err := os.ReadDir(ds.dataDir)
	if err != nil {
		log.Printf("Failed to read data directory: %v", err)
		return
	}

	for _, file := range files {
		if !file.IsDir() && len(file.Name()) > 6 && file.Name()[:6] == "block_" {
			var height int64
			if _, err := fmt.Sscanf(file.Name(), "block_%d.json", &height); err == nil {
				if data, err := ds.loadBlockDataFromFile(height); err == nil {
					cacheData, ok := data.(*BlockDataCache)
					if ok {
						ds.cache[height] = cacheData
					}
				}
			}
		}
	}

	log.Printf("Loaded %d block data entries into cache", len(ds.cache))
}

// cleanOldCache removes expired cache entries
func (ds *DataStorage) cleanOldCache() {
	now := time.Now()
	for height, cached := range ds.cache {
		if now.Sub(cached.CacheTimestamp) > ds.cacheTimeout {
			delete(ds.cache, height)
		}
	}
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
