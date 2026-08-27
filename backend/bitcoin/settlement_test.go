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
