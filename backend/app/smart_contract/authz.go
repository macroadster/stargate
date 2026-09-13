package smart_contract

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	auth "stargate-backend/storage/auth"
)

// WishCreatorAuthorizer decides whether the wallet bound to an API key may act
// on behalf of the creator of a wish.
//
// Both the REST handlers and the MCP tool surface authorize against this type so
// the rule exists in one place; previously enforceCreatorApproval and
// requireAuthorizedApprover carried separate copies that could drift.
type WishCreatorAuthorizer struct {
	Keys      auth.APIKeyValidator
	Ingestion *services.IngestionService
}

// Authorize reports whether apiKey's bound wallet may act as the creator of the
// wish identified by visibleHash. subject names what is being acted on and is
// used only in log and error messages.
func (a WishCreatorAuthorizer) Authorize(apiKey, visibleHash, subject string) error {
	wallet := a.boundWallet(apiKey)
	if wallet == "" {
		return fmt.Errorf("api key with wallet binding required to approve %s", subject)
	}

	if a.isGlobalAuditor(wallet) {
		log.Printf("AUTHORIZATION: Allowing %s based on Global Auditor status (%s)", subject, wallet)
		return nil
	}

	creator, known := a.wishCreator(visibleHash)
	if known {
		if strings.EqualFold(creator, wallet) {
			return nil
		}
		return fmt.Errorf("approver wallet %s does not match wish creator", wallet)
	}

	// Deliberate fail-open, retained from the original implementations so this
	// consolidation is behaviour-preserving. Closing it is tracked separately as
	// stargate-irl.2; it is now one branch rather than two.
	log.Printf("WARNING: allowing %s with NO wish creator info", subject)
	return nil
}

// boundWallet returns the wallet bound to apiKey, or "" when the key is unknown
// or has no wallet binding.
func (a WishCreatorAuthorizer) boundWallet(apiKey string) string {
	if a.Keys == nil {
		return ""
	}
	rec, ok := a.Keys.Get(apiKey)
	if !ok {
		return ""
	}
	return strings.TrimSpace(rec.Wallet)
}

// isGlobalAuditor reports whether wallet is the configured donation address,
// which is treated as a node-wide approver.
func (a WishCreatorAuthorizer) isGlobalAuditor(wallet string) bool {
	donationAddr := strings.TrimSpace(os.Getenv("STARLIGHT_DONATION_ADDRESS"))
	return donationAddr != "" && strings.EqualFold(wallet, donationAddr)
}

// wishCreator looks up the creator wallet recorded for visibleHash. The second
// return distinguishes "no creator recorded" from "creator recorded as empty",
// because the caller treats those differently.
func (a WishCreatorAuthorizer) wishCreator(visibleHash string) (string, bool) {
	visibleHash = strings.TrimSpace(visibleHash)
	if visibleHash == "" || a.Ingestion == nil {
		return "", false
	}

	rec, err := a.Ingestion.Get(visibleHash)
	if err != nil {
		rec, _ = a.Ingestion.Get("wish-" + visibleHash)
	}
	if rec == nil || rec.Metadata == nil {
		return "", false
	}

	creator, ok := rec.Metadata["creator_wallet"].(string)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(creator), true
}

// ProposalWishHash returns the visible pixel hash a proposal refers to, checking
// the dedicated field before falling back to metadata.
func ProposalWishHash(p smart_contract.Proposal) string {
	if h := strings.TrimSpace(p.VisiblePixelHash); h != "" {
		return h
	}
	if v, ok := p.Metadata["visible_pixel_hash"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// SubmissionWishHash resolves the wish a submission belongs to by walking
// submission -> task -> contract, since Submission carries no wish reference of
// its own. Contract IDs use the "wish-<hash>" form.
//
// It returns an error rather than an empty hash when the chain cannot be walked,
// so callers fail closed instead of authorizing against an unknown wish.
func SubmissionWishHash(store Store, sub smart_contract.Submission) (string, error) {
	taskID := strings.TrimSpace(sub.TaskID)
	if taskID == "" {
		return "", fmt.Errorf("submission %s has no task, cannot resolve wish creator", sub.SubmissionID)
	}

	task, err := store.GetTask(taskID)
	if err != nil {
		return "", fmt.Errorf("cannot resolve task %s for submission %s: %w", taskID, sub.SubmissionID, err)
	}

	contractID := strings.TrimSpace(task.ContractID)
	if contractID == "" {
		return "", fmt.Errorf("task %s has no contract, cannot resolve wish creator", taskID)
	}

	hash := strings.TrimSpace(strings.TrimPrefix(contractID, "wish-"))
	if hash == "" {
		return "", fmt.Errorf("contract %s does not identify a wish", contractID)
	}
	return hash, nil
}

// authorizer builds an authorizer from the server's dependencies.
func (s *Server) authorizer() WishCreatorAuthorizer {
	return WishCreatorAuthorizer{Keys: s.apiKeys, Ingestion: s.ingestionSvc}
}

// AuthorizeSubmissionReview reports whether apiKey may review (approve, reject
// or mark reviewed) the given submission. Exported so the MCP tool surface
// enforces the same rule as the REST route.
func (s *Server) AuthorizeSubmissionReview(ctx context.Context, apiKey, submissionID string) error {
	sub, err := s.store.GetSubmission(ctx, submissionID)
	if err != nil || sub.SubmissionID == "" {
		return fmt.Errorf("submission %s not found", submissionID)
	}

	hash, err := SubmissionWishHash(s.store, sub)
	if err != nil {
		return err
	}
	return s.authorizer().Authorize(apiKey, hash, "submission "+submissionID)
}
