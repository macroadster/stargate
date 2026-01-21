package mcp

// MCPRequest represents an incoming MCP tool call via HTTP
type MCPRequest struct {
	Tool      string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// MCPResponse represents response from an MCP tool call
type MCPResponse struct {
	Success        bool                   `json:"success"`
	Result         interface{}            `json:"result,omitempty"`
	Error          string                 `json:"error,omitempty"`
	ErrorCode      string                 `json:"error_code,omitempty"`
	Message        string                 `json:"message,omitempty"`
	Code           int                    `json:"code,omitempty"`
	Hint           string                 `json:"hint,omitempty"`
	Timestamp      string                 `json:"timestamp,omitempty"`
	RequiredFields []string               `json:"required_fields,omitempty"`
	DocsURL        string                 `json:"docs_url,omitempty"`
	RequestID      string                 `json:"request_id,omitempty"`
	Version        string                 `json:"version,omitempty"`
	Details        map[string]interface{} `json:"details,omitempty"`
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
