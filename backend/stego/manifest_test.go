package stego

import "testing"

func TestBuildManifestYAMLDeterministic(t *testing.T) {
	out, err := BuildManifestYAML(Manifest{
		SchemaVersion:    1,
		ContractID:       "deadbeef",
		ProposalID:       "proposal-123",
		VisiblePixelHash: "abcd",
		PayloadCID:       "bafydata",
		TasksCID:         "",
		CreatedAt:        1700000000,
		Issuer:           "oracle-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "schema_version: 1\n" +
		"contract_id: deadbeef\n" +
		"proposal_id: proposal-123\n" +
		"visible_pixel_hash: abcd\n" +
		"payload_cid: bafydata\n" +
		"created_at: 1700000000\n" +
		"issuer: oracle-1"
	if string(out) != expected {
		t.Fatalf("unexpected manifest:\n%s", string(out))
	}
}

func TestBuildManifestYAMLQuotes(t *testing.T) {
	out, err := BuildManifestYAML(Manifest{
		SchemaVersion:    1,
		ProposalID:       "proposal:1",
		VisiblePixelHash: "abcd",
		PayloadCID:       "bafydata",
		CreatedAt:        1700000000,
		Issuer:           "oracle 1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) == "" {
		t.Fatalf("expected output")
	}
	if string(out) == "proposal:1" {
		t.Fatalf("expected quoted values")
	}
}
