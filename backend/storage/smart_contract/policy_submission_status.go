package smart_contract

import (
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
)

// SubmissionCascade is the side-effect on claim/task after a status change.
type SubmissionCascade int

const (
	SubmissionCascadeNone SubmissionCascade = iota
	// SubmissionCascadeApprove: task → approved, claim → complete.
	SubmissionCascadeApprove
	// SubmissionCascadeReject: task → available (clear claim fields), claim → rejected.
	SubmissionCascadeReject
)

// SubmissionStatusPlan is the dialect-agnostic result of DecideSubmissionStatusUpdate.
type SubmissionStatusPlan struct {
	Status          string
	RejectionReason string
	RejectionType   string
	RejectedAt      *time.Time
	Cascade         SubmissionCascade

	// After cascade — intended task/claim statuses (for Memory apply / docs).
	TaskStatus   string
	ClaimStatus  string
	ClearClaimOnTask bool // claimed_by / claimed_at / claim_expires / active_claim_id
}

// DecideSubmissionStatusUpdate encodes review cascade rules once for all dialects.
// status is the new submission status (approved/accepted/rejected/reviewed/…).
func DecideSubmissionStatusUpdate(status, reviewerNotes, rejectionType string, now time.Time) SubmissionStatusPlan {
	if now.IsZero() {
		now = time.Now()
	}
	st := strings.TrimSpace(status)
	plan := SubmissionStatusPlan{Status: st}

	switch strings.ToLower(st) {
	case "rejected":
		note := strings.TrimSpace(reviewerNotes)
		rej := strings.TrimSpace(rejectionType)
		plan.RejectionReason = note
		plan.RejectionType = rej
		t := now
		plan.RejectedAt = &t
		plan.Cascade = SubmissionCascadeReject
		plan.TaskStatus = smart_contract.TaskStatusAvailable
		plan.ClaimStatus = smart_contract.ClaimStatusRejected
		plan.ClearClaimOnTask = true
	case "accepted", "approved":
		plan.RejectionReason = ""
		plan.RejectionType = ""
		plan.RejectedAt = nil
		plan.Cascade = SubmissionCascadeApprove
		plan.TaskStatus = smart_contract.TaskStatusApproved
		plan.ClaimStatus = smart_contract.ClaimStatusComplete
	default:
		// reviewed, pending_review, etc. — no claim/task cascade
		plan.RejectionReason = ""
		plan.RejectionType = ""
		plan.RejectedAt = nil
		plan.Cascade = SubmissionCascadeNone
	}
	return plan
}
