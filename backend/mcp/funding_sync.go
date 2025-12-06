package mcp

import (
	"context"
	"errors"
	"log"
	"time"
)

// FundingProvider fetches up-to-date funding proofs for a task.
type FundingProvider interface {
	FetchProof(ctx context.Context, task Task) (*MerkleProof, error)
}

// NewFundingProvider selects a provider based on name.
func NewFundingProvider(name, base string) FundingProvider {
	switch name {
	case "blockstream":
		return NewBlockstreamFundingProvider(base)
	default:
		return NewMockFundingProvider()
	}
}

// mockFundingProvider confirms provisional proofs without external calls.
type mockFundingProvider struct{}

// NewMockFundingProvider returns a provider that simply upgrades provisional proofs to confirmed.
func NewMockFundingProvider() FundingProvider {
	return &mockFundingProvider{}
}

func (m *mockFundingProvider) FetchProof(ctx context.Context, task Task) (*MerkleProof, error) {
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
		proof.ProofPath = []ProofNode{{Hash: proof.TxID, Direction: "left"}}
	}
	return &proof, nil
}

// StartFundingSync periodically refreshes provisional proofs using the provider.
func StartFundingSync(ctx context.Context, store Store, provider FundingProvider, interval time.Duration) error {
	pgStore, ok := store.(*PGStore)
	if !ok {
		return errors.New("funding sync requires Postgres store")
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := refreshProofs(ctx, pgStore, provider); err != nil {
					log.Printf("funding sync error: %v", err)
				}
			}
		}
	}()
	return nil
}

func refreshProofs(ctx context.Context, store *PGStore, provider FundingProvider) error {
	tasks, err := store.ListTasks(TaskFilter{Status: ""})
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.MerkleProof == nil || t.MerkleProof.ConfirmationStatus != "provisional" {
			continue
		}
		proof, err := provider.FetchProof(ctx, t)
		if err != nil {
			continue
		}
		if err := store.UpdateTaskProof(ctx, t.TaskID, proof); err != nil {
			log.Printf("failed to update proof for %s: %v", t.TaskID, err)
		}
	}
	return nil
}
