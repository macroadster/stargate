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

// SubmissionService encapsulates submission review/rework domain logic.
type SubmissionService struct {
	store  scstore.Store
	record EventRecorder
}

// NewSubmissionService constructs a SubmissionService.
func NewSubmissionService(store scstore.Store, record EventRecorder) *SubmissionService {
	return &SubmissionService{store: store, record: record}
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
func (s *SubmissionService) Review(ctx context.Context, submissionID string, body SubmissionReviewInput) (map[string]interface{}, error) {
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
	s.emit(smart_contract.Event{
		Type: "review", EntityID: submissionID, Actor: "reviewer",
		Message: fmt.Sprintf("submission %s", body.Action), CreatedAt: time.Now(),
	})
	return map[string]interface{}{
		"message":       fmt.Sprintf("submission %sd successfully", body.Action),
		"status":        newStatus,
		"submission_id": submissionID,
	}, nil
}

func (s *SubmissionService) maybeResolveRework(ctx context.Context, submissionID string) {
	submission, err := s.store.GetSubmission(ctx, submissionID)
	if err != nil || submission.TaskID == "" {
		return
	}
	task, err := s.store.GetTask(submission.TaskID)
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
func (s *SubmissionService) Rework(ctx context.Context, submissionID string, body SubmissionReworkInput) (map[string]interface{}, error) {
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
		Type: "rework", EntityID: submissionID, Actor: "claimant",
		Message: "submission reworked", CreatedAt: time.Now(),
	})
	return map[string]interface{}{
		"message": "rework submitted successfully", "status": "pending_review", "submission_id": submissionID,
	}, nil
}
