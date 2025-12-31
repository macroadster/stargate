package smart_contract

import (
	"context"
	"strings"
	"testing"
	"time"

	"stargate-backend/stego"
	scstore "stargate-backend/storage/smart_contract"
)

func TestEnsureProposalFromStegoPayloadCreatesProposal(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	ctx := context.Background()
	now := time.Now().Unix()

	payload := stego.Payload{
		SchemaVersion: 1,
		Proposal: stego.PayloadProposal{
			ID:               "proposal-stego-1",
			Title:            "Stego Proposal",
			DescriptionMD:    "Test proposal",
			BudgetSats:       1200,
			VisiblePixelHash: strings.Repeat("a", 64),
			CreatedAt:        now,
		},
		Tasks: []stego.PayloadTask{
			{
				TaskID:     "task-stego-1",
				Title:      "Do thing",
				BudgetSats: 1200,
				Skills:     []string{"planning"},
			},
		},
		Metadata: []stego.MetadataEntry{
			{Key: "funding_txid", Value: "txid123"},
		},
	}
	manifest := stego.Manifest{
		SchemaVersion:    1,
		ProposalID:       payload.Proposal.ID,
		VisiblePixelHash: payload.Proposal.VisiblePixelHash,
		PayloadCID:       "payloadcid123",
		CreatedAt:        now,
		Issuer:           "test-issuer",
	}

	if err := ensureProposalFromStegoPayload(ctx, store, "stegocid123", manifest, payload); err != nil {
		t.Fatalf("ensureProposalFromStegoPayload error: %v", err)
	}

	got, err := store.GetProposal(ctx, payload.Proposal.ID)
	if err != nil {
		t.Fatalf("proposal not created: %v", err)
	}
	if got.Status != "approved" {
		t.Fatalf("expected proposal status approved, got %s", got.Status)
	}
	if got.VisiblePixelHash != payload.Proposal.VisiblePixelHash {
		t.Fatalf("visible_pixel_hash mismatch: %s", got.VisiblePixelHash)
	}
	if meta := got.Metadata; meta == nil || meta["stego_image_cid"] != "stegocid123" {
		t.Fatalf("missing stego_image_cid in metadata")
	}

	if len(got.Tasks) == 0 {
		t.Fatalf("expected tasks from payload to be stored on proposal")
	}
}
