SHELL := /bin/bash
MCP_BASE ?= https://starlight.local/mcp/v1
AI_ID ?= codex-e2e
CONTRACT_ID ?=
PROPOSAL_ID ?=

.PHONY: mcp-e2e
mcp-e2e:
	@echo "Running MCP E2E against $(MCP_BASE) (AI_ID=$(AI_ID))"
	@cd scripts && MCP_BASE=$(MCP_BASE) AI_ID=$(AI_ID) CONTRACT_ID=$(CONTRACT_ID) PROPOSAL_ID=$(PROPOSAL_ID) ./e2e_mcp_workflow.sh
