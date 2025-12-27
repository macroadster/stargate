#!/bin/bash

# Contract Pruning Script for Stargate PostgreSQL
# Safely removes a specific contract and all related data

set -euo pipefail

# Configuration
CONTRACT_ID="34f1777c3188b0fe397d8ce6a35c88f0de7bcdff4f35dd6b345fb5fc9bf8d0aa"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
DRY_RUN=false
FORCE=false
PRUNE_ALL=false

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
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            --force)
                FORCE=true
                shift
                ;;
            --contract-id)
                CONTRACT_ID="$2"
                shift 2
                ;;
            --all)
                PRUNE_ALL=true
                shift
                ;;
            --help|-h)
                cat << EOF
Stargate Contract Pruning Script

Usage: $0 [OPTIONS]

Options:
    --dry-run          Show what would be deleted without actually deleting
    --force            Skip confirmation prompts
    --contract-id ID    Specify contract ID to prune (default: $CONTRACT_ID)
    --all              Prune all contracts and related data
    --help, -h         Show this help message

Example:
    $0 --dry-run
    $0 --contract-id abc123 --force
    $0 --all --force
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
        exit 1
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

# Check if contract exists
check_contract_exists() {
    if [[ "$PRUNE_ALL" = true ]]; then
        info "Checking for any contracts in database"
        local count=$(psql -t -c "SELECT COUNT(*) FROM mcp_contracts;" | tr -d ' ')
        local proposal_count=$(psql -t -c "SELECT COUNT(*) FROM mcp_proposals;" | tr -d ' ')
        info "Contracts: $count"
        info "Proposals: $proposal_count"
        if [[ "$count" -eq 0 && "$proposal_count" -eq 0 ]]; then
            warn "No contracts or proposals found in database"
            return 1
        fi
        return 0
    fi

    info "Checking if contract exists: $CONTRACT_ID"
    
    local count=$(psql -t -c "SELECT COUNT(*) FROM mcp_contracts WHERE contract_id = '$CONTRACT_ID';" | tr -d ' ')
    local proposal_count=$(psql -t -c "SELECT COUNT(*) FROM mcp_proposals WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';" | tr -d ' ')
    
    if [[ "$count" -eq 0 && "$proposal_count" -eq 0 ]]; then
        warn "Contract not found in database"
        info "No records found with contract_id: $CONTRACT_ID"
        return 1
    fi
    
    info "Found contract in mcp_contracts: $count"
    info "Found matching proposals: $proposal_count"
    return 0
}

# Show what will be deleted
show_affected_data() {
    if [[ "$PRUNE_ALL" = true ]]; then
        info "Analyzing affected data for all contracts"
        local contracts=$(psql -t -c "SELECT COUNT(*) FROM mcp_contracts;" | tr -d ' ')
        local tasks=$(psql -t -c "SELECT COUNT(*) FROM mcp_tasks;" | tr -d ' ')
        local claims=$(psql -t -c "SELECT COUNT(*) FROM mcp_claims;" | tr -d ' ')
        local submissions=$(psql -t -c "SELECT COUNT(*) FROM mcp_submissions;" | tr -d ' ')
        local proposals=$(psql -t -c "SELECT COUNT(*) FROM mcp_proposals;" | tr -d ' ')
        echo "=== AFFECTED RECORDS ==="
        echo -e "${YELLOW}Contracts:${NC} $contracts"
        echo -e "${YELLOW}Tasks:${NC} $tasks"
        echo -e "${YELLOW}Claims:${NC} $claims"
        echo -e "${YELLOW}Submissions:${NC} $submissions"
        echo -e "${YELLOW}Proposals:${NC} $proposals"
        echo
        return
    fi

    info "Analyzing affected data for contract: $CONTRACT_ID"
    
    # Count affected records in each table
    local contracts=$(psql -t -c "SELECT COUNT(*) FROM mcp_contracts WHERE contract_id = '$CONTRACT_ID';" | tr -d ' ')
    local tasks=$(psql -t -c "SELECT COUNT(*) FROM mcp_tasks WHERE contract_id = '$CONTRACT_ID';" | tr -d ' ')
    local claims=$(psql -t -c "SELECT COUNT(c.claim_id) FROM mcp_claims c JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';" | tr -d ' ')
    local submissions=$(psql -t -c "SELECT COUNT(s.submission_id) FROM mcp_submissions s JOIN mcp_claims c ON s.claim_id = c.claim_id JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';" | tr -d ' ')
    local proposals=$(psql -t -c "SELECT COUNT(*) FROM mcp_proposals WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';" | tr -d ' ')
    
    echo "=== AFFECTED RECORDS ==="
    echo -e "${YELLOW}Contracts:${NC} $contracts"
    echo -e "${YELLOW}Tasks:${NC} $tasks"
    echo -e "${YELLOW}Claims:${NC} $claims"
    echo -e "${YELLOW}Submissions:${NC} $submissions"
    echo -e "${YELLOW}Proposals:${NC} $proposals"
    echo
    
    # Show some details about the contract if it exists
    if [[ "$contracts" -gt 0 ]]; then
        info "Contract details:"
        psql -c "SELECT contract_id, title, total_budget_sats, status FROM mcp_contracts WHERE contract_id = '$CONTRACT_ID';"
    fi
    
    if [[ "$proposals" -gt 0 ]]; then
        info "Proposal details:"
        psql -c "SELECT id, title, budget_sats, status, created_at FROM mcp_proposals WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';"
    fi
}

