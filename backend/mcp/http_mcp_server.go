package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"stargate-backend/core/smart_contract"
)

// HTTPMCPServer provides HTTP endpoints for MCP tools
type HTTPMCPServer struct {
	mcpServer *MCPServer
}

// NewHTTPMCPServer creates a new HTTP MCP server
func NewHTTPMCPServer(mcpServer *MCPServer) *HTTPMCPServer {
	return &HTTPMCPServer{
		mcpServer: mcpServer,
	}
}

// MCPRequest represents an incoming MCP tool call via HTTP
type MCPRequest struct {
	Tool      string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// MCPResponse represents response from an MCP tool call
type MCPResponse struct {
	Success bool        `json:"success"`
	Result  interface{} `json:"result,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// RegisterRoutes registers HTTP MCP endpoints
func (h *HTTPMCPServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/mcp/tools", h.authWrap(h.handleListTools))
	mux.HandleFunc("/mcp/call", h.authWrap(h.handleToolCall))
	mux.HandleFunc("/mcp/", h.authWrap(h.handleToolCall))
}

func (h *HTTPMCPServer) authWrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check API key if configured
		if h.mcpServer.apiKey != "" {
			key := r.Header.Get("X-API-Key")
			if key == "" {
				http.Error(w, "Missing API key", http.StatusUnauthorized)
				return
			}
			if key != h.mcpServer.apiKey {
				http.Error(w, "Invalid API key", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}

func (h *HTTPMCPServer) handleListTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Create a simple list of available tools
	tools := map[string]interface{}{
		"contracts": []string{
			"list_contracts",
			"get_contract",
			"get_contract_funding",
		},
		"tasks": []string{
			"list_tasks",
			"get_task",
			"claim_task",
			"submit_work",
			"get_task_proof",
			"get_task_status",
		},
		"skills": []string{
			"list_skills",
		},
		"proposals": []string{
			"list_proposals",
			"get_proposal",
			"create_proposal",
			"approve_proposal",
			"publish_proposal",
		},
		"submissions": []string{
			"list_submissions",
			"get_submission",
			"review_submission",
			"rework_submission",
		},
		"events": []string{
			"list_events",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tools": tools,
		"total": 18, // Total number of tools available
	})
}

func (h *HTTPMCPServer) handleToolCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, "Invalid JSON: "+err.Error())
		return
	}

	if req.Tool == "" {
		h.sendError(w, "Tool name is required")
		return
	}

	// Call the appropriate tool handler directly
	ctx := context.Background()
	result, err := h.callToolDirect(ctx, req.Tool, req.Arguments)
	if err != nil {
		h.sendError(w, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MCPResponse{
		Success: true,
		Result:  result,
	})
}

func (h *HTTPMCPServer) callToolDirect(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	store := h.mcpServer.store

	switch toolName {
	case "list_contracts":
		status := ""
		if s, ok := args["status"].(string); ok {
			status = s
		}
		var skills []string
		if skillSlice, ok := args["skills"].([]interface{}); ok {
			for _, skill := range skillSlice {
				if skillStr, ok := skill.(string); ok {
					skills = append(skills, skillStr)
				}
			}
		}
		contracts, err := store.ListContracts(status, skills)
		if err != nil {
			return nil, fmt.Errorf("Failed to list contracts: %v", err)
		}
		return map[string]interface{}{
			"contracts":   contracts,
			"total_count": len(contracts),
		}, nil

	case "get_contract":
		contractID, ok := args["contract_id"].(string)
		if !ok {
			return nil, fmt.Errorf("contract_id is required")
		}
		contract, err := store.GetContract(contractID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get contract: %v", err)
		}
		return contract, nil

	case "get_contract_funding":
		contractID, ok := args["contract_id"].(string)
		if !ok {
			return nil, fmt.Errorf("contract_id is required")
		}
		contract, proofs, err := store.ContractFunding(contractID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get contract funding: %v", err)
		}
		return map[string]interface{}{
			"contract": contract,
			"proofs":   proofs,
		}, nil

	case "list_tasks":
		var skills []string
		if skillSlice, ok := args["skills"].([]interface{}); ok {
			for _, skill := range skillSlice {
				if skillStr, ok := skill.(string); ok {
					skills = append(skills, skillStr)
				}
			}
		}

		filter := smart_contract.TaskFilter{
			Skills:        skills,
			MaxDifficulty: h.toString(args["max_difficulty"]),
			Status:        h.toString(args["status"]),
			Limit:         int(h.toInt64(args["limit"])),
			Offset:        int(h.toInt64(args["offset"])),
			MinBudgetSats: h.toInt64(args["min_budget_sats"]),
			ContractID:    h.toString(args["contract_id"]),
			ClaimedBy:     h.toString(args["claimed_by"]),
		}

		if filter.Limit == 0 {
			filter.Limit = 50
		}

		tasks, err := store.ListTasks(filter)
		if err != nil {
			return nil, fmt.Errorf("Failed to list tasks: %v", err)
		}

		// Get submissions for these tasks
		var taskIDs []string
		for _, t := range tasks {
			taskIDs = append(taskIDs, t.TaskID)
		}
		subs, _ := store.ListSubmissions(ctx, taskIDs)

		return map[string]interface{}{
			"tasks":         tasks,
			"total_matches": len(tasks),
			"submissions":   subs,
		}, nil

	case "get_task":
		taskID, ok := args["task_id"].(string)
		if !ok {
			return nil, fmt.Errorf("task_id is required")
		}
		task, err := store.GetTask(taskID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get task: %v", err)
		}
		return task, nil

	case "claim_task":
		taskID, ok := args["task_id"].(string)
		if !ok {
			return nil, fmt.Errorf("task_id is required")
		}
		aiIdentifier, ok := args["ai_identifier"].(string)
		if !ok {
			return nil, fmt.Errorf("ai_identifier is required")
		}

		claim, err := store.ClaimTask(taskID, aiIdentifier, nil)
		if err != nil {
			return nil, fmt.Errorf("Failed to claim task: %v", err)
		}

		return map[string]interface{}{
			"success":    true,
			"claim_id":   claim.ClaimID,
			"expires_at": claim.ExpiresAt,
			"message":    "Task reserved. Submit work before expiration.",
		}, nil

	case "list_skills":
		tasks, err := store.ListTasks(smart_contract.TaskFilter{})
		if err != nil {
			return nil, fmt.Errorf("Failed to list tasks: %v", err)
		}

		skillSet := make(map[string]struct{})
		// Add default skills
		skillSet["contract_bidding"] = struct{}{}
		skillSet["get_pending_transactions"] = struct{}{}

		for _, t := range tasks {
			for _, skill := range t.Skills {
				key := h.toString(skill)
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

		return map[string]interface{}{
			"skills": skills,
			"count":  len(skills),
		}, nil

	default:
		return nil, fmt.Errorf("tool '%s' not found", toolName)
	}
}

func (h *HTTPMCPServer) sendError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(MCPResponse{
		Success: false,
		Error:   message,
	})
}

// Helper functions
func (h *HTTPMCPServer) toString(val interface{}) string {
	if str, ok := val.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", val)
}

func (h *HTTPMCPServer) toInt64(val interface{}) int64 {
	if i, ok := val.(int64); ok {
		return i
	}
	if i, ok := val.(int); ok {
		return int64(i)
	}
	if f, ok := val.(float64); ok {
		return int64(f)
	}
	return 0
}
