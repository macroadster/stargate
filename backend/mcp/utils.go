package mcp

import (
	"fmt"
	"strings"

	"stargate-backend/core/smart_contract"
)

// requireCreatorApproval checks if the provided wallet address matches the wish creator.
// Only the wish creator can approve proposals (not the proposal creator).
func requireCreatorApproval(approverWallet string, proposal smart_contract.Proposal) error {
	if approverWallet == "" {
		return fmt.Errorf("wallet address required for approval")
	}

	// Get the wish creator wallet from proposal metadata or ingestion record
	// For now, we check if there's a creator_wallet in the proposal metadata
	// which should be the wish creator's wallet
	if proposal.Metadata != nil {
		if creatorWallet, ok := proposal.Metadata["creator_wallet"].(string); ok {
			if strings.EqualFold(strings.TrimSpace(creatorWallet), approverWallet) {
				return nil
			}
		}
	}

	return fmt.Errorf("approver wallet %s does not match wish creator", approverWallet)
}