# Generate pruning SQL
generate_pruning_sql() {
    local temp_sql="/tmp/prune_contract_${TIMESTAMP}.sql"
    
    cat > "$temp_sql" << EOF
-- Contract Pruning Script for $CONTRACT_ID
-- Generated on $(date)
-- Mode: $([ "$DRY_RUN" = true ] && echo "DRY RUN" || echo "EXECUTE")

BEGIN;

-- Display contract being pruned
\\echo 'Pruning contract: $CONTRACT_ID'
\\echo 'Timestamp: $(date)'

-- Set transaction isolation level
SET TRANSACTION ISOLATION LEVEL SERIALIZABLE;

-- Show record counts before deletion
\\echo '=== RECORD COUNTS BEFORE DELETION ==='

SELECT 'CONTRACTS: ' || COUNT(*) as count FROM mcp_contracts $( [[ "$PRUNE_ALL" = true ]] && echo ";" || echo "WHERE contract_id = '$CONTRACT_ID';" );
SELECT 'TASKS: ' || COUNT(*) as count FROM mcp_tasks $( [[ "$PRUNE_ALL" = true ]] && echo ";" || echo "WHERE contract_id = '$CONTRACT_ID';" );
SELECT 'CLAIMS: ' || COUNT(c.claim_id) as count FROM mcp_claims c $( [[ "$PRUNE_ALL" = true ]] && echo ";" || echo "JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';" );
SELECT 'SUBMISSIONS: ' || COUNT(s.submission_id) as count FROM mcp_submissions s $( [[ "$PRUNE_ALL" = true ]] && echo ";" || echo "JOIN mcp_claims c ON s.claim_id = c.claim_id JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';" );
SELECT 'PROPOSALS: ' || COUNT(*) as count FROM mcp_proposals $( [[ "$PRUNE_ALL" = true ]] && echo ";" || echo "WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';" );

EOF

    if [[ "$DRY_RUN" = true ]]; then
        cat >> "$temp_sql" << EOF
-- DRY RUN MODE - Showing what would be deleted

\\echo '=== RECORDS THAT WOULD BE DELETED ==='

-- Show contract records
\\echo 'Contracts to be deleted:'
$( [[ "$PRUNE_ALL" = true ]] && echo "SELECT contract_id, title, total_budget_sats, status FROM mcp_contracts;" || echo "SELECT contract_id, title, total_budget_sats, status FROM mcp_contracts WHERE contract_id = '$CONTRACT_ID';" )

-- Show task records
\\echo 'Tasks to be deleted:'
$( [[ "$PRUNE_ALL" = true ]] && echo "SELECT task_id, title, budget_sats, status FROM mcp_tasks;" || echo "SELECT task_id, title, budget_sats, status FROM mcp_tasks WHERE contract_id = '$CONTRACT_ID';" )

-- Show claim records
\\echo 'Claims to be deleted:'
$( [[ "$PRUNE_ALL" = true ]] && echo "SELECT claim_id, task_id, status, created_at FROM mcp_claims;" || echo "SELECT c.claim_id, c.task_id, c.status, c.created_at FROM mcp_claims c JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';" )

-- Show submission records
\\echo 'Submissions to be deleted:'
$( [[ "$PRUNE_ALL" = true ]] && echo "SELECT submission_id, claim_id, status, created_at FROM mcp_submissions;" || echo "SELECT s.submission_id, s.claim_id, s.status, s.created_at FROM mcp_submissions s JOIN mcp_claims c ON s.claim_id = c.claim_id JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';" )

-- Show proposal records
\\echo 'Proposals to be deleted:'
$( [[ "$PRUNE_ALL" = true ]] && echo "SELECT id, title, budget_sats, status, created_at FROM mcp_proposals;" || echo "SELECT id, title, budget_sats, status, created_at FROM mcp_proposals WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';" )

ROLLBACK;
\\echo 'DRY RUN COMPLETED - No changes made';

EOF
    else
        cat >> "$temp_sql" << EOF
-- EXECUTE MODE - Deleting records

\\echo '=== DELETING RECORDS ==='

-- Delete in correct foreign key order

-- Step 1: Delete submissions (via claims -> tasks -> contract)
\\echo 'Deleting submissions...'
$( [[ "$PRUNE_ALL" = true ]] && echo "DELETE FROM mcp_submissions;" || echo "DELETE FROM mcp_submissions WHERE claim_id IN (SELECT c.claim_id FROM mcp_claims c JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID');" )

-- Step 2: Delete claims (via tasks -> contract)
\\echo 'Deleting claims...'
$( [[ "$PRUNE_ALL" = true ]] && echo "DELETE FROM mcp_claims;" || echo "DELETE FROM mcp_claims WHERE task_id IN (SELECT task_id FROM mcp_tasks WHERE contract_id = '$CONTRACT_ID');" )

-- Step 3: Delete tasks
\\echo 'Deleting tasks...'
$( [[ "$PRUNE_ALL" = true ]] && echo "DELETE FROM mcp_tasks;" || echo "DELETE FROM mcp_tasks WHERE contract_id = '$CONTRACT_ID';" )

-- Step 4: Delete contracts
\\echo 'Deleting contracts...'
$( [[ "$PRUNE_ALL" = true ]] && echo "DELETE FROM mcp_contracts;" || echo "DELETE FROM mcp_contracts WHERE contract_id = '$CONTRACT_ID';" )

-- Step 5: Delete proposals (may reference contract by hash or ID)
\\echo 'Deleting proposals...'
$( [[ "$PRUNE_ALL" = true ]] && echo "DELETE FROM mcp_proposals;" || echo "DELETE FROM mcp_proposals WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';" )

-- Show record counts after deletion
\\echo '=== RECORD COUNTS AFTER DELETION ==='

SELECT 'CONTRACTS: ' || COUNT(*) as count FROM mcp_contracts $( [[ "$PRUNE_ALL" = true ]] && echo ";" || echo "WHERE contract_id = '$CONTRACT_ID';" );
SELECT 'TASKS: ' || COUNT(*) as count FROM mcp_tasks $( [[ "$PRUNE_ALL" = true ]] && echo ";" || echo "WHERE contract_id = '$CONTRACT_ID';" );
SELECT 'CLAIMS: ' || COUNT(c.claim_id) as count FROM mcp_claims c $( [[ "$PRUNE_ALL" = true ]] && echo ";" || echo "JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';" );
SELECT 'SUBMISSIONS: ' || COUNT(s.submission_id) as count FROM mcp_submissions s $( [[ "$PRUNE_ALL" = true ]] && echo ";" || echo "JOIN mcp_claims c ON s.claim_id = c.claim_id JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';" );
SELECT 'PROPOSALS: ' || COUNT(*) as count FROM mcp_proposals $( [[ "$PRUNE_ALL" = true ]] && echo ";" || echo "WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';" );

COMMIT;
\\echo 'PRUNING COMPLETED SUCCESSFULLY';

EOF
    fi
    
    echo "$temp_sql"
}

