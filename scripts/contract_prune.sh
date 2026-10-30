#!/bin/bash

# Contract Pruning Script for Stargate (SQLite + PostgreSQL)
# Safely removes a specific contract and all related data, and unpins/tombstones
# its IPFS artifacts so the mirror does not republish them.

set -euo pipefail

# Configuration
CONTRACT_ID="34f1777c3188b0fe397d8ce6a35c88f0de7bcdff4f35dd6b345fb5fc9bf8d0aa"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
DRY_RUN=false
FORCE=false
PRUNE_ALL=false
DELETE_FILES=false

# Database / IPFS configuration (resolved in main)
DB_KIND=""                 # postgres | sqlite
DB_KIND_OVERRIDE=""
SQLITE_DB=""               # resolved SQLite path
SQLITE_DB_OVERRIDE=""
UPLOADS_DIR="${UPLOADS_DIR:-}"             # resolved uploads directory
UPLOADS_DIR_OVERRIDE=""
HAS_ESCORT=false
REFS_COLLECTED=false
COLLECTED_CIDS=""
COLLECTED_PATHS=""

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
            --db)
                DB_KIND_OVERRIDE="$2"
                shift 2
                ;;
            --sqlite-db)
                SQLITE_DB_OVERRIDE="$2"
                shift 2
                ;;
            --uploads-dir)
                UPLOADS_DIR_OVERRIDE="$2"
                shift 2
                ;;
            --delete-files)
                DELETE_FILES=true
                shift
                ;;
            --help|-h)
                cat << EOF
Stargate Contract Pruning Script

Usage: $0 [OPTIONS]

Database selection:
    By default the script uses SQLite when STARGATE_PG_DSN / DATABASE_URL are
    unset, and PostgreSQL when one of them is set (matching the app default).

Options:
    --dry-run           Show what would be deleted/unpinned without changing anything
    --force             Skip confirmation prompts
    --contract-id ID    Specify contract ID to prune (default: $CONTRACT_ID)
    --all               Prune all contracts and related data
    --db KIND           Force database backend: sqlite | postgres
    --sqlite-db PATH    SQLite mcp.db path (default: \$STARGATE_MCP_DB or
                        \$STARGATE_DATA_DIR/sqlite/mcp.db)
    --uploads-dir DIR   Uploads directory for IPFS tombstones (default:
                        \$UPLOADS_DIR or \$STARGATE_DATA_DIR/uploads)
    --delete-files      Also delete the contract's local upload files
    --help, -h          Show this help message

Environment:
    PostgreSQL: MCP_PG_DSN, STARGATE_PG_DSN, or DATABASE_URL
    SQLite:     STARGATE_MCP_DB, STARGATE_DATA_DIR, STARGATE_STORAGE
    IPFS:       IPFS_API_URL (Kubo /api/v0), IPFS_PATH (for the ipfs CLI)

IPFS unpinning:
    * CIDs referenced by contract/task/submission/proposal metadata are
      unpinned through IPFS_API_URL (Kubo) or the ipfs CLI when available.
    * Upload paths are always tombstoned in .ipfs-mirror-deleted.json and
      .ipfs-wish-mirror-deleted.json so the embedded node stops advertising
      them and does not re-add them.
    * The embedded IPFS node has no shell-accessible API. Restart stargate
      after pruning so the mirror reloads the tombstone files.

Examples:
    $0 --dry-run
    $0 --contract-id abc123 --force
    $0 --db sqlite --sqlite-db /data/sqlite/mcp.db --contract-id abc123
    $0 --all --force --delete-files
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

# Validate IDs before interpolating them into SQL.
validate_contract_id() {
    if [[ "$PRUNE_ALL" = true ]]; then
        return 0
    fi
    if [[ -z "$CONTRACT_ID" ]]; then
        error "Contract ID must not be empty"
        exit 1
    fi
    if [[ ! "$CONTRACT_ID" =~ ^[A-Za-z0-9._:-]+$ ]]; then
        error "Invalid contract ID (allowed: letters, digits, . _ : -): $CONTRACT_ID"
        exit 1
    fi
}

