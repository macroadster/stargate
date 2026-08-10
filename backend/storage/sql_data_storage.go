package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"stargate-backend/bitcoin"
	"stargate-backend/storage/gormdb"
)

// BlockScanRow is the GORM model for block_scans (SQLite text payload + Postgres JSON/text).
// Payload is the marshaled BlockDataCache JSON used by the UI and search APIs.
type BlockScanRow struct {
	// autoIncrement:false — block height is an explicit chain height, not a rowid sequence.
	BlockHeight   int64          `gorm:"column:block_height;primaryKey;autoIncrement:false"`
	BlockHash     string         `gorm:"column:block_hash;not null;index:idx_block_scans_hash"`
	ScannedAt     gormdb.SQLTime `gorm:"column:scanned_at;index:idx_block_scans_scanned"`
	Payload       []byte         `gorm:"column:payload;type:text;not null"`
	StegoDetected int            `gorm:"column:stego_detected;not null;default:0"`
	ImagesScanned int            `gorm:"column:images_scanned;not null;default:0"`
}

// TableName pins the historical table name used by both dialects.
func (BlockScanRow) TableName() string { return "block_scans" }

// SQLDataStorage is the unified GORM-backed block metadata store for SQLite and Postgres.
// Filesystem layout for images remains separate (DataStorage / blocks/ tree).
type SQLDataStorage struct {
	db      *gorm.DB
	dialect gormdb.Dialect
}

// Compile-time: implements ExtendedDataStorage.
var _ ExtendedDataStorage = (*SQLDataStorage)(nil)

// NewSQLDataStorage wraps an existing GORM connection and ensures schema.
func NewSQLDataStorage(db *gorm.DB) (*SQLDataStorage, error) {
	if db == nil {
		return nil, fmt.Errorf("storage: nil gorm DB for block_scans")
	}
	s := &SQLDataStorage{db: db, dialect: gormdb.DialectOf(db)}
	if err := s.ensureSchema(); err != nil {
		return nil, err
	}
	return s, nil
}

// NewSQLiteDataStorage opens pure-Go SQLite block_scans storage.
func NewSQLiteDataStorage(dbPath string) (*SQLDataStorage, error) {
	if dbPath == "" {
		dbPath = DefaultPath("blocks.db")
	}
	if dir := filepath.Dir(dbPath); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}
	db, err := gormdb.OpenSQLite(dbPath)
	if err != nil {
		return nil, err
	}
	s, err := NewSQLDataStorage(db)
	if err != nil {
		_ = gormdb.Close(db)
		return nil, err
	}
	log.Printf("SQLDataStorage (sqlite) ready: %s", dbPath)
	return s, nil
}

// NewPostgresStorage opens Postgres block_scans storage (constructor name preserved).
func NewPostgresStorage(dsn string) (*SQLDataStorage, error) {
	if dsn == "" {
		return nil, fmt.Errorf("empty DSN for Postgres storage")
	}
	db, err := gormdb.OpenPostgres(dsn)
	if err != nil {
		return nil, err
	}
	s, err := NewSQLDataStorage(db)
	if err != nil {
		_ = gormdb.Close(db)
		return nil, err
	}
	log.Printf("SQLDataStorage (postgres) ready")
	return s, nil
}

func (s *SQLDataStorage) ensureSchema() error {
	// AutoMigrate keeps columns aligned; indexes match historical names where possible.
	if err := s.db.AutoMigrate(&BlockScanRow{}); err != nil {
		return fmt.Errorf("block_scans migrate: %w", err)
	}
	// Extra indexes (IF NOT EXISTS) — harmless if AutoMigrate already created them.
	_ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_block_scans_hash ON block_scans (block_hash)`).Error
	_ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_block_scans_scanned ON block_scans (scanned_at)`).Error
	return nil
}

// StoreBlockData persists a block scan result (upsert by height).
func (s *SQLDataStorage) StoreBlockData(blockResponse *bitcoin.BlockInscriptionsResponse, scanResults []map[string]interface{}) error {
	cacheEntry := &BlockDataCache{
		BlockHeight:          blockResponse.BlockHeight,
		BlockHash:            blockResponse.BlockHash,
		Timestamp:            blockResponse.Timestamp,
		TxCount:              blockResponse.TotalTransactions,
		Inscriptions:         sanitizeInscriptions(blockResponse.Inscriptions),
		Images:               blockResponse.Images,
		SmartContracts:       blockResponse.SmartContracts,
		ScanResults:          scanResults,
		ProcessingTime:       blockResponse.ProcessingTime,
		Success:              blockResponse.Success,
		CacheTimestamp:       time.Now(),
		SteganographySummary: createSteganographySummary(blockResponse.Images, scanResults),
	}

	payload, err := json.Marshal(cacheEntry)
	if err != nil {
		return fmt.Errorf("marshal block payload: %w", err)
	}

	stegoDetected := 0
	if cacheEntry.SteganographySummary != nil && cacheEntry.SteganographySummary.StegoDetected {
		stegoDetected = cacheEntry.SteganographySummary.StegoCount
	}

	row := BlockScanRow{
		BlockHeight:   blockResponse.BlockHeight,
		BlockHash:     blockResponse.BlockHash,
		ScannedAt:     gormdb.NewSQLTime(time.Now().UTC()),
		Payload:       payload,
		StegoDetected: stegoDetected,
		ImagesScanned: len(blockResponse.Images),
	}

	err = s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "block_height"}},
		DoUpdates: clause.AssignmentColumns([]string{"block_hash", "payload", "stego_detected", "images_scanned", "scanned_at"}),
	}).Create(&row).Error
	if err != nil {
		_ = os.WriteFile(fmt.Sprintf("/tmp/failed_payload_%d.json", blockResponse.BlockHeight), payload, 0644)
		log.Printf("warning: failed to upsert block data for %d: %v", blockResponse.BlockHeight, err)
		// Match prior behavior: do not fail the scan pipeline.
		return nil
	}
	return nil
}

