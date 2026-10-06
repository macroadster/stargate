package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// SubmissionReviewInput is the body for review actions.
type SubmissionReviewInput struct {
	Action        string
	Notes         string
	RejectionType string
}

// SubmissionReworkInput is the body for rework.
type SubmissionReworkInput struct {
	Deliverables map[string]interface{}
	Notes        string
}

// SubmissionReviewAuthorizer decides whether the holder of an API key may review
// a submission, returning the wallet it authorized so the caller records the
// identity that was actually accepted.
//
// Defined here rather than alongside its implementation because services cannot
// import app/smart_contract.
type SubmissionReviewAuthorizer interface {
	AuthorizeSubmissionReview(ctx context.Context, apiKey, submissionID string) (string, error)
}

// ReviewActor identifies who is performing a review. Only the API key is taken:
// the wallet is whatever the authorizer resolves from it, so a caller cannot
// name one identity while being authorized as another.
type ReviewActor struct {
	APIKey string
}

// SubmissionReworkAuthorizer decides whether the holder of an API key may rework
// a submission, returning the wallet it authorized.
//
// Separate from SubmissionReviewAuthorizer because the rule is the mirror image:
// review belongs to the wish creator, rework belongs to the claimant who did the
// work. Sharing one interface would invite passing the creator's gate here.
type SubmissionReworkAuthorizer interface {
	AuthorizeSubmissionRework(ctx context.Context, apiKey, submissionID string) (string, error)
}

// ReworkActor identifies who is reworking a submission. Only the API key is
// taken, for the same reason as ReviewActor.
type ReworkActor struct {
	APIKey string
}

// reworkableStatuses are the submission states a rework may start from.
//
// Rejected is excluded deliberately: a rejected claim is finished and the
// claimant resubmits through submit_work rather than editing the old artifact.
// Approved is excluded because approval is final; reopening it would undo a
// completed review, which is the decision irl.1 and ugs hardened (stargate-hs2).
var reworkableStatuses = map[string]bool{
	"pending_review": true,
	"reviewed":       true,
}

// SubmissionService encapsulates submission review/rework domain logic.
type SubmissionService struct {
	store      scstore.Store
	record     EventRecorder
	authz      SubmissionReviewAuthorizer
	reworkAuth SubmissionReworkAuthorizer
}

// NewSubmissionService constructs a SubmissionService.
//
// Both authorizers are required: Review and Rework each refuse outright without
// theirs rather than falling back to trusting the caller. They are separate
// parameters so that wiring one cannot silently be taken as wiring the other.
// Pass nil only where that operation is unreachable, or in tests asserting the
// refusal.
func NewSubmissionService(store scstore.Store, record EventRecorder, authz SubmissionReviewAuthorizer, reworkAuth SubmissionReworkAuthorizer) *SubmissionService {
	return &SubmissionService{store: store, record: record, authz: authz, reworkAuth: reworkAuth}
}

// SetRecorder updates the event sink.

func (s *SubmissionService) emit(evt smart_contract.Event) {
	if s.record != nil {
		s.record(evt)
	}
}

// DefaultSubmissionListLimit is the MCP/REST page size when limit is omitted.
const DefaultSubmissionListLimit = smart_contract.DefaultPageLimit

// SubmissionListResult is the shared MCP + REST list payload.
type SubmissionListResult struct {
	Submissions []smart_contract.Submission `json:"submissions"`
	smart_contract.Page
}

// SubmissionFilterFromArgs maps MCP tool arguments onto the store query.
func SubmissionFilterFromArgs(args map[string]interface{}) smart_contract.SubmissionFilter {
	filter := smart_contract.SubmissionFilter{}
	if args == nil {
		return filter
	}
	if v, ok := args["contract_id"].(string); ok {
		filter.ContractID = strings.TrimSpace(v)
	}
	if v, ok := args["task_id"].(string); ok {
		filter.TaskID = strings.TrimSpace(v)
	}
	switch raw := args["task_ids"].(type) {
	case []string:
		filter.TaskIDs = append(filter.TaskIDs, raw...)
	case []interface{}:
		for _, item := range raw {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				filter.TaskIDs = append(filter.TaskIDs, strings.TrimSpace(s))
			}
		}
	case string:
		for _, part := range strings.Split(raw, ",") {
			if s := strings.TrimSpace(part); s != "" {
				filter.TaskIDs = append(filter.TaskIDs, s)
			}
		}
	}
	if v, ok := args["status"].(string); ok {
		filter.Status = strings.TrimSpace(v)
	}
	q := smart_contract.NewPageQuery(
		intArg(args, "limit", 0),
		intArg(args, "offset", 0),
		stringArg(args, "cursor"),
		stringArg(args, "cursor_date"),
		stringArg(args, "cursor_type"),
		DefaultSubmissionListLimit,
	)
	q.ApplyToSubmission(&filter)
	return filter
}

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, _ := args[key].(string)
	return v
}

