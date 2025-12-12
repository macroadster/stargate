package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGAPIKeyStore persists API keys in Postgres.
type PGAPIKeyStore struct {
	pool *pgxpool.Pool
}

// NewPGAPIKeyStore connects and initializes schema.
func NewPGAPIKeyStore(ctx context.Context, dsn string) (*PGAPIKeyStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	s := &PGAPIKeyStore{pool: pool}
	if err := s.initSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *PGAPIKeyStore) initSchema(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS api_keys (
  key TEXT PRIMARY KEY,
  email TEXT,
  wallet_address TEXT,
  source TEXT,
  created_at TIMESTAMPTZ DEFAULT now()
);
`
	_, err := s.pool.Exec(ctx, schema)
	return err
}

// Validate implements APIKeyValidator.
func (s *PGAPIKeyStore) Validate(key string) bool {
	if key == "" {
		return false
	}
	var exists bool
	err := s.pool.QueryRow(context.Background(), "SELECT true FROM api_keys WHERE key=$1", key).Scan(&exists)
	return err == nil && exists
}

// Issue implements APIKeyIssuer.
func (s *PGAPIKeyStore) Issue(email, wallet, source string) (APIKey, error) {
	key, err := generateKey()
	if err != nil {
		return APIKey{}, err
	}
	rec := APIKey{
		Key:       key,
		Email:     email,
		Wallet:    wallet,
		Source:    source,
		CreatedAt: time.Now(),
	}
	_, err = s.pool.Exec(context.Background(),
		"INSERT INTO api_keys (key, email, wallet_address, source, created_at) VALUES ($1,$2,$3,$4,$5)",
		rec.Key, rec.Email, rec.Wallet, rec.Source, rec.CreatedAt)
	if err != nil {
		return APIKey{}, err
	}
	return rec, nil
}

// Seed inserts a provided key if not empty.
func (s *PGAPIKeyStore) Seed(key, email, source string) {
	if key == "" {
		return
	}
	_, _ = s.pool.Exec(context.Background(),
		"INSERT INTO api_keys (key, email, source, created_at) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING",
		key, email, source, time.Now())
}
