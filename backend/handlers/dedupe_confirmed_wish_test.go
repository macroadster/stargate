package handlers

import (
	"strings"
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
	var sawBare, sawOther bool
	for _, c := range out {
		if c.ContractID == "wish-"+hash {
			t.Fatal("wish- twin should be dropped")
		}
		if c.ContractID == hash {
			sawBare = true
		}
		if c.ContractID == "other-contract" {
			sawOther = true
		}
	}
	if !sawBare || !sawOther {
		t.Fatalf("missing expected rows: %+v", out)
	}
}

func TestContractPrefixInscriptionUsesWishAlias(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	ins := contractToInscriptionRequest(sc.Contract{
		ContractID: "contract-" + hash,
		Title:      "Game",
		Status:     "active",
	})
	if ins.ID != "wish-"+hash {
		t.Fatalf("id=%q", ins.ID)
	}
	if ins.VisiblePixelHash != hash {
		t.Fatalf("visible=%q", ins.VisiblePixelHash)
	}
	if ins.ImageData != "/uploads/"+hash {
		t.Fatalf("image=%q", ins.ImageData)
	}
	other := contractToInscriptionRequest(sc.Contract{ContractID: "contract-001", Title: "Keep", Status: "active"})
	if other.ID != "contract-001" {
		t.Fatalf("non-hash id=%q", other.ID)
	}
}
