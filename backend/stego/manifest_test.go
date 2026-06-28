package stego

import (
	"encoding/json"
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

func TestParseEmbeddedJSON(t *testing.T) {
	p := Payload{
		SchemaVersion:    2,
		ProposalID:       "proposal-v2",
		VisiblePixelHash: "abcd1234",
		Issuer:           "oracle-1",
		CreatedAt:        1700000000,
		SandboxHash:      "deadbeef",
		Proposal: PayloadProposal{
			ID:    "proposal-v2",
			Title: "Test Proposal",
		},
		Tasks: []PayloadTask{
			{TaskID: "task-1", Title: "Do stuff", BudgetSats: 5000},
		},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	m, payload, err := ParseEmbedded(data)
	if err != nil {
		t.Fatalf("ParseEmbedded: %v", err)
	}
	if m.ProposalID != "proposal-v2" {
		t.Fatalf("manifest.ProposalID = %q, want proposal-v2", m.ProposalID)
	}
	if m.VisiblePixelHash != "abcd1234" {
		t.Fatalf("manifest.VisiblePixelHash = %q, want abcd1234", m.VisiblePixelHash)
	}
	if m.PayloadCID != "" {
		t.Fatalf("manifest.PayloadCID should be empty for v2, got %q", m.PayloadCID)
	}
	if payload.SchemaVersion != 2 {
		t.Fatalf("payload.SchemaVersion = %d, want 2", payload.SchemaVersion)
	}
	if len(payload.Tasks) != 1 || payload.Tasks[0].TaskID != "task-1" {
		t.Fatalf("payload.Tasks mismatch: %+v", payload.Tasks)
	}
}

func TestParseEmbeddedYAMLFallback(t *testing.T) {
	m := Manifest{
		SchemaVersion:    1,
		ProposalID:       "proposal-v1",
		VisiblePixelHash: "abcd",
		PayloadCID:       "bafydata",
		CreatedAt:        1700000000,
		Issuer:           "oracle-1",
	}
	data, err := BuildManifestYAML(m)
	if err != nil {
		t.Fatalf("BuildManifestYAML: %v", err)
	}
	manifest, payload, err := ParseEmbedded(data)
	if err != nil {
		t.Fatalf("ParseEmbedded: %v", err)
	}
	if manifest.ProposalID != "proposal-v1" {
		t.Fatalf("manifest.ProposalID = %q, want proposal-v1", manifest.ProposalID)
	}
	if manifest.PayloadCID != "bafydata" {
		t.Fatalf("manifest.PayloadCID = %q, want bafydata", manifest.PayloadCID)
	}
	// v1: payload should be zero-value (caller must fetch from IPFS)
	if payload.SchemaVersion != 0 {
		t.Fatalf("payload.SchemaVersion = %d, want 0 for v1 YAML", payload.SchemaVersion)
	}
}
