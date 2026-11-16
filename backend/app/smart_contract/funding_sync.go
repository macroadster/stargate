package smart_contract

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"stargate-backend/bitcoin"

	"stargate-backend/core/smart_contract"
)

type FundingProvider interface {
	FetchProof(ctx context.Context, task smart_contract.Task) (*smart_contract.MerkleProof, error)
}

func NewFundingProvider(name, base string) FundingProvider {
	switch name {
	case "blockcypher":
		return NewCachedFundingProvider(NewBlockcypherProvider(base))
	case "blockstream":
		return NewCachedFundingProvider(NewBlockstreamFundingProvider(base))
	default:
		return NewCachedFundingProvider(NewMockFundingProvider())
	}
}

// cachedFundingProvider wraps a funding provider with caching
type cachedFundingProvider struct {
	provider FundingProvider

	// Cache for proof results
	cache      map[string]*proofCacheEntry
	cacheTime  time.Time
	cacheMutex sync.RWMutex
	ttl        time.Duration
}

type proofCacheEntry struct {
	proof    *smart_contract.MerkleProof
	cachedAt time.Time
}

// NewCachedFundingProvider creates a funding provider with caching
func NewCachedFundingProvider(provider FundingProvider) FundingProvider {
	return &cachedFundingProvider{
		provider:   provider,
		cache:      make(map[string]*proofCacheEntry),
		cacheTime:  time.Time{},
		cacheMutex: sync.RWMutex{},
		ttl:        60 * time.Second, // Cache proofs for 1 minute
	}
}

func (p *cachedFundingProvider) FetchProof(ctx context.Context, task smart_contract.Task) (*smart_contract.MerkleProof, error) {
	// Check cache first - always prefer transaction ID as cache key
	cacheKey := ""
	if task.MerkleProof != nil && task.MerkleProof.TxID != "" {
		cacheKey = task.MerkleProof.TxID
	} else if task.TaskID != "" {
		cacheKey = task.TaskID
	}

	if cacheKey == "" {
		return p.provider.FetchProof(ctx, task)
	}

	// Check cache
	p.cacheMutex.RLock()
	entry, exists := p.cache[cacheKey]
	p.cacheMutex.RUnlock()

	if exists && time.Since(entry.cachedAt) < p.ttl {
		log.Printf("Funding sync cache hit for task %s", cacheKey)
		return entry.proof, nil
	}

	// Cache miss - fetch from underlying provider
	log.Printf("Funding sync cache miss for task %s", cacheKey)
	proof, err := p.provider.FetchProof(ctx, task)
	if err != nil {
		return nil, err
	}

	// Update cache with transaction ID as key when available
	finalCacheKey := cacheKey
	if proof != nil && proof.TxID != "" {
		finalCacheKey = proof.TxID
	}

	p.cacheMutex.Lock()
	p.cache[finalCacheKey] = &proofCacheEntry{
		proof:    proof,
		cachedAt: time.Now(),
	}
	p.cacheMutex.Unlock()

	return proof, nil
}

// mockFundingProvider backfills missing proof fields from the funding tx.
// It never manufactures settlement depth: confirmation happens only in
// refreshProofs, re-checked against the local chain tip.
type mockFundingProvider struct{}

// NewMockFundingProvider returns a provider that only fills in missing proof
// fields. It cannot confirm a proof; settlement depth is re-checked against
// the local tip in refreshProofs.
func NewMockFundingProvider() FundingProvider {
	return &mockFundingProvider{}
}

func (m *mockFundingProvider) FetchProof(ctx context.Context, task smart_contract.Task) (*smart_contract.MerkleProof, error) {
	if task.MerkleProof == nil {
		return nil, errors.New("no proof to refresh")
	}
	proof := *task.MerkleProof
	if proof.TxID == "" {
		proof.TxID = "mock-txid"
	}
	if proof.BlockHeaderMerkleRoot == "" {
		proof.BlockHeaderMerkleRoot = proof.TxID
	}
	if len(proof.ProofPath) == 0 {
		proof.ProofPath = []smart_contract.ProofNode{{Hash: proof.TxID, Direction: "left"}}
	}
	// A previously confirmed proof stays confirmed; anything else stays
	// provisional. refreshProofs re-checks settlement depth before flipping.
	if strings.EqualFold(proof.ConfirmationStatus, "confirmed") {
		return &proof, nil
	}
	proof.ConfirmationStatus = "provisional"
	proof.ConfirmedAt = nil
	return &proof, nil
}

// StartFundingSync periodically refreshes provisional proofs using the provider.
// tipSource reports the local chain tip height; 0 means the tip is unknown,
// in which case funding sync never confirms anything and the block monitor
// remains the authority on settlement depth. Confirmation happens only once
// the funding tx is SettlementConfirmations deep on the local chain — never
// on a provider response alone.
func StartFundingSync(ctx context.Context, store Store, provider FundingProvider, escort *smart_contract.EscortService, interval time.Duration, tipSource func() int64) error {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		log.Printf("funding sync: started (interval=%v)", interval)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				start := time.Now()
				if err := refreshProofs(ctx, store, provider, escort, tipSource); err != nil {
					log.Printf("funding sync error: %v", err)
				} else {
					d := time.Since(start)
					if d > 1*time.Second {
						log.Printf("funding sync: refreshProofs took %v", d)
					}
				}
			}
		}
	}()
	return nil
}

