#!/bin/bash

# Contract Pruning Verification Script for Stargate PostgreSQL
# Verifies that a contract has been completely removed from the database

set -euo pipefail

# Configuration
CONTRACT_ID="34f1777c3188b0fe397d8ce6a35c88f0de7bcdff4f35dd6b345fb5fc9bf8d0aa"
VERBOSE=false

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

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

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --verbose|-v)
                VERBOSE=true
                shift
                ;;
            --contract-id)
                CONTRACT_ID="$2"
                shift 2
                ;;
            --help|-h)
                cat << EOF
Stargate Contract Pruning Verification Script

Usage: $0 [OPTIONS]

Options:
    --verbose, -v      Show detailed information about all checks
    --contract-id ID    Specify contract ID to verify (default: $CONTRACT_ID)
    --help, -h         Show this help message

Example:
    $0 --verbose
    $0 --contract-id abc123
EOF
                exit 0
                ;;
            *)
                error "Unknown option: $1"
                exit 1
                ;;
        esac
    done
}

# Get database connection from environment
get_db_dsn() {
    if [[ -n "${MCP_PG_DSN:-}" ]]; then
        echo "$MCP_PG_DSN"
    elif [[ -n "${STARGATE_PG_DSN:-}" ]]; then
        echo "$STARGATE_PG_DSN"
    elif [[ -n "${DATABASE_URL:-}" ]]; then
        echo "$DATABASE_URL"
    else
        error "No database connection string found. Set MCP_PG_DSN, STARGATE_PG_DSN, or DATABASE_URL"
    fi
}

