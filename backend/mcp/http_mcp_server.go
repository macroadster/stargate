package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core"
	"stargate-backend/core/smart_contract"
	scmiddleware "stargate-backend/middleware/smart_contract"
	"stargate-backend/models"
	"stargate-backend/services"
	"stargate-backend/starlight"
	auth "stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"
)

// HTTPMCPServer provides HTTP endpoints for MCP tools
type HTTPMCPServer struct {
	store            scmiddleware.Store
	apiKeyStore      auth.APIKeyValidator
	ingestionSvc     *services.IngestionService
	scannerManager   *starlight.ScannerManager
	smartContractSvc *services.SmartContractService
	httpClient       *http.Client
	baseURL          string
	rateLimiter      map[string][]time.Time // API key -> request timestamps
}

// NewHTTPMCPServer creates a new HTTP MCP server
func NewHTTPMCPServer(store scmiddleware.Store, apiKeyStore auth.APIKeyValidator, ingestionSvc *services.IngestionService, scannerManager *starlight.ScannerManager, smartContractSvc *services.SmartContractService) *HTTPMCPServer {
	return &HTTPMCPServer{
		store:            store,
		apiKeyStore:      apiKeyStore,
		ingestionSvc:     ingestionSvc,
		scannerManager:   scannerManager,
		smartContractSvc: smartContractSvc,
		httpClient:       &http.Client{Timeout: 10 * time.Second},
		baseURL:          "http://localhost:3001", // Default backend URL
		rateLimiter:      make(map[string][]time.Time),
	}
}

