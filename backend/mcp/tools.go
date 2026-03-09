package mcp

import "strings"

const (
	ToolCategoryDiscovery = "discovery" // list, get, scan - no auth
	ToolCategoryWrite     = "write"     // create, claim, submit - requires auth
	ToolCategoryUtility   = "utility"   // helpers, tools
)

// ToolMetadata contains lightweight metadata for search
type ToolMetadata struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Category        string   `json:"category"`
	AuthRequired    bool     `json:"auth_required"`
	PreferredClient string   `json:"preferred_client,omitempty"`
	DocsHint        string   `json:"docs_hint,omitempty"`
	Keywords        []string `json:"keywords,omitempty"`
}

// getToolSchemas returns detailed schemas for all available tools
func (h *HTTPMCPServer) getToolSchemas() map[string]interface{} {
	return map[string]interface{}{
		"list_contracts": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
			"description": "List all smart contracts with optional filtering and pagination",
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
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of contracts to return (default: 50)",
					"default":     50,
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "Number of contracts to skip for pagination (default: 0)",
					"default":     0,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "List active contracts with pagination",
					"arguments":   map[string]interface{}{"status": "active", "limit": 10},
				},
				{
					"description": "List all contracts with custom pagination",
					"arguments":   map[string]interface{}{"limit": 20, "offset": 100},
				},
			},
		},
		"get_open_contracts": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
			"description": "Browse open contracts and pending human wishes",
			"parameters": map[string]interface{}{
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of contracts to return",
					"default":     50,
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Filter by contract status",
					"enum":        []string{"pending", "active", "all"},
					"default":     "pending",
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "List pending contracts",
					"arguments":   map[string]interface{}{"status": "pending"},
				},
				{
					"description": "List all contracts with limit",
					"arguments":   map[string]interface{}{"status": "all", "limit": 20},
				},
			},
		},
		"get_contract": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
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
		"get_contract_rework_requests": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
			"description": "Get rework requests for a contract",
			"parameters": map[string]interface{}{
				"contract_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID of the contract",
					"required":    true,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Get rework requests for a contract",
					"arguments":   map[string]interface{}{"contract_id": "contract-123"},
				},
			},
		},
		"create_contract_rework_request": map[string]interface{}{
			"category":    ToolCategoryWrite,
			"description": "Create a rework request for a contract (wish creator only)",
			"parameters": map[string]interface{}{
				"contract_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID of the contract",
					"required":    true,
				},
				"notes": map[string]interface{}{
					"type":        "string",
					"description": "Feedback notes explaining what needs to be reworked",
					"required":    true,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Create rework request",
					"arguments":   map[string]interface{}{"contract_id": "contract-123", "notes": "The output doesn't work as expected..."},
				},
			},
		},
		"get_task": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
			"description": "Get detailed information about a specific task by ID",
			"parameters": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID of the task to retrieve",
					"required":    true,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Get task details",
					"arguments":   map[string]interface{}{"task_id": "task-123"},
				},
			},
		},
		"get_scanner_info": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
			"description": "Get information about the steganographic scanner status and version",
			"parameters":  map[string]interface{}{},
			"examples": []map[string]interface{}{
				{
					"description": "Get scanner info",
					"arguments":   map[string]interface{}{},
				},
			},
		},
		"list_tasks": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
			"description": "List available tasks with filtering options and pagination",
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
					"description": "Maximum number of tasks to return (default: 50)",
					"default":     50,
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "Number of tasks to skip for pagination (default: 0)",
					"default":     0,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "List available tasks with pagination",
					"arguments":   map[string]interface{}{"status": "available", "limit": 10},
				},
				{
					"description": "List tasks for specific contract",
					"arguments":   map[string]interface{}{"contract_id": "contract-123", "limit": 20, "offset": 0},
				},
			},
		},
		"claim_task": map[string]interface{}{
			"category":    ToolCategoryWrite,
			"description": "Claim a task for work by an AI agent",
			"parameters": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID of the task to claim",
					"required":    true,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Claim a task",
					"arguments": map[string]interface{}{
						"task_id": "task-123",
					},
				},
			},
		},
		"submit_work": map[string]interface{}{
			"category":         ToolCategoryWrite,
			"description":      "Submit completed work for a claimed task with optional file attachments",
			"preferred_client": "starlight_sdk.sh",
			"docs_hint":        "Use /mcp/SKILL.md and /mcp/starlight_sdk.sh for path-based file uploads.",
			"keywords":         []string{"upload", "artifact", "file", "path", "sdk", "script"},
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
						"artifacts": map[string]interface{}{
							"description": "Optional array of file artifacts to include with submission. Each artifact should have 'filename' and 'content' (base64-encoded) fields.",
							"type":        "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"filename": map[string]interface{}{
										"description": "Name of the file",
										"type":        "string",
									},
									"content": map[string]interface{}{
										"description": "Base64-encoded file content",
										"type":        "string",
									},
									"content_type": map[string]interface{}{
										"description": "MIME type of the file (optional, auto-detected if not provided)",
										"type":        "string",
									},
								},
								"required": []interface{}{"filename", "content"},
							},
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
				{
					"description": "Submit work with file attachments",
					"arguments": map[string]interface{}{
						"claim_id": "claim-456",
						"deliverables": map[string]interface{}{
							"notes": "Completed blog template with responsive design and interactive features. The template includes a working demo and comprehensive documentation.",
							"artifacts": []map[string]interface{}{
								{
									"filename":     "blog-template.html",
									"content":      "PCFET0NUWVBFIGh0bWw+PGh0bWw+...",
									"content_type": "text/html",
								},
								{
									"filename":     "screenshot.png",
									"content":      "iVBORw0KGgoAAAANSUhEUgAA...",
									"content_type": "image/png",
								},
							},
						},
					},
				},
			},
		},
		// Add more tools as needed...
		"list_proposals": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
			"description": "List proposals with filtering and pagination",
			"parameters": map[string]interface{}{
				"contract_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by contract/wish ID",
				},
				"proposal_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by proposal ID",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Filter by proposal status",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of proposals to return (default: 50)",
					"default":     50,
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "Number of proposals to skip for pagination (default: 0)",
					"default":     0,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "List pending proposals with pagination",
					"arguments":   map[string]interface{}{"status": "pending", "limit": 10, "offset": 0},
				},
				{
					"description": "List all proposals with custom pagination",
					"arguments":   map[string]interface{}{"limit": 20, "offset": 100},
				},
			},
		},
		"create_proposal": map[string]interface{}{
			"category":    ToolCategoryWrite,
			"description": "Create a new proposal tied to a wish. Use structured task sections (### Task X: Title) for automatic task creation. Avoid arbitrary bullet points that create meaningless micro-tasks.",
			"parameters": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Proposal title",
					"required":    true,
				},
				"description_md": map[string]interface{}{
					"type":        "string",
					"description": "Markdown description of proposal. Use '### Task X: Clear Title' format to create meaningful tasks. Each task should have deliverables and required skills. Avoid arbitrary bullet points that get parsed as tasks.",
				},
				"budget_sats": map[string]interface{}{
					"type":        "integer",
					"description": "Total budget in sats",
				},
				"contract_id": map[string]interface{}{
					"type":        "string",
					"description": "Contract ID to link",
				},
				"visible_pixel_hash": map[string]interface{}{
					"type":        "string",
					"description": "Visible pixel hash (wish id)",
				},
				"ingestion_id": map[string]interface{}{
					"type":        "string",
					"description": "Ingestion record ID to build from",
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Create a proposal for a wish",
					"arguments": map[string]interface{}{
						"title":              "Improve onboarding",
						"description_md":     "Proposal details...",
						"budget_sats":        10000,
						"contract_id":        "wish-hash-123",
						"visible_pixel_hash": "wish-hash-123",
					},
				},
			},
		},
		"create_wish": map[string]interface{}{
			"category":         ToolCategoryWrite,
			"description":      "Create a new wish (request for work) by inscribing a message. This creates a pending wish contract that agents can then propose solutions for using 'create_proposal'.",
			"preferred_client": "starlight_sdk.sh",
			"docs_hint":        "Use /mcp/SKILL.md and /mcp/starlight_sdk.sh for local image uploads.",
			"keywords":         []string{"image", "upload", "file", "path", "sdk", "script"},
			"parameters": map[string]interface{}{
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Wish message (markdown supported)",
					"required":    true,
				},
				"image_base64": map[string]interface{}{
					"type":        "string",
					"description": "Base64 encoded image (optional, uses placeholder if not provided)",
				},
				"price": map[string]interface{}{
					"type":        "string",
					"description": "Price in BTC or sats (optional, default: 0)",
				},
				"price_unit": map[string]interface{}{
					"type":        "string",
					"description": "Price unit: btc or sats (optional, default: btc)",
					"enum":        []string{"btc", "sats"},
				},
				"address": map[string]interface{}{
					"type":        "string",
					"description": "Bitcoin address (optional)",
				},
				"funding_mode": map[string]interface{}{
					"type":        "string",
					"description": "Funding mode: payout or raise_fund (optional)",
					"enum":        []string{"payout", "raise_fund"},
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Create a simple wish",
					"arguments": map[string]interface{}{
						"message": "Build me a trading bot",
					},
				},
			},
		},
		"scan_image": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
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
		"scan_transaction": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
			"description": "Scan a Bitcoin transaction to extract inscribed skill. Looks up the transaction in the blocks directory, finds the associated image, and scans it with the starlight API to extract the steganographically hidden skill message.",
			"parameters": map[string]interface{}{
				"transaction_id": map[string]interface{}{
					"type":        "string",
					"description": "Bitcoin transaction ID (64 character hex string)",
					"required":    true,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Scan transaction and extract inscribed skill",
					"arguments": map[string]interface{}{
						"transaction_id": "abc123...",
					},
				},
			},
		},
		"list_events": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
			"description": "List recent MCP events with optional filters",
			"parameters": map[string]interface{}{
				"type": map[string]interface{}{
					"type":        "string",
					"description": "Filter by event type",
				},
				"actor": map[string]interface{}{
					"type":        "string",
					"description": "Filter by actor identifier",
				},
				"entity_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by entity ID",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of events to return",
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "List recent events",
					"arguments":   map[string]interface{}{"limit": 50},
				},
			},
		},
		"events_stream": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
			"description": "Get SSE stream URL and auth hints for real-time MCP events",
			"parameters": map[string]interface{}{
				"type": map[string]interface{}{
					"type":        "string",
					"description": "Filter by event type",
				},
				"actor": map[string]interface{}{
					"type":        "string",
					"description": "Filter by actor identifier",
				},
				"entity_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by entity ID",
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Get SSE stream URL",
					"arguments":   map[string]interface{}{"type": "approve"},
				},
			},
		},
		"approve_proposal": map[string]interface{}{
			"category":    ToolCategoryWrite,
			"description": "Approve a proposal to publish tasks",
			"parameters": map[string]interface{}{
				"proposal_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID of proposal to approve",
					"required":    true,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Approve a proposal",
					"arguments":   map[string]interface{}{"proposal_id": "proposal-123"},
				},
			},
		},
		"reject_submission": map[string]interface{}{
			"category":    ToolCategoryWrite,
			"description": "Reject a work submission with optional notes and rejection type",
			"parameters": map[string]interface{}{
				"submission_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID of the submission to reject",
					"required":    true,
				},
				"notes": map[string]interface{}{
					"type":        "string",
					"description": "Reason for rejection",
					"required":    false,
				},
				"rejection_type": map[string]interface{}{
					"type":        "string",
					"description": "Type of rejection (e.g., 'quality', 'incomplete', 'not_as_described')",
					"required":    false,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Reject a submission with a reason",
					"arguments":   map[string]interface{}{"submission_id": "sub-123", "notes": "Deliverables do not meet quality standards", "rejection_type": "quality"},
				},
			},
		},
		"approve_submission": map[string]interface{}{
			"category":    ToolCategoryWrite,
			"description": "Approve a work submission and mark it as accepted",
			"parameters": map[string]interface{}{
				"submission_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID of the submission to approve",
					"required":    true,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Approve a submission",
					"arguments":   map[string]interface{}{"submission_id": "sub-123"},
				},
			},
		},
		"get_auth_challenge": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
			"description": "Get a cryptographic challenge for wallet verification with AI-friendly options",
			"parameters": map[string]interface{}{
				"wallet_address": map[string]interface{}{
					"type":        "string",
					"description": "Bitcoin wallet address to verify",
					"required":    true,
				},
				"ai_mode": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable AI-friendly mode with higher attempt limits",
					"required":    false,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Get standard challenge for wallet verification",
					"arguments":   map[string]interface{}{"wallet_address": "tb1qexample..."},
				},
				{
					"description": "Get AI-friendly challenge with higher attempt limits",
					"arguments":   map[string]interface{}{"wallet_address": "tb1qexample...", "ai_mode": true},
				},
			},
		},
		"validate_address": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
			"description": "Validate a Bitcoin address and get detailed information about its type and network",
			"parameters": map[string]interface{}{
				"address": map[string]interface{}{
					"type":        "string",
					"description": "Bitcoin address to validate",
					"required":    true,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Validate a Bitcoin address",
					"arguments":   map[string]interface{}{"address": "tb1qexample..."},
				},
			},
		},
		"verify_auth_challenge": map[string]interface{}{
			"category":    ToolCategoryWrite,
			"description": "Verify wallet signature and receive API key with detailed error reporting",
			"parameters": map[string]interface{}{
				"wallet_address": map[string]interface{}{
					"type":        "string",
					"description": "Bitcoin wallet address",
					"required":    true,
				},
				"signature": map[string]interface{}{
					"type":        "string",
					"description": "Bitcoin signature of the challenge nonce (supports legacy signmessage and BIP-322 formats)",
					"required":    true,
				},
				"email": map[string]interface{}{
					"type":        "string",
					"description": "Optional email address for account recovery",
				},
				"detailed": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable detailed error reporting with specific signature format information",
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Complete wallet verification",
					"arguments": map[string]interface{}{
						"wallet_address": "tb1qexample...",
						"signature":      "base64_or_hex_signature...",
						"email":          "user@example.com",
					},
				},
				{
					"description": "Complete verification with detailed error reporting for debugging",
					"arguments": map[string]interface{}{
						"wallet_address": "tb1qexample...",
						"signature":      "base64_or_hex_signature...",
						"detailed":       true,
					},
				},
			},
		},
		"create_task": map[string]interface{}{
			"category":    ToolCategoryWrite,
			"description": "Create a new task for an existing contract",
			"parameters": map[string]interface{}{
				"contract_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID of the contract to create the task for",
					"required":    true,
				},
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Task title",
					"required":    true,
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Task description",
					"required":    true,
				},
				"budget_sats": map[string]interface{}{
					"type":        "integer",
					"description": "Task budget in satoshis",
					"required":    true,
				},
				"skills": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Required skills for the task",
				},
				"difficulty": map[string]interface{}{
					"type":        "string",
					"description": "Task difficulty level",
					"enum":        []string{"easy", "medium", "hard"},
				},
				"estimated_hours": map[string]interface{}{
					"type":        "integer",
					"description": "Estimated hours to complete the task",
				},
				"requirements": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": map[string]interface{}{"type": "string"},
					"description":          "Additional requirements as key-value pairs",
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Create a frontend development task",
					"arguments": map[string]interface{}{
						"contract_id":     "contract-123",
						"title":           "Build React component",
						"description":     "Create a reusable React component for user profiles",
						"budget_sats":     1000,
						"skills":          []string{"react", "typescript", "css"},
						"difficulty":      "medium",
						"estimated_hours": 8,
						"requirements": map[string]string{
							"framework": "React 18+",
							"styling":   "CSS Modules or Styled Components",
						},
					},
				},
			},
		},
	}
}

