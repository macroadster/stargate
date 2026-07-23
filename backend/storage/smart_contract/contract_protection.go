package smart_contract

import (
	"strings"

	"stargate-backend/core/identity"
	coresc "stargate-backend/core/smart_contract"
)

// ConfirmContractIDPlan describes how ConfirmContract should resolve a caller's
// contract_id into a single confirmed row (and which aliases to collapse).
//
// Historical bug: ConfirmContract bootstrapped a bare-hash confirmed row from
// wish-<hash> and then tried to supersede the wish. After integrity hardening,
// confirmed wishes cannot be demoted via supersedeEligibleStatusSQL — so both
// rows stayed status=confirmed and /contracts listed the wish twice.
//
// For 64-hex pixel hashes the canonical id is wish-<hash> (identity.ToWishID).
// Confirm prefers that row; bare-hash aliases are force-superseded after confirm.
type ConfirmContractIDPlan struct {
	// Normalized bare hash (or non-pixel id with prefixes stripped once).
	Normalized string
	// Preferred row to confirm for pixel-hash wishes.
	Canonical string
	// Other ids that may exist historically (bare hash) and must not stay confirmed.
	Aliases []string
	// True when Normalized is a 64-char hex pixel/stego hash.
	IsPixelHash bool
}

// PlanConfirmContractIDs returns the canonical confirm target and aliases.
func PlanConfirmContractIDs(contractID string) ConfirmContractIDPlan {
	contractID = strings.TrimSpace(contractID)
	normalized := identity.Normalize(contractID)
	if normalized == "" {
		return ConfirmContractIDPlan{}
	}
	if identity.IsPixelHash(normalized) {
		canonical := identity.ToWishID(normalized)
		aliases := []string{}
		if normalized != canonical {
			aliases = append(aliases, normalized)
		}
		// If caller passed a non-canonical form we still try it before bootstrap.
		return ConfirmContractIDPlan{
			Normalized:  normalized,
			Canonical:   canonical,
			Aliases:     aliases,
			IsPixelHash: true,
		}
	}
	return ConfirmContractIDPlan{
		Normalized:  normalized,
		Canonical:   contractID,
		Aliases:     nil,
		IsPixelHash: false,
	}
}

// ConfirmTryOrder is the preference order of existing rows to UPDATE.
// Pixel hashes: canonical wish- first, then caller id, then bare hash.
func (p ConfirmContractIDPlan) ConfirmTryOrder(callerID string) []string {
	callerID = strings.TrimSpace(callerID)
	seen := map[string]struct{}{}
	var out []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if p.IsPixelHash {
		add(p.Canonical)
		add(callerID)
		add(p.Normalized)
		return out
	}
	add(callerID)
	return out
}

// Contract statuses that must never be demoted or field-overwritten by
// untrusted replication paths (stego/IPFS reconcile, naive upserts).
var protectedContractStatuses = map[string]struct{}{
	"confirmed":  {},
	"completed":  {},
	"superseded": {},
}

// terminalContractStatuses cannot be superseded by proposal approval.
// Confirmed/completed wishes stay put; already-superseded is a no-op target.
var nonSupersedableContractStatuses = map[string]struct{}{
	"confirmed": {},
	"completed": {},
}

// IsProtectedContractStatus reports whether status is immutable under upsert.
func IsProtectedContractStatus(status string) bool {
	_, ok := protectedContractStatuses[strings.ToLower(strings.TrimSpace(status))]
	return ok
}

// IsNonSupersedableContractStatus reports whether a wish must not be marked superseded.
func IsNonSupersedableContractStatus(status string) bool {
	_, ok := nonSupersedableContractStatuses[strings.ToLower(strings.TrimSpace(status))]
	return ok
}

// ProtectContractUpsert merges incoming contract fields onto existing when the
// existing row is protected (confirmed/completed/superseded). Title, budget,
// and status stay; non-empty stego URL and skills may still refresh for display.
func ProtectContractUpsert(existing, incoming coresc.Contract) coresc.Contract {
	if !IsProtectedContractStatus(existing.Status) {
		return incoming
	}
	out := existing
	// Allow stego image URL refresh so peers can locate product artifacts.
	if strings.TrimSpace(incoming.StegoImageURL) != "" {
		out.StegoImageURL = incoming.StegoImageURL
	}
	if len(incoming.Skills) > 0 {
		out.Skills = incoming.Skills
	}
	// Merge metadata without dropping existing keys.
	if incoming.Metadata != nil {
		if out.Metadata == nil {
			out.Metadata = map[string]interface{}{}
		}
		for k, v := range incoming.Metadata {
			if _, exists := out.Metadata[k]; !exists {
				out.Metadata[k] = v
			}
		}
	}
	if incoming.AvailableTasksCount > out.AvailableTasksCount {
		out.AvailableTasksCount = incoming.AvailableTasksCount
	}
	return out
}

// SQL predicate fragment: status values that may be superseded (SQLite/PG).
// Used as: AND lower(status) NOT IN (...)
const supersedeEligibleStatusSQL = `lower(status) NOT IN ('confirmed','completed','superseded')`