func intArg(args map[string]interface{}, key string, def int) int {
	switch v := args[key].(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case json.Number:
		n, err := v.Int64()
		if err == nil {
			return int(n)
		}
	}
	return def
}

// List returns submissions for the shared MCP/REST query (filters + pagination).
func (s *SubmissionService) List(ctx context.Context, filter smart_contract.SubmissionFilter) (*SubmissionListResult, error) {
	limit := smart_contract.NormalizeLimit(filter.Limit, DefaultSubmissionListLimit)
	offset := smart_contract.NormalizeOffset(filter.Offset)
	filter.Limit = smart_contract.OverFetchLimit(limit)
	filter.Offset = offset

	page, err := s.store.ListSubmissions(ctx, filter)
	if err != nil {
		return nil, Fail(http.StatusInternalServerError, err.Error())
	}
	if page == nil {
		page = []smart_contract.Submission{}
	}
	fetched := len(page)
	page = smart_contract.TrimWindow(page, limit)
	lastID, lastDate := "", time.Time{}
	if n := len(page); n > 0 {
		lastID = page[n-1].SubmissionID
		lastDate = page[n-1].CreatedAt
	}
	return &SubmissionListResult{
		Submissions: page,
		Page:        smart_contract.BuildPage(limit, offset, fetched, len(page), lastID, lastDate),
	}, nil
}

// Get returns a submission by ID.
func (s *SubmissionService) Get(ctx context.Context, submissionID string) (smart_contract.Submission, error) {
	submission, err := s.store.GetSubmission(ctx, submissionID)
	if err != nil {
		return smart_contract.Submission{}, Fail(http.StatusInternalServerError, err.Error())
	}
	if submission.SubmissionID == "" {
		return smart_contract.Submission{}, Fail(http.StatusNotFound, "submission not found")
	}
	return submission, nil
}

// Review updates submission status and may auto-resolve rework requests.
//
// Authorization happens here, not in the handlers. Review is the payout gate, so
// the check belongs with the state change it guards: handlers were each
// enforcing it separately, which left the rule optional for any new caller.
func (s *SubmissionService) Review(ctx context.Context, submissionID string, body SubmissionReviewInput, actor ReviewActor) (map[string]interface{}, error) {
	if s.authz == nil {
		// Refuse rather than proceed unauthorized: an unwired service must not be
		// the difference between a guarded payout and an open one.
		return nil, Fail(http.StatusInternalServerError, "submission review authorizer not configured")
	}
	wallet, err := s.authz.AuthorizeSubmissionReview(ctx, actor.APIKey, submissionID)
	if err != nil {
		return nil, Fail(http.StatusForbidden, err.Error())
	}

	if body.Action == "" {
		return nil, Fail(http.StatusBadRequest, "action is required")
	}
	validActions := map[string]bool{"review": true, "approve": true, "reject": true}
	if !validActions[body.Action] {
		return nil, Fail(http.StatusBadRequest, "invalid action. must be: review, approve, or reject")
	}
	var newStatus string
	switch body.Action {
	case "review":
		newStatus = "reviewed"
	case "approve":
		newStatus = "approved"
	case "reject":
		newStatus = "rejected"
	}
	rejectionType, reviewNotes := "", ""
	if body.Action == "reject" {
		reviewNotes = body.Notes
		rejectionType = body.RejectionType
	}
	if err := s.store.UpdateSubmissionStatus(ctx, submissionID, newStatus, reviewNotes, rejectionType); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, Fail(http.StatusNotFound, "submission not found")
		}
		return nil, Fail(http.StatusInternalServerError, err.Error())
	}
	if newStatus == "approved" {
		s.maybeResolveRework(ctx, submissionID)
	}
	// The authorized wallet, not a literal "reviewer": approving a submission
	// releases funds, so the record has to say which key did it.
	s.emit(smart_contract.Event{
		Type: "review", EntityID: submissionID, Actor: wallet,
		Message: fmt.Sprintf("submission %s", body.Action), CreatedAt: time.Now(),
	})
	return map[string]interface{}{
		"message":       fmt.Sprintf("submission %sd successfully", body.Action),
		"status":        newStatus,
		"submission_id": submissionID,
	}, nil
}

