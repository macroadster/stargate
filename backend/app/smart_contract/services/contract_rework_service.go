package services

import (
	"context"
	"net/http"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// ReworkRequestAuthorizer decides whether an actor may file a rework request
// against a contract, and returns the wallet it authorized.
//
// The wallet is returned rather than taken from the caller because Requester is
// documented as the wish creator's address. A caller that resolves its own
// wallet and passes it alongside can disagree with what authorization accepted;
// the recorded requester should not be able to.
type ReworkRequestAuthorizer interface {
	AuthorizeReworkRequest(ctx context.Context, apiKey, contractID string) (string, error)
}

// ReworkRequestActor names who is filing the request.
type ReworkRequestActor struct {
	APIKey string
}

// ContractReworkService owns filing rework requests against a contract.
//
// Both surfaces previously called store.CreateContractReworkRequest directly and
// recorded whatever wallet the API key happened to be bound to, with no check
// that it belonged to the wish creator. Any self-issued key could therefore file
// a request against any contract and be stored as its creator
// (stargate-irl.8). Rework requests are consumed by
// SubmissionService.maybeResolveRework, so this is workflow-affecting rather
// than a cosmetic field.
type ContractReworkService struct {
	store scstore.Store
	authz ReworkRequestAuthorizer
	emit  EventRecorder
}

// NewContractReworkService builds the service. A nil authorizer makes filing
// fail closed rather than unauthorized.
func NewContractReworkService(store scstore.Store, authz ReworkRequestAuthorizer, emit EventRecorder) *ContractReworkService {
	return &ContractReworkService{store: store, authz: authz, emit: emit}
}

// CreateRequest files a rework request after confirming the actor may act as the
// contract's wish creator.
func (s *ContractReworkService) CreateRequest(ctx context.Context, contractID, notes string, actor ReworkRequestActor) (smart_contract.ContractReworkRequest, error) {
	if strings.TrimSpace(contractID) == "" {
		return smart_contract.ContractReworkRequest{}, Fail(http.StatusBadRequest, "contract_id is required")
	}
	if strings.TrimSpace(notes) == "" {
		return smart_contract.ContractReworkRequest{}, Fail(http.StatusBadRequest, "notes are required")
	}
	if s.authz == nil {
		// Refusing is the only safe answer: an unconfigured authorizer cannot
		// establish that this wallet is the creator, and filing anyway is the
		// bug being fixed.
		return smart_contract.ContractReworkRequest{}, Fail(http.StatusForbidden, "rework request authorization is not configured")
	}

	wallet, err := s.authz.AuthorizeReworkRequest(ctx, actor.APIKey, contractID)
	if err != nil {
		return smart_contract.ContractReworkRequest{}, Fail(http.StatusForbidden, err.Error())
	}

	// wallet, not a wallet the caller resolved for itself.
	req, err := s.store.CreateContractReworkRequest(ctx, contractID, wallet, notes)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return smart_contract.ContractReworkRequest{}, FailKind(http.StatusNotFound, KindContractNotFound, err.Error())
		}
		return smart_contract.ContractReworkRequest{}, Fail(http.StatusInternalServerError, err.Error())
	}

	s.emitEvent(smart_contract.Event{
		Type:      "contract_rework_requested",
		EntityID:  contractID,
		Actor:     wallet,
		Message:   "rework requested",
		CreatedAt: time.Now(),
	})
	return req, nil
}

func (s *ContractReworkService) emitEvent(evt smart_contract.Event) {
	if s.emit == nil {
		return
	}
	s.emit(evt)
}
