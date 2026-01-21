package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"stargate-backend/core/smart_contract"
	smartstore "stargate-backend/middleware/smart_contract"
	"stargate-backend/middleware/smart_contract/middleware"
	"stargate-backend/services"
)

// TaskHandler handles task-related HTTP endpoints
type TaskHandler struct {
	store        smartstore.Store
	ingestionSvc *services.IngestionService
}

// NewTaskHandler creates a new task handler
func NewTaskHandler(store smartstore.Store, ingestionSvc *services.IngestionService) *TaskHandler {
	return &TaskHandler{
		store:        store,
		ingestionSvc: ingestionSvc,
	}
}

// Tasks handles GET/POST /tasks
func (h *TaskHandler) Tasks(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/tasks")
	path = strings.Trim(path, "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" {
			h.handleListTasks(w, r)
			return
		}

		parts := strings.Split(path, "/")
		taskID := parts[0]

		// Nested resources
		if len(parts) > 1 && parts[1] == "merkle-proof" {
			h.handleTaskProof(w, r, taskID)
			return
		}

		if len(parts) > 1 && parts[1] == "status" {
			h.handleTaskStatus(w, r, taskID)
			return
		}

		h.handleGetTask(w, r, taskID)
	case http.MethodPost:
		parts := strings.Split(path, "/")
		if len(parts) < 2 {
			middleware.Error(w, http.StatusBadRequest, "expected /tasks/{task_id}/claim")
			return
		}
		taskID := parts[0]
		switch parts[1] {
		case "claim":
			h.handleClaimTask(w, r, taskID)
		default:
			middleware.Error(w, http.StatusNotFound, "unknown task action")
		}
	default:
		middleware.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleListTasks handles GET /tasks
func (h *TaskHandler) handleListTasks(w http.ResponseWriter, r *http.Request) {
	filter := smart_contract.TaskFilter{
		Skills:        h.splitCSV(r.URL.Query().Get("skills")),
		MaxDifficulty: r.URL.Query().Get("max_difficulty"),
		Status:        r.URL.Query().Get("status"),
		Limit:         h.intFromQuery(r, "limit", 50),
		Offset:        h.intFromQuery(r, "offset", 0),
		MinBudgetSats: h.int64FromQuery(r, "min_budget_sats", 0),
		ContractID:    r.URL.Query().Get("contract_id"),
		ClaimedBy:     r.URL.Query().Get("claimed_by"),
	}

	tasks, err := h.store.ListTasks(filter)
	if err != nil {
		middleware.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// hydrate submissions for these tasks
	var taskIDs []string
	for _, t := range tasks {
		taskIDs = append(taskIDs, t.TaskID)
	}
	subs, _ := h.store.ListSubmissions(r.Context(), taskIDs)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tasks":         tasks,
		"total_matches": len(tasks),
		"submissions":   subs,
	})
}

// handleGetTask handles GET /tasks/{id}
func (h *TaskHandler) handleGetTask(w http.ResponseWriter, r *http.Request, taskID string) {
	task, err := h.store.GetTask(taskID)
	if err != nil {
		middleware.Error(w, http.StatusNotFound, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

// handleTaskProof handles GET /tasks/{id}/merkle-proof
func (h *TaskHandler) handleTaskProof(w http.ResponseWriter, r *http.Request, taskID string) {
	proof, err := h.store.GetTaskProof(taskID)
	if err != nil {
		middleware.Error(w, http.StatusNotFound, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proof)
}

// handleTaskStatus handles GET /tasks/{id}/status
func (h *TaskHandler) handleTaskStatus(w http.ResponseWriter, r *http.Request, taskID string) {
	status, err := h.store.TaskStatus(taskID)
	if err != nil {
		middleware.Error(w, http.StatusNotFound, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleClaimTask handles POST /tasks/{id}/claim
func (h *TaskHandler) handleClaimTask(w http.ResponseWriter, r *http.Request, taskID string) {
	// TODO: Implement task claiming logic
	// This needs to be extracted from the original handleClaimTask function
	middleware.Error(w, http.StatusNotImplemented, "task claiming not yet extracted")
}

// Helper functions
func (h *TaskHandler) splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(value), ",")
}

func (h *TaskHandler) intFromQuery(r *http.Request, key string, defaultValue int) int {
	if value := r.URL.Query().Get(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func (h *TaskHandler) int64FromQuery(r *http.Request, key string, defaultValue int64) int64 {
	if value := r.URL.Query().Get(key); value != "" {
		if intValue, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intValue
		}
	}
	return defaultValue
}
