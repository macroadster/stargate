SHELL := /bin/bash
MCP_BASE ?= https://starlight.local/mcp/v1
AI_ID ?= codex-e2e
CONTRACT_ID ?=
PROPOSAL_ID ?=

FRONTEND_IMAGE := stargate-frontend:latest
BACKEND_IMAGE := stargate-backend:latest

.PHONY: all frontend backend mcp-e2e
all: frontend backend

frontend:
	@echo "Building frontend Docker image: $(FRONTEND_IMAGE)"
	@cd frontend && docker build -t $(FRONTEND_IMAGE) .

backend:
	@echo "Building backend Docker image: $(BACKEND_IMAGE)"
	@cd backend && docker build -t $(BACKEND_IMAGE) .

mcp-e2e:
	@echo "Running MCP E2E against $(MCP_BASE) (AI_ID=$(AI_ID))"
	@cd scripts && MCP_BASE=$(MCP_BASE) AI_ID=$(AI_ID) CONTRACT_ID=$(CONTRACT_ID) PROPOSAL_ID=$(PROPOSAL_ID) ./e2e_mcp_workflow.sh
