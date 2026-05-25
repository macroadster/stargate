package stego

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseEmbedded detects the format of stego-extracted bytes and returns
// both a Manifest and a Payload.
//
// Schema v2 (JSON, starts with '{'): the full payload is inline — Manifest
// is synthesised from the Payload fields and the returned Payload is ready
// to use.
//
// Schema v1 (YAML): only the manifest pointer is available — Payload is
// returned zero-value and the caller must fetch it from IPFS via
// manifest.PayloadCID.
func ParseEmbedded(data []byte) (Manifest, Payload, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return Manifest{}, Payload{}, fmt.Errorf("stego payload empty")
	}

	// Detect format: JSON starts with '{', YAML does not.
	if data[0] == '{' {
		return parseEmbeddedJSON(data)
	}
	// Legacy YAML path
	m, err := ParseManifestYAML(data)
	return m, Payload{}, err
}

func parseEmbeddedJSON(data []byte) (Manifest, Payload, error) {
	var p Payload
	if err := json.Unmarshal(data, &p); err != nil {
		return Manifest{}, Payload{}, fmt.Errorf("embedded json decode failed: %w", err)
	}
	if p.SchemaVersion < 2 {
		return Manifest{}, Payload{}, fmt.Errorf("embedded json schema_version must be >= 2, got %d", p.SchemaVersion)
	}
	if strings.TrimSpace(p.ProposalID) == "" {
		return Manifest{}, Payload{}, fmt.Errorf("embedded json proposal_id missing")
	}
	if strings.TrimSpace(p.VisiblePixelHash) == "" {
		return Manifest{}, Payload{}, fmt.Errorf("embedded json visible_pixel_hash missing")
	}
	// Synthesise Manifest from the inline fields so callers that need it
	// (e.g. ingestion metadata, contract upsert) can use the same struct.
	m := Manifest{
		SchemaVersion:    p.SchemaVersion,
		ContractID:       strings.TrimSpace(p.ContractID),
		ProposalID:       strings.TrimSpace(p.ProposalID),
		VisiblePixelHash: strings.TrimSpace(p.VisiblePixelHash),
		SandboxHash:      strings.TrimSpace(p.SandboxHash),
		CreatedAt:        p.CreatedAt,
		Issuer:           strings.TrimSpace(p.Issuer),
		// PayloadCID deliberately empty — payload is inline
	}
	return m, p, nil
}

// ParseManifestYAML parses a v1 YAML stego manifest.
// PayloadCID is optional for v2-era manifests (payload is inline).
func ParseManifestYAML(data []byte) (Manifest, error) {
	var raw Manifest
	if len(data) == 0 {
		return Manifest{}, fmt.Errorf("manifest payload empty")
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Manifest{}, fmt.Errorf("manifest yaml decode failed: %w", err)
	}
	if raw.SchemaVersion <= 0 {
		return Manifest{}, fmt.Errorf("manifest schema_version missing")
	}
	raw.ProposalID = strings.TrimSpace(raw.ProposalID)
	raw.VisiblePixelHash = strings.TrimSpace(raw.VisiblePixelHash)
	raw.PayloadCID = strings.TrimSpace(raw.PayloadCID)
	raw.TasksCID = strings.TrimSpace(raw.TasksCID)
	raw.SandboxHash = strings.TrimSpace(raw.SandboxHash)
	raw.Issuer = strings.TrimSpace(raw.Issuer)
	raw.ContractID = strings.TrimSpace(raw.ContractID)
	if raw.ProposalID == "" {
		return Manifest{}, fmt.Errorf("manifest proposal_id missing")
	}
	if raw.VisiblePixelHash == "" {
		return Manifest{}, fmt.Errorf("manifest visible_pixel_hash missing")
	}
	// PayloadCID is required only for v1 (legacy) manifests.
	// v2+ embeds the payload inline as JSON.
	if raw.SchemaVersion < 2 && raw.PayloadCID == "" {
		return Manifest{}, fmt.Errorf("manifest payload_cid missing")
	}
	if raw.CreatedAt <= 0 {
		return Manifest{}, fmt.Errorf("manifest created_at missing")
	}
	if raw.Issuer == "" {
		return Manifest{}, fmt.Errorf("manifest issuer missing")
	}
	return raw, nil
}
