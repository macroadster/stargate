package stego

import (
	"strings"
)

// WishCreatorMessagePrefix is the domain separator for a creator attestation
// over a wish hash. Do not sign the raw hex hash: auth already special-cases
// hex-looking messages, and a prefix keeps this distinct from challenge nonces.
const WishCreatorMessagePrefix = "STARLIGHT-WISH-V1\n"

// WishCreatorMessage is the Bitcoin signed-message body a wish creator signs.
func WishCreatorMessage(wishHash string) string {
	return WishCreatorMessagePrefix + strings.TrimSpace(wishHash)
}

// IsCreatorAttestationMetaKey reports whether k is a creator identity field
// that must not travel in payload.Metadata (attacker-controlled, incoming-wins).
func IsCreatorAttestationMetaKey(k string) bool {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "creator_wallet", "creator_sig":
		return true
	default:
		return false
	}
}
