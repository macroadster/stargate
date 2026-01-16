package mcp

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
		"create_proposal": map[string]interface{}{
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
		"create_contract": map[string]interface{}{
			"description": "Create a smart contract record for a stego image",
			"parameters": map[string]interface{}{
				"contract_id": map[string]interface{}{
					"type":        "string",
					"description": "Contract ID to create",
					"required":    true,
				},
				"block_height": map[string]interface{}{
					"type":        "integer",
					"description": "Optional block height",
				},
				"contract_type": map[string]interface{}{
					"type":        "string",
					"description": "Contract type (e.g., steganographic)",
				},
				"metadata": map[string]interface{}{
					"type":        "object",
					"description": "Additional metadata for the contract",
				},
			},
			"examples": []map[string]interface{}{
				{
					"description": "Create a stego contract",
					"arguments": map[string]interface{}{
						"contract_id":   "contract-123",
						"contract_type": "steganographic",
						"metadata": map[string]interface{}{
							"visible_pixel_hash": "contract-123",
						},
					},
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
		"list_events": map[string]interface{}{
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
	}
}
