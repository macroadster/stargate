package ingestion

// Package ingestion centralizes all logic related to the starlight_ingestions
// and starlight_ingest_updates tables.
//
// This used to live in services/ingestion_service.go. It is now the single
// source of truth under the storage package as part of the STARGATE_STORAGE
// unification effort.
//
// Supports both PostgreSQL (pgx) and SQLite (mattn/go-sqlite3) backends.
// The dialect is auto-detected from the DSN: file paths and strings ending
// in ".db" use SQLite; everything else uses Postgres.

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"
)

var ingestionTablePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// IngestionRecord is the primary row type for the ingestions table.
type IngestionRecord struct {
	ID            string                 `json:"id"`
	Filename      string                 `json:"filename"`
	Method        string                 `json:"method"`
	MessageLength int                    `json:"message_length"`
	ImageBase64   string                 `json:"image_base64,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	Status        string                 `json:"status"`
	CreatedAt     time.Time              `json:"created_at"`
}

// IngestUpdateRow represents a pending/processing update row.
type IngestUpdateRow struct {
	ID       int64
	Payload  []byte
	Attempts int
}

// scanTime parses a created_at value that may be a time.Time (Postgres) or
// a string (SQLite).  Returns zero time on failure.
func scanTime(v interface{}) time.Time {
	switch val := v.(type) {
	case time.Time:
		return val
	case string:
		for _, layout := range []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
		} {
			if t, err := time.Parse(layout, val); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// IngestionService is the ingestion persistence service (Postgres or SQLite).
type IngestionService struct {
	db        *sql.DB
	tableName string
	dialect   string // "postgres" or "sqlite"
}

func isValidIngestionTableName(name string) bool {
	return ingestionTablePattern.MatchString(name)
}

// isSQLiteDSN returns true when the DSN looks like a SQLite file path
// rather than a Postgres connection string.
func isSQLiteDSN(dsn string) bool {
	if strings.HasSuffix(dsn, ".db") || strings.HasSuffix(dsn, ".sqlite") || strings.HasSuffix(dsn, ".sqlite3") {
		return true
	}
	if strings.HasPrefix(dsn, "/") || strings.HasPrefix(dsn, "./") || strings.HasPrefix(dsn, "../") {
		return true
	}
	// Postgres DSNs contain "://" or start with "host="/"postgres"
	if strings.Contains(dsn, "://") || strings.HasPrefix(dsn, "host=") || strings.HasPrefix(dsn, "postgres") {
		return false
	}
	// File paths without slashes but with path separators (e.g. "data/ingestions.db")
	if strings.Contains(dsn, "/") && !strings.Contains(dsn, " ") {
		return true
	}
	return false
}

// NewIngestionService creates a new IngestionService.
// The backend dialect is auto-detected from the DSN: file paths use SQLite,
// connection strings use Postgres.
func NewIngestionService(dsn string) (*IngestionService, error) {
	dialect := "postgres"
	driver := "pgx"
	if isSQLiteDSN(dsn) {
		dialect = "sqlite"
		driver = "sqlite3"
		dsn = dsn + "?_foreign_keys=on"
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", driver, err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(1 * time.Hour)
	if dialect == "postgres" {
		db.SetConnMaxIdleTime(30 * time.Minute)
	}

	tableName := "starlight_ingestions"
	if !isValidIngestionTableName(tableName) {
		return nil, fmt.Errorf("invalid ingestion table name: %s", tableName)
	}

	s := &IngestionService{
		db:        db,
		tableName: tableName,
		dialect:   dialect,
	}
	if err := s.ensureSchema(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *IngestionService) ensureSchema(ctx context.Context) error {
	var schema string
	if s.dialect == "sqlite" {
		schema = fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
  id TEXT PRIMARY KEY,
  filename TEXT NOT NULL,
  method TEXT NOT NULL,
  message_length INT NOT NULL,
  image_base64 TEXT NOT NULL,
  metadata TEXT DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS starlight_ingest_updates (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ingestion_id TEXT,
  visible_pixel_hash TEXT,
  proposal_id TEXT,
  payload TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INT NOT NULL DEFAULT 0,
  last_error TEXT,
  next_retry_at TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS starlight_ingest_updates_status_idx
  ON starlight_ingest_updates (status, next_retry_at);
`, s.tableName)
	} else {
		schema = fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
  id TEXT PRIMARY KEY,
  filename TEXT NOT NULL,
  method TEXT NOT NULL,
  message_length INT NOT NULL,
  image_base64 TEXT NOT NULL,
  metadata JSONB,
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS starlight_ingest_updates (
  id BIGSERIAL PRIMARY KEY,
  ingestion_id TEXT,
  visible_pixel_hash TEXT,
  proposal_id TEXT,
  payload JSONB NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INT NOT NULL DEFAULT 0,
  last_error TEXT,
  next_retry_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS starlight_ingest_updates_status_idx
  ON starlight_ingest_updates (status, next_retry_at);
`, s.tableName)
	}
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

// GetIngestionSchema returns the dialect name for the given DSN.
func GetIngestionSchema(dialect string) string {
	if dialect == "sqlite" {
		return "sqlite"
	}
	return "postgres"
}


// All the method implementations follow (Create, Get, GetBy*, Update*,
// List*, Delete, Enqueue*, Claim*, Mark*...)
// They are copied verbatim from the original services version.

func (s *IngestionService) Create(rec IngestionRecord) error {
	if rec.ID == "" {
		return fmt.Errorf("missing id")
	}
	if _, err := base64.StdEncoding.DecodeString(rec.ImageBase64); err != nil {
		return fmt.Errorf("invalid base64: %w", err)
	}
	metadataJSON, err := toJSONB(rec.Metadata)
	if err != nil {
		return err
	}
	metadataParam := string(metadataJSON)
	var query string
	if s.dialect == "sqlite" {
		query = fmt.Sprintf(`
INSERT OR IGNORE INTO %s (id, filename, method, message_length, image_base64, metadata, status)
VALUES ($1, $2, $3, $4, $5, $6, $7)
`, s.tableName)
	} else {
		query = fmt.Sprintf(`
INSERT INTO %s (id, filename, method, message_length, image_base64, metadata, status)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO NOTHING;
`, s.tableName)
	}
	_, err = s.db.Exec(query, rec.ID, rec.Filename, rec.Method, rec.MessageLength, rec.ImageBase64, metadataParam, rec.Status)
	return err
}

func (s *IngestionService) Get(id string) (*IngestionRecord, error) {
	if s.db == nil {
		return nil, fmt.Errorf("ingestion service is in memory-only mode (no database)")
	}
	query := fmt.Sprintf(`SELECT id, filename, method, message_length, image_base64, metadata, status, created_at FROM %s WHERE id=$1`, s.tableName)
	var rec IngestionRecord
	var metadataRaw []byte
	var createdAtRaw interface{}
	if err := s.db.QueryRow(query, id).Scan(&rec.ID, &rec.Filename, &rec.Method, &rec.MessageLength, &rec.ImageBase64, &metadataRaw, &rec.Status, &createdAtRaw); err != nil {
		return nil, err
	}
	rec.CreatedAt = scanTime(createdAtRaw)
	rec.Metadata, _ = fromJSONB(metadataRaw)
	return &rec, nil
}

func (s *IngestionService) GetByImageAndMessage(imageBase64, message string) (*IngestionRecord, error) {
	var query string
	if s.dialect == "sqlite" {
		query = fmt.Sprintf(`
SELECT id, filename, method, message_length, image_base64, metadata, status, created_at
FROM %s
WHERE image_base64 = $1
  AND (json_extract(metadata, '$.embedded_message') = $2 OR json_extract(metadata, '$.message') = $2)
LIMIT 1
`, s.tableName)
	} else {
		query = fmt.Sprintf(`
SELECT id, filename, method, message_length, image_base64, metadata, status, created_at
FROM %s
WHERE image_base64 = $1
  AND (metadata->>'embedded_message' = $2 OR metadata->>'message' = $2)
LIMIT 1
`, s.tableName)
	}

	var rec IngestionRecord
	var metadataRaw []byte
	var createdAtRaw interface{}
	if err := s.db.QueryRow(query, imageBase64, message).Scan(&rec.ID, &rec.Filename, &rec.Method, &rec.MessageLength, &rec.ImageBase64, &metadataRaw, &rec.Status, &createdAtRaw); err != nil {
		return nil, err
	}
	rec.CreatedAt = scanTime(createdAtRaw)
	rec.Metadata, _ = fromJSONB(metadataRaw)
	return &rec, nil
}

func (s *IngestionService) GetByFilenameAndMessage(filename, message string) (*IngestionRecord, error) {
	var query string
	if s.dialect == "sqlite" {
		query = fmt.Sprintf(`
SELECT id, filename, method, message_length, image_base64, metadata, status, created_at
FROM %s
WHERE filename = $1
  AND (json_extract(metadata, '$.embedded_message') = $2 OR json_extract(metadata, '$.message') = $2)
ORDER BY created_at DESC
LIMIT 1
`, s.tableName)
	} else {
		query = fmt.Sprintf(`
SELECT id, filename, method, message_length, image_base64, metadata, status, created_at
FROM %s
WHERE filename = $1
  AND (metadata->>'embedded_message' = $2 OR metadata->>'message' = $2)
ORDER BY created_at DESC
LIMIT 1
`, s.tableName)
	}

	var rec IngestionRecord
	var metadataRaw []byte
	var createdAtRaw interface{}
	if err := s.db.QueryRow(query, filename, message).Scan(&rec.ID, &rec.Filename, &rec.Method, &rec.MessageLength, &rec.ImageBase64, &metadataRaw, &rec.Status, &createdAtRaw); err != nil {
		return nil, err
	}
	rec.CreatedAt = scanTime(createdAtRaw)
	rec.Metadata, _ = fromJSONB(metadataRaw)
	return &rec, nil
}

func (s *IngestionService) UpdateStatusWithNote(id, status, note string) error {
	var query string
	if s.dialect == "sqlite" {
		query = fmt.Sprintf(`
UPDATE %s
SET status = $2,
    metadata = json_set(COALESCE(metadata, '{}'), '$.validation', $3)
WHERE id = $1
`, s.tableName)
	} else {
		query = fmt.Sprintf(`
UPDATE %s
SET status = $2,
    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('validation', $3::text)
WHERE id = $1
`, s.tableName)
	}
	_, err := s.db.Exec(query, id, status, note)
	return err
}

func (s *IngestionService) UpdateFromIngest(id string, rec IngestionRecord) error {
	if id == "" {
		return fmt.Errorf("missing id")
	}
	metadataJSON, err := toJSONB(rec.Metadata)
	if err != nil {
		return err
	}
	var query string
	if s.dialect == "sqlite" {
		query = fmt.Sprintf(`
UPDATE %s
SET filename = $2,
    method = $3,
    message_length = $4,
    image_base64 = $5,
    metadata = json_patch(COALESCE(metadata, '{}'), $6),
    status = $7
WHERE id = $1
`, s.tableName)
	} else {
		query = fmt.Sprintf(`
UPDATE %s
SET filename = $2,
    method = $3,
    message_length = $4,
    image_base64 = $5,
    metadata = COALESCE(metadata, '{}'::jsonb) || $6::jsonb,
    status = $7
WHERE id = $1
`, s.tableName)
	}
	_, err = s.db.Exec(query, id, rec.Filename, rec.Method, rec.MessageLength, rec.ImageBase64, string(metadataJSON), rec.Status)
	return err
}

func (s *IngestionService) UpdateMetadata(id string, updates map[string]interface{}) error {
	if id == "" {
		return fmt.Errorf("missing id")
	}
	updatesJSON, err := toJSONB(updates)
	if err != nil {
		return err
	}
	var query string
	if s.dialect == "sqlite" {
		query = fmt.Sprintf(`
UPDATE %s
SET metadata = json_patch(COALESCE(metadata, '{}'), $2)
WHERE id = $1
`, s.tableName)
	} else {
		query = fmt.Sprintf(`
UPDATE %s
SET metadata = COALESCE(metadata, '{}'::jsonb) || $2::jsonb
WHERE id = $1
`, s.tableName)
	}
	_, err = s.db.Exec(query, id, string(updatesJSON))
	return err
}

func (s *IngestionService) UpdateID(oldID, newID string) error {
	if oldID == "" || newID == "" {
		return fmt.Errorf("missing id")
	}
	if oldID == newID {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var exists bool
	checkQuery := fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s WHERE id=$1)`, s.tableName)
	if err = tx.QueryRow(checkQuery, newID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("ingestion id %s already exists", newID)
	}

	updateQuery := fmt.Sprintf(`UPDATE %s SET id=$2 WHERE id=$1`, s.tableName)
	if _, err = tx.Exec(updateQuery, oldID, newID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *IngestionService) ListRecent(status string, limit int) ([]IngestionRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	query := fmt.Sprintf(`SELECT id, filename, method, message_length, image_base64, metadata, status, created_at FROM %s`, s.tableName)
	var args []interface{}
	limitPlaceholder := "$1"
	if status != "" {
		query += " WHERE status=$1"
		args = append(args, status)
		limitPlaceholder = "$2"
	}
	query += " ORDER BY created_at DESC LIMIT " + limitPlaceholder
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recs []IngestionRecord
	for rows.Next() {
		var rec IngestionRecord
		var metadataRaw []byte
		var createdAtRaw interface{}
		if err := rows.Scan(&rec.ID, &rec.Filename, &rec.Method, &rec.MessageLength, &rec.ImageBase64, &metadataRaw, &rec.Status, &createdAtRaw); err != nil {
			return nil, err
		}
		rec.CreatedAt = scanTime(createdAtRaw)
		rec.Metadata, _ = fromJSONB(metadataRaw)
		recs = append(recs, rec)
	}
	return recs, rows.Err()
}

func (s *IngestionService) ListByIDs(ids []string) ([]IngestionRecord, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if s.db == nil {
		return nil, fmt.Errorf("ingestion service is in memory-only mode (no database)")
	}

	var rows *sql.Rows
	var err error
	if s.dialect == "sqlite" {
		// Build IN clause with positional params for SQLite
		placeholders := make([]string, len(ids))
		args := make([]interface{}, len(ids))
		for i, id := range ids {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args[i] = id
		}
		query := fmt.Sprintf(`
SELECT id, filename, method, message_length, image_base64, metadata, status, created_at
FROM %s
WHERE id IN (%s)
`, s.tableName, strings.Join(placeholders, ","))
		rows, err = s.db.Query(query, args...)
	} else {
		query := fmt.Sprintf(`
SELECT id, filename, method, message_length, image_base64, metadata, status, created_at
FROM %s
WHERE id = ANY($1)
`, s.tableName)
		rows, err = s.db.Query(query, ids)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recs []IngestionRecord
	for rows.Next() {
		var rec IngestionRecord
		var metadataRaw []byte
		var createdAtRaw interface{}
		if err := rows.Scan(&rec.ID, &rec.Filename, &rec.Method, &rec.MessageLength, &rec.ImageBase64, &metadataRaw, &rec.Status, &createdAtRaw); err != nil {
			return nil, err
		}
		rec.CreatedAt = scanTime(createdAtRaw)
		rec.Metadata, _ = fromJSONB(metadataRaw)
		recs = append(recs, rec)
	}
	return recs, rows.Err()
}

func (s *IngestionService) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM starlight_ingest_updates WHERE ingestion_id = $1", id); err != nil {
		return fmt.Errorf("delete ingest updates: %w", err)
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE id = $1", s.tableName)
	if _, err := tx.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("delete ingestion: %w", err)
	}
	return tx.Commit()
}

