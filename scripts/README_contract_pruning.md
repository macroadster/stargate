# Stargate Database Contract Pruning Scripts

This directory contains scripts for safely pruning specific contracts from the Stargate database. `contract_prune.sh` supports both SQLite (the single-binary default) and PostgreSQL.

## Scripts Overview

### 1. `db_backup.sh` - Database Backup
Creates comprehensive backups before any pruning operations.

**Features:**
- Full database backup using `pg_dump`
- Contract-specific backup with detailed data extraction
- Verification of backup files
- Automatic backup directory creation

### 2. `contract_prune.sh` - Contract Pruning  
Safely removes a specific contract and all related data, then unpins/tombstones
its IPFS artifacts.

**Features:**
- SQLite and PostgreSQL backends (auto-detected, or force with `--db`)
- Dry-run mode to preview what will be deleted
- Proper foreign key deletion order
- Transaction-safe operations
- Detailed logging and confirmation
- Forced mode for automated execution
- Unpins referenced CIDs via Kubo (`IPFS_API_URL`) or the `ipfs` CLI
- Tombstones upload paths in the IPFS mirror so they are not republished
- Optional `--delete-files` to remove local upload copies

### 3. `contract_verify.sh` - Verification Script
Confirms that contract has been completely removed.

**Features:**
- Comprehensive table verification
- Orphaned data detection
- Detailed error reporting
- Cleanup recommendations

## Prerequisites

`contract_prune.sh` selects the backend automatically:

- **SQLite** (default when no PostgreSQL DSN is set): `sqlite3` must be installed.
  The database defaults to `$STARGATE_MCP_DB`, then
  `$STARGATE_DATA_DIR/sqlite/mcp.db`, then `data/sqlite/mcp.db`.
- **PostgreSQL** (when `MCP_PG_DSN`, `STARGATE_PG_DSN`, or `DATABASE_URL` is set,
  or `STARGATE_STORAGE=postgres`): `psql` must be installed.

Force a backend with `--db sqlite` or `--db postgres`.

> `db_backup.sh` and `contract_verify.sh` are still PostgreSQL-only. For SQLite,
> back up the `mcp.db` file (and its `-wal`/`-shm` siblings) before pruning.

## Environment Setup

```bash
# SQLite (single-binary default)
export STARGATE_DATA_DIR="/data"          # mcp.db at /data/sqlite/mcp.db
export UPLOADS_DIR="/data/uploads"        # IPFS tombstones live here
./scripts/contract_prune.sh --contract-id <id> --dry-run

# PostgreSQL
export MCP_PG_DSN="postgresql://username:password@hostname:5432/database_name"
./scripts/contract_prune.sh --contract-id <id> --dry-run

# Optional: set custom contract ID
export CONTRACT_ID="your-contract-id-here"
```

## IPFS Unpinning

Before deleting rows, the script collects:

- IPFS CIDs found in `stego_image_url`, contract metadata, task
  `requirements`/`merkle_proof`, submission `deliverables`/`completion_proof`,
  and proposal metadata (keys such as `stego_image_cid`, `stego_payload_cid`,
  `sandbox_tarball_cid`).
- `/uploads/...` paths referenced by the same rows.

After a successful commit it:

1. Unpins each CID through `IPFS_API_URL` (Kubo `/api/v0/pin/rm`) or the `ipfs`
   CLI when available.
2. Merges the upload paths into `.ipfs-mirror-deleted.json` and
   `.ipfs-wish-mirror-deleted.json` under `UPLOADS_DIR`, so the IPFS mirror
   stops advertising them and does not re-add them.
3. With `--delete-files`, removes the local upload copies as well.

The embedded IPFS node has no shell-accessible unpin API. Tombstones are read
when the mirror starts, so restart stargate after pruning to apply them. Direct
Kubo/CLI unpins apply immediately.

## Usage in Kubernetes

### Option 1: Direct Pod Execution

```bash
# Find the PostgreSQL pod
kubectl get pods -n default | grep postgres

# Execute backup in the pod
kubectl exec -it postgres-pod-xxxxx -- /bin/bash
cd /tmp/stargate_scripts
./db_backup.sh

# Execute pruning (after backup)
./contract_prune.sh --dry-run
./contract_prune.sh --force

# Verify removal
./contract_verify.sh --verbose
```

### Option 2: Copy Scripts to Pod

```bash
# Copy scripts to PostgreSQL pod
kubectl cp scripts/ postgres-pod-xxxxx:/tmp/stargate_scripts/

# Set executable permissions
kubectl exec postgres-pod-xxxxx -- chmod +x /tmp/stargate_scripts/*.sh

# Execute operations
kubectl exec postgres-pod-xxxxx -- bash -c "cd /tmp/stargate_scripts && ./db_backup.sh"
```

### Option 3: Using Local Database Connection

If you can connect to the Kubernetes PostgreSQL from your local machine:

