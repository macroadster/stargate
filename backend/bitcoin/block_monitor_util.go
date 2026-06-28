package bitcoin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"

	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
)

func fundingTxIDsFromMeta(meta map[string]any) []string {
	var txids []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range txids {
			if existing == value {
				return
			}
		}
		txids = append(txids, value)
	}
	if meta == nil {
		return txids
	}

	switch v := meta["funding_txids"].(type) {
	case []string:
		for _, txid := range v {
			add(txid)
		}
	case []any:
		for _, item := range v {
			if txid, ok := item.(string); ok {
				add(txid)
			}
		}
	case string:
		for _, part := range strings.Split(v, ",") {
			add(part)
		}
	}
	return txids
}

func outputAddresses(script []byte, params *chaincfg.Params) []string {
	class, addrs, _, err := txscript.ExtractPkScriptAddrs(script, params)
	if err != nil || class == txscript.NonStandardTy {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if addr != nil {
			out = append(out, addr.EncodeAddress())
		}
	}
	return out
}

// persistDiscoveryContract saves a contract discovered purely from on-chain
// OP_RETURN data to the MCP store and ingestion database.  This enables a
// fresh instance to rebuild its database from block scans before IPFS sync
// delivers the original wish image.
func (bm *BlockMonitor) persistDiscoveryContract(contractID, wishHash, txID string, blockHeight int64, productHash string) {
	ctx := context.Background()

	// Persist to MCP store so the contract is visible in /api/contracts.
	if upserter, ok := bm.sweepStore.(contractUpserter); ok {
		bh := int(blockHeight)
		now := time.Now()
		c := smart_contract.Contract{
			ContractID:           contractID,
			Title:                "Wish " + wishHash[:8] + "...",
			Status:               "confirmed",
			ConfirmedBlockHeight: &bh,
			ConfirmedAt:          &now,
			Metadata: map[string]interface{}{
				"visible_pixel_hash": wishHash,
				"confirmed_txid":     txID,
				"confirmed_height":   blockHeight,
				"match_type":         "op_return_discovery",
				"product_hash":       productHash,
			},
			CreatedAt: now,
		}
		if err := upserter.UpsertContractWithTasks(ctx, c, nil); err != nil {
			log.Printf("oracle reconcile: failed to persist discovery contract %s: %v", contractID, err)
		} else {
			log.Printf("oracle reconcile: persisted discovery contract %s to MCP store", contractID)
		}
		// Also call ConfirmContract to set confirmed metadata consistently.
		if err := bm.sweepStore.ConfirmContract(ctx, contractID, bh, txID); err != nil {
			log.Printf("oracle reconcile: ConfirmContract for %s: %v", contractID, err)
		}
	}

	// Create ingestion record so re-scans match via the candidate path and
	// IPFS sync can later enrich with the actual wish image.
	if bm.ingestion != nil {
		rec := services.IngestionRecord{
			ID:     wishHash,
			Method: "on_chain_discovery",
			Status: "confirmed",
			Metadata: map[string]interface{}{
				"visible_pixel_hash": wishHash,
				"confirmed_txid":     txID,
				"confirmed_height":   blockHeight,
				"match_type":         "op_return_discovery",
			},
		}
		if err := bm.ingestion.Create(rec); err != nil {
			log.Printf("oracle reconcile: failed to create discovery ingestion for %s: %v", wishHash, err)
		} else {
			log.Printf("oracle reconcile: created discovery ingestion record %s", wishHash)
		}
	}
}

func hashPrefixFromFilename(filename string) string {
	if strings.TrimSpace(filename) == "" {
		return ""
	}
	base := filepath.Base(filename)
	sep := strings.Index(base, "_")
	if sep <= 0 {
		return ""
	}
	prefix := normalizeHex(base[:sep])
	if len(prefix) != 40 && len(prefix) != 64 {
		return ""
	}
	return prefix
}

func commitmentScriptHashFromMeta(rec services.IngestionRecord, params *chaincfg.Params) string {
	if params == nil || rec.Metadata == nil {
		return ""
	}
	visible := normalizeHex(stringFromAny(rec.Metadata["visible_pixel_hash"]))
	if len(visible) != 64 {
		return ""
	}
	pixelBytes, err := hex.DecodeString(visible)
	if err != nil {
		return ""
	}
	// The hashlock redeem script is OP_SHA256 <SHA256(pixelHash)> OP_EQUAL —
	// it depends only on visible_pixel_hash, not on any address.  Peer nodes
	// that receive the contract via stego announcement don't have
	// commitment_lock_address, so we compute the script hash directly.
	redeemScript, err := buildHashlockRedeemScript(pixelBytes)
	if err != nil {
		return ""
	}
	scriptHash := sha256.Sum256(redeemScript)
	return hex.EncodeToString(scriptHash[:])
}

// isIdentityHash returns true if hash is a known contract-identity hash of the
// ingestion record (visible_pixel_hash, pixel_hash, or commitment_script_hash).
// Used to reject witness matches against incidental fallback hashes (e.g. rec.ID
// or filename prefix) that are not cryptographic contract identifiers.
func isIdentityHash(rec *services.IngestionRecord, hash string, params *chaincfg.Params) bool {
	hash = normalizeHex(hash)
	if hash == "" {
		return false
	}
	if v := normalizeHex(stringFromAny(rec.Metadata["visible_pixel_hash"])); v != "" && v == hash {
		return true
	}
	if v := normalizeHex(stringFromAny(rec.Metadata["pixel_hash"])); v != "" && v == hash {
		return true
	}
	if v := commitmentScriptHashFromMeta(*rec, params); normalizeHex(v) == hash {
		return true
	}
	if v := normalizeHex(stringFromAny(rec.Metadata["product_hash"])); v != "" && v == hash {
		return true
	}
	return false
}

func scriptAddressHashes(script []byte, params *chaincfg.Params) []string {
	class, addrs, _, err := txscript.ExtractPkScriptAddrs(script, params)
	if err != nil {
		return nil
	}
	if class != txscript.ScriptHashTy && class != txscript.WitnessV0ScriptHashTy {
		return nil
	}
	var out []string
	for _, addr := range addrs {
		hash := hex.EncodeToString(addr.ScriptAddress())
		if hash != "" {
			out = append(out, hash)
		}
	}
	return out
}
