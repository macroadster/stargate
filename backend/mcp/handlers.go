package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
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
		"tools":          tools,
		"tool_names":     toolNames,
		"total":          len(tools),
		"categories":     h.getCategoriesMap(),
		"http_endpoints": h.getHTTPEndpointsMap(base),
		"agent_assets":   h.getAgentAssetsMap(base),
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
		return
	}

	sessionID := h.createSession()
	base := h.externalBaseURL(r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("MCP-Session-Id", sessionID)
	w.Header().Set("Accept", "application/json, text/event-stream")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      nil,
		"result": map[string]interface{}{
			"protocolVersion": "2025-03-26",
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
				"logging":   map[string]bool{},
				"streaming": map[string]bool{"accept": true},
			},
			"serverInfo": map[string]string{
				"name":    "starlight",
				"version": "1.0.0",
			},
			"instructions": "Use POST /mcp with JSON-RPC to call tools. Pass 'sessionId' in MCP-Session-Id header for Streamable HTTP.",
			"links": map[string]string{
				"docs":   base + "/mcp/docs",
				"skill":  base + "/mcp/SKILL.md",
				"sdk":    base + "/mcp/starlight_sdk.sh",
				"tools":  base + "/mcp/tools",
				"events": base + "/mcp/events",
			},
		},
	})
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
		"http_endpoints": h.getHTTPEndpointsMap(base),
		"agent_assets":   h.getAgentAssetsMap(base),
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
		"endpoints": []string{"/mcp", "/mcp/tools", "/mcp/call", "/mcp/docs", "/mcp/SKILL.md", "/mcp/starlight_sdk.sh"},
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
	// Support Streamable HTTP for real-time events
	// Set the necessary headers for streaming
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Ensure the ResponseWriter supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeHTTPError(w, http.StatusInternalServerError, "STREAMING_NOT_SUPPORTED", "Streaming not supported", "Streaming not possible with current server configuration.")
		return
	}

	// Send the 'endpoint' event telling the client where to send POST requests
	// This is standard for MCP over HTTP (Streamable HTTP)
	base := h.externalBaseURL(r)
	endpointURL := base + "/mcp/call"
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointURL)
	flusher.Flush()

	// Keep the connection open for the client session
	// We can also send a periodic heartbeat to prevent timeouts
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			// Client disconnected
			return
		case <-ticker.C:
			// Send heartbeat/ping to keep connection alive
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

type ChatSendRequest struct {
	RoomID  string                 `json:"room_id"`
	AgentID string                 `json:"agent_id"`
	Content string                 `json:"content"`
	Type    string                 `json:"type"` // "message", "typing"
	Meta    map[string]interface{} `json:"meta,omitempty"`
}

type ChatSendResponse struct {
	Success   bool   `json:"success"`
	MessageID int64  `json:"message_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (h *HTTPMCPServer) handleChatStream(w http.ResponseWriter, r *http.Request) {
	roomID := r.URL.Query().Get("room")
	agentID := r.URL.Query().Get("agent")

	if roomID == "" {
		h.writeHTTPError(w, http.StatusBadRequest, "MISSING_ROOM", "Room ID required", "Specify 'room' query parameter.")
		return
	}
	if agentID == "" {
		h.writeHTTPError(w, http.StatusBadRequest, "MISSING_AGENT", "Agent ID required", "Specify 'agent' query parameter.")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeHTTPError(w, http.StatusInternalServerError, "SSE_NOT_SUPPORTED", "SSE not supported", "Streaming not possible with current server configuration.")
		return
	}

	msgs := h.chatHub.JoinRoom(roomID, agentID)
	defer h.chatHub.LeaveRoom(roomID, agentID)

	recentMsgs := h.chatHub.GetRecentMessages(roomID, 50)
	if len(recentMsgs) > 0 {
		historyMsg := &ChatMessage{
			Type:      "history",
			RoomID:    roomID,
			AgentID:   agentID,
			Timestamp: time.Now().UnixMilli(),
			Meta:      map[string]interface{}{"messages": recentMsgs},
		}
		historyJSON, _ := json.Marshal(historyMsg)
		fmt.Fprintf(w, "event: chat\ndata: %s\n\n", historyJSON)
		flusher.Flush()
	}

	joinMsg := &ChatMessage{
		Type:      "join",
		RoomID:    roomID,
		AgentID:   agentID,
		Timestamp: time.Now().UnixMilli(),
	}
	joinJSON, _ := json.Marshal(joinMsg)
	fmt.Fprintf(w, "event: chat\ndata: %s\n\n", joinJSON)
	flusher.Flush()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			leaveMsg := &ChatMessage{
				Type:      "leave",
				RoomID:    roomID,
				AgentID:   agentID,
				Timestamp: time.Now().UnixMilli(),
			}
			leaveJSON, _ := json.Marshal(leaveMsg)
			fmt.Fprintf(w, "event: chat\ndata: %s\n\n", leaveJSON)
			return
		case msg := <-msgs:
			fmt.Fprintf(w, "event: chat\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-time.After(30 * time.Second):
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func (h *HTTPMCPServer) handleChatSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeHTTPError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "Use POST /mcp/chat/send.")
		return
	}

	var req ChatSendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeHTTPError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON", "Request body must be valid JSON.")
		return
	}

	if req.RoomID == "" {
		h.writeHTTPError(w, http.StatusBadRequest, "MISSING_ROOM", "Room ID required", "Specify 'room_id' field in request.")
		return
	}
	if req.AgentID == "" {
		h.writeHTTPError(w, http.StatusBadRequest, "MISSING_AGENT", "Agent ID required", "Specify 'agent_id' field in request.")
		return
	}
	if req.Content == "" && req.Type != "typing" {
		h.writeHTTPError(w, http.StatusBadRequest, "MISSING_CONTENT", "Content required", "Specify 'content' field in request.")
		return
	}

	msgType := req.Type
	if msgType == "" {
		msgType = "message"
	}

	msg := &ChatMessage{
		Type:      msgType,
		RoomID:    req.RoomID,
		AgentID:   req.AgentID,
		Content:   req.Content,
		Timestamp: time.Now().UnixMilli(),
		Meta:      req.Meta,
	}

	h.chatHub.SendToRoom(req.RoomID, msg)

	resp := ChatSendResponse{
		Success:   true,
		MessageID: msg.Timestamp,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *HTTPMCPServer) handleChatMembers(w http.ResponseWriter, r *http.Request) {
	roomID := r.URL.Query().Get("room")

	if roomID == "" {
		h.writeHTTPError(w, http.StatusBadRequest, "MISSING_ROOM", "Room ID required", "Specify 'room' query parameter.")
		return
	}

	members := h.chatHub.GetRoomMembers(roomID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"room_id": roomID,
		"members": members,
	})
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

	result, err := h.callToolDirect(r.Context(), req.Tool, req.Arguments, apiKey, r)
	if err != nil {
		// Handle structured errors - always return 200 OK with error in JSON-RPC format
		h.writeStructuredErrorJSONRPC(w, err)
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
