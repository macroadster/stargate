package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stargate-backend/services"
	"stargate-backend/starlight"
	auth "stargate-backend/storage/auth"
)

func TestToolSearch(t *testing.T) {
	ingestionSvc := &services.IngestionService{}
	scannerManager := &starlight.ScannerManager{}
	server := NewHTTPMCPServer(nil, nil, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

	t.Run("search without filters returns all", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/search", nil)
		server.handleToolSearch(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		// Check response contains tools
		body := w.Body.String()
		if !strings.Contains(body, "tools") {
			t.Fatalf("response should contain 'tools' key")
		}
		if !strings.Contains(body, "matched") {
			t.Fatalf("response should contain 'matched' count")
		}
	})

	t.Run("search by keyword filters results", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/search?q=contract", nil)
		server.handleToolSearch(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		// Check response contains matching tools
		body := w.Body.String()
		if !strings.Contains(body, "list_contracts") {
			t.Fatalf("should contain 'list_contracts'")
		}
	})

	t.Run("search by category filters results", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/search?category=discovery", nil)
		server.handleToolSearch(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		// Should not contain write tools
		if strings.Contains(body, "create_proposal") {
			t.Fatalf("should not contain 'create_proposal' when filtering by discovery category")
		}
	})

	t.Run("search with limit results", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/search?limit=2", nil)
		server.handleToolSearch(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		// Check matched count respects limit
		if !strings.Contains(body, `"matched":2`) {
			t.Fatalf("should limit to 2 results")
		}
	})

	t.Run("search with query and limit", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/search?q=task&limit=1", nil)
		server.handleToolSearch(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		if !strings.Contains(body, `"matched":1`) {
			t.Fatalf("should limit to 1 result for query 'task'")
		}
	})
}

func TestGetToolList(t *testing.T) {
	ingestionSvc := &services.IngestionService{}
	scannerManager := &starlight.ScannerManager{}
	server := NewHTTPMCPServer(nil, nil, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

	t.Run("getToolList returns all tools with metadata", func(t *testing.T) {
		tools := server.getToolList()

		if len(tools) == 0 {
			t.Fatalf("expected at least one tool, got none")
		}

		// Check first tool has required fields
		first := tools[0]
		if first.Name == "" {
			t.Fatalf("tool should have name")
		}
		if first.Description == "" {
			t.Fatalf("tool should have description")
		}
		if first.Category == "" {
			t.Fatalf("tool should have category")
		}
	})

	t.Run("getToolList sets auth_required correctly", func(t *testing.T) {
		tools := server.getToolList()

		writeTools := 0
		for _, tool := range tools {
			if tool.AuthRequired {
				writeTools++
			}
		}

		if writeTools == 0 {
			t.Fatalf("expected some tools to require auth")
		}

		if writeTools != 5 { // create_wish, create_proposal, claim_task, submit_work, approve_proposal
			t.Fatalf("expected 5 tools to require auth, got %d", writeTools)
		}
	})
}
