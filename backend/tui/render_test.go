package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func sampleSnap() Snapshot {
	now := time.Date(2026, 10, 16, 15, 4, 5, 0, time.UTC)
	return Snapshot{
		Version:          "stargate dev",
		Phase:            PhaseReady,
		Detail:           "listening on :3001",
		StartedAt:        now.Add(-time.Hour),
		SampledAt:        now,
		UIURL:            "http://127.0.0.1:3001",
		DocsURL:          "http://127.0.0.1:3001/docs",
		APIURL:           "http://127.0.0.1:3001/api/",
		MCPURL:           "http://127.0.0.1:3001/mcp/tools",
		HTTPPort:         "3001",
		Storage:          "sqlite",
		DataDir:          "data",
		Network:          "testnet4",
		ChainMode:        "managed",
		HealthStatus:     "healthy",
		ScannerType:      "trin-gguf",
		ScannerVersion:   "starlight",
		ScannerReady:     true,
		BitcoinConnected: true,
		BlockHeight:      102441,
		HeaderHeight:     102441,
		PeerCount:        8,
		MinPeers:         1,
		Synced:           true,
		BestBlockHash:    "0000000000000000000abc1234",
		MonitorRunning:   true,
		MonitorHeight:    102400,
		BlocksProcessed:  1204,
		InscriptionsSeen: 18,
		ContractsSeen:    6,
		ImagesSeen:       40,
		MirrorEnabled:    true,
		WishMirrorOn:     true,
		MirrorUploads:    12,
		MirrorWishes:     4,
		MirrorPeers:      3,
		MirrorPeerID:     "12D3KooWpeer",
		AgentEnabled:     false,
		Goroutines:       48,
		Logs:             []string{"bitcoin chain ready", "listening on :3001"},
		Reachable:        true,
	}
}

func viewAt(snap Snapshot) ViewState {
	return ViewState{Now: snap.SampledAt}
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func visible(frame string) string { return ansiRE.ReplaceAllString(frame, "") }

func assertWidth(t *testing.T, frame string, w int) {
	t.Helper()
	for i, line := range strings.Split(visible(frame), "\n") {
		n := utf8.RuneCountInString(line)
		if n > w {
			t.Fatalf("line %d width %d > %d: %q", i, n, w, line)
		}
	}
}

func TestRenderWelcomeAndMonitor(t *testing.T) {
	snap := sampleSnap()
	welcome := Render(snap, viewAt(snap), 100, 40)
	text := visible(welcome)
	for _, want := range []string{
		"Open this in your browser",
		"http://127.0.0.1:3001",
		"/docs",
		"/mcp/tools",
		"testnet4",
		"trin-gguf",
		"sqlite",
		"Agents",
		"off",
		"listening on :3001",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("welcome missing %q\n%s", want, text)
		}
	}
	assertWidth(t, welcome, 100)

	term24 := Render(snap, viewAt(snap), 80, 24)
	text24 := visible(term24)
	if !strings.Contains(text24, "Open this in your browser") || !strings.Contains(text24, "http://127.0.0.1:3001") {
		t.Fatalf("80x24 welcome\n%s", text24)
	}
	assertWidth(t, term24, 80)
	if !strings.Contains(welcome, "┌") {
		t.Fatal("expected unicode border")
	}

	mon := Render(snap, ViewState{View: ViewMonitor, Now: snap.SampledAt}, 100, 40)
	mtext := visible(mon)
	for _, want := range []string{"Block monitor", "IPFS mirror", "102,441", "Peers", "12D3KooWpeer", "http://127.0.0.1:3001"} {
		if !strings.Contains(mtext, want) {
			t.Fatalf("monitor missing %q\n%s", want, mtext)
		}
	}
	assertWidth(t, mon, 100)

	logs := Render(snap, ViewState{View: ViewLogs, Now: snap.SampledAt}, 80, 24)
	if !strings.Contains(visible(logs), "bitcoin chain ready") {
		t.Fatalf("logs:\n%s", visible(logs))
	}
	assertWidth(t, logs, 80)
}

