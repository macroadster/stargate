package bitcoin

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/btcsuite/btcd/wire/v2"

	"stargate-backend/core/identity"
	"stargate-backend/services"
)

// chainReplicationState is the fresh-node cursor. Tip-only block monitoring
// does not walk history. This file remembers how far the OP_RETURN walk got
// and which commitments are still waiting on a mirrored stego image.
type chainReplicationState struct {
	NextHeight int64             `json:"next_height"`
	FailHeight int64             `json:"fail_height,omitempty"`
	FailCount  int               `json:"fail_count,omitempty"`
	Pending    []chainCommitment `json:"pending,omitempty"`
}

// chainCommitment is one on-chain wish_hash||stego_hash. The contracts page
// lists it only after the stego image is reconciled and the funding block is
// buried. The blocks page lists it as soon as that block is stored.
type chainCommitment struct {
	Height     int64  `json:"height"`
	TxID       string `json:"txid"`
	WishHash   string `json:"wish_hash"`
	StegoHash  string `json:"stego_hash"`
	Reconciled bool   `json:"reconciled,omitempty"`
	Attempts   int    `json:"attempts,omitempty"`
}

func (c chainCommitment) key() string {
	return c.TxID + "|" + c.WishHash + "|" + c.StegoHash
}

func (c chainCommitment) normalize() chainCommitment {
	c.TxID = strings.ToLower(strings.TrimSpace(c.TxID))
	wish := identity.CanonicalContractID(c.WishHash)
	if wish == "" {
		wish = strings.ToLower(strings.TrimSpace(c.WishHash))
	}
	c.WishHash = wish
	c.StegoHash = strings.ToLower(strings.TrimSpace(c.StegoHash))
	return c
}

func contractDiscoverEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BLOCK_MONITOR_CONTRACT_DISCOVER"))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

func contractDiscoverInterval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("BLOCK_MONITOR_CONTRACT_DISCOVER_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Second {
			return d
		}
	}
	return 5 * time.Second
}

func contractDiscoverBatch() int64 {
	n := int64(100)
	if v := strings.TrimSpace(os.Getenv("BLOCK_MONITOR_CONTRACT_DISCOVER_BATCH")); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil && parsed > 0 {
			n = parsed
		}
	}
	if n > 500 {
		return 500
	}
	return n
}

