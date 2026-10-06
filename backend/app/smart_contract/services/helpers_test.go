package services

import "testing"

func TestContractIDFromMeta(t *testing.T) {
	if got := ContractIDFromMeta(map[string]interface{}{"contract_id": "x"}, "p"); got != "x" {
		t.Fatalf("got %q", got)
	}
	if got := ContractIDFromMeta(nil, "p"); got != "contract-p" {
		t.Fatalf("got %q", got)
	}
	hash := "41a974b813b024a3817c9c99b5406cc5131a00406c76a4e01fd7519974ccfb40"
	if got := ContractIDFromMeta(nil, hash); got != hash {
		t.Fatalf("pixel proposal id: got %q", got)
	}
	if got := ContractIDFromMeta(map[string]interface{}{"visible_pixel_hash": hash}, "proposal-1"); got != hash {
		t.Fatalf("visible pixel hash: got %q", got)
	}
	if got := ContractIDFromMeta(map[string]interface{}{"contract_id": "contract-" + hash}, "proposal-1"); got != hash {
		t.Fatalf("contract- alias: got %q", got)
	}
	if got := ContractIDFromMeta(map[string]interface{}{
		"visible_pixel_hash": hash,
		"contract_id":        "contract-" + hash,
	}, hash); got != hash {
		t.Fatalf("visible hash beats contract- alias: got %q", got)
	}
	if got := ContractIDFromMeta(map[string]interface{}{"contract_id": "contract-001"}, "p"); got != "contract-001" {
		t.Fatalf("non-pixel contract id: got %q", got)
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
