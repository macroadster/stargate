package services

import (
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

// ClaimTask claims a task for walletAddress (idempotent for same wallet).
func (s *ClaimService) ClaimTask(taskID, walletAddress string, estimatedCompletion *time.Time) (smart_contract.Claim, error) {
	if s.store == nil {
		return smart_contract.Claim{}, Fail(http.StatusBadRequest, "store unavailable")
	}
	return s.store.ClaimTask(taskID, walletAddress, estimatedCompletion)
}

// SubmitWork records work against a claim.
func (s *ClaimService) SubmitWork(claimID string, deliverables, proof map[string]interface{}) (smart_contract.Submission, error) {
	if s.store == nil {
		return smart_contract.Submission{}, Fail(http.StatusBadRequest, "store unavailable")
	}
	return s.store.SubmitWork(claimID, deliverables, proof)
}
