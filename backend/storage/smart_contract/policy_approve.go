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
	// MatchValues is the set used in SQL IN (...) / conflict scans (normalized + wish- form).
	MatchValues []string
}

// ResolveApproveKeys derives conflict keys for a proposal being approved.
func ResolveApproveKeys(meta map[string]interface{}, proposalID string) ApproveKeys {
	cid := contractIDFromMeta(meta, proposalID)
	norm := NormalizeContractID(cid)
	wish := "wish-" + norm
	return ApproveKeys{
		ContractID:           cid,
		NormalizedContractID: norm,
		WishContractID:       wish,
		MatchValues:          []string{norm, wish},
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

// ErrApproveConflict is the dual-approve error (shared wording).
func ErrApproveConflict(normalizedContractID string) error {
	return fmt.Errorf("another proposal is already approved/published for contract %s", normalizedContractID)
}

// ErrApproveNoTasks is returned when neither proposal nor contract has tasks.
func ErrApproveNoTasks() error {
	return fmt.Errorf("approved proposals must contain at least one task")
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
	return !IsProtectedContractStatus(status)
}

// EnsureVisibleInMeta copies column visible_pixel_hash into metadata when missing.
func EnsureVisibleInMeta(meta map[string]interface{}, visiblePixelHash string) map[string]interface{} {
	if meta == nil {
		meta = map[string]interface{}{}
	}
	if strings.TrimSpace(visiblePixelHash) != "" {
		if vph, ok := meta["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			meta["visible_pixel_hash"] = visiblePixelHash
		}
	}
	return meta
}

// ApprovePlan is the dialect-agnostic result after pure approval checks pass.
// Stores still run conflict queries / writes, but all decisions live here.
type ApprovePlan struct {
	Keys              ApproveKeys
	WishToSupersede   string // empty if none
	NewProposalStatus string // "approved"
	// SetRelatedTasksApproved is true for Memory (SQL dialects leave tasks alone at approve).
	SetRelatedTasksApproved bool
}

// BuildApprovePlan runs shared approve preconditions.
// proposal must already have Tasks populated (populateProposalTasks) and Metadata set.
// contractTaskCount is COUNT of mcp_tasks for keys.ContractID (0 if unknown / none).
func BuildApprovePlan(proposalID, currentStatus string, proposal *smart_contract.Proposal, contractTaskCount int) (ApprovePlan, error) {
	if proposal == nil {
		return ApprovePlan{}, fmt.Errorf("proposal %s not found", proposalID)
	}
	if err := CheckProposalApprovable(proposalID, currentStatus); err != nil {
		return ApprovePlan{}, err
	}
	if err := ValidateProposalForApproval(proposal); err != nil {
		return ApprovePlan{}, fmt.Errorf("proposal validation failed: %v", err)
	}
	if len(proposal.Tasks) == 0 && contractTaskCount == 0 {
		return ApprovePlan{}, ErrApproveNoTasks()
	}
	keys := ResolveApproveKeys(proposal.Metadata, proposalID)
	return ApprovePlan{
		Keys:                    keys,
		WishToSupersede:         WishContractIDForSupersede(proposal.VisiblePixelHash, proposal.Metadata),
		NewProposalStatus:       smart_contract.ProposalStatusApproved,
		SetRelatedTasksApproved: true, // Memory applies; SQL ignores
	}, nil
}

// PublishPlan is the dialect-agnostic publish decision.
type PublishPlan struct {
	ContractID        string
	NewProposalStatus string
}

// BuildPublishPlan validates status and resolves the contract id for task/claim updates.
func BuildPublishPlan(proposalID, currentStatus string, meta map[string]interface{}) (PublishPlan, error) {
	if err := CheckProposalPublishable(proposalID, currentStatus); err != nil {
		return PublishPlan{}, err
	}
	return PublishPlan{
		ContractID:        contractIDFromMeta(meta, proposalID),
		NewProposalStatus: smart_contract.ProposalStatusPublished,
	}, nil
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

// SQL IN-list fragments for publish (shared across dialects; placeholders differ).
const (
	PublishTaskStatusSQL  = `'submitted','pending_review','claimed','approved'`
	PublishClaimStatusSQL = `'submitted','pending_review','active','approved'`
)
