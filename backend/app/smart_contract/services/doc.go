// Package services holds smart-contract application services extracted from HTTP handlers.
// These types depend on storage/smart_contract (and optionally ingestion), not on net/http.
// Wire them from app/smart_contract.Server; keep server_* files as thin adapters.
//
// Policy for claim / submit / submission review cascades is centralized in
// storage/smart_contract (DecideClaim, DecideSubmit, DecideSubmissionStatusUpdate)
// so Memory / SQLite / Postgres cannot drift. Services here add HTTP/MCP
// orchestration (auth, events) on top of that shared policy.
package services
