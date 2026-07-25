package smart_contract

import (
	"fmt"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
)

// SubmitInput is pure submit-work policy input.
type SubmitInput struct {
	Claim                      smart_contract.Claim
	ExistingSubmissionStatuses []string // statuses of prior submissions for this claim
	Deliverables               map[string]interface{}
	Proof                      map[string]interface{}
	Now                        time.Time
	NewSubmissionID            string
}

// SubmitPlan is the dialect-agnostic result of DecideSubmit.
type SubmitPlan struct {
	// MarkClaimExpired is set when the claim is past expires_at.
	MarkClaimExpired bool
	// ReactivateClaim is set when resubmitting after rejected/reviewed.
	ReactivateClaim bool
	Submission      smart_contract.Submission
	TaskID          string
}

// DecideSubmit encodes submit-work rules once for all dialects.
//
// Rules:
//  1. Claim status must be active or submitted.
//  2. If submitted, at least one prior submission must be rejected or reviewed (resubmit path).
//  3. Expired claims → MarkClaimExpired + error.
//  4. Strip external "status" keys from deliverables/proof.
//  5. New submission status is pending_review; claim+task become submitted.
func DecideSubmit(in SubmitInput) (SubmitPlan, error) {
	claim := in.Claim
	if strings.TrimSpace(claim.ClaimID) == "" {
		return SubmitPlan{}, ErrClaimNotFound
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	status := strings.ToLower(strings.TrimSpace(claim.Status))
	if status != smart_contract.ClaimStatusActive && status != "active" &&
		status != smart_contract.ClaimStatusSubmitted && status != "submitted" {
		return SubmitPlan{}, fmt.Errorf("claim %s not active or submitted", claim.ClaimID)
	}

	reactivate := false
	if status == smart_contract.ClaimStatusSubmitted || status == "submitted" {
		canResubmit := false
		for _, st := range in.ExistingSubmissionStatuses {
			st = strings.ToLower(strings.TrimSpace(st))
			if st == smart_contract.SubmissionStatusRejected || st == "rejected" ||
				st == smart_contract.SubmissionStatusReviewed || st == "reviewed" {
				canResubmit = true
				break
			}
		}
		if !canResubmit {
			return SubmitPlan{}, fmt.Errorf("claim %s already submitted with no eligible resubmission", claim.ClaimID)
		}
		reactivate = true
	}

	if !claim.ExpiresAt.IsZero() && now.After(claim.ExpiresAt) {
		return SubmitPlan{MarkClaimExpired: true, TaskID: claim.TaskID}, fmt.Errorf("claim %s expired", claim.ClaimID)
	}

	// Safeguard: external tools must not set internal status fields.
	deliv := in.Deliverables
	if deliv != nil {
		delete(deliv, "status")
	}
	proof := in.Proof
	if proof != nil {
		delete(proof, "status")
	}

	subID := strings.TrimSpace(in.NewSubmissionID)
	if subID == "" {
		subID = fmt.Sprintf("SUB-%d", now.UnixNano())
	}
	sub := smart_contract.Submission{
		SubmissionID:    subID,
		ClaimID:         claim.ClaimID,
		TaskID:          claim.TaskID,
		Status:          smart_contract.SubmissionStatusPendingReview,
		Deliverables:    deliv,
		CompletionProof: proof,
		CreatedAt:       now,
	}
	return SubmitPlan{
		ReactivateClaim: reactivate,
		Submission:      sub,
		TaskID:          claim.TaskID,
	}, nil
}