func TestRenderDegradedHelpAndNarrow(t *testing.T) {
	snap := sampleSnap()
	snap.HealthStatus = "degraded"
	snap.TipLagging = true
	snap.TipLagBlocks = 9
	snap.FetchError = "connection refused"
	vs := viewAt(snap)
	vs.Help = true
	vs.Color = true
	frame := Render(snap, vs, 100, 40)
	text := visible(frame)
	if !strings.Contains(text, "degraded") || !strings.Contains(text, "Open the browser") {
		t.Fatalf("help/degraded:\n%s", text)
	}
	if !strings.Contains(frame, "\x1b[") {
		t.Fatal("expected color")
	}
	if !strings.Contains(frame, "48;5;231") {
		t.Fatal("help dialog has no solid background")
	}
	helpLine := dialogInterior(text, "Toggle this help")
	if strings.TrimSpace(helpLine) != "? or h    Toggle this help" {
		t.Fatalf("help line shows the console behind it: %q", helpLine)
	}
	if blank := solidDialogRow(text); strings.TrimSpace(blank) != "" {
		t.Fatalf("blank help row is not solid: %q", blank)
	}
	assertWidth(t, frame, 100)

	ascii := Render(snap, ViewState{ASCII: true, Now: snap.SampledAt}, 80, 30)
	if strings.Contains(ascii, "┌") || !strings.Contains(ascii, "+") {
		t.Fatal("ascii mode should use + borders")
	}
	assertWidth(t, ascii, 80)

	narrow := Render(snap, ViewState{Now: snap.SampledAt}, 36, 10)
	ntext := visible(narrow)
	if !strings.Contains(ntext, "http://127.0.0.1:3001") {
		t.Fatalf("narrow:\n%s", ntext)
	}
	assertWidth(t, narrow, 36)

	small := Render(Snapshot{}, ViewState{}, 20, 4)
	if !strings.Contains(visible(small), "Stargate") {
		t.Fatal(visible(small))
	}
}

// dialogInterior returns the text between the vertical bars that enclose needle.
func dialogInterior(frame, needle string) string {
	for _, line := range strings.Split(frame, "\n") {
		i := strings.Index(line, needle)
		if i < 0 {
			continue
		}
		left := strings.LastIndex(line[:i], "│")
		right := strings.Index(line[i:], "│")
		if left < 0 || right < 0 {
			continue
		}
		return line[left+len("│") : i+right]
	}
	return ""
}

// solidDialogRow returns a wide run of spaces sitting between two vertical bars.
func solidDialogRow(frame string) string {
	for _, line := range strings.Split(frame, "\n") {
		rest := line
		for {
			l := strings.Index(rest, "│")
			if l < 0 {
				break
			}
			rest = rest[l+len("│"):]
			r := strings.Index(rest, "│")
			if r < 0 {
				break
			}
			interior := rest[:r]
			if len(interior) >= 40 && strings.TrimSpace(interior) == "" {
				return interior
			}
			rest = rest[r:]
		}
	}
	return "missing"
}

func TestApplyKey(t *testing.T) {
	s := session{view: ViewWelcome}
	if quit, open := applyKey(&s, 'm'); quit || open || s.view != ViewMonitor {
		t.Fatalf("monitor: %+v", s)
	}
	if quit, open := applyKey(&s, 'l'); quit || open || s.view != ViewLogs {
		t.Fatalf("logs: %+v", s)
	}
	if quit, open := applyKey(&s, '\t'); quit || open || s.view != ViewWelcome {
		t.Fatalf("tab: %+v", s)
	}
	if quit, open := applyKey(&s, '?'); quit || open || !s.help {
		t.Fatalf("help: %+v", s)
	}
	if quit, open := applyKey(&s, 'o'); quit || !open {
		t.Fatal("open")
	}
	if quit, _ := applyKey(&s, 'q'); !quit {
		t.Fatal("quit")
	}
}
