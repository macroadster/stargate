package smart_contract

import (
	"testing"

	coresc "stargate-backend/core/smart_contract"
)

func TestProtectContractUpsert(t *testing.T) {
	existing := coresc.Contract{
		ContractID:      "wish-abc",
		Title:           "Legitimate",
		TotalBudgetSats: 1000,
		Status:          "confirmed",
		Metadata:        map[string]interface{}{"origin": "local"},
	}
	incoming := coresc.Contract{
		ContractID:      "wish-abc",
		Title:           "Hijacked",
		TotalBudgetSats: 42,
		Status:          "funded",
		StegoImageURL:   "/stego/x.png",
		Metadata:        map[string]interface{}{"sandbox_hash": "deadbeef"},
	}
	got := ProtectContractUpsert(existing, incoming)
	if got.Title != "Legitimate" || got.TotalBudgetSats != 1000 || got.Status != "confirmed" {
		t.Fatalf("protected fields mutated: %+v", got)
	}
	if got.StegoImageURL != "/stego/x.png" {
		t.Fatalf("expected stego url refresh, got %q", got.StegoImageURL)
	}
	if got.Metadata["origin"] != "local" {
		t.Fatal("existing metadata dropped")
	}
	if got.Metadata["sandbox_hash"] != "deadbeef" {
		t.Fatal("expected additive metadata merge")
	}

	active := existing
	active.Status = "active"
	got2 := ProtectContractUpsert(active, incoming)
	if got2.Title != "Hijacked" {
		t.Fatalf("active should allow overwrite at store helper, got %q", got2.Title)
	}
}

func TestIsNonSupersedableContractStatus(t *testing.T) {
	if !IsNonSupersedableContractStatus("confirmed") {
		t.Fatal("confirmed should not supersede")
	}
	if !IsNonSupersedableContractStatus("COMPLETED") {
		t.Fatal("completed should not supersede")
	}
	if IsNonSupersedableContractStatus("active") {
		t.Fatal("active may supersede")
	}
}
