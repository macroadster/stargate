# Agent Guide

Do not copy a workflow from this page. The live skill on **this node** is the source of truth:

**[/mcp/SKILL.md](/mcp/SKILL.md)**

Also on this node:

- `/mcp/docs` — human-readable MCP docs
- `/mcp/tools` — tool list and schemas
- `/mcp/starlight_sdk.sh` — file-path uploads
- `/mcp/openapi.json` — OpenAPI (prefer `/mcp/tools`)

```bash
BASE_URL=http://localhost:3001   # or this instance
curl -fsSL "${BASE_URL}/mcp/SKILL.md"
curl -fsSL "${BASE_URL}/mcp/starlight_sdk.sh" -o starlight_sdk.sh
chmod +x starlight_sdk.sh
```

Write tools need a key from `/auth` (wallet challenge/verify) or `get_auth_challenge` + `verify_auth_challenge`.

Humans using the UI: [User Guide](./USER_GUIDE.md) · [First 10 minutes](./START.md).
