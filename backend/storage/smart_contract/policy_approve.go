package smart_contract

import (
	"fmt"
	"strings"

	"stargate-backend/core/identity"
	"stargate-backend/core/smart_contract"
)

// ApproveKeys holds contract-id keys used for dual-approve conflict and sibling reject.
type ApproveKeys struct {
	ContractID           string // raw from metadata / proposal id
	NormalizedContractID string
	WishContractID       string // wish-<normalized>
}

// ResolveApproveKeys derives conflict keys for a proposal being approved.
func ResolveApproveKeys(meta map[string]interface{}, proposalID string) ApproveKeys {
	cid := contractIDFromMeta(meta, proposalID)
	norm := NormalizeContractID(cid)
	return ApproveKeys{
		ContractID:           cid,
		NormalizedContractID: norm,
		WishContractID:       "wish-" + norm,
	}
}

// CheckProposalApprovable enforces pending-only (and not already approved/published).
func CheckProposalApprovable(proposalID, currentStatus string) error {
	st := strings.TrimSpace(currentStatus)
	if strings.EqualFold(st, smart_contract.ProposalStatusApproved) || strings.EqualFold(st, smart_contract.ProposalStatusPublished) {
		return fmt.Errorf("proposal %s is already %s", proposalID, st)
	}
	if !strings.EqualFold(st, smart_contract.ProposalStatusPending) {
		return fmt.Errorf("proposal %s must be pending to approve, current status: %s", proposalID, st)
	}
	return nil
}

// ProposalMatchesApproveConflict reports whether another proposal collides on
// id or visible_pixel_hash with the keys used at approval time.
func ProposalMatchesApproveConflict(otherID, otherVisible string, otherMeta map[string]interface{}, keys ApproveKeys) bool {
	if strings.EqualFold(otherID, keys.NormalizedContractID) || strings.EqualFold(otherID, keys.WishContractID) {
		return true
	}
	ov := strings.TrimSpace(otherVisible)
	if ov == "" && otherMeta != nil {
		if v, ok := otherMeta["visible_pixel_hash"].(string); ok {
			ov = strings.TrimSpace(v)
		}
	}
	if ov != "" && (strings.EqualFold(ov, keys.NormalizedContractID) || strings.EqualFold(ov, keys.WishContractID)) {
		return true
	}
	// Also match by normalized contract id from other metadata (Memory path).
	otherCID := NormalizeContractID(contractIDFromMeta(otherMeta, otherID))
	return otherCID != "" && otherCID == keys.NormalizedContractID
}

// VisibleHashForSupersede picks the pixel hash used to supersede the wish contract.
func VisibleHashForSupersede(visiblePixelHash string, meta map[string]interface{}) string {
	visible := strings.TrimSpace(visiblePixelHash)
	if visible == "" && meta != nil {
		if v, ok := meta["visible_pixel_hash"].(string); ok {
			visible = strings.TrimSpace(v)
		}
	}
	return visible
}

// WishContractIDForSupersede returns wish-<hash> or empty if no visible hash.
func WishContractIDForSupersede(visiblePixelHash string, meta map[string]interface{}) string {
	visible := VisibleHashForSupersede(visiblePixelHash, meta)
	if visible == "" {
		return ""
	}
	return identity.ToWishID(visible)
}

// ContractStatusMaySupersede mirrors supersedeEligibleStatusSQL for in-memory paths.
// confirmed / completed / already-superseded must not be demoted.
func ContractStatusMaySupersede(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "confirmed", "completed", "superseded":
		return false
	default:
		return true
	}
}

// CheckProposalPublishable requires approved or already published.
func CheckProposalPublishable(proposalID, currentStatus string) error {
	st := strings.TrimSpace(currentStatus)
	if !strings.EqualFold(st, smart_contract.ProposalStatusApproved) && !strings.EqualFold(st, smart_contract.ProposalStatusPublished) {
		return fmt.Errorf("proposal %s must be approved before publish", proposalID)
	}
	return nil
}

// TaskStatusShouldPublishOnPublish is true when publish should set task → published.
func TaskStatusShouldPublishOnPublish(taskStatus string) bool {
	switch strings.ToLower(strings.TrimSpace(taskStatus)) {
	case "submitted", "pending_review", "claimed", "approved":
		return true
	default:
		return false
	}
}

// ClaimStatusShouldCompleteOnPublish is true when publish should set claim → complete.
func ClaimStatusShouldCompleteOnPublish(claimStatus string) bool {
	switch strings.ToLower(strings.TrimSpace(claimStatus)) {
	case "submitted", "pending_review", "active", "approved":
		return true
	default:
		return false
	}
}