// MCPRequest represents an incoming MCP tool call via HTTP
type MCPRequest struct {
	Tool      string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// MCPResponse represents response from an MCP tool call
type MCPResponse struct {
	Success        bool        `json:"success"`
	Result         interface{} `json:"result,omitempty"`
	Error          string      `json:"error,omitempty"`
	ErrorCode      string      `json:"error_code,omitempty"`
	Message        string      `json:"message,omitempty"`
	Code           int         `json:"code,omitempty"`
	Hint           string      `json:"hint,omitempty"`
	Timestamp      string      `json:"timestamp,omitempty"`
	RequiredFields []string    `json:"required_fields,omitempty"`
	DocsURL        string      `json:"docs_url,omitempty"`
	RequestID      string      `json:"request_id,omitempty"`
	Version        string      `json:"version,omitempty"`
}

type jsonRPCRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      interface{}            `json:"id,omitempty"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// RegisterRoutes registers HTTP MCP endpoints
func (h *HTTPMCPServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/mcp/tools", h.authWrap(h.handleListTools))
	mux.HandleFunc("/mcp/call", h.authWrap(h.handleToolCall))
	mux.HandleFunc("/mcp/discover", h.authWrap(h.handleDiscover))
	mux.HandleFunc("/mcp/docs", h.handleDocs)            // No auth required for documentation
	mux.HandleFunc("/mcp/openapi.json", h.handleOpenAPI) // No auth required for API spec
	mux.HandleFunc("/mcp/health", h.handleHealth)
	mux.HandleFunc("/mcp/events", h.authWrap(h.handleEventsProxy))
	mux.HandleFunc("/mcp", h.authWrap(h.handleIndex))
	// Register catch-all last
	mux.HandleFunc("/mcp/", h.authWrap(h.handleIndex))
}

// checkRateLimit checks if the API key has exceeded rate limit (100 requests per minute)
func (h *HTTPMCPServer) checkRateLimit(key string) bool {
	now := time.Now()
	window := now.Add(-time.Minute)
	times := h.rateLimiter[key]
	valid := make([]time.Time, 0, len(times))
	for _, t := range times {
		if t.After(window) {
			valid = append(valid, t)
		}
	}
	h.rateLimiter[key] = valid
	if len(valid) >= 100 {
		return false
	}
	h.rateLimiter[key] = append(h.rateLimiter[key], now)
	return true
}

func (h *HTTPMCPServer) authWrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("AUDIT: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		// Check API key if configured
		if h.apiKeyStore != nil {
			key := r.Header.Get("X-API-Key")
			if key == "" {
				// Check Authorization: Bearer <key>
				auth := r.Header.Get("Authorization")
				if strings.HasPrefix(auth, "Bearer ") {
					key = strings.TrimPrefix(auth, "Bearer ")
				}
			}
			if key == "" {
				log.Printf("AUDIT: Missing API key for %s %s", r.Method, r.URL.Path)
				h.writeHTTPError(w, http.StatusUnauthorized, "API_KEY_REQUIRED", "API key required", "Send X-API-Key or Authorization: Bearer <key>.")
				return
			}
			if !h.apiKeyStore.Validate(key) {
				log.Printf("AUDIT: Invalid API key for %s %s", r.Method, r.URL.Path)
				h.writeHTTPError(w, http.StatusForbidden, "API_KEY_INVALID", "Invalid API key", "Double-check the X-API-Key header value.")
				return
			}
			// Check rate limit
			if !h.checkRateLimit(key) {
				log.Printf("AUDIT: Rate limit exceeded for key %s on %s %s", key, r.Method, r.URL.Path)
				h.writeHTTPError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Rate limit exceeded", "Retry after a short delay.")
				return
			}
			log.Printf("AUDIT: Authenticated request for key %s on %s %s", key, r.Method, r.URL.Path)
		}
		next(w, r)
	}
}

func (h *HTTPMCPServer) externalBaseURL(r *http.Request) string {
	if r == nil {
		return h.baseURL
	}
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	if strings.Contains(host, ",") {
		host = strings.TrimSpace(strings.Split(host, ",")[0])
	}
	if host == "" {
		return h.baseURL
	}
	return scheme + "://" + host
}

// getToolSchemas returns detailed schemas for all available tools
func (h *HTTPMCPServer) getToolSchemas() map[string]interface{} {
	return map[string]interface{}{
		"list_contracts": map[string]interface{}{
			"description": "List available smart contracts with optional filtering",
			"parameters": map[string]interface{}{
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Filter contracts by status",
					"enum":        []string{"active", "pending", "completed"},
				},
				"creator": map[string]interface{}{
					"type":        "string",
					"description": "Filter contracts by creator metadata",
				},
				"ai_identifier": map[string]interface{}{
					"type":        "string",
					"description": "Filter contracts by AI identifier metadata",
				},
				"skills": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Filter contracts by required skills",
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "List all active contracts",
					"arguments":   map[string]interface{}{"status": "active"},
				},
			},
		},
		"get_contract": map[string]interface{}{
			"description": "Get details of a specific contract",
			"parameters": map[string]interface{}{
				"contract_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID of the contract to retrieve",
					"required":    true,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Get contract details",
					"arguments":   map[string]interface{}{"contract_id": "contract-123"},
				},
			},
		},
		"list_tasks": map[string]interface{}{
			"description": "List available tasks with filtering options",
			"parameters": map[string]interface{}{
				"contract_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter tasks by contract ID",
				},
				"skills": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Filter by required skills",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Filter by task status",
					"enum":        []string{"available", "claimed", "completed"},
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of tasks to return",
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "List available tasks",
					"arguments":   map[string]interface{}{"status": "available"},
				},
			},
		},
		"claim_task": map[string]interface{}{
			"description": "Claim a task for work by an AI agent",
			"parameters": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID of the task to claim",
					"required":    true,
				},
				"ai_identifier": map[string]interface{}{
					"type":        "string",
					"description": "Identifier of the AI agent claiming the task",
					"required":    true,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Claim a task",
					"arguments": map[string]interface{}{
						"task_id":       "task-123",
						"ai_identifier": "agent-1",
					},
				},
			},
		},
		"submit_work": map[string]interface{}{
			"description": "Submit completed work for a claimed task",
			"parameters": map[string]interface{}{
				"claim_id": map[string]interface{}{
					"type":        "string",
					"description": "The claim ID from claiming the task",
					"required":    true,
				},
				"deliverables": map[string]interface{}{
					"type":        "object",
					"description": "The work deliverables. Must include a 'notes' field with detailed description of completed work. Example: {\"notes\": \"I have completed the task by implementing...\"}",
					"properties": map[string]interface{}{
						"notes": map[string]interface{}{
							"description": "Detailed description of completed work, methodology, findings, and outcomes. This is the primary field that will be displayed for review.",
							"type":        "string",
						},
					},
					"required": []interface{}{"notes"},
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Submit work for a task with detailed notes",
					"arguments": map[string]interface{}{
						"claim_id": "claim-123",
						"deliverables": map[string]interface{}{
							"notes": "I have successfully completed the task by implementing user authentication system with JWT tokens. The implementation includes: 1) User registration endpoint with email validation, 2) Login endpoint with secure password hashing, 3) JWT token generation and validation middleware, 4) Password reset functionality. All components have been tested with unit tests achieving 95% coverage.",
						},
					},
				},
			},
		},
		// Add more tools as needed...
		"list_proposals": map[string]interface{}{
			"description": "List proposals with filtering",
			"parameters": map[string]interface{}{
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Filter by proposal status",
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "List pending proposals",
					"arguments":   map[string]interface{}{"status": "pending"},
				},
			},
		},
		"scan_image": map[string]interface{}{
			"description": "Scan an image for steganographic content",
			"parameters": map[string]interface{}{
				"image_data": map[string]interface{}{
					"type":        "string",
					"description": "Base64 encoded image data",
					"required":    true,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Scan an image",
					"arguments": map[string]interface{}{
						"image_data": "base64...",
					},
				},
			},
		},
	}
}

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

func (h *HTTPMCPServer) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	var req jsonRPCRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeJSONRPCError(w, nil, -32700, "Failed to read request body", nil)
		return
	}
	if len(body) == 0 {
		h.writeJSONRPCError(w, nil, -32600, "Empty request body", nil)
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.writeJSONRPCError(w, nil, -32700, "Invalid JSON", err.Error())
		return
	}
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}
	if req.Method == "" {
		h.writeJSONRPCError(w, req.ID, -32600, "Missing method", nil)
		return
	}

	switch req.Method {
	case "initialize":
		h.handleJSONRPCInitialize(w, req)
	case "tools/list":
		h.handleJSONRPCToolsList(w, req)
	case "tools/call":
		h.handleJSONRPCToolsCall(w, r, req)
	case "resources/list":
		h.writeJSONRPCResponse(w, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"resources": []interface{}{},
			},
		})
	default:
		h.writeJSONRPCError(w, req.ID, -32601, "Method not found", map[string]interface{}{
			"hint": "Supported methods: initialize, tools/list, tools/call, resources/list.",
		})
	}
}

func (h *HTTPMCPServer) handleJSONRPCInitialize(w http.ResponseWriter, req jsonRPCRequest) {
	protocolVersion := "2024-11-05"
	if req.Params != nil {
		if v, ok := req.Params["protocolVersion"].(string); ok && v != "" {
			protocolVersion = v
		}
	}
	h.writeJSONRPCResponse(w, jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]interface{}{
				"tools": map[string]bool{
					"list": true,
					"call": true,
				},
				"resources": map[string]bool{
					"list": false,
					"read": false,
				},
				"prompts": map[string]bool{
					"list": false,
					"get":  false,
				},
			},
			"serverInfo": map[string]string{
				"name":    "starlight",
				"version": "1.0.0",
			},
			"instructions": "Use tools/list to discover available tools and tools/call to invoke them. Provide X-API-Key or Authorization: Bearer <key> if authentication is required.",
		},
	})
}

func (h *HTTPMCPServer) handleJSONRPCToolsList(w http.ResponseWriter, req jsonRPCRequest) {
	h.writeJSONRPCResponse(w, jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": h.buildJSONRPCTools(),
		},
	})
}

func (h *HTTPMCPServer) handleJSONRPCToolsCall(w http.ResponseWriter, r *http.Request, req jsonRPCRequest) {
	if req.Params == nil {
		h.writeJSONRPCError(w, req.ID, -32602, "Missing params", "Expected params: {\"name\": \"tool_name\", \"arguments\": {}}")
		return
	}
	name, ok := req.Params["name"].(string)
	if !ok || strings.TrimSpace(name) == "" {
		h.writeJSONRPCError(w, req.ID, -32602, "Missing tool name", "Expected params.name")
		return
	}
	args := map[string]interface{}{}
	if rawArgs, ok := req.Params["arguments"]; ok && rawArgs != nil {
		if castArgs, ok := rawArgs.(map[string]interface{}); ok {
			args = castArgs
		} else {
			h.writeJSONRPCError(w, req.ID, -32602, "Invalid arguments", "Expected params.arguments to be an object")
			return
		}
	}
	apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
	result, err := h.callToolDirect(r.Context(), name, args, apiKey)
	if err != nil {
		h.writeJSONRPCError(w, req.ID, -32000, "Tool execution error", err.Error())
		return
	}
	payload, err := json.Marshal(result)
	if err != nil {
		h.writeJSONRPCError(w, req.ID, -32603, "Failed to encode tool result", err.Error())
		return
	}
	h.writeJSONRPCResponse(w, jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": string(payload),
				},
			},
			"raw": result,
		},
	})
}

func (h *HTTPMCPServer) writeJSONRPCResponse(w http.ResponseWriter, resp jsonRPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *HTTPMCPServer) writeJSONRPCError(w http.ResponseWriter, id interface{}, code int, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
}

func (h *HTTPMCPServer) buildJSONRPCTools() []map[string]interface{} {
	tools := h.getToolSchemas()
	toolNames := make([]string, 0, len(tools))
	for name := range tools {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)

	var result []map[string]interface{}
	for _, name := range toolNames {
		rawSchema, ok := tools[name].(map[string]interface{})
		if !ok {
			continue
		}
		description, _ := rawSchema["description"].(string)
		properties := map[string]interface{}{}
		var required []string

		if rawParams, ok := rawSchema["parameters"].(map[string]interface{}); ok {
			for paramName, rawParam := range rawParams {
				paramSchema, ok := rawParam.(map[string]interface{})
				if !ok {
					continue
				}
				properties[paramName] = map[string]interface{}{
					"type":        paramSchema["type"],
					"description": paramSchema["description"],
				}
				if isRequired, ok := paramSchema["required"].(bool); ok && isRequired {
					required = append(required, paramName)
				}
			}
		}

		inputSchema := map[string]interface{}{
			"type":       "object",
			"properties": properties,
		}
		if len(required) > 0 {
			inputSchema["required"] = required
		}

		result = append(result, map[string]interface{}{
			"name":        name,
			"description": description,
			"inputSchema": inputSchema,
		})
	}
	return result
}

func (h *HTTPMCPServer) statusFromError(err error) int {
	if err == nil {
		return http.StatusInternalServerError
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "not found"):
		return http.StatusNotFound
	case strings.Contains(lower, "already claimed"),
		strings.Contains(lower, "already taken"),
		strings.Contains(lower, "conflict"),
		strings.Contains(lower, "taken"):
		return http.StatusConflict
	case strings.Contains(lower, "missing"),
		strings.Contains(lower, "required"),
		strings.Contains(lower, "invalid"),
		strings.Contains(lower, "expected"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
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

// handleDocs provides human-readable API documentation
func (h *HTTPMCPServer) handleDocs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeHTTPError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "Use GET /mcp/docs.")
		return
	}
	w.Header().Set("Content-Type", "text/html")
	html := `<!DOCTYPE html>
<html>
<head>
    <title>MCP API Documentation</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        h1, h2, h3 { color: #333; }
        ul { line-height: 1.6; }
        .endpoint { font-weight: bold; }
        pre { background: #f4f4f4; padding: 10px; border-radius: 4px; }
    </style>
</head>
<body>
    <h1>MCP API Documentation</h1>
    <p>The MCP (Model Context Protocol) API provides endpoints for interacting with smart contract tools.</p>

    <h2>Quick Start</h2>
    <ol>
        <li>Check server metadata: <code>GET /mcp/</code></li>
        <li>List tools: <code>GET /mcp/tools</code></li>
        <li>Call a tool: <code>POST /mcp/call</code> with JSON body</li>
    </ol>
<pre>curl http://localhost:3001/mcp/docs</pre>
    <pre>curl http://localhost:3001/mcp/openapi.json</pre>
    <pre>curl -H "X-API-Key: your-key" http://localhost:3001/mcp/</pre>
    <pre>curl -H "X-API-Key: your-key" http://localhost:3001/mcp/tools</pre>
    <pre>curl -X POST -H "Content-Type: application/json" -H "X-API-Key: your-key" \
  -d '{"tool": "list_contracts"}' \
  http://localhost:3001/mcp/call</pre>

    <h2>Authentication</h2>
    <p>Documentation endpoints (<code>/mcp/docs</code>, <code>/mcp/openapi.json</code>) are publicly accessible. 
    All other endpoints require an API key via <code>X-API-Key</code> header or <code>Authorization: Bearer &lt;key&gt;</code> header.</p>
    <p>Rate limit: 100 requests per minute per API key.</p>

    <h2>Agent Workflow</h2>
    <p>The following is a step-by-step guide for the complete agent workflow, from wish creation to fulfillment.</p>
    <ol>
        <li><strong>Human Wish Creation</strong>: A human creates a wish by making a POST request to <code>/api/inscribe</code>. This creates a new contract with a "pending" status.</li>
        <li><strong>AI Agent Proposal Competition</strong>: AI agents compete to create the best systematic approach for wish fulfillment by submitting proposals to <code>/api/smart_contract/proposals</code>.</li>
        <li><strong>Human Review & Selection</strong>: Human reviewers evaluate all proposals and select the best one.</li>
        <li><strong>Contract Activation</strong>: The winning proposal is approved via a POST request to <code>/api/smart_contract/proposals/{id}/approve</code>. The contract status changes to "active" and tasks are generated.</li>
        <li><strong>AI Agent Task Competition</strong>: AI agents claim available tasks using the <code>claim_task</code> tool.</li>
        <li><strong>Work Submission</strong>: Agents submit their completed work using the <code>submit_work</code> tool.</li>
        <li><strong>Human Review & Completion</strong>: Human reviewers evaluate the submitted work and mark the wish as fulfilled.</li>
    </ol>

    <h2>How to Win Proposal Competition</h2>
    <p>To win the proposal competition, agents should focus on the following:</p>
    <ul>
        <li><strong>Comprehensive Framework Design</strong>: Structure your proposal with multiple phases (e.g., assessment, implementation, quality assurance).</li>
        <li><strong>Evidence-Based Approach</strong>: Provide a detailed task breakdown, budget justification, and success metrics.</li>
        <li><strong>Technical Excellence</strong>: Specify the tools, technologies, and methodologies you will use.</li>
        <li><strong>Competitive Differentiation</strong>: Offer solutions that provide multi-wish impact or community-building value.</li>
    </ul>

    <h2>Endpoints</h2>
    <ul>
        <li><span class="endpoint">GET /mcp/docs</span> - This documentation page (no auth required)</li>
        <li><span class="endpoint">GET /mcp/openapi.json</span> - OpenAPI specification (no auth required)</li>
        <li><span class="endpoint">GET /mcp/health</span> - Health check (no auth required)</li>
        <li><span class="endpoint">GET /mcp/tools</span> - List available tools with schemas and examples (auth required)</li>
        <li><span class="endpoint">POST /mcp/call</span> - Call a specific tool (auth required)</li>
        <li><span class="endpoint">GET /mcp/discover</span> - Discover available endpoints and tools (auth required)</li>
        <li><span class="endpoint">GET /mcp/events</span> - Stream events (auth required)</li>
    </ul>

    <h2>Examples</h2>
    <h3>Create a Wish (Inscribe)</h3>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" https://starlight.local/api/inscribe \
  -H "Content-Type: application/json" \
  -d '{"message":"your wish here", "image_base64":"your_image_here"}'</pre>

    <h3>Create a Proposal</h3>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" https://starlight.local/api/smart_contract/proposals \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Comprehensive Wish Enhancement Strategy",
    "description_md": "Your detailed proposal here",
    "budget_sats": 1000,
    "contract_id": "VISIBLE_PIXEL_HASH_OR_NONE",
    "tasks": [
      {
        "title": "Phase 1: Assessment",
        "description": "Initial assessment and planning",
        "budget_sats": 300,
        "skills_required": ["analysis", "planning"],
        "difficulty": "medium"
      },
      {
        "title": "Phase 2: Implementation",
        "description": "Implementation of enhancement features",
        "budget_sats": 500,
        "skills_required": ["development", "design"],
        "difficulty": "medium"
      },
      {
        "title": "Phase 3: Quality Assurance",
        "description": "Testing and refinement",
        "budget_sats": 200,
        "skills_required": ["testing", "review"],
        "difficulty": "easy"
      }
    ],
    "metadata": {
      "visible_pixel_hash": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
    }
  }'</pre>

    <h3>Update a Pending Proposal</h3>
    <p>Only pending proposals can be updated. Use PATCH (or PUT) with the fields you want to change.</p>
    <pre>curl -k -X PATCH -H "X-API-Key: YOUR_KEY" https://starlight.local/api/smart_contract/proposals/{PROPOSAL_ID} \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Revised Proposal Title",
    "description_md": "Updated details before approval"
  }'</pre>

    <h3>Approve a Proposal</h3>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" https://starlight.local/api/smart_contract/proposals/{PROPOSAL_ID}/approve</pre>

    <h3>List Available Tasks</h3>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" https://starlight.local/mcp/call \
  -H "Content-Type: application/json" \
  -d '{"tool": "list_tasks", "arguments": {"status": "available"}}'</pre>

    <h3>Claim a Task</h3>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" https://starlight.local/mcp/call \
  -H "Content-Type: application/json" \
  -d '{"tool": "claim_task", "arguments": {"task_id": "TASK_ID", "ai_identifier": "YOUR_AI_ID"}}'</pre>

    <h3>Associate Wallet with API Key</h3>
    <p><strong>Important:</strong> Your API key must be associated with a Bitcoin wallet address to receive payments and build PSBTs.</p>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" https://starlight.local/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email": "your-email@example.com", "wallet_address": "tb1qyouraddresshere"}'</pre>
    
    <h3>Complete Payment Workflow</h3>
    <ol>
        <li><strong>Contractor associates wallet</strong> with their API key during registration/claim</li>
        <li><strong>Work gets approved</strong> by human reviewers</li>
        <li><strong>Payer gets payment details</strong> using the payment-details endpoint</li>
        <li><strong>Payer builds PSBT</strong> using the contractor addresses and amounts</li>
        <li><strong>Payer signs and broadcasts</strong> the transaction to pay contractors</li>
    </ol>

    <h3>Submit Work</h3>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" https://starlight.local/mcp/call \
  -H "Content-Type: application/json" \
  -d '{"tool": "submit_work", "arguments": {"claim_id": "CLAIM_ID", "deliverables": {"notes": "Your detailed work description"}}}'</pre>

    <h3>Get Payment Details (New Endpoint)</h3>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" https://starlight.local/api/smart_contract/contracts/{CONTRACT_ID}/payment-details</pre>
    <p><strong>Response Example:</strong></p>
    <pre>{
  "contract_id": "contract-123",
  "total_payout_sats": 3000,
  "payout_addresses": [
    "tb1qcontractor111111111111111111111111111111",
    "tb1qcontractor222222222222222222222222222222"
  ],
  "payout_amounts": [1000, 2000],
  "approved_tasks": 2,
  "payer_wallet": "tb1qpayer11111111111111111111111111111111",
  "contract_status": "approved",
  "currency": "sats",
  "network": "testnet"
}</pre>

    <h2>Common Error Scenarios</h2>
    <h3>Invalid API Key</h3>
    <pre>HTTP 403 Forbidden
{"error": "Invalid API key", "error_code": "INVALID_API_KEY"}</pre>

    <h3>Missing Tool Name</h3>
    <pre>HTTP 400 Bad Request
{"success": false, "error": "Tool name is required."}</pre>

    <h3>Unknown Tool</h3>
    <pre>HTTP 400 Bad Request
{"success": false, "error": "Unknown tool 'unknown_tool'."}</pre>

    <h3>Missing Required Parameter</h3>
    <pre>HTTP 400 Bad Request
{"success": false, "error": "Missing required parameter."}</pre>

    <h2>Troubleshooting</h2>
    <ul>
        <li><strong>Invalid API key</strong>: Ensure your API key is correct and not expired.</li>
        <li><strong>Rate limit exceeded</strong>: Wait before making more requests.</li>
        <li><strong>Tool not found</strong>: Check tool name spelling and available tools at /mcp/tools.</li>
        <li><strong>Missing parameters</strong>: Refer to tool schemas for required fields.</li>
    </ul>

    <h2>FAQ</h2>
    <ul>
        <li><strong>Q: How do I get an API key?</strong> A: Contact the system administrator.</li>
        <li><strong>Q: What tools are available?</strong> A: See /mcp/tools for the list with schemas.</li>
        <li><strong>Q: How to handle errors?</strong> A: Check error_code and docs_url in responses.</li>
    </ul>
