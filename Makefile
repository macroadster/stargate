SHELL := /bin/bash
MCP_BASE ?= https://starlight.local/mcp/v1
AI_ID ?= codex-e2e
CONTRACT_ID ?=
PROPOSAL_ID ?=

FRONTEND_IMAGE := stargate-frontend:latest
BACKEND_IMAGE := stargate-backend:latest
SINGLE_IMAGE := stargate:latest

.PHONY: all frontend frontend-legacy backend backend-legacy mcp-e2e single-binary docker
all: single-binary

docker:
	@echo "Building single Docker image: $(SINGLE_IMAGE) (linux/amd64 for cluster)"
	docker build --platform linux/amd64 -t $(SINGLE_IMAGE) .

# Legacy convenience aliases. The documented primary path (single-binary + starlight-helm) is `make docker`.
backend: backend-legacy
frontend: frontend-legacy

single-binary:
	@echo "Building single binary project..."
	@echo "1. Building frontend..."
	@cd frontend && npm install && npm run build
	@echo "2. Preparing assets..."
	@mkdir -p backend/assets/frontend
	@rm -rf backend/assets/frontend/*
	@cp -rv frontend/build/* backend/assets/frontend/
	@echo "3. Building backend binary..."
	@cd backend && CGO_ENABLED=0 go build -tags gms_pure_go -o ../stargate .
	@echo "Done! Binary produced as ./stargate"

frontend-legacy:
	@echo "Building frontend Docker image (LEGACY): $(FRONTEND_IMAGE) (linux/amd64)"
	@cd frontend && docker build --platform linux/amd64 -t $(FRONTEND_IMAGE) .

backend-legacy:
	@echo "Building backend Docker image (LEGACY): $(BACKEND_IMAGE) (linux/amd64)"
	@cd backend && docker build --platform linux/amd64 -t $(BACKEND_IMAGE) .

mcp-e2e:
	@echo "Running MCP E2E against $(MCP_BASE) (AI_ID=$(AI_ID))"
	@cd scripts && MCP_BASE=$(MCP_BASE) AI_ID=$(AI_ID) CONTRACT_ID=$(CONTRACT_ID) PROPOSAL_ID=$(PROPOSAL_ID) ./e2e_mcp_workflow.sh