# Resolve the database backend.
resolve_db_kind() {
    local forced
    forced=$(echo "${DB_KIND_OVERRIDE:-}" | tr '[:upper:]' '[:lower:]')
    case "$forced" in
        "") ;;
        postgres|pg|postgresql) echo "postgres"; return 0 ;;
        sqlite|sqlite3) echo "sqlite"; return 0 ;;
        *)
            error "Unsupported --db value: $DB_KIND_OVERRIDE (use sqlite or postgres)"
            exit 1
            ;;
    esac

    if [[ "${STARGATE_STORAGE:-}" == "postgres" ]]; then
        echo "postgres"
        return 0
    fi
    if [[ "${STARGATE_STORAGE:-}" == "sqlite" ]]; then
        echo "sqlite"
        return 0
    fi
    if [[ "${STARGATE_STORAGE:-}" == "memory" ]]; then
        error "STARGATE_STORAGE=memory has no persistent database to prune (use --db sqlite or --db postgres)"
        exit 1
    fi
    if [[ -n "${MCP_PG_DSN:-}" || -n "${STARGATE_PG_DSN:-}" || -n "${DATABASE_URL:-}" ]]; then
        echo "postgres"
        return 0
    fi
    echo "sqlite"
}

# Get PostgreSQL connection string from environment.
get_db_dsn() {
    if [[ -n "${MCP_PG_DSN:-}" ]]; then
        echo "$MCP_PG_DSN"
    elif [[ -n "${STARGATE_PG_DSN:-}" ]]; then
        echo "$STARGATE_PG_DSN"
    elif [[ -n "${DATABASE_URL:-}" ]]; then
        echo "$DATABASE_URL"
    else
        error "No PostgreSQL connection string found. Set MCP_PG_DSN, STARGATE_PG_DSN, or DATABASE_URL (or use --db sqlite)"
        exit 1
    fi
}

# Parse database connection.
parse_dsn() {
    local dsn="$1"
    if [[ $dsn =~ ^postgres(ql)?://([^:@/]+)(:([^@]*))?@([^:/]+)(:([0-9]+))?/([^?]+) ]]; then
        export PGUSER="${BASH_REMATCH[2]}"
        if [[ -n "${BASH_REMATCH[4]}" ]]; then
            export PGPASSWORD="${BASH_REMATCH[4]}"
        fi
        export PGHOST="${BASH_REMATCH[5]}"
        export PGPORT="${BASH_REMATCH[7]:-5432}"
        export PGDATABASE="${BASH_REMATCH[8]}"
    else
        error "Invalid DSN format (expected postgresql://user:pass@host:port/db): $dsn"
        exit 1
    fi
}

# Resolve the SQLite database path.
resolve_sqlite_db() {
    if [[ -n "$SQLITE_DB_OVERRIDE" ]]; then
        echo "$SQLITE_DB_OVERRIDE"
    elif [[ -n "${STARGATE_MCP_DB:-}" ]]; then
        echo "$STARGATE_MCP_DB"
    elif [[ -n "${STARGATE_DATA_DIR:-}" ]]; then
        echo "${STARGATE_DATA_DIR}/sqlite/mcp.db"
    else
        echo "data/sqlite/mcp.db"
    fi
}

# Resolve the uploads directory.
resolve_uploads_dir() {
    if [[ -n "$UPLOADS_DIR_OVERRIDE" ]]; then
        echo "$UPLOADS_DIR_OVERRIDE"
    elif [[ -n "${UPLOADS_DIR:-}" ]]; then
        echo "$UPLOADS_DIR"
    elif [[ -n "${STARGATE_DATA_DIR:-}" ]]; then
        echo "${STARGATE_DATA_DIR}/uploads"
    else
        echo "data/uploads"
    fi
}

# Check dependencies for the selected backend.
check_dependencies() {
    case "$DB_KIND" in
        postgres)
            if ! command -v psql &> /dev/null; then
                error "psql is not installed or not in PATH"
                exit 1
            fi
            ;;
        sqlite)
            if ! command -v sqlite3 &> /dev/null; then
                error "sqlite3 is not installed or not in PATH"
                exit 1
            fi
            ;;
    esac
}

# Run a SQL query and return raw rows.
db_query() {
    local sql="$1"
    case "$DB_KIND" in
        postgres)
            psql -t -A -v ON_ERROR_STOP=1 -c "$sql"
            ;;
        sqlite)
            sqlite3 -batch -noheader -cmd ".timeout 10000" "$SQLITE_DB" "$sql"
            ;;
    esac
}

# Run a SQL query and return a single trimmed scalar.
db_scalar() {
    db_query "$1" | tr -d '[:space:]' | head -n 1
}

# Return a text expression for a JSON/JSONB column.
json_text_expr() {
    if [[ "$DB_KIND" = "postgres" ]]; then
        echo "COALESCE($1::text,'')"
    else
        echo "COALESCE($1,'')"
    fi
}

# Check whether a table exists.
table_exists() {
    local table="$1"
    case "$DB_KIND" in
        postgres)
            [[ "$(db_scalar "SELECT to_regclass('$table') IS NOT NULL;")" = "t" ]]
            ;;
        sqlite)
            [[ "$(db_scalar "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='$table';")" -gt 0 ]]
            ;;
    esac
}

