// Package services holds smart-contract application services extracted from HTTP handlers.
// These types depend on storage/smart_contract (and optionally ingestion), not on net/http.
// Wire them from app/smart_contract.Server; keep server_* files as thin adapters.
package services
