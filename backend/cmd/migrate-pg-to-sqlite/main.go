// Command migrate-pg-to-sqlite performs a one-time data migration from a
// PostgreSQL Stargate backend to the SQLite embedded databases used for
// single-binary deployments (STARGATE_STORAGE=sqlite).
//
// It handles all dialect differences:
//   - TEXT[] skills  -> comma-separated TEXT
//   - JSONB          -> TEXT (JSON)
//   - TIMESTAMPTZ    -> TEXT (RFC3339)
//   - BIGINT/INT     -> INTEGER (preserving values)
//   - Proper handling of image_base64 and large JSON payloads in ingestions
//
// Tables migrated:
//
//	MCP:       mcp_contracts, mcp_tasks, mcp_claims, mcp_submissions,
//	           mcp_proposals, mcp_escort_status
//	Auth:      api_keys
//	Ingestion: starlight_ingestions, starlight_ingest_updates
//	Data:      block_scans  (metadata; actual block JSONs remain in data/blocks/)
//
// Usage:
//
//	go run ./backend/cmd/migrate-pg-to-sqlite \
//	  --pg-dsn "$STARGATE_PG_DSN" \
//	  --target-dir ./data/sqlite
//
//	# With overrides
//	... --mcp-db /path/mcp.db --api-keys-db /path/api_keys.db ...
//
// Safety:
//   - Refuses to run if target DBs contain data (unless --force)
//   - --dry-run only prints counts and exits
//   - Source Postgres is never modified
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	scstore "stargate-backend/storage/smart_contract"
)

var (
	pgDSN     = flag.String("pg-dsn", "", "PostgreSQL DSN (required). Use STARGATE_PG_DSN or DATABASE_URL value.")
	targetDir = flag.String("target-dir", "data/sqlite", "Directory for target SQLite files (mcp.db, api_keys.db, etc.)")
	mcpDBPath = flag.String("mcp-db", "", "Explicit path for mcp.db (overrides --target-dir)")
	apiKeysDB = flag.String("api-keys-db", "", "Explicit path for api_keys.db")
	ingestDB  = flag.String("ingestions-db", "", "Explicit path for ingestions.db")
	blocksDB  = flag.String("blocks-db", "", "Explicit path for blocks.db")

	dryRun  = flag.Bool("dry-run", false, "Only count source rows and print plan; do not write")
	force   = flag.Bool("force", false, "Allow writing even if target tables already contain rows")
	verbose = flag.Bool("verbose", false, "Verbose logging of every row (warning: very noisy for large DBs)")

	tables = flag.String("tables", "all", "Comma-separated list of table groups to migrate: all,mcp,keys,ingest,blocks")
	verify = flag.Bool("verify", true, "After migration, re-count rows in targets and compare")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Postgres → SQLite migration utility for Stargate\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Required:\n")
		fmt.Fprintf(os.Stderr, "  --pg-dsn string   Postgres connection string\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s --pg-dsn 'postgres://user:pass@host:5432/stargate?sslmode=disable'\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --pg-dsn \"$DATABASE_URL\" --target-dir ./data/sqlite --dry-run\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --pg-dsn \"$STARGATE_PG_DSN\" --force --tables mcp,keys\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "After successful migration:\n")
		fmt.Fprintf(os.Stderr, "  1. Set STARGATE_STORAGE=sqlite (or unset STARGATE_PG_DSN)\n")
		fmt.Fprintf(os.Stderr, "  2. Restart backend\n")
		fmt.Fprintf(os.Stderr, "  3. (Optional) rsync data/blocks data/uploads data/ipfs* to new host\n")
	}
	flag.Parse()

	if *pgDSN == "" {
		if env := os.Getenv("STARGATE_PG_DSN"); env != "" {
			*pgDSN = env
		} else if env := os.Getenv("DATABASE_URL"); env != "" {
			*pgDSN = env
		}
	}
	if *pgDSN == "" {
		log.Fatal("ERROR: --pg-dsn is required (or set STARGATE_PG_DSN / DATABASE_URL)")
	}

	groups := parseTableGroups(*tables)
	if len(groups) == 0 {
		log.Fatal("ERROR: --tables must include at least one of: all,mcp,keys,ingest,blocks")
	}

	// Resolve target paths
	resolvePaths()

	log.Printf("=== Stargate Postgres → SQLite Migration ===")
	log.Printf("Source PG: %s", redactDSN(*pgDSN))
	log.Printf("Target dir: %s", *targetDir)
	log.Printf("Groups: %v  dry-run=%v force=%v", groups, *dryRun, *force)

	// Connect to source
	srcDB, err := sql.Open("pgx", *pgDSN)
	if err != nil {
		log.Fatalf("failed to open postgres: %v", err)
	}
	defer srcDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srcDB.PingContext(ctx); err != nil {
		log.Fatalf("cannot connect to postgres: %v", err)
	}
	log.Printf("Connected to Postgres source")

	// Collect source counts (always do this for visibility)
	counts := collectSourceCounts(srcDB)
	printSourceCounts(counts)

	if *dryRun {
		log.Printf("DRY RUN complete. No data written.")
		return
	}

	// Safety check on targets
	if !*force {
		checkTargetsEmptyOrDie(groups)
	}

	start := time.Now()

	// Migrate in order (FK dependencies for MCP)
	if groups["mcp"] || groups["all"] {
		migrateMCP(srcDB)
	}
	if groups["keys"] || groups["all"] {
		migrateAPIKeys(srcDB)
	}
	if groups["ingest"] || groups["all"] {
		migrateIngestion(srcDB)
	}
	if groups["blocks"] || groups["all"] {
		migrateBlockScans(srcDB)
	}

	elapsed := time.Since(start)
	log.Printf("Migration completed in %s", elapsed.Truncate(time.Millisecond))

	if *verify {
		verifyMigration(counts)
	}

	log.Printf("\n✅ SUCCESS")
	log.Printf("Next steps:")
	log.Printf("  export STARGATE_STORAGE=sqlite")
	log.Printf("  # or simply unset STARGATE_PG_DSN / DATABASE_URL")
	log.Printf("  # Then restart the backend. It will use the new SQLite files.")
}

