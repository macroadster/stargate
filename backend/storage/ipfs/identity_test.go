package ipfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateIdentityStable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ipfs_identity.key")

	first, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("identity perms %o want 0600", info.Mode().Perm())
	}

	second, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	a, err := first.Raw()
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.Raw()
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("reloaded identity does not match")
	}
}

func TestEmbeddedNodeIdentityPersistsAcrossRestart(t *testing.T) {
	tmp := t.TempDir()
	idPath := filepath.Join(tmp, "ipfs_identity.key")
	cfg := func() NodeConfig {
		return NodeConfig{
			RepoPath:                 filepath.Join(tmp, "repo"),
			ListenAddrs:              []string{"/ip4/127.0.0.1/tcp/0"},
			Bootstrap:                nil,
			DHTMode:                  DHTModeClient,
			ForcePrivateReachability: true,
			ConnLowWater:             5,
			ConnHighWater:            10,
			IdentityPath:             idPath,
		}
	}

	n1, err := NewEmbeddedNode(context.Background(), cfg())
	if err != nil {
		t.Fatalf("first node: %v", err)
	}
	id1 := n1.PeerID()
	if id1 == "" {
		t.Fatal("empty peer id")
	}
	if err := n1.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	n2, err := NewEmbeddedNode(context.Background(), cfg())
	if err != nil {
		t.Fatalf("second node: %v", err)
	}
	defer n2.Close()
	if got := n2.PeerID(); got != id1 {
		t.Fatalf("peer id changed across restart: %s vs %s", id1, got)
	}
}

func TestGetTopicAllowsWishAndMirror(t *testing.T) {
	t.Setenv("IPFS_MIRROR_TOPIC", DefaultMirrorTopic)
	t.Setenv("IPFS_WISH_TOPIC", DefaultWishTopic)

	tmp := t.TempDir()
	node, err := NewEmbeddedNode(context.Background(), NodeConfig{
		RepoPath:                 filepath.Join(tmp, "repo"),
		ListenAddrs:              []string{"/ip4/127.0.0.1/tcp/0"},
		Bootstrap:                nil,
		DHTMode:                  DHTModeClient,
		ForcePrivateReachability: true,
		IdentityPath:             filepath.Join(tmp, "ipfs_identity.key"),
	})
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	defer node.Close()

	if _, err := node.getTopic(DefaultWishTopic); err != nil {
		t.Fatalf("wish topic should be allowed: %v", err)
	}
	if _, err := node.getTopic(DefaultMirrorTopic); err != nil {
		t.Fatalf("mirror topic should be allowed: %v", err)
	}
	if _, err := node.getTopic("stargate-stego"); err == nil {
		t.Fatal("stargate-stego should be refused")
	}
}
