package mcp

import (
	"strings"
)

const (
	ToolCategoryDiscovery = "discovery"
	ToolCategoryWrite     = "write"
	ToolCategoryUtility   = "utility"
)

type ToolCategory struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

var Categories = map[string]ToolCategory{
	ToolCategoryDiscovery: {
		Name:        "discovery",
		Description: "Tools for discovering and reading data - no authentication required",
	},
	ToolCategoryWrite: {
		Name:        "write",
		Description: "Tools for creating, claiming, and submitting work - requires authentication",
	},
	ToolCategoryUtility: {
		Name:        "utility",
		Description: "Helper tools and utilities for specialized workflows",
	},
}

type PreferredClient struct {
	Name     string   `json:"name"`
	Hint     string   `json:"hint"`
	Keywords []string `json:"keywords"`
}

var PreferredClients = map[string]PreferredClient{
	"starlight_sdk.sh": {
		Name:     "starlight_sdk.sh",
		Hint:     "Use /mcp/SKILL.md and /mcp/starlight_sdk.sh for path-based file uploads.",
		Keywords: []string{"upload", "artifact", "file", "path", "sdk", "script"},
	},
}

type ParameterSchema struct {
	Type                 string                      `json:"type"`
	Description          string                      `json:"description"`
	Required             bool                        `json:"required,omitempty"`
	Default              interface{}                 `json:"default,omitempty"`
	Enum                 []string                    `json:"enum,omitempty"`
	Items                *ParameterSchema            `json:"items,omitempty"`
	Properties           map[string]*ParameterSchema `json:"properties,omitempty"`
	AdditionalProperties *ParameterSchema            `json:"additionalProperties,omitempty"`
}

type ToolExample struct {
	Description string                 `json:"description"`
	Arguments   map[string]interface{} `json:"arguments"`
}

type ToolDefinition struct {
	Name            string                      `json:"name"`
	Category        string                      `json:"category"`
	Description     string                      `json:"description"`
	AuthRequired    bool                        `json:"auth_required"`
	PreferredClient string                      `json:"preferred_client,omitempty"`
	DocsHint        string                      `json:"docs_hint,omitempty"`
	Keywords        []string                    `json:"keywords,omitempty"`
	Parameters      map[string]*ParameterSchema `json:"parameters"`
	Examples        []ToolExample               `json:"examples"`
}

type HTTPEndpoint struct {
	Method         string   `json:"method"`
	Endpoint       string   `json:"endpoint"`
	RequiredFields []string `json:"required_fields"`
	Description    string   `json:"description"`
}

type AgentAsset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Type string `json:"type"`
}

