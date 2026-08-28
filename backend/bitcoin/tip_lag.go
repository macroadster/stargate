package bitcoin

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TipLagStatus is a snapshot of local tip vs external explorer tip.
// Used for health endpoints and ops logging so a "synced" local node that
// is actually stuck (e.g. future-timestamp rejects) is visible.
type TipLagStatus struct {
	LocalTip      int64      `json:"local_tip"`
	ExternalTip   int64      `json:"external_tip"`
	LagBlocks     int64      `json:"lag_blocks"`
	Lagging       bool       `json:"lagging"`
	Threshold     int64      `json:"threshold"`
	ExternalError string     `json:"external_error,omitempty"`
	CheckedAt     time.Time  `json:"checked_at"`
	FirstLagAt    *time.Time `json:"first_lag_at,omitempty"`
	LagDuration   string     `json:"lag_duration,omitempty"`
}

var (
	tipLagMu     sync.RWMutex
	tipLagStatus TipLagStatus
	// firstLagAt is sticky while lagging; cleared when lag recovers.
	tipLagFirstSeen time.Time
	// lastRestartAt prevents thrashing managed btcd restarts.
	tipLagLastRestart time.Time
)

// GetTipLagStatus returns the latest tip-lag snapshot (may be zero if never checked).
func GetTipLagStatus() TipLagStatus {
	tipLagMu.RLock()
	defer tipLagMu.RUnlock()
	out := tipLagStatus
	if out.Lagging && !tipLagFirstSeen.IsZero() {
		t := tipLagFirstSeen
		out.FirstLagAt = &t
		out.LagDuration = time.Since(tipLagFirstSeen).Round(time.Second).String()
	} else {
		out.FirstLagAt = nil
		out.LagDuration = ""
	}
	return out
}

// resetTipLagStateForTest clears package tip-lag state (tests only).
func resetTipLagStateForTest() {
	tipLagMu.Lock()
	defer tipLagMu.Unlock()
	tipLagStatus = TipLagStatus{}
	tipLagFirstSeen = time.Time{}
	tipLagLastRestart = time.Time{}
}

// tipLagThreshold is how many blocks behind external tip counts as lagging.
func tipLagThreshold() int64 {
	if v := strings.TrimSpace(os.Getenv("CHAIN_TIP_LAG_THRESHOLD")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 3
}

// tipLagExternalCheckEnabled controls explorer tip comparison (default on).
func tipLagExternalCheckEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("CHAIN_EXTERNAL_TIP_CHECK")))
	if v == "" {
		return true
	}
	return v != "0" && v != "false" && v != "off" && v != "no"
}

// tipLagRestartAfter is how long sustained lag must last before restarting
// managed btcd. Zero/negative disables auto-restart. Default 15m.
func tipLagRestartAfter() time.Duration {
	v := strings.TrimSpace(os.Getenv("CHAIN_TIP_LAG_RESTART_AFTER"))
	if v == "" {
		return 15 * time.Minute
	}
	if v == "0" || strings.EqualFold(v, "off") || strings.EqualFold(v, "false") {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 15 * time.Minute
	}
	return d
}

// tipLagRestartCooldown is minimum time between managed btcd restarts.
func tipLagRestartCooldown() time.Duration {
	v := strings.TrimSpace(os.Getenv("CHAIN_TIP_LAG_RESTART_COOLDOWN"))
	if v == "" {
		return 30 * time.Minute
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 30 * time.Minute
	}
	return d
}

