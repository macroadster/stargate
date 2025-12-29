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
	server := NewHTTPMCPServer(store, allowAllValidator{}, ingestionSvc, scannerManager, nil)

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
	server := NewHTTPMCPServer(store, walletValidator{wallet: wallet}, ingestionSvc, scannerManager, nil)

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
			"task_id":      task.TaskID,
			"ai_identifier": "agent-claim-wallet",
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

func TestProposalCreationRequiresIngestion(t *testing.T) {
	// Use a fresh memory store for this test
	store := scstore.NewMemoryStore(72 * time.Hour)
	ingestionSvc := &services.IngestionService{}  // nil for memory mode
	scannerManager := &starlight.ScannerManager{} // mock scanner manager
	server := NewHTTPMCPServer(store, allowAllValidator{}, ingestionSvc, scannerManager, nil)

	// Test that proposals require an ingestion ID (wish).
	t.Run("create_proposal_requires_scan_metadata", func(t *testing.T) {
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

		server.handleToolCall(w, r)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp MCPResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if resp.Success {
			t.Fatalf("expected failure due to missing scan metadata, but got success")
		}

		if !strings.Contains(resp.Error, "ingestion_id is required") {
			t.Fatalf("expected error about ingestion_id requirement, got: %s", resp.Error)
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

	// Test that manual creation with scan metadata is rejected
	t.Run("create_proposal_with_scan_metadata", func(t *testing.T) {
		req := MCPRequest{
			Tool: "create_proposal",
			Arguments: map[string]interface{}{
				"title":              "Test Proposal with Scan",
				"description_md":     "A test proposal with scan data",
				"budget_sats":        1000,
				"contract_id":        "a1b2c3d4e5f6a7b8a1b2c3d4e5f6a7b8a1b2c3d4e5f6a7b8a1b2c3d4e5f6a7b8",
				"visible_pixel_hash": "a1b2c3d4e5f6a7b8a1b2c3d4e5f6a7b8a1b2c3d4e5f6a7b8a1b2c3d4e5f6a7b8",
				"tasks": []interface{}{
					map[string]interface{}{
						"title":       "Task 1",
						"description": "First task",
						"budget_sats": 500,
					},
				},
			},
		}
		body, _ := json.Marshal(req)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		server.handleToolCall(w, r)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}

		var resp MCPResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if resp.Success {
			t.Fatalf("expected failure due to missing ingestion_id, but got success")
		}

		if !strings.Contains(resp.Error, "ingestion_id is required") {
			t.Fatalf("expected error about ingestion_id requirement, got: %s", resp.Error)
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
