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
// the rule exists in one place; the REST and MCP approval paths used to carry
// separate copies that could drift.
type WishCreatorAuthorizer struct {
	Keys      auth.APIKeyValidator
	Ingestion *services.IngestionService
}

// Authorize reports whether apiKey's bound wallet may act as the creator of the
// wish identified by visibleHash. subject names what is being acted on and is
// used only in log and error messages.
//
// An unestablishable creator denies, for proposals as well as submissions. There
// is deliberately no override: proposal approval used to log a warning and allow
// it through, which let any wallet-bound key approve wishes whose ingest carried
// missing or partial creator metadata. An env-gated escape hatch would be
// explicit but would restore that hole in the first deployment to meet a legacy
// ingest, so the allowance is gone rather than configurable (stargate-irl.2).
//
// It returns the wallet it authorized, so callers that record who acted use the
// identity authorization actually accepted rather than one passed alongside it.
// Those can disagree; the audit trail should not be able to.
func (a WishCreatorAuthorizer) Authorize(apiKey, visibleHash, subject string) (string, error) {
	wallet := a.boundWallet(apiKey)
	if wallet == "" {
		return "", fmt.Errorf("api key with wallet binding required to approve %s", subject)
	}

	if a.isGlobalAuditor(wallet) {
		log.Printf("AUTHORIZATION: Allowing %s based on Global Auditor status (%s)", subject, wallet)
		return wallet, nil
	}

	own := a.wishOwnership(visibleHash)
	if own.known {
		if strings.EqualFold(own.creator, wallet) {
			return wallet, nil
		}
		return "", fmt.Errorf("approver wallet %s does not match wish creator", wallet)
	}

	// A replicated wish is the expected reason to land here, so say so rather
	// than reporting it as missing data: the creator exists, just not on this
	// node, and approval belongs on the node that holds their key.
	if own.replicated {
		return "", fmt.Errorf("cannot approve %s: wish %s was replicated from another node and carries no creator wallet; approve it on the node that created it", subject, own.hash)
	}
	return "", fmt.Errorf("cannot approve %s: no creator wallet recorded for wish %s", subject, own.hash)
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

	// An empty value does not establish a creator. inscription_handler.go writes
	// creator_wallet unconditionally, including when no creator key resolved, and
	// "" type-asserts as a string just as well as a wallet does. Treating that as
	// known meant the denial read "approver wallet X does not match wish creator",
	// which asserts a creator exists and sends the operator looking for a
	// different key, when nothing about the wish records who made it
	// (stargate-b11). Every case denied before and still denies; only the
	// explanation changes.
	creator, ok := rec.Metadata["creator_wallet"].(string)
	if !ok {
		return own
	}
	if creator = strings.TrimSpace(creator); creator == "" {
		return own
	}
	own.creator, own.known = creator, true
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
	return ContractWishHash(contractID)
}

// ContractWishHash returns the visible pixel hash a contract belongs to.
// Contract IDs use the "wish-<hash>" form.
func ContractWishHash(contractID string) (string, error) {
	hash := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(contractID), "wish-"))
	if hash == "" {
		return "", fmt.Errorf("contract %s does not identify a wish", contractID)
	}
	return hash, nil
}

// authorizer builds an authorizer from the server's dependencies.
func (s *Server) authorizer() WishCreatorAuthorizer {
	return WishCreatorAuthorizer{Keys: s.apiKeys, Ingestion: s.ingestionSvc}
}

// SubmissionReviewGate authorizes submission review for any surface. It is
// handed to SubmissionService so the check runs inside the review itself rather
// than in each handler that calls it: a new caller that forgets to authorize is
// then refused by construction instead of silently approving payouts.
//
// It satisfies services.SubmissionReviewAuthorizer. The interface lives in
// services because services cannot import this package.
type SubmissionReviewGate struct {
	Store     Store
	Keys      auth.APIKeyValidator
	Ingestion *services.IngestionService
}

// AuthorizeSubmissionReview reports whether apiKey may review (approve, reject
// or mark reviewed) the given submission, returning the wallet it authorized.
func (g SubmissionReviewGate) AuthorizeSubmissionReview(ctx context.Context, apiKey, submissionID string) (string, error) {
	sub, err := g.Store.GetSubmission(ctx, submissionID)
	if err != nil || sub.SubmissionID == "" {
		return "", fmt.Errorf("submission %s not found", submissionID)
	}

	hash, err := SubmissionWishHash(g.Store, sub)
	if err != nil {
		return "", err
	}
	return WishCreatorAuthorizer{Keys: g.Keys, Ingestion: g.Ingestion}.
		Authorize(apiKey, hash, "submission "+submissionID)
}

// submissionGate builds the gate from the server's dependencies.
func (s *Server) submissionGate() SubmissionReviewGate {
	return SubmissionReviewGate{Store: s.store, Keys: s.apiKeys, Ingestion: s.ingestionSvc}
}

// ProposalEditGate authorizes editing and publishing a proposal against the
// creator of the wish it refers to. It is handed to ProposalService for the same
// reason as SubmissionReviewGate: publish and PATCH each reached the store with
// no check at all, and one of them sat beside an approve path that did check
// (stargate-irl.7).
//
// It satisfies services.ProposalEditAuthorizer.
type ProposalEditGate struct {
	Store     Store
	Keys      auth.APIKeyValidator
	Ingestion *services.IngestionService
}

// AuthorizeProposalEdit reports whether apiKey may edit or publish the given
// proposal, returning the wallet it authorized.
func (g ProposalEditGate) AuthorizeProposalEdit(ctx context.Context, apiKey, proposalID string) (string, error) {
	proposal, err := g.Store.GetProposal(ctx, proposalID)
	if err != nil || proposal.ID == "" {
		return "", fmt.Errorf("proposal %s not found", proposalID)
	}

	hash := ProposalWishHash(proposal)
	if hash == "" {
		// A proposal that names no wish has no creator to compare against, so
		// there is nothing to authorize against and it stays closed.
		return "", fmt.Errorf("proposal %s does not identify a wish", proposalID)
	}
	return WishCreatorAuthorizer{Keys: g.Keys, Ingestion: g.Ingestion}.
		Authorize(apiKey, hash, "proposal "+proposalID)
}

// proposalGate builds the gate from the server's dependencies.
func (s *Server) proposalGate() ProposalEditGate {
	return ProposalEditGate{Store: s.store, Keys: s.apiKeys, Ingestion: s.ingestionSvc}
}

// AuthorizeContractOwner reports whether apiKey's wallet may act on contractID,
// returning the wallet it authorized. Tasks are created against a contract, so
// ownership of the contract's wish is what governs writing to it.
func AuthorizeContractOwner(keys auth.APIKeyValidator, ingestion *services.IngestionService, apiKey, contractID string) (string, error) {
	hash, err := ContractWishHash(contractID)
	if err != nil {
		return "", err
	}
	return WishCreatorAuthorizer{Keys: keys, Ingestion: ingestion}.
		Authorize(apiKey, hash, "contract "+contractID)
}
