package services

// Kind names a domain failure precisely enough for a caller to translate it
// into its own error taxonomy. Status alone is too coarse: a missing wish and a
// malformed request are both 400 to REST, but the MCP surface has to answer
// RESOURCE_NOT_FOUND for one and a validation error for the other. Callers
// matched on message text before this existed, which broke whenever the wording
// changed.
type Kind string

const (
	// KindWishNotFound marks a failure caused by the referenced wish being
	// absent, rather than by the request being malformed.
	KindWishNotFound Kind = "wish_not_found"
	// KindProposalNotFound marks a failure caused by the proposal itself being
	// absent.
	KindProposalNotFound Kind = "proposal_not_found"
	// KindBudgetExceeded marks a proposal budget larger than the wish it answers.
	KindBudgetExceeded Kind = "budget_exceeded"
	// KindBudgetMismatch marks task budgets that do not sum to the proposal
	// budget, in either direction.
	KindBudgetMismatch Kind = "budget_mismatch"
	// KindProposalLimitReached marks the per-wish proposal cap being exhausted.
	KindProposalLimitReached Kind = "proposal_limit_reached"
	// KindProposalAlreadyFinalized marks a wish that already has an approved or
	// published proposal and accepts no more.
	KindProposalAlreadyFinalized Kind = "proposal_already_finalized"
	// KindContractNotFound marks a failure caused by the referenced contract
	// being absent.
	KindContractNotFound Kind = "contract_not_found"
)

// StatusError is a domain error with an HTTP-oriented status code.
type StatusError struct {
	Status  int
	Message string
	// Kind is optional. It carries no meaning for REST, which maps on Status
	// alone, so adding one never changes an existing response.
	Kind Kind
}

func (e *StatusError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Fail constructs a StatusError.
func Fail(status int, message string) *StatusError {
	return &StatusError{Status: status, Message: message}
}

// FailKind constructs a StatusError carrying a Kind, for failures a caller may
// need to distinguish from others sharing the same status.
func FailKind(status int, kind Kind, message string) *StatusError {
	return &StatusError{Status: status, Message: message, Kind: kind}
}

// Failf constructs a StatusError with formatting.

// AsStatus extracts StatusError from err, if present.
func AsStatus(err error) *StatusError {
	if err == nil {
		return nil
	}
	if se, ok := err.(*StatusError); ok {
		return se
	}
	return nil
}
