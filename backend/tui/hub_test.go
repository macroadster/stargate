package tui

import (
	"strings"
	"testing"
	"time"
)

func TestHubLogRingAndPhase(t *testing.T) {
	h := NewHub()
	h.SetVersion("stargate dev")
	h.SetPhase(PhaseStarting, "starting Bitcoin node")
	h.SetLineFormatter(func(line string) string {
		if line == "skip" {
			return ""
		}
		return "x:" + line
	})
	if _, err := h.Write([]byte("one\ntwo\nsk")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Write([]byte("ip\nthree\n")); err != nil {
		t.Fatal(err)
	}
	s := h.Snapshot()
	if s.Version != "stargate dev" || s.Phase != PhaseStarting || s.Detail != "starting Bitcoin node" {
		t.Fatalf("header: %+v", s)
	}
	if strings.Join(s.Logs, "|") != "x:one|x:two|x:three" {
		t.Fatalf("logs: %#v", s.Logs)
	}
	if s.SampledAt.IsZero() {
		t.Fatal("missing sample time")
	}

	h.ApplyStatus(Snapshot{Detail: "listening on :3001", BlockHeight: 3, UIURL: "http://127.0.0.1:3001"})
	s = h.Snapshot()
	if s.Detail != "starting Bitcoin node" || s.BlockHeight != 3 || s.UIURL != "http://127.0.0.1:3001" {
		t.Fatalf("apply while starting: %+v", s)
	}
	if len(s.Logs) != 3 {
		t.Fatalf("logs wiped: %#v", s.Logs)
	}

	h.SetPhase(PhaseReady, "listening on :3001")
	h.ApplyStatus(Snapshot{Detail: "lagging", BlockHeight: 9})
	s = h.Snapshot()
	if s.Phase != PhaseReady || s.Detail != "lagging" || s.BlockHeight != 9 || s.Version != "stargate dev" {
		t.Fatalf("apply while ready: %+v", s)
	}
}

func TestHubLogCap(t *testing.T) {
	h := NewHub()
	for i := 0; i < logCap+25; i++ {
		if _, err := h.Write([]byte("l\n")); err != nil {
			t.Fatal(err)
		}
	}
	s := h.Snapshot()
	if len(s.Logs) != logCap {
		t.Fatalf("cap: %d", len(s.Logs))
	}
}

func TestDeriveHealth(t *testing.T) {
	s := Snapshot{BitcoinConnected: true, Synced: true}
	DeriveHealth(&s, PhaseStarting)
	if s.HealthStatus != "starting" {
		t.Fatal(s.HealthStatus)
	}
	DeriveHealth(&s, PhaseReady)
	if s.HealthStatus != "healthy" {
		t.Fatal(s.HealthStatus)
	}
	s.TipLagging = true
	DeriveHealth(&s, PhaseReady)
	if s.HealthStatus != "degraded" {
		t.Fatal(s.HealthStatus)
	}
	s = Snapshot{BitcoinConnected: true, InitialDownload: true}
	DeriveHealth(&s, PhaseReady)
	if s.HealthStatus != "syncing" {
		t.Fatal(s.HealthStatus)
	}
	s = Snapshot{FetchError: "connection refused"}
	DeriveHealth(&s, PhaseReady)
	if s.HealthStatus != "down" {
		t.Fatal(s.HealthStatus)
	}
}

func TestFormatPlainAndUptime(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	s := Snapshot{
		Version:          "stargate dev",
		UIURL:            "http://127.0.0.1:3001",
		DocsURL:          "http://127.0.0.1:3001/docs",
		HealthStatus:     "healthy",
		Network:          "testnet4",
		ChainMode:        "managed",
		BlockHeight:      102441,
		BitcoinConnected: true,
		Synced:           true,
		StartedAt:        now.Add(-3*time.Minute - 12*time.Second),
		SampledAt:        now,
	}
	out := FormatPlain(s)
	for _, want := range []string{"Open this in your browser: http://127.0.0.1:3001", "Guides:", "testnet4", "102,441", "3m12s"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	if formatInt(-1200) != "-1,200" {
		t.Fatal(formatInt(-1200))
	}
}
