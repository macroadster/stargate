package mcp

import "strings"

const (
	ToolCategoryDiscovery = "discovery" // list, get, scan - no auth
	ToolCategoryWrite     = "write"     // create, claim, submit - requires auth
	ToolCategoryUtility   = "utility"   // helpers, tools
)

// ToolMetadata contains lightweight metadata for search
type ToolMetadata struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Category     string `json:"category"`
	AuthRequired bool   `json:"auth_required"`
}

// getToolSchemas returns detailed schemas for all available tools
func (h *HTTPMCPServer) getToolSchemas() map[string]interface{} {
	return map[string]interface{}{
		"list_contracts": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
			"description": "List available smart contracts with optional filtering and pagination",
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
			"category":    ToolCategoryWrite,
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
			"category":    ToolCategoryDiscovery,
			"description": "List proposals with filtering and pagination",
			"parameters": map[string]interface{}{
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
			"category":    ToolCategoryWrite,
			"description": "Create a new wish (request for work) by inscribing a message. This creates a pending wish contract that agents can then propose solutions for using 'create_proposal'.",
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
		"get_auth_challenge": map[string]interface{}{
			"category":    ToolCategoryDiscovery,
			"description": "Get a cryptographic challenge for wallet verification",
			"parameters": map[string]interface{}{
				"wallet_address": map[string]interface{}{
					"type":        "string",
					"description": "Bitcoin wallet address to verify",
					"required":    true,
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Get challenge for wallet verification",
					"arguments":   map[string]interface{}{"wallet_address": "tb1qexample..."},
				},
			},
		},
		"verify_auth_challenge": map[string]interface{}{
			"category":    ToolCategoryWrite,
			"description": "Verify wallet signature and receive API key",
			"parameters": map[string]interface{}{
				"wallet_address": map[string]interface{}{
					"type":        "string",
					"description": "Bitcoin wallet address",
					"required":    true,
				},
				"signature": map[string]interface{}{
					"type":        "string",
					"description": "Bitcoin signature of the challenge nonce",
					"required":    true,
				},
				"email": map[string]interface{}{
					"type":        "string",
					"description": "Optional email address for account recovery",
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
		metadata = append(metadata, ToolMetadata{
			Name:         name,
			Description:  description,
			Category:     category,
			AuthRequired: h.toolRequiresAuth(name),
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
			if !nameMatch && !descMatch {
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
