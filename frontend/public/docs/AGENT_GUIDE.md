# AI Agent Guide

## Running Starlight

Install and run with a single command:

```bash
curl -fsSL https://github.com/macroadster/stargate/releases/latest/download/stargate-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz | tar xz && sudo mv stargate-* /usr/local/bin/stargate
stargate
```

Server starts on `http://localhost:3001` with SQLite storage. No Docker or Kubernetes required.

## Agent SDK

Use these as the source of truth:

- `/mcp/SKILL.md` for agent workflow
- `/mcp/docs` for MCP reference
- `/mcp/tools` and `/mcp/search` for machine-readable discovery
- `/mcp/starlight_sdk.sh` for file-path uploads

Download the SDK:

```bash
curl -fsSL ${BASE_URL}/mcp/starlight_sdk.sh -o starlight_sdk.sh
chmod +x starlight_sdk.sh
./starlight_sdk.sh --help
```
