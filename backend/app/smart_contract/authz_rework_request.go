package smart_contract

import (
	"context"

	"stargate-backend/services"
	auth "stargate-backend/storage/auth"
)

// ReworkRequestGate authorizes filing a rework request against a contract, for
// any surface. It is handed to ContractReworkService so the check runs inside the
// operation rather than beside it in each handler.
//
// A rework request records its requester as the wish creator, and both surfaces
// recorded whatever wallet the API key was bound to without checking
// (stargate-irl.8). The creator question is the same one WishCreatorAuthorizer
// already answers for approval and review, so this resolves the contract to its
// wish hash and defers to it rather than introducing a second notion of who owns
// a wish.
type ReworkRequestGate struct {
	Keys      auth.APIKeyValidator
	Ingestion *services.IngestionService
}

// AuthorizeReworkRequest reports whether apiKey's wallet may file a rework
// request against contractID, returning the wallet it authorized.
func (g ReworkRequestGate) AuthorizeReworkRequest(_ context.Context, apiKey, contractID string) (string, error) {
	hash, err := ContractWishHash(contractID)
	if err != nil {
		return "", err
	}
	return WishCreatorAuthorizer{Keys: g.Keys, Ingestion: g.Ingestion}.Authorize(apiKey, hash, "rework request for "+contractID)
}

// reworkRequestGate builds the gate from the server's dependencies.
func (s *Server) reworkRequestGate() ReworkRequestGate {
	return ReworkRequestGate{Keys: s.apiKeys, Ingestion: s.ingestionSvc}
}
