package handlers

import (
	"testing"

	sc "stargate-backend/core/smart_contract"
)

func TestNormalizeBlockImageURLStripsWishPrefix(t *testing.T) {
	hash := "a72d3bcda257ff166b14393b96651a8a49bdc20d8ab7e8a8d239be662db21f59"
	in := "/api/block-image/145333/wish-" + hash
	want := "/api/block-image/145333/" + hash
	if got := normalizeBlockImageURL(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := normalizeBlockImageURL(want); got != want {
		t.Fatalf("idempotent: got %q", got)
	}
	if got := normalizeBlockImageURL("/uploads/" + hash); got != "/uploads/"+hash {
		t.Fatalf("non block-image unchanged: %q", got)
	}
}

func TestDedupeConfirmedWishTwins(t *testing.T) {
	hash := "a72d3bcda257ff166b14393b96651a8a49bdc20d8ab7e8a8d239be662db21f59"
	in := []sc.Contract{
		{ContractID: hash, Status: "confirmed", Title: "bare"},
		{ContractID: "wish-" + hash, Status: "confirmed", Title: "wish"},
		{ContractID: "other-contract", Status: "confirmed", Title: "other"},
	}
	out := dedupeConfirmedWishTwins(in)
	if len(out) != 2 {
		t.Fatalf("got %d want 2: %+v", len(out), out)
	}
	var sawWish, sawOther bool
	for _, c := range out {
		if c.ContractID == "wish-"+hash {
			sawWish = true
		}
		if c.ContractID == hash {
			t.Fatal("bare twin should be dropped")
		}
		if c.ContractID == "other-contract" {
			sawOther = true
		}
	}
	if !sawWish || !sawOther {
		t.Fatalf("missing expected rows: %+v", out)
	}
}
