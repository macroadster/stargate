package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	scmiddleware "stargate-backend/app/smart_contract"
	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	auth "stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"
)

// These drive the MCP tool surface end to end. The authz helper tests passed
// while these handlers still called store.UpdateSubmissionStatus directly, so
// the review rule and its side effects have to be asserted through handleToolCall.

const (
	surfaceWishHash    = "b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8"
	surfaceCreatorKey  = "surface-key-creator"
	surfaceStrangerKey = "surface-key-stranger"
	surfaceCreatorWlt  = "bc1qsurfacecreator"
	surfaceStrangerWlt = "bc1qsurfacestranger"
)

type surfaceKeyStore struct{ keys map[string]auth.APIKey }

func (m *surfaceKeyStore) Validate(key string) bool {
	_, ok := m.keys[key]
	return ok
}

func (m *surfaceKeyStore) Get(key string) (auth.APIKey, bool) {
	k, ok := m.keys[key]
	return k, ok
}

// surfaceFixture builds an MCP server whose ingestion record names
// surfaceCreatorWlt as the wish creator.
func surfaceFixture(t *testing.T) (*HTTPMCPServer, scstore.Store) {
	t.Helper()

	ingestSvc, err := services.NewIngestionService(filepath.Join(t.TempDir(), "ingest.db"))
	if err != nil {
		t.Fatalf("ingestion service: %v", err)
	}
	if err := ingestSvc.Create(services.IngestionRecord{
		ID:       surfaceWishHash,
		Filename: "wish.png",
		Method:   "test",
		Status:   "completed",
		Metadata: map[string]interface{}{"creator_wallet": surfaceCreatorWlt},
	}); err != nil {
		t.Fatalf("seed ingestion: %v", err)
	}

	keys := &surfaceKeyStore{keys: map[string]auth.APIKey{
		surfaceCreatorKey:  {Key: surfaceCreatorKey, Wallet: surfaceCreatorWlt},
		surfaceStrangerKey: {Key: surfaceStrangerKey, Wallet: surfaceStrangerWlt},
	}}

	store := scstore.NewMemoryStore(72 * 60 * 60)
	srv := NewHTTPMCPServer(store, keys, nil, ingestSvc, nil, nil, auth.NewChallengeStore(10*time.Minute))
	return srv, store
}

// callTool posts a tool invocation the way an MCP client would.
func callTool(t *testing.T, srv *HTTPMCPServer, apiKey, tool string, args map[string]interface{}) MCPResponse {
	t.Helper()
	body, err := json.Marshal(MCPRequest{Tool: tool, Arguments: args})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/mcp/call", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		r.Header.Set("X-API-Key", apiKey)
	}
	w := httptest.NewRecorder()
	srv.handleToolCall(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 with a payload, got %d: %s", w.Code, w.Body.String())
	}
	var resp MCPResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

