package stego

import (
	"encoding/json"
	"testing"
)

func TestWishCreatorMessageIsDomainSeparated(t *testing.T) {
	got := WishCreatorMessage("  abcd  ")
	if got != "STARLIGHT-WISH-V1\nabcd" {
		t.Fatalf("WishCreatorMessage = %q", got)
	}
}

func TestParseEmbeddedJSONCarriesCreatorAttestation(t *testing.T) {
	p := Payload{
		SchemaVersion:    2,
		ProposalID:       "proposal-v2",
		VisiblePixelHash: "abcd1234",
		Issuer:           "oracle-1",
		CreatedAt:        1700000000,
		CreatorWallet:    "tb1qcreator",
		CreatorSig:       "c2ln",
		Proposal:         PayloadProposal{ID: "proposal-v2", Title: "t"},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	m, payload, err := ParseEmbedded(data)
	if err != nil {
		t.Fatalf("ParseEmbedded: %v", err)
	}
	if m.CreatorWallet != "tb1qcreator" || m.CreatorSig != "c2ln" {
		t.Fatalf("manifest attestation = %q / %q", m.CreatorWallet, m.CreatorSig)
	}
	if payload.CreatorWallet != "tb1qcreator" || payload.CreatorSig != "c2ln" {
		t.Fatalf("payload attestation = %q / %q", payload.CreatorWallet, payload.CreatorSig)
	}
}
