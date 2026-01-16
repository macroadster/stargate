package smart_contract

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	scstore "stargate-backend/storage/smart_contract"

	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
)

type FundingProvider interface {
	FetchProof(ctx context.Context, task smart_contract.Task) (*smart_contract.MerkleProof, error)
}

// NewFundingProvider selects a provider based on name.
func NewFundingProvider(name, base string) FundingProvider {
	switch name {
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
	// Check cache first
	taskID := task.TaskID
	if taskID == "" && task.MerkleProof != nil && task.MerkleProof.TxID != "" {
		taskID = task.MerkleProof.TxID
	}

	if taskID == "" {
		return p.provider.FetchProof(ctx, task)
	}

	// Check cache
	p.cacheMutex.RLock()
	entry, exists := p.cache[taskID]
	p.cacheMutex.RUnlock()

	if exists && time.Since(entry.cachedAt) < p.ttl {
		log.Printf("Funding sync cache hit for task %s", taskID)
		return entry.proof, nil
	}

	// Cache miss - fetch from underlying provider
	log.Printf("Funding sync cache miss for task %s", taskID)
	proof, err := p.provider.FetchProof(ctx, task)
	if err != nil {
		return nil, err
	}

	// Update cache
	p.cacheMutex.Lock()
	p.cache[taskID] = &proofCacheEntry{
		proof:    proof,
		cachedAt: time.Now(),
	}
	p.cacheMutex.Unlock()

	return proof, nil
}

// mockFundingProvider confirms provisional proofs without external calls.
type mockFundingProvider struct{}

// NewMockFundingProvider returns a provider that simply upgrades provisional proofs to confirmed.
func NewMockFundingProvider() FundingProvider {
	return &mockFundingProvider{}
}

func (m *mockFundingProvider) FetchProof(ctx context.Context, task smart_contract.Task) (*smart_contract.MerkleProof, error) {
	if task.MerkleProof == nil {
		return nil, errors.New("no proof to upgrade")
	}
	proof := *task.MerkleProof
	if proof.ConfirmationStatus == "confirmed" {
		return task.MerkleProof, nil
	}
	now := time.Now()
	proof.ConfirmationStatus = "confirmed"
	proof.ConfirmedAt = &now
	if proof.BlockHeight == 0 {
		proof.BlockHeight = 0
	}
	if proof.TxID == "" {
		proof.TxID = "mock-txid"
	}
	if proof.BlockHeaderMerkleRoot == "" {
		proof.BlockHeaderMerkleRoot = proof.TxID
	}
	if len(proof.ProofPath) == 0 {
		proof.ProofPath = []smart_contract.ProofNode{{Hash: proof.TxID, Direction: "left"}}
	}
	return &proof, nil
}

// StartFundingSync periodically refreshes provisional proofs using the provider.
func StartFundingSync(ctx context.Context, store Store, provider FundingProvider, escort *smart_contract.EscortService, interval time.Duration) error {
	pgStore, ok := store.(*scstore.PGStore)
	if !ok {
		return errors.New("funding sync requires Postgres store")
	}
	mempool := bitcoin.NewMempoolClient()
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := refreshProofs(ctx, pgStore, provider, escort, mempool); err != nil {
					log.Printf("funding sync error: %v", err)
				}
			}
		}
	}()
	return nil
}

func refreshProofs(ctx context.Context, store *scstore.PGStore, provider FundingProvider, escort *smart_contract.EscortService, mempool *bitcoin.MempoolClient) error {
	tasks, err := store.ListTasks(smart_contract.TaskFilter{Status: ""})
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.MerkleProof == nil {
			continue
		}
		proof := t.MerkleProof
		prevStatus := proof.ConfirmationStatus
		if proof.ConfirmationStatus == "provisional" {
			fetched, err := provider.FetchProof(ctx, t)
			if err != nil {
				continue
			}
			proof = fetched
			if err := store.UpdateTaskProof(ctx, t.TaskID, proof); err != nil {
				log.Printf("failed to update proof for %s: %v", t.TaskID, err)
			} else {
				// Always publish proof update to sync across instances
				PublishEvent(smart_contract.Event{
					Type:      "task_proof_update",
					EntityID:  t.TaskID,
					Actor:     "oracle",
					Message:   fmt.Sprintf("task proof updated (status=%s)", proof.ConfirmationStatus),
					CreatedAt: time.Now(),
				})

				if prevStatus != "confirmed" && proof.ConfirmationStatus == "confirmed" {
					PublishEvent(smart_contract.Event{
						Type:      "contract_confirmed",
						EntityID:  t.ContractID,
						Actor:     "oracle",
						Message:   fmt.Sprintf("contract confirmed on-chain via task %s", t.TaskID),
						CreatedAt: time.Now(),
					})
				}
			}
		}

		// Use EscortService to validate proof and publish results
		if escort != nil {
			escortStatus, err := escort.ValidateProof(proof)
			if err == nil && escortStatus != nil {
				// We need TaskID in EscortStatus for sync
				escortStatus.TaskID = t.TaskID

				// Persist locally
				_ = store.SyncEscortStatus(ctx, *escortStatus)

				// Publish for other instances
				PublishEvent(smart_contract.Event{
					Type:      "escort_validation",
					EntityID:  t.TaskID,
					Actor:     "escort",
					Message:   fmt.Sprintf("escort validation result: %s", escortStatus.ProofStatus),
					CreatedAt: time.Now(),
				})
			}
		}

		if err := bitcoin.SweepCommitmentIfReady(ctx, store, mempool, t, proof); err != nil {
			log.Printf("commitment sweep error for %s: %v", t.TaskID, err)
		}
	}
	return nil
}
