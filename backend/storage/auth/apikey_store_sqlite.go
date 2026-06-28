package auth

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteAPIKeyStore struct {
	db *sql.DB
}

func NewSQLiteAPIKeyStore(dbPath string) (*SQLiteAPIKeyStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite3: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(1 * time.Hour)

	s := &SQLiteAPIKeyStore{db: db}
	if err := s.initSchema(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteAPIKeyStore) initSchema(ctx context.Context) error {
	schema := `
CREATE TABLE IF NOT EXISTS api_keys (
  key_hash TEXT PRIMARY KEY,
  email TEXT,
  wallet_address TEXT,
  source TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_api_keys_wallet ON api_keys(wallet_address);
`
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

func (s *SQLiteAPIKeyStore) Validate(key string) bool {
	if key == "" {
		return false
	}
	keyHash := hashAPIKey(key)

	var exists bool
	err := s.db.QueryRowContext(context.Background(),
		"SELECT true FROM api_keys WHERE key_hash=?", keyHash).Scan(&exists)
	return err == nil && exists
}

func (s *SQLiteAPIKeyStore) Get(key string) (APIKey, bool) {
	if key == "" {
		return APIKey{}, false
	}
	var rec APIKey

	keyHash := hashAPIKey(key)

	var createdAtStr string
	err := s.db.QueryRowContext(context.Background(),
		"SELECT email, wallet_address, source, created_at FROM api_keys WHERE key_hash=?",
		keyHash,
	).Scan(&rec.Email, &rec.Wallet, &rec.Source, &createdAtStr)

	if err != nil {
		return APIKey{}, false
	}

	rec.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
	return rec, true
}

func (s *SQLiteAPIKeyStore) Issue(email, wallet, source string) (APIKey, error) {
	key, err := generateKey()
	if err != nil {
		return APIKey{}, err
	}

	keyHash := hashAPIKey(key)

	rec := APIKey{
		Key:       key,
		Email:     email,
		Wallet:    wallet,
		Source:   source,
		CreatedAt: time.Now(),
	}
	_, err = s.db.ExecContext(context.Background(),
		"INSERT INTO api_keys (key_hash, email, wallet_address, source, created_at) VALUES (?,?,?,?,?)",
		keyHash, rec.Email, rec.Wallet, rec.Source, rec.CreatedAt)
	if err != nil {
		return APIKey{}, err
	}
	return rec, nil
}

func (s *SQLiteAPIKeyStore) UpdateWallet(key, wallet string) (APIKey, error) {
	normalizedKey := strings.TrimSpace(key)
	normalizedWallet := strings.TrimSpace(wallet)
	if normalizedKey == "" {
		return APIKey{}, fmt.Errorf("api key required")
	}
	if normalizedWallet == "" {
		return APIKey{}, fmt.Errorf("wallet_address required")
	}

	keyHash := hashAPIKey(normalizedKey)

	var rec APIKey
	var createdAtStr string
	err := s.db.QueryRowContext(context.Background(), `
UPDATE api_keys
SET wallet_address=?
WHERE key_hash=?
RETURNING email, wallet_address, source, created_at
`, normalizedWallet, keyHash).Scan(&rec.Email, &rec.Wallet, &rec.Source, &createdAtStr)
	if err != nil {
		return APIKey{}, err
	}
	rec.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
	return rec, nil
}

func (s *SQLiteAPIKeyStore) InvalidateByWallet(wallet string) error {
	if strings.TrimSpace(wallet) == "" {
		return fmt.Errorf("wallet required")
	}
	normalizedWallet := strings.ToLower(strings.TrimSpace(wallet))

	_, err := s.db.ExecContext(context.Background(),
		"DELETE FROM api_keys WHERE LOWER(wallet_address) = ?",
		normalizedWallet)
	return err
}

func (s *SQLiteAPIKeyStore) Seed(key, email, source string) {
	if key == "" {
		return
	}
	hash := hashAPIKey(key)

	_, _ = s.db.ExecContext(context.Background(),
		"INSERT OR IGNORE INTO api_keys (key_hash, email, source, created_at) VALUES (?,?,?,?)",
		hash, email, source, time.Now())
}

func (s *SQLiteAPIKeyStore) SeedEnvironmentVariables() {
	plan := PlanEnvSeed()
	if plan.BindKey != "" {
		_, _ = s.db.ExecContext(context.Background(),
			"INSERT OR IGNORE INTO api_keys (key_hash, email, wallet_address, source, created_at) VALUES (?,?,?,?,?)",
			hashAPIKey(plan.BindKey), "", plan.BindWallet, "seed", time.Now())
		return
	}
	if plan.SeedKeyOnly != "" {
		s.Seed(plan.SeedKeyOnly, "", "seed")
	}
	if plan.SeedDonationAsKey != "" {
		s.Seed(plan.SeedDonationAsKey, "donation@starlight", "donation_seed")
	}
}

func (s *SQLiteAPIKeyStore) Close() error {
	return s.db.Close()
}