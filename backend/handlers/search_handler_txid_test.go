package handlers

import (
	"testing"

	sc "stargate-backend/core/smart_contract"
)

func TestContractMatchesQueryByConfirmedTxid(t *testing.T) {
	c := sc.Contract{
		ContractID: "wish-97ad5c72e81b89a103beea1ec57cb2c5795bf05e190be23eb96c7ab1ac49e124",
		Title:      "Complete Retro Arcade Shooter - Full Implementation",
		Status:     "confirmed",
		Metadata: map[string]interface{}{
			"confirmed_txid": "f3549ce6fa4e2cb8f3cdd347fd046aa061d97172360e1b626aa821e383e9e875",
		},
	}

	if !contractMatchesQuery("f3549ce6fa4e2cb8f3cdd347fd046aa061d97172360e1b626aa821e383e9e875", c) {
		t.Fatal("full confirmed_txid should match")
	}
	if !contractMatchesQuery("f3549ce6", c) {
		t.Fatal("txid prefix should match (explorer paste)")
	}
	if !contractMatchesQuery("F3549CE6", c) {
		t.Fatal("txid match should be case-insensitive")
	}
	if !contractMatchesQuery("97ad5c72", c) {
		t.Fatal("wish hash prefix should still match")
	}
	if contractMatchesQuery("41e2b72334d9b03df24b5d481f56e0a45b16d8375888d7a554679a0383a4e346", c) {
		t.Fatal("unrelated txid must not match")
	}
}

func TestContractMatchesQueryByFundingTxid(t *testing.T) {
	c := sc.Contract{
		ContractID: "wish-deadbeef",
		Title:      "Pending fund",
		Status:     "funded",
		Metadata: map[string]interface{}{
			"funding_txid": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}
	if !contractMatchesQuery("aaaaaaaaaaaaaaaa", c) {
		t.Fatal("funding_txid prefix should match before confirm")
	}
}
