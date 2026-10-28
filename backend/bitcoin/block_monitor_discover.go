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

// pendingQueueVersion is the chain-contracts.json shape that stores one row
// per wish and only raw hash pairs. Older files are compacted on load.
const pendingQueueVersion = 2

// contractDiscoverReconcileLimit is how many times a mirrored file may fail
// to produce a contract row before the walk gives up on that wish.
const contractDiscoverReconcileLimit = 20

// chainReplicationState is the fresh-node cursor. Tip-only block monitoring
// does not walk history. This file remembers how far the OP_RETURN walk got
// and which commitments are still waiting on a mirrored stego image.
type chainReplicationState struct {
	NextHeight     int64             `json:"next_height"`
	FailHeight     int64             `json:"fail_height,omitempty"`
	FailCount      int               `json:"fail_count,omitempty"`
	PendingVersion int               `json:"pending_version,omitempty"`
	CaughtUpSince  int64             `json:"caught_up_since,omitempty"`
	Pending        []chainCommitment `json:"pending,omitempty"`
}

// chainCommitment is one on-chain wish. The contracts page lists it only
// after the stego image is reconciled and the funding block is buried.
// The blocks page lists it only when UPLOADS_DIR holds the wish image.
// An OP_RETURN hash pair alone is generic pay-to-anchor, not a Stargate
// contract. A later transaction for the same wish replaces this row.
type chainCommitment struct {
	Height       int64  `json:"height"`
	TxID         string `json:"txid"`
	WishHash     string `json:"wish_hash"`
	StegoHash    string `json:"stego_hash"`
	Reconciled   bool   `json:"reconciled,omitempty"`
	Attempts     int    `json:"attempts,omitempty"`
	MissingSince int64  `json:"missing_since,omitempty"`
	Legacy       bool   `json:"legacy,omitempty"`
}

// key is the wish. Settlement is per wish, so a second transaction must not
// become a second contract waiting for confirmation.
func (c chainCommitment) key() string {
	return c.WishHash
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

// contractDiscoverMissGrace is how long a new commitment may wait for its
// stego file after the walk has caught up. Rows loaded from a pre-filter
// queue are not held for this long; they drop on the first caught-up pass
// when the file and the contract row are both still absent.
func contractDiscoverMissGrace() time.Duration {
	if v := strings.TrimSpace(os.Getenv("BLOCK_MONITOR_CONTRACT_DISCOVER_MISS_GRACE")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			return d
		}
	}
	return 6 * time.Hour
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
	before := len(st.Pending)
	pending, changed := compactPending(st.Pending)
	st.Pending = pending
	if st.PendingVersion < pendingQueueVersion {
		for i := range st.Pending {
			st.Pending[i].Legacy = true
		}
		st.PendingVersion = pendingQueueVersion
		changed = true
	}
	bm.replication = st
	if !changed {
		return
	}
	if len(pending) != before {
		log.Printf("contract discover: compacted pending queue from %d to %d", before, len(pending))
	}
	bm.saveReplicationLocked()
}

