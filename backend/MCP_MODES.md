# Stargate Backend - Unified HTTP Server

The Stargate backend runs as a unified HTTP server with all endpoints available on a single port. Both REST API and MCP tools are accessible via different URL prefixes.

## Server Architecture

### Single HTTP Server
All functionality is available through one HTTP server on port 3001 (default):
- REST API endpoints at `/api/*`
- Smart contract API at `/api/smart_contract/*` 
- Bitcoin steganography API at `/bitcoin/v1/*`
- MCP HTTP tools at `/mcp/*`
- Frontend serving at `/`
- Background services for MCP and data synchronization

### URL Prefix Separation
Different functionality is separated by URL prefixes:
- `/api/smart_contract/*` - Smart contract REST API
- `/mcp/tools` - List available MCP tools
- `/mcp/call` - Execute MCP tools via HTTP POST

## Usage

### Start Server
```bash
# Start unified server (default port 3001)
go run .

# Custom port
STARGATE_HTTP_PORT=8080 go run .
```

### Access Points

#### REST API
```bash
# Smart contract API
curl http://localhost:3001/api/smart_contract/contracts

# Other APIs
curl http://localhost:3001/api/blocks
curl http://localhost:3001/bitcoin/v1/info
```

#### MCP Tools via HTTP
```bash
# List available tools
curl http://localhost:3001/mcp/tools

# Call a tool
curl -X POST http://localhost:3001/mcp/call \
  -H "Content-Type: application/json" \
  -d '{"tool": "list_contracts", "arguments": {"status": "active"}}'
```

### Access Points

#### Both Modes Running

**Option 1: Separate Processes (Recommended)**
```bash
# Terminal 1: HTTP server
STARGATE_MODE=http-only go run .

# Terminal 2: MCP server  
STARGATE_MODE=mcp-only go run .
```
- HTTP server: http://localhost:3001
- MCP server: stdio transport (separate process)
- API docs: http://localhost:3001/api/docs
- Bitcoin API: http://localhost:3001/bitcoin/v1

**Option 2: Single Process**
```bash
# Both modes in one process (HTTP on custom port)
STARGATE_HTTP_PORT=3001 go run .
```
- HTTP server: http://localhost:3001 (background)
- MCP server: stdio transport (foreground)
- API docs: http://localhost:3001/api/docs
- Bitcoin API: http://localhost:3001/bitcoin/v1

#### HTTP Only Mode
```bash
STARGATE_MODE=http-only go run .
```
- HTTP server: http://localhost:3001 (foreground)
- API docs: http://localhost:3001/api/docs  
- Bitcoin API: http://localhost:3001/bitcoin/v1

#### MCP Only Mode
```bash
STARGATE_MODE=mcp-only go run .
```
- MCP server: stdio transport (foreground)
- Background services: Managed by MCP process

### MCP Tools Available

The MCP server exposes 18 tools:

**Contracts:**
- `list_contracts` - List available contracts
- `get_contract` - Get specific contract details  
- `get_contract_funding` - Get contract funding information

**Tasks:**
- `list_tasks` - List available tasks with filtering
- `get_task` - Get specific task details
- `claim_task` - Claim a task for work
- `submit_work` - Submit completed work
- `get_task_proof` - Get task merkle proof
- `get_task_status` - Get task status

**Skills:**
- `list_skills` - List all available skills

**Proposals:**
- `list_proposals` - List proposals
- `get_proposal` - Get specific proposal
- `create_proposal` - Create new proposal
- `approve_proposal` - Approve proposal and publish tasks
- `publish_proposal` - Publish proposal without approval

Note: Contract creation should use `POST /api/inscribe`. Proposals are for review/approval; they are not the primary contract creation path.

**Submissions:**
- `list_submissions` - List submissions
- `get_submission` - Get specific submission
- `review_submission` - Review submission (approve/reject)
- `rework_submission` - Rework submission

**Events:**
- `list_events` - List MCP events

### MCP Client Configuration

#### HTTP MCP Access (Recommended)

For any HTTP client (web apps, scripts, other services):

```bash
# Direct HTTP calls
curl -X POST http://localhost:3001/mcp/call \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{"tool": "list_tasks", "arguments": {"status": "available"}}'
```

#### For Claude Desktop (Stdio Transport)

If you need stdio transport for Claude Desktop, you would need to modify the code to disable HTTP MCP, but the recommended approach is to use the HTTP endpoints directly from Claude via web requests or build a proxy.

```json
{
  "mcpServers": {
    "stargate-http": {
      "command": "curl",
      "args": ["-X", "POST", "http://localhost:3001/mcp/call", "-H", "Content-Type: application/json", "-d", "{\"tool\": \"{{tool}}\", \"arguments\": {{arguments}}}"]
    }
  }
}
```

## Environment Variables

Both modes respect these environment variables:

### Database Configuration
- `MCP_STORE_DRIVER` - "memory" (default) or "postgres"
- `MCP_PG_DSN` - PostgreSQL connection string
- `MCP_SEED_FIXTURES` - "true" (default) or "false"

### MCP Configuration  
- `MCP_API_KEY` - API key for MCP authentication
- `MCP_DEFAULT_CLAIM_TTL_HOURS` - Claim TTL (default: 72)
- `MCP_ENABLE_INGEST_SYNC` - Enable ingestion sync (default: true)
- `MCP_INGEST_SYNC_INTERVAL_SEC` - Sync interval (default: 30)
- `MCP_ENABLE_FUNDING_SYNC` - Enable funding sync (default: true) 
- `MCP_FUNDING_SYNC_INTERVAL_SEC` - Funding interval (default: 60)
- `MCP_FUNDING_PROVIDER` - "mock" (default) or "blockstream"
- `MCP_FUNDING_API_BASE` - Funding API base URL

### Other Configuration
- `BLOCKS_DIR` - Blocks directory path (default: "blocks")
- `UPLOADS_DIR` - Uploads directory path (default: "/data/uploads")
- `STARGATE_HTTP_PORT` - HTTP server port (default: "3001")
- `MCP_API_KEY` - API key for MCP tool authentication (optional)
- `IPFS_MIRROR_ENABLED` - Enable IPFS mirroring for uploads (default: false)
- `IPFS_MIRROR_UPLOAD_ENABLED` - Publish local uploads to IPFS (default: true)
- `IPFS_MIRROR_DOWNLOAD_ENABLED` - Fetch uploads announced by peers (default: true)
- `IPFS_API_URL` - IPFS HTTP API base URL (default: "http://127.0.0.1:5001")
- `IPFS_MIRROR_TOPIC` - PubSub topic for sync announcements (default: "stargate-uploads")
- `IPFS_MIRROR_POLL_INTERVAL_SEC` - Scan interval for local uploads (default: 10)
- `IPFS_MIRROR_PUBLISH_INTERVAL_SEC` - Publish interval for manifests (default: 30)
- `IPFS_MIRROR_MAX_FILES` - Max files to include in manifests (default: 2000)
- `IPFS_HTTP_TIMEOUT_SEC` - IPFS HTTP request timeout (default: 30)

## Development

### Build
```bash
go build .
```

### Test Both Modes
```bash
# Test HTTP mode
go run .

# Test MCP mode  
STARGATE_MODE=mcp go run .
```

The backend automatically detects the mode via `STARGATE_MODE` environment variable and initializes the appropriate server type while maintaining all background services and data synchronization.
