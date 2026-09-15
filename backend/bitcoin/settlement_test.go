package bitcoin

import (
	"testing"
	"time"
)

func TestSettlementConfirmationsDefaults(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "")
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	if got := SettlementConfirmations(); got != 20 {
		t.Fatalf("testnet4: got %d want 20", got)
	}
	t.Setenv("BITCOIN_NETWORK", "mainnet")
	if got := SettlementConfirmations(); got != 6 {
		t.Fatalf("mainnet: got %d want 6", got)
	}
	t.Setenv("BITCOIN_NETWORK", "regtest")
	if got := SettlementConfirmations(); got != 1 {
		t.Fatalf("regtest: got %d want 1", got)
	}
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "42")
	if got := SettlementConfirmations(); got != 42 {
		t.Fatalf("override: got %d want 42", got)
	}
}

func TestBlockConfirmationsAndSettlementReady(t *testing.T) {
	resetTipLagStateForTest()
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "6")
	t.Setenv("BITCOIN_NETWORK", "mainnet")

	if blockConfirmations(100, 100) != 1 {
		t.Fatalf("1-conf: %d", blockConfirmations(100, 100))
	}
	if blockConfirmations(105, 100) != 6 {
		t.Fatalf("6-conf: %d", blockConfirmations(105, 100))
	}
	if blockConfirmations(99, 100) != 0 {
		t.Fatalf("ahead of tip should be 0")
	}
	if SettlementReady(105, 100) != true {
		t.Fatal("expected ready at 6 confs")
	}
	if SettlementReady(104, 100) != false {
		t.Fatal("expected not ready at 5 confs")
	}
}

func TestScanMayConfirmRejectsHistoricalCatchup(t *testing.T) {
	resetTipLagStateForTest()
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")
	t.Setenv("BITCOIN_NETWORK", "testnet4")

	// 6500-deep catch-up height is settlement-ready but must not confirm on scan.
	if !SettlementReady(152464, 145884) {
		t.Fatal("historical height is buried, SettlementReady should be true")
	}
	if ScanMayConfirm(152464, 145884) {
		t.Fatal("catch-up scan must not confirm a 6500-deep height")
	}

	// Just at settlement depth (20 confs: tip-height+1=20 => tip-height=19).
	if !ScanMayConfirm(119, 100) {
		t.Fatal("near-tip height at exactly 20 confs should confirm")
	}
	if ScanMayConfirm(121, 100) {
		t.Fatal("tip-height=21 is past the live confirm window")
	}
	if ScanMayConfirm(118, 100) {
		t.Fatal("not yet settlement-ready")
	}
}

func TestLiveTipWhileBackfill(t *testing.T) {
	if got := liveTipWhileBackfill(146099, 152464); got != 152464 {
		t.Fatalf("got %d want 152464", got)
	}
	if got := liveTipWhileBackfill(152464, 152464); got != 0 {
		t.Fatalf("backfill already at tip, got %d", got)
	}
	if got := liveTipWhileBackfill(0, 100); got != 100 {
		t.Fatalf("got %d want 100", got)
	}
}

func TestSettlementBlockedOnHashMismatch(t *testing.T) {
	resetTipLagStateForTest()
	now := time.Now()
	st := EvaluateTipLag(100, 100, 3, nil, now)
	if SettlementBlocked() {
		t.Fatal("matching height should not block")
	}
	st = ApplyTipHashCheck(st, 100, "aa", "bb")
	if !st.HashMismatch || !st.Lagging {
		t.Fatalf("expected mismatch+lagging: %+v", st)
	}
	if !SettlementBlocked() {
		t.Fatal("hash mismatch must block settlement")
	}
	if SettlementReady(200, 100) {
		t.Fatal("depth must not override hash mismatch")
	}
}

func TestApplyTipHashCheckSameHash(t *testing.T) {
	resetTipLagStateForTest()
	st := EvaluateTipLag(100, 100, 3, nil, time.Now())
	st = ApplyTipHashCheck(st, 100, "AbCd", "abcd")
	if st.HashMismatch {
		t.Fatalf("case-insensitive match should pass: %+v", st)
	}
	if SettlementBlocked() {
		t.Fatal("matching hashes must not block")
	}
}

func TestSettlementBlocked_RegtestIgnoresForeignExplorerTip(t *testing.T) {
	resetTipLagStateForTest()
	t.Setenv("BITCOIN_NETWORK", "regtest")
	t.Setenv("CHAIN_EXTERNAL_TIP_CHECK", "")
	// Simulate a leftover/testnet4-sized explorer tip. Must not block.
	EvaluateTipLag(10, 150232, 3, nil, time.Now())
	if SettlementBlocked() {
		t.Fatal("regtest must not SettlementBlocked on a foreign (testnet4-sized) tip")
	}
	if SettlementReady(10, 10) != true {
		t.Fatal("regtest 1-conf settlement must still be ready when not forked")
	}
}

func TestMinPeerCountDefault(t *testing.T) {
	t.Setenv("BTCD_MIN_PEERS", "")
	if got := minPeerCount(); got != 1 {
		t.Fatalf("default min peers=%d want 1", got)
	}
	t.Setenv("BTCD_MIN_PEERS", "4")
	if got := minPeerCount(); got != 4 {
		t.Fatalf("got %d", got)
	}
}

func TestReorgWatchDepthTracksSettlement(t *testing.T) {
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "20")
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	if got := reorgWatchDepth(); got != 20 {
		t.Fatalf("got %d", got)
	}
	t.Setenv("CHAIN_SETTLEMENT_CONFIRMATIONS", "1")
	if got := reorgWatchDepth(); got != 2 {
		t.Fatalf("floor 2, got %d", got)
	}
}