func (bm *BlockMonitor) saveReplicationLocked() {
	path := bm.replicationPath()
	if path == "" {
		return
	}
	if bm.replication.PendingVersion < pendingQueueVersion {
		bm.replication.PendingVersion = pendingQueueVersion
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
// The base name is a content hash: the wish image or the stego image from OP_RETURN.
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
	bm.storeReadyWishBlocks()
}

// storeReadyWishBlocks stores queued blocks whose wish image is now in uploads.
// A hash pair with no wish image is left queued and is not a smart contract.
// Chain hex must already be local: an empty reply is not fetched from a public explorer.
func (bm *BlockMonitor) storeReadyWishBlocks() {
	if bm == nil || bm.chain == nil {
		return
	}
	if !bm.processMu.TryLock() {
		return
	}
	bm.processMu.Unlock()

	bm.replicationMu.Lock()
	bm.loadReplicationLocked()
	pending := append([]chainCommitment(nil), bm.replication.Pending...)
	bm.replicationMu.Unlock()

	seenHeight := make(map[int64]struct{}, len(pending))
	for _, c := range pending {
		if !wishImageOnDisk(c.WishHash) || bm.wishAlreadyStored(c.WishHash, c.Height) {
			continue
		}
		if _, ok := seenHeight[c.Height]; ok {
			continue
		}
		hexData, err := bm.chain.GetRawBlockHex(context.Background(), c.Height)
		if err != nil || strings.TrimSpace(hexData) == "" {
			continue
		}
		seenHeight[c.Height] = struct{}{}
		if err := bm.ProcessBlock(c.Height); err != nil {
			n := bm.noteWishStoreTry(c.WishHash)
			log.Printf("contract discover: wish image for %s is in uploads, store block %d: %v (attempt %d)", c.WishHash, c.Height, err, n)
			if n >= contractDiscoverStoreAttempts() {
				bm.markWishStored(c.WishHash, c.Height)
				log.Printf("contract discover: gave up storing block %d for wish %s after %d attempts", c.Height, c.WishHash, n)
			}
			continue
		}
		for _, other := range pending {
			if other.Height == c.Height && wishImageOnDisk(other.WishHash) {
				bm.markWishStored(other.WishHash, other.Height)
			}
		}
	}
}

func (bm *BlockMonitor) wishAlreadyStored(wish string, height int64) bool {
	bm.wishStoreMu.Lock()
	defer bm.wishStoreMu.Unlock()
	prev, ok := bm.wishStored[wish]
	return ok && prev >= height
}

func (bm *BlockMonitor) markWishStored(wish string, height int64) {
	if bm == nil || wish == "" {
		return
	}
	bm.wishStoreMu.Lock()
	defer bm.wishStoreMu.Unlock()
	if bm.wishStored == nil {
		bm.wishStored = make(map[string]int64)
	}
	if prev, ok := bm.wishStored[wish]; ok && prev >= height {
		return
	}
	bm.wishStored[wish] = height
}

func (bm *BlockMonitor) noteWishStoreTry(wish string) int {
	bm.wishStoreMu.Lock()
	defer bm.wishStoreMu.Unlock()
	if bm.wishStoreTries == nil {
		bm.wishStoreTries = make(map[string]int)
	}
	bm.wishStoreTries[wish]++
	return bm.wishStoreTries[wish]
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
	if !pendingCommitmentOK(c) {
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
	if !pendingCommitmentOK(c) {
		return false
	}
	for i := range bm.replication.Pending {
		prev := &bm.replication.Pending[i]
		if prev.WishHash != c.WishHash {
			continue
		}
		if c.Height < prev.Height {
			return false
		}
		if c.StegoHash == prev.StegoHash {
			c.Reconciled = prev.Reconciled
			c.Attempts = prev.Attempts
			c.MissingSince = prev.MissingSince
		}
		c.Legacy = prev.Legacy
		*prev = c
		return false
	}
	bm.replication.Pending = append(bm.replication.Pending, c)
	return true
}

// pendingCommitmentOK is the gate for the walk queue. One wish, two distinct
// raw digests, and a real transaction.
func pendingCommitmentOK(c chainCommitment) bool {
	if c.Height < 0 || strings.TrimSpace(c.TxID) == "" {
		return false
	}
	wish, errW := hex.DecodeString(c.WishHash)
	stego, errS := hex.DecodeString(c.StegoHash)
	if errW != nil || errS != nil || len(wish) != 32 || len(stego) != 32 {
		return false
	}
	return commitmentBytesOK(append(append([]byte{}, wish...), stego...))
}

// compactPending drops payloads that are not a Stargate hash pair and keeps
// the newest transaction for each wish.
func compactPending(in []chainCommitment) ([]chainCommitment, bool) {
	changed := false
	byWish := make(map[string]chainCommitment, len(in))
	order := make([]string, 0, len(in))
	for _, c := range in {
		c = c.normalize()
		if !pendingCommitmentOK(c) {
			changed = true
			continue
		}
		prev, ok := byWish[c.WishHash]
		if !ok {
			byWish[c.WishHash] = c
			order = append(order, c.WishHash)
			continue
		}
		changed = true
		if c.Height < prev.Height {
			continue
		}
		if c.StegoHash == prev.StegoHash {
			c.Reconciled = prev.Reconciled
			c.Attempts = prev.Attempts
			if c.MissingSince == 0 {
				c.MissingSince = prev.MissingSince
			}
		}
		c.Legacy = prev.Legacy || c.Legacy
		byWish[c.WishHash] = c
	}
	if len(order) != len(in) {
		changed = true
	}
	out := make([]chainCommitment, 0, len(order))
	for _, wish := range order {
		out = append(out, byWish[wish])
	}
	return out, changed
}

func (bm *BlockMonitor) retryPendingChainContracts(onlyStego string) {
	if bm == nil {
		return
	}
	onlyStego = strings.ToLower(strings.TrimSpace(onlyStego))
	bm.replicationMu.Lock()
	bm.loadReplicationLocked()
	snapshot := append([]chainCommitment(nil), bm.replication.Pending...)
	clock := pendingClock{
		now:    time.Now().Unix(),
		grace:  contractDiscoverMissGrace(),
		caught: bm.replication.CaughtUpSince,
		next:   bm.replication.NextHeight,
	}
	bm.replicationMu.Unlock()
	if len(snapshot) == 0 {
		return
	}
	if tip, err := bm.getChainTipHeight(); err == nil && tip >= 0 && clock.next > tip {
		clock.pastTip = true
	}
	keep := make([]chainCommitment, 0, len(snapshot))
	changed := false
	expired := 0
	gaveUp := 0
	for _, c := range snapshot {
		if onlyStego != "" && c.StegoHash != onlyStego && c.WishHash != onlyStego {
			keep = append(keep, c)
			continue
		}
		done, updated := bm.applyChainCommitment(c, clock)
		if done {
			changed = true
			switch {
			case updated.Legacy && clock.pastTip && !updated.Reconciled:
				expired++
			case !updated.Reconciled && updated.Attempts >= contractDiscoverReconcileLimit:
				gaveUp++
			case !updated.Reconciled && updated.MissingSince > 0 && clock.pastTip && !bm.contractRowExists(updated.WishHash):
				expired++
			}
			continue
		}
		if updated.Reconciled != c.Reconciled || updated.Attempts != c.Attempts || updated.WishHash != c.WishHash || updated.MissingSince != c.MissingSince || updated.Legacy != c.Legacy {
			changed = true
		}
		keep = append(keep, updated)
	}
	if expired > 0 {
		log.Printf("contract discover: dropped %d pending commitments with no stego image and no contract row", expired)
	}
	if gaveUp > 0 {
		log.Printf("contract discover: dropped %d pending commitments after %d reconcile attempts", gaveUp, contractDiscoverReconcileLimit)
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

// pendingClock is one retry pass. now is unix seconds. pastTip is true when
// the walk cursor is already above the chain tip.
type pendingClock struct {
	now     int64
	grace   time.Duration
	caught  int64
	next    int64
	pastTip bool
}

func (clock pendingClock) unixNow() int64 {
	if clock.now > 0 {
		return clock.now
	}
	return time.Now().Unix()
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
func (bm *BlockMonitor) applyChainCommitment(c chainCommitment, clock pendingClock) (bool, chainCommitment) {
	c = c.normalize()
	if !pendingCommitmentOK(c) {
		return true, c
	}
	id := c.WishHash
	if !c.Reconciled && c.Attempts >= contractDiscoverReconcileLimit {
		return true, c
	}
	// Rows queued before the hash-pair filter have already been waiting.
	// Once the walk is caught up, drop the ones that still have no image
	// and no contract. A file or a row means this wish is real; keep it.
	if c.Legacy && clock.pastTip && !stegoFileOnDisk(c.StegoHash) && !bm.contractRowExists(id) {
		return true, c
	}
	if c.Legacy && (stegoFileOnDisk(c.StegoHash) || bm.contractRowExists(id)) {
		c.Legacy = false
	}
	// No wish image in uploads: this hash pair is not a Stargate contract yet.
	// It may be generic pay-to-anchor. Keep it only until the miss grace expires.
	if !wishImageOnDisk(id) && !bm.contractRowExists(id) {
		if c.MissingSince == 0 {
			c.MissingSince = clock.unixNow()
		}
		if missExpired(c, clock, false) {
			return true, c
		}
		return false, c
	}
	if !c.Reconciled && stegoFileOnDisk(c.StegoHash) {
		c.MissingSince = 0
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
			if c.MissingSince == 0 {
				c.MissingSince = clock.unixNow()
			}
		default:
			c.Attempts++
			bm.logReconcileAttempt(c, err)
		}
	}
	if !c.Reconciled && c.Attempts >= contractDiscoverReconcileLimit {
		return true, c
	}
	if !c.Reconciled {
		if !stegoFileOnDisk(c.StegoHash) {
			if c.MissingSince == 0 {
				c.MissingSince = clock.unixNow()
			}
			if missExpired(c, clock, bm.contractRowExists(id)) {
				return true, c
			}
		}
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

func missExpired(c chainCommitment, clock pendingClock, hasContract bool) bool {
	if hasContract || c.MissingSince <= 0 || !clock.pastTip || clock.caught <= 0 {
		return false
	}
	grace := clock.grace
	if grace < 0 {
		grace = 0
	}
	now := clock.unixNow()
	missed := time.Duration(now-c.MissingSince) * time.Second
	caught := time.Duration(now-clock.caught) * time.Second
	return missed >= grace && caught >= grace
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
	if bm.chain != nil {
		synced, syncErr := bm.chain.Synced(context.Background())
		if syncErr == nil && !synced {
			if !bm.discoverWaitLogged {
				log.Printf("contract discover: waiting until btcd finishes initial block download")
				bm.discoverWaitLogged = true
			}
			bm.retryPendingChainContracts("")
			return
		}
	}
	bm.discoverWaitLogged = false
	tip, err := bm.getChainTipHeight()
	if err == nil && tip >= 0 {
		bm.markDiscoverCaughtUp(tip)
	}
	bm.retryPendingChainContracts("")
	bm.storeReadyWishBlocks()
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
	skipped := 0
	reportSkipped := func() {
		if skipped == 0 {
			return
		}
		log.Printf("contract discover: %d blocks had OP_RETURN without a wish image in uploads; not recorded as smart contracts", skipped)
	}
	for h := start; h <= end; h++ {
		if ctx.Err() != nil {
			bm.setDiscoverProgress(next, failHeight, failCount)
			reportSkipped()
			return
		}
		hexData, err := bm.rawBlockHex(h)
		if err != nil {
			log.Printf("contract discover: block %d not available yet: %v", h, err)
			bm.setDiscoverProgress(next, failHeight, failCount)
			if next > start {
				bm.logDiscoverProgress(start, next-1, tip)
			}
			reportSkipped()
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
		ready := false
		for i := range hits {
			hits[i].Height = h
			bm.noteChainCommitment(hits[i])
			if wishImageOnDisk(hits[i].WishHash) {
				ready = true
			}
		}
		if !ready {
			// Remember the hash pair. Do not store the block as a smart contract.
			skipped++
			next = h + 1
			failHeight = 0
			failCount = 0
			continue
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
			reportSkipped()
			return
		}
		for i := range hits {
			if wishImageOnDisk(hits[i].WishHash) {
				bm.markWishStored(hits[i].WishHash, h)
			}
		}
		next = h + 1
		failHeight = 0
		failCount = 0
	}
	bm.setDiscoverProgress(next, failHeight, failCount)
	if next > tip {
		bm.markDiscoverCaughtUp(tip)
	}
	if next > start {
		bm.logDiscoverProgress(start, next-1, tip)
	}
	reportSkipped()
}

// markDiscoverCaughtUp records the first time the walk cursor passed the tip.
// Miss expiry starts from this clock, not from block age, so a node still
// walking history does not drop commitments the mirror has not fetched yet.
func (bm *BlockMonitor) markDiscoverCaughtUp(tip int64) {
	if bm == nil || tip < 0 {
		return
	}
	bm.replicationMu.Lock()
	defer bm.replicationMu.Unlock()
	bm.loadReplicationLocked()
	if bm.replication.NextHeight <= tip || bm.replication.CaughtUpSince > 0 {
		return
	}
	bm.replication.CaughtUpSince = time.Now().Unix()
	bm.saveReplicationLocked()
	log.Printf("contract discover: walk caught up at tip %d; new unmatched commitments expire after %s", tip, contractDiscoverMissGrace())
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