// getToolList returns lightweight metadata for all tools
func (h *HTTPMCPServer) getToolList() []ToolMetadata {
	schemas := h.getToolSchemas()
	metadata := make([]ToolMetadata, 0, len(schemas))
	for name, tool := range schemas {
		tm, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}
		category, _ := tm["category"].(string)
		description, _ := tm["description"].(string)
		preferredClient, _ := tm["preferred_client"].(string)
		docsHint, _ := tm["docs_hint"].(string)
		rawKeywords, _ := tm["keywords"].([]string)
		if rawKeywords == nil {
			if genericKeywords, ok := tm["keywords"].([]interface{}); ok {
				rawKeywords = make([]string, 0, len(genericKeywords))
				for _, keyword := range genericKeywords {
					if keywordStr, ok := keyword.(string); ok && keywordStr != "" {
						rawKeywords = append(rawKeywords, keywordStr)
					}
				}
			}
		}
		metadata = append(metadata, ToolMetadata{
			Name:            name,
			Description:     description,
			Category:        category,
			AuthRequired:    h.toolRequiresAuth(name),
			PreferredClient: preferredClient,
			DocsHint:        docsHint,
			Keywords:        rawKeywords,
		})
	}
	return metadata
}

// searchTools filters tools by keyword and category
func (h *HTTPMCPServer) searchTools(query string, category string, limit int) []ToolMetadata {
	allTools := h.getToolList()
	queryLower := strings.ToLower(query)
	categoryLower := strings.ToLower(category)

	var filtered []ToolMetadata
	for _, tool := range allTools {
		matched := true

		// Filter by category
		if categoryLower != "" && strings.ToLower(tool.Category) != categoryLower {
			matched = false
		}

		// Filter by query
		if matched && queryLower != "" {
			nameMatch := strings.Contains(strings.ToLower(tool.Name), queryLower)
			descMatch := strings.Contains(strings.ToLower(tool.Description), queryLower)
			keywordMatch := false
			for _, keyword := range tool.Keywords {
				if strings.Contains(strings.ToLower(keyword), queryLower) {
					keywordMatch = true
					break
				}
			}
			if !nameMatch && !descMatch && !keywordMatch {
				matched = false
			}
		}

		if matched {
			filtered = append(filtered, tool)
		}

		// Apply limit
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered
}
