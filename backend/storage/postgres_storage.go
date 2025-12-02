package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"stargate-backend/bitcoin"
)

// PostgresStorage implements DataStorageInterface using a Postgres JSONB table.
type PostgresStorage struct {
	db        *sql.DB
	tableName string
}

// NewPostgresStorage creates a Postgres-backed storage implementation.
// Expects dsn like: postgres://user:pass@host:5432/dbname?sslmode=disable
func NewPostgresStorage(dsn string) (*PostgresStorage, error) {
	if dsn == "" {
		return nil, fmt.Errorf("empty DSN for Postgres storage")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open Postgres connection: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	ps := &PostgresStorage{
		db:        db,
		tableName: "block_scans",
	}

	if err := ps.ensureSchema(context.Background()); err != nil {
		return nil, err
	}

	return ps, nil
}

func (ps *PostgresStorage) ensureSchema(ctx context.Context) error {
	schema := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    block_height   BIGINT PRIMARY KEY,
    block_hash     TEXT NOT NULL,
    scanned_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    payload        JSONB NOT NULL,
    stego_detected INT NOT NULL,
    images_scanned INT NOT NULL
);
CREATE INDEX IF NOT EXISTS %s_block_hash_idx ON %s (block_hash);
CREATE INDEX IF NOT EXISTS %s_scanned_at_idx ON %s (scanned_at);
CREATE INDEX IF NOT EXISTS %s_payload_idx ON %s USING GIN (payload jsonb_path_ops);
`, ps.tableName, ps.tableName, ps.tableName, ps.tableName, ps.tableName, ps.tableName, ps.tableName)

	if _, err := ps.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("failed to ensure schema: %w", err)
	}
	return nil
}

// StoreBlockData persists a block scan result.
func (ps *PostgresStorage) StoreBlockData(blockResponse *bitcoin.BlockInscriptionsResponse, scanResults []map[string]interface{}) error {
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
		SteganographySummary: createSteganographySummary(blockResponse.Images, scanResults),
	}

	payload, err := json.Marshal(cacheEntry)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	stegoDetected := 0
	if cacheEntry.SteganographySummary != nil && cacheEntry.SteganographySummary.StegoDetected {
		stegoDetected = cacheEntry.SteganographySummary.StegoCount
	}

	imagesScanned := len(blockResponse.Images)

	query := fmt.Sprintf(`
INSERT INTO %s (block_height, block_hash, payload, stego_detected, images_scanned, scanned_at)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (block_height) DO UPDATE SET
  block_hash = EXCLUDED.block_hash,
  payload = EXCLUDED.payload,
  stego_detected = EXCLUDED.stego_detected,
  images_scanned = EXCLUDED.images_scanned,
  scanned_at = EXCLUDED.scanned_at;
`, ps.tableName)

	if _, err := ps.db.Exec(query, blockResponse.BlockHeight, blockResponse.BlockHash, payload, stegoDetected, imagesScanned); err != nil {
		return fmt.Errorf("failed to upsert block data: %w", err)
	}

	return nil
}

// GetBlockData retrieves block data by height.
func (ps *PostgresStorage) GetBlockData(height int64) (interface{}, error) {
	query := fmt.Sprintf(`SELECT payload FROM %s WHERE block_height = $1`, ps.tableName)

	var payload []byte
	if err := ps.db.QueryRow(query, height).Scan(&payload); err != nil {
		return nil, fmt.Errorf("block %d not found: %w", height, err)
	}

	var cacheEntry BlockDataCache
	if err := json.Unmarshal(payload, &cacheEntry); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}
	cacheEntry.CacheTimestamp = time.Now()
	return &cacheEntry, nil
}

// GetRecentBlocks retrieves the most recent blocks.
func (ps *PostgresStorage) GetRecentBlocks(limit int) ([]interface{}, error) {
	// Order by block height so UI sees canonical chain order, falling back to scan time.
	query := fmt.Sprintf(`SELECT payload FROM %s ORDER BY block_height DESC, scanned_at DESC LIMIT $1`, ps.tableName)
	rows, err := ps.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent blocks: %w", err)
	}
	defer rows.Close()

	var result []interface{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		var cacheEntry BlockDataCache
		if err := json.Unmarshal(payload, &cacheEntry); err != nil {
			log.Printf("failed to unmarshal block payload: %v", err)
			continue
		}
		cacheEntry.CacheTimestamp = time.Now()
		result = append(result, &cacheEntry)
	}
	return result, nil
}

// GetSteganographyStats returns aggregate stats from cached columns.
func (ps *PostgresStorage) GetSteganographyStats() map[string]interface{} {
	stats := map[string]interface{}{
		"total_blocks":         0,
		"total_images":         0,
		"total_stego_detected": 0,
		"stego_detection_rate": 0.0,
		"stego_types":          map[string]int{},
		"last_updated":         time.Now().Unix(),
	}

	query := fmt.Sprintf(`SELECT COUNT(*) AS cnt, COALESCE(SUM(images_scanned),0) AS images_sum, COALESCE(SUM(CASE WHEN stego_detected > 0 THEN 1 ELSE 0 END),0) AS stego_blocks FROM %s`, ps.tableName)
	var cnt, imagesSum, stegoBlocks int64
	if err := ps.db.QueryRow(query).Scan(&cnt, &imagesSum, &stegoBlocks); err != nil {
		log.Printf("failed to compute stats: %v", err)
		return stats
	}

	stats["total_blocks"] = cnt
	stats["total_images"] = imagesSum
	stats["total_stego_detected"] = stegoBlocks
	if imagesSum > 0 {
		stats["stego_detection_rate"] = float64(stegoBlocks) / float64(imagesSum) * 100.0
	}
	return stats
}

// ValidateDataIntegrity performs a lightweight check (presence in DB).
func (ps *PostgresStorage) ValidateDataIntegrity(height int64) error {
	_, err := ps.GetBlockData(height)
	return err
}

// CreateRealtimeUpdate builds a realtime update message (no persistence).
func (ps *PostgresStorage) CreateRealtimeUpdate(updateType string, blockHeight int64, data interface{}) *RealtimeUpdate {
	return &RealtimeUpdate{
		Type:        updateType,
		Timestamp:   time.Now().Unix(),
		BlockHeight: blockHeight,
		Data:        data,
	}
}

// ReadTextContent is not implemented for Postgres storage (no local files).
func (ps *PostgresStorage) ReadTextContent(height int64, filePath string) (string, error) {
	return "", fmt.Errorf("ReadTextContent not supported in Postgres storage")
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
