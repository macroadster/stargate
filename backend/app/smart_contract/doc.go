// Package smart_contract implements the smart-contract application layer:
// HTTP route handlers (thin), background sync (ingestion, stego, funding),
// and orchestration over storage/smart_contract and core/smart_contract.
//
// Prefer putting new business logic in package services (sibling) or in
// core/smart_contract. Server methods should decode HTTP, call a service,
// and encode the response.
//
// Import path: stargate-backend/app/smart_contract
// (formerly stargate-backend/middleware/smart_contract — renamed for package-boundary clarity.)
//
// Persistence interfaces live in stargate-backend/storage/smart_contract.
// Domain types live in stargate-backend/core/smart_contract.
package smart_contract
