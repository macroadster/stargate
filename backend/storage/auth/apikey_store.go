package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

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

// APIKeyStore provides in-memory API key validation/issuance.
type APIKeyStore struct {
	mu   sync.RWMutex
	keys map[string]APIKey
}

// NewAPIKeyStore constructs an empty store.
func NewAPIKeyStore() *APIKeyStore {
	return &APIKeyStore{keys: make(map[string]APIKey)}
}

// Seed adds a pre-existing key (e.g., from env).
func (s *APIKeyStore) Seed(key, email, source string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[key] = APIKey{Key: key, Email: email, Source: source, CreatedAt: time.Now()}
}

// Validate returns true if the key exists.
func (s *APIKeyStore) Validate(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.keys[key]
	return ok
}

// Get returns the stored record for a key, if present.
func (s *APIKeyStore) Get(key string) (APIKey, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.keys[key]
	return rec, ok
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
		return APIKey{}, fmt.Errorf("api key not found")
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
