package smart_contract

// Compatibility re-exports for the Store interface and errors.
// The authoritative definitions live in:
//   stargate-backend/storage/smart_contract/{store.go, errors.go}
//
// Prefer importing storage/smart_contract directly in new code.
// This package (app/smart_contract) is the application layer, not storage.

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
