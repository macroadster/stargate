//go:build gms_pure_go
// +build gms_pure_go

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"time"

	_ "github.com/dolthub/driver"
	"stargate-backend/bitcoin"
)

var doltTablePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

type DoltStorage struct {
	db        *sql.DB
	dbPath    string
	tableName string
}

func isValidDoltTableName(name string) bool {
	return doltTablePattern.MatchString(name)
}

func NewDoltStorage(ctx context.Context, dbPath string, commitName string, commitEmail string) (*DoltStorage, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("empty path for Dolt storage")
	}

	if err := os.MkdirAll(dbPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create Dolt database directory: %w", err)
	}

	if err := initDoltDatabaseDir(dbPath, commitName, commitEmail); err != nil {
		log.Printf("Warning: database init warning: %v (continuing anyway)", err)
	}

	doltRootDir := filepath.Join(dbPath, "stargate")
	dsn := fmt.Sprintf("file://%s?commitname=%s&commitemail=%s&database=stargate", doltRootDir,
		urlEncode(commitName), urlEncode(commitEmail))

	db, err := sql.Open("dolt", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open Dolt database: %w", err)
	}

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(1 * time.Hour)
	db.SetConnMaxIdleTime(30 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping Dolt database: %w", err)
	}

	if _, err := db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS stargate"); err != nil {
		log.Printf("Warning: failed to create database: %v", err)
	}

	ds := &DoltStorage{
		db:     db,
		dbPath: dbPath,
	}
	ds.tableName = "block_scans"
	if !isValidDoltTableName(ds.tableName) {
		return nil, fmt.Errorf("invalid dolt table name: %s", ds.tableName)
	}

	if err := ds.ensureSchema(ctx); err != nil {
		return nil, err
	}

	return ds, nil
}

func initDoltDatabaseDir(dbPath, commitName, commitEmail string) error {
	dbDir := filepath.Join(dbPath, "stargate")

	if _, err := os.Stat(dbDir); err == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Join(dbDir, ".dolt"), 0755); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	_ = commitName
	_ = commitEmail
	_ = time.Now()

	log.Printf("Initialized Dolt database at %s", dbDir)
	return nil
}

func urlEncode(s string) string {
	result := ""
	for _, c := range s {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
			result += string(c)
		} else if c == ' ' {
			result += "%20"
		} else {
			result += fmt.Sprintf("%%%02X", c)
		}
	}
	return result
}

func (ds *DoltStorage) ensureSchema(ctx context.Context) error {
	schema := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    block_height   BIGINT PRIMARY KEY,
    block_hash     TEXT NOT NULL,
    scanned_at     TIMESTAMP NOT NULL DEFAULT now(),
    payload        TEXT NOT NULL,
    stego_detected INT NOT NULL,
    images_scanned INT NOT NULL
);
`, ds.tableName)

	if _, err := ds.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("failed to ensure schema: %w", err)
	}

	indexes := []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s_scanned_at_idx ON %s (scanned_at)", ds.tableName, ds.tableName),
	}

	for _, idx := range indexes {
		if _, err := ds.db.ExecContext(ctx, idx); err != nil {
			log.Printf("Warning: failed to create index: %v", err)
		}
	}

	return nil
}

func sanitizeInscriptionsForDolt(inscriptions []bitcoin.InscriptionData) []bitcoin.InscriptionData {
	out := make([]bitcoin.InscriptionData, len(inscriptions))
	for i, ins := range inscriptions {
		ins.Content = ""
		out[i] = ins
	}
	return out
}

func (ds *DoltStorage) StoreBlockData(blockResponse *bitcoin.BlockInscriptionsResponse, scanResults []map[string]interface{}) error {
	cacheEntry := &BlockDataCache{
		BlockHeight:          blockResponse.BlockHeight,
		BlockHash:            blockResponse.BlockHash,
		Timestamp:            blockResponse.Timestamp,
		TxCount:              blockResponse.TotalTransactions,
		Inscriptions:         sanitizeInscriptionsForDolt(blockResponse.Inscriptions),
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
VALUES (?, ?, ?, ?, ?, NOW())
ON DUPLICATE KEY UPDATE
  block_hash = VALUES(block_hash),
  payload = VALUES(payload),
  stego_detected = VALUES(stego_detected),
  images_scanned = VALUES(images_scanned),
  scanned_at = VALUES(scanned_at)
`, ds.tableName)

	if _, err := ds.db.Exec(query, blockResponse.BlockHeight, blockResponse.BlockHash, string(payload), stegoDetected, imagesScanned); err != nil {
		_ = os.WriteFile(fmt.Sprintf("/tmp/failed_payload_%d.json", blockResponse.BlockHeight), payload, 0644)
		log.Printf("warning: failed to upsert block data for %d: %v", blockResponse.BlockHeight, err)
		return nil
	}

	return nil
}

