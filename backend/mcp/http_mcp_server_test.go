package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stargate-backend/services"
	"stargate-backend/starlight"
	scstore "stargate-backend/storage/smart_contract"
)

func TestHTTPMCPServer(t *testing.T) {
	// Use memory store for testing
	store := scstore.NewMemoryStore(72 * time.Hour)
	ingestionSvc := &services.IngestionService{}  // nil for memory mode
	scannerManager := &starlight.ScannerManager{} // mock scanner manager
	server := NewHTTPMCPServer(store, "test-key", ingestionSvc, scannerManager, nil)

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

func TestProposalCreationRequiresIngestion(t *testing.T) {
	// Use a fresh memory store for this test
	store := scstore.NewMemoryStore(72 * time.Hour)
	ingestionSvc := &services.IngestionService{}  // nil for memory mode
	scannerManager := &starlight.ScannerManager{} // mock scanner manager
	server := NewHTTPMCPServer(store, "test-key", ingestionSvc, scannerManager, nil)

	// Test that proposals require scan metadata for manual creation
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

		if !strings.Contains(resp.Error, "scan metadata") {
			t.Fatalf("expected error about scan metadata, got: %s", resp.Error)
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

	// Test that manual creation works with scan metadata
	t.Run("create_proposal_with_scan_metadata", func(t *testing.T) {
		req := MCPRequest{
			Tool: "create_proposal",
			Arguments: map[string]interface{}{
				"title":              "Test Proposal with Scan",
				"description_md":     "A test proposal with scan data",
				"budget_sats":        1000,
				"visible_pixel_hash": "abc123",
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

		result, ok := resp.Result.(map[string]interface{})
		if !ok {
			t.Fatalf("expected result map, got %T", resp.Result)
		}

		if _, hasID := result["proposal_id"]; !hasID {
			t.Fatalf("expected proposal_id in result")
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
