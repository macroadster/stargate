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