# Execute pruning
execute_pruning() {
    local sql_file=$(generate_pruning_sql)
    
    info "Generated pruning SQL: $sql_file"
    
    if [[ "$DRY_RUN" = true ]]; then
        warn "DRY RUN MODE - No actual deletion will occur"
        echo
        echo "Preview of what would be deleted:"
        echo "================================"
    fi
    
    # Execute the SQL
    if ! psql -f "$sql_file"; then
        error "Failed to execute pruning script"
        exit 1
    fi

    if [[ "$DRY_RUN" = false ]]; then
        log "Contract pruning completed successfully"
        log "Contract $CONTRACT_ID and all related data have been removed"
    else
        info "Dry run completed successfully"
        info "No changes were made to the database"
    fi
    
    # Clean up temp file
    rm -f "$sql_file"
}

# Confirmation prompt
confirm_action() {
    if [[ "$FORCE" = true ]]; then
        return 0
    fi
    
    if [[ "$DRY_RUN" = true ]]; then
        echo
        echo -e "${BLUE}This is a DRY RUN. No data will be deleted.${NC}"
        read -p "Continue with dry run? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            log "Operation cancelled by user"
            exit 0
        fi
        return 0
    fi
    
    echo
    if [[ "$PRUNE_ALL" = true ]]; then
        echo -e "${RED}WARNING: This will permanently delete ALL contracts and related data!${NC}"
        echo -e "${RED}This includes all contracts, tasks, claims, submissions, and proposals.${NC}"
    else
        echo -e "${RED}WARNING: This will permanently delete contract $CONTRACT_ID and all related data!${NC}"
        echo -e "${RED}This includes contracts, tasks, claims, submissions, and proposals.${NC}"
    fi
    echo
    echo -e "${YELLOW}Make sure you have a recent backup before proceeding!${NC}"
    echo
    if [[ "$PRUNE_ALL" = true ]]; then
        read -p "Type DELETE ALL to confirm deletion: " -r
        if [[ "$REPLY" != "DELETE ALL" ]]; then
            error "Confirmation mismatch. Operation cancelled for safety."
            exit 1
        fi
    else
        read -p "Type the contract ID to confirm deletion: " -r
        if [[ "$REPLY" != "$CONTRACT_ID" ]]; then
            error "Contract ID mismatch. Operation cancelled for safety."
            exit 1
        fi
    fi
    
    echo
    read -p "Are you absolutely sure you want to proceed? (yes/NO): " -r
    if [[ "$REPLY" != "yes" ]]; then
        log "Operation cancelled by user"
        exit 0
    fi
}