# Test database connection.
test_connection() {
    info "Testing $DB_KIND database connection..."
    case "$DB_KIND" in
        postgres)
            if ! psql -c "SELECT 1;" &> /dev/null; then
                error "Cannot connect to PostgreSQL database"
                exit 1
            fi
            if [[ "$(db_scalar "SELECT to_regclass('mcp_contracts') IS NOT NULL;")" != "t" ]]; then
                error "mcp_contracts table not found in PostgreSQL database"
                exit 1
            fi
            ;;
        sqlite)
            if [[ ! -f "$SQLITE_DB" ]]; then
                error "SQLite database not found: $SQLITE_DB"
                exit 1
            fi
            if ! sqlite3 -batch "$SQLITE_DB" "SELECT 1;" &> /dev/null; then
                error "Cannot open SQLite database: $SQLITE_DB"
                exit 1
            fi
            if [[ "$(db_scalar "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='mcp_contracts';")" -eq 0 ]]; then
                error "mcp_contracts table not found in SQLite database: $SQLITE_DB"
                exit 1
            fi
            info "SQLite journal mode: $(db_scalar "PRAGMA journal_mode;")"
            ;;
    esac
    info "Database connection successful"
}

# Check if contract exists.
check_contract_exists() {
    if [[ "$PRUNE_ALL" = true ]]; then
        info "Checking for any contracts in database"
        local count proposal_count
        count=$(db_scalar "SELECT COUNT(*) FROM mcp_contracts;")
        proposal_count=$(db_scalar "SELECT COUNT(*) FROM mcp_proposals;")
        info "Contracts: $count"
        info "Proposals: $proposal_count"
        if [[ "$count" -eq 0 && "$proposal_count" -eq 0 ]]; then
            warn "No contracts or proposals found in database"
            return 1
        fi
        return 0
    fi

    info "Checking if contract exists: $CONTRACT_ID"

    local count proposal_count
    count=$(db_scalar "SELECT COUNT(*) FROM mcp_contracts WHERE contract_id = '$CONTRACT_ID';")
    proposal_count=$(db_scalar "SELECT COUNT(*) FROM mcp_proposals WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';")

    if [[ "$count" -eq 0 && "$proposal_count" -eq 0 ]]; then
        warn "Contract not found in database"
        info "No records found with contract_id: $CONTRACT_ID"
        return 1
    fi

    info "Found contract in mcp_contracts: $count"
    info "Found matching proposals: $proposal_count"
    return 0
}

# Show what will be deleted.
show_affected_data() {
    local contracts tasks claims submissions proposals
    if [[ "$PRUNE_ALL" = true ]]; then
        info "Analyzing affected data for all contracts"
        contracts=$(db_scalar "SELECT COUNT(*) FROM mcp_contracts;")
        tasks=$(db_scalar "SELECT COUNT(*) FROM mcp_tasks;")
        claims=$(db_scalar "SELECT COUNT(*) FROM mcp_claims;")
        submissions=$(db_scalar "SELECT COUNT(*) FROM mcp_submissions;")
        proposals=$(db_scalar "SELECT COUNT(*) FROM mcp_proposals;")
    else
        info "Analyzing affected data for contract: $CONTRACT_ID"
        contracts=$(db_scalar "SELECT COUNT(*) FROM mcp_contracts WHERE contract_id = '$CONTRACT_ID';")
        tasks=$(db_scalar "SELECT COUNT(*) FROM mcp_tasks WHERE contract_id = '$CONTRACT_ID';")
        claims=$(db_scalar "SELECT COUNT(c.claim_id) FROM mcp_claims c JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';")
        submissions=$(db_scalar "SELECT COUNT(s.submission_id) FROM mcp_submissions s JOIN mcp_claims c ON s.claim_id = c.claim_id JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';")
        proposals=$(db_scalar "SELECT COUNT(*) FROM mcp_proposals WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';")
    fi

    echo "=== AFFECTED RECORDS ==="
    echo -e "${YELLOW}Contracts:${NC} $contracts"
    echo -e "${YELLOW}Tasks:${NC} $tasks"
    echo -e "${YELLOW}Claims:${NC} $claims"
    echo -e "${YELLOW}Submissions:${NC} $submissions"
    echo -e "${YELLOW}Proposals:${NC} $proposals"
    if [[ "$HAS_ESCORT" = true ]]; then
        local escort
        if [[ "$PRUNE_ALL" = true ]]; then
            escort=$(db_scalar "SELECT COUNT(*) FROM mcp_escort_status;")
        else
            escort=$(db_scalar "SELECT COUNT(*) FROM mcp_escort_status e JOIN mcp_tasks t ON e.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';")
        fi
        echo -e "${YELLOW}Escort status:${NC} $escort"
    fi
    echo

    # Show some details about the contract if it exists
    if [[ "$PRUNE_ALL" = false && "$contracts" -gt 0 ]]; then
        info "Contract details:"
        db_query "SELECT contract_id || ' | ' || COALESCE(title,'') || ' | ' || COALESCE(total_budget_sats,0) || ' sats | ' || COALESCE(status,'') FROM mcp_contracts WHERE contract_id = '$CONTRACT_ID';"
    fi

    if [[ "$PRUNE_ALL" = false && "$proposals" -gt 0 ]]; then
        info "Proposal details:"
        db_query "SELECT id || ' | ' || COALESCE(title,'') || ' | ' || COALESCE(budget_sats,0) || ' sats | ' || COALESCE(status,'') FROM mcp_proposals WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';"
    fi
}