# Parse database connection
parse_dsn() {
    local dsn="$1"
    if [[ $dsn =~ postgresql://([^:]+):([^@]+)@([^:]+):([0-9]+)/(.+) ]]; then
        export PGUSER="${BASH_REMATCH[1]}"
        export PGPASSWORD="${BASH_REMATCH[2]}"
        export PGHOST="${BASH_REMATCH[3]}"
        export PGPORT="${BASH_REMATCH[4]}"
        export PGDATABASE="${BASH_REMATCH[5]}"
    else
        error "Invalid DSN format: $dsn"
    fi
}

# Check dependencies
check_dependencies() {
    if ! command -v psql &> /dev/null; then
        error "psql is not installed or not in PATH"
    fi
}

# Test database connection
test_connection() {
    info "Testing database connection..."
    if ! psql -c "SELECT 1;" &> /dev/null; then
        error "Cannot connect to database"
    fi
    info "Database connection successful"
}

# Verify table has no matching records
verify_table_clean() {
    local table_name="$1"
    local where_clause="$2"
    local expected_count="$3"
    
    local actual_count
    actual_count=$(psql -t -c "SELECT COUNT(*) FROM $table_name WHERE $where_clause;" | tr -d ' ')
    
    if [[ "$actual_count" -eq "$expected_count" ]]; then
        echo -e "  ${GREEN}✓${NC} $table_name: Clean ($actual_count records)"
        return 0
    else
        echo -e "  ${RED}✗${NC} $table_name: Found $actual_count records (expected $expected_count)"
        if [[ "$VERBOSE" == true ]]; then
            echo -e "    ${YELLOW}Where clause:${NC} $where_clause"
            echo -e "    ${YELLOW}Actual records:${NC}"
            psql -c "SELECT * FROM $table_name WHERE $where_clause LIMIT 5;" 2>/dev/null || echo "    (Cannot display records)"
        fi
        return 1
    fi
}

# Comprehensive verification
run_verification() {
    local errors=0
    
    log "Starting verification for contract: $CONTRACT_ID"
    echo
    
    # Verify each table in the dependency order
    
    # 1. Verify mcp_contracts
    echo -e "${BLUE}Verifying mcp_contracts table...${NC}"
    if ! verify_table_clean "mcp_contracts" "contract_id = '$CONTRACT_ID'" 0; then
        ((errors++))
    fi
    echo
    
    # 2. Verify mcp_tasks
    echo -e "${BLUE}Verifying mcp_tasks table...${NC}"
    if ! verify_table_clean "mcp_tasks" "contract_id = '$CONTRACT_ID'" 0; then
        ((errors++))
    fi
    echo
    
    # 3. Verify mcp_claims (via tasks)
    echo -e "${BLUE}Verifying mcp_claims table...${NC}"
    local claims_count
    claims_count=$(psql -t -c "SELECT COUNT(c.claim_id) FROM mcp_claims c JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';" | tr -d ' ')
    if [[ "$claims_count" -eq 0 ]]; then
        echo -e "  ${GREEN}✓${NC} mcp_claims: Clean (0 records via tasks)"
    else
        echo -e "  ${RED}✗${NC} mcp_claims: Found $claims_count records (expected 0)"
        if [[ "$VERBOSE" == true ]]; then
            echo -e "    ${YELLOW}Claims via tasks:${NC}"
            psql -c "SELECT c.claim_id, c.task_id, c.status FROM mcp_claims c JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID' LIMIT 5;" 2>/dev/null || echo "    (Cannot display records)"
        fi
        ((errors++))
    fi
    echo
    
    # 4. Verify mcp_submissions (via claims -> tasks)
    echo -e "${BLUE}Verifying mcp_submissions table...${NC}"
    local submissions_count
    submissions_count=$(psql -t -c "SELECT COUNT(s.submission_id) FROM mcp_submissions s JOIN mcp_claims c ON s.claim_id = c.claim_id JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';" | tr -d ' ')
    if [[ "$submissions_count" -eq 0 ]]; then
        echo -e "  ${GREEN}✓${NC} mcp_submissions: Clean (0 records via claims->tasks)"
    else
        echo -e "  ${RED}✗${NC} mcp_submissions: Found $submissions_count records (expected 0)"
        if [[ "$VERBOSE" == true ]]; then
            echo -e "    ${YELLOW}Submissions via claims->tasks:${NC}"
            psql -c "SELECT s.submission_id, s.claim_id, s.status FROM mcp_submissions s JOIN mcp_claims c ON s.claim_id = c.claim_id JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID' LIMIT 5;" 2>/dev/null || echo "    (Cannot display records)"
        fi
        ((errors++))
    fi
    echo
    
    # 5. Verify mcp_proposals
    echo -e "${BLUE}Verifying mcp_proposals table...${NC}"
    if ! verify_table_clean "mcp_proposals" "visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID'" 0; then
        ((errors++))
    fi
    echo
    
    # 6. Check for any orphaned data
    echo -e "${BLUE}Checking for orphaned references...${NC}"
    
    # Check for claims without tasks
    local orphaned_claims
    orphaned_claims=$(psql -t -c "SELECT COUNT(*) FROM mcp_claims WHERE task_id NOT IN (SELECT task_id FROM mcp_tasks);" | tr -d ' ')
    if [[ "$orphaned_claims" -gt 0 ]]; then
        echo -e "  ${YELLOW}⚠${NC} Orphaned claims found: $orphaned_claims"
        if [[ "$VERBOSE" == true ]]; then
            psql -c "SELECT claim_id, task_id, status FROM mcp_claims WHERE task_id NOT IN (SELECT task_id FROM mcp_tasks) LIMIT 3;" 2>/dev/null
        fi
    else
        echo -e "  ${GREEN}✓${NC} No orphaned claims found"
    fi
    
    # Check for submissions without claims
    local orphaned_submissions
    orphaned_submissions=$(psql -t -c "SELECT COUNT(*) FROM mcp_submissions WHERE claim_id NOT IN (SELECT claim_id FROM mcp_claims);" | tr -d ' ')
    if [[ "$orphaned_submissions" -gt 0 ]]; then
        echo -e "  ${YELLOW}⚠${NC} Orphaned submissions found: $orphaned_submissions"
        if [[ "$VERBOSE" == true ]]; then
            psql -c "SELECT submission_id, claim_id, status FROM mcp_submissions WHERE claim_id NOT IN (SELECT claim_id FROM mcp_claims) LIMIT 3;" 2>/dev/null
        fi
    else
        echo -e "  ${GREEN}✓${NC} No orphaned submissions found"
    fi
    echo
    
    # 7. Summary statistics
    echo -e "${BLUE}Database Summary Statistics...${NC}"
    
    local total_contracts=$(psql -t -c "SELECT COUNT(*) FROM mcp_contracts;" | tr -d ' ')
    local total_tasks=$(psql -t -c "SELECT COUNT(*) FROM mcp_tasks;" | tr -d ' ')
    local total_claims=$(psql -t -c "SELECT COUNT(*) FROM mcp_claims;" | tr -d ' ')
    local total_submissions=$(psql -t -c "SELECT COUNT(*) FROM mcp_submissions;" | tr -d ' ')
    local total_proposals=$(psql -t -c "SELECT COUNT(*) FROM mcp_proposals;" | tr -d ' ')
    
    echo "  Total contracts: $total_contracts"
    echo "  Total tasks: $total_tasks"
    echo "  Total claims: $total_claims"
    echo "  Total submissions: $total_submissions"
    echo "  Total proposals: $total_proposals"
    echo
    
    # Return result
    if [[ "$errors" -eq 0 ]]; then
        log "Verification PASSED: Contract $CONTRACT_ID has been completely removed"
        echo -e "${GREEN}✓${NC} No trace of contract found in database"
        return 0
    else
        error "Verification FAILED: Found $errors issues with contract removal"
        echo -e "${RED}✗${NC} Contract data still exists in database"
        return 1
    fi
}

# Show detailed cleanup recommendations
show_recommendations() {
    log "Cleanup Recommendations:"
    
    echo
    echo -e "${YELLOW}If verification failed, consider the following:${NC}"
    echo "1. Re-run the pruning script with --force flag"
    echo "2. Check for related contracts with similar IDs"
    echo "3. Manually clean up any orphaned records"
    echo "4. Verify foreign key constraints didn't prevent deletion"
    echo
    echo -e "${YELLOW}Manual cleanup SQL if needed:${NC}"
    echo "-- Manually delete any remaining records:"
    echo "DELETE FROM mcp_submissions WHERE claim_id IN ("
    echo "  SELECT claim_id FROM mcp_claims WHERE task_id IN ("
    echo "    SELECT task_id FROM mcp_tasks WHERE contract_id = '$CONTRACT_ID'"
    echo "  )"
    echo ");"
    echo
    echo "DELETE FROM mcp_claims WHERE task_id IN ("
    echo "  SELECT task_id FROM mcp_tasks WHERE contract_id = '$CONTRACT_ID'"
    echo ");"
    echo
    echo "DELETE FROM mcp_tasks WHERE contract_id = '$CONTRACT_ID';"
    echo "DELETE FROM mcp_contracts WHERE contract_id = '$CONTRACT_ID';"
    echo "DELETE FROM mcp_proposals WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';"
    echo
}

# Main execution
main() {
    log "Starting contract pruning verification"
    log "Contract ID: $CONTRACT_ID"
    
    # Parse arguments
    parse_args "$@"
    
    # Get database connection
    local dsn
    dsn=$(get_db_dsn)
    info "Using database connection: ${dsn%%:*}:***@${dsn##*@}"
    
    # Parse DSN and set environment
    parse_dsn "$dsn"
    
    # Setup and verification
    check_dependencies
    test_connection
    
    # Run verification
    if run_verification; then
        log "Verification completed successfully"
        echo
        log "Contract $CONTRACT_ID has been completely pruned from database"
    else
        show_recommendations
        exit 1
    fi
}

# Run main function with all arguments
main "$@"