# Main execution
main() {
    # Parse arguments
    parse_args "$@"

    log "Starting Stargate contract pruning"
    if [[ "$PRUNE_ALL" = true ]]; then
        log "Contract ID: <all>"
    else
        log "Contract ID: $CONTRACT_ID"
    fi
    log "Mode: $([ "$DRY_RUN" = true ] && echo "DRY RUN" || echo "EXECUTE")"
    log "Prune all: $PRUNE_ALL"
    
    # Get database connection
    local dsn
    dsn=$(get_db_dsn)
    info "Using database connection: ${dsn%%:*}:***@${dsn##*@}"
    
    # Parse DSN and set environment
    parse_dsn "$dsn"
    
    # Setup and validation
    check_dependencies
    test_connection
    
    # Check if contract exists
    if ! check_contract_exists; then
        if [[ "$FORCE" = false ]]; then
            error "Contract not found. Use --force to continue anyway."
        fi
    fi
    
    # Show what will be affected
    show_affected_data
    
    # Confirm action
    confirm_action
    
    # Execute pruning
    execute_pruning
    
    if [[ "$DRY_RUN" = false ]]; then
        log "Pruning operation completed successfully!"
        log "Remember to verify the deletion using the verification script"
    fi
}

# Run main function with all arguments
main "$@"
