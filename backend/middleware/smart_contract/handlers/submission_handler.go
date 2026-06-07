package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"stargate-backend/core/smart_contract"
	smartstore "stargate-backend/middleware/smart_contract"
	"stargate-backend/middleware/smart_contract/middleware"
)

// SubmissionHandler handles submission-related HTTP endpoints
type SubmissionHandler struct {
	store smartstore.Store
}

// NewSubmissionHandler creates a new submission handler
func NewSubmissionHandler(store smartstore.Store) *SubmissionHandler {
	return &SubmissionHandler{
		store: store,
	}
}

// Submissions handles GET/POST /submissions
func (h *SubmissionHandler) Submissions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/submissions")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" || path == "/" {
			h.handleListSubmissions(w, r)
			return
		}

		submissionID := parts[0]
		h.handleGetSubmission(w, r, submissionID)
	case http.MethodPost:
		h.handleSubmitWork(w, r)
	default:
		middleware.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleListSubmissions handles GET /submissions
func (h *SubmissionHandler) handleListSubmissions(w http.ResponseWriter, r *http.Request) {
	contractID := r.URL.Query().Get("contract_id")
	taskIDs := h.splitCSV(r.URL.Query().Get("task_ids"))
	status := r.URL.Query().Get("status")

	var submissions []smart_contract.Submission
	var err error

	if len(taskIDs) > 0 {
		submissions, err = h.store.ListSubmissions(r.Context(), taskIDs)
	} else if contractID != "" {
		// Get tasks for contract, then submissions for those tasks
		tasks, err := h.store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
		if err != nil {
			middleware.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		taskIDs = make([]string, len(tasks))
		for i, task := range tasks {
			taskIDs[i] = task.TaskID
		}
		submissions, err = h.store.ListSubmissions(r.Context(), taskIDs)
	} else {
		// Get all tasks, then all submissions
		tasks, err := h.store.ListTasks(smart_contract.TaskFilter{})
		if err != nil {
			middleware.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		taskIDs = make([]string, len(tasks))
		for i, task := range tasks {
			taskIDs[i] = task.TaskID
		}
		submissions, err = h.store.ListSubmissions(r.Context(), taskIDs)
	}

	if err != nil {
		middleware.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Filter by status if provided
	if status != "" {
		filtered := make([]smart_contract.Submission, 0)
		for _, sub := range submissions {
			if strings.EqualFold(sub.Status, status) {
				filtered = append(filtered, sub)
			}
		}
		submissions = filtered
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"submissions": submissions,
		"total":       len(submissions),
	})
}

// handleGetSubmission handles GET /submissions/{id}
func (h *SubmissionHandler) handleGetSubmission(w http.ResponseWriter, r *http.Request, submissionID string) {
	sub, err := h.store.GetSubmission(r.Context(), submissionID)
	if err != nil {
		middleware.Error(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sub)
}

// handleSubmitWork handles POST /submissions
func (h *SubmissionHandler) handleSubmitWork(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClaimID     string                 `json:"claim_id"`
		Deliverables map[string]interface{} `json:"deliverables"`
		Proof       map[string]interface{} `json:"proof"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	sub, err := h.store.SubmitWork(req.ClaimID, req.Deliverables, req.Proof)
	if err != nil {
		middleware.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sub)
}

// splitCSV splits comma-separated values
func (h *SubmissionHandler) splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(value), ",")
}
