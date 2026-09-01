package ipfs

import (
	"testing"
)

func TestMirrorAndWishTopics(t *testing.T) {
	t.Setenv("IPFS_MIRROR_TOPIC", "")
	t.Setenv("IPFS_WISH_TOPIC", "")
	if got := MirrorTopic(); got != DefaultMirrorTopic {
		t.Fatalf("MirrorTopic()=%q want %q", got, DefaultMirrorTopic)
	}
	if got := WishTopic(); got != DefaultWishTopic {
		t.Fatalf("WishTopic()=%q want %q", got, DefaultWishTopic)
	}
	if !IsAllowedPubsubTopic(DefaultMirrorTopic) || !IsAllowedPubsubTopic(DefaultWishTopic) {
		t.Fatal("default topics should be allowed")
	}
	if IsAllowedPubsubTopic("stargate-stego") {
		t.Fatal("stargate-stego must remain capped off")
	}

	t.Setenv("IPFS_MIRROR_TOPIC", "custom-uploads")
	t.Setenv("IPFS_WISH_TOPIC", "custom-wishes")
	if !IsAllowedPubsubTopic("custom-uploads") || !IsAllowedPubsubTopic("custom-wishes") {
		t.Fatal("env-overridden topics should be allowed")
	}
	if IsAllowedPubsubTopic(DefaultMirrorTopic) {
		t.Fatal("default mirror topic should not be allowed after override")
	}
}

func TestAllowedPubsubTopicsDedup(t *testing.T) {
	t.Setenv("IPFS_MIRROR_TOPIC", "same-topic")
	t.Setenv("IPFS_WISH_TOPIC", "same-topic")
	got := AllowedPubsubTopics()
	if len(got) != 1 || got[0] != "same-topic" {
		t.Fatalf("dedup: got %v", got)
	}
}
