package smart_contract

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	scservices "stargate-backend/app/smart_contract/services"
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

// MissingCreatorPolicy selects what happens when no creator wallet can be
// established for a wish. This is a real fork in behaviour, not a detail:
// records replicated from peers never carry a creator (see wishOwnership), so
// for those wishes this policy alone decides the outcome.
type MissingCreatorPolicy int

const (
	// DenyOnMissingCreator refuses the action. Required for anything that
	// releases funds, where an unidentifiable approver must not be honoured.
	DenyOnMissingCreator MissingCreatorPolicy = iota

	// AllowOnMissingCreator permits the action and logs it. Retained for
	// proposal approval, whose pre-existing allowance predates this
	// consolidation; stargate-irl.2 tracks closing it.
	AllowOnMissingCreator
)

// Authorize reports whether apiKey's bound wallet may act as the creator of the
// wish identified by visibleHash. subject names what is being acted on and is
// used only in log and error messages. policy decides the unresolvable case.
func (a WishCreatorAuthorizer) Authorize(apiKey, visibleHash, subject string, policy MissingCreatorPolicy) error {
	wallet := a.boundWallet(apiKey)
	if wallet == "" {
		return fmt.Errorf("api key with wallet binding required to approve %s", subject)
	}

	if a.isGlobalAuditor(wallet) {
		log.Printf("AUTHORIZATION: Allowing %s based on Global Auditor status (%s)", subject, wallet)
		return nil
	}

	own := a.wishOwnership(visibleHash)
	if own.known {
		if strings.EqualFold(own.creator, wallet) {
			return nil
		}
		return fmt.Errorf("approver wallet %s does not match wish creator", wallet)
	}

	if policy == AllowOnMissingCreator {
		log.Printf("WARNING: allowing %s with NO wish creator info", subject)
		return nil
	}

	// A replicated wish is the expected reason to land here, so say so rather
	// than reporting it as missing data: the creator exists, just not on this
	// node, and approval belongs on the node that holds their key.
	if own.replicated {
		return fmt.Errorf("cannot approve %s: wish %s was replicated from another node and carries no creator wallet; approve it on the node that created it", subject, own.hash)
	}
	return fmt.Errorf("cannot approve %s: no creator wallet recorded for wish %s", subject, own.hash)
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

// wishOwnership is what the authorizer could establish about a wish. known
// distinguishes "no creator recorded" from "creator recorded as empty", which
// callers treat differently, and replicated explains why a creator is absent.
type wishOwnership struct {
	hash       string
	creator    string
	known      bool
	replicated bool
}

// wishOwnership looks up what is recorded about the creator of visibleHash.
//
// Only the local wish path records creator_wallet (inscription_handler.go). The
// stego reconcile and IPFS sync paths do not, because the only creator identity
// on the wire is the stego payload, which is attacker-controlled and therefore
// excluded from stegoPayloadMetadataAllowlist. manifest.Issuer is not a
// substitute: it is STARGATE_STEGO_ISSUER, a free-form node label, so trusting
// it would let any replicating node claim authorship of every wish it mirrors.
// Replicated wishes consequently have no locally verifiable creator at all.
func (a WishCreatorAuthorizer) wishOwnership(visibleHash string) wishOwnership {
	own := wishOwnership{hash: strings.TrimSpace(visibleHash)}
	if own.hash == "" || a.Ingestion == nil {
		return own
	}

	rec, err := a.Ingestion.Get(own.hash)
	if err != nil {
		rec, _ = a.Ingestion.Get("wish-" + own.hash)
	}
	if rec == nil || rec.Metadata == nil {
		return own
	}

	own.replicated, _ = rec.Metadata["stego_replicated"].(bool)

	creator, ok := rec.Metadata["creator_wallet"].(string)
	if !ok {
		return own
	}
	own.creator, own.known = strings.TrimSpace(creator), true
	return own
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
// Submission.TaskID is omitempty while ClaimID is not, so a submission may
// identify its task only through its claim; those must resolve rather than be
// denied, now that callers fail closed on an unresolvable wish.
//
// It returns an error rather than an empty hash when the chain cannot be walked,
// so callers fail closed instead of authorizing against an unknown wish.
func SubmissionWishHash(store Store, sub smart_contract.Submission) (string, error) {
	taskID, err := scservices.SubmissionTaskID(store, sub)
	if err != nil {
		return "", fmt.Errorf("cannot resolve wish creator: %w", err)
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
//
// Submission review is the payout gate, so it denies on an unresolvable
// creator instead of taking proposal approval's compatibility allowance.
func (s *Server) AuthorizeSubmissionReview(ctx context.Context, apiKey, submissionID string) error {
	sub, err := s.store.GetSubmission(ctx, submissionID)
	if err != nil || sub.SubmissionID == "" {
		return fmt.Errorf("submission %s not found", submissionID)
	}

	hash, err := SubmissionWishHash(s.store, sub)
	if err != nil {
		return err
	}
	return s.authorizer().Authorize(apiKey, hash, "submission "+submissionID, DenyOnMissingCreator)
}
