package mcp

import (
	"fmt"
	"strings"

	"stargate-backend/core/smart_contract"
)

// requireCreatorApproval checks if the provided wallet address matches the proposal creator.
// This is a helper for cases where the apiKeyStore is not available.
func requireCreatorApproval(approverWallet string, proposal smart_contract.Proposal) error {
	if proposal.Metadata == nil {
		return fmt.Errorf("proposal missing creator metadata")
	}
	required, _ := proposal.Metadata["creator_wallet"].(string)
	required = strings.TrimSpace(required)
	if required == "" {
		return fmt.Errorf("proposal missing creator_wallet; recreate the wish to approve")
	}
	if approverWallet == "" {
		return fmt.Errorf("wallet address required for approval")
	}
	if !strings.EqualFold(required, approverWallet) {
		return fmt.Errorf("approver does not match proposal creator")
	}
	return nil
}