func parseTableGroups(s string) map[string]bool {
	g := map[string]bool{}
	for _, p := range strings.Split(strings.ToLower(s), ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		g[p] = true
	}
	if g["all"] {
		g["mcp"] = true
		g["keys"] = true
		g["ingest"] = true
		g["blocks"] = true
	}
	return g
}

var (
	pathMCP    string
	pathKeys   string
	pathIngest string
	pathBlocks string
)

func resolvePaths() {
	if *mcpDBPath != "" {
		pathMCP = *mcpDBPath
	} else {
		pathMCP = filepath.Join(*targetDir, "mcp.db")
	}
	if *apiKeysDB != "" {
		pathKeys = *apiKeysDB
	} else {
		pathKeys = filepath.Join(*targetDir, "api_keys.db")
	}
	if *ingestDB != "" {
		pathIngest = *ingestDB
	} else {
		pathIngest = filepath.Join(*targetDir, "ingestions.db")
	}
	if *blocksDB != "" {
		pathBlocks = *blocksDB
	} else {
		pathBlocks = filepath.Join(*targetDir, "blocks.db")
	}

	// Ensure dirs
	for _, p := range []string{pathMCP, pathKeys, pathIngest, pathBlocks} {
		if dir := filepath.Dir(p); dir != "" && dir != "." {
			_ = os.MkdirAll(dir, 0755)
		}
	}
}

func redactDSN(dsn string) string {
	if idx := strings.Index(dsn, "@"); idx > 0 {
		if start := strings.LastIndex(dsn[:idx], ":"); start > 0 {
			return dsn[:start+1] + "****" + dsn[idx:]
		}
	}
	return dsn
}

// --- Source counting ---

type sourceCounts struct {
	Contracts   int64
	Tasks       int64
	Claims      int64
	Submissions int64
	Proposals   int64
	Escort      int64
	APIKeys     int64
	Ingestions  int64
	IngestUpd   int64
	BlockScans  int64
}