// seedSurfaceSubmission creates wish-<hash> with one task, a claim on it, and a
// submission. When taskOnSubmission is false the submission carries only its
// ClaimID, which is the shape TaskID being omitempty allows.
func seedSurfaceSubmission(t *testing.T, store scstore.Store, submissionID, taskID, claimID string, taskOnSubmission bool) string {
	t.Helper()
	ctx := context.Background()

	contractID := "wish-" + surfaceWishHash
	if err := store.UpsertContractWithTasks(ctx,
		smart_contract.Contract{ContractID: contractID, Title: "wish", Status: "active"},
		[]smart_contract.Task{{TaskID: taskID, ContractID: contractID, Title: "task", Status: "submitted"}},
	); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if err := store.SyncClaim(ctx, smart_contract.Claim{
		ClaimID:   claimID,
		TaskID:    taskID,
		Status:    "submitted",
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed claim: %v", err)
	}
	sub := smart_contract.Submission{
		SubmissionID: submissionID,
		ClaimID:      claimID,
		Status:       "pending_review",
		CreatedAt:    time.Now(),
	}
	if taskOnSubmission {
		sub.TaskID = taskID
	}
	if err := store.SyncSubmission(ctx, sub); err != nil {
		t.Fatalf("seed submission: %v", err)
	}
	return contractID
}

func TestMCPApproveSubmissionRejectsStranger(t *testing.T) {
	srv, store := surfaceFixture(t)
	seedSurfaceSubmission(t, store, "mcp-sub-1", "mcp-task-1", "mcp-claim-1", true)

	resp := callTool(t, srv, surfaceStrangerKey, "approve_submission", map[string]interface{}{
		"submission_id": "mcp-sub-1",
	})

	if resp.Success {
		t.Fatal("expected a wallet that does not own the wish to be refused")
	}
	if resp.ErrorCode != ErrCodeUnauthorized {
		t.Fatalf("error_code = %q, want %q (body: %s)", resp.ErrorCode, ErrCodeUnauthorized, resp.Error)
	}
	if !strings.Contains(resp.Error, "does not match wish creator") {
		t.Fatalf("expected a creator mismatch message, got: %s", resp.Error)
	}

	sub, err := store.GetSubmission(context.Background(), "mcp-sub-1")
	if err != nil {
		t.Fatalf("get submission: %v", err)
	}
	if sub.Status == "approved" {
		t.Fatal("submission was approved despite the unauthorized response")
	}
}

func TestMCPRejectSubmissionRejectsStranger(t *testing.T) {
	srv, store := surfaceFixture(t)
	seedSurfaceSubmission(t, store, "mcp-sub-2", "mcp-task-2", "mcp-claim-2", true)

	resp := callTool(t, srv, surfaceStrangerKey, "reject_submission", map[string]interface{}{
		"submission_id":  "mcp-sub-2",
		"notes":          "no",
		"rejection_type": "quality",
	})

	if resp.Success || resp.ErrorCode != ErrCodeUnauthorized {
		t.Fatalf("expected reject to be refused for a stranger, got success=%v code=%q err=%s",
			resp.Success, resp.ErrorCode, resp.Error)
	}
}

// A replicated wish records no creator_wallet, so review must be refused rather
// than allowed through the proposal-compatibility branch.
func TestMCPApproveSubmissionDeniesReplicatedWish(t *testing.T) {
	srv, store := surfaceFixture(t)
	ctx := context.Background()

	const replicated = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	if err := srv.ingestionSvc.Create(services.IngestionRecord{
		ID:       replicated,
		Filename: "stego.png",
		Method:   "test",
		Status:   "verified",
		Metadata: map[string]interface{}{"stego_replicated": true},
	}); err != nil {
		t.Fatalf("seed replicated ingestion: %v", err)
	}

	contractID := "wish-" + replicated
	if err := store.UpsertContractWithTasks(ctx,
		smart_contract.Contract{ContractID: contractID, Title: "replicated", Status: "active"},
		[]smart_contract.Task{{TaskID: "rep-task", ContractID: contractID, Title: "task", Status: "submitted"}},
	); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if err := store.SyncSubmission(ctx, smart_contract.Submission{
		SubmissionID: "mcp-sub-replicated",
		TaskID:       "rep-task",
		Status:       "pending_review",
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("seed submission: %v", err)
	}

	resp := callTool(t, srv, surfaceCreatorKey, "approve_submission", map[string]interface{}{
		"submission_id": "mcp-sub-replicated",
	})

	if resp.Success {
		t.Fatal("expected approval on a replicated wish to be refused")
	}
	if !strings.Contains(resp.Error, "replicated from another node") {
		t.Fatalf("expected the replica cause to be named, got: %s", resp.Error)
	}
}

// Routing through SubmissionService must not turn a missing submission into an
// internal error; the tool should still report not-found.
func TestMCPApproveSubmissionMissingReportsNotFound(t *testing.T) {
	srv, _ := surfaceFixture(t)

	resp := callTool(t, srv, surfaceCreatorKey, "approve_submission", map[string]interface{}{
		"submission_id": "does-not-exist",
	})

	if resp.Success {
		t.Fatal("expected a missing submission to fail")
	}
	if resp.ErrorCode != ErrCodeNotFound {
		t.Fatalf("error_code = %q, want %q (body: %s)", resp.ErrorCode, ErrCodeNotFound, resp.Error)
	}
}

// The MCP SubmissionService was built with a nil EventRecorder, whose emit is
// nil-guarded, so review events were dropped with no error and no log. This is
// the test that would have caught it.
func TestMCPApproveSubmissionEmitsReviewEvent(t *testing.T) {
	srv, store := surfaceFixture(t)
	seedSurfaceSubmission(t, store, "mcp-sub-event", "mcp-task-event", "mcp-claim-event", true)

	// RegisterEventSink appends to a process-global slice with no way to
	// unregister, so this sink outlives the test. Safe only because nothing else
	// in this package asserts on sinks; do not copy this into another test.
	events := make(chan smart_contract.Event, 8)
	scmiddleware.RegisterEventSink(func(evt smart_contract.Event) {
		select {
		case events <- evt:
		default:
		}
	})

	resp := callTool(t, srv, surfaceCreatorKey, "approve_submission", map[string]interface{}{
		"submission_id": "mcp-sub-event",
	})
	if !resp.Success {
		t.Fatalf("expected the wish creator to be allowed, got: %s", resp.Error)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case evt := <-events:
			if evt.Type == "review" && evt.EntityID == "mcp-sub-event" {
				// End to end through the real gate, not a stub: the recorded actor
				// is the wallet authorization accepted, not the old "reviewer".
				if evt.Actor != surfaceCreatorWlt {
					t.Fatalf("event actor = %q, want the approving wallet %q", evt.Actor, surfaceCreatorWlt)
				}
				return
			}
		case <-deadline:
			t.Fatal("no review event reached the sink; MCP is dropping review events")
		}
	}
}

// maybeResolveRework re-reads the submission and used to skip on an empty
// TaskID, so a ClaimID-only approval never resolved rework even though the
// task itself was cascaded via the claim.
func TestMCPApproveSubmissionResolvesReworkViaClaim(t *testing.T) {
	srv, store := surfaceFixture(t)
	ctx := context.Background()
	contractID := seedSurfaceSubmission(t, store, "mcp-sub-rework", "mcp-task-rework", "mcp-claim-rework", false)

	req, err := store.CreateContractReworkRequest(ctx, contractID, surfaceCreatorWlt, "please redo")
	if err != nil {
		t.Fatalf("seed rework request: %v", err)
	}

	resp := callTool(t, srv, surfaceCreatorKey, "approve_submission", map[string]interface{}{
		"submission_id": "mcp-sub-rework",
	})
	if !resp.Success {
		t.Fatalf("expected approval to succeed for a claim-only submission, got: %s", resp.Error)
	}

	reqs, err := store.GetContractReworkRequests(ctx, contractID)
	if err != nil {
		t.Fatalf("get rework requests: %v", err)
	}
	for _, got := range reqs {
		if got.RequestID == req.RequestID && got.Status == "open" {
			t.Fatal("rework request still open: approval did not resolve rework for a claim-only submission")
		}
	}
}
