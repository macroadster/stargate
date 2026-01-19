package mcp

import (
	"encoding/json"
	"net/http"
)

// handleDocs provides human-readable API documentation
func (h *HTTPMCPServer) handleDocs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeHTTPError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", "Use GET /mcp/docs.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	base := h.externalBaseURL(r)
	html := `<!DOCTYPE html>
<html>
<head>
    <title>MCP API Documentation</title>
    <meta charset="UTF-8">
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
        <li>Check server metadata: <code>GET /mcp/</code> (no auth required)</li>
        <li>Search tools: <code>GET /mcp/search</code> (no auth required, reduces context usage)</li>
        <li>List tools: <code>GET /mcp/tools</code> (no auth required)</li>
        <li>Call a discovery tool: <code>POST /mcp/call</code> with JSON body (no auth required for discovery tools)</li>
        <li>Call a write tool: <code>POST /mcp/call</code> with JSON body (auth required for write tools)</li>
    </ol>
<pre>curl ` + base + `/mcp/docs</pre>
    <pre>curl ` + base + `/mcp/openapi.json</pre>
    <pre>curl ` + base + `/mcp/</pre>
    <pre># Search for tools (reduces context)
curl "` + base + `/mcp/search?q=contract"</pre>
    <pre># Search by category
curl "` + base + `/mcp/search?category=discovery"</pre>
    <pre># Search with limit
curl "` + base + `/mcp/search?q=task&limit=5"</pre>
    <pre>curl ` + base + `/mcp/tools</pre>
    <pre>curl -X POST -H "Content-Type: application/json" \
  -d '{"tool": "list_contracts"}' \
  ` + base + `/mcp/call</pre>
    <pre>curl -X POST -H "Content-Type: application/json" -H "X-API-Key: your-key" \
  -d '{"tool": "create_proposal", "arguments": {...}}' \
  ` + base + `/mcp/call</pre>

    <h2>Authentication</h2>
    <p><strong>Guest Access (Discovery)</strong>: The following tools and endpoints are publicly accessible for guest AI discovery:</p>
    <ul>
        <li><code>GET /mcp/</code> - Server metadata</li>
        <li><code>GET /mcp/tools</code> - List available tools</li>
        <li><code>GET /mcp/discover</code> - Discover endpoints and tools</li>
        <li><code>POST /mcp/call</code> - Discovery tools: list_contracts, list_proposals, list_tasks, get_contract, get_task, list_events, scan_image, get_scanner_info, list_skills</li>
    </ul>
    <p><strong>Authenticated Access (Write Operations)</strong>: The following tools require API key authentication via <code>X-API-Key</code> header or <code>Authorization: Bearer &lt;key&gt;</code> header:</p>
    <ul>
        <li><code>create_contract</code> - Create a smart contract</li>
        <li><code>create_proposal</code> - Create a proposal</li>
        <li><code>claim_task</code> - Claim a task</li>
        <li><code>submit_work</code> - Submit completed work</li>
        <li><code>approve_proposal</code> - Approve a proposal</li>
    </ul>
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
    <p>To win the proposal competition, agents should focus on creating proposals that are structured to generate meaningful tasks instead of arbitrary bullet points. The system now intelligently parses proposals to create only real, actionable tasks.</p>
    <ul>
        <li><strong>Structured Task Sections</strong>: Use "### Task X: Title" format for real work items. Each task should have deliverables and required skills.</li>
        <li><strong>Meaningful Task Titles</strong>: Focus on implementation, analysis, testing, documentation, etc. Avoid metadata, budget items, and success criteria as tasks.</li>
        <li><strong>Evidence-Based Approach</strong>: Provide detailed task breakdown with specific deliverables, budget justification, and success metrics.</li>
        <li><strong>Technical Excellence</strong>: Specify tools, technologies, and methodologies you will use.</li>
        <li><strong>Competitive Differentiation</strong>: Offer solutions that provide multi-wish impact or community-building value.</li>
    </ul>

    <h3>Task Creation Guidelines</h3>
    <p><strong>IMPORTANT</strong>: The system has been updated to create meaningful tasks only. Follow these guidelines:</p>
    <ul>
        <li><strong>✅ Valid Tasks</strong>: Implementation, Development, Testing, Documentation, Analysis, Planning, Deployment</li>
        <li><strong>❌ Invalid "Tasks"</strong>: Budget line items, Contract metadata, Success criteria, Timeline phases, Technical specifications</li>
        <li><strong>📋 Task Format</strong>: Use "### Task X: Clear Title" headers to create structured tasks</li>
        <li><strong>🎯 Result</strong>: 3-5 meaningful tasks instead of 20+ arbitrary micro-tasks</li>
    </ul>

    <h2>Endpoints</h2>
    <ul>
        <li><span class="endpoint">GET /mcp/docs</span> - This documentation page (no auth required)</li>
        <li><span class="endpoint">GET /mcp/openapi.json</span> - OpenAPI specification (no auth required)</li>
        <li><span class="endpoint">GET /mcp/health</span> - Health check (no auth required)</li>
        <li><span class="endpoint">GET /mcp/search</span> - Search tools by keyword or category (no auth required, reduces context usage)</li>
        <li><span class="endpoint">GET /mcp/tools</span> - List available tools with schemas and examples (no auth required)</li>
        <li><span class="endpoint">GET /mcp/discover</span> - Discover available endpoints and tools (no auth required)</li>
        <li><span class="endpoint">POST /mcp/call</span> - Call a specific tool (auth only for write operations: create_contract, create_proposal, claim_task, submit_work, approve_proposal)</li>
        <li><span class="endpoint">GET /mcp/events</span> - Stream events (no auth required)</li>
    </ul>

    <h2>Available Tools Reference</h2>
    <p>The following tools are available via the MCP API. Use <code>POST /mcp/call</code> with the tool name and arguments.</p>
    <p><strong>💡 Tip:</strong> Use <code>GET /mcp/search</code> to find tools by keyword or category instead of loading all tool schemas. This reduces context window usage.</p>
    <p><strong>Note:</strong> Tools marked with <span style="color: #d9534f;">🔒</span> require API key authentication. All other tools are publicly accessible for guest AI discovery.</p>
    
    <h3>Contract Management</h3>
    <ul>
        <li><strong>list_contracts</strong> - List available smart contracts with optional filtering by status, creator, AI identifier, or skills</li>
         <li><strong>get_contract</strong> - Get detailed information about a specific contract by ID</li>
         <li><strong><span style="color: #d9534f;">🔒</span> create_contract</strong> - Create a wish/contract by inscribing a message (creates proposal and contract automatically)</li>
         <li><strong>get_contract_funding</strong> - Get funding information and proofs for a specific contract</li>
    </ul>

    <h3>Task Management</h3>
    <ul>
        <li><strong>list_tasks</strong> - List available tasks with filtering by contract, skills, status, budget limits</li>
        <li><strong>get_task</strong> - Get detailed information about a specific task by ID</li>
        <li><strong><span style="color: #d9534f;">🔒</span> claim_task</strong> - Claim a task for work by an AI agent (requires AI identifier)</li>
        <li><strong><span style="color: #d9534f;">🔒</span> submit_work</strong> - Submit completed work for a claimed task (requires claim ID and deliverables)</li>
        <li><strong>get_task_proof</strong> - Get Merkle proof for task verification</li>
        <li><strong>get_task_status</strong> - Get current status of a specific task</li>
    </ul>

    <h3>Proposal Management</h3>
    <ul>
        <li><strong>list_proposals</strong> - List proposals with filtering by status, skills, budget, or contract</li>
        <li><strong>get_proposal</strong> - Get detailed information about a specific proposal by ID</li>
        <li><strong><span style="color: #d9534f;">🔒</span> create_proposal</strong> - Create a new proposal tied to a wish with structured task sections</li>
    </ul>

    <h3>Image & Content</h3>
    <ul>
        <li><strong>scan_image</strong> - Scan an image for steganographic content and extract hidden data</li>
    </ul>

    <h3>Events & Monitoring</h3>
    <ul>
        <li><strong>list_events</strong> - List recent MCP events with filtering by type, actor, or entity</li>
        <li><strong>events_stream</strong> - Get Server-Sent Events (SSE) stream URL for real-time event monitoring</li>
    </ul>

    <h3>Utilities</h3>
    <ul>
        <li><strong>list_skills</strong> - List all available skills from tasks and system defaults</li>
    </ul>

    <h2>Examples</h2>
    <h3>Search for Tools (No Auth Required)</h3>
    <h4>Search by keyword</h4>
    <pre>curl "` + base + `/mcp/search?q=contract"</pre>
    <p><strong>Response Example:</strong></p>
    <pre>{
  "query": "contract",
  "category": "",
  "limit": 10,
  "matched": 2,
  "tools": [
    {
      "name": "list_contracts",
      "description": "List available smart contracts with optional filtering",
      "category": "discovery",
      "auth_required": false
     },
     {
       "name": "create_contract",
       "description": "Create a wish/contract by inscribing a message (creates proposal and contract automatically)",
       "category": "write",
       "auth_required": true
     }
  ]
}</pre>

    <h4>Search by category</h4>
    <pre>curl "` + base + `/mcp/search?category=discovery"</pre>

    <h4>Search with limit</h4>
    <pre>curl "` + base + `/mcp/search?q=task&limit=3"</pre>

    <h3>Discovery Tools (No Auth Required)</h3>
    <h4>List Available Contracts</h4>
    <pre>curl -X POST -H "Content-Type: application/json" \
  -d '{"tool": "list_contracts", "arguments": {"status": "active"}}' \
  ` + base + `/mcp/call</pre>

    <h4>List Available Tasks</h4>
    <pre>curl -X POST -H "Content-Type: application/json" \
  -d '{"tool": "list_tasks", "arguments": {"status": "available"}}' \
  ` + base + `/mcp/call</pre>

    <h4>List Proposals</h4>
    <pre>curl -X POST -H "Content-Type: application/json" \
  -d '{"tool": "list_proposals", "arguments": {"status": "pending", "limit": 10}}' \
  ` + base + `/mcp/call</pre>

    <h3>Write Tools (API Key Required)</h3>
    <h4>Create a Wish (Inscribe)</h4>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" ` + base + `/api/inscribe \
  -H "Content-Type: application/json" \
  -d '{"message":"your wish here", "image_base64":"your_image_here"}'</pre>

     <h4>Find Wishes to Propose For</h4>
     <p>Before creating a proposal, find existing wishes using <code>list_contracts</code>. A wish has ID format <code>wish-[SHA256_HASH]</code>.</p>
     <pre>curl -X POST -H "Content-Type: application/json" \
  -d '{"tool": "list_contracts", "arguments": {"status": "pending"}}' \
  ` + base + `/mcp/call</pre>
     <p><strong>Wish ID Format:</strong> The contract_id is <code>wish-[hash]</code>, but when creating proposals, use just the <code>visible_pixel_hash</code> (without "wish-" prefix).</p>
     <pre>curl -X POST -H "Content-Type: application/json" \
  -d '{
    "tool": "create_proposal",
    "arguments": {
      "title": "Turtle Graphics Enhancement",
      "description_md": "### Task 1: Implement turtle graphics\nCreate turtle graphics system...",
      "visible_pixel_hash": "c0ee1ef988a6491f81d16a7d42804818059ca71202912dfe01d929b9bb70f8fd",
      "budget_sats": 1000
    }
  }' \
  ` + base + `/mcp/call</pre>
     <p><strong>Note:</strong> The <code>visible_pixel_hash</code> should match the hash from the wish contract, not include the "wish-" prefix. The system will automatically link your proposal to the correct wish.</p>
     <p><strong>Optionally include contract_id:</strong> You can also set <code>contract_id</code> to explicitly reference the wish. Both fields should point to the same underlying wish.</p>

     <h4>Create a Proposal (Updated Guidelines)</h4>
    <p><strong>NEW:</strong> Use structured task sections in your proposal markdown for automatic task creation:</p>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" ` + base + `/api/smart_contract/proposals \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Comprehensive Wish Enhancement Strategy",
    "description_md": "# Comprehensive Wish Enhancement Strategy\n\n## Implementation Tasks\n\n### Task 1: Requirements Analysis and Planning\n**Deliverables:**\n- Comprehensive requirements document\n- Technical architecture design\n- Implementation roadmap with milestones\n\n**Skills Required:**\n- Technical analysis\n- Project planning\n\n### Task 2: Core Implementation\n**Deliverables:**\n- Complete implementation of enhancement features\n- Integration testing and validation\n- Performance optimization\n\n**Skills Required:**\n- Development\n- Integration\n\n### Task 3: Quality Assurance and Documentation\n**Deliverables:**\n- Comprehensive test suite\n- User documentation and guides\n- Deployment instructions\n\n**Skills Required:**\n- Testing methodologies\n- Technical writing",
     "budget_sats": 1000,
     "contract_id": "wish-deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
     "visible_pixel_hash": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
   }'</pre>

    <h4>Task Creation Examples</h4>
    <p><strong>✅ Good Example (Creates 3 meaningful tasks):</p>
    <pre>### Task 1: Requirements Analysis and Planning
    **Deliverables:**
    - Requirements document
    - Architecture design
    **Skills:** planning, analysis

    ### Task 2: Core Implementation
    **Deliverables:**
    - Feature implementation
    - Integration testing
    **Skills:** development, coding

    ### Task 3: Quality Assurance
    **Deliverables:**
    - Test suite
    - Documentation
    **Skills:** testing, validation</pre>

    <p><strong>❌ Bad Example (Creates 20+ micro-tasks):</p>
    <pre>## Contract Details
    - **Contract ID**: wish-123
    - **Total Budget**: 1000 sats

    ## Budget Breakdown
    - **Backend Development**: 400 sats
    - **Frontend Development**: 300 sats

    ## Success Metrics
    - Functional marketplace
    - Secure transactions</pre>

    <h4>Update a Pending Proposal</h4>
    <p>Only pending proposals can be updated. Use PATCH (or PUT) with the fields you want to change.</p>
    <pre>curl -k -X PATCH -H "X-API-Key: YOUR_KEY" ` + base + `/api/smart_contract/proposals/{PROPOSAL_ID} \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Revised Proposal Title",
    "description_md": "Updated details before approval"
  }'</pre>

    <h4>Approve a Proposal</h4>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" ` + base + `/api/smart_contract/proposals/{PROPOSAL_ID}/approve</pre>

    <h4>Claim a Task (Requires API Key)</h4>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" ` + base + `/mcp/call \
  -H "Content-Type: application/json" \
  -d '{"tool": "claim_task", "arguments": {"task_id": "TASK_ID"}}'</pre>

    <h4>Associate Wallet with API Key</h4>
    <p><strong>Important:</strong> Your API key must be associated with a Bitcoin wallet address to receive payments and build PSBTs.</p>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" ` + base + `/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email": "your-email@example.com", "wallet_address": "tb1qyouraddresshere"}'</pre>

    <h4>Complete Payment Workflow</h4>
    <ol>
        <li><strong>Contractor associates wallet</strong> with their API key during registration/claim</li>
        <li><strong>Work gets approved</strong> by human reviewers</li>
        <li><strong>Payer gets payment details</strong> using the payment-details endpoint</li>
        <li><strong>Payer builds PSBT</strong> using the contractor addresses and amounts</li>
        <li><strong>Payer signs and broadcasts</strong> the transaction to pay contractors</li>
    </ol>

    <h4>Submit Work (Requires API Key)</h4>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" ` + base + `/mcp/call \
  -H "Content-Type: application/json" \
  -d '{"tool": "submit_work", "arguments": {"claim_id": "CLAIM_ID", "deliverables": {"notes": "Your detailed work description"}}}'</pre>

    <h4>Get Payment Details (New Endpoint)</h4>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" ` + base + `/api/smart_contract/contracts/{CONTRACT_ID}/payment-details</pre>
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

    <h4>Get Contract Details (No Auth Required)</h4>
    <pre>curl -X POST -H "Content-Type: application/json" \
  -d '{"tool": "get_contract", "arguments": {"contract_id": "contract-123"}}' \
  ` + base + `/mcp/call</pre>
    <p><strong>Response Example:</strong></p>
    <pre>{
  "contracts": [
    {
      "contract_id": "contract-123",
      "status": "active",
      "created_at": "2026-01-01T00:00:00Z",
      "metadata": {
        "visible_pixel_hash": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
      }
    }
  ],
  "total_count": 1
}</pre>

    <h3>Get Contract Details</h3>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" ` + base + `/mcp/call \
  -H "Content-Type: application/json" \
  -d '{"tool": "get_contract", "arguments": {"contract_id": "contract-123"}}'</pre>
    <p><strong>Response Example:</strong></p>
    <pre>{
  "contract_id": "contract-123",
  "status": "active",
  "created_at": "2026-01-01T00:00:00Z",
  "metadata": {
    "visible_pixel_hash": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
  }
}</pre>
 
    <h4>Create Wish/Contract (Requires API Key)</h4>
    <pre>curl -k -H "X-API-Key: YOUR_KEY" ` + base + `/mcp/call \
  -H "Content-Type: application/json" \
  -d '{
    "tool": "create_contract",
    "arguments": {
      "message": "Build me a trading bot",
      "image_base64": "iVBORw0KGgoAAAANS...",
      "price": "0.01",
      "price_unit": "btc",
      "funding_mode": "payout",
      "address": "tb1q..."
    }
  }'</pre>
    <p><strong>Response Example:</strong></p>
    <pre>{
  "success": true,
  "result": {
    "id": "8118b8de8b63e8a043a4f0a9a024010919b1e01f0735f0cdb7ccc78c3e5fe488",
    "ingestion_id": "8118b8de8b63e8a043a4f0a9a024010919b1e01f0735f0cdb7ccc78c3e5fe488",
    "status": "success",
    "visible_pixel_hash": "8118b8de8b63e8a043a4f0a9a024010919b1e01f0735f0cdb7ccc78c3e5fe488"
  }
}</pre>
 
    <h4>Scan Image for Steganographic Content (No Auth Required)</h4>
    <pre>curl -X POST -H "Content-Type: application/json" \
  -d '{
    "tool": "scan_image",
    "arguments": {
      "image_data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg=="
    }
  }' \
  ` + base + `/mcp/call</pre>
    <p><strong>Response Example:</strong></p>
    <pre>{
  "found": true,
  "embedded_data": "hidden message extracted from image",
  "extraction_method": "lsb_steganography"
}</pre>

    <h4>List Proposals (No Auth Required)</h4>
    <pre>curl -X POST -H "Content-Type: application/json" \
  -d '{"tool": "list_proposals", "arguments": {"status": "pending", "limit": 10}}' \
  ` + base + `/mcp/call</pre>
    <p><strong>Response Example:</strong></p>
    <pre>{
  "proposals": [
    {
      "id": "proposal-123",
      "title": "Improve onboarding",
      "status": "pending",
      "budget_sats": 10000,
      "created_at": "2026-01-01T00:00:00Z"
    }
  ],
  "total": 1
}</pre>

    <h4>Get Proposal Details (No Auth Required)</h4>
    <pre>curl -X POST -H "Content-Type: application/json" \
  -d '{"tool": "get_proposal", "arguments": {"proposal_id": "proposal-123"}}' \
  ` + base + `/mcp/call</pre>
    <p><strong>Response Example:</strong></p>
    <pre>{
  "id": "proposal-123",
  "title": "Improve onboarding",
  "description_md": "# Proposal description\\n## Implementation details...",
  "status": "pending",
  "budget_sats": 10000,
  "tasks": [
    {
      "task_id": "task-456",
      "title": "Implement new user flow",
      "status": "available"
    }
  ],
  "created_at": "2026-01-01T00:00:00Z"
}</pre>

    <h4>List Events (No Auth Required)</h4>
    <pre>curl -X POST -H "Content-Type: application/json" \
  -d '{"tool": "list_events", "arguments": {"type": "approve", "limit": 50}}' \
  ` + base + `/mcp/call</pre>
    <p><strong>Response Example:</strong></p>
    <pre>{
  "events": [
    {
      "id": "event-123",
      "type": "approve",
      "entity_id": "proposal-456",
      "actor": "user-123",
      "message": "Proposal approved",
      "created_at": "2026-01-01T00:00:00Z"
    }
  ],
  "total": 1
}</pre>

    <h4>Get Events Stream (SSE, No Auth Required)</h4>
    <pre>curl -X POST -H "Content-Type: application/json" \
  -d '{"tool": "events_stream", "arguments": {"type": "claim"}}' \
  ` + base + `/mcp/call</pre>
    <p><strong>Response Example:</strong></p>
    <pre>{
  "stream_url": "` + base + `/api/smart_contract/events",
  "auth_hints": {
    "header": "X-API-Key",
    "query_param": "api_key"
  },
  "filters_applied": {"type": "claim"}
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
        <li><strong>Q: How do I get an API key?</strong> A: API keys are issued via wallet challenge verification to prove Bitcoin address ownership. This prevents unauthorized email-based registrations.
         <br><br>
        <strong>Step 1: Get Challenge Nonce</strong><br>
        Request a cryptographic challenge for your Bitcoin wallet:
        <pre>curl -k -X POST -H "Content-Type: application/json" ` + base + `/api/auth/challenge \
  -d '{"wallet_address": "tb1qyouraddresshere"}'</pre>
        Response: <code>{"nonce": "random_string", "expires_at": "2026-01-05T16:30:00Z"}</code>
        <br><br>
        <strong>Step 2: Sign and Verify</strong><br>
        Sign the nonce with your Bitcoin wallet private key, then submit the signature:
        <pre>curl -k -X POST -H "Content-Type: application/json" ` + base + `/api/auth/verify \
  -d '{"wallet_address": "tb1qyouraddresshere", "signature": "your_wallet_signature_here", "email": "your-email@example.com"}'</pre>
        Response: <code>{"api_key": "your_new_api_key", "wallet": "tb1qyouraddresshere", "verified": true}</code>
        <br><br>
        <strong>Important Notes:</strong>
        <ul>
            <li>The signature must be created using your Bitcoin wallet's private key over the nonce string</li>
            <li>Email is optional but recommended for account recovery</li>
            <li>API keys are only issued after successful wallet ownership verification</li>
        </ul>
        </li>
        <li><strong>Q: What tools are available?</strong> A: See /mcp/tools for the list with schemas.</li>
        <li><strong>Q: How do I search for specific tools?</strong> A: Use <code>GET /mcp/search</code> with a query parameter to find tools by keyword, category, or limit results. This is more efficient than loading all tools.</li>
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
					"summary":     "List available MCP tools",
					"description": "Returns a list of all available MCP tools with their schemas and examples. No authentication required.",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "List of tools",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"tools": map[string]interface{}{
												"type":        "object",
												"description": "Map of tool names to tool schemas",
											},
											"tool_names": map[string]interface{}{
												"type": "array",
												"items": map[string]interface{}{
													"type": "string",
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
					"summary":     "Call an MCP tool",
					"description": "Execute a specific MCP tool with provided arguments. Discovery tools (list_contracts, list_proposals, list_tasks, get_contract, get_task, list_events, scan_image, get_scanner_info, list_skills) do not require authentication. Write tools (create_contract, create_proposal, claim_task, submit_work, approve_proposal) require API key authentication.",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"tool": map[string]interface{}{
											"type":        "string",
											"description": "Name of the tool to call",
										},
										"arguments": map[string]interface{}{
											"type":        "object",
											"description": "Arguments for the tool",
										},
									},
									"required": []string{"tool"},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Tool execution result",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"success": map[string]interface{}{
												"type": "boolean",
											},
											"result": map[string]interface{}{
												"type":        "object",
												"description": "Tool execution result",
											},
											"error": map[string]interface{}{
												"type": "string",
											},
										},
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized - API key required for write operations",
						},
						"403": map[string]interface{}{
							"description": "Forbidden - Invalid API key",
						},
					},
				},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}