// GetBlockData retrieves block data by height.
func (s *SQLDataStorage) GetBlockData(height int64) (interface{}, error) {
	var row BlockScanRow
	if err := s.db.Select("payload").Where("block_height = ?", height).First(&row).Error; err != nil {
		return nil, fmt.Errorf("block %d not found: %w", height, err)
	}
	var cacheEntry BlockDataCache
	if err := json.Unmarshal(row.Payload, &cacheEntry); err != nil {
		return nil, fmt.Errorf("unmarshal block payload: %w", err)
	}
	cacheEntry.CacheTimestamp = time.Now()
	return &cacheEntry, nil
}

// GetRecentBlocks retrieves the most recent blocks by height.
func (s *SQLDataStorage) GetRecentBlocks(limit int) ([]interface{}, error) {
	if limit <= 0 {
		limit = 10
	}
	var rows []BlockScanRow
	if err := s.db.Select("payload").
		Order("block_height DESC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query recent blocks: %w", err)
	}
	out := make([]interface{}, 0, len(rows))
	for _, row := range rows {
		var cacheEntry BlockDataCache
		if err := json.Unmarshal(row.Payload, &cacheEntry); err != nil {
			log.Printf("failed to unmarshal block payload: %v", err)
			continue
		}
		cacheEntry.CacheTimestamp = time.Now()
		out = append(out, &cacheEntry)
	}
	return out, nil
}

// GetMaxBlockHeight returns the highest stored block height.
func (s *SQLDataStorage) GetMaxBlockHeight() (int64, error) {
	var maxH *int64
	err := s.db.Model(&BlockScanRow{}).Select("MAX(block_height)").Scan(&maxH).Error
	if err != nil {
		return 0, err
	}
	if maxH == nil {
		return 0, nil
	}
	return *maxH, nil
}

// GetSteganographyStats returns aggregate stats from cached columns.
func (s *SQLDataStorage) GetSteganographyStats() map[string]interface{} {
	stats := map[string]interface{}{
		"total_blocks":         0,
		"total_images":         0,
		"total_stego_detected": 0,
		"stego_detection_rate": 0.0,
		"stego_types":          map[string]int{},
		"last_updated":         time.Now().Unix(),
	}

	type agg struct {
		Cnt         int64
		ImagesSum   int64
		StegoBlocks int64
	}
	var a agg
	err := s.db.Model(&BlockScanRow{}).
		Select(`COUNT(*) AS cnt,
			COALESCE(SUM(images_scanned),0) AS images_sum,
			COALESCE(SUM(CASE WHEN stego_detected > 0 THEN 1 ELSE 0 END),0) AS stego_blocks`).
		Scan(&a).Error
	if err != nil {
		log.Printf("failed to compute stego stats: %v", err)
		return stats
	}
	stats["total_blocks"] = a.Cnt
	stats["total_images"] = a.ImagesSum
	stats["total_stego_detected"] = a.StegoBlocks
	if a.ImagesSum > 0 {
		stats["stego_detection_rate"] = float64(a.StegoBlocks) / float64(a.ImagesSum) * 100.0
	}
	return stats
}

// ValidateDataIntegrity checks that a height exists.
func (s *SQLDataStorage) ValidateDataIntegrity(height int64) error {
	_, err := s.GetBlockData(height)
	return err
}

// CreateRealtimeUpdate builds a realtime update message (no persistence).
func (s *SQLDataStorage) CreateRealtimeUpdate(updateType string, blockHeight int64, data interface{}) *RealtimeUpdate {
	return &RealtimeUpdate{
		Type:        updateType,
		Timestamp:   time.Now().Unix(),
		BlockHeight: blockHeight,
		Data:        data,
	}
}

// ReadTextContent is not implemented for SQL metadata storage (images stay on FS).
func (s *SQLDataStorage) ReadTextContent(height int64, filePath string) (string, error) {
	return "", fmt.Errorf("ReadTextContent not supported on SQLDataStorage (filesystem blocks/ layout still used for text/images)")
}

// Close releases the underlying pool.
func (s *SQLDataStorage) Close() error {
	return gormdb.Close(s.db)
}

// sanitizeInscriptions removes inline content to avoid oversized JSON payloads.
func sanitizeInscriptions(inscriptions []bitcoin.InscriptionData) []bitcoin.InscriptionData {
	out := make([]bitcoin.InscriptionData, len(inscriptions))
	for i, ins := range inscriptions {
		ins.Content = ""
		out[i] = ins
	}
	return out
}

// createSteganographySummary mirrors the filesystem storage logic.
func createSteganographySummary(images []bitcoin.ExtractedImageData, scanResults []map[string]interface{}) *bitcoin.SteganographySummary {
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
