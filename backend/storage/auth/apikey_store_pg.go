package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGAPIKeyStore persists API keys in Postgres.
type PGAPIKeyStore struct {
	pool *pgxpool.Pool
}

// NewPGAPIKeyStore connects and initializes schema.
func NewPGAPIKeyStore(ctx context.Context, dsn string) (*PGAPIKeyStore, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}

	if config.MaxConns < 20 {
		config.MaxConns = 20
	}

	config.MinConns = 5
	config.HealthCheckPeriod = 1 * time.Minute
	config.MaxConnLifetime = 1 * time.Hour
	config.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
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
  key_hash TEXT PRIMARY KEY,
  email TEXT,
  wallet_address TEXT,
  source TEXT,
  created_at TIMESTAMPTZ DEFAULT now()
);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS wallet_address TEXT;
`
	_, err := s.pool.Exec(ctx, schema)
	return err
}

// Validate implements APIKeyValidator.
func (s *PGAPIKeyStore) Validate(key string) bool {
	if key == "" {
		return false
	}

	// Hash the key for lookup (matches how it's stored)
	keyHash := hashAPIKey(key)

	var exists bool
	err := s.pool.QueryRow(context.Background(),
		"SELECT true FROM api_keys WHERE key_hash=$1",
		keyHash).Scan(&exists)
	return err == nil && exists
}

// Get returns the API key record for the provided key.
func (s *PGAPIKeyStore) Get(key string) (APIKey, bool) {
	if key == "" {
		return APIKey{}, false
	}
	var rec APIKey

	// Hash the key for lookup (matches how it's stored)
	keyHash := hashAPIKey(key)

	err := s.pool.QueryRow(context.Background(),
		"SELECT email, wallet_address, source, created_at FROM api_keys WHERE key_hash=$1",
		keyHash,
	).Scan(&rec.Email, &rec.Wallet, &rec.Source, &rec.CreatedAt)

	if err != nil {
		return APIKey{}, false
	}
	return rec, true
}

// Issue implements APIKeyIssuer.
func (s *PGAPIKeyStore) Issue(email, wallet, source string) (APIKey, error) {
	key, err := generateKey()
	if err != nil {
		return APIKey{}, err
	}

	keyHash := hashAPIKey(key)

	rec := APIKey{
		Key:       key,
		Email:     email,
		Wallet:    wallet,
		Source:    source,
		CreatedAt: time.Now(),
	}
	_, err = s.pool.Exec(context.Background(),
		"INSERT INTO api_keys (key_hash, email, wallet_address, source, created_at) VALUES ($1,$2,$3,$4,$5)",
		keyHash, rec.Email, rec.Wallet, rec.Source, rec.CreatedAt)
	if err != nil {
		return APIKey{}, err
	}
	return rec, nil
}

// UpdateWallet binds a wallet address to an existing API key.
func (s *PGAPIKeyStore) UpdateWallet(key, wallet string) (APIKey, error) {
	normalizedKey := strings.TrimSpace(key)
	normalizedWallet := strings.TrimSpace(wallet)
	if normalizedKey == "" {
		return APIKey{}, fmt.Errorf("api key required")
	}
	if normalizedWallet == "" {
		return APIKey{}, fmt.Errorf("wallet_address required")
	}

	// Hash the key for lookup (matches how it's stored)
	keyHash := hashAPIKey(normalizedKey)

	var rec APIKey
	err := s.pool.QueryRow(context.Background(), `
UPDATE api_keys
SET wallet_address=$2
WHERE key_hash=$1
RETURNING email, wallet_address, source, created_at
`, keyHash, normalizedWallet).Scan(&rec.Email, &rec.Wallet, &rec.Source, &rec.CreatedAt)
	if err != nil {
		return APIKey{}, err
	}
	return rec, nil
}

// InvalidateByWallet removes all API keys associated with a wallet address.
func (s *PGAPIKeyStore) InvalidateByWallet(wallet string) error {
	if strings.TrimSpace(wallet) == "" {
		return fmt.Errorf("wallet required")
	}
	normalizedWallet := strings.ToLower(strings.TrimSpace(wallet))

	_, err := s.pool.Exec(context.Background(),
		"DELETE FROM api_keys WHERE LOWER(wallet_address) = $1",
		normalizedWallet)
	return err
}

// Seed inserts a provided key if not empty.
func (s *PGAPIKeyStore) Seed(key, email, source string) {
	if key == "" {
		return
	}
	hash := hashAPIKey(key)

	_, _ = s.pool.Exec(context.Background(),
		"INSERT INTO api_keys (key_hash, email, source, created_at) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING",
		hash, email, source, time.Now())
}

// SeedEnvironmentVariables seeds STARGATE_API_KEY and STARLIGHT_DONATION_ADDRESS from environment variables.
func (s *PGAPIKeyStore) SeedEnvironmentVariables() {
	plan := PlanEnvSeed()
	if plan.BindKey != "" {
		_, _ = s.pool.Exec(context.Background(),
			"INSERT INTO api_keys (key_hash, email, wallet_address, source, created_at) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING",
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
