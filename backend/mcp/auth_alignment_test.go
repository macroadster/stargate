package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	scmiddleware "stargate-backend/app/smart_contract"
	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	"stargate-backend/starlight"
	"stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"
)

// Register/login, REST authWrap, MCP tool call, and MCP session must share one
// bearer path and the same api_keys store (including STARGATE_API_KEY seed).
func TestOneBearerPathAPIAndMCP(t *testing.T) {
	t.Setenv("STARGATE_API_KEY", "seed-bearer-key")
	t.Setenv("STARLIGHT_DONATION_ADDRESS", "tb1qseedwallet")

	keys := auth.NewAPIKeyStore()
	keys.SeedEnvironmentVariables()
	if !keys.Validate("seed-bearer-key") {
		t.Fatal("seed must land in the shared store")
	}

	store := scstore.NewMemoryStore(72 * time.Hour)
	if err := store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID: "c-align", Title: "t", Status: "open",
	}, nil); err != nil {
		t.Fatal(err)
	}

	rest := scmiddleware.NewServer(store, keys, nil)
	mcpServer := NewHTTPMCPServer(store, keys, keys, &services.IngestionService{}, &starlight.ScannerManager{}, nil, auth.NewChallengeStore(10*time.Minute))

	mux := http.NewServeMux()
	rest.RegisterRoutes(mux)
	mcpServer.RegisterRoutes(mux)
	handler := mux

	// REST: Bearer seed key
	req := httptest.NewRequest(http.MethodGet, "/api/smart_contract/contracts", nil)
	req.Header.Set("Authorization", "Bearer seed-bearer-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("REST bearer: %d %s", w.Code, w.Body.String())
	}

	// REST: missing key is 401 (same as middleware.APIAuth)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/smart_contract/contracts", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("REST missing key: %d %s", w.Code, w.Body.String())
	}

	// MCP initialize with Bearer binds the session
	initBody, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]interface{}{},
	})
	initReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(initBody))
	initReq.Header.Set("Authorization", "Bearer seed-bearer-key")
	initReq.Header.Set("Content-Type", "application/json")
	initW := httptest.NewRecorder()
	handler.ServeHTTP(initW, initReq)
	if initW.Code != http.StatusOK {
		t.Fatalf("MCP initialize: %d %s", initW.Code, initW.Body.String())
	}
	sessionID := initW.Header().Get("MCP-Session-Id")
	if sessionID == "" {
		t.Fatal("expected MCP-Session-Id")
	}

	// Follow-up tool call with session only (no Authorization) must use bound seed key
	callBody, _ := json.Marshal(MCPRequest{Tool: "create_proposal", Arguments: map[string]interface{}{
		"title": "from-session",
	}})
	callReq := httptest.NewRequest(http.MethodPost, "/mcp/call", bytes.NewReader(callBody))
	callReq.Header.Set("Content-Type", "application/json")
	callReq.Header.Set("MCP-Session-Id", sessionID)
	callW := httptest.NewRecorder()
	handler.ServeHTTP(callW, callReq)
	if callW.Code == http.StatusUnauthorized ||
		bytes.Contains(callW.Body.Bytes(), []byte("API_KEY_REQUIRED")) ||
		bytes.Contains(callW.Body.Bytes(), []byte("UNAUTHORIZED")) {
		t.Fatalf("session-bound seed key rejected: %d %s", callW.Code, callW.Body.String())
	}

	// Direct MCP Bearer (no session) also works
	direct, _ := json.Marshal(MCPRequest{Tool: "list_contracts"})
	dreq := httptest.NewRequest(http.MethodPost, "/mcp/call", bytes.NewReader(direct))
	dreq.Header.Set("Authorization", "Bearer seed-bearer-key")
	dreq.Header.Set("Content-Type", "application/json")
	dw := httptest.NewRecorder()
	handler.ServeHTTP(dw, dreq)
	if dw.Code != http.StatusOK {
		t.Fatalf("MCP bearer list: %d %s", dw.Code, dw.Body.String())
	}

	// Protected tool without key or session must fail auth
	noAuth, _ := json.Marshal(MCPRequest{Tool: "create_proposal", Arguments: map[string]interface{}{"title": "x"}})
	nreq := httptest.NewRequest(http.MethodPost, "/mcp/call", bytes.NewReader(noAuth))
	nreq.Header.Set("Content-Type", "application/json")
	nw := httptest.NewRecorder()
	handler.ServeHTTP(nw, nreq)
	if !bytes.Contains(nw.Body.Bytes(), []byte("UNAUTHORIZED")) &&
		!bytes.Contains(nw.Body.Bytes(), []byte("API_KEY_REQUIRED")) &&
		!bytes.Contains(nw.Body.Bytes(), []byte("API key required")) {
		t.Fatalf("expected MCP auth failure without key, got %d %s", nw.Code, nw.Body.String())
	}
}