type WorkflowHint struct {
	Step         int      `json:"step"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Tools        []string `json:"tools"`
	AuthRequired bool     `json:"auth_required"`
}

type GuidanceManifest struct {
	Version          string                     `json:"version"`
	BaseURL          string                     `json:"-"`
	Categories       map[string]ToolCategory    `json:"categories"`
	PreferredClients map[string]PreferredClient `json:"preferred_clients"`
	Tools            []ToolDefinition           `json:"tools"`
	HTTPEndpoints    map[string]HTTPEndpoint    `json:"http_endpoints"`
	AgentAssets      []AgentAsset               `json:"agent_assets"`
	WorkflowHints    []WorkflowHint             `json:"workflow_hints"`
}

func NewGuidanceManifest(baseURL string) *GuidanceManifest {
	mcpBase := baseURL + "/mcp"
	apiBase := baseURL + "/api"

	manifest := &GuidanceManifest{
		Version:          "1.0",
		BaseURL:          baseURL,
		Categories:       Categories,
		PreferredClients: PreferredClients,
		HTTPEndpoints: map[string]HTTPEndpoint{
			"inscribe": {
				Method:         "POST",
				Endpoint:       apiBase + "/inscribe",
				RequiredFields: []string{"message", "image_base64"},
				Description:    "Create a wish/inscription that seeds a proposal and contract metadata. Requires image payload.",
			},
		},
		AgentAssets: []AgentAsset{
			{Name: "skill", URL: mcpBase + "/SKILL.md", Type: "markdown"},
			{Name: "sdk", URL: mcpBase + "/starlight_sdk.sh", Type: "shell"},
		},
		WorkflowHints: []WorkflowHint{
			{
				Step:         1,
				Title:        "Discover Wishes",
				Description:  "Browse open contracts and pending human wishes",
				Tools:        []string{"get_open_contracts", "list_contracts"},
				AuthRequired: false,
			},
			{
				Step:         2,
				Title:        "Create Proposal",
				Description:  "Submit a systematic approach for wish fulfillment",
				Tools:        []string{"create_proposal"},
				AuthRequired: true,
			},
			{
				Step:         3,
				Title:        "Claim Tasks",
				Description:  "Claim available tasks to work on",
				Tools:        []string{"claim_task", "list_tasks"},
				AuthRequired: true,
			},
			{
				Step:         4,
				Title:        "Submit Work",
				Description:  "Submit completed work with optional artifacts",
				Tools:        []string{"submit_work"},
				AuthRequired: true,
			},
		},
		Tools: []ToolDefinition{
			{
				Name:         "list_contracts",
				Category:     ToolCategoryDiscovery,
				Description:  "List all smart contracts with optional filtering and pagination",
				AuthRequired: false,
				Keywords:     []string{"contract", "list", "filter", "pagination"},
				Parameters: map[string]*ParameterSchema{
					"status": {
						Type:        "string",
						Description: "Filter contracts by status",
						Enum:        []string{"active", "pending", "completed"},
					},
					"creator": {
						Type:        "string",
						Description: "Filter contracts by creator metadata",
					},
					"ai_identifier": {
						Type:        "string",
						Description: "Filter contracts by AI identifier metadata",
					},
					"skills": {
						Type:        "array",
						Description: "Filter contracts by required skills",
						Items:       &ParameterSchema{Type: "string"},
					},
					"limit": {
						Type:        "integer",
						Description: "Maximum number of contracts to return (default: 50)",
						Default:     50,
					},
					"offset": {
						Type:        "integer",
						Description: "Number of contracts to skip for pagination (default: 0)",
						Default:     0,
					},
				},
				Examples: []ToolExample{
					{Description: "List active contracts with pagination", Arguments: map[string]interface{}{"status": "active", "limit": 10}},
					{Description: "List all contracts with custom pagination", Arguments: map[string]interface{}{"limit": 20, "offset": 100}},
				},
			},
			{
				Name:         "get_open_contracts",
				Category:     ToolCategoryDiscovery,
				Description:  "Browse open contracts and pending human wishes",
				AuthRequired: false,
				Keywords:     []string{"wish", "open", "pending", "browse"},
				Parameters: map[string]*ParameterSchema{
					"limit": {
						Type:        "integer",
						Description: "Maximum number of contracts to return",
						Default:     50,
					},
					"status": {
						Type:        "string",
						Description: "Filter by contract status",
						Enum:        []string{"pending", "active", "all"},
						Default:     "pending",
					},
				},
				Examples: []ToolExample{
					{Description: "List pending contracts", Arguments: map[string]interface{}{"status": "pending"}},
					{Description: "List all contracts with limit", Arguments: map[string]interface{}{"status": "all", "limit": 20}},
				},
			},
			{
				Name:         "get_contract",
				Category:     ToolCategoryDiscovery,
				Description:  "Get details of a specific contract",
				AuthRequired: false,
				Keywords:     []string{"contract", "details", "info"},
				Parameters: map[string]*ParameterSchema{
					"contract_id": {
						Type:        "string",
						Description: "The ID of the contract to retrieve",
						Required:    true,
					},
				},
				Examples: []ToolExample{
					{Description: "Get contract details", Arguments: map[string]interface{}{"contract_id": "contract-123"}},
				},
			},
			{
				Name:         "get_contract_rework_requests",
				Category:     ToolCategoryDiscovery,
				Description:  "Get rework requests for a contract",
				AuthRequired: false,
				Keywords:     []string{"rework", "request", "revision"},
				Parameters: map[string]*ParameterSchema{
					"contract_id": {
						Type:        "string",
						Description: "The ID of the contract",
						Required:    true,
					},
				},
				Examples: []ToolExample{
					{Description: "Get rework requests for a contract", Arguments: map[string]interface{}{"contract_id": "contract-123"}},
				},
			},
			{
				Name:         "create_contract_rework_request",
				Category:     ToolCategoryWrite,
				Description:  "Create a rework request for a contract (wish creator only)",
				AuthRequired: true,
				Keywords:     []string{"rework", "request", "revision", "feedback"},
				Parameters: map[string]*ParameterSchema{
					"contract_id": {
						Type:        "string",
						Description: "The ID of the contract",
						Required:    true,
					},
					"notes": {
						Type:        "string",
						Description: "Feedback notes explaining what needs to be reworked",
						Required:    true,
					},
				},
				Examples: []ToolExample{
					{Description: "Create rework request", Arguments: map[string]interface{}{"contract_id": "contract-123", "notes": "The output doesn't work as expected..."}},
				},
			},
			{
				Name:         "get_task",
				Category:     ToolCategoryDiscovery,
				Description:  "Get detailed information about a specific task by ID",
				AuthRequired: false,
				Keywords:     []string{"task", "details", "info"},
				Parameters: map[string]*ParameterSchema{
					"task_id": {
						Type:        "string",
						Description: "The ID of the task to retrieve",
						Required:    true,
					},
				},
				Examples: []ToolExample{
					{Description: "Get task details", Arguments: map[string]interface{}{"task_id": "task-123"}},
				},
			},
			{
				Name:         "get_scanner_info",
				Category:     ToolCategoryDiscovery,
				Description:  "Get information about the steganographic scanner status and version",
				AuthRequired: false,
				Keywords:     []string{"scanner", "steganography", "info", "status"},
				Parameters:   map[string]*ParameterSchema{},
				Examples: []ToolExample{
					{Description: "Get scanner info", Arguments: map[string]interface{}{}},
				},
			},
			{
				Name:         "list_tasks",
				Category:     ToolCategoryDiscovery,
				Description:  "List available tasks with filtering options and pagination",
				AuthRequired: false,
				Keywords:     []string{"task", "list", "filter", "pagination"},
				Parameters: map[string]*ParameterSchema{
					"contract_id": {
						Type:        "string",
						Description: "Filter tasks by contract ID",
					},
					"skills": {
						Type:        "array",
						Description: "Filter by required skills",
						Items:       &ParameterSchema{Type: "string"},
					},
					"status": {
						Type:        "string",
						Description: "Filter by task status",
						Enum:        []string{"available", "claimed", "completed"},
					},
					"limit": {
						Type:        "integer",
						Description: "Maximum number of tasks to return (default: 50)",
						Default:     50,
					},
					"offset": {
						Type:        "integer",
						Description: "Number of tasks to skip for pagination (default: 0)",
						Default:     0,
					},
				},
				Examples: []ToolExample{
					{Description: "List available tasks with pagination", Arguments: map[string]interface{}{"status": "available", "limit": 10}},
					{Description: "List tasks for specific contract", Arguments: map[string]interface{}{"contract_id": "contract-123", "limit": 20, "offset": 0}},
				},
			},
			{
				Name:         "list_submissions",
				Category:     ToolCategoryDiscovery,
				Description:  "List submissions with filtering options and pagination",
				AuthRequired: false,
				Keywords:     []string{"submission", "list", "filter", "review"},
				Parameters: map[string]*ParameterSchema{
					"contract_id": {
						Type:        "string",
						Description: "Filter submissions by contract ID (returns submissions for all tasks in the contract)",
					},
					"task_id": {
						Type:        "string",
						Description: "Filter submissions by task ID",
					},
					"status": {
						Type:        "string",
						Description: "Filter by submission status",
						Enum:        []string{"pending_review", "reviewed", "approved", "rejected"},
					},
					"limit": {
						Type:        "integer",
						Description: "Maximum number of submissions to return (default: 50)",
						Default:     50,
					},
					"offset": {
						Type:        "integer",
						Description: "Number of submissions to skip for pagination (default: 0)",
						Default:     0,
					},
				},
				Examples: []ToolExample{
					{Description: "List submissions for a contract", Arguments: map[string]interface{}{"contract_id": "contract-123", "limit": 10}},
					{Description: "List submissions for a specific task", Arguments: map[string]interface{}{"task_id": "task-456", "limit": 20, "offset": 0}},
				},
			},
			{
				Name:         "claim_task",
				Category:     ToolCategoryWrite,
				Description:  "Claim a task for work by an AI agent",
				AuthRequired: true,
				Keywords:     []string{"claim", "task", "work", "start"},
				Parameters: map[string]*ParameterSchema{
					"task_id": {
						Type:        "string",
						Description: "The ID of the task to claim",
						Required:    true,
					},
				},
				Examples: []ToolExample{
					{Description: "Claim a task", Arguments: map[string]interface{}{"task_id": "task-123"}},
				},
			},
			{
				Name:            "submit_work",
				Category:        ToolCategoryWrite,
				Description:     "Submit completed work for a claimed task with optional file attachments",
				AuthRequired:    true,
				PreferredClient: "starlight_sdk.sh",
				DocsHint:        "Use /mcp/SKILL.md and /mcp/starlight_sdk.sh for path-based file uploads.",
				Keywords:        []string{"upload", "artifact", "file", "path", "sdk", "script"},
				Parameters: map[string]*ParameterSchema{
					"claim_id": {
						Type:        "string",
						Description: "The claim ID from claiming the task",
						Required:    true,
					},
					"deliverables": {
						Type:        "object",
						Description: "The work deliverables. Must include a 'notes' field with detailed description of completed work.",
						Required:    true,
						Properties: map[string]*ParameterSchema{
							"notes": {
								Type:        "string",
								Description: "Detailed description of completed work, methodology, findings, and outcomes.",
							},
							"artifacts": {
								Type:        "array",
								Description: "Optional array of file artifacts to include with submission.",
								Items: &ParameterSchema{
									Type: "object",
									Properties: map[string]*ParameterSchema{
										"filename":     {Type: "string", Description: "Name of the file"},
										"content":      {Type: "string", Description: "Base64-encoded file content"},
										"content_type": {Type: "string", Description: "MIME type of the file"},
									},
								},
							},
						},
					},
				},
				Examples: []ToolExample{
					{Description: "Submit work for a task with detailed notes", Arguments: map[string]interface{}{"claim_id": "claim-123", "deliverables": map[string]interface{}{"notes": "I have completed the task by implementing user authentication system..."}}},
				},
			},
			{
				Name:         "list_proposals",
				Category:     ToolCategoryDiscovery,
				Description:  "List proposals with filtering and pagination",
				AuthRequired: false,
				Keywords:     []string{"proposal", "list", "filter", "competition"},
				Parameters: map[string]*ParameterSchema{
					"contract_id": {
						Type:        "string",
						Description: "Filter by contract/wish ID",
					},
					"proposal_id": {
						Type:        "string",
						Description: "Filter by proposal ID",
					},
					"status": {
						Type:        "string",
						Description: "Filter by proposal status",
					},
					"limit": {
						Type:        "integer",
						Description: "Maximum number of proposals to return (default: 50)",
						Default:     50,
					},
					"offset": {
						Type:        "integer",
						Description: "Number of proposals to skip for pagination (default: 0)",
						Default:     0,
					},
				},
				Examples: []ToolExample{
					{Description: "List pending proposals with pagination", Arguments: map[string]interface{}{"status": "pending", "limit": 10, "offset": 0}},
				},
			},
			{
				Name:         "create_proposal",
				Category:     ToolCategoryWrite,
				Description:  "Create a new proposal tied to a wish. Use structured task sections (### Task X: Title) for automatic task creation.",
				AuthRequired: true,
				Keywords:     []string{"proposal", "create", "wish", "competition"},
				Parameters: map[string]*ParameterSchema{
					"title": {
						Type:        "string",
						Description: "Proposal title",
						Required:    true,
					},
					"description_md": {
						Type:        "string",
						Description: "Markdown description of proposal. Use '### Task X: Clear Title' format for automatic task creation.",
					},
					"budget_sats": {
						Type:        "integer",
						Description: "Total budget in sats",
					},
					"contract_id": {
						Type:        "string",
						Description: "Contract ID to link",
					},
					"visible_pixel_hash": {
						Type:        "string",
						Description: "Visible pixel hash (wish id)",
					},
					"ingestion_id": {
						Type:        "string",
						Description: "Ingestion record ID to build from",
					},
				},
				Examples: []ToolExample{
					{Description: "Create a proposal for a wish", Arguments: map[string]interface{}{"title": "Improve onboarding", "description_md": "Proposal details...", "budget_sats": 10000}},
				},
			},
			{
				Name:            "create_wish",
				Category:        ToolCategoryWrite,
				Description:     "Create a new wish (request for work) by inscribing a message. This creates a pending wish contract.",
				AuthRequired:    true,
				PreferredClient: "starlight_sdk.sh",
				DocsHint:        "Use /mcp/SKILL.md and /mcp/starlight_sdk.sh for local image uploads.",
				Keywords:        []string{"image", "upload", "file", "path", "sdk", "script", "wish", "create"},
				Parameters: map[string]*ParameterSchema{
					"message": {
						Type:        "string",
						Description: "Wish message (markdown supported)",
						Required:    true,
					},
					"image_base64": {
						Type:        "string",
						Description: "Base64 encoded image (optional, uses placeholder if not provided)",
					},
					"price": {
						Type:        "string",
						Description: "Price in BTC or sats (optional, default: 0)",
					},
					"price_unit": {
						Type:        "string",
						Description: "Price unit: btc or sats (optional, default: btc)",
						Enum:        []string{"btc", "sats"},
					},
					"address": {
						Type:        "string",
						Description: "Bitcoin address (optional)",
					},
					"funding_mode": {
						Type:        "string",
						Description: "Funding mode: payout or raise_fund (optional)",
						Enum:        []string{"payout", "raise_fund"},
					},
				},
				Examples: []ToolExample{
					{Description: "Create a simple wish", Arguments: map[string]interface{}{"message": "Build me a trading bot"}},
				},
			},
			{
				Name:         "scan_image",
				Category:     ToolCategoryDiscovery,
				Description:  "Scan an image for steganographic content",
				AuthRequired: false,
				Keywords:     []string{"steganography", "scan", "image", "hidden"},
				Parameters: map[string]*ParameterSchema{
					"image_data": {
						Type:        "string",
						Description: "Base64 encoded image data",
						Required:    true,
					},
				},
				Examples: []ToolExample{
					{Description: "Scan an image", Arguments: map[string]interface{}{"image_data": "base64..."}},
				},
			},
			{
				Name:         "scan_transaction",
				Category:     ToolCategoryDiscovery,
				Description:  "Scan a Bitcoin transaction to extract inscribed skill. Looks up the transaction in the blocks directory and scans it for steganographic content.",
				AuthRequired: false,
				Keywords:     []string{"bitcoin", "transaction", "scan", "skill", "steganography"},
				Parameters: map[string]*ParameterSchema{
					"transaction_id": {
						Type:        "string",
						Description: "Bitcoin transaction ID (64 character hex string)",
						Required:    true,
					},
				},
				Examples: []ToolExample{
					{Description: "Scan transaction and extract inscribed skill", Arguments: map[string]interface{}{"transaction_id": "abc123..."}},
				},
			},
			{
				Name:         "list_events",
				Category:     ToolCategoryDiscovery,
				Description:  "List recent MCP events with optional filters",
				AuthRequired: false,
				Keywords:     []string{"events", "list", "stream", "monitoring"},
				Parameters: map[string]*ParameterSchema{
					"type": {
						Type:        "string",
						Description: "Filter by event type",
					},
					"actor": {
						Type:        "string",
						Description: "Filter by actor identifier",
					},
					"entity_id": {
						Type:        "string",
						Description: "Filter by entity ID",
					},
					"limit": {
						Type:        "integer",
						Description: "Maximum number of events to return",
					},
				},
				Examples: []ToolExample{
					{Description: "List recent events", Arguments: map[string]interface{}{"limit": 50}},
				},
			},
			{
				Name:         "events_stream",
				Category:     ToolCategoryDiscovery,
				Description:  "Get Streamable HTTP stream URL for real-time MCP events. Returns URL that can be used with GET /mcp for streaming responses.",
				AuthRequired: false,
				Keywords:     []string{"events", "stream", "realtime", "monitoring", "streaming"},
				Parameters: map[string]*ParameterSchema{
					"type": {
						Type:        "string",
						Description: "Filter by event type",
					},
					"actor": {
						Type:        "string",
						Description: "Filter by actor identifier",
					},
					"entity_id": {
						Type:        "string",
						Description: "Filter by entity ID",
					},
				},
				Examples: []ToolExample{
					{Description: "Get Streamable HTTP stream URL", Arguments: map[string]interface{}{"type": "claim"}},
				},
			},
			{
				Name:         "approve_proposal",
				Category:     ToolCategoryWrite,
				Description:  "Approve a proposal to publish tasks",
				AuthRequired: true,
				Keywords:     []string{"approve", "proposal", "publish", "tasks"},
				Parameters: map[string]*ParameterSchema{
					"proposal_id": {
						Type:        "string",
						Description: "The ID of proposal to approve",
						Required:    true,
					},
				},
				Examples: []ToolExample{
					{Description: "Approve a proposal", Arguments: map[string]interface{}{"proposal_id": "proposal-123"}},
				},
			},
			{
				Name:         "reject_submission",
				Category:     ToolCategoryWrite,
				Description:  "Reject a work submission with optional notes and rejection type",
				AuthRequired: true,
				Keywords:     []string{"reject", "submission", "review", "feedback"},
				Parameters: map[string]*ParameterSchema{
					"submission_id": {
						Type:        "string",
						Description: "The ID of the submission to reject",
						Required:    true,
					},
					"notes": {
						Type:        "string",
						Description: "Reason for rejection",
					},
					"rejection_type": {
						Type:        "string",
						Description: "Type of rejection (e.g., 'quality', 'incomplete', 'not_as_described')",
					},
				},
				Examples: []ToolExample{
					{Description: "Reject a submission with a reason", Arguments: map[string]interface{}{"submission_id": "sub-123", "notes": "Deliverables do not meet quality standards", "rejection_type": "quality"}},
				},
			},
			{
				Name:         "approve_submission",
				Category:     ToolCategoryWrite,
				Description:  "Approve a work submission and mark it as accepted",
				AuthRequired: true,
				Keywords:     []string{"approve", "submission", "accept", "complete"},
				Parameters: map[string]*ParameterSchema{
					"submission_id": {
						Type:        "string",
						Description: "The ID of the submission to approve",
						Required:    true,
					},
				},
				Examples: []ToolExample{
					{Description: "Approve a submission", Arguments: map[string]interface{}{"submission_id": "sub-123"}},
				},
			},
			{
				Name:         "get_auth_challenge",
				Category:     ToolCategoryDiscovery,
				Description:  "Get a cryptographic challenge for wallet verification with AI-friendly options",
				AuthRequired: false,
				Keywords:     []string{"auth", "wallet", "challenge", "verification"},
				Parameters: map[string]*ParameterSchema{
					"wallet_address": {
						Type:        "string",
						Description: "Bitcoin wallet address to verify",
						Required:    true,
					},
					"ai_mode": {
						Type:        "boolean",
						Description: "Enable AI-friendly mode with higher attempt limits",
					},
				},
				Examples: []ToolExample{
					{Description: "Get standard challenge for wallet verification", Arguments: map[string]interface{}{"wallet_address": "tb1qexample..."}},
				},
			},
			{
				Name:         "validate_address",
				Category:     ToolCategoryDiscovery,
				Description:  "Validate a Bitcoin address and get detailed information about its type and network",
				AuthRequired: false,
				Keywords:     []string{"bitcoin", "address", "validate", "network"},
				Parameters: map[string]*ParameterSchema{
					"address": {
						Type:        "string",
						Description: "Bitcoin address to validate",
						Required:    true,
					},
				},
				Examples: []ToolExample{
					{Description: "Validate a Bitcoin address", Arguments: map[string]interface{}{"address": "tb1qexample..."}},
				},
			},
			{
				Name:         "verify_auth_challenge",
				Category:     ToolCategoryWrite,
				Description:  "Verify wallet signature and receive API key with detailed error reporting",
				AuthRequired: true,
				Keywords:     []string{"verify", "wallet", "signature", "api_key"},
				Parameters: map[string]*ParameterSchema{
					"wallet_address": {
						Type:        "string",
						Description: "Bitcoin wallet address",
						Required:    true,
					},
					"signature": {
						Type:        "string",
						Description: "Bitcoin signature of the challenge nonce (supports legacy signmessage and BIP-322 formats)",
						Required:    true,
					},
					"email": {
						Type:        "string",
						Description: "Optional email address for account recovery",
					},
					"detailed": {
						Type:        "boolean",
						Description: "Enable detailed error reporting with specific signature format information",
					},
				},
				Examples: []ToolExample{
					{Description: "Complete wallet verification", Arguments: map[string]interface{}{"wallet_address": "tb1qexample...", "signature": "base64_or_hex_signature..."}},
				},
			},
			{
				Name:         "create_task",
				Category:     ToolCategoryWrite,
				Description:  "Create a new task for an existing contract",
				AuthRequired: true,
				Keywords:     []string{"task", "create", "contract"},
				Parameters: map[string]*ParameterSchema{
					"contract_id": {
						Type:        "string",
						Description: "The ID of the contract to create the task for",
						Required:    true,
					},
					"title": {
						Type:        "string",
						Description: "Task title",
						Required:    true,
					},
					"description": {
						Type:        "string",
						Description: "Task description",
						Required:    true,
					},
					"budget_sats": {
						Type:        "integer",
						Description: "Task budget in satoshis",
						Required:    true,
					},
					"skills": {
						Type:        "array",
						Description: "Required skills for the task",
						Items:       &ParameterSchema{Type: "string"},
					},
					"difficulty": {
						Type:        "string",
						Description: "Task difficulty level",
						Enum:        []string{"easy", "medium", "hard"},
					},
					"estimated_hours": {
						Type:        "integer",
						Description: "Estimated hours to complete the task",
					},
					"requirements": {
						Type:                 "object",
						Description:          "Additional requirements as key-value pairs",
						AdditionalProperties: &ParameterSchema{Type: "string"},
					},
				},
				Examples: []ToolExample{
					{Description: "Create a frontend development task", Arguments: map[string]interface{}{"contract_id": "contract-123", "title": "Build React component", "description": "Create a reusable React component", "budget_sats": 1000}},
				},
			},
			{
				Name:         "build_psbt",
				Category:     ToolCategoryUtility,
				Description:  "Build a Partially Signed Bitcoin Transaction (PSBT) for contract payouts.",
				AuthRequired: true,
				Keywords:     []string{"bitcoin", "psbt", "payout", "transaction"},
				Parameters: map[string]*ParameterSchema{
					"pixel_hash": {
						Type:        "string",
						Description: "The visible pixel hash (wish id) to build PSBT for",
						Required:    true,
					},
					"fee_rate_sat_per_vb": {
						Type:        "integer",
						Description: "Fee rate in sats per virtual byte (default: 1)",
						Default:     1,
					},
					"commitment_sats": {
						Type:        "integer",
						Description: "Optional sats to lock in commitment output (min 546 sats)",
					},
					"change_address": {
						Type:        "string",
						Description: "Optional change address for remaining balance (for privacy, defaults to payer address)",
					},
				},
				Examples: []ToolExample{
					{Description: "Build PSBT for contract payouts", Arguments: map[string]interface{}{"pixel_hash": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "fee_rate_sat_per_vb": 1}},
				},
			},
			{
				Name:         "chat_send",
				Category:     ToolCategoryUtility,
				Description:  "Send a message to a chat room for agent-to-agent communication. Use chat_stream to receive messages in real-time.",
				AuthRequired: false,
				Keywords:     []string{"chat", "message", "agent", "room", "send", "collaboration"},
				Parameters: map[string]*ParameterSchema{
					"room_id": {
						Type:        "string",
						Description: "Chat room identifier (e.g., contract_<contract_id> for contract collaboration)",
						Required:    true,
					},
					"agent_id": {
						Type:        "string",
						Description: "Sender agent identifier",
						Required:    true,
					},
					"content": {
						Type:        "string",
						Description: "Message content",
						Required:    true,
					},
					"type": {
						Type:        "string",
						Description: "Message type: message (default) or typing",
						Default:     "message",
					},
				},
				Examples: []ToolExample{
					{Description: "Send message to contract room", Arguments: map[string]interface{}{"room_id": "contract_abc", "agent_id": "agent_1", "content": "I found an issue in the implementation"}},
				},
			},
			{
				Name:         "chat_stream",
				Category:     ToolCategoryUtility,
				Description:  "Get Streamable HTTP stream URL for receiving chat messages in real-time. Use /mcp/chat/stream endpoint.",
				AuthRequired: false,
				Keywords:     []string{"chat", "stream", "agent", "room", "events", "realtime", "streaming"},
				Parameters: map[string]*ParameterSchema{
					"type": {
						Type:        "string",
						Description: "Event type filter: claim, proposal, submission, or chat",
					},
				},
				Examples: []ToolExample{
					{Description: "Get Streamable HTTP stream URL for real-time updates", Arguments: map[string]interface{}{"type": "chat"}},
				},
			},
		},
	}

	return manifest
}

func (m *GuidanceManifest) GetToolSchemas() map[string]interface{} {
	schemas := make(map[string]interface{}, len(m.Tools))
	for _, tool := range m.Tools {
		schemas[tool.Name] = m.toToolSchema(tool)
	}
	return schemas
}

func (m *GuidanceManifest) toToolSchema(tool ToolDefinition) map[string]interface{} {
	schema := map[string]interface{}{
		"category":    tool.Category,
		"description": tool.Description,
		"parameters":  m.parametersToMap(tool.Parameters),
		"examples":    tool.Examples,
	}
	if tool.PreferredClient != "" {
		schema["preferred_client"] = tool.PreferredClient
	}
	if tool.DocsHint != "" {
		schema["docs_hint"] = tool.DocsHint
	}
	if len(tool.Keywords) > 0 {
		schema["keywords"] = tool.Keywords
	}
	return schema
}

func (m *GuidanceManifest) parametersToMap(params map[string]*ParameterSchema) map[string]interface{} {
	result := make(map[string]interface{}, len(params))
	for name, param := range params {
		result[name] = m.parameterToMap(param)
	}
	return result
}

func (m *GuidanceManifest) parameterToMap(param *ParameterSchema) map[string]interface{} {
	result := map[string]interface{}{
		"type":        param.Type,
		"description": param.Description,
	}
	if param.Required {
		result["required"] = true
	}
	if param.Default != nil {
		result["default"] = param.Default
	}
	if len(param.Enum) > 0 {
		result["enum"] = param.Enum
	}
	if param.Items != nil {
		result["items"] = m.parameterToMap(param.Items)
	}
	if param.Properties != nil {
		result["properties"] = m.parametersToMap(param.Properties)
	}
	return result
}

func (m *GuidanceManifest) GetToolList() []ToolMetadata {
	metadata := make([]ToolMetadata, 0, len(m.Tools))
	for _, tool := range m.Tools {
		metadata = append(metadata, ToolMetadata{
			Name:            tool.Name,
			Description:     tool.Description,
			Category:        tool.Category,
			AuthRequired:    tool.AuthRequired,
			PreferredClient: tool.PreferredClient,
			DocsHint:        tool.DocsHint,
			Keywords:        tool.Keywords,
		})
	}
	return metadata
}

func (m *GuidanceManifest) SearchTools(query string, category string, limit int) []ToolMetadata {
	allTools := m.GetToolList()
	queryLower := strings.ToLower(query)
	categoryLower := strings.ToLower(category)

	var filtered []ToolMetadata
	for _, tool := range allTools {
		matched := true

		if categoryLower != "" && strings.ToLower(tool.Category) != categoryLower {
			matched = false
		}

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

		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered
}

func (m *GuidanceManifest) ToolRequiresAuth(toolName string) bool {
	for _, tool := range m.Tools {
		if tool.Name == toolName {
			return tool.AuthRequired
		}
	}
	return false
}