func (ds *DoltStorage) GetBlockData(height int64) (interface{}, error) {
	query := fmt.Sprintf(`SELECT payload FROM %s WHERE block_height = ?`, ds.tableName)

	var payload string
	if err := ds.db.QueryRow(query, height).Scan(&payload); err != nil {
		return nil, fmt.Errorf("block %d not found: %w", height, err)
	}

	var cacheEntry BlockDataCache
	if err := json.Unmarshal([]byte(payload), &cacheEntry); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}
	cacheEntry.CacheTimestamp = time.Now()
	return &cacheEntry, nil
}

func (ds *DoltStorage) GetRecentBlocks(limit int) ([]interface{}, error) {
	query := fmt.Sprintf(`SELECT payload FROM %s ORDER BY block_height DESC, scanned_at DESC LIMIT ?`, ds.tableName)
	rows, err := ds.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent blocks: %w", err)
	}
	defer rows.Close()

	var result []interface{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		var cacheEntry BlockDataCache
		if err := json.Unmarshal([]byte(payload), &cacheEntry); err != nil {
			log.Printf("failed to unmarshal block payload: %v", err)
			continue
		}
		cacheEntry.CacheTimestamp = time.Now()
		result = append(result, &cacheEntry)
	}
	return result, nil
}

func (ds *DoltStorage) GetSteganographyStats() map[string]interface{} {
	stats := map[string]interface{}{
		"total_blocks":          0,
		"total_images":          0,
		"total_stego_detected":   0,
		"stego_detection_rate": 0.0,
		"stego_types":         map[string]int{},
		"last_updated":        time.Now().Unix(),
	}

	query := fmt.Sprintf(`SELECT COUNT(*) AS cnt, COALESCE(SUM(images_scanned),0) AS images_sum, COALESCE(SUM(CASE WHEN stego_detected > 0 THEN 1 ELSE 0 END),0) AS stego_blocks FROM %s`, ds.tableName)
	var cnt, imagesSum, stegoBlocks int64
	if err := ds.db.QueryRow(query).Scan(&cnt, &imagesSum, &stegoBlocks); err != nil {
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

func (ds *DoltStorage) ValidateDataIntegrity(height int64) error {
	_, err := ds.GetBlockData(height)
	return err
}

func (ds *DoltStorage) CreateRealtimeUpdate(updateType string, blockHeight int64, data interface{}) *RealtimeUpdate {
	return &RealtimeUpdate{
		Type:        updateType,
		Timestamp:   time.Now().Unix(),
		BlockHeight: blockHeight,
		Data:        data,
	}
}

func (ds *DoltStorage) ReadTextContent(height int64, filePath string) (string, error) {
	return "", fmt.Errorf("ReadTextContent not supported in Dolt storage")
}

func (ds *DoltStorage) Commit(ctx context.Context, message string) error {
	_, err := ds.db.ExecContext(ctx, fmt.Sprintf("CALL DOLT_COMMIT('-am', '%s')", message))
	if err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}
	log.Printf("Committed changes to Dolt database: %s", message)
	return nil
}

func (ds *DoltStorage) GetCommitHistory() ([]string, error) {
	rows, err := ds.db.Query("SELECT message FROM dolt_log ORDER BY date DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to get commit history: %w", err)
	}
	defer rows.Close()

	var commits []string
	for rows.Next() {
		var msg string
		if err := rows.Scan(&msg); err != nil {
			return nil, fmt.Errorf("failed to scan commit: %w", err)
		}
		commits = append(commits, msg)
	}
	return commits, nil
}

func (ds *DoltStorage) Close() error {
	if ds.db != nil {
		return ds.db.Close()
	}
	return nil
}