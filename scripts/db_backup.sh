#!/bin/bash

# Database Backup Script for Stargate PostgreSQL
# Creates a full backup before contract pruning operations

set -euo pipefail

# Configuration
CONTRACT_ID="34f1777c3188b0fe397d8ce6a35c88f0de7bcdff4f35dd6b345fb5fc9bf8d0aa"

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --contract-id)
                CONTRACT_ID="$2"
                shift 2
                ;;
            --help|-h)
                cat << EOF
Stargate Database Backup Script

Usage: $0 [OPTIONS]

Options:
    --contract-id ID    Specify contract ID to backup (default: $CONTRACT_ID)
    --help, -h         Show this help message

Example:
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
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
BACKUP_DIR="/tmp/stargate_backups"
BACKUP_FILE="${BACKUP_DIR}/stargate_backup_${TIMESTAMP}.sql"
CONTRACT_BACKUP_FILE="${BACKUP_DIR}/contract_${CONTRACT_ID}_${TIMESTAMP}.sql"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[$(date +'%Y-%m-%d %H:%M:%S')] WARNING:${NC} $1"
}

error() {
    echo -e "${RED}[$(date +'%Y-%m-%d %H:%M:%S')] ERROR:${NC} $1"
    exit 1
}

# Get database connection from environment
get_db_dsn() {
    # Try multiple environment variable names
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

# Extract connection details from DSN
parse_dsn() {
    local dsn="$1"
    # Parse postgresql://user:pass@host:port/database
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

# Check if psql is available
check_dependencies() {
    if ! command -v psql &> /dev/null; then
        error "psql is not installed or not in PATH"
    fi
    
    if ! command -v pg_dump &> /dev/null; then
        error "pg_dump is not installed or not in PATH"
    fi
}

# Test database connection
test_connection() {
    log "Testing database connection..."
    if ! psql -c "SELECT 1;" &> /dev/null; then
        error "Cannot connect to database"
    fi
    log "Database connection successful"
}

# Create backup directory
create_backup_dir() {
    mkdir -p "$BACKUP_DIR"
    log "Created backup directory: $BACKUP_DIR"
}

# Create full database backup
create_full_backup() {
    log "Creating full database backup..."
    if pg_dump --format=custom --compress=9 --file="$BACKUP_FILE" "$PGDATABASE"; then
        log "Full backup created: $BACKUP_FILE"
        log "Backup size: $(du -h "$BACKUP_FILE" | cut -f1)"
    else
        error "Failed to create full backup"
    fi
}

# Create contract-specific backup
create_contract_backup() {
    log "Creating contract-specific backup for: $CONTRACT_ID"
    
    local temp_sql="${BACKUP_DIR}/contract_data_${TIMESTAMP}.sql"
    
    # Create SQL to extract contract data
    cat > "$temp_sql" << EOF
-- Contract data backup for $CONTRACT_ID
-- Generated on $(date)

-- Set transaction isolation level
SET TRANSACTION ISOLATION LEVEL SERIALIZABLE;

-- Backup contracts
\\o ${BACKUP_DIR}/contracts_${TIMESTAMP}.sql
SELECT 'INSERT INTO mcp_contracts (contract_id, title, total_budget_sats, goals_count, available_tasks_count, status, skills) VALUES (' ||
       quote_literal(contract_id) || ', ' ||
       quote_literal(title) || ', ' ||
       COALESCE(total_budget_sats::text, 'NULL') || ', ' ||
       COALESCE(goals_count::text, 'NULL') || ', ' ||
       COALESCE(available_tasks_count::text, 'NULL') || ', ' ||
       quote_literal(status) || ', ' ||
       'ARRAY[' || array_to_string(skills, ',', 'quote_literal') || ']' ||
       ');' as sql_statement
FROM mcp_contracts 
WHERE contract_id = '$CONTRACT_ID';

-- Backup tasks
\\o ${BACKUP_DIR}/tasks_${TIMESTAMP}.sql
SELECT 'INSERT INTO mcp_tasks (task_id, contract_id, goal_id, title, description, budget_sats, skills, status, claimed_by, claimed_at, claim_expires_at, difficulty, estimated_hours, requirements, merkle_proof) VALUES (' ||
       quote_literal(task_id) || ', ' ||
       quote_literal(contract_id) || ', ' ||
       quote_literal(goal_id) || ', ' ||
       quote_literal(title) || ', ' ||
       quote_literal(description) || ', ' ||
       COALESCE(budget_sats::text, 'NULL') || ', ' ||
       'ARRAY[' || array_to_string(skills, ',', 'quote_literal') || ']' || ', ' ||
       quote_literal(status) || ', ' ||
       quote_literal(claimed_by) || ', ' ||
       COALESCE(quote_literal(claimed_at::text), 'NULL') || ', ' ||
       COALESCE(quote_literal(claim_expires_at::text), 'NULL') || ', ' ||
       quote_literal(difficulty) || ', ' ||
       COALESCE(estimated_hours::text, 'NULL') || ', ' ||
       quote_literal(requirements::text) || ', ' ||
       quote_literal(merkle_proof::text) ||
       ');' as sql_statement
FROM mcp_tasks 
WHERE contract_id = '$CONTRACT_ID';

-- Backup claims (via tasks)
\\o ${BACKUP_DIR}/claims_${TIMESTAMP}.sql
SELECT 'INSERT INTO mcp_claims (claim_id, task_id, ai_identifier, status, expires_at, created_at) VALUES (' ||
       quote_literal(c.claim_id) || ', ' ||
       quote_literal(c.task_id) || ', ' ||
       quote_literal(c.ai_identifier) || ', ' ||
       quote_literal(c.status) || ', ' ||
       COALESCE(quote_literal(c.expires_at::text), 'NULL') || ', ' ||
       COALESCE(quote_literal(c.created_at::text), 'NULL') ||
       ');' as sql_statement
FROM mcp_claims c
JOIN mcp_tasks t ON c.task_id = t.task_id
WHERE t.contract_id = '$CONTRACT_ID';

-- Backup submissions (via claims)
\\o ${BACKUP_DIR}/submissions_${TIMESTAMP}.sql
SELECT 'INSERT INTO mcp_submissions (submission_id, claim_id, status, deliverables, completion_proof, rejection_reason, rejection_type, rejected_at, created_at) VALUES (' ||
       quote_literal(s.submission_id) || ', ' ||
       quote_literal(s.claim_id) || ', ' ||
       quote_literal(s.status) || ', ' ||
       quote_literal(deliverables::text) || ', ' ||
       quote_literal(completion_proof::text) || ', ' ||
       quote_literal(rejection_reason) || ', ' ||
       quote_literal(rejection_type) || ', ' ||
       COALESCE(quote_literal(rejected_at::text), 'NULL') || ', ' ||
       COALESCE(quote_literal(created_at::text), 'NULL') ||
       ');' as sql_statement
FROM mcp_submissions s
JOIN mcp_claims c ON s.claim_id = c.claim_id
JOIN mcp_tasks t ON c.task_id = t.task_id
WHERE t.contract_id = '$CONTRACT_ID';

-- Backup proposals
\\o ${BACKUP_DIR}/proposals_${TIMESTAMP}.sql
SELECT 'INSERT INTO mcp_proposals (id, title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at) VALUES (' ||
       quote_literal(id) || ', ' ||
       quote_literal(title) || ', ' ||
       quote_literal(description_md) || ', ' ||
       quote_literal(visible_pixel_hash) || ', ' ||
       COALESCE(budget_sats::text, 'NULL') || ', ' ||
       quote_literal(status) || ', ' ||
       quote_literal(metadata::text) || ', ' ||
       COALESCE(quote_literal(created_at::text), 'NULL') ||
       ');' as sql_statement
FROM mcp_proposals 
WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';

\\o
EOF

    # Execute the backup
    if psql -f "$temp_sql" &> /dev/null; then
        # Combine all backup files
        cat "${BACKUP_DIR}/contracts_${TIMESTAMP}.sql" \
            "${BACKUP_DIR}/tasks_${TIMESTAMP}.sql" \
            "${BACKUP_DIR}/claims_${TIMESTAMP}.sql" \
            "${BACKUP_DIR}/submissions_${TIMESTAMP}.sql" \
            "${BACKUP_DIR}/proposals_${TIMESTAMP}.sql" > "$CONTRACT_BACKUP_FILE"
        
        # Clean up individual files
        rm -f "${BACKUP_DIR}/contracts_${TIMESTAMP}.sql" \
              "${BACKUP_DIR}/tasks_${TIMESTAMP}.sql" \
              "${BACKUP_DIR}/claims_${TIMESTAMP}.sql" \
              "${BACKUP_DIR}/submissions_${TIMESTAMP}.sql" \
              "${BACKUP_DIR}/proposals_${TIMESTAMP}.sql" \
              "$temp_sql"
        
        log "Contract backup created: $CONTRACT_BACKUP_FILE"
        log "Contract backup size: $(du -h "$CONTRACT_BACKUP_FILE" | cut -f1)"
    else
        error "Failed to create contract backup"
    fi
}

# Verify backup files
verify_backups() {
    log "Verifying backup files..."
    
    if [[ ! -f "$BACKUP_FILE" ]]; then
        error "Full backup file not found: $BACKUP_FILE"
    fi
    
    if [[ ! -f "$CONTRACT_BACKUP_FILE" ]]; then
        error "Contract backup file not found: $CONTRACT_BACKUP_FILE"
    fi
    
    # Check if files are not empty
    if [[ ! -s "$BACKUP_FILE" ]]; then
        error "Full backup file is empty"
    fi
    
    if [[ ! -s "$CONTRACT_BACKUP_FILE" ]]; then
        warn "Contract backup file is empty (contract may not exist in database)"
    fi
    
    log "Backup files verified successfully"
}

# Show backup summary
backup_summary() {
    log "=== Backup Summary ==="
    log "Full Database Backup: $BACKUP_FILE"
    log "Contract Backup: $CONTRACT_BACKUP_FILE"
    log "Contract ID: $CONTRACT_ID"
    log "Timestamp: $TIMESTAMP"
    log "Backup Directory: $BACKUP_DIR"
    
    # Show disk usage
    log "Disk Usage:"
    du -h "$BACKUP_DIR" | tail -1
}

main() {
    # Parse arguments
    parse_args "$@"
    
    log "Starting Stargate database backup for contract pruning"
    log "Contract ID: $CONTRACT_ID"
    
    # Get database connection
    local dsn
    dsn=$(get_db_dsn)
    log "Using database connection: ${dsn%%:*}:***@${dsn##*@}"
    
    # Parse DSN and set environment variables
    parse_dsn "$dsn"
    
    # Run backup process
    check_dependencies
    test_connection
    create_backup_dir
    create_full_backup
    create_contract_backup
    verify_backups
    backup_summary
    
    log "Backup completed successfully!"
    log "You can now proceed with contract pruning."
    log "To restore: pg_restore -d $PGDATABASE $BACKUP_FILE"
}

# Run main function
main "$@"