// JSON helpers
func toJSONB(v map[string]interface{}) ([]byte, error) {
	if v == nil {
		return []byte(`{}`), nil
	}
	return json.Marshal(v)
}

func fromJSONB(b []byte) (map[string]interface{}, error) {
	if len(b) == 0 {
		return map[string]interface{}{}, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]interface{}{}, err
	}
	return m, nil
}

// Queue / update tracking methods (used by IPFS + stego reconciliation flows)
func (s *IngestionService) EnqueueIngestUpdate(ctx context.Context, ingestionID, visiblePixelHash, proposalID string, payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("missing payload")
	}
	query := `
INSERT INTO starlight_ingest_updates (ingestion_id, visible_pixel_hash, proposal_id, payload)
VALUES ($1, $2, $3, $4)
`
	_, err := s.db.ExecContext(ctx, query, ingestionID, visiblePixelHash, proposalID, string(payload))
	return err
}

func (s *IngestionService) ClaimIngestUpdates(ctx context.Context, limit int) ([]IngestUpdateRow, error) {
	if limit <= 0 {
		limit = 25
	}
	if s.dialect == "sqlite" {
		// SQLite lacks FOR UPDATE SKIP LOCKED and CTE+UPDATE RETURNING.
		// Use a two-step select-then-update within a transaction.
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()

		selQuery := `
SELECT id, payload, attempts FROM starlight_ingest_updates
WHERE status IN ('pending', 'retry')
  AND (next_retry_at IS NULL OR next_retry_at <= datetime('now'))
ORDER BY created_at ASC
LIMIT $1
`
		rows, err := tx.QueryContext(ctx, selQuery, limit)
		if err != nil {
			return nil, err
		}
		var out []IngestUpdateRow
		for rows.Next() {
			var row IngestUpdateRow
			if err := rows.Scan(&row.ID, &row.Payload, &row.Attempts); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, row)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for i := range out {
			out[i].Attempts++
			_, err := tx.ExecContext(ctx, `
UPDATE starlight_ingest_updates
SET status = 'processing', attempts = $2, updated_at = datetime('now')
WHERE id = $1`, out[i].ID, out[i].Attempts)
			if err != nil {
				return nil, err
			}
		}
		return out, tx.Commit()
	}

	query := `
WITH picked AS (
  SELECT id
  FROM starlight_ingest_updates
  WHERE status IN ('pending', 'retry')
    AND (next_retry_at IS NULL OR next_retry_at <= now())
  ORDER BY created_at ASC
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
UPDATE starlight_ingest_updates
SET status = 'processing',
    attempts = attempts + 1,
    updated_at = now()
WHERE id IN (SELECT id FROM picked)
RETURNING id, payload, attempts
`
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IngestUpdateRow
	for rows.Next() {
		var row IngestUpdateRow
		if err := rows.Scan(&row.ID, &row.Payload, &row.Attempts); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *IngestionService) MarkIngestUpdateApplied(ctx context.Context, id int64) error {
	now := "now()"
	if s.dialect == "sqlite" {
		now = "datetime('now')"
	}
	query := fmt.Sprintf(`
UPDATE starlight_ingest_updates
SET status = 'applied',
    last_error = NULL,
    next_retry_at = NULL,
    updated_at = %s
WHERE id = $1
`, now)
	_, err := s.db.ExecContext(ctx, query, id)
	return err
}

func (s *IngestionService) MarkIngestUpdateRetry(ctx context.Context, id int64, lastErr string, delay time.Duration) error {
	nextRetry := time.Now().Add(delay)
	now := "now()"
	if s.dialect == "sqlite" {
		now = "datetime('now')"
	}
	query := fmt.Sprintf(`
UPDATE starlight_ingest_updates
SET status = 'retry',
    last_error = $2,
    next_retry_at = $3,
    updated_at = %s
WHERE id = $1
`, now)
	_, err := s.db.ExecContext(ctx, query, id, lastErr, nextRetry)
	return err
}

func (s *IngestionService) MarkIngestUpdateFailed(ctx context.Context, id int64, lastErr string) error {
	now := "now()"
	if s.dialect == "sqlite" {
		now = "datetime('now')"
	}
	query := fmt.Sprintf(`
UPDATE starlight_ingest_updates
SET status = 'failed',
    last_error = $2,
    updated_at = %s
WHERE id = $1
`, now)
	_, err := s.db.ExecContext(ctx, query, id, lastErr)
	return err
}
