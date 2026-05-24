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
	ConfirmContract(ctx context.Context, contractID string, blockHeight int, txid string) error
}

// SweepCommitmentIfReady builds and broadcasts sweep transactions for confirmed commitment outputs.
//
// Two-phase sweep when ProductPixelHash is set:
//   Phase 1 (recommit): wish-hash UTXO → product-hash P2WSH hashlock
//   Phase 2 (final):    product-hash UTXO → STARLIGHT_DONATION_ADDRESS
//
// Single-phase sweep when ProductPixelHash is empty (backward compat):
//   wish-hash UTXO → STARLIGHT_DONATION_ADDRESS
func SweepCommitmentIfReady(ctx context.Context, store SweepStore, mempool *MempoolClient, task smart_contract.Task, proof *smart_contract.MerkleProof) error {
	if proof == nil {
		log.Printf("commitment sweep: proof is nil for task %s", task.TaskID)
		return nil
	}
	if proof.ConfirmationStatus != "confirmed" {
		log.Printf("commitment sweep: proof not confirmed for task %s (status: %s)", task.TaskID, proof.ConfirmationStatus)
		return nil
	}
	if proof.SweepStatus == "confirmed" {
		return nil
	}

	donation := strings.TrimSpace(os.Getenv("STARLIGHT_DONATION_ADDRESS"))
	if donation == "" {
		return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", "donation address not configured")
	}

	productHash := strings.TrimSpace(proof.ProductPixelHash)
	hasProduct := len(productHash) == 64

	// --- Phase 2: recommitment confirmed → sweep product hashlock to donation ---
	if hasProduct && proof.RecommitStatus == "confirmed" {
		return sweepPhase2(ctx, store, mempool, task, proof, donation)
	}

	// --- Phase 1: recommit wish UTXO → product hashlock ---
	if hasProduct && proof.RecommitStatus == "" {
		return sweepPhase1Recommit(ctx, store, mempool, task, proof)
	}

	// Recommitment broadcast but not yet confirmed — wait.
	if hasProduct && proof.RecommitStatus == "broadcast" {
		log.Printf("commitment sweep: waiting for recommitment confirmation for task %s (tx %s)", task.TaskID, proof.RecommitTxID)
		return nil
	}

	// --- No product hash: single-phase sweep to donation (backward compat) ---
	return sweepDirect(ctx, store, mempool, task, proof, donation)
}

func sweepFeeRate() int64 {
	feeRate := int64(1)
	if raw := strings.TrimSpace(os.Getenv("STARLIGHT_SWEEP_FEE_RATE")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			feeRate = parsed
		}
	}
	return feeRate
}

// sweepPhase1Recommit sweeps the wish-hash UTXO into a product-hash P2WSH hashlock.
func sweepPhase1Recommit(ctx context.Context, store SweepStore, mempool *MempoolClient, task smart_contract.Task, proof *smart_contract.MerkleProof) error {
	if proof.CommitmentRedeemScript == "" || proof.CommitmentVout == 0 || proof.TxID == "" {
		return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", "no donation commitment data available")
	}
	if strings.TrimSpace(proof.CommitmentPixelHash) == "" {
		log.Printf("commitment sweep: missing wish pixel hash for task %s", task.TaskID)
		return nil
	}

	wishRedeemScript, err := hex.DecodeString(strings.TrimSpace(proof.CommitmentRedeemScript))
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid wish redeem script")
	}
	if !isHashlockOnlyRedeemScript(wishRedeemScript) {
		return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", "wish redeem script requires signature")
	}
	wishPreimage, err := hex.DecodeString(strings.TrimSpace(proof.CommitmentPixelHash))
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid wish preimage")
	}
	productHash, err := hex.DecodeString(strings.TrimSpace(proof.ProductPixelHash))
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid product hash")
	}

	params := sweepNetworkParamsFromEnv()
	log.Printf("commitment sweep phase1: task %s recommitting wish hashlock → product hashlock", task.TaskID)

	res, err := BuildRecommitSweepTx(mempool, params, proof.TxID, proof.CommitmentVout, wishRedeemScript, wishPreimage, productHash, sweepFeeRate())
	if err != nil {
		log.Printf("commitment sweep phase1: failed to build recommit tx for task %s: %v", task.TaskID, err)
		if strings.Contains(err.Error(), "output below dust") || strings.Contains(err.Error(), "Transaction not found") {
			return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", err.Error())
		}
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", err.Error())
	}

	txid, err := mempool.BroadcastTx(res.RawTxHex)
	if err != nil {
		log.Printf("commitment sweep phase1 ERROR: broadcast failed for task %s: %v", task.TaskID, err)
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", err.Error())
	}

	proof.RecommitTxID = txid
	proof.RecommitVout = res.Vout
	proof.RecommitSats = res.OutputSats
	proof.RecommitRedeemScript = hex.EncodeToString(res.RedeemScript)
	proof.RecommitRedeemHash = hex.EncodeToString(res.RedeemScriptHash)
	proof.RecommitAddress = res.P2WSHAddr
	proof.RecommitStatus = "broadcast"
	log.Printf("commitment sweep phase1: broadcast recommit tx=%s task=%s (wish → product hashlock at %s)", txid, task.TaskID, res.P2WSHAddr)
	return store.UpdateTaskProof(ctx, task.TaskID, proof)
}

