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

// getSchema returns the complete database schema for Postgres.
// Delegates to the single source of truth in schema.go.
func (m *SchemaManager) getSchema() string {
	// The shared postgresMCPSchema() already includes the ALTER ADD COLUMN IF NOT EXISTS
	// statements and the DO $$ foreign-key block that are Postgres-specific.
	return postgresMCPSchema()
}

// SeedFixtures adds initial seed data
func (m *SchemaManager) SeedFixtures(ctx context.Context) error {
	// This would contain the seed data logic from the original pg_store.go
	// For now, returning nil to avoid duplication
	return nil
}