```bash
# Port-forward PostgreSQL
kubectl port-forward service/postgres-service 5432:5432 -n default

# Set local connection
export MCP_PG_DSN="postgresql://username:password@localhost:5432/database_name"

# Run scripts locally
./scripts/db_backup.sh
./scripts/contract_prune.sh --dry-run
./scripts/contract_prune.sh --force
./scripts/contract_verify.sh --verbose
```

## Step-by-Step Pruning Process

### Step 1: Create Backup
```bash
./scripts/db_backup.sh
```
- Creates full database backup in `/tmp/stargate_backups/`
- Creates contract-specific backup
- Verifies backup integrity

### Step 2: Preview Pruning (Dry Run)
```bash
./scripts/contract_prune.sh --dry-run
```
- Shows what will be deleted
- Shows CIDs to unpin and upload paths to tombstone
- No actual deletion or IPFS change occurs
- Counts affected records in each table

### Step 3: Execute Pruning
```bash
./scripts/contract_prune.sh --force
```
- Safely deletes contract and related data
- Uses proper foreign key deletion order
- Transaction-safe operation
- Unpins/tombstones related IPFS artifacts

### Step 4: Verify Removal
```bash
./scripts/contract_verify.sh --verbose
```
- Confirms complete removal
- Checks for orphaned data
- Provides cleanup recommendations if needed

## Target Contract

Default target contract ID:
```
34f1777c3188b0fe397d8ce6a35c88f0de7bcdff4f35dd6b345fb5fc9bf8d0aa
```

## Script Arguments

### db_backup.sh
```bash
./db_backup.sh  # Uses default contract ID
```

### contract_prune.sh
```bash
./contract_prune.sh [OPTIONS]

Options:
  --dry-run           Preview deletion without executing
  --force             Skip confirmation prompts
  --contract-id ID    Specify different contract ID
  --all               Prune all contracts and related data
  --db KIND           Force sqlite or postgres
  --sqlite-db PATH    SQLite mcp.db path
  --uploads-dir DIR   Uploads directory for IPFS tombstones
  --delete-files      Delete local upload copies after pruning
  --help, -h          Show help
```

### contract_verify.sh
```bash
./contract_verify.sh [OPTIONS]

Options:
  --verbose, -v      Show detailed check information
  --contract-id ID    Specify different contract ID
  --help, -h         Show help
```

## Database Schema Affected

The scripts operate on these tables in deletion order:

1. **mcp_submissions** - Work submissions
2. **mcp_claims** - Task claims  
3. **mcp_escort_status** - Escort proof status (when the table exists)
4. **mcp_tasks** - Individual tasks
5. **mcp_contracts** - Main contracts
6. **mcp_proposals** - Contract proposals

## Safety Features

- **Transaction Safety**: All operations run in database transactions
- **Confirmation Prompts**: Requires explicit confirmation for deletion
- **Backup First**: Backup script must be run before pruning
- **Dry Run Mode**: Preview changes before executing
- **Verification**: Confirms complete removal
- **Foreign Key Order**: Proper deletion order to prevent constraint violations

## Troubleshooting

### Connection Issues
```bash
# PostgreSQL
psql "$MCP_PG_DSN" -c "SELECT version();"
which psql pg_dump

# SQLite
sqlite3 /data/sqlite/mcp.db "SELECT COUNT(*) FROM mcp_contracts;"
which sqlite3
```

### IPFS Unpin Issues
- Set `IPFS_API_URL` (for example `http://stargate-ipfs:5001`) or install the
  `ipfs` CLI to unpin CIDs directly.
- With the embedded node, pruning still tombstones paths. Restart stargate so
  the mirror reloads `.ipfs-mirror-deleted.json`.
- Use `--delete-files` to remove local upload copies as well.

### Permission Issues
```bash
# Ensure scripts are executable
chmod +x scripts/*.sh

# Check pod access
kubectl auth can-i exec pods/subresource --as=system:serviceaccount:default:default
```

### Backup Restoration
```bash
# Restore full backup
pg_restore -d database_name /tmp/stargate_backups/backup_file.dump

# Restore contract-specific backup
psql -d database_name -f /tmp/stargate_backups/contract_backup.sql
```

## Monitoring

During execution, monitor:
- PostgreSQL pod logs: `kubectl logs postgres-pod-xxxxx`
- Database connections: `psql -c "SELECT * FROM pg_stat_activity;"`
- Disk space: `df -h /tmp/`

## Emergency Rollback

If pruning causes issues:

1. **Stop all Stargate services**
2. **Restore backup**: `pg_restore -d database_name backup_file`
3. **Verify restoration**: Run verification script to confirm data integrity
4. **Restart services**: Resume normal operations

## Log Files

All scripts create detailed logs with timestamps. Log levels:
- ✅ **INFO**: Normal operations
- ⚠️ **WARNING**: Non-critical issues  
- ❌ **ERROR**: Failed operations
- ℹ️ **DEBUG**: Detailed information (with --verbose)

## Support

For issues or questions:
1. Check script logs for error messages
2. Verify database connectivity and permissions
3. Ensure PostgreSQL client tools are properly installed
4. Check Kubernetes pod status and resource availability