# Build the SQL that gathers every text blob that may reference IPFS data.
build_ipfs_ref_sql() {
    local meta req proof deliv comp prop_meta c_where t_where p_where sub_where
    meta=$(json_text_expr "metadata")
    req=$(json_text_expr "requirements")
    proof=$(json_text_expr "merkle_proof")
    deliv=$(json_text_expr "s.deliverables")
    comp=$(json_text_expr "s.completion_proof")
    prop_meta=$(json_text_expr "metadata")

    c_where=""
    t_where=""
    p_where=""
    sub_where=""
    if [[ "$PRUNE_ALL" = false ]]; then
        c_where=" WHERE contract_id = '$CONTRACT_ID'"
        t_where=" WHERE contract_id = '$CONTRACT_ID'"
        p_where=" WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID'"
        sub_where=" WHERE t.contract_id = '$CONTRACT_ID'"
    fi

    cat << SQL
SELECT COALESCE(stego_image_url,'') FROM mcp_contracts$c_where;
SELECT $meta FROM mcp_contracts$c_where;
SELECT $req || ' ' || $proof FROM mcp_tasks$t_where;
SELECT $deliv || ' ' || $comp FROM mcp_submissions s JOIN mcp_claims c ON s.claim_id = c.claim_id JOIN mcp_tasks t ON c.task_id = t.task_id$sub_where;
SELECT $prop_meta FROM mcp_proposals$p_where;
SQL
}

# Collect CIDs and upload paths referenced by the rows being pruned.
collect_ipfs_refs() {
    if [[ "$REFS_COLLECTED" = true ]]; then
        return 0
    fi

    local raw
    raw=$(db_query "$(build_ipfs_ref_sql)") || raw=""

    COLLECTED_CIDS=$(printf '%s\n' "$raw" \
        | grep -oE 'baf[a-z2-7]{20,}|Qm[1-9A-HJ-NP-Za-km-z]{44}' \
        | sort -u || true)
    COLLECTED_PATHS=$(printf '%s\n' "$raw" \
        | grep -oE '/uploads/[A-Za-z0-9._~%/-]+' \
        | sed -E 's/[.,;:)]+$//' \
        | sort -u || true)

    REFS_COLLECTED=true
}

# Print the IPFS references that will be unpinned/tombstoned.
show_ipfs_refs() {
    collect_ipfs_refs
    echo "=== IPFS REFERENCES ==="
    if [[ -z "$COLLECTED_CIDS" && -z "$COLLECTED_PATHS" ]]; then
        echo "None found"
        echo
        return 0
    fi
    if [[ -n "$COLLECTED_CIDS" ]]; then
        echo -e "${YELLOW}CIDs to unpin:${NC}"
        printf '%s\n' "$COLLECTED_CIDS" | sed 's/^/  - /'
    fi
    if [[ -n "$COLLECTED_PATHS" ]]; then
        echo -e "${YELLOW}Upload paths to tombstone:${NC}"
        printf '%s\n' "$COLLECTED_PATHS" | sed 's/^/  - /'
    fi
    echo
}

