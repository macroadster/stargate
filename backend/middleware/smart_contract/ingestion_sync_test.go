package smart_contract

import (
	"context"
	"testing"

	"stargate-backend/core/smart_contract"
)

type fakeWishKeyStore struct {
	proposals []smart_contract.Proposal
	contracts []smart_contract.Contract
}

func (f *fakeWishKeyStore) ListProposals(_ context.Context, _ smart_contract.ProposalFilter) ([]smart_contract.Proposal, error) {
	return f.proposals, nil
}

func (f *fakeWishKeyStore) ListContracts(_ smart_contract.ContractFilter) ([]smart_contract.Contract, error) {
	return f.contracts, nil
}

func TestWishKeyFromTextIngest(t *testing.T) {
	got := wishKeyFromTextIngest(" # Build an AI Market Exchange \n\nDetails here.")
	if got != "build an ai market exchange" {
		t.Fatalf("unexpected wish key: %q", got)
	}
	if wishKeyFromTextIngest("  ") != "" {
		t.Fatalf("expected empty wish key for blank input")
	}
}

func TestHasExistingWishKey(t *testing.T) {
	store := &fakeWishKeyStore{
		proposals: []smart_contract.Proposal{
			{ID: "proposal-1", DescriptionMD: "# Build an AI Market Exchange\nMore info"},
		},
		contracts: []smart_contract.Contract{
			{ContractID: "contract-1", Title: "# Another Wish"},
		},
	}
	ctx := context.Background()
	if !hasExistingWishKey(ctx, store, "build an ai market exchange") {
		t.Fatalf("expected to find matching proposal wish key")
	}
	if !hasExistingWishKey(ctx, store, "another wish") {
		t.Fatalf("expected to find matching contract wish key")
	}
	if hasExistingWishKey(ctx, store, "missing wish") {
		t.Fatalf("did not expect to find missing wish key")
	}
}
