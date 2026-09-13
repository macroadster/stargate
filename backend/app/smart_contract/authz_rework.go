package smart_contract

import (
	"context"
	"fmt"
	"strings"

	auth "stargate-backend/storage/auth"
)

// SubmissionReworkGate authorizes reworking a submission against the claimant who
// did the work, and is handed to SubmissionService so the check runs inside Rework
// rather than in the handler that calls it (stargate-hs2).
//
// It is the mirror of SubmissionReviewGate: review belongs to the wish creator,
// rework belongs to the claimant. Keeping them as separate types means neither can
// be passed where the other is expected.
//
// It satisfies services.SubmissionReworkAuthorizer. The interface lives in
// services because services cannot import this package.
//
// The comparison is wallet to wallet. Claim.AiIdentifier is set from the claiming
// key's bound wallet (policy_claim.go), and claiming refuses a key with no wallet
// binding at all, so the recorded claimant is always a key-bound wallet.
type SubmissionReworkGate struct {
	Store Store
	Keys  auth.APIKeyValidator
}

// AuthorizeSubmissionRework reports whether apiKey may rework the given
// submission, returning the wallet it authorized.
func (g SubmissionReworkGate) AuthorizeSubmissionRework(ctx context.Context, apiKey, submissionID string) (string, error) {
	wallet := ""
	if g.Keys != nil {
		if rec, ok := g.Keys.Get(strings.TrimSpace(apiKey)); ok {
			wallet = strings.TrimSpace(rec.Wallet)
		}
	}
	if wallet == "" {
		return "", fmt.Errorf("api key with wallet binding required to rework submission %s", submissionID)
	}

	sub, err := g.Store.GetSubmission(ctx, submissionID)
	if err != nil || sub.SubmissionID == "" {
		return "", fmt.Errorf("submission %s not found", submissionID)
	}

	claimID := strings.TrimSpace(sub.ClaimID)
	if claimID == "" {
		// Without a claim there is no recorded claimant to compare against, so
		// there is nothing to authorize against and it stays closed.
		return "", fmt.Errorf("submission %s has no claim, cannot establish the claimant", submissionID)
	}
	claim, err := g.Store.GetClaim(claimID)
	if err != nil {
		return "", fmt.Errorf("cannot resolve claim %s for submission %s: %w", claimID, submissionID, err)
	}

	claimant := strings.TrimSpace(claim.AiIdentifier)
	if claimant == "" {
		return "", fmt.Errorf("claim %s records no claimant, cannot authorize rework", claimID)
	}
	if !strings.EqualFold(claimant, wallet) {
		return "", fmt.Errorf("wallet %s is not the claimant of submission %s", wallet, submissionID)
	}
	return claimant, nil
}

// reworkGate builds the gate from the server's dependencies.
func (s *Server) reworkGate() SubmissionReworkGate {
	return SubmissionReworkGate{Store: s.store, Keys: s.apiKeys}
}
