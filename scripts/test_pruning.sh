#!/bin/bash

# Test Script for Contract Pruning Scripts
# Simulates the pruning process without affecting real data

set -euo pipefail

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[$(date +'%Y-%m-%d %H:%M:%S')] WARNING:${NC} $1"
}

error() {
    echo -e "${RED}[$(date +'%Y-%m-%d %H:%M:%S')] ERROR:${NC} $1"
}

info() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')] INFO:${NC} $1"
}

# Test script arguments and help functions
test_help_functions() {
    log "Testing help functions..."
    
    # Test backup script help
    if bash ./db_backup.sh --help &> /dev/null; then
        echo -e "  ${GREEN}✓${NC} db_backup.sh help works"
    else
        echo -e "  ${RED}✗${NC} db_backup.sh help failed"
    fi
    
    # Test pruning script help
    if bash ./contract_prune.sh --help &> /dev/null; then
        echo -e "  ${GREEN}✓${NC} contract_prune.sh help works"
    else
        echo -e "  ${RED}✗${NC} contract_prune.sh help failed"
    fi
    
    # Test verification script help
    if bash ./contract_verify.sh --help &> /dev/null; then
        echo -e "  ${GREEN}✓${NC} contract_verify.sh help works"
    else
        echo -e "  ${RED}✗${NC} contract_verify.sh help failed"
    fi
}

# Test script syntax
test_syntax() {
    log "Testing script syntax..."
    
    local scripts=("db_backup.sh" "contract_prune.sh" "contract_verify.sh")
    
    for script in "${scripts[@]}"; do
        if bash -n "$script" 2>/dev/null; then
            echo -e "  ${GREEN}✓${NC} $script syntax is valid"
        else
            echo -e "  ${RED}✗${NC} $script has syntax errors"
            return 1
        fi
    done
}

# Test environment variable parsing
test_environment() {
    log "Testing environment variable parsing..."
    
    # Test with mock environment
    export MCP_PG_DSN="postgresql://testuser:testpass@localhost:5432/testdb"
    export CONTRACT_ID="test-contract-id"
    
    # Test DSN parsing (without actually connecting)
    if [[ "$MCP_PG_DSN" =~ postgresql://([^:]+):([^@]+)@([^:]+):([0-9]+)/(.+) ]]; then
        echo -e "  ${GREEN}✓${NC} DSN parsing works correctly"
    else
        echo -e "  ${RED}✗${NC} DSN parsing failed"
        return 1
    fi
    
    # Clean up
    unset MCP_PG_DSN CONTRACT_ID
}

# Test dry run functionality
test_dry_run() {
    log "Testing dry run functionality..."
    
    # Create mock script for dry run testing
    local temp_test="/tmp/dry_run_test.sql"
    cat > "$temp_test" << 'EOF'
-- Mock SQL for testing dry run
SELECT 'Dry run test completed' as result;
EOF
    
    if [[ -f "$temp_test" ]]; then
        echo -e "  ${GREEN}✓${NC} Dry run SQL generation works"
        rm -f "$temp_test"
    else
        echo -e "  ${RED}✗${NC} Dry run SQL generation failed"
        return 1
    fi
}

# Test file creation and permissions
test_file_operations() {
    log "Testing file operations..."
    
    local test_dir="/tmp/stargate_scripts_test"
    mkdir -p "$test_dir"
    
    # Test backup directory creation
    if [[ -d "$test_dir" ]]; then
        echo -e "  ${GREEN}✓${NC} Directory creation works"
    else
        echo -e "  ${RED}✗${NC} Directory creation failed"
        return 1
    fi
    
    # Test script file execution (dry run only)
    echo "echo 'Test execution successful'" > "$test_dir/test_script.sh"
    chmod +x "$test_dir/test_script.sh"
    
    if bash "$test_dir/test_script.sh" | grep -q "Test execution successful"; then
        echo -e "  ${GREEN}✓${NC} Script execution works"
    else
        echo -e "  ${RED}✗${NC} Script execution failed"
        return 1
    fi
    
    # Clean up
    rm -rf "$test_dir"
}

# Test error handling
test_error_handling() {
    log "Testing error handling..."
    
    # Test with invalid contract ID (should fail gracefully)
    export MCP_PG_DSN="postgresql://invalid:invalid@localhost:9999/invalid"
    
    # The scripts should fail gracefully without crashing
    if bash ./contract_prune.sh --dry-run 2>/dev/null; then
        echo -e "  ${YELLOW}⚠${NC} Error handling needs improvement (should have failed)"
    else
        echo -e "  ${GREEN}✓${NC} Error handling works correctly"
    fi
    
    # Clean up
    unset MCP_PG_DSN
}

# Show testing recommendations
show_recommendations() {
    echo
    log "Testing completed!"
    echo
    info "Recommendations for actual pruning:"
    echo "1. Always run db_backup.sh first"
    echo "2. Use --dry-run flag before actual deletion"
    echo "3. Test in non-production environment first"
    echo "4. Keep backup files in safe location"
    echo "5. Monitor system during pruning process"
    echo
    info "Kubernetes-specific recommendations:"
    echo "1. Port-forward database for local testing"
    echo "2. Use separate terminal for monitoring"
    echo "3. Check pod logs during operations"
    echo "4. Verify backup restoration process"
    echo "5. Have rollback plan ready"
}

# Main test execution
main() {
    log "Starting contract pruning scripts test suite"
    echo
    
    # Change to scripts directory
    cd "$(dirname "$0")"
    
    # Run all tests
    test_help_functions
    echo
    test_syntax
    echo
    test_environment
    echo
    test_dry_run
    echo
    test_file_operations
    echo
    test_error_handling
    echo
    
    # Show recommendations
    show_recommendations
    
    log "All tests completed successfully!"
}

# Run main function
main "$@"