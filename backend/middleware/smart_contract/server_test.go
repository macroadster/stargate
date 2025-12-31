package smart_contract

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

func TestApproveProposalRequiresWishContract(t *testing.T) {
	store := scstore.NewMemoryStore(72 * 60 * 60)
	server := NewServer(store, nil, nil)

	apiKey := "approve-rest-key"
	visibleHash := strings.Repeat("b", 64)
	proposal := smart_contract.Proposal{
		ID:               "proposal-approve-rest",
		Title:            "Approve proposal",
		DescriptionMD:    "Approve proposal details",
		VisiblePixelHash: visibleHash,
		BudgetSats:       1000,
		Status:           "pending",
		Tasks: []smart_contract.Task{
			{
				TaskID:     "proposal-approve-rest-task-1",
				ContractID: "proposal-approve-rest",
				Title:      "Do work",
				BudgetSats: 1000,
				Status:     "available",
			},
		},
		Metadata: map[string]interface{}{
			"creator_api_key_hash": creatorAPIKeyHash(apiKey),
			"visible_pixel_hash":   visibleHash,
		},
	}
	if err := store.CreateProposal(context.Background(), proposal); err != nil {
		t.Fatalf("failed to seed proposal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/proposals/"+proposal.ID+"/approve", nil)
	req.Header.Set("X-API-Key", apiKey)
	rec := httptest.NewRecorder()
	server.handleProposals(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "wish not found") {
		t.Fatalf("expected wish not found error, got: %s", rec.Body.String())
	}

	wishID := "wish-" + visibleHash
	contract := smart_contract.Contract{
		ContractID: wishID,
		Title:      "Wish",
		Status:     "pending",
	}
	if err := store.UpsertContractWithTasks(context.Background(), contract, nil); err != nil {
		t.Fatalf("failed to seed wish contract: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/smart_contract/proposals/"+proposal.ID+"/approve", nil)
	req.Header.Set("X-API-Key", apiKey)
	rec = httptest.NewRecorder()
	server.handleProposals(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