func collectSourceCounts(db *sql.DB) sourceCounts {
	var c sourceCounts
	_ = db.QueryRow(`SELECT COUNT(*) FROM mcp_contracts`).Scan(&c.Contracts)
	_ = db.QueryRow(`SELECT COUNT(*) FROM mcp_tasks`).Scan(&c.Tasks)
	_ = db.QueryRow(`SELECT COUNT(*) FROM mcp_claims`).Scan(&c.Claims)
	_ = db.QueryRow(`SELECT COUNT(*) FROM mcp_submissions`).Scan(&c.Submissions)
	_ = db.QueryRow(`SELECT COUNT(*) FROM mcp_proposals`).Scan(&c.Proposals)
	_ = db.QueryRow(`SELECT COUNT(*) FROM mcp_escort_status`).Scan(&c.Escort)
	_ = db.QueryRow(`SELECT COUNT(*) FROM api_keys`).Scan(&c.APIKeys)
	_ = db.QueryRow(`SELECT COUNT(*) FROM starlight_ingestions`).Scan(&c.Ingestions)
	_ = db.QueryRow(`SELECT COUNT(*) FROM starlight_ingest_updates`).Scan(&c.IngestUpd)
	_ = db.QueryRow(`SELECT COUNT(*) FROM block_scans`).Scan(&c.BlockScans)
	return c
}

func printSourceCounts(c sourceCounts) {
	log.Printf("Source row counts:")
	log.Printf("  mcp_contracts:     %d", c.Contracts)
	log.Printf("  mcp_tasks:         %d", c.Tasks)
	log.Printf("  mcp_claims:        %d", c.Claims)
	log.Printf("  mcp_submissions:   %d", c.Submissions)
	log.Printf("  mcp_proposals:     %d", c.Proposals)
	log.Printf("  mcp_escort_status: %d", c.Escort)
	log.Printf("  api_keys:          %d", c.APIKeys)
	log.Printf("  starlight_ingestions:      %d", c.Ingestions)
	log.Printf("  starlight_ingest_updates:  %d", c.IngestUpd)
	log.Printf("  block_scans:       %d", c.BlockScans)
}

func checkTargetsEmptyOrDie(groups map[string]bool) {
	checks := []struct {
		name string
		path string
		tbl  string
		grp  string
	}{
		{"mcp", pathMCP, "mcp_contracts", "mcp"},
		{"api_keys", pathKeys, "api_keys", "keys"},
		{"ingestions", pathIngest, "starlight_ingestions", "ingest"},
		{"blocks", pathBlocks, "block_scans", "blocks"},
	}
	for _, ch := range checks {
		if !(groups[ch.grp] || groups["all"]) {
			continue
		}
		if _, err := os.Stat(ch.path); os.IsNotExist(err) {
			continue // fresh file = ok
		}
		db, err := sql.Open("sqlite", ch.path+"?_foreign_keys=on")
		if err != nil {
			continue
		}
		defer db.Close()
		var cnt int64
		_ = db.QueryRow("SELECT COUNT(*) FROM " + ch.tbl).Scan(&cnt)
		if cnt > 0 {
			log.Fatalf("ERROR: target %s (%s) already has %d rows. Use --force to overwrite.", ch.name, ch.path, cnt)
		}
	}
}

// --- MCP migration ---

