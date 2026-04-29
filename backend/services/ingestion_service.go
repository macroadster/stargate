package services

// Compatibility shim (Phase 3 of storage centralization).
//
// IngestionService has been moved to the authoritative location:
//
//     "stargate-backend/storage/ingestion"
//
// This file provides type aliases + constructor forwarding so the rest
// of the codebase (main, container, middleware, handlers, mcp, core, tests)
// continues to compile with zero import changes. The shim will be deleted
// in Phase 7 cleanup.

import ingestion "stargate-backend/storage/ingestion"

// Re-exported public types (identical)
type (
	IngestionService = ingestion.IngestionService
	IngestionRecord  = ingestion.IngestionRecord
	IngestUpdateRow  = ingestion.IngestUpdateRow
)

// NewIngestionService forwards to the canonical storage implementation.
func NewIngestionService(dsn string) (*IngestionService, error) {
	return ingestion.NewIngestionService(dsn)
}
