package bitcoin

import "time"

// BlockDataCache represents cached block data with metadata
type BlockDataCache struct {
	BlockHeight          int64                    `json:"block_height"`
	BlockHash            string                   `json:"block_hash"`
	Timestamp            int64                    `json:"timestamp"`
	Inscriptions         []InscriptionData        `json:"inscriptions"`
	Images               []ExtractedImageData     `json:"images"`
	SmartContracts       []SmartContractData      `json:"smart_contracts"`
	ScanResults          []map[string]interface{} `json:"scan_results"`
	ProcessingTime       int64                    `json:"processing_time_ms"`
	Success              bool                     `json:"success"`
	CacheTimestamp       time.Time                `json:"cache_timestamp"`
	SteganographySummary *SteganographySummary    `json:"steganography_summary"`
}

// SteganographySummary represents steganography scan summary for a block
type SteganographySummary struct {
	TotalImages   int      `json:"total_images"`
	StegoDetected bool     `json:"stego_detected"`
	StegoCount    int      `json:"stego_count"`
	ScanTimestamp int64    `json:"scan_timestamp"`
	AvgConfidence float64  `json:"avg_confidence"`
	StegoTypes    []string `json:"stego_types"`
}

// RealtimeUpdate represents a real-time update message
type RealtimeUpdate struct {
	Type        string      `json:"type"` // "new_block", "scan_complete", "stego_detected"
	Timestamp   int64       `json:"timestamp"`
	BlockHeight int64       `json:"block_height,omitempty"`
	Data        interface{} `json:"data"`
}

// DataStorageInterface defines the interface for block data storage
type DataStorageInterface interface {
	StoreBlockData(blockResponse *BlockInscriptionsResponse, scanResults []map[string]interface{}) error
	GetBlockData(height int64) (interface{}, error)
	GetRecentBlocks(limit int) ([]interface{}, error)
	GetSteganographyStats() map[string]interface{}
	ValidateDataIntegrity(height int64) error
}
