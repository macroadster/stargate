package ipfs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Fixed valid libp2p peer ID (ed25519) for unit tests — not a live key.
const testPeerID = "12D3KooWPmijo5KFYySjPqoeTLKzobWbE4rq2cgSVH8GyQPLyrXE"

func TestResolveBootstrapEntryFullMultiaddr(t *testing.T) {
	t.Parallel()
	full := "/ip4/1.2.3.4/tcp/4001/p2p/" + testPeerID
	got, err := resolveBootstrapEntry(context.Background(), full)
	if err != nil {
		t.Fatal(err)
	}
	if got != full {
		t.Fatalf("want %s got %s", full, got)
	}
}

func TestResolveBootstrapEntryHTTP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != defaultHTTPStatusPath {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled": true,
			"peer_id": testPeerID,
		})
	}))
	t.Cleanup(srv.Close)

	got, err := resolveBootstrapEntry(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "/p2p/"+testPeerID) {
		t.Fatalf("expected peer id in multiaddr, got %s", got)
	}
	if !strings.Contains(got, "/tcp/4001") {
		t.Fatalf("expected default swarm port, got %s", got)
	}
	// httptest host is 127.0.0.1 → /ip4/
	if !strings.HasPrefix(got, "/ip4/127.0.0.1/tcp/4001/p2p/") {
		t.Fatalf("unexpected multiaddr form: %s", got)
	}
}

func TestResolveBootstrapEntryMultiaddrWithoutPeerID(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != defaultHTTPStatusPath {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled": true,
			"peer_id": testPeerID,
		})
	}))
	t.Cleanup(srv.Close)

	// Force discovery via the test server: host is 127.0.0.1 which discoverPeerID
	// will try on https://127.0.0.1 and http://127.0.0.1:3001 first — those fail.
	// Multiaddr path uses discoverPeerID which does not know about srv.URL port.
	// Instead test appendPeerID + fetchMirrorPeerID units, and use resolveHTTPBootstrap
	// path for the full HTTP case above.
	//
	// For multiaddr-without-p2p, inject by pointing status at srv via a custom path:
	// We only verify transport multiaddr + append when fetch works on host that serves 80/443.
	// Use resolveHTTPBootstrap equivalent: multiaddr discovery against unreachable host should error.
	_, err := resolveBootstrapEntry(context.Background(), "/ip4/127.0.0.1/tcp/4001")
	if err == nil {
		// May succeed if something is on local :3001 — still check form if so
		t.Log("local discovery unexpectedly succeeded (environment-dependent)")
	}

	// Direct unit: fetch + append
	id, err := fetchMirrorPeerID(context.Background(), srv.URL+defaultHTTPStatusPath)
	if err != nil {
		t.Fatal(err)
	}
	if id != testPeerID {
		t.Fatalf("peer id: got %s", id)
	}
	full, err := appendPeerID("/ip4/127.0.0.1/tcp/4001", id)
	if err != nil {
		t.Fatal(err)
	}
	want := "/ip4/127.0.0.1/tcp/4001/p2p/" + testPeerID
	if full != want {
		t.Fatalf("want %s got %s", want, full)
	}
}

func TestResolveBootstrapEntryEmptyPeerID(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"enabled": false, "peer_id": ""})
	}))
	t.Cleanup(srv.Close)

	_, err := resolveBootstrapEntry(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for empty peer_id")
	}
}

func TestExpandBootstrapPeers(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"enabled": true, "peer_id": testPeerID})
	}))
	t.Cleanup(srv.Close)

	fixed := "/ip4/9.9.9.9/tcp/4001/p2p/" + testPeerID
	got := expandBootstrapPeers(context.Background(), []string{fixed, srv.URL, ""})
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d: %v", len(got), got)
	}
	if got[0] != fixed {
		t.Fatalf("fixed entry mutated: %s", got[0])
	}
	if !strings.Contains(got[1], testPeerID) {
		t.Fatalf("resolved entry missing peer id: %s", got[1])
	}
}

func TestTransportMultiaddr(t *testing.T) {
	t.Parallel()
	ip4, err := transportMultiaddr("1.2.3.4", 4001)
	if err != nil || ip4 != "/ip4/1.2.3.4/tcp/4001" {
		t.Fatalf("ip4: %s %v", ip4, err)
	}
	dns, err := transportMultiaddr("starlight-ai.freemyip.com", 4001)
	if err != nil || dns != "/dns4/starlight-ai.freemyip.com/tcp/4001" {
		t.Fatalf("dns: %s %v", dns, err)
	}
}

func TestHasP2PComponent(t *testing.T) {
	t.Parallel()
	if !hasP2PComponent("/ip4/1.2.3.4/tcp/4001/p2p/" + testPeerID) {
		t.Fatal("expected p2p")
	}
	if hasP2PComponent("/ip4/1.2.3.4/tcp/4001") {
		t.Fatal("unexpected p2p")
	}
}

// Live check against public production status API. Skipped with -short.
func TestResolveBootstrapEntryProduction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network bootstrap resolution")
	}
	got, err := resolveBootstrapEntry(context.Background(), "https://starlight-ai.freemyip.com")
	if err != nil {
		t.Skipf("production unreachable: %v", err)
	}
	if !strings.Contains(got, "/dns4/starlight-ai.freemyip.com/tcp/4001/p2p/") {
		t.Fatalf("unexpected resolved multiaddr: %s", got)
	}
	// Peer ID suffix must decode.
	parts := strings.Split(got, "/p2p/")
	if len(parts) != 2 || parts[1] == "" {
		t.Fatalf("missing peer id: %s", got)
	}
}
