package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// discoverLabel is the historical OP_RETURN walk for the status console.
// The chain tip is the block height already on the snapshot.
func discoverLabel(s Snapshot) string {
	if !s.DiscoverEnabled {
		return "off"
	}
	tip := s.BlockHeight
	if tip <= 0 {
		tip = s.TipLocal
	}
	cursor := s.DiscoverNext
	if cursor < 0 {
		cursor = 0
	}
	progress := formatInt(cursor)
	if tip > 0 {
		progress += " / " + formatInt(tip)
	}
	waiting := ""
	if s.DiscoverPending > 0 {
		waiting = " · " + formatInt(int64(s.DiscoverPending)) + " waiting"
	}
	if s.DiscoverWaiting {
		if cursor > 0 || tip > 0 {
			return "paused · " + progress + waiting
		}
		return "waiting for sync"
	}
	if tip > 0 && cursor > tip {
		if s.DiscoverPending > 0 {
			return "caught up" + waiting
		}
		return "caught up"
	}
	return progress + waiting
}

func formatInt(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	pre := len(s) % 3
	if pre == 0 {
		pre = 3
	}
	b.WriteString(s[:pre])
	for i := pre; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func formatUptime(since, now time.Time) string {
	if since.IsZero() || now.IsZero() {
		return "—"
	}
	d := now.Sub(since)
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	sec := int(d.Seconds())
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	case m > 0:
		return fmt.Sprintf("%dm%02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func shorten(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 5 {
		return string(r[:n])
	}
	tail := 4
	if n < 12 {
		tail = 2
	}
	head := n - tail - 1
	if head < 1 {
		head = 1
	}
	if head+1+tail > n {
		return string(r[:n])
	}
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func syncLabel(s Snapshot) string {
	switch {
	case s.FetchError != "":
		return "unreachable"
	case s.ChainError != "":
		return "rpc error"
	case s.TipHashMismatch:
		return "fork"
	case s.TipLagging:
		if s.TipLagBlocks > 0 {
			return fmt.Sprintf("lagging %s", formatInt(s.TipLagBlocks))
		}
		return "lagging"
	case s.HealthStatus == "starting" && !s.BitcoinConnected:
		return "starting"
	case s.InitialDownload:
		return "syncing"
	case s.BitcoinConnected && !s.Synced:
		return "catching up"
	case s.BitcoinConnected && s.Synced:
		return "synced"
	default:
		if s.HealthStatus == "starting" || s.Phase == PhaseStarting {
			return "starting"
		}
		return "down"
	}
}

func chainLabel(s Snapshot) string {
	height := "—"
	if s.BlockHeight > 0 || s.BitcoinConnected {
		height = formatInt(s.BlockHeight)
	}
	return dash(s.Network) + " · " + dash(s.ChainMode) + " · " + height + " · " + syncLabel(s)
}

func scannerLabel(s Snapshot) string {
	if s.ScannerType == "" {
		if s.Phase == PhaseStarting || s.HealthStatus == "starting" {
			return "starting"
		}
		return "—"
	}
	state := "not ready"
	if s.ScannerReady {
		state = "ready"
	}
	label := s.ScannerType + " · " + state
	if v := s.ScannerVersion; v != "" && v != "unknown" && v != "none" {
		label = s.ScannerType + " · " + v + " · " + state
	}
	return label
}

func storageLabel(s Snapshot) string {
	if s.Storage == "" && s.DataDir == "" {
		return "—"
	}
	if s.DataDir == "" {
		return s.Storage
	}
	if s.Storage == "" {
		return s.DataDir
	}
	return s.Storage + " · " + s.DataDir
}

func mirrorLabel(s Snapshot) string {
	if !s.MirrorEnabled && !s.WishMirrorOn {
		if s.Phase == PhaseStarting || s.HealthStatus == "starting" {
			return "starting"
		}
		return "off"
	}
	return fmt.Sprintf("uploads %s · wishes %s · %d peers", formatInt(int64(s.MirrorUploads)), formatInt(int64(s.MirrorWishes)), s.MirrorPeers)
}

func agentLabel(s Snapshot) string {
	if !s.AgentEnabled {
		return "off"
	}
	parts := []string{"on"}
	if s.AgentWatcher {
		parts = append(parts, "watcher")
	}
	if s.AgentWorker {
		parts = append(parts, "worker")
	}
	if s.AgentTool != "" {
		parts = append(parts, s.AgentTool)
	}
	return strings.Join(parts, " · ")
}

func peerLabel(s Snapshot) string {
	if s.MinPeers > 0 {
		return fmt.Sprintf("%d (min %d)", s.PeerCount, s.MinPeers)
	}
	if s.PeerCount == 0 && !s.BitcoinConnected {
		return "—"
	}
	return fmt.Sprintf("%d", s.PeerCount)
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// FormatPlain renders one snapshot as wrapped-free text for a non-TTY
// `stargate console --once` and for a console that cannot take the screen.
func FormatPlain(s Snapshot) string {
	var b strings.Builder
	if s.Version != "" {
		fmt.Fprintf(&b, "%s\n", s.Version)
	} else {
		b.WriteString("stargate\n")
	}
	fmt.Fprintf(&b, "Open this in your browser: %s\n", dash(s.UIURL))
	if s.DocsURL != "" {
		fmt.Fprintf(&b, "Guides: %s\n", s.DocsURL)
	}
	if s.MCPURL != "" {
		fmt.Fprintf(&b, "Agent tools: %s\n", s.MCPURL)
	}
	status := s.HealthStatus
	if status == "" {
		status = string(s.Phase)
	}
	fmt.Fprintf(&b, "Status: %s\n", dash(status))
	if s.Detail != "" {
		fmt.Fprintf(&b, "Detail: %s\n", s.Detail)
	}
	if s.FetchError != "" {
		fmt.Fprintf(&b, "Error: %s\n", s.FetchError)
	}
	fmt.Fprintf(&b, "Chain: %s\n", chainLabel(s))
	if s.BestBlockHash != "" {
		fmt.Fprintf(&b, "Hash: %s\n", s.BestBlockHash)
	}
	if s.PeerCount > 0 || s.MinPeers > 0 {
		fmt.Fprintf(&b, "Peers: %s\n", peerLabel(s))
	}
	fmt.Fprintf(&b, "Scanner: %s\n", scannerLabel(s))
	fmt.Fprintf(&b, "Storage: %s\n", storageLabel(s))
	fmt.Fprintf(&b, "Mirror: %s\n", mirrorLabel(s))
	fmt.Fprintf(&b, "Agents: %s\n", agentLabel(s))
	if !s.StartedAt.IsZero() {
		now := s.SampledAt
		if now.IsZero() {
			now = time.Now()
		}
		fmt.Fprintf(&b, "Uptime: %s\n", formatUptime(s.StartedAt, now))
	}
	if s.MonitorRunning || s.BlocksProcessed > 0 || s.DiscoverEnabled {
		fmt.Fprintf(&b, "Block monitor: height %s, processed %s, inscriptions %s, contracts %s, walk %s\n",
			formatInt(s.MonitorHeight), formatInt(s.BlocksProcessed), formatInt(s.InscriptionsSeen), formatInt(s.ContractsSeen), discoverLabel(s))
	}
	return b.String()
}