# Generate pruning SQL (portable across PostgreSQL and SQLite).
generate_pruning_sql() {
    local temp_sql
    temp_sql=$(mktemp "${TMPDIR:-/tmp}/prune_contract_${TIMESTAMP}.XXXXXX")

    local c_where="" t_where="" p_where=""
    if [[ "$PRUNE_ALL" = false ]]; then
        c_where=" WHERE contract_id = '$CONTRACT_ID'"
        t_where=" WHERE contract_id = '$CONTRACT_ID'"
        p_where=" WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID'"
    fi

    local counts_sql
    counts_sql="SELECT 'CONTRACTS: ' || COUNT(*) AS count FROM mcp_contracts${c_where};
SELECT 'TASKS: ' || COUNT(*) AS count FROM mcp_tasks${t_where};"
    if [[ "$PRUNE_ALL" = true ]]; then
        counts_sql+="
SELECT 'CLAIMS: ' || COUNT(*) AS count FROM mcp_claims;
SELECT 'SUBMISSIONS: ' || COUNT(*) AS count FROM mcp_submissions;
SELECT 'PROPOSALS: ' || COUNT(*) AS count FROM mcp_proposals;"
    else
        counts_sql+="
SELECT 'CLAIMS: ' || COUNT(c.claim_id) AS count FROM mcp_claims c JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';
SELECT 'SUBMISSIONS: ' || COUNT(s.submission_id) AS count FROM mcp_submissions s JOIN mcp_claims c ON s.claim_id = c.claim_id JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';
SELECT 'PROPOSALS: ' || COUNT(*) AS count FROM mcp_proposals WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';"
    fi
    if [[ "$HAS_ESCORT" = true ]]; then
        if [[ "$PRUNE_ALL" = true ]]; then
            counts_sql+="
SELECT 'ESCORT STATUS: ' || COUNT(*) AS count FROM mcp_escort_status;"
        else
            counts_sql+="
SELECT 'ESCORT STATUS: ' || COUNT(e.task_id) AS count FROM mcp_escort_status e JOIN mcp_tasks t ON e.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';"
        fi
    fi

    local delete_sql
    if [[ "$PRUNE_ALL" = true ]]; then
        delete_sql="DELETE FROM mcp_submissions;
DELETE FROM mcp_claims;"
        if [[ "$HAS_ESCORT" = true ]]; then
            delete_sql+="
DELETE FROM mcp_escort_status;"
        fi
        delete_sql+="
DELETE FROM mcp_tasks;
DELETE FROM mcp_contracts;
DELETE FROM mcp_proposals;"
    else
        delete_sql="DELETE FROM mcp_submissions WHERE claim_id IN (SELECT c.claim_id FROM mcp_claims c JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID');
DELETE FROM mcp_claims WHERE task_id IN (SELECT task_id FROM mcp_tasks WHERE contract_id = '$CONTRACT_ID');"
        if [[ "$HAS_ESCORT" = true ]]; then
            delete_sql+="
DELETE FROM mcp_escort_status WHERE task_id IN (SELECT task_id FROM mcp_tasks WHERE contract_id = '$CONTRACT_ID');"
        fi
        delete_sql+="
DELETE FROM mcp_tasks WHERE contract_id = '$CONTRACT_ID';
DELETE FROM mcp_contracts WHERE contract_id = '$CONTRACT_ID';
DELETE FROM mcp_proposals WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';"
    fi

    local dry_sql
    if [[ "$PRUNE_ALL" = true ]]; then
        dry_sql="SELECT 'Contracts to be deleted:';
SELECT contract_id || ' | ' || COALESCE(title,'') || ' | ' || COALESCE(total_budget_sats,0) || ' sats | ' || COALESCE(status,'') FROM mcp_contracts;
SELECT 'Tasks to be deleted:';
SELECT task_id || ' | ' || COALESCE(title,'') || ' | ' || COALESCE(budget_sats,0) || ' sats | ' || COALESCE(status,'') FROM mcp_tasks;
SELECT 'Claims to be deleted:';
SELECT claim_id || ' | ' || task_id || ' | ' || COALESCE(status,'') FROM mcp_claims;
SELECT 'Submissions to be deleted:';
SELECT submission_id || ' | ' || claim_id || ' | ' || COALESCE(status,'') FROM mcp_submissions;
SELECT 'Proposals to be deleted:';
SELECT id || ' | ' || COALESCE(title,'') || ' | ' || COALESCE(budget_sats,0) || ' sats | ' || COALESCE(status,'') FROM mcp_proposals;"
    else
        dry_sql="SELECT 'Contracts to be deleted:';
SELECT contract_id || ' | ' || COALESCE(title,'') || ' | ' || COALESCE(total_budget_sats,0) || ' sats | ' || COALESCE(status,'') FROM mcp_contracts WHERE contract_id = '$CONTRACT_ID';
SELECT 'Tasks to be deleted:';
SELECT task_id || ' | ' || COALESCE(title,'') || ' | ' || COALESCE(budget_sats,0) || ' sats | ' || COALESCE(status,'') FROM mcp_tasks WHERE contract_id = '$CONTRACT_ID';
SELECT 'Claims to be deleted:';
SELECT c.claim_id || ' | ' || c.task_id || ' | ' || COALESCE(c.status,'') FROM mcp_claims c JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';
SELECT 'Submissions to be deleted:';
SELECT s.submission_id || ' | ' || s.claim_id || ' | ' || COALESCE(s.status,'') FROM mcp_submissions s JOIN mcp_claims c ON s.claim_id = c.claim_id JOIN mcp_tasks t ON c.task_id = t.task_id WHERE t.contract_id = '$CONTRACT_ID';
SELECT 'Proposals to be deleted:';
SELECT id || ' | ' || COALESCE(title,'') || ' | ' || COALESCE(budget_sats,0) || ' sats | ' || COALESCE(status,'') FROM mcp_proposals WHERE visible_pixel_hash = '$CONTRACT_ID' OR id = '$CONTRACT_ID';"
    fi

    {
        echo "-- Contract Pruning Script for $([ "$PRUNE_ALL" = true ] && echo "<all>" || echo "$CONTRACT_ID")"
        echo "-- Generated on $(date)"
        echo "-- Backend: $DB_KIND"
        echo "-- Mode: $([ "$DRY_RUN" = true ] && echo "DRY RUN" || echo "EXECUTE")"
        echo
        if [[ "$DB_KIND" = "postgres" ]]; then
            echo "BEGIN;"
            echo "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE;"
        else
            echo ".timeout 10000"
            echo "PRAGMA foreign_keys = ON;"
            echo "BEGIN IMMEDIATE;"
        fi
        echo
        echo "SELECT '=== RECORD COUNTS BEFORE DELETION ===';"
        echo "$counts_sql"
        echo
        if [[ "$DRY_RUN" = true ]]; then
            echo "SELECT '=== RECORDS THAT WOULD BE DELETED ===';"
            echo "$dry_sql"
            echo
            echo "ROLLBACK;"
            echo "SELECT 'DRY RUN COMPLETED - No changes made';"
        else
            echo "SELECT '=== DELETING RECORDS ===';"
            echo "$delete_sql"
            echo
            echo "SELECT '=== RECORD COUNTS AFTER DELETION ===';"
            echo "$counts_sql"
            echo
            echo "COMMIT;"
            echo "SELECT 'PRUNING COMPLETED SUCCESSFULLY';"
        fi
    } > "$temp_sql"

    echo "$temp_sql"
}

