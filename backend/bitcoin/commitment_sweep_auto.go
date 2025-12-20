package bitcoin

import (
	"context"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"

	"stargate-backend/core/smart_contract"
)

// SweepStore persists updates to task proofs after sweep attempts.
type SweepStore interface {
	UpdateTaskProof(ctx context.Context, taskID string, proof *smart_contract.MerkleProof) error
}

// SweepTaskStore can also list tasks for a given contract.
type SweepTaskStore interface {
	SweepStore
	ListTasks(filter smart_contract.TaskFilter) ([]smart_contract.Task, error)
}

// SweepCommitmentIfReady builds and broadcasts a sweep transaction for confirmed commitment outputs.
func SweepCommitmentIfReady(ctx context.Context, store SweepStore, mempool *MempoolClient, task smart_contract.Task, proof *smart_contract.MerkleProof) error {
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

	res, err := BuildCommitmentSweepTx(mempool, params, proof.TxID, proof.CommitmentVout, redeemScript, preimage, destAddr, feeRate)
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

func markSweepStatus(ctx context.Context, store SweepStore, taskID string, proof *smart_contract.MerkleProof, status, errMsg string) error {
	proof.SweepStatus = status
	proof.SweepError = errMsg
	now := time.Now()
	proof.SweepAttemptedAt = &now
	return store.UpdateTaskProof(ctx, taskID, proof)
}

func sweepNetworkParamsFromEnv() *chaincfg.Params {
	switch GetCurrentNetwork() {
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
