package smart_contract

import (
	"encoding/json"
	"fmt"
	"strings"

	"stargate-backend/core/identity"
	coresc "stargate-backend/core/smart_contract"
)

// PrepareProposalForCreate normalizes metadata, validates input/status, and returns
// fields needed by both SQLite and Postgres CreateProposal implementations.
// Mutates p in place (metadata, status defaults, sanitization via ValidateProposalInput).
func PrepareProposalForCreate(p *coresc.Proposal) (visibleHash string, metadataJSON []byte, wishToSupersede string, err error) {
	if p.Metadata == nil {
		p.Metadata = map[string]interface{}{}
	}
	if strings.TrimSpace(p.VisiblePixelHash) != "" {
		if vph, ok := p.Metadata["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			p.Metadata["visible_pixel_hash"] = p.VisiblePixelHash
		}
	}
	if metaContract, ok := p.Metadata["contract_id"].(string); ok {
		metaContract = strings.TrimSpace(metaContract)
		if metaContract != "" {
			if metaHash, ok2 := p.Metadata["visible_pixel_hash"].(string); ok2 {
				metaHash = strings.TrimSpace(metaHash)
				normalizedContract := identity.Normalize(metaContract)
				if metaHash != "" && metaHash != normalizedContract {
					return "", nil, "", fmt.Errorf("visible_pixel_hash must match contract_id when both are set (normalized: %s)", normalizedContract)
				}
			}
		}
	}
	if err := ValidateProposalInput(p); err != nil {
		return "", nil, "", fmt.Errorf("proposal validation failed: %v", err)
	}
	if p.Status == "" {
		p.Status = "pending"
	} else if !isValidProposalStatus(p.Status) {
		return "", nil, "", fmt.Errorf("invalid proposal status: %s (must be one of: pending, approved, rejected, published)", p.Status)
	}

	visibleHash = strings.TrimSpace(p.VisiblePixelHash)
	if visibleHash == "" {
		if v, ok := p.Metadata["visible_pixel_hash"].(string); ok {
			visibleHash = strings.TrimSpace(v)
		}
	}

	metaMap := p.Metadata
	if metaMap == nil {
		metaMap = map[string]interface{}{}
	}
	if len(p.Tasks) > 0 {
		metaMap["suggested_tasks"] = p.Tasks
	}
	metadataJSON, _ = json.Marshal(metaMap)

	if strings.EqualFold(p.Status, "approved") || strings.EqualFold(p.Status, "published") {
		visible := strings.TrimSpace(p.VisiblePixelHash)
		if visible == "" {
			if v, ok := p.Metadata["visible_pixel_hash"].(string); ok {
				visible = strings.TrimSpace(v)
			}
		}
		if visible == "" {
			if v, ok := p.Metadata["contract_id"].(string); ok {
				visible = strings.TrimSpace(v)
			}
		}
		if visible != "" {
			wishToSupersede = identity.ToWishID(visible)
		}
	}
	return visibleHash, metadataJSON, wishToSupersede, nil
}

// ProposalConflictApprovedMsg is the shared error text for approved/published VPH conflicts.
func ProposalConflictApprovedMsg(visibleHash, conflictID string) error {
	return fmt.Errorf("a proposal with visible_pixel_hash=%s is already approved/published (id=%s)", visibleHash, conflictID)
}

// ProposalMaxPerWishMsg is the shared error when too many proposals exist for a wish.
func ProposalMaxPerWishMsg(visibleHash string) error {
	return fmt.Errorf("maximum of 5 proposals reached for wish %s", visibleHash)
}

// MaxProposalsPerWish is the safeguard applied by both dialects on create.
const MaxProposalsPerWish = 5
