package smart_contract

import (
	"strings"

	coresc "stargate-backend/core/smart_contract"
)

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
