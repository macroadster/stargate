package tui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolveConsoleURLs(t *testing.T) {
	root, status, err := ResolveConsoleURLs("http://127.0.0.1:3001/api/health")
	if err != nil {
		t.Fatal(err)
	}
	if root != "http://127.0.0.1:3001" || status != "http://127.0.0.1:3001/api/console/status" {
		t.Fatalf("root=%s status=%s", root, status)
	}
	root, status, err = ResolveConsoleURLs("127.0.0.1:9")
	if err != nil || !strings.HasPrefix(status, "http://127.0.0.1:9/") {
		t.Fatalf("bare: %s %s %v", root, status, err)
	}
	if _, _, err := ResolveConsoleURLs("ftp://example.com"); err == nil {
		t.Fatal("expected reject ftp")
	}
}

func TestFetchConsoleStatusAndFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/console/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"version":"stargate dev",
			"phase":"ready",
			"health_status":"healthy",
			"ui_url":"http://node.example:3001",
			"network":"testnet4",
			"chain_mode":"managed",
			"block_height":102441,
			"bitcoin_connected":true,
			"synced":true,
			"peer_count":8,
			"scanner_type":"trin-gguf",
			"scanner_ready":true
		}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	snap, err := Fetch(srv.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if snap.UIURL != "http://node.example:3001" || snap.BlockHeight != 102441 || snap.Network != "testnet4" {
		t.Fatalf("%+v", snap)
	}
	if !snap.Reachable || snap.HealthStatus != "healthy" {
		t.Fatalf("status %+v", snap)
	}
	if snap.DocsURL != srv.URL+"/docs" {
		t.Fatalf("docs %s", snap.DocsURL)
	}

	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/console/status":
			http.NotFound(w, r)
		case "/api/health":
			_, _ = w.Write([]byte(`{"status":"healthy","version":"1.0.0","scanner":{"model_loaded":true,"model_version":"alpha"},"bitcoin":{"node_connected":true,"block_height":10}}`))
		case "/bitcoin/v1/health":
			_, _ = w.Write([]byte(`{"status":"degraded","network":"signet","bitcoin":{"node_connected":true,"block_height":11},"tip_lag":{"lagging":true,"lag_blocks":4,"external_tip":15,"local_tip":11},"scanner_manager":{"scanner_type":"alpha"}}`))
		case "/api/ipfs-mirror/status":
			_, _ = w.Write([]byte(`{"enabled":true,"peer_id":"peer1","known_files":6,"wish_enabled":true,"wish_known_files":2,"swarm_peers":["a","b"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(legacy.Close)

	snap, err = Fetch(legacy.URL+"/api/health", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Network != "signet" || snap.BlockHeight != 11 || snap.ScannerType != "alpha" {
		t.Fatalf("fallback chain: %+v", snap)
	}
	if !snap.TipLagging || snap.HealthStatus != "degraded" {
		t.Fatalf("fallback health: %+v", snap)
	}
	if !snap.MirrorEnabled || snap.MirrorPeers != 2 || snap.MirrorWishes != 2 {
		t.Fatalf("fallback mirror: %+v", snap)
	}
	if !strings.Contains(snap.UIURL, legacy.URL) {
		t.Fatalf("url %s", snap.UIURL)
	}
}

func TestUnreachableKeepsBrowserHint(t *testing.T) {
	s := Unreachable("http://127.0.0.1:3001", http.ErrServerClosed)
	if s.HealthStatus != "down" || s.UIURL != "http://127.0.0.1:3001" || s.FetchError == "" {
		t.Fatalf("%+v", s)
	}
	if !strings.Contains(FormatPlain(s), "Open this in your browser") {
		t.Fatal(FormatPlain(s))
	}
}