// sweepPhase2 sweeps the product-hash UTXO to the donation address.
func sweepPhase2(ctx context.Context, store SweepStore, mempool *MempoolClient, task smart_contract.Task, proof *smart_contract.MerkleProof, donation string) error {
	if proof.RecommitRedeemScript == "" || proof.RecommitTxID == "" {
		return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", "missing recommitment data for phase2")
	}

	// Allow retry for broadcast sweeps after cooldown.
	if proof.SweepStatus == "broadcast" {
		if proof.SweepAttemptedAt != nil && time.Since(*proof.SweepAttemptedAt) < 10*time.Minute {
			return nil
		}
		proof.SweepTxID = ""
		proof.SweepStatus = ""
		proof.SweepAttemptedAt = nil
	}

	redeemScript, err := hex.DecodeString(strings.TrimSpace(proof.RecommitRedeemScript))
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid product redeem script")
	}
	preimage, err := hex.DecodeString(strings.TrimSpace(proof.ProductPixelHash))
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid product preimage")
	}

	params := sweepNetworkParamsFromEnv()
	destAddr, err := btcutil.DecodeAddress(donation, params)
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid donation address")
	}

	log.Printf("commitment sweep phase2: task %s sweeping product hashlock → donation %s", task.TaskID, donation)

	res, err := BuildCommitmentSweepTx(mempool, params, proof.RecommitTxID, proof.RecommitVout, redeemScript, preimage, destAddr, sweepFeeRate())
	if err != nil {
		log.Printf("commitment sweep phase2: failed to build sweep tx for task %s: %v", task.TaskID, err)
		if strings.Contains(err.Error(), "output below dust") || strings.Contains(err.Error(), "Transaction not found") {
			return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", err.Error())
		}
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", err.Error())
	}

	txid, err := mempool.BroadcastTx(res.RawTxHex)
	if err != nil {
		log.Printf("commitment sweep phase2 ERROR: broadcast failed for task %s: %v", task.TaskID, err)
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", err.Error())
	}

	proof.SweepTxID = txid
	proof.SweepStatus = "broadcast"
	now := time.Now()
	proof.SweepAttemptedAt = &now
	proof.SweepError = ""
	log.Printf("commitment sweep phase2: broadcast tx=%s task=%s (product hashlock → donation)", txid, task.TaskID)
	return store.UpdateTaskProof(ctx, task.TaskID, proof)
}

// sweepDirect is the legacy single-phase sweep (no product hash).
func sweepDirect(ctx context.Context, store SweepStore, mempool *MempoolClient, task smart_contract.Task, proof *smart_contract.MerkleProof, donation string) error {
	// Allow retry for broadcast or skipped transactions that may have failed
	if (proof.SweepTxID != "" && proof.SweepStatus == "broadcast") || proof.SweepStatus == "skipped" {
		if proof.SweepStatus == "broadcast" && proof.SweepAttemptedAt != nil && time.Since(*proof.SweepAttemptedAt) < 10*time.Minute {
			return nil
		}
		proof.SweepTxID = ""
		proof.SweepStatus = ""
		proof.SweepAttemptedAt = nil
	}

	if proof.CommitmentRedeemScript == "" || proof.CommitmentVout == 0 || proof.TxID == "" {
		return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", "no donation commitment data available")
	}
	if strings.TrimSpace(proof.CommitmentPixelHash) == "" {
		log.Printf("commitment sweep: missing commitment pixel hash for task %s", task.TaskID)
		return nil
	}

	redeemScript, err := hex.DecodeString(strings.TrimSpace(proof.CommitmentRedeemScript))
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid redeem script")
	}
	if !isHashlockOnlyRedeemScript(redeemScript) {
		return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", "commitment redeem script requires signature")
	}
	preimage, err := hex.DecodeString(strings.TrimSpace(proof.CommitmentPixelHash))
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid preimage")
	}

	params := sweepNetworkParamsFromEnv()
	destAddr, err := btcutil.DecodeAddress(donation, params)
	if err != nil {
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", "invalid donation address")
	}
	log.Printf("commitment sweep: task %s sweeping hashlock commitment to donation address %s", task.TaskID, donation)

	res, err := BuildCommitmentSweepTx(mempool, params, proof.TxID, proof.CommitmentVout, redeemScript, preimage, destAddr, sweepFeeRate())
	if err != nil {
		log.Printf("commitment sweep: failed to build sweep tx for task %s: %v", task.TaskID, err)
		if strings.Contains(err.Error(), "output below dust") || strings.Contains(err.Error(), "Transaction not found") {
			return markSweepStatus(ctx, store, task.TaskID, proof, "skipped", err.Error())
		}
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", err.Error())
	}

	txid, err := mempool.BroadcastTx(res.RawTxHex)
	if err != nil {
		log.Printf("commitment sweep ERROR: Failed to broadcast tx for task %s: %v", task.TaskID, err)
		return markSweepStatus(ctx, store, task.TaskID, proof, "failed", err.Error())
	}

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
