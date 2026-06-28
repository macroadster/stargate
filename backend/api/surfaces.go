package api

import (
	"encoding/json"
	"net/http"
	"sync"
)

// Surface ownership for the single-binary API.
//
// Primary surfaces (prefer these for new code):
//   - Agents / smart-contract lifecycle: /api/smart_contract/*  (REST)
//     MCP tools under /mcp/* are a thin tool shim over the same store/handlers.
//   - Block / inscription browse data:   /api/data/*
//   - Bitcoin scan/extract:              /bitcoin/v1/*
//
// Legacy aliases remain registered for UI/clients but send Deprecation + Link headers
// pointing at the primary path.

// RouteAlias describes a legacy path that forwards to a primary handler.
type RouteAlias struct {
	Path        string `json:"path"`
	Primary     string `json:"primary"`
	Surface     string `json:"surface"`
	Description string `json:"description,omitempty"`
}

// SurfaceCatalog is the machine-readable map of primary vs legacy routes.
type SurfaceCatalog struct {
	Primary  map[string]string `json:"primary"`
	Aliases  []RouteAlias      `json:"aliases"`
	MCPNote  string            `json:"mcp_note"`
	ToolREST map[string]string `json:"mcp_tool_rest_backing,omitempty"`
}

// DefaultPrimary surfaces for discovery docs and /api/surfaces.
var DefaultPrimary = map[string]string{
	"smart_contract": "/api/smart_contract/*",
	"block_data":     "/api/data/*",
	"bitcoin_scan":   "/bitcoin/v1/*",
	"mcp_tools":      "/mcp/tools + /mcp/call (shim over smart_contract store)",
	"auth":           "/api/auth/*",
	"health":         "/api/health",
}

// DefaultAliases lists historical paths that are no longer registered (3bk.8).
// Kept here for GET /api/surfaces documentation / client migration.
// Active legacy routes: none — use primary surfaces only.
var DefaultAliases = []RouteAlias{
	{Path: "/api/smart-contracts", Primary: "/api/open-contracts", Surface: "ui_contracts", Description: "Retired; use /api/open-contracts"},
	{Path: "/api/contracts-confirmed", Primary: "/api/open-contracts", Surface: "ui_contracts", Description: "Retired; use /api/open-contracts"},
	{Path: "/api/data/contracts-with-pagination", Primary: "/api/open-contracts", Surface: "ui_contracts", Description: "Retired; use /api/open-contracts"},
	{Path: "/api/blocks", Primary: "/api/data/blocks", Surface: "block_data", Description: "Retired; use /api/data/blocks"},
	{Path: "/api/block-images", Primary: "/api/data/block-images", Surface: "block_data", Description: "Retired; use /api/data/block-images"},
	{Path: "/api/contract-stego", Primary: "/api/smart_contract/contracts", Surface: "smart_contract", Description: "Retired; use /api/smart_contract/contracts/{id}"},
	{Path: "/api/contract-stego/create", Primary: "/api/smart_contract/proposals", Surface: "smart_contract", Description: "Retired; use POST /api/smart_contract/proposals"},
}

// DefaultToolREST maps MCP tool names to their REST backing paths (docs/parity).
var DefaultToolREST = map[string]string{
	"list_contracts":               "GET /api/smart_contract/contracts",
	"get_contract":                 "GET /api/smart_contract/contracts/{id}",
	"get_open_contracts":           "GET /api/open-contracts",
	"list_proposals":               "GET /api/smart_contract/proposals",
	"get_proposal":                 "GET /api/smart_contract/proposals/{id}",
	"create_proposal":              "POST /api/smart_contract/proposals",
	"approve_proposal":             "POST /api/smart_contract/proposals/{id}/approve",
	"list_tasks":                   "GET /api/smart_contract/tasks",
	"get_task":                     "GET /api/smart_contract/tasks/{id}",
	"claim_task":                   "POST /api/smart_contract/tasks/{id}/claim",
	"submit_work":                  "POST /api/smart_contract/claims/{id}/submit",
	"list_submissions":             "GET /api/smart_contract/submissions",
	"approve_submission":           "POST /api/smart_contract/submissions/{id}/review",
	"reject_submission":            "POST /api/smart_contract/submissions/{id}/review",
	"list_events":                  "GET /api/smart_contract/events",
	"build_psbt":                   "POST /api/smart_contract/contracts/{id}/psbt",
	"scan_image":                   "POST /bitcoin/v1/scan/image",
	"scan_transaction":             "POST /bitcoin/v1/scan/transaction",
	"get_scanner_info":             "GET /bitcoin/v1/info",
	"get_auth_challenge":           "GET /api/auth/challenge",
	"verify_auth_challenge":        "POST /api/auth/verify",
}

// Catalog returns the full surface catalog.
func Catalog() SurfaceCatalog {
	return SurfaceCatalog{
		Primary:  DefaultPrimary,
		Aliases:  DefaultAliases,
		MCPNote:  "MCP HTTP tools (/mcp/call, /mcp/tools) share the smart_contract store with /api/smart_contract/*; prefer REST for new integrations, MCP for agent tool use.",
		ToolREST: DefaultToolREST,
	}
}

// HandleSurfaces serves GET /api/surfaces.
func HandleSurfaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(Catalog())
}

// WithDeprecation wraps a handler to emit Deprecation and Link headers for legacy aliases.
func WithDeprecation(primary string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Deprecation", "true")
		w.Header().Set("Link", "<"+primary+">; rel=\"successor-version\"")
		w.Header().Set("X-Stargate-Primary-Path", primary)
		next(w, r)
	}
}

// RegisterAliases registers legacy paths that share a handler, with deprecation headers.
func RegisterAliases(mux *http.ServeMux, primary string, handler http.HandlerFunc, aliases ...string) {
	for _, alias := range aliases {
		mux.HandleFunc(alias, WithDeprecation(primary, handler))
	}
}

// RegisterAliasHandlers registers each alias with the same handler + deprecation.
func RegisterAliasHandlers(mux *http.ServeMux, handler http.HandlerFunc, aliases []RouteAlias) {
	for _, a := range aliases {
		primary := a.Primary
		mux.HandleFunc(a.Path, WithDeprecation(primary, handler))
	}
}

var deprecationLogOnce sync.Map

// LogDeprecationOnce logs the first use of a legacy path (optional observability).
func LogDeprecationOnce(path, primary string, logf func(string, ...interface{})) {
	if _, loaded := deprecationLogOnce.LoadOrStore(path, true); !loaded && logf != nil {
		logf("api surface: legacy path %s used (primary: %s)", path, primary)
	}
}
