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

	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	"stargate-backend/starlight"
	"stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"
)

type allowAllValidator struct{}

func (a allowAllValidator) Validate(string) bool { return true }
func (a allowAllValidator) Get(key string) (auth.APIKey, bool) {
	return auth.APIKey{Key: key}, true
}

type walletValidator struct {
	wallet string
}

func (w walletValidator) Validate(key string) bool { return strings.TrimSpace(key) != "" }
func (w walletValidator) Get(key string) (auth.APIKey, bool) {
	return auth.APIKey{Key: key, Wallet: w.wallet}, true
}

func TestHTTPMCPServer(t *testing.T) {
	// Use memory store for testing
	store := scstore.NewMemoryStore(72 * time.Hour)
	ingestionSvc := &services.IngestionService{}  // nil for memory mode
	scannerManager := &starlight.ScannerManager{} // mock scanner manager
	server := NewHTTPMCPServer(store, allowAllValidator{}, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

	// Test list_contracts
	t.Run("list_contracts", func(t *testing.T) {
		req := MCPRequest{
			Tool: "list_contracts",
		}
		body, _ := json.Marshal(req)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		server.handleToolCall(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp MCPResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
	})

	// Test list_proposals
	t.Run("list_proposals", func(t *testing.T) {
		req := MCPRequest{
			Tool: "list_proposals",
		}
		body, _ := json.Marshal(req)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		server.handleToolCall(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp MCPResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
	})
}

func TestClaimTaskUsesAPIKeyWallet(t *testing.T) {
	store := scstore.NewMemoryStore(72 * time.Hour)
	ingestionSvc := &services.IngestionService{}
	scannerManager := &starlight.ScannerManager{}
	apiKey := "test-api-key"
	wallet := "tb1qwallettest000000000000000000000000000000000"
	server := NewHTTPMCPServer(store, walletValidator{wallet: wallet}, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

	contract := smart_contract.Contract{
		ContractID:          "contract-claim-wallet",
		Title:               "Wallet Binding Contract",
		TotalBudgetSats:     1000,
		GoalsCount:          1,
		AvailableTasksCount: 1,
		Status:              "active",
	}
	task := smart_contract.Task{
		TaskID:      "contract-claim-wallet-task-1",
		ContractID:  "contract-claim-wallet",
		Title:       "Wallet Binding Task",
		Description: "Test task for wallet binding",
		BudgetSats:  1000,
		Status:      "available",
	}
	if err := store.UpsertContractWithTasks(context.Background(), contract, []smart_contract.Task{task}); err != nil {
		t.Fatalf("failed to seed tasks: %v", err)
	}

	req := MCPRequest{
		Tool: "claim_task",
		Arguments: map[string]interface{}{
			"task_id": task.TaskID,
		},
	}
	body, _ := json.Marshal(req)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-API-Key", apiKey)

	server.handleToolCall(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated, err := store.GetTask(task.TaskID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if updated.ContractorWallet != wallet {
		t.Fatalf("expected contractor wallet %s, got %s", wallet, updated.ContractorWallet)
	}
	if updated.MerkleProof == nil || updated.MerkleProof.ContractorWallet != wallet {
		t.Fatalf("expected merkle proof wallet %s, got %#v", wallet, updated.MerkleProof)
	}
}

func TestProposalCreationRequiresWish(t *testing.T) {
	// Use a fresh memory store for this test
	store := scstore.NewMemoryStore(72 * time.Hour)
	ingestionSvc := &services.IngestionService{}  // nil for memory mode
	scannerManager := &starlight.ScannerManager{} // mock scanner manager
	server := NewHTTPMCPServer(store, allowAllValidator{}, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

	// Test that proposals require a wish hash.
	t.Run("create_proposal_requires_visible_pixel_hash", func(t *testing.T) {
		req := MCPRequest{
			Tool: "create_proposal",
			Arguments: map[string]interface{}{
				"title":          "Test Proposal",
				"description_md": "A test proposal",
				"budget_sats":    1000,
			},
		}
		body, _ := json.Marshal(req)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-API-Key", "test-key")

		server.handleToolCall(w, r)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp MCPResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if resp.Success {
			t.Fatalf("expected failure due to missing visible_pixel_hash, but got success")
		}

		if resp.ErrorCode != "VALIDATION_FAILED" {
			t.Fatalf("expected VALIDATION_FAILED error code, got: %s", resp.ErrorCode)
		}

		// Check that visible_pixel_hash is in validation errors
		if resp.Details == nil {
			t.Fatalf("expected validation details in error response")
		}
		validationErrors, ok := resp.Details["validation_errors"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected validation_errors in details")
		}
		if _, ok := validationErrors["visible_pixel_hash"]; !ok {
			t.Fatalf("expected visible_pixel_hash in validation errors")
		}
	})

	// Test that scanning tools are available (will fail if scanner not available)
	t.Run("scan_tools_available", func(t *testing.T) {
		req := MCPRequest{
			Tool: "get_scanner_info",
		}
		body, _ := json.Marshal(req)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		server.handleToolCall(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp MCPResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		// Should return scanner info even if scanner is not fully initialized
		if !resp.Success {
			// This is expected if scanner is not available in test environment
			if !strings.Contains(resp.Error, "scanner not available") {
				t.Fatalf("unexpected error: %s", resp.Error)
			}
		}
	})

	// Test that proposal creation fails when wish is missing
	t.Run("create_proposal_without_wish", func(t *testing.T) {
		req := MCPRequest{
			Tool: "create_proposal",
			Arguments: map[string]interface{}{
				"title":              "Test Proposal with Scan",
				"description_md":     "A test proposal with scan data",
				"budget_sats":        1000,
				"contract_id":        "a1b2c3d4e5f6a7b8a1b2c3d4e5f6a7b8a1b2c3d4e5f6a7b8a1b2c3d4e5f6a7b8",
				"visible_pixel_hash": "a1b2c3d4e5f6a7b8a1b2c3d4e5f6a7b8a1b2c3d4e5f6a7b8a1b2c3d4e5f6a7b8",
			},
		}
		body, _ := json.Marshal(req)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-API-Key", "test-key")

		server.handleToolCall(w, r)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}

		var resp MCPResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if resp.Success {
			t.Fatalf("expected failure due to missing wish, but got success")
		}

		if resp.ErrorCode != "RESOURCE_NOT_FOUND" {
			t.Fatalf("expected RESOURCE_NOT_FOUND error code, got: %s", resp.ErrorCode)
		}

		if !strings.Contains(resp.Error, "wish") || !strings.Contains(resp.Error, "not found") {
			t.Fatalf("expected error about missing wish, got: %s", resp.Error)
		}
	})

	// Test that proposal creation succeeds when wish exists
	t.Run("create_proposal_with_wish", func(t *testing.T) {
		visibleHash := strings.Repeat("1", 64)
		wishID := "wish-" + visibleHash
		storeContract := smart_contract.Contract{
			ContractID: wishID,
			Title:      "Wish",
			Status:     "pending",
		}
		if err := store.UpsertContractWithTasks(context.Background(), storeContract, nil); err != nil {
			t.Fatalf("failed to seed wish contract: %v", err)
		}

		req := MCPRequest{
			Tool: "create_proposal",
			Arguments: map[string]interface{}{
				"title":              "Wish Proposal",
				"description_md":     "Proposal for wish",
				"budget_sats":        1000,
				"visible_pixel_hash": visibleHash,
			},
		}
		body, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-API-Key", "test-key")

		server.handleToolCall(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp MCPResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
	})

	t.Run("approve_proposal_requires_wish", func(t *testing.T) {
		apiKey := "approve-test-key"
		visibleHash := strings.Repeat("a", 64)
		proposal := smart_contract.Proposal{
			ID:               "proposal-approve-test",
			Title:            "Approve proposal",
			DescriptionMD:    "Approve proposal details",
			VisiblePixelHash: visibleHash,
			BudgetSats:       1000,
			Status:           "pending",
			Tasks: []smart_contract.Task{
				{
					TaskID:     "proposal-approve-test-task-1",
					ContractID: "proposal-approve-test",
					Title:      "Do work",
					BudgetSats: 1000,
					Status:     "available",
				},
			},
			Metadata: map[string]interface{}{
				"creator_api_key_hash": apiKeyHash(apiKey),
				"visible_pixel_hash":   visibleHash,
			},
		}
		if err := store.CreateProposal(context.Background(), proposal); err != nil {
			t.Fatalf("failed to seed proposal: %v", err)
		}

		req := MCPRequest{
			Tool: "approve_proposal",
			Arguments: map[string]interface{}{
				"proposal_id": proposal.ID,
			},
		}
		body, _ := json.Marshal(req)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-API-Key", apiKey)

		server.handleToolCall(w, r)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}

		var resp MCPResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if resp.Success {
			t.Fatalf("expected failure due to missing wish, but got success")
		}
		if resp.ErrorCode != "RESOURCE_NOT_FOUND" {
			t.Fatalf("expected RESOURCE_NOT_FOUND error code, got: %s", resp.ErrorCode)
		}
		if !strings.Contains(resp.Error, "wish") || !strings.Contains(resp.Error, "not found") {
			t.Fatalf("expected error about missing wish, got: %s", resp.Error)
		}

		wishID := "wish-" + visibleHash
		storeContract := smart_contract.Contract{
			ContractID: wishID,
			Title:      "Wish",
			Status:     "pending",
		}
		if err := store.UpsertContractWithTasks(context.Background(), storeContract, nil); err != nil {
			t.Fatalf("failed to seed wish contract: %v", err)
		}

		w = httptest.NewRecorder()
		r = httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-API-Key", apiKey)

		server.handleToolCall(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
	})

	t.Run("approve_proposal_requires_creator", func(t *testing.T) {
		creatorKey := "creator-key"
		otherKey := "other-key"
		visibleHash := strings.Repeat("b", 64)

		proposal := smart_contract.Proposal{
			ID:               "proposal-creator-test",
			Title:            "Creator test proposal",
			DescriptionMD:    "Test proposal for creator validation",
			VisiblePixelHash: visibleHash,
			BudgetSats:       1000,
			Status:           "pending",
			Metadata: map[string]interface{}{
				"creator_api_key_hash": apiKeyHash(creatorKey),
				"visible_pixel_hash":   visibleHash,
			},
		}
		if err := store.CreateProposal(context.Background(), proposal); err != nil {
			t.Fatalf("failed to seed proposal: %v", err)
		}

		wishID := "wish-" + visibleHash
		wishContract := smart_contract.Contract{
			ContractID: wishID,
			Title:      "Wish",
			Status:     "pending",
		}
		if err := store.UpsertContractWithTasks(context.Background(), wishContract, nil); err != nil {
			t.Fatalf("failed to seed wish contract: %v", err)
		}

		req := MCPRequest{
			Tool: "approve_proposal",
			Arguments: map[string]interface{}{
				"proposal_id": proposal.ID,
			},
		}
		body, _ := json.Marshal(req)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-API-Key", otherKey)

		server.handleToolCall(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
		}

		var resp MCPResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if resp.Success {
			t.Fatalf("expected failure when non-creator tries to approve")
		}
		if resp.ErrorCode != "UNAUTHORIZED" {
			t.Fatalf("expected UNAUTHORIZED error code, got: %s", resp.ErrorCode)
		}
		if !strings.Contains(resp.Error, "approver does not match proposal creator") {
			t.Fatalf("expected creator mismatch error, got: %s", resp.Error)
		}
	})
}

// For PostgreSQL testing with an in-memory simulation, you could use a test database.
// Note: PostgreSQL doesn't have true in-memory mode, but you can use a temporary test DB.
// Example using testcontainers (requires docker):
//
// import "github.com/testcontainers/testcontainers-go/postgres"
//
// func TestHTTPMCPServerWithPostgres(t *testing.T) {
//     ctx := context.Background()
//     pgContainer, err := postgres.RunContainer(ctx,
//         testcontainers.WithImage("postgres:15-alpine"),
//         postgres.WithDatabase("testdb"),
//         postgres.WithUsername("postgres"),
//         postgres.WithPassword("password"),
//         testcontainers.WithWaitStrategy(
//             wait.ForLog("database system is ready to accept connections").
//                 WithOccurrence(2).WithStartupTimeout(5*time.Second)),
//     )
//     if err != nil {
//         t.Fatalf("failed to start container: %v", err)
//     }
//     defer pgContainer.Terminate(ctx)
//
//     dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
//     if err != nil {
//         t.Fatalf("failed to get connection string: %v", err)
//     }
//
//     store, err := scstore.NewPGStore(ctx, dsn, 72*time.Hour, false)
//     if err != nil {
//         t.Fatalf("failed to create PG store: %v", err)
//     }
//
//     ingestionSvc, err := services.NewIngestionService(dsn)
//     if err != nil {
//         t.Fatalf("failed to create ingestion service: %v", err)
//     }
//
//     server := NewHTTPMCPServer(store, "test-key", ingestionSvc)
//
//     // Run similar subtests as TestHTTPMCPServer
// }