# Execute pruning.
execute_pruning() {
    local sql_file
    sql_file=$(generate_pruning_sql)

    info "Generated pruning SQL: $sql_file"

    if [[ "$DRY_RUN" = true ]]; then
        warn "DRY RUN MODE - No actual deletion will occur"
        echo
        echo "Preview of what would be deleted:"
        echo "================================"
    fi

    case "$DB_KIND" in
        postgres)
            if ! psql -v ON_ERROR_STOP=1 -f "$sql_file"; then
                error "Failed to execute pruning script"
                rm -f "$sql_file"
                exit 1
            fi
            ;;
        sqlite)
            if ! sqlite3 -batch -bail -cmd ".timeout 10000" "$SQLITE_DB" < "$sql_file"; then
                error "Failed to execute pruning script"
                rm -f "$sql_file"
                exit 1
            fi
            ;;
    esac

    if [[ "$DRY_RUN" = false ]]; then
        log "Contract pruning completed successfully"
        if [[ "$PRUNE_ALL" = true ]]; then
            log "All contracts and related data have been removed"
        else
            log "Contract $CONTRACT_ID and all related data have been removed"
        fi
    else
        info "Dry run completed successfully"
        info "No changes were made to the database"
    fi

    # Clean up temp file
    rm -f "$sql_file"
}

# Convert an upload path into the logical path used by the IPFS mirror.
normalize_upload_rel() {
    local rel="$1"
    rel="${rel#/uploads/}"
    rel="${rel%%\?*}"
    rel="${rel%%#*}"
    local base
    base=$(basename "$rel")
    if [[ "$base" =~ ^[0-9a-fA-F]{64}$ ]]; then
        rel="$base"
    fi
    echo "$rel"
}

