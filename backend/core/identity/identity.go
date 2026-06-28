// Package identity defines the shared wish / proposal / contract identity model.
//
// Visible pixel hashes (64-char hex) are the stable join key across:
//   - inscriptions / ingestion records
//   - wishes (contract ID wish-<hash>)
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

// ContractIDFromVisibleHash returns the canonical wish-prefixed contract ID when
// the input is a bare pixel hash; otherwise returns the trimmed input.
func ContractIDFromVisibleHash(hashOrID string) string {
	h := strings.TrimSpace(hashOrID)
	if h == "" {
		return ""
	}
	if IsPixelHash(h) || IsPixelHash(Normalize(h)) {
		return ToWishID(h)
	}
	return h
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
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	out := []string{id}
	n := Normalize(id)
	if n != id {
		out = append(out, n)
	}
	wish := ToWishID(id)
	if wish != id {
		out = append(out, wish)
	}
	return out
}
