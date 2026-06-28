package smart_contract

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
)

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/tasks")
	path = strings.Trim(path, "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" {
			filter := smart_contract.TaskFilter{
				Skills:        splitCSV(r.URL.Query().Get("skills")),
				MaxDifficulty: r.URL.Query().Get("max_difficulty"),
				Status:        r.URL.Query().Get("status"),
				Limit:         intFromQuery(r, "limit", 50),
				Offset:        intFromQuery(r, "offset", 0),
				MinBudgetSats: int64FromQuery(r, "min_budget_sats", 0),
				ContractID:    r.URL.Query().Get("contract_id"),
				ClaimedBy:     r.URL.Query().Get("claimed_by"),
			}
			tasks, err := s.store.ListTasks(filter)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			// hydrate submissions for these tasks
			var taskIDs []string
			for _, t := range tasks {
				taskIDs = append(taskIDs, t.TaskID)
			}
			subs, _ := s.store.ListSubmissions(r.Context(), taskIDs)
			JSON(w, http.StatusOK, map[string]interface{}{
				"tasks":         tasks,
				"total_matches": len(tasks),
				"submissions":   subs,
			})
			return
		}

		parts := strings.Split(path, "/")
		taskID := parts[0]

		// Nested resources
		if len(parts) > 1 && parts[1] == "merkle-proof" {
			proof, err := s.store.GetTaskProof(taskID)
			if err != nil {
				Error(w, http.StatusNotFound, err.Error())
				return
			}
			JSON(w, http.StatusOK, proof)
			return
		}

		if len(parts) > 1 && parts[1] == "status" {
			status, err := s.store.TaskStatus(taskID)
			if err != nil {
				Error(w, http.StatusNotFound, err.Error())
				return
			}
			JSON(w, http.StatusOK, status)
			return
		}

		task, err := s.store.GetTask(taskID)
		if err != nil {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		JSON(w, http.StatusOK, task)
	case http.MethodPost:
		parts := strings.Split(path, "/")
		if len(parts) < 2 {
			Error(w, http.StatusBadRequest, "expected /tasks/{task_id}/claim")
			return
		}
		taskID := parts[0]
		switch parts[1] {
		case "claim":
			s.handleClaimTask(w, r, taskID)
		default:
			Error(w, http.StatusNotFound, "unknown task action")
		}
	default:
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleClaimTask(w http.ResponseWriter, r *http.Request, taskID string) {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "application/json") {
		Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	var body struct {
		EstimatedCompletion *time.Time `json:"estimated_completion,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}

	if task, err := s.store.GetTask(taskID); err == nil {
		if strings.TrimSpace(task.ContractID) != "" {
			if contract, err := s.store.GetContract(task.ContractID); err == nil {
				status := strings.ToLower(strings.TrimSpace(contract.Status))
				if status == "confirmed" || status == "published" {
					Error(w, http.StatusConflict, "task claims closed for confirmed contract")
					return
				}
			}
			if proposals, err := s.store.ListProposals(r.Context(), smart_contract.ProposalFilter{ContractID: task.ContractID}); err == nil {
				for _, p := range proposals {
					if strings.EqualFold(strings.TrimSpace(p.Status), "confirmed") {
						Error(w, http.StatusConflict, "task claims closed for confirmed proposal")
						return
					}
				}
			}
		}
	}

	walletAddress := ""
	if s.apiKeys != nil {
		key := r.Header.Get("X-API-Key")
		if rec, ok := s.apiKeys.Get(key); ok {
			walletAddress = strings.TrimSpace(rec.Wallet)
		}
	}
	if walletAddress == "" {
		Error(w, http.StatusBadRequest, "wallet address required - please bind wallet to API key using /api/auth/verify")
		return
	}

	claim, err := s.store.ClaimTask(taskID, walletAddress, body.EstimatedCompletion)
	if err != nil {
		if err == ErrTaskNotFound {
			// Attempt to publish tasks lazily from proposals that reference this task id, then retry.
			if s.tryPublishTasksForTaskID(r.Context(), taskID) == nil {
				if retry, retryErr := s.store.ClaimTask(taskID, walletAddress, body.EstimatedCompletion); retryErr == nil {
					claim = retry
					err = nil
				} else {
					err = retryErr
				}
			}
			if err == nil {
				goto claim_success
			}
			if err == ErrTaskNotFound {
				Error(w, http.StatusNotFound, err.Error())
				return
			}
		}
		if err == ErrTaskTaken || err == ErrTaskUnavailable || err.Error() == ErrTaskUnavailable.Error() {
			Error(w, http.StatusConflict, err.Error())
			return
		}
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
claim_success:

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"claim_id":   claim.ClaimID,
		"expires_at": claim.ExpiresAt,
		"message":    "Task reserved. Submit work before expiration.",
	})

	s.recordEvent(smart_contract.Event{
		Type:      "claim",
		EntityID:  taskID,
		Actor:     walletAddress,
		Message:   "task claimed",
		CreatedAt: time.Now(),
	})
}

func (s *Server) handleClaims(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/claims/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		Error(w, http.StatusBadRequest, "claim id required")
		return
	}
	claimID := parts[0]

	if len(parts) < 2 || parts[1] != "submit" {
		Error(w, http.StatusNotFound, "unknown claim action")
		return
	}

	if r.Method != http.MethodPost {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "application/json") {
		Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}

	var body struct {
		Deliverables    map[string]interface{} `json:"deliverables"`
		CompletionProof map[string]interface{} `json:"completion_proof"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}

	sub, err := s.store.SubmitWork(claimID, body.Deliverables, body.CompletionProof)
	if err != nil {
		if err == ErrClaimNotFound {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	actor := "claimant"
	if who, ok := body.Deliverables["submitted_by"].(string); ok && who != "" {
		actor = who
	}
	s.recordEvent(smart_contract.Event{
		Type:      "submit",
		EntityID:  claimID,
		Actor:     actor,
		Message:   "submission created",
		CreatedAt: time.Now(),
	})

	JSON(w, http.StatusOK, sub)
}

// handleSkills returns a unique list of skills across all tasks for quick capability checks by agents.
func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tasks, err := s.store.ListTasks(smart_contract.TaskFilter{})
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	skillSet := make(map[string]struct{})
	// Add default skills
	skillSet["contract_bidding"] = struct{}{}
	skillSet["get_open_contracts"] = struct{}{}

	for _, t := range tasks {
		for _, skill := range t.Skills {
			key := strings.ToLower(strings.TrimSpace(skill))
			if key == "" {
				continue
			}
			skillSet[key] = struct{}{}
		}
	}
	skills := make([]string, 0, len(skillSet))
	for k := range skillSet {
		skills = append(skills, k)
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"skills": skills,
		"count":  len(skills),
	})
}

// handleDiscover advertises API endpoints and MCP tool surface for clients.
func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	base := fmt.Sprintf("http://%s", r.Host)
	resp := map[string]interface{}{
		"version": "1.0",
		"base_urls": map[string]string{
			"api": base + "/api/smart_contract",
			"mcp": base + "/mcp",
		},
		"endpoints": []string{
			"/api/smart_contract/contracts",
			"/api/smart_contract/tasks",
			"/api/smart_contract/claims",
			"/api/smart_contract/submissions",
			"/api/smart_contract/events",
			"/api/open-contracts",
		},
		"tools": []string{
			"list_contracts", "get_contract", "get_contract_funding", "get_open_contracts",
			"get_contract_rework_requests", "create_contract_rework_request",
			"list_tasks", "get_task", "claim_task", "submit_work", "get_task_proof", "get_task_status",
			"list_skills",
			"list_proposals", "get_proposal", "create_proposal", "approve_proposal", "publish_proposal",
			"list_submissions", "get_submission", "review_submission", "rework_submission",
			"list_events",
			"scan_image", "scan_transaction", "scan_block", "extract_message", "get_scanner_info",
		},
		"authentication": map[string]string{
			"type":        "api_key",
			"header_name": "X-API-Key",
			"required":    fmt.Sprintf("%t", s.apiKeys != nil),
		},
		"rate_limits": map[string]interface{}{
			"enabled":       false,
			"notes":         "rate limiting planned; not enforced by default",
			"recommended":   "10 rps claim, 5 rps submit (see roadmap)",
			"burst_example": 100,
		},
	}
	JSON(w, http.StatusOK, resp)
}

func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func intFromQuery(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}

func int64FromQuery(r *http.Request, key string, def int64) int64 {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return def
	}
	return v
}

func includeConfirmed(r *http.Request) bool {
	raw := strings.TrimSpace(r.URL.Query().Get("include_confirmed"))
	if raw == "" {
		return false
	}
	return strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes") || raw == "1"
}
