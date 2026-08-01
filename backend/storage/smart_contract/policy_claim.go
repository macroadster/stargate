package smart_contract

import (
	"fmt"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
)

// ClaimAction is the outcome of DecideClaim.
type ClaimAction int

const (
	// ClaimActionIdempotent: same wallet already holds an active claim — return it.
	ClaimActionIdempotent ClaimAction = iota
	// ClaimActionCreate: insert a new claim and mark the task claimed.
	ClaimActionCreate
)

// ClaimInput is everything pure claim policy needs (no DB).
type ClaimInput struct {
	TaskID              string
	TaskStatus          string
	Wallet              string
	ExistingClaims      []smart_contract.Claim // claims for this task only
	CurrentProof        *smart_contract.MerkleProof
	CurrentContractor   string // task.ContractorWallet if tracked separately
	ClaimTTL            time.Duration
	Now                 time.Time
	NewClaimID          string // pre-generated CLAIM-… id for Create
}

// ClaimPlan is the dialect-agnostic result of DecideClaim.
type ClaimPlan struct {
	Action ClaimAction
	Claim  smart_contract.Claim

	// Task row updates (both actions ensure claimed + claimed_by).
	TaskStatus   string
	ClaimedBy    string
	ClaimedAt    *time.Time // nil on pure idempotent when only backfilling wallet
	ClaimExpires *time.Time

	// Merkle proof / contractor wallet backfill.
	Proof              *smart_contract.MerkleProof
	UpdateProof        bool
	ContractorWallet   string
}

// DecideClaim encodes claim rules once for Memory / SQLite / Postgres.
//
// Rules:
//  1. Non-empty wallet required.
//  2. Active (non-expired) claim by same wallet → idempotent return + ensure task/proof.
//  3. Active claim by another wallet → ErrTaskTaken.
//  4. Terminal task statuses cannot take a new claim → ErrTaskUnavailable.
//  5. Otherwise create a new active claim with TTL.
//
// Terminal (blocked for new claims): submitted, approved, published, completed, pending_review.
// Allowed for new claims: available, claimed (e.g. after prior claim expired), empty, and other non-terminal.
func DecideClaim(in ClaimInput) (ClaimPlan, error) {
	wallet := strings.TrimSpace(in.Wallet)
	if wallet == "" {
		return ClaimPlan{}, fmt.Errorf("wallet address required")
	}
	if strings.TrimSpace(in.TaskID) == "" {
		return ClaimPlan{}, ErrTaskNotFound
	}

	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	var idempotent *smart_contract.Claim
	for i := range in.ExistingClaims {
		c := in.ExistingClaims[i]
		if c.Status != smart_contract.ClaimStatusActive && c.Status != "active" {
			continue
		}
		if !c.ExpiresAt.IsZero() && !now.Before(c.ExpiresAt) {
			continue // expired
		}
		if strings.EqualFold(c.AiIdentifier, wallet) {
			cp := c
			idempotent = &cp
			break
		}
		return ClaimPlan{}, ErrTaskTaken
	}

	if idempotent != nil {
		proof, update := mergeContractorWallet(in.CurrentProof, in.CurrentContractor, wallet)
		return ClaimPlan{
			Action:           ClaimActionIdempotent,
			Claim:            *idempotent,
			TaskStatus:       smart_contract.TaskStatusClaimed,
			ClaimedBy:        idempotent.AiIdentifier,
			Proof:            proof,
			UpdateProof:      update,
			ContractorWallet: wallet,
		}, nil
	}

	if !taskStatusAllowsNewClaim(in.TaskStatus) {
		return ClaimPlan{}, ErrTaskUnavailable
	}

	ttl := in.ClaimTTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	claimID := strings.TrimSpace(in.NewClaimID)
	if claimID == "" {
		claimID = fmt.Sprintf("CLAIM-%d", now.UnixNano())
	}
	expires := now.Add(ttl)
	claim := smart_contract.Claim{
		ClaimID:      claimID,
		TaskID:       in.TaskID,
		AiIdentifier: in.Wallet, // preserve original casing from caller
		Status:       smart_contract.ClaimStatusActive,
		ExpiresAt:    expires,
		CreatedAt:    now,
	}
	proof, update := mergeContractorWallet(in.CurrentProof, in.CurrentContractor, wallet)
	return ClaimPlan{
		Action:           ClaimActionCreate,
		Claim:            claim,
		TaskStatus:       smart_contract.TaskStatusClaimed,
		ClaimedBy:        claim.AiIdentifier,
		ClaimedAt:        &claim.CreatedAt,
		ClaimExpires:     &expires,
		Proof:            proof,
		UpdateProof:      update,
		ContractorWallet: wallet,
	}, nil
}

func taskStatusAllowsNewClaim(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case smart_contract.TaskStatusSubmitted,
		smart_contract.TaskStatusApproved,
		smart_contract.TaskStatusPublished,
		smart_contract.TaskStatusCompleted,
		smart_contract.SubmissionStatusPendingReview: // "pending_review"
		return false
	default:
		// available, claimed (stale), empty, funded, etc.
		return true
	}
}

// mergeContractorWallet returns an updated proof when wallet should be written.
func mergeContractorWallet(existing *smart_contract.MerkleProof, currentContractor, wallet string) (*smart_contract.MerkleProof, bool) {
	wallet = strings.TrimSpace(wallet)
	if wallet == "" {
		return existing, false
	}
	cur := strings.TrimSpace(currentContractor)
	if existing != nil && strings.TrimSpace(existing.ContractorWallet) != "" {
		cur = strings.TrimSpace(existing.ContractorWallet)
	}
	if strings.EqualFold(cur, wallet) {
		return existing, false
	}
	proof := existing
	if proof == nil {
		proof = &smart_contract.MerkleProof{}
	} else {
		// shallow copy so callers can mutate safely
		cp := *proof
		proof = &cp
	}
	proof.ContractorWallet = wallet
	return proof, true
}
