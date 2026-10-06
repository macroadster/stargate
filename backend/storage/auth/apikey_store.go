package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// ErrAPIKeyNotFound is returned when UpdateWallet / Get cannot locate a key.
var ErrAPIKeyNotFound = errors.New("api key not found")

// APIKey represents an issued API key and optional user metadata.
type APIKey struct {
	Key       string    `json:"key"`
	Email     string    `json:"email,omitempty"`
	Wallet    string    `json:"wallet,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Source    string    `json:"source,omitempty"` // e.g. "seed", "registration"
}

// APIKeyValidator defines the minimal interface required by auth middleware.
type APIKeyValidator interface {
	Validate(key string) bool
	Get(key string) (APIKey, bool)
}

// APIKeyWalletUpdater allows updating a wallet binding for an existing API key.
type APIKeyWalletUpdater interface {
	UpdateWallet(key, wallet string) (APIKey, error)
}

// APIKeyIssuer allows creating new API keys.
type APIKeyIssuer interface {
	Issue(email, wallet, source string) (APIKey, error)
}

// APIKeyWalletReissuer allows invalidating existing keys for a wallet before reissuing.
type APIKeyWalletReissuer interface {
	InvalidateByWallet(wallet string) error
}

// APIKeyStore provides in-memory API key validation/issuance.
type APIKeyStore struct {
	mu   sync.RWMutex
	keys map[string]APIKey
}

// NewAPIKeyStore constructs an empty store.
func NewAPIKeyStore() *APIKeyStore {
	return &APIKeyStore{keys: make(map[string]APIKey)}
}

// Seed adds a pre-existing key. Production keys are issued by challenge/verify;
// this is a test helper, not an environment backdoor.
func (s *APIKeyStore) Seed(key, email, source string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[key] = APIKey{Key: key, Email: email, Source: source, CreatedAt: time.Now()}
}

// SeedEnvironmentVariables used to mint an unverified login from
// STARGATE_API_KEY (optionally bound to STARLIGHT_DONATION_ADDRESS). That was
// a static-secret backdoor. Keys are issued only by challenge/verify now.
// The hook remains so storage backends have one startup call; it only warns
// if the leftover env var is still set (stargate-2f6).
func (s *APIKeyStore) SeedEnvironmentVariables() {
	warnIgnoredAPIKeyEnv()
}

// Validate returns true if the key exists.
func (s *APIKeyStore) Validate(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.keys[key]
	return ok
}

// Get returns the stored record for a key, if present.
func (s *APIKeyStore) Get(key string) (APIKey, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return APIKey{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.keys[key]
	return rec, ok
}

// InvalidateByWallet removes all API keys associated with a wallet address.
func (s *APIKeyStore) InvalidateByWallet(wallet string) error {
	if strings.TrimSpace(wallet) == "" {
		return fmt.Errorf("wallet required")
	}
	normalizedWallet := strings.ToLower(strings.TrimSpace(wallet))
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, rec := range s.keys {
		if strings.ToLower(rec.Wallet) == normalizedWallet {
			delete(s.keys, key)
		}
	}
	return nil
}

// Issue creates and stores a new API key.
func (s *APIKeyStore) Issue(email, wallet, source string) (APIKey, error) {
	key, err := generateKey()
	if err != nil {
		return APIKey{}, err
	}
	rec := APIKey{Key: key, Email: email, Wallet: wallet, Source: source, CreatedAt: time.Now()}
	s.mu.Lock()
	s.keys[key] = rec
	s.mu.Unlock()
	return rec, nil
}

// UpdateWallet binds a wallet address to an existing API key.
func (s *APIKeyStore) UpdateWallet(key, wallet string) (APIKey, error) {
	normalizedKey := strings.TrimSpace(key)
	normalizedWallet := strings.TrimSpace(wallet)
	if normalizedKey == "" {
		return APIKey{}, fmt.Errorf("api key required")
	}
	if normalizedWallet == "" {
		return APIKey{}, fmt.Errorf("wallet_address required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.keys[normalizedKey]
	if !ok {
		return APIKey{}, ErrAPIKeyNotFound
	}
	rec.Wallet = normalizedWallet
	s.keys[normalizedKey] = rec
	return rec, nil
}

func generateKey() (string, error) {
	b := make([]byte, 32) // 256-bit key
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashAPIKey returns the SHA256 hex digest used as api_keys.key_hash in SQLite and Postgres.
func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

func warnIgnoredAPIKeyEnv() {
	if strings.TrimSpace(os.Getenv("STARGATE_API_KEY")) == "" {
		return
	}
	log.Printf("SECURITY: STARGATE_API_KEY is ignored; API keys are issued only by POST /api/auth/challenge + /api/auth/verify")
}