// FetchExternalTipHeight queries the public explorer HeightURL for the network.
// This is a reference tip only — production block data still comes from local btcd.
func FetchExternalTipHeight(ctx context.Context, network string) (int64, error) {
	cfg := GetNetworkConfig(network)
	if cfg == nil || cfg.HeightURL == "" {
		return 0, fmt.Errorf("no height URL for network %q", network)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.HeightURL, nil)
	if err != nil {
		return 0, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("external tip HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return 0, err
	}
	h, err := strconv.ParseInt(strings.TrimSpace(string(body)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse external tip: %w", err)
	}
	return h, nil
}

// EvaluateTipLag compares local and external tips and updates the shared status.
// Pure-ish: given inputs returns the new status; also stores it globally.
func EvaluateTipLag(localTip, externalTip, threshold int64, externalErr error, now time.Time) TipLagStatus {
	if threshold <= 0 {
		threshold = 3
	}
	st := TipLagStatus{
		LocalTip:    localTip,
		ExternalTip: externalTip,
		Threshold:   threshold,
		CheckedAt:   now,
	}
	if externalErr != nil {
		st.ExternalError = externalErr.Error()
		// Keep previous lagging sticky only if we already were lagging; do not
		// flip lagging=true solely because explorer is unreachable.
		tipLagMu.Lock()
		prev := tipLagStatus
		if prev.Lagging && !tipLagFirstSeen.IsZero() {
			st.Lagging = true
			st.LagBlocks = prev.LagBlocks
			st.ExternalTip = prev.ExternalTip
			t := tipLagFirstSeen
			st.FirstLagAt = &t
			st.LagDuration = now.Sub(tipLagFirstSeen).Round(time.Second).String()
		}
		tipLagStatus = st
		tipLagMu.Unlock()
		return st
	}

	if externalTip > localTip {
		st.LagBlocks = externalTip - localTip
	}
	st.Lagging = st.LagBlocks >= threshold

	tipLagMu.Lock()
	defer tipLagMu.Unlock()
	if st.Lagging {
		if tipLagFirstSeen.IsZero() {
			tipLagFirstSeen = now
		}
		t := tipLagFirstSeen
		st.FirstLagAt = &t
		st.LagDuration = now.Sub(tipLagFirstSeen).Round(time.Second).String()
	} else {
		tipLagFirstSeen = time.Time{}
		st.FirstLagAt = nil
		st.LagDuration = ""
	}
	tipLagStatus = st
	return st
}

// mergeTipLagIntoStatus adds tip-lag fields into a NodeStatus map.
func mergeTipLagIntoStatus(out map[string]any) {
	if out == nil {
		return
	}
	st := GetTipLagStatus()
	if st.CheckedAt.IsZero() {
		return
	}
	out["external_tip"] = st.ExternalTip
	out["tip_lag_blocks"] = st.LagBlocks
	out["tip_lagging"] = st.Lagging
	out["tip_lag_threshold"] = st.Threshold
	if st.ExternalError != "" {
		out["external_tip_error"] = st.ExternalError
	}
	if st.Lagging {
		out["tip_lag_duration"] = st.LagDuration
		// Local IBD-complete "synced" is not enough when we lag the network.
		out["synced"] = false
		out["sync_note"] = "local node reports caught up but lags external explorer tip"
	}
	if !st.CheckedAt.IsZero() {
		out["external_tip_checked_at"] = st.CheckedAt.UTC().Format(time.RFC3339)
	}
}

// tipCatchupWindow returns the inclusive [first, last] height range to process
// this cycle without skipping intermediate heights.
//
// coldStartWindow: when current==0, seed near tip so we do not scan the whole chain.
// maxPerCycle: max blocks to process when already tracking (normal pace).
// catchupBatch: max blocks when behind by more than maxPerCycle (recovery pace).
func tipCatchupWindow(current, tip, maxPerCycle, catchupBatch, coldStartWindow int64) (first, last int64, seededCurrent int64, seeded bool) {
	if maxPerCycle <= 0 {
		maxPerCycle = 3
	}
	if catchupBatch < maxPerCycle {
		catchupBatch = maxPerCycle
	}
	if coldStartWindow <= 0 {
		coldStartWindow = maxPerCycle
	}
	if tip <= 0 {
		return 0, 0, current, false
	}

	cur := current
	if cur == 0 {
		// Cold start: only seed near the tip (historical gaps stay on-demand).
		start := tip
		if tip > coldStartWindow {
			start = tip - (coldStartWindow - 1)
		}
		cur = start - 1
		seeded = true
		seededCurrent = cur
	}

	if tip <= cur {
		return 0, 0, seededCurrent, seeded
	}

	first = cur + 1
	remaining := tip - cur
	batch := maxPerCycle
	if remaining > maxPerCycle {
		batch = catchupBatch
	}
	last = first + batch - 1
	if last > tip {
		last = tip
	}
	return first, last, seededCurrent, seeded
}

// monitorCatchupBatch is the max blocks per cycle when we are more than
// maxPerCycle behind (gap backfill after stall recovery).
func monitorCatchupBatch() int64 {
	if v := os.Getenv("BLOCK_MONITOR_CATCHUP_BATCH"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			if n > 100 {
				n = 100
			}
			return n
		}
	}
	return 25
}

// maybeRestartManagedBtcd restarts the child process if lag has been sustained.
// Returns true if a restart was attempted.
func maybeRestartManagedBtcd(ctx context.Context, node *EmbeddedBtcd, st TipLagStatus) bool {
	if node == nil || !st.Lagging {
		return false
	}
	after := tipLagRestartAfter()
	if after <= 0 {
		return false
	}
	if st.FirstLagAt == nil || time.Since(*st.FirstLagAt) < after {
		return false
	}

	tipLagMu.Lock()
	if !tipLagLastRestart.IsZero() && time.Since(tipLagLastRestart) < tipLagRestartCooldown() {
		tipLagMu.Unlock()
		return false
	}
	tipLagLastRestart = time.Now()
	tipLagMu.Unlock()

	log.Printf("chain tip lag: sustained lag of %d blocks for %s (threshold=%d) — restarting managed btcd",
		st.LagBlocks, st.LagDuration, st.Threshold)

	if err := node.Restart(ctx); err != nil {
		log.Printf("chain tip lag: managed btcd restart failed: %v", err)
		return true
	}
	log.Printf("chain tip lag: managed btcd restarted; waiting for RPC")
	// Best-effort: caller may re-check on next tick. If we have a backend via
	// node config we cannot easily WaitRPC without the client — next status
	// loop will observe recovery or continued lag.
	return true
}
