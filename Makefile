SHELL := /bin/bash
MCP_BASE ?= https://starlight.local/mcp/v1
AI_ID ?= codex-e2e
CONTRACT_ID ?=
PROPOSAL_ID ?=

SINGLE_IMAGE := stargate:latest

.PHONY: all docker single-binary mcp-e2e backend frontend backend-legacy frontend-legacy
all: single-binary

# Primary build: unified image for Helm / local k8s (ADR 0001).
docker:
	@echo "Building single Docker image: $(SINGLE_IMAGE) (linux/amd64 for cluster)"
	docker build --platform linux/amd64 -t $(SINGLE_IMAGE) .

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

# Retired split-image targets (stargate-3bk.8). Use `make docker` or `make single-binary`.
backend frontend backend-legacy frontend-legacy:
	@echo "ERROR: Split backend/frontend images are retired (ADR 0001 / stargate-3bk.8)." >&2
	@echo "Use:  make docker          # stargate:latest single image" >&2
	@echo "  or:  make single-binary   # local ./stargate binary with embedded UI" >&2
	@exit 1

mcp-e2e:
	@echo "Running MCP E2E against $(MCP_BASE) (AI_ID=$(AI_ID))"
	@cd scripts && MCP_BASE=$(MCP_BASE) AI_ID=$(AI_ID) CONTRACT_ID=$(CONTRACT_ID) PROPOSAL_ID=$(PROPOSAL_ID) ./e2e_mcp_workflow.sh
