package smart_contract

import (
	"context"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
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
func StartFundingSync(ctx context.Context, store Store, provider FundingProvider, interval time.Duration) error {
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
				if err := refreshProofs(ctx, pgStore, provider, mempool); err != nil {
					log.Printf("funding sync error: %v", err)
				}
			}
		}
	}()
	return nil
}

func refreshProofs(ctx context.Context, store *scstore.PGStore, provider FundingProvider, mempool *bitcoin.MempoolClient) error {
	tasks, err := store.ListTasks(smart_contract.TaskFilter{Status: ""})
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
		if err := sweepCommitmentIfReady(ctx, store, mempool, t, proof); err != nil {
			log.Printf("commitment sweep error for %s: %v", t.TaskID, err)
		}
	}
	return nil
}

func sweepCommitmentIfReady(ctx context.Context, store *scstore.PGStore, mempool *bitcoin.MempoolClient, task smart_contract.Task, proof *smart_contract.MerkleProof) error {
	if proof == nil {
		return nil
	}
	if proof.ConfirmationStatus != "confirmed" {
		return nil
	}
	if proof.SweepTxID != "" || proof.SweepStatus == "broadcast" || proof.SweepStatus == "confirmed" {
		return nil
	}
	if proof.CommitmentRedeemScript == "" || proof.CommitmentVout == 0 || proof.TxID == "" {
		return nil
	}
	if strings.TrimSpace(proof.VisiblePixelHash) == "" {
		return nil
	}
	donation := strings.TrimSpace(os.Getenv("STARLIGHT_DONATION_ADDRESS"))
	if donation == "" {
		return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", "donation address not configured")
	}

	redeemScript, err := hex.DecodeString(strings.TrimSpace(proof.CommitmentRedeemScript))
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid redeem script")
	}
	preimage, err := hex.DecodeString(strings.TrimSpace(proof.VisiblePixelHash))
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid preimage")
	}

	params := sweepNetworkParamsFromEnv()
	destAddr, err := btcutil.DecodeAddress(donation, params)
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid donation address")
	}

	feeRate := int64(1)
	if raw := strings.TrimSpace(os.Getenv("STARLIGHT_SWEEP_FEE_RATE")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			feeRate = parsed
		}
	}

	res, err := bitcoin.BuildCommitmentSweepTx(mempool, params, proof.TxID, proof.CommitmentVout, redeemScript, preimage, destAddr, feeRate)
	if err != nil {
		if strings.Contains(err.Error(), "output below dust") {
			return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", err.Error())
		}
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", err.Error())
	}

	txid, err := mempool.BroadcastTx(res.RawTxHex)
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", err.Error())
	}

	proof.SweepTxID = txid
	proof.SweepStatus = "broadcast"
	now := time.Now()
	proof.SweepAttemptedAt = &now
	proof.SweepError = ""
	return store.UpdateTaskProof(ctx, task.TaskID, proof)
}

func markSweepStatus(ctx context.Context, store *scstore.PGStore, taskID string, proof *smart_contract.MerkleProof, status, errMsg string) error {
	proof.SweepStatus = status
	proof.SweepError = errMsg
	now := time.Now()
	proof.SweepAttemptedAt = &now
	return store.UpdateTaskProof(ctx, taskID, proof)
}

func sweepNetworkParamsFromEnv() *chaincfg.Params {
	switch bitcoin.GetCurrentNetwork() {
	case "mainnet":
		return &chaincfg.MainNetParams
	case "signet":
		return &chaincfg.SigNetParams
	case "testnet":
		return &chaincfg.TestNet3Params
	default:
		return &chaincfg.TestNet4Params
	}
}
