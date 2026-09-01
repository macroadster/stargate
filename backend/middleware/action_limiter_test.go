package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientKeyPriority(t *testing.T) {
	if got := ClientKey("k", "w", "claim_task"); got != "apikey:k" {
		t.Fatalf("api key should win, got %q", got)
	}
	if got := ClientKey("", "w", "claim_task"); got != "wallet:w" {
		t.Fatalf("wallet fallback, got %q", got)
	}
	if got := ClientKey("", "", "claim_task"); got != "tool:claim_task" {
		t.Fatalf("tool fallback, got %q", got)
	}
	if got := ClientKey(" ", " ", " "); got != "anon" {
		t.Fatalf("blank identity, got %q", got)
	}
}

func TestActionForToolAndRESTClassify(t *testing.T) {
	if a, ok := ActionForTool("claim_task"); !ok || a != ActionClaim {
		t.Fatalf("claim_task: %v %v", a, ok)
	}
	if a, ok := ActionForTool("submit_work"); !ok || a != ActionSubmit {
		t.Fatalf("submit_work: %v %v", a, ok)
	}
	if a, ok := ActionForTool("approve_submission"); !ok || a != ActionReview {
		t.Fatalf("approve_submission: %v %v", a, ok)
	}
	if a, ok := ActionForTool("reject_submission"); !ok || a != ActionReview {
		t.Fatalf("reject_submission: %v %v", a, ok)
	}
	if a, ok := ActionForTool("review_submission"); !ok || a != ActionReview {
		t.Fatalf("review_submission: %v %v", a, ok)
	}
	if _, ok := ActionForTool("list_tasks"); ok {
		t.Fatal("list_tasks should not be action-limited")
	}

	a, tool, ok := ClassifySmartContractAction(http.MethodPost, "/api/smart_contract/tasks/t1/claim")
	if !ok || a != ActionClaim || tool != "claim_task" {
		t.Fatalf("rest claim: %v %s %v", a, tool, ok)
	}
	a, tool, ok = ClassifySmartContractAction(http.MethodPost, "/api/smart_contract/claims/c1/submit")
	if !ok || a != ActionSubmit || tool != "submit_work" {
		t.Fatalf("rest submit: %v %s %v", a, tool, ok)
	}
	a, tool, ok = ClassifySmartContractAction(http.MethodPost, "/api/smart_contract/submissions/s1/review")
	if !ok || a != ActionReview || tool != "review_submission" {
		t.Fatalf("rest review: %v %s %v", a, tool, ok)
	}
	if _, _, ok := ClassifySmartContractAction(http.MethodGet, "/api/smart_contract/tasks/t1/claim"); ok {
		t.Fatal("GET must not classify")
	}
	if _, _, ok := ClassifySmartContractAction(http.MethodPost, "/api/smart_contract/tasks"); ok {
		t.Fatal("list must not classify")
	}
}

func TestActionLimiterSharedKeySeparateActions(t *testing.T) {
	lim := NewActionLimiter(map[Action]ActionLimit{
		ActionClaim:  {RPS: 0.001, Burst: 2},
		ActionSubmit: {RPS: 0.001, Burst: 2},
	})
	key := ClientKey("same-key", "wallet", "claim_task")

	if !lim.Allow(key, ActionClaim).Allowed || !lim.Allow(key, ActionClaim).Allowed {
		t.Fatal("burst 2 claim should pass")
	}
	if lim.Allow(key, ActionClaim).Allowed {
		t.Fatal("third claim should deny")
	}
	if !lim.Allow(key, ActionSubmit).Allowed {
		t.Fatal("submit must not share the claim bucket")
	}
}

func TestActionLimiterRefill(t *testing.T) {
	lim := NewActionLimiter(map[Action]ActionLimit{
		ActionClaim: {RPS: 10, Burst: 1},
	})
	now := time.Now()
	lim.now = func() time.Time { return now }

	key := ClientKey("k", "", "claim_task")
	if !lim.Allow(key, ActionClaim).Allowed {
		t.Fatal("first should pass")
	}
	if lim.Allow(key, ActionClaim).Allowed {
		t.Fatal("second should deny before refill")
	}
	now = now.Add(200 * time.Millisecond)
	if !lim.Allow(key, ActionClaim).Allowed {
		t.Fatal("should refill after 200ms at 10 rps")
	}
}

func TestNilLimiterAllows(t *testing.T) {
	var lim *ActionLimiter
	if !lim.Allow("k", ActionClaim).Allowed {
		t.Fatal("nil limiter must allow")
	}
	if lim.Discover()["enabled"] != false {
		t.Fatal("nil discover should be disabled")
	}
}

func TestWriteRateLimitHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteRateLimitHeaders(rec, AllowResult{
		Allowed:    false,
		Limit:      2,
		Remaining:  0,
		ResetUnix:  1700000000,
		RetryAfter: 1500 * time.Millisecond,
	})
	if rec.Header().Get("X-RateLimit-Limit") != "2" {
		t.Fatalf("limit header: %s", rec.Header().Get("X-RateLimit-Limit"))
	}
	if rec.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Fatalf("remaining header: %s", rec.Header().Get("X-RateLimit-Remaining"))
	}
	if rec.Header().Get("X-RateLimit-Reset") != "1700000000" {
		t.Fatalf("reset header: %s", rec.Header().Get("X-RateLimit-Reset"))
	}
	if rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("retry-after: %s", rec.Header().Get("Retry-After"))
	}
}

func TestActionLimitContext(t *testing.T) {
	if ActionLimitApplied(context.Background()) {
		t.Fatal("fresh ctx")
	}
	ctx := MarkActionLimited(context.Background())
	if !ActionLimitApplied(ctx) {
		t.Fatal("marked ctx")
	}
}
