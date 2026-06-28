package smart_contract

// Compatibility re-exports for the MCP/Store interface and errors.
// The authoritative definitions now live in:
//   stargate-backend/storage/smart_contract/{store.go, errors.go}
//
// This allows a gradual migration. All existing imports of
// "stargate-backend/middleware/smart_contract" (as scmiddleware or smartstore)
// continue to work unchanged. New code should prefer importing directly from
// the storage package.

import scstore "stargate-backend/storage/smart_contract"

// Store is the MCP persistence interface (single source of truth in storage/).
type Store = scstore.Store

// Err is the error type used by store implementations.
type Err = scstore.Err

// Re-exported sentinel errors for callers that do errors.Is(...) against them.
var (
	ErrTaskNotFound    = scstore.ErrTaskNotFound
	ErrClaimNotFound   = scstore.ErrClaimNotFound
	ErrTaskTaken       = scstore.ErrTaskTaken
	ErrTaskUnavailable = scstore.ErrTaskUnavailable
)
