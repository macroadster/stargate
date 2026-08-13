package smart_contract

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stargate-backend/middleware"
	auth "stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"
)

func TestRESTActionLimitClaimSubmitReview(t *testing.T) {
	store := scstore.NewMemoryStore(72 * 60 * 60)
	apiKey := "rest-limit-key"
	keys := &mockAPIKeyStore{keys: map[string]auth.APIKey{
		apiKey: {Key: apiKey, Wallet: "bc1qrestlimit"},
	}}
	server := NewServer(store, keys, nil)
	lim := middleware.NewActionLimiter(map[middleware.Action]middleware.ActionLimit{
		middleware.ActionClaim:  {RPS: 0.001, Burst: 1},
		middleware.ActionSubmit: {RPS: 0.001, Burst: 1},
		middleware.ActionReview: {RPS: 0.001, Burst: 1},
	})
	server.SetActionLimiter(lim)

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	post := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", apiKey)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	first := post("/api/smart_contract/tasks/t1/claim")
	if first.Code == http.StatusTooManyRequests {
		t.Fatalf("first claim should not be limited: %s", first.Body.String())
	}
	if first.Header().Get("X-RateLimit-Limit") != "1" {
		t.Fatalf("expected limit header on first claim, got %q", first.Header().Get("X-RateLimit-Limit"))
	}

	second := post("/api/smart_contract/tasks/t1/claim")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second claim should 429, got %d: %s", second.Code, second.Body.String())
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}

	// Submit is a separate bucket.
	submit := post("/api/smart_contract/claims/c1/submit")
	if submit.Code == http.StatusTooManyRequests {
		t.Fatalf("first submit should not share claim bucket: %s", submit.Body.String())
	}
	if post("/api/smart_contract/claims/c1/submit").Code != http.StatusTooManyRequests {
		t.Fatal("second submit should 429")
	}

	review := post("/api/smart_contract/submissions/s1/review")
	if review.Code == http.StatusTooManyRequests {
		t.Fatalf("first review should not share other buckets: %s", review.Body.String())
	}
	if post("/api/smart_contract/submissions/s1/review").Code != http.StatusTooManyRequests {
		t.Fatal("second review should 429")
	}

	// Reads are not limited.
	req := httptest.NewRequest(http.MethodGet, "/api/smart_contract/tasks", nil)
	req.Header.Set("X-API-Key", apiKey)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("GET tasks should not be action-limited: %s", rec.Body.String())
	}
}

func TestRESTDiscoverAdvertisesSharedLimits(t *testing.T) {
	server := NewServer(scstore.NewMemoryStore(72*60*60), nil, nil)
	server.SetActionLimiter(middleware.NewActionLimiter(nil))
	req := httptest.NewRequest(http.MethodGet, "/api/smart_contract/discover", nil)
	rec := httptest.NewRecorder()
	server.handleDiscover(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("discover: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"enabled":true`) && !strings.Contains(body, `"enabled": true`) {
		t.Fatalf("expected enabled rate_limits, got %s", body)
	}
	if !strings.Contains(body, "/mcp/call") {
		t.Fatalf("expected shared surface in discover: %s", body)
	}
}
