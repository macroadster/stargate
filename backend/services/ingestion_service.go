package services

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

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

type IngestionService struct {
	db        *sql.DB
	tableName string
}

func NewIngestionService(dsn string) (*IngestionService, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open pgx: %w", err)
	}
	s := &IngestionService{
		db:        db,
		tableName: "starlight_ingestions",
	}
	if err := s.ensureSchema(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *IngestionService) ensureSchema(ctx context.Context) error {
	schema := fmt.Sprintf(`
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
`, s.tableName)
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

func (s *IngestionService) Create(rec IngestionRecord) error {
	// basic sanity
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
	query := fmt.Sprintf(`
INSERT INTO %s (id, filename, method, message_length, image_base64, metadata, status)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO NOTHING;
`, s.tableName)
	_, err = s.db.Exec(query, rec.ID, rec.Filename, rec.Method, rec.MessageLength, rec.ImageBase64, metadataParam, rec.Status)
	return err
}

func (s *IngestionService) Get(id string) (*IngestionRecord, error) {
	query := fmt.Sprintf(`SELECT id, filename, method, message_length, image_base64, metadata, status, created_at FROM %s WHERE id=$1`, s.tableName)
	var rec IngestionRecord
	var metadataRaw []byte
	if err := s.db.QueryRow(query, id).Scan(&rec.ID, &rec.Filename, &rec.Method, &rec.MessageLength, &rec.ImageBase64, &metadataRaw, &rec.Status, &rec.CreatedAt); err != nil {
		return nil, err
	}
	rec.Metadata, _ = fromJSONB(metadataRaw)
	return &rec, nil
}

// GetByImageAndMessage returns a record that matches the image payload and embedded message.
func (s *IngestionService) GetByImageAndMessage(imageBase64, message string) (*IngestionRecord, error) {
	query := fmt.Sprintf(`
SELECT id, filename, method, message_length, image_base64, metadata, status, created_at
FROM %s
WHERE image_base64 = $1
  AND (metadata->>'embedded_message' = $2 OR metadata->>'message' = $2)
LIMIT 1
`, s.tableName)

	var rec IngestionRecord
	var metadataRaw []byte
	if err := s.db.QueryRow(query, imageBase64, message).Scan(&rec.ID, &rec.Filename, &rec.Method, &rec.MessageLength, &rec.ImageBase64, &metadataRaw, &rec.Status, &rec.CreatedAt); err != nil {
		return nil, err
	}
	rec.Metadata, _ = fromJSONB(metadataRaw)
	return &rec, nil
}

// GetByFilenameAndMessage returns a record matching filename and embedded message.
func (s *IngestionService) GetByFilenameAndMessage(filename, message string) (*IngestionRecord, error) {
	query := fmt.Sprintf(`
SELECT id, filename, method, message_length, image_base64, metadata, status, created_at
FROM %s
WHERE filename = $1
  AND (metadata->>'embedded_message' = $2 OR metadata->>'message' = $2)
ORDER BY created_at DESC
LIMIT 1
`, s.tableName)

	var rec IngestionRecord
	var metadataRaw []byte
	if err := s.db.QueryRow(query, filename, message).Scan(&rec.ID, &rec.Filename, &rec.Method, &rec.MessageLength, &rec.ImageBase64, &metadataRaw, &rec.Status, &rec.CreatedAt); err != nil {
		return nil, err
	}
	rec.Metadata, _ = fromJSONB(metadataRaw)
	return &rec, nil
}

// UpdateStatusWithNote sets the status and appends a validation note into metadata.validation.
func (s *IngestionService) UpdateStatusWithNote(id, status, note string) error {
	query := fmt.Sprintf(`
UPDATE %s
SET status = $2,
    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('validation', $3::text)
WHERE id = $1
`, s.tableName)
	_, err := s.db.Exec(query, id, status, note)
	return err
}

// UpdateFromIngest updates fields using data from the ingest callback.
func (s *IngestionService) UpdateFromIngest(id string, rec IngestionRecord) error {
	if id == "" {
		return fmt.Errorf("missing id")
	}
	metadataJSON, err := toJSONB(rec.Metadata)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`
UPDATE %s
SET filename = $2,
    method = $3,
    message_length = $4,
    image_base64 = $5,
    metadata = COALESCE(metadata, '{}'::jsonb) || $6::jsonb,
    status = $7
WHERE id = $1
`, s.tableName)
	_, err = s.db.Exec(query, id, rec.Filename, rec.Method, rec.MessageLength, rec.ImageBase64, string(metadataJSON), rec.Status)
	return err
}

// UpdateMetadata merges the provided metadata into the ingestion record.
func (s *IngestionService) UpdateMetadata(id string, updates map[string]interface{}) error {
	if id == "" {
		return fmt.Errorf("missing id")
	}
	updatesJSON, err := toJSONB(updates)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`
UPDATE %s
SET metadata = COALESCE(metadata, '{}'::jsonb) || $2::jsonb
WHERE id = $1
`, s.tableName)
	_, err = s.db.Exec(query, id, string(updatesJSON))
	return err
}

// UpdateID moves a record to a new id when it is safe to do so.
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

// ListRecent returns recent ingestions, optionally filtered by status.
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
		if err := rows.Scan(&rec.ID, &rec.Filename, &rec.Method, &rec.MessageLength, &rec.ImageBase64, &metadataRaw, &rec.Status, &rec.CreatedAt); err != nil {
			return nil, err
		}
		rec.Metadata, _ = fromJSONB(metadataRaw)
		recs = append(recs, rec)
	}
	return recs, rows.Err()
}

// Helpers to marshal/unmarshal metadata safely.
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
