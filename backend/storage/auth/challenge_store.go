package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Challenge represents a pending wallet verification.
type Challenge struct {
	Nonce       string    `json:"nonce"`
	Wallet      string    `json:"wallet_address"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
	Attempts    int       `json:"attempts"`
	MaxAttempts int       `json:"max_attempts"`
}

// ChallengeStore keeps in-memory challenges (sufficient for current needs; can be swapped for Postgres).
type ChallengeStore struct {
	mu         sync.Mutex
	ttl        time.Duration
	challenges map[string]Challenge // keyed by wallet
}

// NewChallengeStore builds a new in-memory challenge store with AI-friendly settings.
func NewChallengeStore(ttl time.Duration) *ChallengeStore {
	return &ChallengeStore{
		ttl:        ttl,
		challenges: make(map[string]Challenge),
	}
}

// NewAIChallengeStore builds a challenge store optimized for AI agents with higher limits.
func NewAIChallengeStore(ttl time.Duration) *ChallengeStore {
	return &ChallengeStore{
		ttl:        ttl,
		challenges: make(map[string]Challenge),
	}
}

// Issue creates or refreshes a challenge for a wallet.
func (s *ChallengeStore) Issue(wallet string) (Challenge, error) {
	return s.IssueWithOptions(wallet, 5)
}

// IssueWithOptions creates a challenge with custom attempt limits for different use cases.
func (s *ChallengeStore) IssueWithOptions(wallet string, maxAttempts int) (Challenge, error) {
	nonce, err := randomNonce()
	if err != nil {
		return Challenge{}, err
	}
	ch := Challenge{
		Nonce:       nonce,
		Wallet:      wallet,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(s.ttl),
		MaxAttempts: maxAttempts,
	}
	s.mu.Lock()
	s.challenges[wallet] = ch
	s.mu.Unlock()
	return ch, nil
}

// IssueAIChallenge creates a challenge with AI-friendly settings (higher attempt limits).
func (s *ChallengeStore) IssueAIChallenge(wallet string) (Challenge, error) {
	return s.IssueWithOptions(wallet, 20) // Allow more attempts for AI debugging
}

// Verify checks signature against the outstanding nonce.
func (s *ChallengeStore) Verify(wallet, signature string, verifier func(ch Challenge, sig string) bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.challenges[wallet]
	if !ok {
		return false
	}
	if time.Now().After(ch.ExpiresAt) {
		delete(s.challenges, wallet)
		return false
	}
	ch.Attempts++
	s.challenges[wallet] = ch
	if ch.Attempts > ch.MaxAttempts {
		delete(s.challenges, wallet)
		return false
	}
	if verifier != nil && verifier(ch, signature) {
		delete(s.challenges, wallet)
		return true
	}
	return false
}

// VerifyWithDetails returns detailed verification results for better error handling.
func (s *ChallengeStore) VerifyWithDetails(wallet, signature string, verifier func(ch Challenge, sig string) bool) VerificationResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := VerificationResult{Success: false}

	ch, ok := s.challenges[wallet]
	if !ok {
		result.Reason = "No active challenge found for wallet"
		return result
	}

	result.RemainingAttempts = ch.MaxAttempts - ch.Attempts

	if time.Now().After(ch.ExpiresAt) {
		delete(s.challenges, wallet)
		result.Reason = "Challenge expired"
		return result
	}

	ch.Attempts++
	s.challenges[wallet] = ch

	if ch.Attempts > ch.MaxAttempts {
		delete(s.challenges, wallet)
		result.Reason = "Maximum attempts exceeded"
		return result
	}

	if verifier != nil && verifier(ch, signature) {
		delete(s.challenges, wallet)
		result.Success = true
		result.Reason = "Signature verified successfully"
	} else {
		result.Reason = "Invalid signature"
	}

	return result
}

// VerificationResult provides detailed feedback about verification attempts.
type VerificationResult struct {
	Success           bool   `json:"success"`
	Reason            string `json:"reason"`
	RemainingAttempts int    `json:"remaining_attempts"`
}

// Get returns a copy of the current challenge for a wallet.
func (s *ChallengeStore) Get(wallet string) (Challenge, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.challenges[wallet]
	return ch, ok
}

// Delete removes a challenge (used on success or cleanup).
func (s *ChallengeStore) Delete(wallet string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.challenges, wallet)
}

func randomNonce() (string, error) {
	b := make([]byte, 16) // 128-bit nonce
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
