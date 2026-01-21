package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func generateSalt() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func hashKey(key, salt string) string {
	h := sha256.New()
	h.Write([]byte(salt + key))
	return hex.EncodeToString(h.Sum(nil))
}

func encodeKeyHash(salt, hash string) string {
	return base64.URLEncoding.EncodeToString([]byte(salt + ":" + hash))
}

func decodeKeyHash(encoded string) (salt, hash string, err error) {
	decoded, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", fmt.Errorf("decode key hash: %w", err)
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid key hash format")
	}
	return parts[0], parts[1], nil
}

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
  key_hash TEXT PRIMARY KEY,
  key TEXT,
  email TEXT,
  wallet_address TEXT,
  source TEXT,
  created_at TIMESTAMPTZ DEFAULT now()
);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS key_hash TEXT;
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

	// Try new format: check key_hash
	var hashedKeyExists bool
	err := s.pool.QueryRow(context.Background(),
		"SELECT true FROM api_keys WHERE key_hash=$1",
		key).Scan(&hashedKeyExists)
	if err == nil && hashedKeyExists {
		return true
	}

	// Fallback: check legacy key column (plain text)
	var plainKeyExists bool
	err = s.pool.QueryRow(context.Background(),
		"SELECT true FROM api_keys WHERE key=$1",
		key).Scan(&plainKeyExists)
	return err == nil && plainKeyExists
}

// Get returns the API key record for the provided key.
func (s *PGAPIKeyStore) Get(key string) (APIKey, bool) {
	if key == "" {
		return APIKey{}, false
	}
	var rec APIKey
	var keyHash, plainKey sql.NullString

	err := s.pool.QueryRow(context.Background(),
		"SELECT COALESCE(key_hash, '') as key_hash, COALESCE(key, '') as key, email, wallet_address, source, created_at FROM api_keys WHERE key_hash=$1 OR key=$1",
		key,
	).Scan(&keyHash, &plainKey, &rec.Email, &rec.Wallet, &rec.Source, &rec.CreatedAt)

	if err != nil {
		return APIKey{}, false
	}
	if keyHash.Valid {
		rec.Key = keyHash.String
	} else if plainKey.Valid {
		rec.Key = plainKey.String
	}
	return rec, true
}

// Issue implements APIKeyIssuer.
func (s *PGAPIKeyStore) Issue(email, wallet, source string) (APIKey, error) {
	key, err := generateKey()
	if err != nil {
		return APIKey{}, err
	}

	salt, err := generateSalt()
	if err != nil {
		return APIKey{}, err
	}
	hash := hashKey(key, salt)
	keyHash := encodeKeyHash(salt, hash)

	rec := APIKey{
		Key:       key,
		Email:     email,
		Wallet:    wallet,
		Source:    source,
		CreatedAt: time.Now(),
	}
	_, err = s.pool.Exec(context.Background(),
		"INSERT INTO api_keys (key_hash, key, email, wallet_address, source, created_at) VALUES ($1,$2,$3,$4,$5,$6)",
		keyHash, key, rec.Email, rec.Wallet, rec.Source, rec.CreatedAt)
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
	var rec APIKey
	var keyHash, plainKey sql.NullString
	err := s.pool.QueryRow(context.Background(), `
UPDATE api_keys
SET wallet_address=$2
WHERE key_hash=$1 OR key=$1
RETURNING COALESCE(key_hash, '') as key_hash, COALESCE(key, '') as key, email, wallet_address, source, created_at
`, normalizedKey, normalizedWallet).Scan(&keyHash, &plainKey, &rec.Email, &rec.Wallet, &rec.Source, &rec.CreatedAt)
	if err != nil {
		return APIKey{}, err
	}
	if keyHash.Valid {
		rec.Key = keyHash.String
	} else if plainKey.Valid {
		rec.Key = plainKey.String
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
