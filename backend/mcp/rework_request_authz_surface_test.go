package mcp

import (
	"context"
	"testing"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// irl.8 over the MCP tool. create_contract_rework_request required a
// wallet-bound key and then recorded that wallet as the requester without ever
// checking it against the wish creator.

func seedReworkRequestContract(t *testing.T, store scstore.Store) string {
	t.Helper()
	contractID := "wish-" + surfaceWishHash
	if err := store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID: contractID,
		Title:      "Wish",
		Status:     "active",
	}, nil); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	return contractID
}

func reworkRequestsFor(t *testing.T, store scstore.Store, contractID string) []smart_contract.ContractReworkRequest {
	t.Helper()
	reqs, err := store.GetContractReworkRequests(context.Background(), contractID)
	if err != nil {
		t.Fatalf("get rework requests: %v", err)
	}
	return reqs
}

func TestMCPReworkRequestRejectsStranger(t *testing.T) {
	srv, store := surfaceFixture(t)
	contractID := seedReworkRequestContract(t, store)

	resp := callTool(t, srv, surfaceStrangerKey, "create_contract_rework_request", map[string]interface{}{
		"contract_id": contractID,
		"notes":       "please redo the header",
	})
	if resp.Success {
		t.Fatal("a wallet-bound key that is not the wish creator must not file a rework request")
	}
	if resp.ErrorCode != ErrCodeUnauthorized {
		t.Fatalf("error_code = %q, want %q (body: %s)", resp.ErrorCode, ErrCodeUnauthorized, resp.Error)
	}
	if reqs := reworkRequestsFor(t, store, contractID); len(reqs) != 0 {
		t.Fatalf("a refused call wrote %d rework requests", len(reqs))
	}
}

func TestMCPReworkRequestAllowsCreatorAndRecordsAuthorizedWallet(t *testing.T) {
	srv, store := surfaceFixture(t)
	contractID := seedReworkRequestContract(t, store)

	resp := callTool(t, srv, surfaceCreatorKey, "create_contract_rework_request", map[string]interface{}{
		"contract_id": contractID,
		"notes":       "please redo the header",
	})
	if !resp.Success {
		t.Fatalf("the wish creator must be allowed to file: %s", resp.Error)
	}

	reqs := reworkRequestsFor(t, store, contractID)
	if len(reqs) != 1 {
		t.Fatalf("stored %d rework requests, want 1", len(reqs))
	}
	// Requester is documented as the wish creator's wallet, so it must be the
	// identity authorization accepted rather than whatever the caller's key
	// happened to be bound to.
	if reqs[0].Requester != surfaceCreatorWlt {
		t.Fatalf("requester = %q, want the authorized wallet %q", reqs[0].Requester, surfaceCreatorWlt)
	}
}

func TestMCPReworkRequestEmitsEventNamingTheWallet(t *testing.T) {
	srv, store := surfaceFixture(t)
	contractID := seedReworkRequestContract(t, store)

	events := captureEvents(t)

	resp := callTool(t, srv, surfaceCreatorKey, "create_contract_rework_request", map[string]interface{}{
		"contract_id": contractID,
		"notes":       "please redo the header",
	})
	if !resp.Success {
		t.Fatalf("the wish creator must be allowed to file: %s", resp.Error)
	}

	// Filing left no audit trail on either surface before this.
	evt := awaitEvent(t, events, "contract_rework_requested", contractID)
	if evt.Actor != surfaceCreatorWlt {
		t.Fatalf("event actor = %q, want the authorized wallet %q", evt.Actor, surfaceCreatorWlt)
	}
}
