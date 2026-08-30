package services

import (
	"context"
	"net/http"
	"time"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// ClaimService is the application entry for claim / submit-work.
// Persistence still goes through scstore.Store; policy is centralized in
// storage/smart_contract.DecideClaim / DecideSubmit (shared by all dialects).
// Callers (REST/MCP) should prefer this service over raw store methods when
// adding HTTP-level concerns (auth, events).
type ClaimService struct {
	store scstore.Store
}

// NewClaimService constructs a ClaimService.
func NewClaimService(store scstore.Store) *ClaimService {
	return &ClaimService{store: store}
}

// ClaimResult is a claim plus the wish-budget envelope so AIs can see
// how many sats this task locked and what remains.
type ClaimResult struct {
	Claim          smart_contract.Claim
	AmountSats     int64
	WishBudgetSats int64
	AllocatedSats  int64
	RemainingSats  int64
	ClaimableSats  int64
}

// ClaimTask claims a task for walletAddress (idempotent for same wallet).
func (s *ClaimService) ClaimTask(taskID, walletAddress string, estimatedCompletion *time.Time) (smart_contract.Claim, error) {
	res, err := s.ClaimTaskWithAmount(context.Background(), taskID, walletAddress, estimatedCompletion, nil)
	return res.Claim, err
}

// ClaimTaskWithAmount claims a task and optionally locks amount_sats against
// the original wish budget. amountSats may be nil to keep the task's current budget.
func (s *ClaimService) ClaimTaskWithAmount(ctx context.Context, taskID, walletAddress string, estimatedCompletion *time.Time, amountSats *int64) (ClaimResult, error) {
	if s.store == nil {
		return ClaimResult{}, Fail(http.StatusBadRequest, "store unavailable")
	}
	if amountSats != nil {
		snap, err := scstore.LoadBudgetSnapshot(s.store, taskID)
		if err != nil {
			return ClaimResult{}, err
		}
		if err := snap.ValidateAmount(*amountSats); err != nil {
			return ClaimResult{}, Fail(http.StatusBadRequest, err.Error())
		}
	}
	claim, err := s.store.ClaimTask(taskID, walletAddress, estimatedCompletion)
	if err != nil {
		return ClaimResult{}, err
	}
	var snap scstore.BudgetSnapshot
	if amountSats != nil {
		applied, applyErr := scstore.ApplyTaskAmount(ctx, s.store, taskID, *amountSats)
		if applyErr != nil {
			return ClaimResult{}, Fail(http.StatusBadRequest, applyErr.Error())
		}
		snap = applied
	} else if loaded, loadErr := scstore.LoadBudgetSnapshot(s.store, taskID); loadErr == nil {
		snap = loaded
	}
	return ClaimResult{
		Claim:          claim,
		AmountSats:     snap.Task.BudgetSats,
		WishBudgetSats: snap.WishBudgetSats,
		AllocatedSats:  snap.AllocatedSats,
		RemainingSats:  snap.RemainingSats,
		ClaimableSats:  snap.ClaimableSats,
	}, nil
}

// SubmitWork records work against a claim.
func (s *ClaimService) SubmitWork(claimID string, deliverables, proof map[string]interface{}) (smart_contract.Submission, error) {
	if s.store == nil {
		return smart_contract.Submission{}, Fail(http.StatusBadRequest, "store unavailable")
	}
	return s.store.SubmitWork(claimID, deliverables, proof)
}
