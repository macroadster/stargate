package stego

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseManifestYAML parses a stego manifest payload into a Manifest struct.
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
	raw.Issuer = strings.TrimSpace(raw.Issuer)
	raw.ContractID = strings.TrimSpace(raw.ContractID)
	if raw.ProposalID == "" {
		return Manifest{}, fmt.Errorf("manifest proposal_id missing")
	}
	if raw.VisiblePixelHash == "" {
		return Manifest{}, fmt.Errorf("manifest visible_pixel_hash missing")
	}
	if raw.PayloadCID == "" {
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
