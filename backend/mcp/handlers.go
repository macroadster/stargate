package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
)

func (h *HTTPMCPServer) handleListTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeHTTPError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "Use GET /mcp/tools.")
		return
	}

	tools := h.getToolSchemas()
	toolNames := make([]string, 0, len(tools))
	for name := range tools {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)
	base := h.externalBaseURL(r)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tools":      tools,
		"tool_names": toolNames,
		"total":      len(tools),
		"categories": map[string]bool{
			"discovery": true,
			"write":     true,
			"utility":   true,
		},
		"http_endpoints": map[string]interface{}{
			"inscribe": map[string]interface{}{
				"method":          "POST",
				"endpoint":        base + "/api/inscribe",
				"required_fields": []string{"message", "image_base64"},
				"description":     "Create a wish/inscription that seeds a proposal and contract metadata. Requires image payload.",
			},
		},
		"endpoints": []string{
			"/api/inscribe",
			"/api/smart_contract/contracts",
			"/api/smart_contract/tasks",
			"/api/smart_contract/claims",
			"/api/smart_contract/submissions",
			"/api/smart_contract/events",
			"/api/open-contracts",
		},
	})
}

func (h *HTTPMCPServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if r.URL.Path == "/mcp" || r.URL.Path == "/mcp/" {
			h.handleJSONRPC(w, r)
			return
		}
		h.writeHTTPError(w, http.StatusNotFound, "MCP_ENDPOINT_NOT_FOUND", "Unknown MCP endpoint", "Use /mcp for JSON-RPC or /mcp/docs for HTTP endpoints.")
		return
	}
	if r.Method != http.MethodGet {
		h.writeHTTPError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "Use GET /mcp for metadata or POST /mcp for MCP JSON-RPC.")
		return
	}
	if r.URL.Path != "/mcp" && r.URL.Path != "/mcp/" {
		h.writeHTTPError(w, http.StatusNotFound, "MCP_ENDPOINT_NOT_FOUND", "Unknown MCP endpoint", "See available MCP endpoints at /mcp/docs or /mcp/discover.")
		return
	}

	base := h.externalBaseURL(r)
	taskCount := 0
	contractCount := 0
	if h.store != nil {
		if tasks, err := h.store.ListTasks(smart_contract.TaskFilter{}); err == nil {
			taskCount = len(tasks)
		}
		if contracts, err := h.store.ListContracts(smart_contract.ContractFilter{}); err == nil {
			contractCount = len(contracts)
		}
	}
	resp := map[string]interface{}{
		"message": "MCP HTTP server is running. Use /mcp/tools or /mcp/discover to list tools.",
		"links": map[string]string{
			"tools":        base + "/mcp/tools",
			"discover":     base + "/mcp/discover",
			"docs":         base + "/mcp/docs",
			"openapi":      base + "/mcp/openapi.json",
			"health":       base + "/mcp/health",
			"events":       base + "/mcp/events",
			"tool_call":    base + "/mcp/call",
			"mcp_base_url": base + "/mcp",
		},
		"quick_start": []string{
			"GET /mcp/tools to fetch available tools.",
			"POST /mcp/call with {\"tool\": \"list_contracts\"} to execute a tool.",
			"GET /mcp/docs for full examples.",
		},
		"counts": map[string]int{
			"tools":     len(h.getToolSchemas()),
			"contracts": contractCount,
			"tasks":     taskCount,
		},
		"agent_playbook": []string{
			"Agent 1: POST /api/inscribe to create a wish (message + image required).",
			"Agent 2: POST /api/smart_contract/proposals to draft tasks from the wish.",
			"Agent 1: POST /api/smart_contract/proposals/{id}/approve to publish tasks.",
			"Agent 2: claim and submit work via tasks/claims endpoints.",
			"Agent 1: review submissions and build PSBT.",
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleDiscover advertises available tools and base routes for agents.
func (h *HTTPMCPServer) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeHTTPError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "Use GET /mcp/discover.")
		return
	}
	base := h.externalBaseURL(r)
	tools := h.getToolSchemas()
	toolNames := make([]string, 0, len(tools))
	for name := range tools {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)
	resp := map[string]interface{}{
		"version": "1.0",
		"base_urls": map[string]string{
			"api": base + "/api/smart_contract",
			"mcp": base + "/mcp",
		},
		"http_endpoints": map[string]interface{}{
			"inscribe": map[string]interface{}{
				"method":          "POST",
				"endpoint":        base + "/api/inscribe",
				"required_fields": []string{"message", "image_base64"},
				"description":     "Create a wish/inscription that seeds a proposal and contract metadata. Requires image payload.",
			},
		},
		"endpoints": []string{
			"/api/inscribe",
			"/api/smart_contract/contracts",
			"/api/smart_contract/tasks",
			"/api/smart_contract/claims",
			"/api/smart_contract/submissions",
			"/api/smart_contract/events",
			"/api/open-contracts",
		},
		"tools":      tools,
		"tool_names": toolNames,
		"total":      len(tools),
		"authentication": map[string]string{
			"type":        "api_key",
			"header_name": "X-API-Key",
			"required":    fmt.Sprintf("%t", h.apiKeyStore != nil),
		},
		"rate_limits": map[string]interface{}{
			"enabled":       false,
			"notes":         "rate limiting planned; not enforced by default",
			"recommended":   "10 rps claim, 5 rps submit (see roadmap)",
			"burst_example": 100,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *HTTPMCPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeHTTPError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "Use GET /mcp/health.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"version":   "1.0.0",
		"service":   "stargate-mcp",
		"endpoints": []string{"/mcp", "/mcp/tools", "/mcp/call", "/mcp/docs"},
		"components": map[string]string{
			"store":              fmt.Sprintf("%t", h.store != nil),
			"api_key_store":      fmt.Sprintf("%t", h.apiKeyStore != nil),
			"ingestion_svc":      fmt.Sprintf("%t", h.ingestionSvc != nil),
			"scanner_manager":    fmt.Sprintf("%t", h.scannerManager != nil),
			"smart_contract_svc": fmt.Sprintf("%t", h.smartContractSvc != nil),
		},
	})
}

