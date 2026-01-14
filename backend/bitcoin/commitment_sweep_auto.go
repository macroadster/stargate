package bitcoin

import (
	"context"
	"encoding/hex"
	"log"
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
	UpdateContractStatus(ctx context.Context, contractID, status string) error
}

// SweepCommitmentIfReady builds and broadcasts a sweep transaction for confirmed commitment outputs.
func SweepCommitmentIfReady(ctx context.Context, store SweepStore, mempool *MempoolClient, task smart_contract.Task, proof *smart_contract.MerkleProof) error {
	if proof == nil {
		log.Printf("commitment sweep: proof is nil for task %s", task.TaskID)
		return nil
	}
	log.Printf("commitment sweep DEBUG: task %s proof status=%s sweep_status=%s txid=%s", task.TaskID, proof.ConfirmationStatus, proof.SweepStatus, proof.SweepTxID)
	if proof.ConfirmationStatus != "confirmed" {
		log.Printf("commitment sweep: proof not confirmed for task %s (status: %s)", task.TaskID, proof.ConfirmationStatus)
		return nil
	}
	// Only exit early if sweep is confirmed
	if proof.SweepStatus == "confirmed" {
		log.Printf("commitment sweep: sweep already confirmed for task %s", task.TaskID)
		return nil
	}
	if proof.ConfirmationStatus != "confirmed" {
		return nil
	}
	// Only exit early if sweep is confirmed
	if proof.SweepStatus == "confirmed" {
		return nil
	}

	// Allow retry for broadcast or skipped transactions that may have failed
	if (proof.SweepTxID != "" && proof.SweepStatus == "broadcast") || proof.SweepStatus == "skipped" {
		// Check if enough time has passed since last attempt (for broadcast only)
		if proof.SweepStatus == "broadcast" && proof.SweepAttemptedAt != nil && time.Since(*proof.SweepAttemptedAt) < 10*time.Minute {
			log.Printf("commitment sweep: skipping retry for task %s, last attempt %v ago", task.TaskID, time.Since(*proof.SweepAttemptedAt))
			return nil
		}
		log.Printf("commitment sweep: retrying sweep for task %s (previous status: %s, tx: %s)", task.TaskID, proof.SweepStatus, proof.SweepTxID)
		// Clear previous sweep info to allow retry
		proof.SweepTxID = ""
		proof.SweepStatus = ""
		proof.SweepAttemptedAt = nil
	}
	log.Printf("commitment sweep DEBUG: task %s checking required fields - script: %s, vout: %d, txid: %s, hash: %s",
		task.TaskID,
		func() string {
			if proof.CommitmentRedeemScript == "" {
				return "EMPTY"
			}
			return proof.CommitmentRedeemScript[:10] + "..."
		}(),
		proof.CommitmentVout,
		func() string {
			if proof.TxID == "" {
				return "EMPTY"
			}
			return proof.TxID
		}(),
		func() string {
			if proof.VisiblePixelHash == "" {
				return "EMPTY"
			}
			return proof.VisiblePixelHash[:10] + "..."
		}())

	if proof.CommitmentRedeemScript == "" || proof.CommitmentVout == 0 || proof.TxID == "" {
		log.Printf("commitment sweep: missing required data for task %s - script_empty: %v, vout_zero: %v, txid_empty: %v",
			task.TaskID, proof.CommitmentRedeemScript == "", proof.CommitmentVout == 0, proof.TxID == "")
		return nil
	}
	if strings.TrimSpace(proof.CommitmentPixelHash) == "" {
		log.Printf("commitment sweep: missing commitment pixel hash for task %s", task.TaskID)
		return nil
	}
	donation := strings.TrimSpace(os.Getenv("STARLIGHT_DONATION_ADDRESS"))
	log.Printf("commitment sweep DEBUG: task %s donation address: %s", task.TaskID, func() string {
		if donation == "" {
			return "NOT_SET"
		}
		return donation[:10] + "..."
	}())
	if donation == "" {
		return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", "donation address not configured")
	}

	redeemScript, err := hex.DecodeString(strings.TrimSpace(proof.CommitmentRedeemScript))
	if err != nil {
		log.Printf("commitment sweep: failed to decode redeem script for task %s: %v", task.TaskID, err)
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid redeem script")
	}
	log.Printf("commitment sweep DEBUG: task %s redeem script length: %d, hashlock_only: %v", task.TaskID, len(redeemScript), isHashlockOnlyRedeemScript(redeemScript))
	if !isHashlockOnlyRedeemScript(redeemScript) {
		return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", "commitment redeem script requires signature")
	}
	preimage, err := hex.DecodeString(strings.TrimSpace(proof.CommitmentPixelHash))
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid preimage")
	}
	log.Printf("commitment sweep DEBUG: task %s using preimage from CommitmentPixelHash: %s (length=%d)", task.TaskID, strings.TrimSpace(proof.CommitmentPixelHash)[:10]+"...", len(preimage))

	params := sweepNetworkParamsFromEnv()

	// Simple hashlock sweep - use the visible_pixel_hash as preimage to unlock
	// The commitment script should be hashlock-only (no signature required)
	destAddr, err := btcutil.DecodeAddress(donation, params)
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid donation address")
	}
	log.Printf("commitment sweep: task %s sweeping hashlock commitment to donation address %s", task.TaskID, donation)

	feeRate := int64(1)
	if raw := strings.TrimSpace(os.Getenv("STARLIGHT_SWEEP_FEE_RATE")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			feeRate = parsed
		}
	}

	log.Printf("commitment sweep DEBUG: task %s building sweep transaction - txid=%s, vout=%d, dest=%s",
		task.TaskID, proof.TxID, proof.CommitmentVout, destAddr.String())
	res, err := BuildCommitmentSweepTx(mempool, params, proof.TxID, proof.CommitmentVout, redeemScript, preimage, destAddr, feeRate)
	if err != nil {
		log.Printf("commitment sweep: failed to build sweep tx for task %s: %v", task.TaskID, err)
		if strings.Contains(err.Error(), "output below dust") {
			return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", err.Error())
		}
		// If transaction fetch failed, mark as retryable
		if strings.Contains(err.Error(), "Transaction not found") {
			return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", "fetch retry: "+err.Error())
		}
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", err.Error())
	}
	log.Printf("commitment sweep DEBUG: task %s built sweep tx successfully - raw_tx_length=%d", task.TaskID, len(res.RawTxHex))

	log.Printf("commitment sweep DEBUG: Broadcasting sweep tx, raw_hex_length=%d", len(res.RawTxHex))
	log.Printf("commitment sweep DEBUG: Broadcasting sweep tx, raw_hex_length=%d", len(res.RawTxHex))
	log.Printf("commitment sweep DEBUG: Raw tx hex for manual broadcast: %s", res.RawTxHex)
	txid, err := mempool.BroadcastTx(res.RawTxHex)
	if err != nil {
		log.Printf("commitment sweep ERROR: Failed to broadcast tx for task %s: %v", task.TaskID, err)
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", err.Error())
	}
	log.Printf("commitment sweep DEBUG: Successfully broadcast tx=%s for task %s", txid, task.TaskID)

	proof.SweepTxID = txid
	proof.SweepStatus = "broadcast"
	now := time.Now()
	proof.SweepAttemptedAt = &now
	proof.SweepError = ""
	log.Printf("commitment sweep broadcast tx=%s task=%s contract=%s output=%d", txid, task.TaskID, task.ContractID, proof.CommitmentVout)
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
