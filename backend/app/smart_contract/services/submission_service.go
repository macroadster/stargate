package services

import (
	"context"
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
func (s *SubmissionService) SetRecorder(record EventRecorder) { s.record = record }

func (s *SubmissionService) emit(evt smart_contract.Event) {
	if s.record != nil {
		s.record(evt)
	}
}

// List returns submissions filtered by task IDs, contract, and optional status.
func (s *SubmissionService) List(ctx context.Context, contractID string, taskIDs []string, status string) (map[string]smart_contract.Submission, int, error) {
	var submissions []smart_contract.Submission
	var err error
	if len(taskIDs) > 0 {
		submissions, err = s.store.ListSubmissions(ctx, taskIDs)
	} else if contractID != "" {
		tasks, terr := s.store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
		if terr != nil {
			return nil, 0, Fail(http.StatusInternalServerError, terr.Error())
		}
		taskIDs = make([]string, len(tasks))
		for i, task := range tasks {
			taskIDs[i] = task.TaskID
		}
		submissions, err = s.store.ListSubmissions(ctx, taskIDs)
	} else {
		tasks, terr := s.store.ListTasks(smart_contract.TaskFilter{})
		if terr != nil {
			return nil, 0, Fail(http.StatusInternalServerError, terr.Error())
		}
		taskIDs = make([]string, len(tasks))
		for i, task := range tasks {
			taskIDs[i] = task.TaskID
		}
		submissions, err = s.store.ListSubmissions(ctx, taskIDs)
	}
	if err != nil {
		return nil, 0, Fail(http.StatusInternalServerError, err.Error())
	}
	if status != "" {
		filtered := make([]smart_contract.Submission, 0)
		for _, sub := range submissions {
			if strings.EqualFold(sub.Status, status) {
				filtered = append(filtered, sub)
			}
		}
		submissions = filtered
	}
	submissionMap := make(map[string]smart_contract.Submission)
	for _, sub := range submissions {
		submissionMap[sub.SubmissionID] = sub
	}
	return submissionMap, len(submissions), nil
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
