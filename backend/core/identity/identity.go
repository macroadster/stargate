// Package identity defines the shared wish / proposal / contract identity model.
//
// Visible pixel hashes (64-char hex) are the stable join key and the stored
// contract primary key across:
//   - inscriptions / ingestion records
//   - wishes / contracts (bare hash; wish-<hash> is a lookup alias only)
//   - proposals (metadata.visible_pixel_hash)
//   - stego manifests (visible_pixel_hash + proposal_id)
//   - on-chain commitments (hashlock / OP_RETURN linkage)
//
// Bitcoin reconciliation, stego publish/reconcile, and PSBT flows must resolve
// IDs through these helpers rather than inventing ad-hoc prefix rules.
package identity

import (
	"strings"
)

// Normalize strips common prefixes (wish-, proposal-, task-) once.
func Normalize(id string) string {
	id = strings.TrimSpace(id)
	for _, prefix := range []string{"wish-", "proposal-", "task-"} {
		if strings.HasPrefix(id, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(id, prefix))
		}
	}
	return id
}

// ToWishID returns wish-<normalized-hash>.
// Historical alias only — new writes persist CanonicalContractID.
func ToWishID(hash string) string {
	n := Normalize(hash)
	if n == "" {
		return ""
	}
	if strings.HasPrefix(strings.TrimSpace(hash), "wish-") && Normalize(hash) == n {
		// already wish- form after normalize of inner — rebuild for consistency
	}
	return "wish-" + n
}

// CanonicalContractID is the stored contract primary key.
// For a 64-hex visible pixel hash (with or without a wish- prefix) this is the
// bare lowercase hash. The legacy publish alias contract-<64-hex> is the same
// key: ContractIDFromMeta used to mint it when a wish proposal had no
// contract_id. Other ids, including contract-001, are returned trimmed and
// unchanged. Normalize does not strip contract-; doing so would rename
// non-hash contract ids.
func CanonicalContractID(id string) string {
	id = strings.TrimSpace(id)
	n := Normalize(id)
	if IsPixelHash(n) {
		return strings.ToLower(n)
	}
	const legacy = "contract-"
	if strings.HasPrefix(strings.ToLower(n), legacy) {
		rest := n[len(legacy):]
		if IsPixelHash(rest) {
			return strings.ToLower(rest)
		}
	}
	return id
}

// ResultsDirKey is the on-disk results directory name for a wish.
// It prefers visiblePixelHash, then contractID. wish-<hash> and
// contract-<hash> both resolve to the bare lowercase hash. The second
// result is false when neither value is a 64-char pixel hash, so callers
// do not shard an arbitrary string into a path the HTTP reader cannot serve.
func ResultsDirKey(visiblePixelHash, contractID string) (string, bool) {
	if n := CanonicalContractID(visiblePixelHash); IsPixelHash(n) {
		return n, true
	}
	if n := CanonicalContractID(contractID); IsPixelHash(n) {
		return n, true
	}
	return "", false
}

// IsPixelHash reports whether s looks like a 64-char hex pixel/stego hash.
func IsPixelHash(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// CandidateIDs returns unique identifiers to try when resolving contracts/proposals
// from a visible hash and/or ingestion id (bare and wish- prefixed forms).
func CandidateIDs(visible, ingestionID string) []string {
	seen := make(map[string]struct{})
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
	add(visible)
	add(ingestionID)
	for _, base := range []string{visible, ingestionID} {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		n := Normalize(base)
		add(n)
		if !strings.HasPrefix(base, "wish-") {
			add("wish-" + n)
		}
		add(ToWishID(base))
	}
	return out
}

// ExpandWishVariants returns id and its wish-/bare variants (for UI/API matching).
func ExpandWishVariants(id string) []string {
	return CandidateIDs(id, "")
}
