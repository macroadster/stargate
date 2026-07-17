package ipfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBootstrapPeers(t *testing.T) {
	t.Parallel()

	// Unset → Starlight production host (peer ID resolved at dial time).
	if got := resolveBootstrapPeers("", false); len(got) != 1 || got[0] != defaultStarlightBootstrap {
		t.Fatalf("empty bootstrap want [%s], got %v", defaultStarlightBootstrap, got)
	}
	if got := resolveBootstrapPeers("  ", false); len(got) != 1 || got[0] != defaultStarlightBootstrap {
		t.Fatalf("whitespace bootstrap want [%s], got %v", defaultStarlightBootstrap, got)
	}

	// Opt out of default mesh seed.
	for _, kw := range []string{"none", "off", "private", "local", "NONE"} {
		if got := resolveBootstrapPeers(kw, false); got != nil {
			t.Fatalf("%q bootstrap want nil (private mesh), got %v", kw, got)
		}
	}

	pub := resolveBootstrapPeers("public", false)
	if len(pub) != len(publicBootstrapPeers) {
		t.Fatalf("public: want %d peers, got %d", len(publicBootstrapPeers), len(pub))
	}
	pub2 := resolveBootstrapPeers("DEFAULT", false)
	if len(pub2) != len(publicBootstrapPeers) {
		t.Fatalf("default keyword: want %d peers, got %d", len(publicBootstrapPeers), len(pub2))
	}
	pub3 := resolveBootstrapPeers("", true)
	if len(pub3) != len(publicBootstrapPeers) {
		t.Fatalf("joinPublic: want %d peers, got %d", len(publicBootstrapPeers), len(pub3))
	}

	custom := resolveBootstrapPeers("/ip4/1.2.3.4/tcp/4001/p2p/QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N,  ", false)
	if len(custom) != 1 || custom[0] != "/ip4/1.2.3.4/tcp/4001/p2p/QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N" {
		t.Fatalf("custom peers: got %v", custom)
	}

	mixed := resolveBootstrapPeers("public,/ip4/1.2.3.4/tcp/4001/p2p/QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N", false)
	if len(mixed) != len(publicBootstrapPeers)+1 {
		t.Fatalf("mixed: want %d, got %d", len(publicBootstrapPeers)+1, len(mixed))
	}
}

func TestDHTModeFromString(t *testing.T) {
	t.Parallel()
	// Just ensure mapping is stable; compare via label helper.
	cases := map[string]string{
		"":       DHTModeClient,
		"client": DHTModeClient,
		"CLIENT": DHTModeClient,
		"server": DHTModeServer,
		"auto":   DHTModeAuto,
		"autoserver": DHTModeAuto,
	}
	for in, want := range cases {
		got := dhtModeLabel(dhtModeFromString(in))
		if got != want {
			t.Errorf("dhtModeFromString(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNewEmbeddedNodePrivateMesh(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}

	node, err := NewEmbeddedNode(context.Background(), NodeConfig{
		RepoPath:                 repo,
		ListenAddrs:              []string{"/ip4/127.0.0.1/tcp/0"},
		Bootstrap:                nil, // private mesh
		DHTMode:                  DHTModeClient,
		PeerExchange:             false,
		RoutingDiscovery:         false,
		ForcePrivateReachability: true,
		ConnLowWater:             5,
		ConnHighWater:            10,
	})
	if err != nil {
		t.Fatalf("NewEmbeddedNode: %v", err)
	}
	defer node.Close()

	if node.PeerID() == "" {
		t.Fatal("expected peer id")
	}
	// No public bootstrap should mean few connections shortly after start.
	// Peerstore may still have local listen addrs; host.Network().Peers() is the check.
	if n := len(node.host.Network().Peers()); n > 2 {
		t.Fatalf("private mesh should not dial public peers; connected peers=%d", n)
	}
}