func (h *HTTPMCPServer) handleEventsProxy(w http.ResponseWriter, r *http.Request) {
	// This would proxy to the events endpoint
	h.writeHTTPError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Events proxy not implemented", "Use /api/smart_contract/events directly.")
}

func (h *HTTPMCPServer) handleToolCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeHTTPError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "Use POST /mcp/call.")
		return
	}

	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeHTTPError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON", "Request body must be valid JSON.")
		return
	}

	if req.Tool == "" {
		h.writeHTTPError(w, http.StatusBadRequest, "MISSING_TOOL", "Tool name required", "Specify 'tool' field in request.")
		return
	}

	apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if apiKey == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			apiKey = strings.TrimPrefix(auth, "Bearer ")
		}
	}

	if h.toolRequiresAuth(req.Tool) {
		if apiKey == "" {
			h.writeHTTPError(w, http.StatusUnauthorized, "API_KEY_REQUIRED", "API key required", "Tool '"+req.Tool+"' requires authentication. Send X-API-Key or Authorization: Bearer <key>.")
			return
		}
		if h.apiKeyStore != nil && !h.apiKeyStore.Validate(apiKey) {
			h.writeHTTPError(w, http.StatusForbidden, "API_KEY_INVALID", "Invalid API key", "Double-check the X-API-Key header value.")
			return
		}
		if h.apiKeyStore != nil && !h.checkRateLimit(apiKey) {
			h.writeHTTPError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Rate limit exceeded", "Retry after a short delay.")
			return
		}
	}

	result, err := h.callToolDirect(r.Context(), req.Tool, req.Arguments, apiKey)
	if err != nil {
		// Handle structured errors
		status := GetHTTPStatusFromError(err)
		h.writeHTTPStructuredError(w, status, err)
		return
	}

	resp := MCPResponse{
		Success: true,
		Result:  result,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *HTTPMCPServer) handleToolSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeHTTPError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "Use GET /mcp/search")
		return
	}

	query := r.URL.Query().Get("q")
	category := r.URL.Query().Get("category")
	limit := 10

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	results := h.searchTools(query, category, limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"query":    query,
		"category": category,
		"limit":    limit,
		"matched":  len(results),
		"tools":    results,
	})
}
