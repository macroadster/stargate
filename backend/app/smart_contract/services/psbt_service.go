package services

import (
	"context"
	"fmt"
	"strings"

	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
	appservices "stargate-backend/services"
	scstore "stargate-backend/storage/smart_contract"
)

// PSBTService holds shared resolution helpers used by contract PSBT handlers.
type PSBTService struct {
	store        scstore.Store
	mempool      *bitcoin.MempoolClient
	ingestionSvc *appservices.IngestionService
}

// NewPSBTService creates a PSBTService.
func NewPSBTService(store scstore.Store, mempool *bitcoin.MempoolClient, ingestionSvc *appservices.IngestionService) *PSBTService {
	return &PSBTService{store: store, mempool: mempool, ingestionSvc: ingestionSvc}
}

// ResolveFundingMode returns (mode, fundingAddress) for a contract/proposal id.
func (s *PSBTService) ResolveFundingMode(ctx context.Context, contractID string) (string, string) {
	var meta map[string]interface{}
	var proposal *smart_contract.Proposal
	if s.store != nil {
		if stored, err := s.store.GetProposal(ctx, contractID); err == nil {
			proposal = &stored
			meta = stored.Metadata
		} else if proposals, err := s.store.ListProposals(ctx, smart_contract.ProposalFilter{ContractID: contractID}); err == nil && len(proposals) > 0 {
			proposal = &proposals[0]
			meta = proposals[0].Metadata
		}
	}
	if meta == nil && s.ingestionSvc != nil {
		if rec, err := s.ingestionSvc.Get(contractID); err == nil && rec != nil {
			meta = rec.Metadata
		}
	}
	mode := strings.ToLower(strings.TrimSpace(metaString(meta, "funding_mode")))
	if mode == "" && proposal != nil {
		if LooksLikeRaiseFund(proposal.Title) || LooksLikeRaiseFund(proposal.DescriptionMD) {
			mode = "raise_fund"
		}
	}
	return mode, FundingAddressFromMeta(meta)
}

// ResolveIngestionRecord finds an ingestion record for a contract id (direct or via proposal meta).
func (s *PSBTService) ResolveIngestionRecord(ctx context.Context, contractID string) *appservices.IngestionRecord {
	if s.ingestionSvc == nil || strings.TrimSpace(contractID) == "" {
		return nil
	}
	if rec, err := s.ingestionSvc.Get(contractID); err == nil && rec != nil {
		return rec
	}
	if s.store != nil {
		if stored, err := s.store.GetProposal(ctx, contractID); err == nil {
			if rec := s.IngestionFromProposalMeta(stored.Metadata, stored.VisiblePixelHash); rec != nil {
				return rec
			}
		} else if proposals, err := s.store.ListProposals(ctx, smart_contract.ProposalFilter{ContractID: contractID}); err == nil && len(proposals) > 0 {
			if rec := s.IngestionFromProposalMeta(proposals[0].Metadata, proposals[0].VisiblePixelHash); rec != nil {
				return rec
			}
		}
	}
	return nil
}

// ResolveProposalIDForContract maps a contract id (or wish- prefix) to a proposal id.
func (s *PSBTService) ResolveProposalIDForContract(ctx context.Context, contractID string, rec *appservices.IngestionRecord) string {
	if s.store != nil {
		if proposals, err := s.store.ListProposals(ctx, smart_contract.ProposalFilter{ContractID: contractID}); err == nil && len(proposals) > 0 {
			for _, proposal := range proposals {
				if status := strings.ToLower(strings.TrimSpace(proposal.Status)); status == "approved" || status == "published" {
					if id := strings.TrimSpace(proposal.ID); id != "" {
						return id
					}
				}
			}
			if id := strings.TrimSpace(proposals[0].ID); id != "" {
				return id
			}
		}
		if stored, err := s.store.GetProposal(ctx, contractID); err == nil && strings.TrimSpace(stored.ID) != "" {
			return strings.TrimSpace(stored.ID)
		}
		candidateID := strings.TrimSpace(contractID)
		if strings.HasPrefix(candidateID, "wish-") {
			stripped := strings.TrimPrefix(candidateID, "wish-")
			if stored, err := s.store.GetProposal(ctx, stripped); err == nil && strings.TrimSpace(stored.ID) != "" {
				return strings.TrimSpace(stored.ID)
			}
		}
	}
	if rec != nil && rec.Metadata != nil {
		for _, key := range []string{"origin_proposal_id", "stego_manifest_proposal_id", "proposal_id"} {
			if id := strings.TrimSpace(metaString(rec.Metadata, key)); id != "" {
				return id
			}
		}
	}
	return strings.TrimSpace(contractID)
}

// IngestionFromProposalMeta looks up an ingestion record using proposal metadata keys.
func (s *PSBTService) IngestionFromProposalMeta(meta map[string]interface{}, visiblePixelHash string) *appservices.IngestionRecord {
	if s.ingestionSvc == nil {
		return nil
	}
	ingestionID := strings.TrimSpace(metaString(meta, "ingestion_id"))
	if ingestionID == "" {
		ingestionID = strings.TrimSpace(visiblePixelHash)
	}
	if ingestionID == "" {
		ingestionID = strings.TrimSpace(metaString(meta, "visible_pixel_hash"))
	}
	if ingestionID == "" {
		return nil
	}
	if rec, err := s.ingestionSvc.Get(ingestionID); err == nil && rec != nil {
		return rec
	}
	return nil
}

// LooksLikeRaiseFund detects raise-fund wording in free text (matches prior server helper).
func LooksLikeRaiseFund(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(normalized, "fund raising") ||
		strings.Contains(normalized, "fundraising") ||
		strings.Contains(normalized, "raise fund") ||
		strings.Contains(normalized, "fundraise")
}

// FundingAddressFromMeta reads funding address fields from metadata.
func FundingAddressFromMeta(meta map[string]interface{}) string {
	if meta == nil {
		return ""
	}
	if v := strings.TrimSpace(metaString(meta, "funding_address")); v != "" {
		return v
	}
	if v := strings.TrimSpace(metaString(meta, "address")); v != "" {
		return v
	}
	return ""
}

// IsRaiseFund reports whether a funding mode string is a raise-fund variant.
func IsRaiseFund(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "raise_fund", "fundraiser", "fundraise":
		return true
	default:
		return false
	}
}

func metaString(meta map[string]interface{}, key string) string {
	if meta == nil {
		return ""
	}
	switch v := meta[key].(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
}
