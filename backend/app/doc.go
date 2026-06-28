// Package app is the application / use-case layer of the Stargate backend.
//
// Layering (outer → inner):
//
//	handlers, mcp, api     — transport (HTTP adapters)
//	app/*                  — application services & orchestration (this package tree)
//	core/*                 — pure domain types and domain services
//	storage/*              — persistence adapters
//	middleware             — HTTP middleware only (CORS, auth, recovery) — not business logic
//
// Historically, smart-contract application logic lived under middleware/smart_contract
// despite owning business rules. It now lives in app/smart_contract. Do not add
// domain logic to package middleware or to thin server_*.go handlers — put it in
// app/smart_contract/services or core/smart_contract.
package app