func contractDiscoverStoreAttempts() int {
	n := 5
	if v := strings.TrimSpace(os.Getenv("BLOCK_MONITOR_CONTRACT_DISCOVER_STORE_ATTEMPTS")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	if n > 50 {
		return 50
	}
	return n
}

func (bm *BlockMonitor) replicationPath() string {
	if bm == nil || strings.TrimSpace(bm.blocksDir) == "" {
		return ""
	}
	return filepath.Join(bm.blocksDir, "chain-contracts.json")
}

func (bm *BlockMonitor) loadReplicationLocked() {
	if bm.replicationLoaded {
		return
	}
	bm.replicationLoaded = true
	path := bm.replicationPath()
	if path == "" {
		return
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var st chainReplicationState
	if json.Unmarshal(blob, &st) != nil {
		return
	}
	for i := range st.Pending {
		st.Pending[i] = st.Pending[i].normalize()
	}
	bm.replication = st
}

func (bm *BlockMonitor) saveReplicationLocked() {
	path := bm.replicationPath()
	if path == "" {
		return
	}
	blob, err := json.MarshalIndent(bm.replication, "", "  ")
	if err != nil {
		log.Printf("contract discover: encode state: %v", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o644); err != nil {
		log.Printf("contract discover: write state: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("contract discover: rename state: %v", err)
	}
}

func (bm *BlockMonitor) contractDiscoverLoop(stop <-chan struct{}) {
	defer bm.wg.Done()
	interval := contractDiscoverInterval()
	log.Printf("contract discover: walking local blocks for OP_RETURN contracts every %s (batch %d)", interval, contractDiscoverBatch())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	startup := time.NewTimer(interval)
	defer startup.Stop()
	for {
		select {
		case <-stop:
			return
		case <-startup.C:
			bm.discoverContractBlocks(context.Background())
		case <-ticker.C:
			bm.discoverContractBlocks(context.Background())
		}
	}
}

// NoteStegoFile tells the block monitor that a mirrored upload is on disk.
// The base name of a content-addressed file is the stego hash from OP_RETURN.
func (bm *BlockMonitor) NoteStegoFile(name string) {
	if bm == nil {
		return
	}
	name = strings.ToLower(strings.TrimSpace(filepath.Base(name)))
	if len(name) != 64 {
		return
	}
	if _, err := hex.DecodeString(name); err != nil {
		return
	}
	bm.retryPendingChainContracts(name)
}

// queueMissingStego keeps a commitment whose stego image has not been mirrored yet.
func (bm *BlockMonitor) queueMissingStego(err error, height int64, txid string, match *services.IngestionRecord, wishHash, stegoHash string) {
	if !errorsIsStegoMissing(err) {
		return
	}
	wish := strings.TrimSpace(wishHash)
	if wish == "" && match != nil && match.Metadata != nil {
		wish = stringFromAny(match.Metadata["visible_pixel_hash"])
	}
	if wish == "" && match != nil {
		wish = match.ID
	}
	bm.noteChainCommitment(chainCommitment{
		Height:    height,
		TxID:      txid,
		WishHash:  wish,
		StegoHash: stegoHash,
	})
}

func errorsIsStegoMissing(err error) bool {
	return errors.Is(err, errStegoNotOnDisk)
}

func (bm *BlockMonitor) noteChainCommitment(c chainCommitment) {
	if bm == nil {
		return
	}
	c = c.normalize()
	if c.Height < 0 || c.TxID == "" || c.WishHash == "" || len(c.StegoHash) != 64 {
		return
	}
	bm.replicationMu.Lock()
	bm.loadReplicationLocked()
	fresh := bm.upsertPendingLocked(c)
	bm.saveReplicationLocked()
	bm.replicationMu.Unlock()
	if fresh && !stegoFileOnDisk(c.StegoHash) {
		log.Printf("contract discover: queued wish %s stego %s at height %d until the image is mirrored", c.WishHash, c.StegoHash, c.Height)
	}
	bm.retryPendingChainContracts(c.StegoHash)
}

func (bm *BlockMonitor) upsertPendingLocked(c chainCommitment) bool {
	for i := range bm.replication.Pending {
		if bm.replication.Pending[i].key() == c.key() {
			return false
		}
	}
	bm.replication.Pending = append(bm.replication.Pending, c)
	return true
}

func (bm *BlockMonitor) retryPendingChainContracts(onlyStego string) {
	if bm == nil {
		return
	}
	onlyStego = strings.ToLower(strings.TrimSpace(onlyStego))
	bm.replicationMu.Lock()
	bm.loadReplicationLocked()
	snapshot := append([]chainCommitment(nil), bm.replication.Pending...)
	bm.replicationMu.Unlock()
	if len(snapshot) == 0 {
		return
	}
	keep := make([]chainCommitment, 0, len(snapshot))
	changed := false
	for _, c := range snapshot {
		if onlyStego != "" && c.StegoHash != onlyStego && c.WishHash != onlyStego {
			keep = append(keep, c)
			continue
		}
		done, updated := bm.applyChainCommitment(c)
		if done {
			changed = true
			continue
		}
		if updated.Reconciled != c.Reconciled || updated.Attempts != c.Attempts || updated.WishHash != c.WishHash {
			changed = true
		}
		keep = append(keep, updated)
	}
	if !changed {
		return
	}
	bm.replicationMu.Lock()
	bm.loadReplicationLocked()
	bm.replication.Pending = mergePending(bm.replication.Pending, keep, snapshot)
	bm.saveReplicationLocked()
	bm.replicationMu.Unlock()
}

func mergePending(current, keep, snapshot []chainCommitment) []chainCommitment {
	removed := make(map[string]struct{}, len(snapshot))
	for _, c := range snapshot {
		removed[c.key()] = struct{}{}
	}
	kept := make(map[string]chainCommitment, len(keep))
	for _, c := range keep {
		kept[c.key()] = c
		delete(removed, c.key())
	}
	out := make([]chainCommitment, 0, len(current)+len(kept))
	seen := make(map[string]struct{}, len(current))
	for _, c := range current {
		k := c.key()
		if _, drop := removed[k]; drop {
			continue
		}
		if updated, ok := kept[k]; ok {
			c = updated
			delete(kept, k)
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, c)
	}
	for _, c := range kept {
		if _, ok := seen[c.key()]; ok {
			continue
		}
		seen[c.key()] = struct{}{}
		out = append(out, c)
	}
	return out
}

// applyChainCommitment reconciles a mirrored stego image into the contract
// store and confirms it once the funding block is buried.
//
// scanMayConfirm refuses historical heights so a catch-up rescan cannot
// rewrite an already-confirmed height (stargate-3p2.3). A fresh node has no
// row yet. Confirming here is the first write, and an existing confirmed or
// completed row is left alone.
func (bm *BlockMonitor) applyChainCommitment(c chainCommitment) (bool, chainCommitment) {
	c = c.normalize()
	if c.WishHash == "" || len(c.StegoHash) != 64 {
		return true, c
	}
	id := c.WishHash
	if !c.Reconciled && stegoFileOnDisk(c.StegoHash) {
		err := bm.reconcileOnChainArtifacts(id, c.StegoHash)
		switch {
		case err == nil && bm.contractRowExists(id):
			c.Reconciled = true
			c.Attempts = 0
		case err == nil:
			c.Attempts++
			bm.logReconcileAttempt(c, fmt.Errorf("reconcile wrote no contract row"))
		case errors.Is(err, errStegoNotOnDisk):
			// Image disappeared between the stat and the read. Keep waiting.
		default:
			c.Attempts++
			bm.logReconcileAttempt(c, err)
		}
	}
	if !c.Reconciled {
		return false, c
	}
	if bm.chainContractSettled(id) {
		return true, c
	}
	if bm.sweepStore == nil || !bm.settlementReady(c.Height) {
		return false, c
	}
	bm.confirmContractTasks(id, c.TxID, c.Height)
	if err := bm.confirmContractOnChain(context.Background(), id, int(c.Height), c.TxID); err != nil {
		c.Attempts++
		bm.logReconcileAttempt(c, err)
		return false, c
	}
	if !bm.chainContractSettled(id) {
		c.Attempts++
		bm.logReconcileAttempt(c, fmt.Errorf("confirm did not settle %s", id))
		return false, c
	}
	log.Printf("contract discover: confirmed %s at height %d tx %s", id, c.Height, c.TxID)
	return true, c
}

func (bm *BlockMonitor) logReconcileAttempt(c chainCommitment, err error) {
	if c.Attempts == 1 || c.Attempts%10 == 0 {
		log.Printf("contract discover: wish %s stego %s attempt %d: %v", c.WishHash, c.StegoHash, c.Attempts, err)
	}
}

func (bm *BlockMonitor) contractRowExists(id string) bool {
	getter, ok := bm.sweepStore.(contractGetter)
	if !ok || getter == nil || strings.TrimSpace(id) == "" {
		return false
	}
	got, err := getter.GetContract(id)
	return err == nil && strings.TrimSpace(got.ContractID) != ""
}

func (bm *BlockMonitor) chainContractSettled(id string) bool {
	getter, ok := bm.sweepStore.(contractGetter)
	if !ok || getter == nil {
		return false
	}
	got, err := getter.GetContract(id)
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(got.Status)) {
	case "confirmed", "completed", "superseded":
		return true
	default:
		return false
	}
}

func (bm *BlockMonitor) discoverContractBlocks(ctx context.Context) {
	if bm == nil || !contractDiscoverEnabled() {
		return
	}
	bm.discoverMu.Lock()
	defer bm.discoverMu.Unlock()
	bm.retryPendingChainContracts("")
	if bm.chain != nil {
		synced, syncErr := bm.chain.Synced(context.Background())
		if syncErr == nil && !synced {
			if !bm.discoverWaitLogged {
				log.Printf("contract discover: waiting until btcd finishes initial block download")
				bm.discoverWaitLogged = true
			}
			return
		}
	}
	bm.discoverWaitLogged = false
	tip, err := bm.getChainTipHeight()
	if err != nil || tip < 0 {
		return
	}
	bm.replicationMu.Lock()
	bm.loadReplicationLocked()
	next := bm.replication.NextHeight
	failHeight := bm.replication.FailHeight
	failCount := bm.replication.FailCount
	bm.replicationMu.Unlock()
	if next < 0 {
		next = 0
	}
	if next > tip {
		return
	}
	batch := contractDiscoverBatch()
	end := next + batch - 1
	if end > tip {
		end = tip
	}
	start := next
	for h := start; h <= end; h++ {
		if ctx.Err() != nil {
			bm.setDiscoverProgress(next, failHeight, failCount)
			return
		}
		hexData, err := bm.rawBlockHex(h)
		if err != nil {
			log.Printf("contract discover: block %d not available yet: %v", h, err)
			bm.setDiscoverProgress(next, failHeight, failCount)
			if next > start {
				bm.logDiscoverProgress(start, next-1, tip)
			}
			return
		}
		raw, err := hex.DecodeString(strings.TrimSpace(hexData))
		if err != nil {
			log.Printf("contract discover: block %d hex: %v", h, err)
			next = h + 1
			continue
		}
		hits, err := commitmentsFromRawBlock(raw)
		if err != nil {
			log.Printf("contract discover: block %d parse: %v", h, err)
			next = h + 1
			continue
		}
		if len(hits) == 0 {
			next = h + 1
			continue
		}
		for i := range hits {
			hits[i].Height = h
			bm.noteChainCommitment(hits[i])
		}
		if err := bm.ProcessBlock(h); err != nil {
			n := 1
			if failHeight == h {
				n = failCount + 1
			}
			log.Printf("contract discover: store block %d: %v (attempt %d)", h, err, n)
			if n >= contractDiscoverStoreAttempts() {
				log.Printf("contract discover: skipping block store at %d after %d attempts; contract rows still follow the pending queue", h, n)
				next = h + 1
				failHeight = 0
				failCount = 0
				continue
			}
			bm.setDiscoverProgress(h, h, n)
			log.Printf("contract discover: paused at height %d / tip %d", h, tip)
			return
		}
		next = h + 1
		failHeight = 0
		failCount = 0
	}
	bm.setDiscoverProgress(next, failHeight, failCount)
	if next > start {
		bm.logDiscoverProgress(start, next-1, tip)
	}
}

func (bm *BlockMonitor) logDiscoverProgress(from, through, tip int64) {
	bm.replicationMu.Lock()
	pending := len(bm.replication.Pending)
	bm.replicationMu.Unlock()
	log.Printf("contract discover: scanned %d..%d / tip %d (%d pending)", from, through, tip, pending)
}

func (bm *BlockMonitor) setDiscoverProgress(next, failHeight int64, failCount int) {
	bm.replicationMu.Lock()
	defer bm.replicationMu.Unlock()
	bm.loadReplicationLocked()
	bm.replication.NextHeight = next
	bm.replication.FailHeight = failHeight
	bm.replication.FailCount = failCount
	bm.saveReplicationLocked()
}

func (bm *BlockMonitor) rawBlockHex(height int64) (string, error) {
	if bm.chain != nil {
		hexData, err := bm.chain.GetRawBlockHex(context.Background(), height)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(hexData) == "" {
			return "", fmt.Errorf("empty block %d", height)
		}
		return hexData, nil
	}
	if bm.rawClient == nil {
		return "", fmt.Errorf("no chain backend")
	}
	return bm.rawClient.GetRawBlockHex(height)
}

func commitmentsFromRawBlock(raw []byte) ([]chainCommitment, error) {
	var msg wire.MsgBlock
	if err := msg.Deserialize(bytes.NewReader(raw)); err != nil {
		return nil, err
	}
	out := make([]chainCommitment, 0)
	for _, tx := range msg.Transactions {
		if tx == nil {
			continue
		}
		txid := strings.ToLower(tx.TxHash().String())
		for _, output := range tx.TxOut {
			if output == nil {
				continue
			}
			wish, stego, ok := parseOPReturnHashes(output.PkScript)
			if !ok || stego == "" {
				continue
			}
			out = append(out, chainCommitment{
				TxID:      txid,
				WishHash:  wish,
				StegoHash: stego,
			}.normalize())
		}
	}
	return out, nil
}
