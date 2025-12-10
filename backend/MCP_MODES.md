# Stargate Backend - Multi-Mode Operation

The Stargate backend supports multiple operational modes that can run simultaneously or independently:

## Mode Options

### HTTP Server Mode (Default)
Runs the full HTTP API server with all endpoints including:
- REST API endpoints
- Bitcoin steganography API  
- Smart contract endpoints
- Block data API
- Frontend serving
- MCP background services (if PostgreSQL enabled and MCP not running separately)

### MCP Server Mode
Runs the MCP (Machine Control Protocol) server using stdio transport for integration with MCP clients like Claude Desktop, Cursor, etc.

### Both Modes (Default)
When `STARGATE_MODE` is not set, both HTTP and MCP servers run simultaneously:
- HTTP server runs in background on :3001
- MCP server runs in foreground via stdio
- Background services coordinated to avoid conflicts

## Usage

### Run Both Modes (Default)
```bash
# Both HTTP and MCP (default)
go run .

# Explicitly both modes
STARGATE_MODE=both go run .
```

### HTTP Only Mode
```bash
# Only HTTP server
STARGATE_MODE=http-only go run .
```

### MCP Only Mode
```bash
# Only MCP server
STARGATE_MODE=mcp-only go run .
```

### Legacy Mode Names
For backward compatibility:
- `STARGATE_MODE=mcp` → MCP only mode
- `STARGATE_MODE=http` → HTTP only mode  
- `STARGATE_MODE=""` (empty) → Both modes (default)

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

**Submissions:**
- `list_submissions` - List submissions
- `get_submission` - Get specific submission
- `review_submission` - Review submission (approve/reject)
- `rework_submission` - Rework submission

**Events:**
- `list_events` - List MCP events

### MCP Client Configuration

For Claude Desktop, add to your MCP configuration:

```json
{
  "mcpServers": {
    "stargate": {
      "command": "go",
      "args": ["run", "."],
      "env": {
        "STARGATE_MODE": "mcp"
      }
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