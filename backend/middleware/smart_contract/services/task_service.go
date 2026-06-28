package services

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
	appservices "stargate-backend/services"
	scstore "stargate-backend/storage/smart_contract"
)

// TaskService handles task commitment proofs and pixel-hash resolution.
type TaskService struct {
	store        scstore.Store
	ingestionSvc *appservices.IngestionService
}

// NewTaskService creates a TaskService.
func NewTaskService(store scstore.Store, ingestionSvc *appservices.IngestionService) *TaskService {
	return &TaskService{store: store, ingestionSvc: ingestionSvc}
}

// ResolvePixelHashFromIngestion derives a normalized pixel hash from an ingestion record.
func ResolvePixelHashFromIngestion(rec *appservices.IngestionRecord, normalize func([]byte) []byte) []byte {
	if rec == nil || normalize == nil {
		return nil
	}
	for _, key := range []string{"pixel_hash", "visible_pixel_hash"} {
		if v, ok := rec.Metadata[key].(string); ok {
			if b, err := hex.DecodeString(strings.TrimSpace(v)); err == nil {
				if normalized := normalize(b); normalized != nil {
					return normalized
				}
			}
		}
	}
	if rec.ImageBase64 == "" {
		return nil
	}
	imageBytes, err := base64.StdEncoding.DecodeString(rec.ImageBase64)
	if err != nil {
		return nil
	}
	sum := sha256.Sum256(imageBytes)
	return normalize(sum[:])
}

// UpdateTaskCommitmentProof updates MerkleProof fields after PSBT creation.
func (s *TaskService) UpdateTaskCommitmentProof(ctx context.Context, taskID string, res *bitcoin.PSBTResult, pixelBytes []byte, commitmentTarget string) error {
	if s.store == nil {
		return nil
	}
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
		proof.CommitmentPixelHash = hex.EncodeToString(pixelBytes)
	}
	if res != nil {
		if res.FundingTxID != "" {
			proof.TxID = res.FundingTxID
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
	}
	if proof.ConfirmationStatus == "" {
		proof.ConfirmationStatus = "provisional"
	}
	if proof.SeenAt.IsZero() {
		proof.SeenAt = time.Now()
	}
	if commitmentTarget == "product" {
		proof.CommitmentSource = "product"
	} else if proof.CommitmentSource == "" {
		proof.CommitmentSource = "wish"
	}
	return s.store.UpdateTaskProof(ctx, taskID, proof)
}
