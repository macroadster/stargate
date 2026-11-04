package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stargate-backend/bitcoin"
	"stargate-backend/tui"
)

func TestBuildConsoleSnapshot(t *testing.T) {
	t.Setenv("STARGATE_HTTP_PORT", "3456")
	t.Setenv("STARGATE_STORAGE", "memory")
	t.Setenv("STARGATE_DATA_DIR", t.TempDir())
	t.Setenv("BITCOIN_NETWORK", "signet")
	t.Setenv("BTCD_MODE", "off")
	t.Setenv("STARGATE_AGENT_ENABLED", "false")

	s := buildConsoleSnapshot(nil, nil, nil, tui.PhaseStarting)
	if s.UIURL != "http://127.0.0.1:3456" || s.DocsURL != "http://127.0.0.1:3456/docs" {
		t.Fatalf("urls: %+v", s)
	}
	if s.Network != "signet" || s.ChainMode != "off" || s.Storage != "memory" {
		t.Fatalf("config: %+v", s)
	}
	if s.HealthStatus != "starting" {
		t.Fatalf("health %s", s.HealthStatus)
	}
	if s.AgentEnabled {
		t.Fatal("agent should be off")
	}
	if s.MCPURL != "http://127.0.0.1:3456/mcp/tools" {
		t.Fatal(s.MCPURL)
	}

	ready := buildConsoleSnapshot(nil, nil, nil, tui.PhaseReady)
	if ready.HealthStatus != "degraded" {
		t.Fatalf("ready without a chain: %s", ready.HealthStatus)
	}

	mon := &bitcoin.BlockMonitor{}
	// SnapshotStats on a zero monitor is stopped. Height stays 0.
	withMon := buildConsoleSnapshot(nil, mon, nil, tui.PhaseStarting)
	if withMon.MonitorRunning {
		t.Fatal("monitor should be stopped")
	}
}

func TestConsoleStatusHandler(t *testing.T) {
	t.Setenv("STARGATE_HTTP_PORT", "3001")
	t.Setenv("STARGATE_AGENT_ENABLED", "false")
	c := newConsoleControl()
	c.hub.SetPhase(tui.PhaseReady, "listening on :3001")
	for i := 0; i < 50; i++ {
		fmt.Fprintf(c.hub, "line %d\n", i)
	}
	// The access-log poll of this same route must not fill the panel.
	fmt.Fprintln(c.hub, `{"method":"GET","path":"/api/console/status","status":200,"duration":"1ms"}`)
	fmt.Fprintln(c.hub, `{"method":"GET","path":"/api/health","status":200,"duration":"2ms"}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/console/status", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	c.handleStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatal(rec.Header().Get("Content-Type"))
	}
	var snap tui.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Logs) != 40 {
		t.Fatalf("log cap %d", len(snap.Logs))
	}
	joined := strings.Join(snap.Logs, "\n")
	if strings.Contains(joined, "/api/console/status") {
		t.Fatalf("poll leaked into logs: %s", joined)
	}
	if !strings.Contains(joined, "GET /api/health 200 2ms") {
		t.Fatalf("access line: %s", joined)
	}
	if snap.UIURL != "http://127.0.0.1:3001" {
		t.Fatal(snap.UIURL)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/console/status", nil)
	req.RemoteAddr = "8.8.8.8:443"
	c.handleStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("remote status %d", rec.Code)
	}
	var remote tui.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &remote); err != nil {
		t.Fatal(err)
	}
	if len(remote.Logs) != 0 || remote.DataDir != "" {
		t.Fatalf("remote sample leaked local detail: logs=%d dir=%q", len(remote.Logs), remote.DataDir)
	}
	if remote.UIURL == "" || remote.HealthStatus == "" {
		t.Fatalf("remote sample missing status: %+v", remote)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/console/status", nil)
	c.handleStatus(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post %d", rec.Code)
	}
}

func TestPrettifyLogLine(t *testing.T) {
	if prettifyLogLine(`{"method":"GET","path":"/api/console/status","status":200,"duration":"1ms"}`) != "" {
		t.Fatal("poll should drop")
	}
	if got := prettifyLogLine(`{"method":"POST","path":"/api/inscribe","status":201,"duration":"4ms"}`); got != "POST /api/inscribe 201 4ms" {
		t.Fatal(got)
	}
	if got := prettifyLogLine("bitcoin chain ready"); got != "bitcoin chain ready" {
		t.Fatal(got)
	}
}

func TestApplyNodeStatus(t *testing.T) {
	var s tui.Snapshot
	applyNodeStatus(&s, map[string]any{
		"network":                "testnet4",
		"blocks":                 int32(100),
		"headers":                float64(101),
		"peers":                  8,
		"min_peers":              1,
		"synced":                 true,
		"best_block_hash":        "abcd",
		"initial_block_download": false,
	})
	if !s.BitcoinConnected || s.BlockHeight != 100 || s.HeaderHeight != 101 || s.PeerCount != 8 || !s.Synced || s.BestBlockHash != "abcd" {
		t.Fatalf("%+v", s)
	}
	applyNodeStatus(&s, map[string]any{"error": "rpc down"})
	if s.BitcoinConnected || s.ChainError != "rpc down" {
		t.Fatalf("%+v", s)
	}
}
