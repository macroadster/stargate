package ipfs

import (
	"os"
	"strings"
)

// Default pubsub topics. Both are file-mirror inventories: durable
// PSBT-built artifacts on the uploads topic, inscribed wishes (no PSBT
// yet) on the wish topic so they can expire independently.
const (
	DefaultMirrorTopic = "stargate-uploads"
	DefaultWishTopic   = "stargate-wishes"
)

// MirrorTopic is the durable file-mirror / PSBT-artifact topic.
func MirrorTopic() string {
	if t := strings.TrimSpace(os.Getenv("IPFS_MIRROR_TOPIC")); t != "" {
		return t
	}
	return DefaultMirrorTopic
}

// WishTopic is the ephemeral inscribed-wish file-mirror topic (no PSBT / no engagement).
func WishTopic() string {
	if t := strings.TrimSpace(os.Getenv("IPFS_WISH_TOPIC")); t != "" {
		return t
	}
	return DefaultWishTopic
}

// AllowedPubsubTopics returns the topics the embedded node will join.
// Only the uploads mirror and the inscribed-wish topic are permitted.
func AllowedPubsubTopics() []string {
	mirror := MirrorTopic()
	wish := WishTopic()
	if mirror == wish {
		return []string{mirror}
	}
	return []string{mirror, wish}
}

// IsAllowedPubsubTopic reports whether the embedded node will join name.
func IsAllowedPubsubTopic(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, t := range AllowedPubsubTopics() {
		if name == t {
			return true
		}
	}
	return false
}
