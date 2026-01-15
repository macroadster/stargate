package services

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
	smartstore "stargate-backend/middleware/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// TaskServiceExtended handles advanced task business logic
type TaskServiceExtended struct {
	store smartstore.Store
}

// NewTaskServiceExtended creates a new extended task service
func NewTaskServiceExtended(store smartstore.Store) *TaskServiceExtended {
	return &TaskServiceExtended{
		store: store,
	}
}

// UpdateTaskCommitmentProof updates task commitment proof after PSBT creation
func (s *TaskServiceExtended) UpdateTaskCommitmentProof(ctx context.Context, taskID string, res *bitcoin.PSBTResult, pixelBytes []byte) error {
	task, err := s.store.GetTask(taskID)
	if err != nil {
		return err
	}

	proof := task.MerkleProof
	if proof == nil {
		proof = &smart_contract.MerkleProof{}
	}

	if len(pixelBytes) == 32 {
		proof.VisiblePixelHash = hex.EncodeToString(pixelBytes)
	}
	if res.FundingTxID != "" {
		proof.TxID = res.FundingTxID
	}
	if proof.ConfirmationStatus == "" {
		proof.ConfirmationStatus = "provisional"
	}
	if proof.SeenAt.IsZero() {
		proof.SeenAt = time.Now()
	}
	if len(res.RedeemScript) > 0 {
		proof.CommitmentRedeemScript = hex.EncodeToString(res.RedeemScript)
	}
	if len(res.RedeemScriptHash) > 0 {
		proof.CommitmentRedeemHash = hex.EncodeToString(res.RedeemScriptHash)
	}
	if res.CommitmentAddr != "" {
		proof.CommitmentAddress = res.CommitmentAddr
	}
	if res.CommitmentVout > 0 {
		proof.CommitmentVout = res.CommitmentVout
	}
	if res.CommitmentSats > 0 {
		proof.CommitmentSats = res.CommitmentSats
	}
	if res.FundingTxID != "" && proof.TxID == "" {
		proof.TxID = res.FundingTxID
	}

	return s.store.UpdateTaskProof(ctx, taskID, proof)
}

// ResolvePixelHashFromIngestion resolves pixel hash from ingestion record
func (s *TaskServiceExtended) ResolvePixelHashFromIngestion(rec interface{}, normalize func([]byte) []byte) []byte {
	// This would need proper ingestion record type
	// TODO: Implement pixel hash resolution logic from original server.go
	return nil
}
