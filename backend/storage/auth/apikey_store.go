package auth

import (
	"crypto/rand"
	"encoding/hex"
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

func generateKey() (string, error) {
	b := make([]byte, 32) // 256-bit key
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
