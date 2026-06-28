package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
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

// Seed adds a pre-existing key (e.g., from env).
func (s *APIKeyStore) Seed(key, email, source string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[key] = APIKey{Key: key, Email: email, Source: source, CreatedAt: time.Now()}
}

// SeedEnvironmentVariables seeds STARGATE_API_KEY and STARLIGHT_DONATION_ADDRESS from environment variables.
func (s *APIKeyStore) SeedEnvironmentVariables() {
	plan := PlanEnvSeed()
	if plan.BindKey != "" {
		s.mu.Lock()
		s.keys[plan.BindKey] = APIKey{
			Key: plan.BindKey, Wallet: plan.BindWallet, Source: "seed", CreatedAt: time.Now(),
		}
		s.mu.Unlock()
		return
	}
	if plan.SeedKeyOnly != "" {
		s.Seed(plan.SeedKeyOnly, "", "seed")
	}
	if plan.SeedDonationAsKey != "" {
		s.Seed(plan.SeedDonationAsKey, "donation@starlight", "donation_seed")
	}
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

// hashAPIKey returns the SHA256 hex digest used as api_keys.key_hash in SQLite and Postgres.
func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

// EnvSeedPlan describes how STARGATE_API_KEY / STARLIGHT_DONATION_ADDRESS should be applied.
// Both SQL dialects interpret this the same way so seed behavior cannot drift.
type EnvSeedPlan struct {
	// BindKey+BindWallet when both env vars set (key bound to donation wallet).
	BindKey    string
	BindWallet string
	// SeedKeyOnly when only STARGATE_API_KEY is set.
	SeedKeyOnly string
	// SeedDonationAsKey when only STARLIGHT_DONATION_ADDRESS is set (legacy seed path).
	SeedDonationAsKey string
}

// PlanEnvSeed reads environment once for API key stores (memory / sqlite / postgres).
func PlanEnvSeed() EnvSeedPlan {
	stargateKey := strings.TrimSpace(os.Getenv("STARGATE_API_KEY"))
	donationAddr := strings.TrimSpace(os.Getenv("STARLIGHT_DONATION_ADDRESS"))
	if stargateKey != "" && donationAddr != "" {
		return EnvSeedPlan{BindKey: stargateKey, BindWallet: donationAddr}
	}
	plan := EnvSeedPlan{}
	if stargateKey != "" {
		plan.SeedKeyOnly = stargateKey
	}
	if donationAddr != "" {
		plan.SeedDonationAsKey = donationAddr
	}
	return plan
}