func migrateMCP(src *sql.DB) {
	log.Printf("→ Migrating MCP tables (smart contracts, tasks, proposals...)")

	db, err := sql.Open("sqlite", pathMCP+"?_foreign_keys=on")
	if err != nil {
		log.Fatalf("open target mcp.db: %v", err)
	}
	defer db.Close()

	// Init schema using the project's canonical definition (DRY)
	schema := scstore.GetMCPSchema("sqlite")
	if _, err := db.Exec(schema); err != nil {
		log.Fatalf("init mcp sqlite schema: %v", err)
	}

	tx, err := db.Begin()
	if err != nil {
		log.Fatalf("begin tx: %v", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	// Order matters for FKs: contracts → tasks → claims → submissions
	// proposals and escort_status are independent
	migrateTable(src, tx, "mcp_contracts",
		`SELECT contract_id, title, total_budget_sats, goals_count, available_tasks_count,
		        status, skills, stego_image_url, confirmed_block_height, confirmed_at,
		        created_at, metadata
		 FROM mcp_contracts`,
		`INSERT OR REPLACE INTO mcp_contracts
		 (contract_id, title, total_budget_sats, goals_count, available_tasks_count,
		  status, skills, stego_image_url, confirmed_block_height, confirmed_at,
		  created_at, metadata)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		12, adaptMCPContractRow)

	migrateTable(src, tx, "mcp_tasks",
		`SELECT task_id, contract_id, goal_id, title, description, budget_sats, skills,
		        status, claimed_by, claimed_at, claim_expires_at, difficulty,
		        estimated_hours, requirements, merkle_proof
		 FROM mcp_tasks`,
		`INSERT OR REPLACE INTO mcp_tasks
		 (task_id, contract_id, goal_id, title, description, budget_sats, skills,
		  status, claimed_by, claimed_at, claim_expires_at, difficulty,
		  estimated_hours, requirements, merkle_proof)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		15, adaptMCPTaskRow)

	migrateTable(src, tx, "mcp_claims",
		`SELECT claim_id, task_id, ai_identifier, status, expires_at, created_at FROM mcp_claims`,
		`INSERT OR REPLACE INTO mcp_claims
		 (claim_id, task_id, ai_identifier, status, expires_at, created_at)
		 VALUES (?,?,?,?,?,?)`,
		6, adaptGenericRow) // times + simple fields

	migrateTable(src, tx, "mcp_submissions",
		`SELECT submission_id, claim_id, task_id, status, deliverables, completion_proof,
		        rejection_reason, rejection_type, rejected_at, created_at
		 FROM mcp_submissions`,
		`INSERT OR REPLACE INTO mcp_submissions
		 (submission_id, claim_id, task_id, status, deliverables, completion_proof,
		  rejection_reason, rejection_type, rejected_at, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		10, adaptGenericRow)

	migrateTable(src, tx, "mcp_proposals",
		`SELECT id, title, description_md, visible_pixel_hash, budget_sats, status,
		        metadata, created_at FROM mcp_proposals`,
		`INSERT OR REPLACE INTO mcp_proposals
		 (id, title, description_md, visible_pixel_hash, budget_sats, status,
		  metadata, created_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		8, adaptGenericRow)

	migrateTable(src, tx, "mcp_escort_status",
		`SELECT task_id, proof_status, last_checked, payload FROM mcp_escort_status`,
		`INSERT OR REPLACE INTO mcp_escort_status
		 (task_id, proof_status, last_checked, payload)
		 VALUES (?,?,?,?)`,
		4, adaptGenericRow)

	if err := tx.Commit(); err != nil {
		log.Fatalf("commit mcp migration: %v", err)
	}
	tx = nil
	log.Printf("   MCP migration complete → %s", pathMCP)
}

func adaptMCPContractRow(row []interface{}) []interface{} {
	out := make([]interface{}, 12)
	out[0] = row[0] // contract_id
	out[1] = row[1] // title
	out[2] = toInt64(row[2])
	out[3] = toInt64(row[3])
	out[4] = toInt64(row[4])
	out[5] = row[5] // status
	out[6] = adaptSkills(row[6])
	out[7] = row[7] // stego url
	out[8] = toInt64(row[8])
	out[9] = adaptTime(row[9])
	out[10] = adaptTime(row[10])
	out[11] = adaptJSON(row[11])
	return out
}

func adaptMCPTaskRow(row []interface{}) []interface{} {
	out := make([]interface{}, 15)
	out[0] = row[0]
	out[1] = row[1]
	out[2] = row[2]
	out[3] = row[3]
	out[4] = row[4]
	out[5] = toInt64(row[5])
	out[6] = adaptSkills(row[6])
	out[7] = row[7]
	out[8] = row[8]
	out[9] = adaptTime(row[9])
	out[10] = adaptTime(row[10])
	out[11] = row[11]
	out[12] = toInt64(row[12])
	out[13] = adaptJSON(row[13])
	out[14] = adaptJSON(row[14])
	return out
}

func adaptGenericRow(row []interface{}) []interface{} {
	out := make([]interface{}, len(row))
	for i := range row {
		out[i] = adaptValue(row[i])
	}
	return out
}

// adaptValue is the universal type converter used by generic table adapters.
// It tries (in order):
//  1. time.Time / timestamp strings → RFC3339 text (for SQLite TEXT timestamps)
//  2. JSONB []byte or maps → JSON text string
//  3. plain strings, numbers, etc. passed through (or lightly stringified)
func adaptValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	// 1. Times first (most specific)
	if t, ok := v.(time.Time); ok {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	if s, ok := v.(string); ok {
		// Could be a pre-formatted timestamp or plain text. Leave for now.
		// (adaptTime would have caught real time.Time from driver)
		return s
	}
	if b, ok := v.([]byte); ok {
		sb := string(b)
		// Heuristic: if it smells like JSON object/array, treat as JSON
		trim := strings.TrimSpace(sb)
		if len(trim) > 0 && (trim[0] == '{' || trim[0] == '[') {
			// Validate
			var tmp interface{}
			if json.Unmarshal(b, &tmp) == nil {
				return sb
			}
		}
		// Otherwise just text (e.g. a status, id, or timestamp string from PG)
		return sb
	}
	// Maps / slices from some drivers or jsonb decoded
	if _, ok := v.(map[string]interface{}); ok {
		b, _ := json.Marshal(v)
		return string(b)
	}
	if _, ok := v.([]interface{}); ok {
		b, _ := json.Marshal(v)
		return string(b)
	}
	// Numbers and everything else
	return v
}

// --- API Keys ---

func migrateAPIKeys(src *sql.DB) {
	log.Printf("→ Migrating api_keys table")

	db, err := sql.Open("sqlite", pathKeys+"?_foreign_keys=on")
	if err != nil {
		log.Fatalf("open target api_keys.db: %v", err)
	}
	defer db.Close()

	schema := `
CREATE TABLE IF NOT EXISTS api_keys (
  key_hash TEXT PRIMARY KEY,
  email TEXT,
  wallet_address TEXT,
  source TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_api_keys_wallet ON api_keys(wallet_address);
`
	if _, err := db.Exec(schema); err != nil {
		log.Fatalf("init api_keys schema: %v", err)
	}

	tx, _ := db.Begin()
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	migrateTable(src, tx, "api_keys",
		`SELECT key_hash, email, wallet_address, source, created_at FROM api_keys`,
		`INSERT OR REPLACE INTO api_keys (key_hash, email, wallet_address, source, created_at)
		 VALUES (?,?,?,?,?)`,
		5, func(row []interface{}) []interface{} {
			return []interface{}{
				row[0],
				row[1],
				row[2],
				row[3],
				adaptTime(row[4]),
			}
		})

	_ = tx.Commit()
	tx = nil
	log.Printf("   api_keys migration complete → %s", pathKeys)
}

// --- Ingestion ---

func migrateIngestion(src *sql.DB) {
	log.Printf("→ Migrating ingestion tables (ingestions + updates)")

	db, err := sql.Open("sqlite", pathIngest+"?_foreign_keys=on")
	if err != nil {
		log.Fatalf("open target ingestions.db: %v", err)
	}
	defer db.Close()

	// Use the exact schema the IngestionService would create for sqlite
	schema := `
CREATE TABLE IF NOT EXISTS starlight_ingestions (
  id TEXT PRIMARY KEY,
  filename TEXT NOT NULL,
  method TEXT NOT NULL,
  message_length INT NOT NULL,
  image_base64 TEXT NOT NULL,
  metadata TEXT DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS starlight_ingest_updates (
  id INTEGER PRIMARY KEY,  -- we preserve the original ids
  ingestion_id TEXT,
  visible_pixel_hash TEXT,
  proposal_id TEXT,
  payload TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INT NOT NULL DEFAULT 0,
  last_error TEXT,
  next_retry_at TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS starlight_ingest_updates_status_idx
  ON starlight_ingest_updates (status, next_retry_at);
`
	if _, err := db.Exec(schema); err != nil {
		log.Fatalf("init ingestion schema: %v", err)
	}

	tx, _ := db.Begin()
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	migrateTable(src, tx, "starlight_ingestions",
		`SELECT id, filename, method, message_length, image_base64, metadata, status, created_at
		 FROM starlight_ingestions`,
		`INSERT OR REPLACE INTO starlight_ingestions
		 (id, filename, method, message_length, image_base64, metadata, status, created_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		8, func(row []interface{}) []interface{} {
			return []interface{}{
				row[0], row[1], row[2], toInt64(row[3]), row[4],
				adaptJSON(row[5]), row[6], adaptTime(row[7]),
			}
		})

	// For updates, preserve the integer PK ids explicitly
	migrateTable(src, tx, "starlight_ingest_updates",
		`SELECT id, ingestion_id, visible_pixel_hash, proposal_id, payload, status,
		        attempts, last_error, next_retry_at, created_at, updated_at
		 FROM starlight_ingest_updates`,
		`INSERT OR REPLACE INTO starlight_ingest_updates
		 (id, ingestion_id, visible_pixel_hash, proposal_id, payload, status,
		  attempts, last_error, next_retry_at, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		11, func(row []interface{}) []interface{} {
			return []interface{}{
				toInt64(row[0]), row[1], row[2], row[3], adaptJSON(row[4]),
				row[5], toInt64(row[6]), row[7], adaptTime(row[8]),
				adaptTime(row[9]), adaptTime(row[10]),
			}
		})

	_ = tx.Commit()
	tx = nil
	log.Printf("   ingestion migration complete → %s", pathIngest)
}

// --- Block scans (data layer metadata) ---

func migrateBlockScans(src *sql.DB) {
	log.Printf("→ Migrating block_scans (block metadata index)")

	db, err := sql.Open("sqlite", pathBlocks+"?_journal_mode=WAL")
	if err != nil {
		log.Fatalf("open target blocks.db: %v", err)
	}
	defer db.Close()

	schema := `
CREATE TABLE IF NOT EXISTS block_scans (
    block_height   INTEGER PRIMARY KEY,
    block_hash     TEXT NOT NULL,
    scanned_at     TEXT NOT NULL DEFAULT (datetime('now')),
    payload        TEXT NOT NULL,
    stego_detected INTEGER NOT NULL DEFAULT 0,
    images_scanned INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_block_scans_hash ON block_scans(block_hash);
CREATE INDEX IF NOT EXISTS idx_block_scans_scanned ON block_scans(scanned_at);
`
	if _, err := db.Exec(schema); err != nil {
		log.Fatalf("init blocks schema: %v", err)
	}

	tx, _ := db.Begin()
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	migrateTable(src, tx, "block_scans",
		`SELECT block_height, block_hash, scanned_at, payload, stego_detected, images_scanned
		 FROM block_scans`,
		`INSERT OR REPLACE INTO block_scans
		 (block_height, block_hash, scanned_at, payload, stego_detected, images_scanned)
		 VALUES (?,?,?,?,?,?)`,
		6, func(row []interface{}) []interface{} {
			return []interface{}{
				toInt64(row[0]),
				row[1],
				adaptTime(row[2]),
				adaptJSON(row[3]), // payload is JSONB in PG → TEXT in SQLite
				toInt64(row[4]),
				toInt64(row[5]),
			}
		})

	_ = tx.Commit()
	tx = nil
	log.Printf("   block_scans migration complete → %s", pathBlocks)
}

// --- Generic row migrator ---

func migrateTable(src *sql.DB, dstTx *sql.Tx, tableName, selectSQL, insertSQL string, numCols int, adapter func([]interface{}) []interface{}) {
	rows, err := src.Query(selectSQL)
	if err != nil {
		log.Printf("  WARNING: skipping %s (query failed: %v)", tableName, err)
		return
	}
	defer rows.Close()

	inserted := int64(0)
	skipped := int64(0)

	for rows.Next() {
		scanArgs := make([]interface{}, numCols)
		for i := 0; i < numCols; i++ {
			var v interface{}
			scanArgs[i] = &v
		}
		if err := rows.Scan(scanArgs...); err != nil {
			log.Printf("  scan error on %s: %v", tableName, err)
			skipped++
			continue
		}

		// deref the pointers we used for scanning
		row := make([]interface{}, numCols)
		for i := range scanArgs {
			row[i] = *(scanArgs[i].(*interface{}))
		}

		bound := adapter(row)

		if *verbose {
			log.Printf("  [%s] %v", tableName, summarizeRow(bound))
		}

		if _, err := dstTx.Exec(insertSQL, bound...); err != nil {
			log.Printf("  insert error on %s: %v (row: %v)", tableName, err, summarizeRow(bound))
			skipped++
			continue
		}
		inserted++
	}

	if err := rows.Err(); err != nil {
		log.Printf("  rows error on %s: %v", tableName, err)
	}

	log.Printf("   %-22s %6d inserted, %4d skipped", tableName, inserted, skipped)
}

func summarizeRow(row []interface{}) string {
	parts := make([]string, len(row))
	for i, v := range row {
		switch x := v.(type) {
		case string:
			if len(x) > 40 {
				parts[i] = x[:37] + "..."
			} else {
				parts[i] = x
			}
		case []byte:
			s := string(x)
			if len(s) > 40 {
				parts[i] = s[:37] + "..."
			} else {
				parts[i] = s
			}
		default:
			parts[i] = fmt.Sprintf("%v", v)
		}
	}
	return strings.Join(parts, "|")
}

// --- Type adapters (shared) ---

func adaptSkills(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case []string:
		return strings.Join(x, ",")
	case []interface{}:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				parts = append(parts, s)
			} else if e != nil {
				parts = append(parts, fmt.Sprintf("%v", e))
			}
		}
		return strings.Join(parts, ",")
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func adaptJSON(v interface{}) string {
	if v == nil {
		return "{}"
	}
	switch x := v.(type) {
	case []byte:
		// Already JSON text or JSONB binary — store as text
		s := string(x)
		if s == "" {
			return "{}"
		}
		// Validate it looks like JSON (best effort)
		var tmp interface{}
		if json.Unmarshal(x, &tmp) == nil {
			return s
		}
		// Not valid JSON, wrap as string? Rare. Fall through.
		return s
	case string:
		if x == "" {
			return "{}"
		}
		return x
	case map[string]interface{}, []interface{}:
		b, _ := json.Marshal(x)
		return string(b)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

func adaptTime(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case time.Time:
		if x.IsZero() {
			return ""
		}
		return x.UTC().Format(time.RFC3339)
	case string:
		return x // already string from PG or previous
	case []byte:
		return string(x)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case int64:
		return x
	case int32:
		return int64(x)
	case int:
		return int64(x)
	case float64:
		return int64(x)
	case []byte:
		var n int64
		fmt.Sscanf(string(x), "%d", &n)
		return n
	case string:
		var n int64
		fmt.Sscanf(x, "%d", &n)
		return n
	default:
		return 0
	}
}

// --- Verification ---

func verifyMigration(sourceCounts sourceCounts) {
	log.Printf("→ Verifying row counts in targets...")

	type check struct {
		name  string
		path  string
		query string
		want  int64
	}
	checks := []check{
		{"mcp_contracts", pathMCP, "SELECT COUNT(*) FROM mcp_contracts", sourceCounts.Contracts},
		{"mcp_tasks", pathMCP, "SELECT COUNT(*) FROM mcp_tasks", sourceCounts.Tasks},
		{"api_keys", pathKeys, "SELECT COUNT(*) FROM api_keys", sourceCounts.APIKeys},
		{"starlight_ingestions", pathIngest, "SELECT COUNT(*) FROM starlight_ingestions", sourceCounts.Ingestions},
		{"block_scans", pathBlocks, "SELECT COUNT(*) FROM block_scans", sourceCounts.BlockScans},
	}

	allOK := true
	for _, ch := range checks {
		if ch.want == 0 {
			continue // nothing to verify
		}
		db, err := sql.Open("sqlite", ch.path+"?_foreign_keys=on")
		if err != nil {
			log.Printf("  VERIFY FAIL %s: cannot open: %v", ch.name, err)
			allOK = false
			continue
		}
		var got int64
		if err := db.QueryRow(ch.query).Scan(&got); err != nil {
			log.Printf("  VERIFY FAIL %s: query error: %v", ch.name, err)
			allOK = false
		} else if got != ch.want {
			log.Printf("  VERIFY MISMATCH %s: source=%d target=%d", ch.name, ch.want, got)
			allOK = false
		} else {
			log.Printf("  OK %s: %d rows", ch.name, got)
		}
		db.Close()
	}

	if allOK {
		log.Printf("✅ All verified counts match (or were empty)")
	} else {
		log.Printf("⚠️  Some verification mismatches — data may still be usable. Review above.")
	}
}
