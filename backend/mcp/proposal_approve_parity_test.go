package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	scmiddleware "stargate-backend/app/smart_contract"
	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// approve_proposal called store.ApproveProposal directly, so it approved without
// any of the work ProposalService.Approve does afterwards (stargate-fhz).
// Authorization was never the gap: this surface ran the same fail-closed
// WishCreatorAuthorizer, which is why the whole suite stayed green while the two
// approve paths drifted. These tests pin the side effects, since those are what
// actually differed.

// seedApprovableProposal creates a proposal the fixture's creator key is allowed
// to approve, against a wish contract that exists.
func seedApprovableProposal(t *testing.T, store scstore.Store, proposalID string, meta map[string]interface{}) {
	t.Helper()
	ctx := context.Background()

	if err := store.UpsertContractWithTasks(ctx, smart_contract.Contract{
		ContractID: "wish-" + surfaceWishHash,
		Title:      "Wish",
		Status:     "pending",
	}, nil); err != nil {
		t.Fatalf("seed wish contract: %v", err)
	}

	if meta == nil {
		meta = map[string]interface{}{}
	}
	meta["visible_pixel_hash"] = surfaceWishHash

	if err := store.CreateProposal(ctx, smart_contract.Proposal{
		ID:               proposalID,
		Title:            "Proposal",
		DescriptionMD:    "do the thing",
		VisiblePixelHash: surfaceWishHash,
		BudgetSats:       1000,
		Status:           "pending",
		Tasks: []smart_contract.Task{{
			TaskID:     proposalID + "-task-1",
			ContractID: proposalID,
			Title:      "Do work",
			BudgetSats: 1000,
			Status:     "available",
		}},
		Metadata: meta,
	}); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}
}

func TestMCPApproveProposalEmitsApproveEventNamingTheWallet(t *testing.T) {
	srv, store := surfaceFixture(t)
	seedApprovableProposal(t, store, "mcp-prop-event", nil)

	// RegisterEventSink appends to a process-global slice with no way to
	// unregister, so this sink outlives the test. Safe only because nothing else
	// in this package asserts on sinks; do not copy this into another test.
	events := make(chan smart_contract.Event, 8)
	scmiddleware.RegisterEventSink(func(evt smart_contract.Event) {
		select {
		case events <- evt:
		default:
		}
	})

	resp := callTool(t, srv, surfaceCreatorKey, "approve_proposal", map[string]interface{}{
		"proposal_id": "mcp-prop-event",
	})
	if !resp.Success {
		t.Fatalf("expected the wish creator to be allowed, got: %s", resp.Error)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case evt := <-events:
			if evt.Type == "approve" && evt.EntityID == "mcp-prop-event" {
				// The actor is the wallet authorization accepted, resolved by the
				// service rather than a hardcoded string.
				if evt.Actor != surfaceCreatorWlt {
					t.Fatalf("event actor = %q, want the approving wallet %q", evt.Actor, surfaceCreatorWlt)
				}
				return
			}
		case <-deadline:
			t.Fatal("no approve event was recorded for an MCP approval; an approval over this surface left no audit trail")
		}
	}
}

func TestMCPApproveRaiseFundBindsPayoutAddress(t *testing.T) {
	srv, store := surfaceFixture(t)
	seedApprovableProposal(t, store, "mcp-prop-fundraiser", map[string]interface{}{
		"funding_mode": "raise_fund",
	})

	resp := callTool(t, srv, surfaceCreatorKey, "approve_proposal", map[string]interface{}{
		"proposal_id": "mcp-prop-fundraiser",
	})
	if !resp.Success {
		t.Fatalf("expected the wish creator to be allowed, got: %s", resp.Error)
	}

	prop, err := store.GetProposal(context.Background(), "mcp-prop-fundraiser")
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}

	// A fundraiser approved with no payout address has nowhere to send raised
	// funds. REST derives both addresses from the approving wallet; approving
	// over MCP used to skip that entirely and leave the proposal approved anyway.
	payout, _ := prop.Metadata["payout_address"].(string)
	if strings.TrimSpace(payout) != surfaceCreatorWlt {
		t.Fatalf("payout_address = %q, want the approving wallet %q", payout, surfaceCreatorWlt)
	}
	funding, _ := prop.Metadata["funding_address"].(string)
	if strings.TrimSpace(funding) != surfaceCreatorWlt {
		t.Fatalf("funding_address = %q, want the approving wallet %q", funding, surfaceCreatorWlt)
	}
}

func TestMCPApproveUnknownProposalDoesNotRevealAbsence(t *testing.T) {
	srv, _ := surfaceFixture(t)

	resp := callTool(t, srv, surfaceCreatorKey, "approve_proposal", map[string]interface{}{
		"proposal_id": "mcp-prop-does-not-exist",
	})

	if resp.Success {
		t.Fatal("approving a proposal that does not exist must fail")
	}
	// This surface used to answer RESOURCE_NOT_FOUND here, because it looked the
	// proposal up itself before authorizing. Authorization now runs first and the
	// gate cannot authorize a proposal it cannot load, so the answer is
	// UNAUTHORIZED and identical to the stranger case above.
	//
	// That is deliberate rather than incidental: a caller who is not the creator
	// learns nothing about whether a given proposal ID exists. Pinned because it
	// is a visible change to this surface's contract, not an internal detail.
	if resp.ErrorCode != ErrCodeUnauthorized {
		t.Fatalf("error_code = %q, want %q (body: %s)", resp.ErrorCode, ErrCodeUnauthorized, resp.Error)
	}
}

func TestMCPApproveProposalStillRefusesStranger(t *testing.T) {
	srv, store := surfaceFixture(t)
	seedApprovableProposal(t, store, "mcp-prop-stranger", nil)

	resp := callTool(t, srv, surfaceStrangerKey, "approve_proposal", map[string]interface{}{
		"proposal_id": "mcp-prop-stranger",
	})
	if resp.Success {
		t.Fatal("a key that is not the wish creator must not approve")
	}
	if resp.ErrorCode != ErrCodeUnauthorized {
		t.Fatalf("error_code = %q, want %q (body: %s)", resp.ErrorCode, ErrCodeUnauthorized, resp.Error)
	}

	// Routing through the service must not weaken the denial into a
	// deny-after-write: the status has to be untouched.
	prop, err := store.GetProposal(context.Background(), "mcp-prop-stranger")
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if prop.Status == "approved" {
		t.Fatal("proposal was approved despite the denial")
	}
}