// saveProof persists the proof and emits update events when something changed.
// prevStatus is the status before the current flip attempt.
func saveProof(ctx context.Context, store Store, t smart_contract.Task, proof *smart_contract.MerkleProof, prevStatus string, changed bool) {
	if !changed {
		return
	}
	if err := store.UpdateTaskProof(ctx, t.TaskID, proof); err != nil {
		log.Printf("failed to update proof for %s: %v", t.TaskID, err)
		return
	}
	PublishEvent(smart_contract.Event{
		Type:      "task_proof_update",
		EntityID:  t.TaskID,
		Actor:     "oracle",
		Message:   fmt.Sprintf("task proof updated (status=%s)", proof.ConfirmationStatus),
		CreatedAt: time.Now(),
	})

	if !strings.EqualFold(prevStatus, "confirmed") && strings.EqualFold(proof.ConfirmationStatus, "confirmed") {
		PublishEvent(smart_contract.Event{
			Type:      "contract_confirmed",
			EntityID:  t.ContractID,
			Actor:     "oracle",
			Message:   fmt.Sprintf("contract confirmed on-chain via task %s", t.TaskID),
			CreatedAt: time.Now(),
		})
	}
}

func refreshProofs(ctx context.Context, store Store, provider FundingProvider, escort *smart_contract.EscortService, tipSource func() int64) error {
	tip := int64(0)
	if tipSource != nil {
		if h := tipSource(); h > 0 {
			tip = h
		}
	}

	// Local settlement promotion. No provider round-trip: a proof whose
	// funding tx is deep enough on the local chain is final, whether or not
	// the task saw recent activity. SettlementReady already accounts for a
	// forked / lagging tip (SettlementBlocked). This mirrors the block
	// monitor's promoteProvisionalProofs; both paths are settlement-gated
	// and idempotent, and neither confirms catch-up scans.
	const page = 200
	for offset := 0; ; offset += page {
		tasks, err := store.ListTasks(smart_contract.TaskFilter{Limit: page, Offset: offset})
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			break
		}
		for _, t := range tasks {
			proof := t.MerkleProof
			if proof == nil || !strings.EqualFold(strings.TrimSpace(proof.ConfirmationStatus), "provisional") {
				continue
			}
			if proof.BlockHeight <= 0 || !bitcoin.SettlementReady(tip, proof.BlockHeight) {
				continue
			}
			prevStatus := proof.ConfirmationStatus
			now := time.Now()
			proof.ConfirmationStatus = "confirmed"
			if proof.ConfirmedAt == nil {
				proof.ConfirmedAt = &now
			}
			if strings.TrimSpace(proof.SweepStatus) == "" {
				// Donation was paid directly in the PSBT; nothing to sweep.
				proof.SweepStatus = "direct"
			}
			saveProof(ctx, store, t, proof, prevStatus, true)
		}
		if len(tasks) < page {
			break
		}
	}

	// Provider refresh for recently active tasks. The provider supplies the
	// block height / merkle path; settlement depth is re-checked against the
	// local tip before anything flips to confirmed.
	twentyFourHoursAgo := time.Now().Add(-24 * time.Hour)
	tasks, err := store.ListTasks(smart_contract.TaskFilter{
		Status:            "",
		LastActivitySince: &twentyFourHoursAgo,
	})
	if err != nil {
		return err
	}
	log.Printf("funding sync: processing %d tasks with activity in last 24 hours", len(tasks))
	for _, t := range tasks {
		if t.MerkleProof == nil {
			continue
		}
		proof := t.MerkleProof

		hasRealCommitment := proof.CommitmentRedeemScript != "" &&
			proof.CommitmentVout > 0 &&
			proof.TxID != "" &&
			proof.TxID != "mock-txid"

		prevStatus := proof.ConfirmationStatus
		if strings.EqualFold(strings.TrimSpace(proof.ConfirmationStatus), "provisional") {
			fetched, err := provider.FetchProof(ctx, t)
			if err != nil {
				continue
			}
			prevHeight, prevTxID := proof.BlockHeight, proof.TxID
			// Never trust a provider's "confirmed" on its own: confirm only
			// at settlement depth on the local chain.
			if strings.EqualFold(strings.TrimSpace(fetched.ConfirmationStatus), "confirmed") &&
				!bitcoin.SettlementReady(tip, fetched.BlockHeight) {
				fetched.ConfirmationStatus = "provisional"
				fetched.ConfirmedAt = nil
			}
			proof = fetched
			changed := !strings.EqualFold(prevStatus, proof.ConfirmationStatus) ||
				prevHeight != proof.BlockHeight ||
				prevTxID != proof.TxID ||
				(fetched.ProofPath != nil && len(fetched.ProofPath) > 0 && len(t.MerkleProof.ProofPath) == 0)
			saveProof(ctx, store, t, proof, prevStatus, changed)
		}

		if escort != nil && hasRealCommitment && !strings.EqualFold(strings.TrimSpace(proof.ConfirmationStatus), "confirmed") {
			escortStatus, err := escort.ValidateProof(proof)
			if err == nil && escortStatus != nil {
				escortStatus.TaskID = t.TaskID

				_ = store.SyncEscortStatus(ctx, *escortStatus)

				PublishEvent(smart_contract.Event{
					Type:      "escort_validation",
					EntityID:  t.TaskID,
					Actor:     "escort",
					Message:   fmt.Sprintf("escort validation result: %s", escortStatus.ProofStatus),
					CreatedAt: time.Now(),
				})
			}
		}

		// OP_RETURN donation flow: donation is paid directly in the PSBT,
		// so no sweep is needed. Legacy hashlock contracts are swept
		// automatically by the block monitor (commitment_sweep_auto.go)
		// once the recommit confirms; there is no manual
		// /api/smart_contract/sweep endpoint.
	}
	return nil
}
