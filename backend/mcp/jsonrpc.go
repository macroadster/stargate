package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
)

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
	case "notifications/initialized":
		w.WriteHeader(http.StatusNoContent)
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
