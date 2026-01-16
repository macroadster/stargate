package smart_contract

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// FundingProvider fetches up-to-date funding proofs for a task.
type FundingProvider interface {
	FetchProof(ctx context.Context, task smart_contract.Task) (*smart_contract.MerkleProof, error)
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
