package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"stargate-backend/core/smart_contract"
)

func apiKeyHash(apiKey string) string {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func requireCreatorApproval(apiKey string, proposal smart_contract.Proposal) error {
	if proposal.Metadata == nil {
		return fmt.Errorf("proposal missing creator metadata")
	}
	required, _ := proposal.Metadata["creator_api_key_hash"].(string)
	required = strings.TrimSpace(required)
	if required == "" {
		return fmt.Errorf("proposal missing creator_api_key_hash; recreate the wish to approve")
	}
	current := apiKeyHash(apiKey)
	if current == "" {
		return fmt.Errorf("api key required for approval")
	}
	if required != current {
		return fmt.Errorf("approver does not match proposal creator")
	}
	return nil
}
