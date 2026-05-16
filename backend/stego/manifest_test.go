package stego

import (
	"strings"
	"testing"
)

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

func TestManifestSandboxHashRoundTrip(t *testing.T) {
	hash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	m := Manifest{
		SchemaVersion:    1,
		ProposalID:       "proposal-sb",
		VisiblePixelHash: "abcd1234",
		PayloadCID:       "bafypayload",
		SandboxHash:      hash,
		CreatedAt:        1700000000,
		Issuer:           "oracle-1",
	}

	// Build YAML
	out, err := BuildManifestYAML(m)
	if err != nil {
		t.Fatalf("BuildManifestYAML: %v", err)
	}
	yaml := string(out)

	// Verify sandbox_hash is present in YAML output
	if !strings.Contains(yaml, "sandbox_hash: "+hash) {
		t.Fatalf("sandbox_hash not found in manifest YAML:\n%s", yaml)
	}

	// Parse back and verify field survives round-trip
	parsed, err := ParseManifestYAML(out)
	if err != nil {
		t.Fatalf("ParseManifestYAML: %v", err)
	}
	if parsed.SandboxHash != hash {
		t.Fatalf("SandboxHash mismatch: got %q, want %q", parsed.SandboxHash, hash)
	}
}

func TestManifestSandboxHashOmittedWhenEmpty(t *testing.T) {
	m := Manifest{
		SchemaVersion:    1,
		ProposalID:       "proposal-no-sb",
		VisiblePixelHash: "abcd1234",
		PayloadCID:       "bafypayload",
		CreatedAt:        1700000000,
		Issuer:           "oracle-1",
	}
	out, err := BuildManifestYAML(m)
	if err != nil {
		t.Fatalf("BuildManifestYAML: %v", err)
	}
	if strings.Contains(string(out), "sandbox_hash") {
		t.Fatalf("sandbox_hash should be omitted when empty, got:\n%s", string(out))
	}
}

func TestManifestSandboxHashFieldOrder(t *testing.T) {
	// sandbox_hash should appear between tasks_cid and created_at
	m := Manifest{
		SchemaVersion:    1,
		ProposalID:       "proposal-order",
		VisiblePixelHash: "abcd1234",
		PayloadCID:       "bafypayload",
		TasksCID:         "bafytasks",
		SandboxHash:      "deadbeef",
		CreatedAt:        1700000000,
		Issuer:           "oracle-1",
	}
	out, err := BuildManifestYAML(m)
	if err != nil {
		t.Fatalf("BuildManifestYAML: %v", err)
	}
	yaml := string(out)
	tasksIdx := strings.Index(yaml, "tasks_cid:")
	sandboxIdx := strings.Index(yaml, "sandbox_hash:")
	createdIdx := strings.Index(yaml, "created_at:")
	if tasksIdx >= sandboxIdx || sandboxIdx >= createdIdx {
		t.Fatalf("field order wrong: tasks_cid@%d sandbox_hash@%d created_at@%d\n%s",
			tasksIdx, sandboxIdx, createdIdx, yaml)
	}
}
