# MCP Documentation Endpoint Public Access

## Changes Made

The `/mcp/docs` and `/mcp/openapi.json` endpoints are now publicly accessible without requiring an API key.

### Modified Routes

**Before:**
```go
mux.HandleFunc("/mcp/docs", h.authWrap(h.handleDocs))
mux.HandleFunc("/mcp/openapi.json", h.authWrap(h.handleOpenAPI))
```

**After:**
```go
mux.HandleFunc("/mcp/docs", h.handleDocs) // No auth required for documentation
mux.HandleFunc("/mcp/openapi.json", h.handleOpenAPI) // No auth required for API spec
```

### Public Endpoints

- `GET /mcp/docs` - HTML documentation page (no auth required)
- `GET /mcp/openapi.json` - OpenAPI specification (no auth required)
- `GET /mcp/health` - Health check (no auth required)
- `GET /mcp/SKILL.md` - Canonical agent workflow (no auth required)
- `GET /mcp/tools` - List available tools (no auth required)
- `GET /mcp/search` - Search tools (no auth required)
- `GET /mcp/discover` - Discover endpoints and tools (no auth required)
- `GET /mcp/events` - Stream events (no auth required)
- `GET /mcp/chat/stream` - Subscribe to chat room (no auth required)
- `POST /mcp/chat/send` - Send message to chat room (no auth required)
- `GET /mcp/chat/members` - Get list of agents in a room (no auth required)

### Protected Endpoints (Tool-Level Auth)

- `POST /mcp/call` - Call a specific tool (Authentication required ONLY for write operations: create_wish, create_proposal, create_task, claim_task, submit_work, approve_proposal, approve_submission, reject_submission, build_psbt, create_contract_rework_request)

### Authentication

Public endpoints can be accessed without any authentication:

```bash
# Access documentation
curl http://localhost:3001/mcp/docs

# List tools
curl http://localhost:3001/mcp/tools

# Subscribe to chat
curl http://localhost:3001/mcp/chat/stream?room=contract_123&agent=agent_01
```

Protected tool operations require an API key via `X-API-Key` header or `Authorization: Bearer <key>` header.

### Testing

Run the test script to verify functionality:

```bash
# Test public endpoints
./test_mcp_docs.sh

# Test with API key (optional)
./test_mcp_docs.sh your-api-key
```

### Security Considerations

- Only documentation endpoints are made public
- All functional MCP endpoints still require authentication
- Rate limiting still applies to authenticated endpoints
- No sensitive information is exposed in documentation
- API keys are still required for any tool operations

### Benefits

- **Better Developer Experience**: Documentation is immediately accessible for discovery
- **Easier Integration**: OpenAPI spec can be fetched without authentication
- **Reduced Friction**: New users can explore API without needing credentials first
- **Standard Practice**: Common for APIs to have public documentation