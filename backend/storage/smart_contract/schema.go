package smart_contract

import "strings"

// Table names (single source of truth)
const (
	TableContracts     = "mcp_contracts"
	TableTasks         = "mcp_tasks"
	TableClaims        = "mcp_claims"
	TableSubmissions   = "mcp_submissions"
	TableProposals     = "mcp_proposals"
	TableEscortStatus  = "mcp_escort_status"
)

// GetMCPSchema returns the CREATE TABLE statements for the MCP/smart-contract
// schema for the requested dialect.
//
// Supported dialects: "postgres", "pg", "postgresql", "sqlite", "sqlite3"
//
// This eliminates the previous duplication between schema_manager.go (PG)
// and sqlite_store.go:initSchema(). Both now call this function.
func GetMCPSchema(dialect string) string {
	d := strings.ToLower(dialect)
	isSQLite := strings.Contains(d, "sqlite")

	if isSQLite {
		return sqliteMCPSchema()
	}
	return postgresMCPSchema()
}

// postgresMCPSchema returns the PostgreSQL (JSONB + arrays + TIMESTAMPTZ) version.
func postgresMCPSchema() string {
	return `
-- Contracts
CREATE TABLE IF NOT EXISTS ` + TableContracts + ` (
  contract_id TEXT PRIMARY KEY,
  title TEXT,
  total_budget_sats BIGINT,
  goals_count INT,
  available_tasks_count INT,
  status TEXT,
  skills TEXT[],
  stego_image_url TEXT,
  confirmed_block_height INTEGER,
  confirmed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Tasks
CREATE TABLE IF NOT EXISTS ` + TableTasks + ` (
  task_id TEXT PRIMARY KEY,
  contract_id TEXT REFERENCES ` + TableContracts + `(contract_id) ON DELETE CASCADE,
  goal_id TEXT,
  title TEXT,
  description TEXT,
  budget_sats BIGINT,
  skills TEXT[],
  status TEXT,
  claimed_by TEXT,
  claimed_at TIMESTAMPTZ,
  claim_expires_at TIMESTAMPTZ,
  difficulty TEXT,
  estimated_hours INT,
  requirements JSONB,
  merkle_proof JSONB
);

-- Claims
CREATE TABLE IF NOT EXISTS ` + TableClaims + ` (
  claim_id TEXT PRIMARY KEY,
  task_id TEXT REFERENCES ` + TableTasks + `(task_id) ON DELETE CASCADE,
  ai_identifier TEXT,
  status TEXT,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ
);

-- Submissions
CREATE TABLE IF NOT EXISTS ` + TableSubmissions + ` (
  submission_id TEXT PRIMARY KEY,
  claim_id TEXT REFERENCES ` + TableClaims + `(claim_id) ON DELETE CASCADE,
  task_id TEXT,
  status TEXT NOT NULL,
  deliverables JSONB,
  completion_proof JSONB,
  rejection_reason TEXT,
  rejection_type TEXT,
  rejected_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Proposals
CREATE TABLE IF NOT EXISTS ` + TableProposals + ` (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  description_md TEXT NOT NULL,
  visible_pixel_hash TEXT,
  budget_sats BIGINT DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending',
  metadata JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Escort status
CREATE TABLE IF NOT EXISTS ` + TableEscortStatus + ` (
  task_id TEXT PRIMARY KEY,
  proof_status TEXT,
  last_checked TIMESTAMPTZ,
  payload JSONB
);

-- Performance indexes (Postgres)
CREATE INDEX IF NOT EXISTS idx_mcp_contracts_confirmed_height ON ` + TableContracts + `(confirmed_block_height DESC);
CREATE INDEX IF NOT EXISTS idx_mcp_contracts_confirmed_at ON ` + TableContracts + `(confirmed_at DESC);
CREATE INDEX IF NOT EXISTS idx_mcp_proposals_status ON ` + TableProposals + `(status);
CREATE INDEX IF NOT EXISTS idx_mcp_tasks_contract_status ON ` + TableTasks + `(contract_id, status);
`
}

// sqliteMCPSchema returns the SQLite-friendly version (TEXT for JSON/timestamps,
// comma-separated skills, no arrays, datetime('now')).
func sqliteMCPSchema() string {
	return `
CREATE TABLE IF NOT EXISTS ` + TableContracts + ` (
  contract_id TEXT PRIMARY KEY,
  title TEXT,
  total_budget_sats INTEGER,
  goals_count INTEGER,
  available_tasks_count INTEGER,
  status TEXT,
  skills TEXT,
  stego_image_url TEXT,
  confirmed_block_height INTEGER,
  confirmed_at TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  metadata TEXT DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_mcp_contracts_confirmed_height ON ` + TableContracts + `(confirmed_block_height DESC);

CREATE TABLE IF NOT EXISTS ` + TableTasks + ` (
  task_id TEXT PRIMARY KEY,
  contract_id TEXT,
  goal_id TEXT,
  title TEXT,
  description TEXT,
  budget_sats INTEGER,
  skills TEXT,
  status TEXT,
  claimed_by TEXT,
  claimed_at TEXT,
  claim_expires_at TEXT,
  difficulty TEXT,
  estimated_hours INTEGER,
  requirements TEXT,
  merkle_proof TEXT,
  FOREIGN KEY (contract_id) REFERENCES ` + TableContracts + `(contract_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS ` + TableClaims + ` (
  claim_id TEXT PRIMARY KEY,
  task_id TEXT,
  ai_identifier TEXT,
  status TEXT,
  expires_at TEXT,
  created_at TEXT,
  FOREIGN KEY (task_id) REFERENCES ` + TableTasks + `(task_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS ` + TableSubmissions + ` (
  submission_id TEXT PRIMARY KEY,
  claim_id TEXT,
  task_id TEXT,
  status TEXT NOT NULL,
  deliverables TEXT,
  completion_proof TEXT,
  rejection_reason TEXT,
  rejection_type TEXT,
  rejected_at TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  FOREIGN KEY (claim_id) REFERENCES ` + TableClaims + `(claim_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_mcp_submissions_claim_id ON ` + TableSubmissions + `(claim_id);
CREATE INDEX IF NOT EXISTS idx_mcp_submissions_status ON ` + TableSubmissions + `(status);

CREATE TABLE IF NOT EXISTS ` + TableProposals + ` (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  description_md TEXT NOT NULL,
  visible_pixel_hash TEXT,
  budget_sats INTEGER DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending',
  metadata TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_mcp_proposals_status ON ` + TableProposals + `(status);

CREATE TABLE IF NOT EXISTS ` + TableEscortStatus + ` (
  task_id TEXT PRIMARY KEY,
  proof_status TEXT,
  last_checked TEXT,
  payload TEXT
);
`
}
