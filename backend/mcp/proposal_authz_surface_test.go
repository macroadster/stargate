package mcp

import (
	"context"
	"strings"
	"testing"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// The REST route has the same coverage in app/smart_contract. Both surfaces are
// tested because the fail-open allowance irl.2 closed existed in both handlers,
// and only a call through the tool proves the tool refuses.

// surfaceUnknownWish has no ingestion record in surfaceFixture, which is how a
// wish with missing or partial creator metadata presents itself.
const surfaceUnknownWish = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

func seedSurfaceProposalWithoutCreator(t *testing.T, store scstore.Store, proposalID string) {
	t.Helper()
	ctx := context.Background()
	// The wish contract is seeded on purpose: without it, a denial could be the
	// missing-wish rejection rather than the authorization one under test.
	if err := store.UpsertContractWithTasks(ctx, smart_contract.Contract{
		ContractID: "wish-" + surfaceUnknownWish,
		Title:      "Wish with no creator on record",
		Status:     "pending",
	}, nil); err != nil {
		t.Fatalf("seed wish contract: %v", err)
	}
	if err := store.CreateProposal(ctx, smart_contract.Proposal{
		ID:               proposalID,
		Title:            "Proposal against a creatorless wish",
		DescriptionMD:    "details",
		VisiblePixelHash: surfaceUnknownWish,
		BudgetSats:       1000,
		Status:           "pending",
		Tasks: []smart_contract.Task{{
			TaskID:     proposalID + "-task-1",
			ContractID: proposalID,
			Title:      "Do work",
			BudgetSats: 1000,
			Status:     "available",
		}},
		Metadata: map[string]interface{}{"visible_pixel_hash": surfaceUnknownWish},
	}); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}
}

func TestMCPApproveProposalDeniesMissingCreator(t *testing.T) {
	srv, store := surfaceFixture(t)
	seedSurfaceProposalWithoutCreator(t, store, "mcp-prop-nocreator")

	resp := callTool(t, srv, surfaceCreatorKey, "approve_proposal", map[string]interface{}{
		"proposal_id": "mcp-prop-nocreator",
	})

	if resp.Success {
		t.Fatal("a wish with no creator on record must not be approvable")
	}
	if resp.ErrorCode != ErrCodeUnauthorized {
		t.Fatalf("error_code = %q, want %q (body: %s)", resp.ErrorCode, ErrCodeUnauthorized, resp.Error)
	}
	if !strings.Contains(resp.Error, "no creator wallet recorded") {
		t.Fatalf("expected the denial to name the missing creator, got: %s", resp.Error)
	}

	prop, err := store.GetProposal(context.Background(), "mcp-prop-nocreator")
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if prop.Status == "approved" {
		t.Fatal("proposal was approved despite the denial")
	}
}
