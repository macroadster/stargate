package bitcoin

import (
	"os"
	"strconv"
	"strings"
)

// SettlementConfirmations is how many blocks must bury a tx before Stargate
// treats it as settlement-final (task proofs / ConfirmContract).
//
// These helpers are read-only gates. They must never submit explorer blocks
// or call invalidateblock based on mempool.space.
//
// Override with CHAIN_SETTLEMENT_CONFIRMATIONS. Defaults are conservative on
// cheap-PoW networks (testnet4 / signet) where a minority fork is inexpensive.
func SettlementConfirmations() int64 {
	if v := strings.TrimSpace(os.Getenv("CHAIN_SETTLEMENT_CONFIRMATIONS")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 1 {
			if n > 1000 {
				return 1000
			}
			return n
		}
	}
	switch strings.ToLower(strings.TrimSpace(GetCurrentNetwork())) {
	case "mainnet", "main":
		return 6
	case "simnet", "regtest":
		return 1
	default: // testnet3, testnet4, signet
		return 20
	}
}

// blockConfirmations is 0 when tip is unknown or the block is not on the tip chain.
func blockConfirmations(tip, height int64) int64 {
	if tip <= 0 || height <= 0 || tip < height {
		return 0
	}
	return tip - height + 1
}

// SettlementReady reports whether a block at height is deep enough and the
// local tip is not on a conflicting fork (hash mismatch) or badly lagged.
func SettlementReady(tip, height int64) bool {
	if SettlementBlocked() {
		return false
	}
	return blockConfirmations(tip, height) >= SettlementConfirmations()
}

// ScanMayConfirm is SettlementReady plus a near-tip bound.
//
// Catch-up scans heights thousands of blocks behind the live tip, so
// SettlementReady is true for every one of them. Confirming on that path
// rewrites confirmed_height to the scanned historical block and re-runs
// stego extract (stargate-3p2.3). Live confirm stays in the window just
// past settlement depth; older proofs promote via promoteProvisionalProofs.
func ScanMayConfirm(tip, height int64) bool {
	if !SettlementReady(tip, height) {
		return false
	}
	need := SettlementConfirmations()
	if need < 1 {
		need = 1
	}
	return tip-height <= need
}

// liveTipWhileBackfill is the live tip to process in parallel with sequential
// gap backfill. 0 means the backfill range already includes the tip.
func liveTipWhileBackfill(backfillLast, tip int64) int64 {
	if tip > 0 && tip > backfillLast {
		return tip
	}
	return 0
}

// SettlementBlocked is true when health says we must not confirm new contracts:
// explorer/local hash mismatch (possible eclipse / minority fork) or tip lag
// against a real explorer. Local-only networks never inherit another chain's tip.
func SettlementBlocked() bool {
	st := GetTipLagStatus()
	if st.CheckedAt.IsZero() {
		return false
	}
	if st.HashMismatch {
		return true
	}
	if localOnlyChain(GetCurrentNetwork()) {
		return false
	}
	return st.Lagging
}

// reorgWatchDepth is how many recent heights to re-check for hash changes.
func reorgWatchDepth() int {
	n := SettlementConfirmations()
	if n < 2 {
		return 2
	}
	if n > 64 {
		return 64
	}
	return int(n)
}

// minPeerCount is the floor on btcd peers before Synced() / health report
// the node as caught up. Isolation (0 peers) is an eclipse precondition.
// Override with BTCD_MIN_PEERS. Default 1 (do not treat 0-peer as synced).
func minPeerCount() int {
	if v := strings.TrimSpace(os.Getenv("BTCD_MIN_PEERS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			if n > 64 {
				return 64
			}
			return n
		}
	}
	return 1
}