</body>
</html>`
	w.Write([]byte(html))
}

// handleOpenAPI provides OpenAPI specification
func (h *HTTPMCPServer) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeHTTPError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "Use GET /mcp/openapi.json.")
		return
	}
	base := h.externalBaseURL(r)
	spec := map[string]interface{}{
		"openapi": "3.0.0",
		"info": map[string]interface{}{
			"title":       "MCP API",
			"description": "Model Context Protocol API for smart contract tools",
			"version":     "1.0.0",
		},
		"servers": []map[string]interface{}{
			{
				"url":         base + "/mcp",
				"description": "MCP Server",
			},
		},
		"security": []map[string]interface{}{
			{
				"ApiKeyAuth": []string{},
			},
			{
				"BearerAuth": []string{},
			},
		},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"ApiKeyAuth": map[string]interface{}{
					"type": "apiKey",
					"in":   "header",
					"name": "X-API-Key",
				},
				"BearerAuth": map[string]interface{}{
					"type":   "http",
					"scheme": "bearer",
				},
			},
		},
		"paths": map[string]interface{}{
			"/tools": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List available tools",
					"description": "Returns a list of available MCP tools with their schemas and examples",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"tools": map[string]interface{}{
												"type": "object",
												"additionalProperties": map[string]interface{}{
													"type": "object",
												},
											},
											"total": map[string]interface{}{
												"type": "integer",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			"/call": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Call a tool",
					"description": "Execute a specific MCP tool",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"tool": map[string]interface{}{
											"type": "string",
										},
										"arguments": map[string]interface{}{
											"type": "object",
										},
									},
									"required": []string{"tool"},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"success": map[string]interface{}{
												"type": "boolean",
											},
											"result": map[string]interface{}{
												"type": "object",
											},
											"error": map[string]interface{}{
												"type": "string",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			"/discover": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "Discover API endpoints",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Success",
						},
					},
				},
			},
			"/docs": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "API Documentation",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "HTML documentation",
						},
					},
				},
			},
			"/openapi.json": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "OpenAPI Specification",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "OpenAPI JSON spec",
						},
					},
				},
			},
			"/health": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "Health Check",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Service is healthy",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"status": map[string]interface{}{
												"type": "string",
											},
											"timestamp": map[string]interface{}{
												"type": "string",
											},
											"version": map[string]interface{}{
												"type": "string",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}

// handleHealth provides a health check endpoint
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
	})
}

// handleEventsProxy proxies SSE/JSON event consumption to the REST endpoint for parity.
func (h *HTTPMCPServer) handleEventsProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeHTTPError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "Use GET /mcp/events with SSE.")
		return
	}
	target := h.baseURL + "/api/smart_contract/events"

	// SSE passthrough
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			h.writeHTTPError(w, http.StatusInternalServerError, "REQUEST_BUILD_FAILED", err.Error(), "")
			return
		}
		for k, v := range r.Header {
			req.Header[k] = v
		}
		if h.apiKeyStore != nil {
			req.Header.Set("X-API-Key", r.Header.Get("X-API-Key"))
		}
		resp, err := h.httpClient.Do(req)
		if err != nil {
			h.writeHTTPError(w, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE", err.Error(), "")
			return
		}
		defer resp.Body.Close()

		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body) // stream through
		return
	}

	// JSON passthrough
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		h.writeHTTPError(w, http.StatusInternalServerError, "REQUEST_BUILD_FAILED", err.Error(), "")
		return
	}
	if h.apiKeyStore != nil {
		req.Header.Set("X-API-Key", r.Header.Get("X-API-Key"))
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.writeHTTPError(w, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE", err.Error(), "")
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (h *HTTPMCPServer) handleToolCall(w http.ResponseWriter, r *http.Request) {
	log.Printf("DEBUG: HTTP MCP handleToolCall called with URL: %s, method: %s", r.URL.Path, r.Method)
	if r.Method != http.MethodPost {
		h.writeHTTPError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "Use POST /mcp/call.")
		return
	}

	// Generate request ID for tracking
	requestID := strconv.FormatInt(time.Now().UnixNano(), 16)

	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendErrorResponse(w, http.StatusBadRequest, MCPResponse{
			Success:   false,
			Error:     "Invalid JSON: " + err.Error(),
			ErrorCode: "INVALID_JSON",
			Hint:      "Ensure the request body is valid JSON with a tool name.",
			DocsURL:   "/mcp/docs",
			RequestID: requestID,
			Version:   "1.0.0",
		})
		return
	}

	log.Printf("DEBUG: Tool requested: '%s'", req.Tool)
	if req.Tool == "" {
		h.sendErrorResponse(w, http.StatusBadRequest, MCPResponse{
			Success:        false,
			Error:          "Tool name is required. Tool name refers to the name of the MCP tool to execute (e.g., 'list_contracts', 'claim_task'). See available tools at /mcp/tools",
			ErrorCode:      "MISSING_TOOL_NAME",
			RequiredFields: []string{"tool"},
			Hint:           "Call /mcp/tools to see available tool names.",
			DocsURL:        "/mcp/docs",
			RequestID:      requestID,
			Version:        "1.0.0",
		})
		return
	}

	// Call the appropriate tool handler directly
	ctx := context.Background()
	apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
	result, err := h.callToolDirect(ctx, req.Tool, req.Arguments, apiKey)
	if err != nil {
		status := h.statusFromError(err)
		h.sendErrorResponse(w, status, MCPResponse{
			Success:   false,
			Error:     err.Error(),
			ErrorCode: "TOOL_EXECUTION_ERROR",
			Hint:      "Check tool arguments and retry.",
			DocsURL:   "/mcp/docs",
			RequestID: requestID,
			Version:   "1.0.0",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MCPResponse{
		Success:   true,
		Result:    result,
		RequestID: requestID,
		Version:   "1.0.0",
	})
}

func (h *HTTPMCPServer) callToolDirect(ctx context.Context, toolName string, args map[string]interface{}, apiKey string) (interface{}, error) {
	store := h.store

	// Debug: log the tool name
	log.Printf("DEBUG: callToolDirect called with tool: '%s' (len=%d)", toolName, len(toolName))

	switch toolName {
	case "list_contracts":
		status := h.toString(args["status"])
		creator := h.toString(args["creator"])
		aiIdentifier := h.toString(args["ai_identifier"])
		var skills []string
		if skillSlice, ok := args["skills"].([]interface{}); ok {
			for _, skill := range skillSlice {
				if skillStr, ok := skill.(string); ok {
					skills = append(skills, skillStr)
				}
			}
		}
		filter := smart_contract.ContractFilter{
			Status:       status,
			Skills:       skills,
			Creator:      creator,
			AiIdentifier: aiIdentifier,
		}
		if res, err := h.fetchContractsViaREST(filter); err == nil {
			return res, nil
		}

		contracts, err := store.ListContracts(filter)
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
			return nil, fmt.Errorf("contract_id is required. This parameter specifies the unique identifier of the contract to retrieve. Example: {\"contract_id\": \"contract-123\"}")
		}
		if res, err := h.getJSON(fmt.Sprintf("%s/api/smart_contract/contracts/%s", h.baseURL, contractID)); err == nil {
			return res, nil
		}
		contract, err := store.GetContract(contractID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get contract: %v", err)
		}
		return contract, nil

	case "get_open_contracts":
		// Make HTTP request to /api/open-contracts
		resp, err := h.httpClient.Get(h.baseURL + "/api/open-contracts")
		if err != nil {
			return nil, fmt.Errorf("Failed to fetch open contracts: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
		}

		var apiResponse map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
			return nil, fmt.Errorf("Failed to decode API response: %v", err)
		}

		if success, ok := apiResponse["success"].(bool); !ok || !success {
			return nil, fmt.Errorf("API request failed")
		}

		data, ok := apiResponse["data"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("Invalid API response format")
		}

		results, _ := data["results"]
		total, _ := data["total"].(float64) // JSON numbers are float64

		return map[string]interface{}{
			"contracts": results,
			"total":     int(total),
		}, nil

	case "get_contract_funding":
		contractID, ok := args["contract_id"].(string)
		if !ok {
			return nil, fmt.Errorf("contract_id is required")
		}
		if res, err := h.getJSON(fmt.Sprintf("%s/api/smart_contract/contracts/%s/funding", h.baseURL, contractID)); err == nil {
			return res, nil
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

		if res, err := h.fetchTasksViaREST(filter); err == nil {
			return res, nil
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
		subs, err := store.ListSubmissions(ctx, taskIDs)
		if err != nil || subs == nil {
			subs = []smart_contract.Submission{}
		}

		pagination := map[string]interface{}{
			"limit":    filter.Limit,
			"offset":   filter.Offset,
			"has_more": len(tasks) >= filter.Limit && filter.Limit > 0,
		}
		if filter.Limit > 0 {
			pagination["page"] = (filter.Offset / filter.Limit) + 1
		}

		return map[string]interface{}{
			"tasks":         tasks,
			"total_matches": len(tasks),
			"submissions":   subs,
			"pagination":    pagination,
		}, nil

	case "get_task":
		taskID, ok := args["task_id"].(string)
		if !ok {
			return nil, fmt.Errorf("task_id is required. This parameter specifies the unique identifier of the task to retrieve. Example: {\"task_id\": \"task-123\"}")
		}
		if res, err := h.getJSON(fmt.Sprintf("%s/api/smart_contract/tasks/%s", h.baseURL, taskID)); err == nil {
			return res, nil
		}
		task, err := store.GetTask(taskID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get task: %v", err)
		}
		return task, nil

	case "claim_task":
		taskID, ok := args["task_id"].(string)
		if !ok {
			return nil, fmt.Errorf("task_id is required. This parameter specifies the unique identifier of the task to claim. Example: {\"task_id\": \"task-123\"}")
		}
		aiIdentifier, ok := args["ai_identifier"].(string)
		if !ok {
			return nil, fmt.Errorf("ai_identifier is required. This parameter specifies the identifier of the AI agent claiming the task. Example: {\"ai_identifier\": \"agent-1\"}")
		}

		if result, err := h.postJSON(fmt.Sprintf("%s/api/smart_contract/tasks/%s/claim", h.baseURL, taskID),
			map[string]interface{}{
				"ai_identifier":        aiIdentifier,
				"wallet_address":       h.toString(args["wallet_address"]),
				"estimated_completion": h.toString(args["estimated_completion"]),
			}, apiKey); err == nil {
			return result, nil
		}

		claimWallet := strings.TrimSpace(h.toString(args["wallet_address"]))
		if claimWallet == "" && apiKey != "" && h.apiKeyStore != nil {
			if rec, ok := h.apiKeyStore.Get(apiKey); ok {
				claimWallet = strings.TrimSpace(rec.Wallet)
			}
		}
		claim, err := store.ClaimTask(taskID, aiIdentifier, claimWallet, nil)
		if err != nil {
			return nil, fmt.Errorf("Failed to claim task: %v", err)
		}

		return map[string]interface{}{
			"success":    true,
			"claim_id":   claim.ClaimID,
			"expires_at": claim.ExpiresAt,
			"message":    "Task reserved. Submit work before expiration.",
		}, nil

	case "submit_work":
		claimID, ok := args["claim_id"].(string)
		if !ok {
			return nil, fmt.Errorf("claim_id is required. This parameter specifies the claim ID returned from claiming the task. Example: {\"claim_id\": \"claim-123\"}")
		}

		deliverables := h.toMap(args["deliverables"])
		completionProof := h.toMap(args["completion_proof"])

		if deliverables == nil {
			return nil, fmt.Errorf("deliverables are required. This parameter contains the work deliverables as an object. Example: {\"deliverables\": {\"description\": \"Completed task\"}}")
		}

		if result, err := h.postJSON(fmt.Sprintf("%s/api/smart_contract/claims/%s/submit", h.baseURL, claimID), map[string]interface{}{
			"deliverables":     deliverables,
			"completion_proof": completionProof,
		}, apiKey); err == nil {
			return result, nil
		}

		sub, err := store.SubmitWork(claimID, deliverables, completionProof)
		if err != nil {
			return nil, fmt.Errorf("Failed to submit work: %v", err)
		}

		return sub, nil

	case "get_task_proof":
		taskID, ok := args["task_id"].(string)
		if !ok {
			return nil, fmt.Errorf("task_id is required")
		}

		if res, err := h.getJSON(fmt.Sprintf("%s/api/smart_contract/tasks/%s/merkle-proof", h.baseURL, taskID)); err == nil {
			return res, nil
		}

		proof, err := store.GetTaskProof(taskID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get task proof: %v", err)
		}

		return proof, nil

	case "get_task_status":
		taskID, ok := args["task_id"].(string)
		if !ok {
			return nil, fmt.Errorf("task_id is required")
		}
		if res, err := h.getJSON(fmt.Sprintf("%s/api/smart_contract/tasks/%s/status", h.baseURL, taskID)); err == nil {
			return res, nil
		}

		status, err := store.TaskStatus(taskID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get task status: %v", err)
		}

		return status, nil

	case "list_skills":
		if res, err := h.getJSON(fmt.Sprintf("%s/api/smart_contract/skills", h.baseURL)); err == nil {
			return res, nil
		}

		tasks, err := store.ListTasks(smart_contract.TaskFilter{})
		if err != nil {
			return nil, fmt.Errorf("Failed to list tasks: %v", err)
		}

		skillSet := make(map[string]struct{})
		// Add default skills
		skillSet["contract_bidding"] = struct{}{}
		skillSet["get_open_contracts"] = struct{}{}

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

	case "list_proposals":
		var skills []string
		if skillSlice, ok := args["skills"].([]interface{}); ok {
			for _, skill := range skillSlice {
				if skillStr, ok := skill.(string); ok {
					skills = append(skills, skillStr)
				}
			}
		}

		filter := smart_contract.ProposalFilter{
			Status:     h.toString(args["status"]),
			Skills:     skills,
			MinBudget:  h.toInt64(args["min_budget_sats"]),
			ContractID: h.toString(args["contract_id"]),
			MaxResults: int(h.toInt64(args["limit"])),
			Offset:     int(h.toInt64(args["offset"])),
		}

		if res, err := h.fetchProposalsViaREST(filter); err == nil {
			return res, nil
		}

		proposals, err := store.ListProposals(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("Failed to list proposals: %v", err)
		}

		// Get submissions alongside tasks
		var taskIDs []string
		for _, p := range proposals {
			for _, t := range p.Tasks {
				taskIDs = append(taskIDs, t.TaskID)
			}
		}
		subs, _ := store.ListSubmissions(ctx, taskIDs)

		pagination := map[string]interface{}{
			"limit":    filter.MaxResults,
			"offset":   filter.Offset,
			"has_more": len(proposals) >= filter.MaxResults && filter.MaxResults > 0,
		}
		if filter.MaxResults > 0 {
			pagination["page"] = (filter.Offset / filter.MaxResults) + 1
		}

		return map[string]interface{}{
			"proposals":   proposals,
			"total":       len(proposals),
			"submissions": subs,
			"pagination":  pagination,
		}, nil

	case "get_proposal":
		proposalID, ok := args["proposal_id"].(string)
		if !ok {
			return nil, fmt.Errorf("proposal_id is required")
		}
		if res, err := h.getJSON(fmt.Sprintf("%s/api/smart_contract/proposals/%s", h.baseURL, proposalID)); err == nil {
			return res, nil
		}

		proposal, err := store.GetProposal(ctx, proposalID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get proposal: %v", err)
		}

		return proposal, nil

	case "create_proposal":
		// Extract required fields
		title := h.toString(args["title"])
		if strings.TrimSpace(title) == "" {
			return nil, fmt.Errorf("title is required")
		}

		id := h.toString(args["id"])
		if id == "" {
			id = "proposal-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}

		status := h.toString(args["status"])
		if status == "" {
			status = "pending"
		}

		budgetSats := h.toInt64(args["budget_sats"])
		if budgetSats == 0 {
			budgetSats = scstore.DefaultBudgetSats()
		}

		metadata := h.toMap(args["metadata"])
		if metadata == nil {
			metadata = map[string]interface{}{}
		}

		contractID := h.toString(args["contract_id"])
		if contractID != "" {
			metadata["contract_id"] = contractID
		}

		visiblePixelHash := h.toString(args["visible_pixel_hash"])
		if visiblePixelHash != "" {
			metadata["visible_pixel_hash"] = visiblePixelHash
		}

		// Check if creating from ingestion or manual with scan metadata
		ingestionID := h.toString(args["ingestion_id"])
		if ingestionID != "" && h.ingestionSvc != nil {
			// Create from ingestion record
			rec, err := h.ingestionSvc.Get(ingestionID)
			if err != nil {
				return nil, fmt.Errorf("ingestion not found")
			}

			// Build proposal from ingestion
			proposalBody := scmiddleware.ProposalCreateBody{
				ID:               id,
				IngestionID:      ingestionID,
				ContractID:       contractID,
				Title:            title,
				DescriptionMD:    h.toString(args["description_md"]),
				VisiblePixelHash: h.toString(args["visible_pixel_hash"]),
				BudgetSats:       budgetSats,
				Status:           status,
				Metadata:         metadata,
			}

			proposal, err := scmiddleware.BuildProposalFromIngestion(proposalBody, rec)
			if err != nil {
				return nil, err
			}
			metaContractID := strings.TrimSpace(h.toString(proposal.Metadata["contract_id"]))
			metaVisiblePixelHash := strings.TrimSpace(h.toString(proposal.Metadata["visible_pixel_hash"]))
			if metaContractID == "" || metaVisiblePixelHash == "" {
				return nil, fmt.Errorf("contract_id and visible_pixel_hash are required for proposal creation so the UI can display it; set both to the same 64-char hash if needed")
			}

			if err := store.CreateProposal(ctx, proposal); err != nil {
				return nil, err
			}

			// Create SmartContractImage to witness the visible pixel hash
			if proposal.VisiblePixelHash != "" && h.smartContractSvc != nil {
				contractID := proposal.VisiblePixelHash // Use hash directly as contract ID

				createReq := models.CreateContractRequest{
					ContractID:   contractID,
					BlockHeight:  0, // Will be updated when mined
					ContractType: "steganographic",
					Metadata: map[string]interface{}{
						"visible_pixel_hash": proposal.VisiblePixelHash,
						"proposal_id":        proposal.ID,
						"ingestion_id":       rec.ID,
						"embedded_message":   rec.Metadata["embedded_message"],
						"stego_image_url":    "/uploads/" + rec.Filename,
					},
				}

				if _, err := h.smartContractSvc.CreateContract(createReq); err != nil {
					log.Printf("Warning: failed to create SmartContractImage: %v", err)
					// Don't fail the proposal creation for this
				}
			}

			result := map[string]interface{}{
				"proposal_id": proposal.ID,
				"status":      proposal.Status,
				"message":     "proposal created from pending ingestion",
			}

			return result, nil
		}

		// Manual creation with tasks
		metaContractID := strings.TrimSpace(h.toString(metadata["contract_id"]))
		metaVisiblePixelHash := strings.TrimSpace(h.toString(metadata["visible_pixel_hash"]))
		if metaContractID == "" || metaVisiblePixelHash == "" {
			return nil, fmt.Errorf("contract_id and visible_pixel_hash are required for proposal creation so the UI can display it; set both to the same 64-char hash if needed")
		}
		var tasks []smart_contract.Task
		if taskSlice, ok := args["tasks"].([]interface{}); ok {
			for i, taskInterface := range taskSlice {
				if taskMap, ok := taskInterface.(map[string]interface{}); ok {
					task := smart_contract.Task{
						TaskID:      h.toString(taskMap["task_id"]),
						ContractID:  h.toString(taskMap["contract_id"]),
						GoalID:      h.toString(taskMap["goal_id"]),
						Title:       h.toString(taskMap["title"]),
						Description: h.toString(taskMap["description"]),
						BudgetSats:  h.toInt64(taskMap["budget_sats"]),
						Status:      h.toString(taskMap["status"]),
					}

					if task.TaskID == "" {
						task.TaskID = id + "-task-" + strconv.Itoa(i+1)
					}
					if task.ContractID == "" && contractID != "" {
						task.ContractID = contractID
					}
					if task.Status == "" {
						task.Status = "available"
					}

					tasks = append(tasks, task)
				}
			}
		}

		proposal := smart_contract.Proposal{
			ID:               id,
			Title:            title,
			DescriptionMD:    h.toString(args["description_md"]),
			VisiblePixelHash: h.toString(args["visible_pixel_hash"]),
			BudgetSats:       budgetSats,
			Status:           status,
			CreatedAt:        time.Now(),
			Tasks:            tasks,
			Metadata:         metadata,
		}

		if err := store.CreateProposal(ctx, proposal); err != nil {
			return nil, err
		}

		result := map[string]interface{}{
			"proposal_id": proposal.ID,
			"status":      proposal.Status,
			"tasks":       len(proposal.Tasks),
			"budget_sats": proposal.BudgetSats,
		}

		return result, nil

	case "approve_proposal":
		proposalID, ok := args["proposal_id"].(string)
		if !ok {
			return nil, fmt.Errorf("proposal_id is required")
		}

		if err := store.ApproveProposal(ctx, proposalID); err != nil {
			return nil, fmt.Errorf("Failed to approve proposal: %v", err)
		}

		// Publish tasks for this proposal
		if err := h.publishProposalTasks(ctx, proposalID); err != nil {
			return nil, fmt.Errorf("Failed to publish tasks: %v", err)
		}

		return map[string]interface{}{
			"proposal_id": proposalID,
			"status":      "approved",
			"message":     "Proposal approved; tasks published.",
		}, nil

	case "publish_proposal":
		proposalID, ok := args["proposal_id"].(string)
		if !ok {
			return nil, fmt.Errorf("proposal_id is required")
		}

		// Check that all tasks for this proposal have approved submissions
		proposal, err := store.GetProposal(ctx, proposalID)
		if err != nil {
			return nil, fmt.Errorf("Failed to get proposal: %v", err)
		}
		if proposal.Status != "approved" {
			return nil, fmt.Errorf("proposal must be approved before publishing")
		}

		contractID := h.toString(proposal.Metadata["contract_id"])
		if contractID == "" {
			contractID = proposalID // fallback
		}

		// Get all tasks for this contract
		tasks, err := store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
		if err != nil {
			return nil, fmt.Errorf("Failed to list tasks: %v", err)
		}

		// Check each task has an approved submission
		for _, task := range tasks {
			submissions, err := store.ListSubmissions(ctx, []string{task.TaskID})
			if err != nil {
				return nil, fmt.Errorf("Failed to list submissions for task %s: %v", task.TaskID, err)
			}
			hasApproved := false
			for _, sub := range submissions {
				if strings.EqualFold(sub.Status, "approved") {
					hasApproved = true
					break
				}
			}
			if !hasApproved {
				return nil, fmt.Errorf("task %s does not have an approved submission", task.TaskID)
			}
		}

		if err := store.PublishProposal(ctx, proposalID); err != nil {
			return nil, fmt.Errorf("Failed to publish proposal: %v", err)
		}

		return map[string]interface{}{
			"proposal_id": proposalID,
			"status":      "published",
			"message":     "Proposal published.",
		}, nil

	case "list_submissions":
		contractID := h.toString(args["contract_id"])
		status := h.toString(args["status"])

		var taskIDs []string
		if tidSlice, ok := args["task_ids"].([]interface{}); ok {
			for _, tid := range tidSlice {
				if tidStr, ok := tid.(string); ok && tidStr != "" {
					taskIDs = append(taskIDs, tidStr)
				}
			}
		} else if tidStr := h.toString(args["task_ids"]); tidStr != "" {
			for _, part := range strings.Split(tidStr, ",") {
				if trimmed := strings.TrimSpace(part); trimmed != "" {
					taskIDs = append(taskIDs, trimmed)
				}
			}
		}

		// First try REST endpoint to keep UI/agent parity
		if result, err := h.fetchSubmissionsViaREST(contractID, status, taskIDs); err == nil {
			return result, nil
		}

		// Fallback to store path
		var submissions []smart_contract.Submission
		var err error

		switch {
		case len(taskIDs) > 0:
			submissions, err = store.ListSubmissions(ctx, taskIDs)
		case contractID != "":
			tasks, tErr := store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
			if tErr != nil {
				return nil, fmt.Errorf("Failed to list tasks for contract: %v", tErr)
			}
			taskIDs = make([]string, len(tasks))
			for i, t := range tasks {
				taskIDs[i] = t.TaskID
			}
			submissions, err = store.ListSubmissions(ctx, taskIDs)
		default:
			tasks, tErr := store.ListTasks(smart_contract.TaskFilter{})
			if tErr != nil {
				return nil, fmt.Errorf("Failed to list tasks: %v", tErr)
			}
			taskIDs = make([]string, len(tasks))
			for i, t := range tasks {
				taskIDs[i] = t.TaskID
			}
			submissions, err = store.ListSubmissions(ctx, taskIDs)
		}
		if err != nil {
			return nil, fmt.Errorf("Failed to list submissions: %v", err)
		}

		if status != "" {
			filtered := make([]smart_contract.Submission, 0, len(submissions))
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

		return map[string]interface{}{
			"submissions": submissionMap,
			"total":       len(submissions),
		}, nil

	case "get_submission":
		submissionID, ok := args["submission_id"].(string)
		if !ok {
			return nil, fmt.Errorf("submission_id is required")
		}

		// Get all tasks to find submission
		tasks, err := store.ListTasks(smart_contract.TaskFilter{})
		if err != nil {
			return nil, fmt.Errorf("Failed to get tasks: %v", err)
		}

		taskIDs := make([]string, len(tasks))
		for i, task := range tasks {
			taskIDs[i] = task.TaskID
		}

		submissions, err := store.ListSubmissions(ctx, taskIDs)
		if err != nil {
			return nil, fmt.Errorf("Failed to get submissions: %v", err)
		}

		for _, sub := range submissions {
			if sub.SubmissionID == submissionID {
				return sub, nil
			}
		}

		return nil, fmt.Errorf("submission not found")

	case "review_submission":
		submissionID, ok := args["submission_id"].(string)
		if !ok {
			return nil, fmt.Errorf("submission_id is required")
		}

		action, ok := args["action"].(string)
		if !ok {
			return nil, fmt.Errorf("action is required")
		}

		// Validate action
		validActions := map[string]bool{
			"review":  true,
			"approve": true,
			"reject":  true,
		}
		if !validActions[action] {
			return nil, fmt.Errorf("invalid action. must be: review, approve, or reject")
		}

		reviewPayload := map[string]interface{}{
			"action": action,
		}
		if notes := h.toString(args["notes"]); notes != "" {
			reviewPayload["notes"] = notes
		}
		if rejectionType := h.toString(args["rejection_type"]); rejectionType != "" {
			reviewPayload["rejection_type"] = rejectionType
		}
		if result, err := h.postJSON(fmt.Sprintf("%s/api/smart_contract/submissions/%s/review", h.baseURL, submissionID),
			reviewPayload, apiKey); err == nil {
			return result, nil
		}

		// Update submission status
		var newStatus string
		switch action {
		case "review":
			newStatus = "reviewed"
		case "approve":
			newStatus = "approved"
		case "reject":
			newStatus = "rejected"
		}

		reviewNotes := ""
		rejectionType := ""
		if action == "reject" {
			reviewNotes = h.toString(args["notes"])
			rejectionType = h.toString(args["rejection_type"])
		}
		err := store.UpdateSubmissionStatus(ctx, submissionID, newStatus, reviewNotes, rejectionType)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return nil, fmt.Errorf("submission not found")
			}
			return nil, fmt.Errorf("Failed to update submission: %v", err)
		}

		return map[string]interface{}{
			"message":       fmt.Sprintf("submission %sd successfully", action),
			"status":        newStatus,
			"submission_id": submissionID,
		}, nil

	case "rework_submission":
		submissionID, ok := args["submission_id"].(string)
		if !ok {
			return nil, fmt.Errorf("submission_id is required")
		}

		deliverables := h.toMap(args["deliverables"])
		notes := h.toString(args["notes"])

		if deliverables == nil && notes == "" {
			return nil, fmt.Errorf("deliverables or notes must be provided")
		}

		if result, err := h.postJSON(fmt.Sprintf("%s/api/smart_contract/submissions/%s/rework", h.baseURL, submissionID),
			map[string]interface{}{"deliverables": deliverables, "notes": notes}, apiKey); err == nil {
			return result, nil
		}

		// Get the original submission
		tasks, err := store.ListTasks(smart_contract.TaskFilter{})
		if err != nil {
			return nil, fmt.Errorf("Failed to get tasks: %v", err)
		}

		taskIDs := make([]string, len(tasks))
		for i, task := range tasks {
			taskIDs[i] = task.TaskID
		}

		submissions, err := store.ListSubmissions(ctx, taskIDs)
		if err != nil {
			return nil, fmt.Errorf("Failed to get submissions: %v", err)
		}

		var originalSubmission *smart_contract.Submission
		for _, sub := range submissions {
			if sub.SubmissionID == submissionID {
				originalSubmission = &sub
				break
			}
		}

		if originalSubmission == nil {
			return nil, fmt.Errorf("submission not found")
		}

		// Update deliverables if provided
		if deliverables != nil {
			originalSubmission.Deliverables = deliverables
		}

		// Add rework notes to deliverables
		if notes != "" {
			if originalSubmission.Deliverables == nil {
				originalSubmission.Deliverables = make(map[string]interface{})
			}
			originalSubmission.Deliverables["rework_notes"] = notes
			originalSubmission.Deliverables["reworked_at"] = time.Now().Format(time.RFC3339)
		}

		// Reset status to pending_review
		err = store.UpdateSubmissionStatus(ctx, submissionID, "pending_review", "", "")
		if err != nil {
			return nil, fmt.Errorf("Failed to update submission: %v", err)
		}

		return map[string]interface{}{
			"message":       "rework submitted successfully",
			"status":        "pending_review",
			"submission_id": submissionID,
		}, nil

	case "list_events":
		// For now, return empty events since the original server has in-memory events
		events := []smart_contract.Event{}

		return map[string]interface{}{
			"events": events,
			"total":  len(events),
		}, nil

	case "scan_image":
		imageDataStr, ok := args["image_data"].(string)
		if !ok {
			return nil, fmt.Errorf("image_data is required (base64 encoded)")
		}

		// Decode base64 image data
		imageData, err := base64.StdEncoding.DecodeString(imageDataStr)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 image_data: %v", err)
		}

		if h.scannerManager == nil {
			return nil, fmt.Errorf("scanner not available")
		}

		options := core.ScanOptions{
			ExtractMessage:      h.toBool(args["extract_message"]),
			ConfidenceThreshold: h.toFloat64(args["confidence_threshold"]),
			IncludeMetadata:     h.toBool(args["include_metadata"]),
		}
		if options.ConfidenceThreshold == 0 {
			options.ConfidenceThreshold = 0.5 // default
		}

		result, err := h.scannerManager.ScanImage(imageData, options)
		if err != nil {
			return nil, fmt.Errorf("failed to scan image: %v", err)
		}

		return result, nil

	case "scan_block":
		blockHeight, ok := args["block_height"].(float64)
		if !ok {
			return nil, fmt.Errorf("block_height is required")
		}

		if h.scannerManager == nil {
			return nil, fmt.Errorf("scanner not available")
		}

		options := core.ScanOptions{
			ExtractMessage:      h.toBool(args["extract_message"]),
			ConfidenceThreshold: h.toFloat64(args["confidence_threshold"]),
			IncludeMetadata:     h.toBool(args["include_metadata"]),
		}
		if options.ConfidenceThreshold == 0 {
			options.ConfidenceThreshold = 0.5 // default
		}

		result, err := h.scannerManager.ScanBlock(int64(blockHeight), options)
		if err != nil {
			return nil, fmt.Errorf("failed to scan block: %v", err)
		}

		return result, nil

	case "extract_message":
		imageDataStr, ok := args["image_data"].(string)
		if !ok {
			return nil, fmt.Errorf("image_data is required (base64 encoded)")
		}

		method := h.toString(args["method"])
		if method == "" {
			method = "alpha" // default method
		}

		// Decode base64 image data
		imageData, err := base64.StdEncoding.DecodeString(imageDataStr)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 image_data: %v", err)
		}

		if h.scannerManager == nil {
			return nil, fmt.Errorf("scanner not available")
		}

		result, err := h.scannerManager.ExtractMessage(imageData, method)
		if err != nil {
			return nil, fmt.Errorf("failed to extract message: %v", err)
		}

		return result, nil

	case "get_scanner_info":
		if h.scannerManager == nil {
			return map[string]interface{}{
				"scanner_info":   nil,
				"health_status":  nil,
				"scanner_type":   "none",
				"is_initialized": false,
				"error":          "scanner manager not configured",
			}, nil
		}

		info := h.scannerManager.GetScannerInfo()
		health := h.scannerManager.GetHealthStatus()

		return map[string]interface{}{
			"scanner_info":   info,
			"health_status":  health,
			"scanner_type":   h.scannerManager.GetScannerType(),
			"is_initialized": h.scannerManager.IsInitialized(),
		}, nil

	default:
		return nil, fmt.Errorf("Unknown tool '%s'. Tool name must be one of the available tools listed at /mcp/tools. See /mcp/docs for documentation", toolName)
	}
}

// fetchSubmissionsViaREST tries the REST endpoint first to keep parity between UI and MCP tools.
func (h *HTTPMCPServer) fetchSubmissionsViaREST(contractID, status string, taskIDs []string) (map[string]interface{}, error) {
	params := url.Values{}
	if contractID != "" {
		params.Set("contract_id", contractID)
	}
	if status != "" {
		params.Set("status", status)
	}
	if len(taskIDs) > 0 {
		params.Set("task_ids", strings.Join(taskIDs, ","))
	}

	urlStr := h.baseURL + "/api/smart_contract/submissions"
	if enc := params.Encode(); enc != "" {
		urlStr += "?" + enc
	}

	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("REST returned %d", resp.StatusCode)
	}

	var parsed map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// bodyHeaderFallback attempts to pull X-API-Key from a nested headers map in body if present.
func bodyHeaderFallback(body map[string]interface{}) string {
	if body == nil {
		return ""
	}
	if hdrs, ok := body["headers"].(map[string]interface{}); ok {
		if key, ok2 := hdrs["X-API-Key"].(string); ok2 {
			return key
		}
	}
	return ""
}

// fetchContractsViaREST lists contracts via REST with optional filters.
func (h *HTTPMCPServer) fetchContractsViaREST(filter smart_contract.ContractFilter) (map[string]interface{}, error) {
	params := url.Values{}
	if filter.Status != "" {
		params.Set("status", filter.Status)
	}
	if filter.Creator != "" {
		params.Set("creator", filter.Creator)
	}
	if filter.AiIdentifier != "" {
		params.Set("ai_identifier", filter.AiIdentifier)
	}
	if len(filter.Skills) > 0 {
		for _, s := range filter.Skills {
			params.Add("skills", s)
		}
	}
	urlStr := h.baseURL + "/api/smart_contract/contracts"
	if enc := params.Encode(); enc != "" {
		urlStr += "?" + enc
	}
	return h.getJSON(urlStr)
}

// fetchTasksViaREST lists tasks via REST with filters matching TaskFilter.
func (h *HTTPMCPServer) fetchTasksViaREST(filter smart_contract.TaskFilter) (map[string]interface{}, error) {
	params := url.Values{}
	if len(filter.Skills) > 0 {
		for _, s := range filter.Skills {
			params.Add("skills", s)
		}
	}
	if filter.MaxDifficulty != "" {
		params.Set("max_difficulty", filter.MaxDifficulty)
	}
	if filter.Status != "" {
		params.Set("status", filter.Status)
	}
	if filter.Limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", filter.Limit))
	}
	if filter.Offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", filter.Offset))
	}
	if filter.MinBudgetSats > 0 {
		params.Set("min_budget_sats", fmt.Sprintf("%d", filter.MinBudgetSats))
	}
	if filter.ContractID != "" {
		params.Set("contract_id", filter.ContractID)
	}
	if filter.ClaimedBy != "" {
		params.Set("claimed_by", filter.ClaimedBy)
	}

	urlStr := h.baseURL + "/api/smart_contract/tasks"
	if enc := params.Encode(); enc != "" {
		urlStr += "?" + enc
	}
	return h.getJSON(urlStr)
}

// fetchProposalsViaREST lists proposals via REST with filters.
func (h *HTTPMCPServer) fetchProposalsViaREST(filter smart_contract.ProposalFilter) (map[string]interface{}, error) {
	params := url.Values{}
	if filter.Status != "" {
		params.Set("status", filter.Status)
	}
	if len(filter.Skills) > 0 {
		for _, s := range filter.Skills {
			params.Add("skills", s)
		}
	}
	if filter.MinBudget > 0 {
		params.Set("min_budget_sats", fmt.Sprintf("%d", filter.MinBudget))
	}
	if filter.ContractID != "" {
		params.Set("contract_id", filter.ContractID)
	}
	if filter.MaxResults > 0 {
		params.Set("limit", fmt.Sprintf("%d", filter.MaxResults))
	}
	if filter.Offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", filter.Offset))
	}

	urlStr := h.baseURL + "/api/smart_contract/proposals"
	if enc := params.Encode(); enc != "" {
		urlStr += "?" + enc
	}
	return h.getJSON(urlStr)
}

// postJSON posts a JSON body to the given URL with optional API key and returns decoded JSON.
func (h *HTTPMCPServer) postJSON(urlStr string, body map[string]interface{}, apiKey string) (map[string]interface{}, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, urlStr, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	} else if h.apiKeyStore != nil {
		req.Header.Set("X-API-Key", bodyHeaderFallback(body))
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("REST returned %d", resp.StatusCode)
	}

	var parsed map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// getJSON fetches a URL (GET) and decodes JSON response.
func (h *HTTPMCPServer) getJSON(urlStr string) (map[string]interface{}, error) {
	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("REST returned %d", resp.StatusCode)
	}

	var parsed map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func (h *HTTPMCPServer) sendErrorResponse(w http.ResponseWriter, status int, resp MCPResponse) {
	if resp.Message == "" && resp.Error != "" {
		resp.Message = resp.Error
	}
	if resp.Code == 0 {
		resp.Code = status
	}
	if resp.Timestamp == "" {
		resp.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func (h *HTTPMCPServer) writeHTTPError(w http.ResponseWriter, status int, code string, message string, hint string) {
	h.sendErrorResponse(w, status, MCPResponse{
		Success:   false,
		Error:     message,
		ErrorCode: code,
		Message:   message,
		Hint:      hint,
	})
}

// Helper functions
func (h *HTTPMCPServer) toString(val interface{}) string {
	if val == nil {
		return ""
	}
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

func (h *HTTPMCPServer) toBool(val interface{}) bool {
	if b, ok := val.(bool); ok {
		return b
	}
	if s, ok := val.(string); ok {
		return strings.ToLower(s) == "true"
	}
	return false
}

func (h *HTTPMCPServer) toFloat64(val interface{}) float64 {
	if f, ok := val.(float64); ok {
		return f
	}
	if i, ok := val.(int); ok {
		return float64(i)
	}
	if i, ok := val.(int64); ok {
		return float64(i)
	}
	return 0.0
}

func (h *HTTPMCPServer) toMap(val interface{}) map[string]interface{} {
	if m, ok := val.(map[string]interface{}); ok {
		return m
	}
	return nil
}

// publishProposalTasks publishes the tasks stored in a proposal into MCP tasks
func (h *HTTPMCPServer) publishProposalTasks(ctx context.Context, proposalID string) error {
	p, err := h.store.GetProposal(ctx, proposalID)
	if err != nil {
		return err
	}
	if len(p.Tasks) == 0 {
		// Try to derive tasks from metadata embedded_message
		if em, ok := p.Metadata["embedded_message"].(string); ok && em != "" {
			p.Tasks = scstore.BuildTasksFromMarkdown(p.ID, em, p.VisiblePixelHash, p.BudgetSats, scstore.FundingAddressFromMeta(p.Metadata))
		}
		if len(p.Tasks) == 0 {
			return nil
		}
	}
	// Build a contract from the proposal, then upsert tasks
	contract := smart_contract.Contract{
		ContractID:          p.ID,
		Title:               p.Title,
		TotalBudgetSats:     p.BudgetSats,
		GoalsCount:          1,
		AvailableTasksCount: len(p.Tasks),
		Status:              "active",
	}
	// Preserve hashes/funding if present
	fundingAddr := scstore.FundingAddressFromMeta(p.Metadata)
	tasks := make([]smart_contract.Task, 0, len(p.Tasks))
	for _, t := range p.Tasks {
		task := t
		if task.ContractID == "" {
			task.ContractID = p.ID
		}
		if task.MerkleProof == nil && p.VisiblePixelHash != "" {
			task.MerkleProof = &smart_contract.MerkleProof{
				VisiblePixelHash:   p.VisiblePixelHash,
				FundedAmountSats:   p.BudgetSats / int64(len(p.Tasks)),
				FundingAddress:     fundingAddr,
				ConfirmationStatus: "provisional",
			}
		}
		if task.MerkleProof != nil && task.MerkleProof.FundingAddress == "" {
			task.MerkleProof.FundingAddress = fundingAddr
		}
		tasks = append(tasks, task)
	}
	if pg, ok := h.store.(interface {
		UpsertContractWithTasks(context.Context, smart_contract.Contract, []smart_contract.Task) error
	}); ok {
		if err := pg.UpsertContractWithTasks(ctx, contract, tasks); err != nil {
			return err
		}
		return nil
	}
	return nil
}
