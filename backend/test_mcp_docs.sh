#!/bin/bash

# Test script for MCP documentation endpoints

echo "🧪 Testing MCP Documentation Endpoints"
echo "======================================"

BASE_URL="http://localhost:3001"

# Function to test endpoint
test_endpoint() {
    local endpoint=$1
    local description=$2
    local expected_status=$3
    
    echo -n "Testing $endpoint ($description)... "
    
    status=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL$endpoint")
    
    if [ "$status" = "$expected_status" ]; then
        echo "✅ $status"
    else
        echo "❌ $status (expected $expected_status)"
        return 1
    fi
}

# Test public endpoints (should return 200)
echo -e "\n📖 Public Documentation Endpoints:"
test_endpoint "/mcp/docs" "Documentation page" "200"
test_endpoint "/mcp/openapi.json" "OpenAPI spec" "200"
test_endpoint "/mcp/health" "Health check" "200"

# Test protected endpoints (should return 401 without auth)
echo -e "\n🔒 Protected Endpoints (no auth):"
test_endpoint "/mcp/tools" "Tools list" "401"
test_endpoint "/mcp/call" "Tool call" "401"
test_endpoint "/mcp/discover" "Discovery" "401"
test_endpoint "/mcp/events" "Events stream" "401"

# Test with valid API key (if provided)
if [ -n "$1" ]; then
    echo -e "\n🔓 Protected Endpoints (with API key):"
    test_endpoint "/mcp/tools" "Tools list" "200"
    test_endpoint "/mcp/discover" "Discovery" "200"
fi

echo -e "\n✅ MCP Documentation endpoint test complete!"
echo "📚 Documentation is now publicly accessible at: $BASE_URL/mcp/docs"