// Package middleware provides HTTP middleware only (CORS, recovery, security headers,
// API-key auth wrappers, timeouts). It must not own business/domain logic.
//
// Application orchestration for smart contracts lives in app/smart_contract.
// Persistence lives in storage/*. Domain types live in core/*.
package middleware
