package smart_contract

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SchemaManager handles database schema migrations
type SchemaManager struct {
	pool *pgxpool.Pool
}

// NewSchemaManager creates a new schema manager
func NewSchemaManager(pool *pgxpool.Pool) *SchemaManager {
	return &SchemaManager{pool: pool}
}

// Initialize creates the database schema
func (m *SchemaManager) Initialize(ctx context.Context) error {
	schema := m.getSchema()
	_, err := m.pool.Exec(ctx, schema)
	return err
}

// getSchema returns the complete database schema
func (m *SchemaManager) getSchema() string {
	return `
-- Contracts table
CREATE TABLE IF NOT EXISTS mcp_contracts (
  contract_id TEXT PRIMARY KEY,
  title TEXT,
  total_budget_sats BIGINT,
  goals_count INT,
  available_tasks_count INT,
  status TEXT,
  skills TEXT[]
);

-- Tasks table
CREATE TABLE IF NOT EXISTS mcp_tasks (
  task_id TEXT PRIMARY KEY,
  contract_id TEXT,
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

-- Claims table
CREATE TABLE IF NOT EXISTS mcp_claims (
  claim_id TEXT PRIMARY KEY,
  task_id TEXT,
  ai_identifier TEXT,
  status TEXT,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ
);

-- Submissions table
CREATE TABLE IF NOT EXISTS mcp_submissions (
  submission_id TEXT PRIMARY KEY,
  claim_id TEXT,
  status TEXT,
  deliverables JSONB,
  completion_proof JSONB,
  rejection_reason TEXT,
  rejection_type TEXT,
  rejected_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ
);

-- Proposals table
CREATE TABLE IF NOT EXISTS mcp_proposals (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  description_md TEXT NOT NULL,
  visible_pixel_hash TEXT,
  budget_sats BIGINT DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending',
  metadata JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Escort status table
CREATE TABLE IF NOT EXISTS mcp_escort_status (
  task_id TEXT PRIMARY KEY,
  proof_status TEXT,
  last_checked TIMESTAMPTZ,
  payload JSONB
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_mcp_proposals_status ON mcp_proposals(status);
CREATE INDEX IF NOT EXISTS idx_mcp_tasks_contract_status ON mcp_tasks(contract_id, status);

-- Column additions for backwards compatibility
ALTER TABLE mcp_submissions ADD COLUMN IF NOT EXISTS rejection_reason TEXT;
ALTER TABLE mcp_submissions ADD COLUMN IF NOT EXISTS rejection_type TEXT;
ALTER TABLE mcp_submissions ADD COLUMN IF NOT EXISTS rejected_at TIMESTAMPTZ;
`
}

// SeedFixtures adds initial seed data
func (m *SchemaManager) SeedFixtures(ctx context.Context) error {
	// This would contain the seed data logic from the original pg_store.go
	// For now, returning nil to avoid duplication
	return nil
}
