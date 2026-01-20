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
	"stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"
)

func TestStructuredErrorResponses(t *testing.T) {
	// Use memory store for testing
	store := scstore.NewMemoryStore(72 * time.Hour)
	ingestionSvc := &services.IngestionService{}  // nil for memory mode
	scannerManager := &starlight.ScannerManager{} // mock scanner manager
	server := NewHTTPMCPServer(store, walletValidator{wallet: "tb1qtestwallet00000000000000000000000000000000"}, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

	t.Run("claim_task_validation_errors", func(t *testing.T) {
		req := MCPRequest{
			Tool:      "claim_task",
			Arguments: map[string]interface{}{
				// Missing task_id
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

		// Test structured error response
		if resp.Success {
			t.Fatalf("expected failure, got success")
		}

		// Check error code is specific, not generic
		if resp.ErrorCode != "VALIDATION_FAILED" {
			t.Fatalf("expected VALIDATION_FAILED error code, got: %s", resp.ErrorCode)
		}

		// Check details contain validation errors
		if resp.Details == nil {
			t.Fatalf("expected details in error response")
		}

		validationErrors, ok := resp.Details["validation_errors"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected validation_errors in details")
		}

		taskIDError, ok := validationErrors["task_id"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected task_id validation error")
		}

		if taskIDError["required"] != true {
			t.Fatalf("expected task_id to be marked as required")
		}

		// Check tool name is included
		if resp.Details["tool"] != "claim_task" {
			t.Fatalf("expected tool name in details")
		}

		// Check required_fields array for easier parsing
		if len(resp.RequiredFields) == 0 || resp.RequiredFields[0] != "task_id" {
			t.Fatalf("expected task_id in required_fields array")
		}
	})

	t.Run("create_proposal_validation_errors", func(t *testing.T) {
		req := MCPRequest{
			Tool: "create_proposal",
			Arguments: map[string]interface{}{
				"title":              "", // Empty title
				"description_md":     "A description",
				"visible_pixel_hash": "",   // Empty hash
				"budget_sats":        -100, // Invalid budget
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
			t.Fatalf("expected failure, got success")
		}

		// Check for multiple field validation errors
		if resp.Details == nil {
			t.Fatalf("expected details in error response")
		}

		validationErrors, ok := resp.Details["validation_errors"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected validation_errors in details")
		}

		// Should have errors for title, visible_pixel_hash, and budget_sats
		expectedFields := []string{"title", "visible_pixel_hash", "budget_sats"}
		for _, field := range expectedFields {
			if _, ok := validationErrors[field]; !ok {
				t.Fatalf("expected validation error for field: %s", field)
			}
		}
	})

	t.Run("submit_work_validation_errors", func(t *testing.T) {
		req := MCPRequest{
			Tool: "submit_work",
			Arguments: map[string]interface{}{
				"claim_id": "", // Empty claim_id
				"deliverables": map[string]interface{}{
					// Missing "notes" field
					"other_field": "some value",
				},
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
			t.Fatalf("expected failure, got success")
		}

		validationErrors, ok := resp.Details["validation_errors"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected validation_errors in details")
		}

		// Should have errors for claim_id and deliverables.notes
		expectedFields := []string{"claim_id", "deliverables.notes"}
		for _, field := range expectedFields {
			if _, ok := validationErrors[field]; !ok {
				t.Fatalf("expected validation error for field: %s", field)
			}
		}
	})

	t.Run("get_contract_not_found_error", func(t *testing.T) {
		req := MCPRequest{
			Tool: "get_contract",
			Arguments: map[string]interface{}{
				"contract_id": "non-existent-contract",
			},
		}
		body, _ := json.Marshal(req)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		server.handleToolCall(w, r)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}

		var resp MCPResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if resp.Success {
			t.Fatalf("expected failure, got success")
		}

		// Check for specific error code, not generic "TOOL_ERROR"
		if resp.ErrorCode != "RESOURCE_NOT_FOUND" {
			t.Fatalf("expected RESOURCE_NOT_FOUND error code, got: %s", resp.ErrorCode)
		}

		// Check that tool name is included in details
		if resp.Details == nil || resp.Details["tool"] != "get_contract" {
			t.Fatalf("expected tool name in error details")
		}

		// Check error message is descriptive
		if !strings.Contains(resp.Error, "not found") {
			t.Fatalf("expected 'not found' in error message, got: %s", resp.Error)
		}
	})
}

func TestErrorResponseStructure(t *testing.T) {
	store := scstore.NewMemoryStore(72 * time.Hour)
	server := NewHTTPMCPServer(store, allowAllValidator{}, nil, nil, nil, auth.NewChallengeStore(10*time.Minute))

	t.Run("error_response_has_all_fields", func(t *testing.T) {
		// Trigger a validation error
		req := MCPRequest{
			Tool:      "get_auth_challenge",
			Arguments: map[string]interface{}{
				// Missing wallet_address
			},
		}
		body, _ := json.Marshal(req)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		server.handleToolCall(w, r)

		var resp MCPResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		// Check all expected fields are present
		if resp.Success {
			t.Fatalf("expected failure")
		}

		if resp.ErrorCode == "" {
			t.Fatalf("expected error_code field")
		}

		if resp.Error == "" {
			t.Fatalf("expected error field")
		}

		if resp.Message == "" {
			t.Fatalf("expected message field")
		}

		if resp.Code == 0 {
			t.Fatalf("expected code field (HTTP status)")
		}

		if resp.Timestamp == "" {
			t.Fatalf("expected timestamp field")
		}

		if resp.Version == "" {
			t.Fatalf("expected version field")
		}
	})
}
