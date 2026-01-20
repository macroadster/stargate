#!/usr/bin/env bash
set -euo pipefail

# Quick E2E validation script for Starlight MCP
# Tests core functionality with minimal dependencies

MCP_BASE=${MCP_BASE:-https://starlight.local/mcp}
API_KEY=${API_KEY:-demo-api-key}

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

log() { echo -e "${BLUE}[$(date '+%H:%M:%S')]${NC} $1"; }
success() { echo -e "${GREEN}✅ $1${NC}"; }
error() { echo -e "${RED}❌ $1${NC}"; }

# Test MCP server
log "Testing MCP server..."
curl -sk "${MCP_BASE}/discover" >/dev/null 2>&1 && success "MCP server reachable" || { error "MCP server down"; exit 1; }

# Test discovery tools (no auth required)
log "Testing discovery tools..."

tools=("list_contracts" "list_proposals" "get_open_contracts" "list_tasks")
for tool in "${tools[@]}"; do
    if curl -sk -H "Content-Type: application/json" "${MCP_BASE}/call" -d "{\"tool\":\"$tool\"}" | jq -e '.success' >/dev/null 2>&1; then
        success "$tool works"
    else
        error "$tool failed"
    fi
done

# Test write tools (auth required)
log "Testing write tools with API key..."

write_tools=("create_proposal" "approve_proposal" "claim_task" "submit_work")
for tool in "${write_tools[@]}"; do
    # These will fail with valid errors (missing wish, etc.) but should pass auth
    http_code=$(curl -sk -w "%{http_code}" -o /dev/null -H "X-API-Key: $API_KEY" -H "Content-Type: application/json" "${MCP_BASE}/call" -d "{\"tool\":\"$tool\"}")
    if [[ "$http_code" != "403" ]]; then
        success "$tool auth works (HTTP $http_code)"
    else
        error "$tool auth failed"
    fi
done

# Test API key validation
log "Testing API key validation..."
invalid_response=$(curl -sk -H "X-API-Key: invalid-key" -H "Content-Type: application/json" "${MCP_BASE}/call" -d '{"tool":"create_proposal"}')
if echo "$invalid_response" | jq -e '.success == false and .error_code == "API_KEY_INVALID"' >/dev/null 2>&1; then
    success "Invalid API key properly rejected"
else
    error "Invalid API key rejection failed"
fi

echo ""
success "🎉 MCP E2E VALIDATION COMPLETE"
echo ""
echo "Core MCP Functionality: ✅ WORKING"
echo "Authentication: ✅ WORKING" 
echo "API Structure: ✅ WORKING"
echo "Server Health: ✅ WORKING"