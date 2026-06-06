package mcp

import (
	"context"
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
	server := NewHTTPMCPServer(nil, nil, nil, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

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

	t.Run("search matches SDK keywords", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/search?q=sdk", nil)
		server.handleToolSearch(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		if !strings.Contains(body, "create_wish") || !strings.Contains(body, "submit_work") {
			t.Fatalf("search for sdk should contain create_wish and submit_work")
		}
	})

	t.Run("search response includes AI guidance prefix", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/search?q=submit", nil)
		server.handleToolSearch(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		if !strings.Contains(body, `"guidance"`) {
			t.Fatalf("search response must include top-level 'guidance' for early AI awareness")
		}
		if !strings.Contains(body, "SKILL.md") || !strings.Contains(body, "starlight_sdk.sh") {
			t.Fatalf("guidance must mention SKILL.md and starlight_sdk.sh")
		}
		if !strings.Contains(body, "sdk_recommended") {
			t.Fatalf("guidance must signal sdk_recommended")
		}
	})
}

func TestGetToolList(t *testing.T) {
	ingestionSvc := &services.IngestionService{}
	scannerManager := &starlight.ScannerManager{}
	server := NewHTTPMCPServer(nil, nil, nil, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

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

		if writeTools != 11 { // create_wish, create_proposal, create_task, claim_task, submit_work, approve_proposal, reject_submission, approve_submission, build_psbt, create_contract_rework_request
			t.Fatalf("expected 11 tools to require auth, got %d", writeTools)
		}
	})

	t.Run("getToolList exposes preferred client metadata", func(t *testing.T) {
		tools := server.getToolList()

		var createWish *ToolMetadata
		var submitWork *ToolMetadata
		for i := range tools {
			switch tools[i].Name {
			case "create_wish":
				createWish = &tools[i]
			case "submit_work":
				submitWork = &tools[i]
			}
		}

		if createWish == nil || submitWork == nil {
			t.Fatalf("expected create_wish and submit_work metadata")
		}
		if createWish.PreferredClient != "starlight_sdk.sh" || submitWork.PreferredClient != "starlight_sdk.sh" {
			t.Fatalf("expected preferred client metadata to point at starlight_sdk.sh")
		}
		if createWish.DocsHint == "" || submitWork.DocsHint == "" {
			t.Fatalf("expected docs hints for upload-focused tools")
		}
	})
}

func TestGetAIGuidanceTool(t *testing.T) {
	ingestionSvc := &services.IngestionService{}
	scannerManager := &starlight.ScannerManager{}
	server := NewHTTPMCPServer(nil, nil, nil, ingestionSvc, scannerManager, nil, auth.NewChallengeStore(10*time.Minute))

	t.Run("get_ai_guidance is discoverable via search and tool list", func(t *testing.T) {
		tools := server.getToolList()
		found := false
		for _, tm := range tools {
			if tm.Name == "get_ai_guidance" {
				found = true
				if tm.AuthRequired {
					t.Fatalf("get_ai_guidance must not require auth")
				}
				if !strings.Contains(strings.ToLower(tm.Description), "skill") {
					t.Fatalf("get_ai_guidance description should mention SKILL guidance")
				}
				break
			}
		}
		if !found {
			t.Fatalf("get_ai_guidance tool must be present in tool list for explicit discovery")
		}

		// Also via search
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/mcp/search?q=guidance", nil)
		server.handleToolSearch(w, r)
		body := w.Body.String()
		if !strings.Contains(body, "get_ai_guidance") {
			t.Fatalf("search for 'guidance' should surface get_ai_guidance")
		}
	})

	t.Run("get_ai_guidance tool returns structured guidance + urls", func(t *testing.T) {
		// Simulate a tool call through the direct dispatcher (no auth needed)
		result, err := server.callToolDirect(context.Background(), "get_ai_guidance", map[string]interface{}{}, "", nil)
		if err != nil {
			t.Fatalf("get_ai_guidance call failed: %v", err)
		}

		resMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map result, got %T", result)
		}
		if resMap["full_skill_md_url"] == "" || resMap["full_sdk_url"] == "" {
			t.Fatalf("result must include full_skill_md_url and full_sdk_url")
		}
		if resMap["guidance"] == nil {
			t.Fatalf("result must embed the 'guidance' block (AIGuidance)")
		}
	})
}
