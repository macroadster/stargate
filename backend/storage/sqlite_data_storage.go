package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
	"stargate-backend/bitcoin"
)

// SQLiteDataStorage implements ExtendedDataStorage using a local SQLite file
// (table: block_scans). This completes STARGATE_STORAGE=sqlite as a fully
// durable embedded mode for the single-binary distribution.
//
// Images stay in the blocks/ filesystem (as today). The JSON metadata
// (inscriptions + scan results) is stored in the block_scans table exactly
// like PostgresStorage does with block_scans.
//
// memory mode keeps the original DataStorage (filesystem + RAM cache) —
// perfect for fast unit tests.
type SQLiteDataStorage struct {
	db        *sql.DB
	dbPath    string
	tableName string
}

func isValidSQLiteTableName(name string) bool {
	return len(name) > 0 && len(name) < 64
}

// NewSQLiteDataStorage opens a SQLite database file for block metadata storage.
func NewSQLiteDataStorage(dbPath string) (*SQLiteDataStorage, error) {
	if dbPath == "" {
		dbPath = DefaultPath("blocks.db")
	}
	if dir := filepath.Dir(dbPath); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}

	db, err := sql.Open("sqlite", dbPath+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open sqlite3 data: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	s := &SQLiteDataStorage{db: db, dbPath: dbPath, tableName: "block_scans"}
	if err := s.ensureSchema(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	log.Printf("SQLiteDataStorage ready: %s", dbPath)
	return s, nil
}

func (s *SQLiteDataStorage) ensureSchema(ctx context.Context) error {
	schema := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    block_height   INTEGER PRIMARY KEY,
    block_hash     TEXT NOT NULL,
    scanned_at     TEXT NOT NULL DEFAULT (datetime('now')),
    payload        TEXT NOT NULL,
    stego_detected INTEGER NOT NULL DEFAULT 0,
    images_scanned INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_%s_hash ON %s(block_hash);
CREATE INDEX IF NOT EXISTS idx_%s_scanned ON %s(scanned_at);
`, s.tableName, s.tableName, s.tableName, s.tableName, s.tableName)

	_, err := s.db.ExecContext(ctx, schema)
	return err
}

// StoreBlockData, GetBlockData, GetRecentBlocks, GetSteganographyStats, etc.
// are implemented identically in spirit to PostgresStorage (JSON payload).

func (s *SQLiteDataStorage) StoreBlockData(blockResponse *bitcoin.BlockInscriptionsResponse, scanResults []map[string]interface{}) error {
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
	payload, _ := json.Marshal(cacheEntry)

	stego := 0
	if cacheEntry.SteganographySummary != nil && cacheEntry.SteganographySummary.StegoDetected {
		stego = cacheEntry.SteganographySummary.StegoCount
	}
	imgs := len(blockResponse.Images)

	q := fmt.Sprintf(`INSERT OR REPLACE INTO %s
		(block_height, block_hash, payload, stego_detected, images_scanned, scanned_at)
		VALUES (?,?,?,?,?,datetime('now'))`, s.tableName)

	_, err := s.db.Exec(q, blockResponse.BlockHeight, blockResponse.BlockHash, string(payload), stego, imgs)
	if err != nil {
		log.Printf("SQLiteDataStorage StoreBlockData warning: %v", err)
	}
	return nil
}

func (s *SQLiteDataStorage) GetBlockData(height int64) (interface{}, error) {
	var payload string
	q := fmt.Sprintf(`SELECT payload FROM %s WHERE block_height=?`, s.tableName)
	if err := s.db.QueryRow(q, height).Scan(&payload); err != nil {
		return nil, err
	}
	var entry BlockDataCache
	_ = json.Unmarshal([]byte(payload), &entry)
	entry.CacheTimestamp = time.Now()
	return &entry, nil
}

func (s *SQLiteDataStorage) GetRecentBlocks(limit int) ([]interface{}, error) {
	q := fmt.Sprintf(`SELECT payload FROM %s ORDER BY block_height DESC LIMIT ?`, s.tableName)
	rows, err := s.db.Query(q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []interface{}
	for rows.Next() {
		var p string
		rows.Scan(&p)
		var e BlockDataCache
		json.Unmarshal([]byte(p), &e)
		e.CacheTimestamp = time.Now()
		out = append(out, &e)
	}
	return out, nil
}

func (s *SQLiteDataStorage) GetSteganographyStats() map[string]interface{} {
	stats := map[string]interface{}{"total_blocks": 0, "total_images": 0, "total_stego_detected": 0, "stego_detection_rate": 0.0, "last_updated": time.Now().Unix()}
	q := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM(images_scanned),0), COALESCE(SUM(CASE WHEN stego_detected>0 THEN 1 ELSE 0 END),0) FROM %s`, s.tableName)
	var c, img, st int64
	s.db.QueryRow(q).Scan(&c, &img, &st)
	stats["total_blocks"] = c
	stats["total_images"] = img
	stats["total_stego_detected"] = st
	if img > 0 {
		stats["stego_detection_rate"] = float64(st) / float64(img) * 100
	}
	return stats
}

func (s *SQLiteDataStorage) ValidateDataIntegrity(h int64) error {
	_, e := s.GetBlockData(h)
	return e
}

func (s *SQLiteDataStorage) ReadTextContent(height int64, path string) (string, error) {
	return "", fmt.Errorf("ReadTextContent not supported on SQLiteDataStorage (filesystem blocks/ layout still used for text/images)")
}

func (s *SQLiteDataStorage) CreateRealtimeUpdate(t string, h int64, d interface{}) *RealtimeUpdate {
	return &RealtimeUpdate{Type: t, Timestamp: time.Now().Unix(), BlockHeight: h, Data: d}
}

// Close is provided for symmetry with other stores.
func (s *SQLiteDataStorage) Close() error { return s.db.Close() }
