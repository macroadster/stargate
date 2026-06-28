package services

import "testing"

func TestContractIDFromMeta(t *testing.T) {
	if got := ContractIDFromMeta(map[string]interface{}{"contract_id": "x"}, "p"); got != "x" {
		t.Fatalf("got %q", got)
	}
	if got := ContractIDFromMeta(nil, "p"); got != "contract-p" {
		t.Fatalf("got %q", got)
	}
}

func TestLooksLikeRaiseFund(t *testing.T) {
	if !LooksLikeRaiseFund("Please raise fund for this") {
		t.Fatal("expected match")
	}
	if LooksLikeRaiseFund("plain proposal") {
		t.Fatal("unexpected match")
	}
}

func TestIsRaiseFund(t *testing.T) {
	if !IsRaiseFund("raise_fund") {
		t.Fatal("expected true")
	}
	if IsRaiseFund("escrow") {
		t.Fatal("expected false")
	}
}