# Merge paths into a mirror tombstone file ({"paths":[...]}), atomically.
tombstone_paths() {
    local file="$1"
    shift
    if [[ $# -eq 0 ]]; then
        return 0
    fi

    local dir tmp list
    dir=$(dirname "$file")
    if ! mkdir -p "$dir"; then
        warn "Cannot create tombstone directory: $dir"
        return 1
    fi
    tmp=$(mktemp "${file}.XXXXXX") || return 1
    list=$(mktemp "${TMPDIR:-/tmp}/tombstone.XXXXXX") || {
        rm -f "$tmp"
        return 1
    }

    {
        if [[ -f "$file" ]]; then
            # Existing files contain only {"paths":[...]}; pull the values.
            grep -o '"[^"]*"' "$file" 2>/dev/null | tr -d '"' | grep -v '^paths$' || true
        fi
        printf '%s\n' "$@"
    } | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//' | grep -v '^$' | sort -u > "$list"

    {
        printf '{"paths":['
        awk 'BEGIN{first=1} {gsub(/"/,"\\\""); if(first){first=0}else{printf ","}; printf "\"%s\"",$0}' "$list"
        printf ']}\n'
    } > "$tmp"

    if ! mv "$tmp" "$file"; then
        warn "Failed to update tombstone file: $file"
        rm -f "$tmp" "$list"
        return 1
    fi
    chmod 0644 "$file" 2>/dev/null || true
    rm -f "$list"
    return 0
}

# Resolve an upload path to an on-disk file (partitioned or legacy layout).
resolve_upload_file() {
    local rel="$1"
    local base candidate
    base=$(basename "$rel")
    if [[ "$rel" == results/* ]]; then
        local after="${rel#results/}"
        local key="${after%%/*}"
        local rest=""
        if [[ "$after" == */* ]]; then
            rest="${after#*/}"
        fi
        if [[ "$key" =~ ^(wish-|contract-)?[0-9a-fA-F]{64}$ ]]; then
            local hex="${key#wish-}"
            hex="${hex#contract-}"
            hex=$(echo "$hex" | tr '[:upper:]' '[:lower:]')
            candidate="${UPLOADS_DIR}/results/${hex:0:2}/${hex:2:2}/${hex:4:2}/${hex}"
            if [[ -n "$rest" ]]; then
                candidate="${candidate}/${rest}"
            fi
            if [[ -e "$candidate" ]]; then
                echo "$candidate"
                return 0
            fi
        fi
        candidate="${UPLOADS_DIR}/${rel}"
        if [[ -e "$candidate" ]]; then
            echo "$candidate"
            return 0
        fi
        return 1
    fi

    if [[ "$base" =~ ^[0-9a-fA-F]{64}$ ]]; then
        local hex
        hex=$(echo "$base" | tr '[:upper:]' '[:lower:]')
        candidate="${UPLOADS_DIR}/${hex:0:2}/${hex:2:2}/${hex:4:2}/${hex}"
        if [[ -e "$candidate" ]]; then
            echo "$candidate"
            return 0
        fi
        candidate="${UPLOADS_DIR}/${hex}"
        if [[ -e "$candidate" ]]; then
            echo "$candidate"
            return 0
        fi
        return 1
    fi

    candidate="${UPLOADS_DIR}/${rel}"
    if [[ -e "$candidate" ]]; then
        echo "$candidate"
        return 0
    fi
    return 1
}

# Unpin one CID via Kubo HTTP API or the ipfs CLI.
unpin_cid() {
    local cid="$1"
    local out status

    if [[ -n "${IPFS_API_URL:-}" ]]; then
        if ! command -v curl &> /dev/null; then
            warn "IPFS_API_URL is set but curl is not installed; cannot unpin $cid via HTTP"
        else
            local api="${IPFS_API_URL%/}"
            out=$(curl -sS -m 20 -w $'\n%{http_code}' -X POST "${api}/api/v0/pin/rm?arg=${cid}" 2>&1) || true
            status="${out##*$'\n'}"
            out="${out%$'\n'*}"
            if [[ "$status" =~ ^2 ]]; then
                log "Unpinned CID via Kubo: $cid"
                return 0
            fi
            if [[ "$out" == *"not pinned"* ]]; then
                info "CID already unpinned: $cid"
                return 0
            fi
            warn "Kubo unpin failed for $cid (HTTP $status): $out"
        fi
    fi

    if command -v ipfs &> /dev/null; then
        if out=$(ipfs pin rm "$cid" 2>&1); then
            log "Unpinned CID via ipfs CLI: $cid"
            return 0
        fi
        if [[ "$out" == *"not pinned"* ]]; then
            info "CID already unpinned: $cid"
            return 0
        fi
        warn "ipfs CLI unpin failed for $cid: $out"
        return 1
    fi

    warn "No IPFS_API_URL (Kubo) or ipfs CLI available; cannot unpin CID $cid directly"
    return 1
}

# Unpin CIDs and tombstone upload paths after a successful prune.
prune_ipfs_refs() {
    collect_ipfs_refs

    local cid rel deleted=0
    if [[ -n "$COLLECTED_CIDS" ]]; then
        info "Unpinning IPFS CIDs..."
        while IFS= read -r cid; do
            [[ -z "$cid" ]] && continue
            unpin_cid "$cid" || true
        done <<< "$COLLECTED_CIDS"
    fi

    if [[ -z "$COLLECTED_PATHS" ]]; then
        if [[ -z "$COLLECTED_CIDS" ]]; then
            info "No IPFS references found for the pruned data"
        fi
        return 0
    fi

    local tombstone_uploads="${UPLOADS_DIR}/.ipfs-mirror-deleted.json"
    local tombstone_wishes="${UPLOADS_DIR}/.ipfs-wish-mirror-deleted.json"
    local -a rels=()
    while IFS= read -r rel; do
        [[ -z "$rel" ]] && continue
        rel=$(normalize_upload_rel "$rel")
        [[ -z "$rel" || "$rel" == *".."* ]] && continue
        rels+=("$rel")
    done <<< "$COLLECTED_PATHS"

    if [[ ${#rels[@]} -gt 0 ]]; then
        if [[ -d "$UPLOADS_DIR" ]]; then
            info "Tombstoning upload paths so the IPFS mirror does not republish them..."
            tombstone_paths "$tombstone_uploads" "${rels[@]}" || true
            tombstone_paths "$tombstone_wishes" "${rels[@]}" || true
        else
            warn "Uploads directory not found ($UPLOADS_DIR); skipping IPFS tombstones"
        fi

        if [[ "$DELETE_FILES" = true ]]; then
            local path
            for rel in "${rels[@]}"; do
                if path=$(resolve_upload_file "$rel"); then
                    if rm -f -- "$path"; then
                        log "Deleted local upload: $path"
                        deleted=$((deleted + 1))
                    else
                        warn "Failed to delete local upload: $path"
                    fi
                fi
            done
        fi
    fi

    warn "Embedded IPFS has no shell-accessible unpin API. Tombstones take effect when"
    warn "stargate restarts (the mirror reloads them at startup). Kubo/CLI unpins above"
    warn "apply immediately. Use --delete-files to remove the local copies as well."
    if [[ "$DELETE_FILES" = true ]]; then
        info "Deleted $deleted local upload file(s)"
    fi
}

# Confirmation prompt.
confirm_action() {
    if [[ "$FORCE" = true ]]; then
        return 0
    fi

    if [[ "$DRY_RUN" = true ]]; then
        echo
        echo -e "${BLUE}This is a DRY RUN. No data will be deleted or unpinned.${NC}"
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
    echo -e "${YELLOW}Related IPFS CIDs will be unpinned (when an IPFS API/CLI is available)${NC}"
    echo -e "${YELLOW}and upload paths will be tombstoned in the IPFS mirror.${NC}"
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
    validate_contract_id

    # Resolve backend and paths
    DB_KIND=$(resolve_db_kind)
    UPLOADS_DIR=$(resolve_uploads_dir)

    log "Starting Stargate contract pruning"
    if [[ "$PRUNE_ALL" = true ]]; then
        log "Contract ID: <all>"
    else
        log "Contract ID: $CONTRACT_ID"
    fi
    log "Mode: $([ "$DRY_RUN" = true ] && echo "DRY RUN" || echo "EXECUTE")"
    log "Prune all: $PRUNE_ALL"
    log "Database: $DB_KIND"
    log "Uploads dir: $UPLOADS_DIR"

    if [[ "$DB_KIND" = "postgres" ]]; then
        local dsn
        dsn=$(get_db_dsn)
        info "Using PostgreSQL connection: ${dsn%%:*}:***@${dsn##*@}"
        parse_dsn "$dsn"
    else
        SQLITE_DB=$(resolve_sqlite_db)
        info "Using SQLite database: $SQLITE_DB"
    fi

    # Setup and validation
    check_dependencies
    test_connection

    HAS_ESCORT=false
    if table_exists "mcp_escort_status"; then
        HAS_ESCORT=true
    fi

    # Check if contract exists
    if ! check_contract_exists; then
        if [[ "$FORCE" = false ]]; then
            error "Contract not found. Use --force to continue anyway."
            exit 1
        fi
    fi

    # Show what will be affected
    show_affected_data
    show_ipfs_refs

    # Confirm action
    confirm_action

    # Execute pruning
    execute_pruning

    # Unpin/tombstone IPFS artifacts only after a successful commit
    if [[ "$DRY_RUN" = false ]]; then
        prune_ipfs_refs
        log "Pruning operation completed successfully!"
        log "Remember to verify the deletion using the verification script"
    fi
}

# Run main function with all arguments
main "$@"