// SubmissionTaskID returns the task a submission belongs to, walking its claim
// when TaskID is unset.
//
// Submission.TaskID is omitempty while ClaimID is not, so a submission can
// legitimately arrive identifying its task only through its claim.
// UpdateSubmissionStatus cascades the task via the claim, but callers that
// re-read the submission afterwards still see an empty TaskID, so they need
// this to reach the task.
func SubmissionTaskID(store scstore.Store, sub smart_contract.Submission) (string, error) {
	if taskID := strings.TrimSpace(sub.TaskID); taskID != "" {
		return taskID, nil
	}
	claimID := strings.TrimSpace(sub.ClaimID)
	if claimID == "" {
		return "", fmt.Errorf("submission %s has neither task nor claim", sub.SubmissionID)
	}
	claim, err := store.GetClaim(claimID)
	if err != nil {
		return "", fmt.Errorf("cannot resolve claim %s for submission %s: %w", claimID, sub.SubmissionID, err)
	}
	taskID := strings.TrimSpace(claim.TaskID)
	if taskID == "" {
		return "", fmt.Errorf("claim %s has no task", claimID)
	}
	return taskID, nil
}

func (s *SubmissionService) maybeResolveRework(ctx context.Context, submissionID string) {
	submission, err := s.store.GetSubmission(ctx, submissionID)
	if err != nil {
		return
	}
	taskID, err := SubmissionTaskID(s.store, submission)
	if err != nil {
		return
	}
	task, err := s.store.GetTask(taskID)
	if err != nil || task.ContractID == "" {
		return
	}
	tasks, err := s.store.ListTasks(smart_contract.TaskFilter{ContractID: task.ContractID})
	if err != nil {
		return
	}
	for _, t := range tasks {
		if t.Status != "approved" && t.Status != "published" {
			return
		}
	}
	reworkReqs, err := s.store.GetContractReworkRequests(ctx, task.ContractID)
	if err != nil {
		return
	}
	for _, req := range reworkReqs {
		if req.Status == "open" {
			_ = s.store.ResolveContractReworkRequest(ctx, task.ContractID, req.RequestID)
		}
	}
}

// Rework updates deliverables and resets status to pending_review.
//
// Authorization and the source-state gate both live here. Previously neither
// existed: any self-serve key could overwrite another claimant's deliverables,
// and status was reset unconditionally, so an already approved or rejected
// submission returned to pending_review and could be reviewed again
// (stargate-hs2). Only the claimant may rework, and only from a state where the
// work is still in play.
func (s *SubmissionService) Rework(ctx context.Context, submissionID string, body SubmissionReworkInput, actor ReworkActor) (map[string]interface{}, error) {
	if s.reworkAuth == nil {
		// Refuse rather than proceed unauthorized, as Review does: an unwired
		// service must not be the difference between a guarded artifact and an
		// editable one.
		return nil, Fail(http.StatusInternalServerError, "submission rework authorizer not configured")
	}
	wallet, err := s.reworkAuth.AuthorizeSubmissionRework(ctx, actor.APIKey, submissionID)
	if err != nil {
		return nil, Fail(http.StatusForbidden, err.Error())
	}

	if body.Deliverables == nil && body.Notes == "" {
		return nil, Fail(http.StatusBadRequest, "deliverables or notes must be provided")
	}
	originalSubmission, err := s.store.GetSubmission(ctx, submissionID)
	if err != nil {
		log.Printf("Failed to get submission %s for rework: %v", submissionID, err)
		return nil, Fail(http.StatusInternalServerError, err.Error())
	}
	if originalSubmission.SubmissionID == "" {
		return nil, Fail(http.StatusNotFound, "submission not found")
	}
	if status := strings.ToLower(strings.TrimSpace(originalSubmission.Status)); !reworkableStatuses[status] {
		// Naming the route back is the difference between a usable refusal and a
		// dead end, since rejected work is expected to continue somewhere.
		switch status {
		case "rejected":
			return nil, Fail(http.StatusConflict, "submission was rejected; submit new work for the task instead of reworking this submission")
		case "approved", "accepted":
			return nil, Fail(http.StatusConflict, "submission is already approved and cannot be reopened")
		default:
			return nil, Fail(http.StatusConflict, fmt.Sprintf("submission %s cannot be reworked from status %q", submissionID, originalSubmission.Status))
		}
	}
	if body.Deliverables != nil {
		originalSubmission.Deliverables = body.Deliverables
	}
	if body.Notes != "" {
		if originalSubmission.Deliverables == nil {
			originalSubmission.Deliverables = make(map[string]interface{})
		}
		originalSubmission.Deliverables["rework_notes"] = body.Notes
		originalSubmission.Deliverables["reworked_at"] = time.Now().Format(time.RFC3339)
	}
	originalSubmission.Status = "pending_review"
	if err := s.store.UpdateSubmission(ctx, originalSubmission); err != nil {
		return nil, Fail(http.StatusInternalServerError, err.Error())
	}
	s.emit(smart_contract.Event{
		Type: "rework", EntityID: submissionID, Actor: wallet,
		Message: "submission reworked", CreatedAt: time.Now(),
	})
	return map[string]interface{}{
		"message": "rework submitted successfully", "status": "pending_review", "submission_id": submissionID,
	}, nil
}
