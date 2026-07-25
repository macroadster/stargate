package smart_contract

import (
	"fmt"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
)

// ConfirmApply describes pure confirm side-effects after a target row is chosen.
type ConfirmApply struct {
	// Plan is the id resolution from PlanConfirmContractIDs.
	Plan ConfirmContractIDPlan
	// Normalized bare / stripped id.
	Normalized string
	// Canonical wish id (or wish- prefix for non-pixel).
	WishID string
	// StegoImageURL for block-image API path.
	StegoImageURL string
	// ImageFileKey used in the URL (bare hash).
	ImageFileKey string
	// AliasesToForceSupersede after pixel confirm (bare hash twins).
	AliasesToForceSupersede []string
	// SupersedeWishIfNonPixel when confirming a non-pixel id, supersede wish- form if eligible.
	SupersedeWishIfNonPixel bool
	// ProposalMatchIDs for setting proposals → confirmed (normalized + wishID).
	ProposalMatchIDs []string
}

// BuildConfirmApply builds shared confirm planning (ids + stego URL + alias collapse).
// imageFileOverride is optional (e.g. from ingestion metadata); empty → BlockImageFileKey.
func BuildConfirmApply(contractID string, blockHeight int, imageFileOverride string) ConfirmApply {
	contractID = strings.TrimSpace(contractID)
	plan := PlanConfirmContractIDs(contractID)
	normalized := plan.Normalized
	wishID := plan.Canonical
	if !plan.IsPixelHash {
		if normalized != "" {
			wishID = "wish-" + normalized
		}
	}

	imageFile := strings.TrimSpace(imageFileOverride)
	if imageFile == "" {
		imageFile = BlockImageFileKey(contractID)
	}
	if strings.HasPrefix(imageFile, "wish-") {
		imageFile = strings.TrimPrefix(imageFile, "wish-")
	}
	if imageFile == "" {
		imageFile = contractID
	}

	stego := ""
	if imageFile != "" && blockHeight > 0 {
		stego = fmt.Sprintf("/api/block-image/%d/%s", blockHeight, imageFile)
	} else if imageFile != "" {
		stego = fmt.Sprintf("/api/block-image/%d/%s", blockHeight, imageFile)
	}

	out := ConfirmApply{
		Plan:             plan,
		Normalized:       normalized,
		WishID:           wishID,
		StegoImageURL:    stego,
		ImageFileKey:     imageFile,
		ProposalMatchIDs: []string{normalized, wishID},
	}
	if plan.IsPixelHash {
		out.AliasesToForceSupersede = append([]string{}, plan.Aliases...)
	} else {
		out.SupersedeWishIfNonPixel = true
	}
	return out
}

// MergeConfirmMetadata writes confirmed_txid / height into a metadata map (copy-safe).
func MergeConfirmMetadata(existing map[string]interface{}, txid string, blockHeight int) map[string]interface{} {
	meta := map[string]interface{}{}
	for k, v := range existing {
		meta[k] = v
	}
	if meta == nil {
		meta = map[string]interface{}{}
	}
	meta["confirmed_txid"] = txid
	meta["confirmed_block_height"] = blockHeight
	return meta
}

// ApplyConfirmToContract mutates a contract struct for in-memory confirm.
func ApplyConfirmToContract(c *smart_contract.Contract, targetID string, blockHeight int, txid, stegoImageURL string, now time.Time) {
	if c == nil {
		return
	}
	c.ContractID = targetID
	c.Status = "confirmed"
	c.ConfirmedBlockHeight = &blockHeight
	t := now
	c.ConfirmedAt = &t
	if c.Metadata == nil {
		c.Metadata = map[string]interface{}{}
	}
	c.Metadata["confirmed_txid"] = txid
	c.Metadata["confirmed_block_height"] = blockHeight
	if stegoImageURL != "" {
		c.StegoImageURL = stegoImageURL
	}
}

// ProposalShouldConfirmOnChain is true when an approved proposal matches the confirmed contract keys.
func ProposalShouldConfirmOnChain(proposalStatus, proposalID, proposalVisible string, meta map[string]interface{}, normalized, wishID string) bool {
	if !strings.EqualFold(strings.TrimSpace(proposalStatus), smart_contract.ProposalStatusApproved) {
		return false
	}
	cid := NormalizeContractID(contractIDFromMeta(meta, proposalID))
	if cid != "" && (cid == normalized || cid == strings.TrimPrefix(wishID, "wish-") || "wish-"+cid == wishID) {
		return true
	}
	if strings.EqualFold(proposalID, normalized) || strings.EqualFold(proposalID, wishID) {
		return true
	}
	vis := strings.TrimSpace(proposalVisible)
	if vis == "" && meta != nil {
		if v, ok := meta["visible_pixel_hash"].(string); ok {
			vis = strings.TrimSpace(v)
		}
	}
	return vis != "" && (strings.EqualFold(vis, normalized) || strings.EqualFold(vis, wishID))
}
