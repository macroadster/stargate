package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	scmiddleware "stargate-backend/app/smart_contract"
	"stargate-backend/middleware"
	"stargate-backend/services"
	"stargate-backend/starlight"
	"stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"
)

func newActionLimitMCP(t *testing.T, lim *middleware.ActionLimiter, validator auth.APIKeyValidator) (*HTTPMCPServer, *scmiddleware.Server) {
	t.Helper()
	store := scstore.NewMemoryStore(72 * time.Hour)
	if validator == nil {
		validator = walletValidator{wallet: "bc1qmcp-limit"}
	}
	mcpServer := NewHTTPMCPServer(store, validator, nil, &services.IngestionService{}, &starlight.ScannerManager{}, nil, auth.NewChallengeStore(10*time.Minute))
	mcpServer.SetActionLimiter(lim)
	rest := scmiddleware.NewServer(store, validator, nil)
	rest.SetActionLimiter(lim)
	return mcpServer, rest
}

func mcpCall(server *HTTPMCPServer, tool, apiKey string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(MCPRequest{Tool: tool, Arguments: map[string]interface{}{}})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp/call", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		r.Header.Set("X-API-Key", apiKey)
	}
	server.handleToolCall(w, r)
	return w
}

func restPost(rest *scmiddleware.Server, path, apiKey string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	rest.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestMCPActionLimitAndSharedRESTKey(t *testing.T) {
	lim := middleware.NewActionLimiter(map[middleware.Action]middleware.ActionLimit{
		middleware.ActionClaim:  {RPS: 0.001, Burst: 1},
		middleware.ActionSubmit: {RPS: 0.001, Burst: 1},
		middleware.ActionReview: {RPS: 0.001, Burst: 1},
	})
	key := "shared-key"
	server, rest := newActionLimitMCP(t, lim, walletValidator{wallet: "bc1qshared"})

	first := mcpCall(server, "claim_task", key)
	if first.Code == http.StatusTooManyRequests {
		t.Fatalf("first MCP claim should pass: %s", first.Body.String())
	}

	// Same API key on REST must share the claim bucket.
	shared := restPost(rest, "/api/smart_contract/tasks/t1/claim", key)
	if shared.Code != http.StatusTooManyRequests {
		t.Fatalf("REST claim with same key should 429, got %d: %s", shared.Code, shared.Body.String())
	}

	// Different API key has its own bucket.
	other := mcpCall(server, "claim_task", "other-key")
	if other.Code == http.StatusTooManyRequests {
		t.Fatalf("different key should not share bucket: %s", other.Body.String())
	}

	// Submit is independent of claim.
	submit := mcpCall(server, "submit_work", key)
	if submit.Code == http.StatusTooManyRequests {
		t.Fatalf("submit should not share claim bucket: %s", submit.Body.String())
	}
}

func TestMCPInProcessStoreCallDoesNotDoubleCount(t *testing.T) {
	lim := middleware.NewActionLimiter(map[middleware.Action]middleware.ActionLimit{
		middleware.ActionClaim: {RPS: 0.001, Burst: 2},
	})
	server, _ := newActionLimitMCP(t, lim, walletValidator{wallet: "bc1qnodouble"})
	key := "no-double"

	first := mcpCall(server, "claim_task", key)
	if first.Code == http.StatusTooManyRequests {
		t.Fatalf("HTTP claim_task should count once: %s", first.Body.String())
	}

	// In-process store path (callToolDirect) must not consume another token.
	_, err := server.callToolDirect(context.Background(), "claim_task", map[string]interface{}{"task_id": "missing"}, key, nil)
	if err == nil {
		t.Fatal("expected task-not-found from store, not a rate-limit error")
	}
	if strings.Contains(err.Error(), "Rate limit") || strings.Contains(err.Error(), "RATE_LIMIT") {
		t.Fatalf("in-process store call was rate limited: %v", err)
	}

	second := mcpCall(server, "claim_task", key)
	if second.Code == http.StatusTooManyRequests {
		t.Fatalf("second HTTP call should still be inside burst=2 if store did not double-count: %s", second.Body.String())
	}

	third := mcpCall(server, "claim_task", key)
	if third.Code != http.StatusTooManyRequests {
		t.Fatalf("third HTTP claim should 429, got %d: %s", third.Code, third.Body.String())
	}
	if third.Header().Get("Retry-After") == "" {
		t.Fatal("MCP 429 missing Retry-After")
	}
	var resp MCPResponse
	if err := json.Unmarshal(third.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ErrorCode != ErrCodeRateLimited {
		t.Fatalf("expected RATE_LIMITED, got %+v", resp)
	}
}

func TestMCPDiscoverAdvertisesSharedLimits(t *testing.T) {
	lim := middleware.NewActionLimiter(nil)
	server, _ := newActionLimitMCP(t, lim, allowAllValidator{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/mcp/discover", nil)
	server.handleDiscover(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("discover %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"enabled":true`) && !strings.Contains(body, `"enabled": true`) {
		t.Fatalf("expected enabled rate_limits: %s", body)
	}
}
