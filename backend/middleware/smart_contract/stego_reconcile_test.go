package smart_contract

import (
	"context"
	"strings"
	"testing"
	"time"

	core "stargate-backend/core/smart_contract"
	"stargate-backend/stego"
	scstore "stargate-backend/storage/smart_contract"
)

func TestUpsertContractFromStegoPayload(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	server := &Server{store: store}
	ctx := context.Background()

	payload := stego.Payload{
		SchemaVersion: 1,
		Proposal: stego.PayloadProposal{
			ID:               "proposal-stego-2",
			Title:            "Stego Contract",
			DescriptionMD:    "Proof of work",
			BudgetSats:       2000,
			VisiblePixelHash: strings.Repeat("b", 64),
			CreatedAt:        time.Now().Unix(),
		},
		Tasks: []stego.PayloadTask{
			{
				TaskID:           "task-stego-2",
				Title:            "Deliver",
				Description:      "Do the work",
				BudgetSats:       2000,
				Skills:           []string{"manual-review"},
				ContractorWallet: "tb1qcontractor",
			},
		},
	}
	manifest := stego.Manifest{
		SchemaVersion:    1,
		ProposalID:       payload.Proposal.ID,
		VisiblePixelHash: payload.Proposal.VisiblePixelHash,
		PayloadCID:       "payloadcid456",
		CreatedAt:        time.Now().Unix(),
		Issuer:           "tester",
	}
	contractID := "contract-stego-2"

	if err := server.upsertContractFromStegoPayload(ctx, contractID, "stegocid456", "stegohash456", manifest, payload); err != nil {
		t.Fatalf("upsertContractFromStegoPayload error: %v", err)
	}

	contract, err := store.GetContract(contractID)
	if err != nil {
		t.Fatalf("contract not created: %v", err)
	}
	if contract.Status != "active" {
		t.Fatalf("expected contract status active, got %s", contract.Status)
	}

	proposal, err := store.GetProposal(ctx, payload.Proposal.ID)
	if err != nil {
		t.Fatalf("proposal not created: %v", err)
	}
	if proposal.Metadata["stego_image_cid"] != "stegocid456" {
		t.Fatalf("stego metadata missing from proposal")
	}

	tasks, err := store.ListTasks(core.TaskFilter{ContractID: contractID})
	if err != nil {
		t.Fatalf("list tasks failed: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatalf("expected tasks to be stored for contract")
	}
}
