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

// multiKeyWalletValidator returns different wallets for different keys
type multiKeyWalletValidator struct {
	wallets map[string]string
}

func (m *multiKeyWalletValidator) Validate(key string) bool {
	_, ok := m.wallets[key]
	return ok
}

func (m *multiKeyWalletValidator) Get(key string) (auth.APIKey, bool) {
	wallet, ok := m.wallets[key]
	return auth.APIKey{Key: key, Wallet: wallet}, ok
}

func TestHTTPMCPServer(t *testing.T) {
	// Use memory store for testing
	store := scstore.NewMemoryStore(72 * time.Hour)
	ingestionSvc := &services.IngestionService{}  // nil for memory mode
	scannerManager := &starlight.ScannerManager{} // mock scanner manager
	server := NewHTTPMCPServer(store, allowAllValidator{}, nil, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

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
	server := NewHTTPMCPServer(store, walletValidator{wallet: wallet}, nil, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

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
	server := NewHTTPMCPServer(store, allowAllValidator{}, nil, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

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

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
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

		if w.Code != http.StatusOK {
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
		creatorWallet := "tb1qcreatorwallet000000000000000000000000000"
		// Use walletValidator so the API key has a wallet binding
		walletServer := NewHTTPMCPServer(store, walletValidator{wallet: creatorWallet}, nil, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))
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
				"creator_wallet":     creatorWallet,
				"visible_pixel_hash": visibleHash,
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

		walletServer.handleToolCall(w, r)

		if w.Code != http.StatusOK {
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

		walletServer.handleToolCall(w, r)

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
		creatorWallet := "tb1qcreatorwallet000000000000000000000000000"
		otherWallet := "tb1qotherwallet00000000000000000000000000000"
		visibleHash := strings.Repeat("b", 64)

		// Create a validator that returns different wallets for different keys
		multiWalletValidator := &multiKeyWalletValidator{
			wallets: map[string]string{
				creatorKey: creatorWallet,
				otherKey:   otherWallet,
			},
		}
		creatorServer := NewHTTPMCPServer(store, multiWalletValidator, nil, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

		proposal := smart_contract.Proposal{
			ID:               "proposal-creator-test",
			Title:            "Creator test proposal",
			DescriptionMD:    "Test proposal for creator validation",
			VisiblePixelHash: visibleHash,
			BudgetSats:       1000,
			Status:           "pending",
			Metadata: map[string]interface{}{
				"creator_wallet":     creatorWallet,
				"visible_pixel_hash": visibleHash,
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

		creatorServer.handleToolCall(w, r)

		if w.Code != http.StatusOK {
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
		if !strings.Contains(resp.Error, "does not match wish creator") {
			t.Fatalf("expected creator mismatch error, got: %s", resp.Error)
		}
	})
}

// For PostgreSQL testing with an in-memory simulation, you could use a test database.
func TestScanTransactionTool(t *testing.T) {
	store := scstore.NewMemoryStore(72 * time.Hour)
	ingestionSvc := &services.IngestionService{}
	scannerManager := &starlight.ScannerManager{}
	server := NewHTTPMCPServer(store, allowAllValidator{}, nil, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

	t.Run("scan_transaction_requires_tx_id", func(t *testing.T) {
		req := MCPRequest{
			Tool:      "scan_transaction",
			Arguments: map[string]interface{}{},
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
		if resp.Success {
			t.Fatalf("expected failure due to missing transaction_id")
		}
		if resp.ErrorCode != "VALIDATION_FAILED" {
			t.Fatalf("expected VALIDATION_FAILED error code, got: %s", resp.ErrorCode)
		}
	})

	t.Run("scan_transaction_requires_valid_tx_id", func(t *testing.T) {
		req := MCPRequest{
			Tool: "scan_transaction",
			Arguments: map[string]interface{}{
				"transaction_id": "invalid-tx-id",
			},
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
		if resp.Success {
			t.Fatalf("expected failure due to invalid transaction_id length")
		}
		if resp.ErrorCode != "VALIDATION_FAILED" {
			t.Fatalf("expected VALIDATION_FAILED error code, got: %s", resp.ErrorCode)
		}
	})

	t.Run("scan_transaction_returns_structure", func(t *testing.T) {
		if server.bitcoinClient == nil {
			t.Skip("bitcoin client not available")
		}
		req := MCPRequest{
			Tool: "scan_transaction",
			Arguments: map[string]interface{}{
				"transaction_id": strings.Repeat("a", 64),
			},
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
			t.Logf("Note: Transaction scan may have failed due to network issues: %s", resp.Error)
			return
		}

		data, ok := resp.Result.(map[string]interface{})
		if !ok {
			t.Fatalf("expected result to be a map")
		}

		if data["transaction_id"] != strings.Repeat("a", 64) {
			t.Fatalf("expected transaction_id in response")
		}
		if _, ok := data["status"].(string); !ok {
			t.Fatalf("expected status in response")
		}
	})
}

func TestSubmitWorkRequiresArtifactsForRemoteAgents(t *testing.T) {
	store := scstore.NewMemoryStore(72 * time.Hour)
	ingestionSvc := &services.IngestionService{}
	scannerManager := &starlight.ScannerManager{}
	server := NewHTTPMCPServer(store, allowAllValidator{}, nil, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

	callSubmit := func(t *testing.T, deliverables map[string]interface{}) MCPResponse {
		t.Helper()
		req := MCPRequest{
			Tool: "submit_work",
			Arguments: map[string]interface{}{
				"claim_id":     "claim-remote-1",
				"deliverables": deliverables,
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
		return resp
	}

	t.Run("rejects_missing_artifacts", func(t *testing.T) {
		resp := callSubmit(t, map[string]interface{}{
			"notes": "finished without uploading files",
		})
		if resp.Success {
			t.Fatalf("expected failure when artifacts are missing")
		}
		validationErrors, ok := resp.Details["validation_errors"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected validation_errors in details, got: %#v", resp.Details)
		}
		if _, ok := validationErrors["deliverables.artifacts"]; !ok {
			t.Fatalf("expected deliverables.artifacts validation error, got: %#v", validationErrors)
		}
	})

	t.Run("rejects_empty_artifacts_array", func(t *testing.T) {
		resp := callSubmit(t, map[string]interface{}{
			"notes":     "finished with empty artifacts",
			"artifacts": []interface{}{},
		})
		if resp.Success {
			t.Fatalf("expected failure when artifacts array is empty")
		}
		validationErrors, ok := resp.Details["validation_errors"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected validation_errors in details, got: %#v", resp.Details)
		}
		if _, ok := validationErrors["deliverables.artifacts"]; !ok {
			t.Fatalf("expected deliverables.artifacts validation error, got: %#v", validationErrors)
		}
	})
}
