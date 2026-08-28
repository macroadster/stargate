package bitcoin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"

	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
)

func (fn StegoReconcilerFunc) ReconcileStego(ctx context.Context, stegoCID, expectedHash string) error {
	return fn(ctx, stegoCID, expectedHash)
}

// countStegoImagesFromAPIResponse counts stego detections from API response
func (bm *BlockMonitor) countStegoImagesFromAPIResponse(scanResults []map[string]any) int {
	count := 0
	for _, result := range scanResults {
		if isStego, ok := result["is_stego"].(bool); ok && isStego {
			count++
		}
	}
	return count
}

// countStegoImages counts how many images have steganography detected
func (bm *BlockMonitor) countStegoImages(scanResults []map[string]any) int {
	return bm.countStegoImagesFromAPIResponse(scanResults)
}

func (bm *BlockMonitor) reconcileIngestionContracts(blockDir string, parsedBlock *ParsedBlock, scanResults []map[string]any, smartContracts []SmartContractData, blockHeight int64) []SmartContractData {
	if bm.ingestion == nil || len(scanResults) == 0 {
		return smartContracts
	}

	txByID := make(map[string]Transaction, len(parsedBlock.Transactions))
	for _, tx := range parsedBlock.Transactions {
		if tx.TxID != "" {
			txByID[tx.TxID] = tx
		}
	}

	for i := range smartContracts {
		smartContracts[i].BlockHeight = blockHeight
	}

	for _, result := range scanResults {
		isStego, _ := result["is_stego"].(bool)
		if !isStego {
			continue
		}

		txID := stringFromAny(result["tx_id"])
		if txID == "" {
			continue
		}

		tx, ok := txByID[txID]
		if !ok {
			continue
		}

		image := bm.findImageForScanResult(parsedBlock.Images, result)
		if image == nil || len(image.Data) == 0 {
			continue
		}

		payload := parseScanPayload(result)
		if payload.message == "" {
			continue
		}

		cleanedImage := sanitizeExtractedImage(*image)
		visibleHash := visiblePixelHash(cleanedImage.Data, payload.message)
		if visibleHash == "" {
			continue
		}

		rec, err := bm.ingestion.Get(visibleHash)
		if err != nil {
			continue
		}

		matchedScript, ok := bm.matchPayoutScript(tx, payload)
		if !ok {
			continue
		}

		destPath, err := bm.moveIngestionImage(blockDir, rec)
		if err != nil {
			log.Printf("Failed to move ingestion image for %s: %v", visibleHash, err)
			bm.maybeReconcileStego(rec)
			continue
		}
		bm.maybeReconcileStego(rec)

		imageFile := filepath.Base(destPath)
		imagePath := filepath.Join("images", imageFile)
		contractMeta := buildContractMetadata(result)
		contractMeta["visible_pixel_hash"] = visibleHash
		if payload.payoutAddress != "" {
			contractMeta["payout_address"] = payload.payoutAddress
		}
		if payload.payoutScript != "" {
			contractMeta["payout_script"] = payload.payoutScript
		}
		if payload.payoutScriptHash != "" {
			contractMeta["payout_script_hash"] = payload.payoutScriptHash
		}
		if len(matchedScript) > 0 && payload.payoutScriptHash == "" {
			contractMeta["payout_script_hash_sha256"] = scriptHashHex(matchedScript)
			contractMeta["payout_script_hash160"] = scriptHash160Hex(matchedScript)
		}
		contractMeta["ingestion_id"] = rec.ID
		contractMeta["image_file"] = imageFile
		contractMeta["image_path"] = imagePath

		if updated := updateContractEntry(smartContracts, result, SmartContractData{
			ContractID:  visibleHash,
			BlockHeight: blockHeight,
			ImagePath:   imagePath,
			Confidence:  confidenceFromAny(result["confidence"]),
			Metadata:    contractMeta,
		}); !updated {
			smartContracts = append(smartContracts, SmartContractData{
				ContractID:  visibleHash,
				BlockHeight: blockHeight,
				ImagePath:   imagePath,
				Confidence:  confidenceFromAny(result["confidence"]),
				Metadata:    contractMeta,
			})
		}
	}

	return smartContracts
}

func (bm *BlockMonitor) reconcileOracleIngestions(blockDir string, parsedBlock *ParsedBlock, smartContracts []SmartContractData, blockHeight int64) []SmartContractData {
	if len(parsedBlock.Transactions) == 0 {
		return smartContracts
	}

	var recs []services.IngestionRecord
	if bm.ingestion != nil {
		var err error
		recs, err = bm.ingestion.ListRecent("", 500)
		if err != nil {
			log.Printf("oracle reconcile: failed to list ingestions: %v", err)
		}
	}

	primaryCandidates := make(map[string]*services.IngestionRecord, len(recs))
	fallbackCandidates := make(map[string]*services.IngestionRecord, len(recs))
	candidatesByID := make(map[string][]string, len(recs))
	txidMatches := make(map[string]*services.IngestionRecord, len(recs))
	matchedTxIDs := make(map[string]string)
	for _, rec := range recs {
		recCopy := rec
		primaryList, fallbackList := ingestionCandidateBuckets(recCopy, bm.networkParams())
		for _, candidate := range primaryList {
			primaryCandidates[candidate] = &recCopy
			candidatesByID[recCopy.ID] = append(candidatesByID[recCopy.ID], candidate)
		}
		for _, candidate := range fallbackList {
			fallbackCandidates[candidate] = &recCopy
			candidatesByID[recCopy.ID] = append(candidatesByID[recCopy.ID], candidate)
		}
		for _, txid := range fundingTxIDsFromMeta(recCopy.Metadata) {
			txidMatches[txid] = &recCopy
		}

	}
	// Also add candidates from proposals (MCP store).  Proposals are replicated
	// more reliably than ingestion records (via MCP event pubsub), so a peer node
	// may have a proposal with visible_pixel_hash but no matching ingestion record.
	proposalCandidates := bm.proposalCandidates(primaryCandidates)
	for hash, rec := range proposalCandidates {
		if _, exists := primaryCandidates[hash]; !exists {
			primaryCandidates[hash] = rec
			candidatesByID[rec.ID] = append(candidatesByID[rec.ID], hash)
		}
	}
	contractCands := bm.contractCandidates(primaryCandidates)
	for hash, rec := range contractCands {
		if _, exists := primaryCandidates[hash]; !exists {
			primaryCandidates[hash] = rec
			candidatesByID[rec.ID] = append(candidatesByID[rec.ID], hash)
		}
	}

	log.Printf("oracle reconcile: block %d: ingestion=%v sweepStore=%v, %d ingestions, %d proposal candidates, %d contract candidates, %d primary, %d fallback, %d txid",
		blockHeight, bm.ingestion != nil, bm.sweepStore != nil, len(recs), len(proposalCandidates), len(contractCands), len(primaryCandidates), len(fallbackCandidates), len(txidMatches))

	if len(primaryCandidates) == 0 && len(fallbackCandidates) == 0 && len(txidMatches) == 0 {
		log.Printf("oracle reconcile: block %d: no candidates found, will still scan for OP_RETURN contracts", blockHeight)
	}

	log.Printf("oracle reconcile: %d primary hashes, %d fallback hashes, %d funding txids across %d ingestions (+%d from proposals, +%d from contracts)", len(primaryCandidates), len(fallbackCandidates), len(txidMatches), len(recs), len(proposalCandidates), len(contractCands))

	for _, tx := range parsedBlock.Transactions {
		if match, ok := txidMatches[tx.TxID]; ok && match != nil {
			var imageFile, imagePath string
			destPath, err := bm.moveIngestionImageWithFilename(blockDir, match, blockImageFilename(match, tx.TxID))
			if err != nil {
				log.Printf("oracle reconcile: image unavailable for %s (will create contract without image): %v", match.ID, err)
			} else {
				imageFile = filepath.Base(destPath)
				imagePath = filepath.Join("images", imageFile)
			}
			bm.maybeReconcileStego(match)
			log.Printf("oracle reconcile: matched ingestion %s via funding_txid=%s", match.ID, tx.TxID)
			contractMeta := map[string]any{
				"tx_id":              tx.TxID,
				"output_index":       0,
				"block_height":       blockHeight,
				"match_type":         "funding_txid",
				"match_hash":         tx.TxID,
				"image_file":         imageFile,
				"image_path":         imagePath,
				"ingestion_id":       match.ID,
				"visible_pixel_hash": stringFromAny(match.Metadata["visible_pixel_hash"]),
			}
			mergeIngestionMetadata(contractMeta, match.Metadata)
			applyStegoMetadata(contractMeta, match.Metadata)
			smartContracts = upsertContractByID(smartContracts, SmartContractData{
				ContractID:  match.ID,
				BlockHeight: blockHeight,
				ImagePath:   imagePath,
				Confidence:  0,
				Metadata:    contractMeta,
			})
			bm.ensureMatchedContract(match.ID, match, tx.TxID, blockHeight, imagePath)
			bm.markIngestionConfirmed(match, tx.TxID, blockHeight, imageFile, imagePath)
			bm.updateTaskFundingProofsFromTx(match.ID, tx, blockHeight)
			bm.confirmContractTasks(match.ID, tx.TxID, blockHeight)
			// Scan OP_RETURN outputs for stego hash so we can reconcile
			// the stego image (extract proposal/tasks) and sandbox tarball.
			// The funding_txid path confirms the contract but doesn't
			// trigger stego reconcile or sandbox extraction on its own.
			for _, output := range tx.Outputs {
				_, stegoHash, opOk := parseOPReturnHashes(output.ScriptPubKey)
				if opOk && stegoHash != "" {
					bm.reconcileOnChainArtifacts(match.ID, stegoHash)
					break
				}
			}
			for _, candidate := range candidatesByID[match.ID] {
				delete(primaryCandidates, candidate)
				delete(fallbackCandidates, candidate)
			}
			delete(txidMatches, tx.TxID)
			matchedTxIDs[tx.TxID] = match.ID
		}

		// OP_RETURN matching: scan outputs for wish_hash || stego_hash proof.
		// This is the primary matching path for new-style transactions that use
		// direct donation + OP_RETURN instead of P2WSH hashlocks.
		for _, output := range tx.Outputs {
			wishHash, stegoHash, ok := parseOPReturnHashes(output.ScriptPubKey)
			if !ok {
				continue
			}
			log.Printf("oracle reconcile: block %d tx %s has OP_RETURN wish=%s stego=%s", blockHeight, tx.TxID, wishHash, stegoHash)
			// Try matching wish_hash against primary candidates.
			match := primaryCandidates[wishHash]
			if match == nil && stegoHash != "" {
				match = primaryCandidates[stegoHash]
			}
			if match == nil {
				match = fallbackCandidates[wishHash]
			}
			if _, ok := matchedTxIDs[tx.TxID]; ok {
				continue // already matched by funding_txid
			}
			if match != nil {
				// Matched against a known candidate (ingestion/proposal/contract).
				var imageFile, imagePath string
				destPath, err := bm.moveIngestionImageWithFilename(blockDir, match, blockImageFilename(match, tx.TxID))
				if err != nil {
					log.Printf("oracle reconcile: image unavailable for %s (will create contract without image): %v", match.ID, err)
				} else {
					imageFile = filepath.Base(destPath)
					imagePath = filepath.Join("images", imageFile)
				}
				bm.maybeReconcileStego(match)
				log.Printf("oracle reconcile: matched ingestion %s via OP_RETURN wish=%s stego=%s in tx %s", match.ID, wishHash, stegoHash, tx.TxID)
				contractMeta := map[string]any{
					"tx_id":              tx.TxID,
					"block_height":       blockHeight,
					"match_type":         "op_return",
					"wish_hash":          wishHash,
					"stego_hash":         stegoHash,
					"image_file":         imageFile,
					"image_path":         imagePath,
					"ingestion_id":       match.ID,
					"visible_pixel_hash": stringFromAny(match.Metadata["visible_pixel_hash"]),
				}
				mergeIngestionMetadata(contractMeta, match.Metadata)
				applyStegoMetadata(contractMeta, match.Metadata)
				smartContracts = upsertContractByID(smartContracts, SmartContractData{
					ContractID:  match.ID,
					BlockHeight: blockHeight,
					ImagePath:   imagePath,
					Confidence:  0,
					Metadata:    contractMeta,
				})
				bm.ensureMatchedContract(match.ID, match, tx.TxID, blockHeight, imagePath)
				bm.markIngestionConfirmed(match, tx.TxID, blockHeight, imageFile, imagePath)
				bm.updateTaskFundingProofsFromTx(match.ID, tx, blockHeight)
				bm.confirmContractTasks(match.ID, tx.TxID, blockHeight)
				// Trigger stego reconciliation and sandbox extraction using
				// the on-chain stego hash.  sandbox_hash is inside the stego
				// v2 JSON payload — reconcileOnChainArtifacts reads it from
				// there after extracting the stego image.
				bm.reconcileOnChainArtifacts(match.ID, stegoHash)
				for _, candidate := range candidatesByID[match.ID] {
					delete(primaryCandidates, candidate)
					delete(fallbackCandidates, candidate)
				}
				matchedTxIDs[tx.TxID] = match.ID
			} else {
				// No candidate match in ingestion/proposal/contract databases.
				// If the stego image exists on disk, reconcile from it — the
				// embedded v2 payload contains the full proposal, tasks, and
				// sandbox_hash.  reconcileOnChainArtifacts will create the
				// contract via the stego reconciler.
				if stegoHash != "" {
					log.Printf("oracle reconcile: block %d tx %s: OP_RETURN wish=%s stego=%s has no candidate, attempting stego reconcile from disk", blockHeight, tx.TxID, wishHash, stegoHash)
					bm.reconcileOnChainArtifacts(wishHash, stegoHash)
					// After reconciliation, confirm the newly-created contract
					// and trigger sandbox extraction.
					normalizedWish := wishHash
					if len(normalizedWish) == 64 {
						if _, decErr := hex.DecodeString(normalizedWish); decErr == nil {
							normalizedWish = "wish-" + normalizedWish
						}
					}
					if bm.sweepStore != nil {
						_ = bm.sweepStore.ConfirmContract(context.Background(), normalizedWish, int(blockHeight), tx.TxID)
						bm.updateTaskFundingProofsFromTx(normalizedWish, tx, blockHeight)
						bm.confirmContractTasks(normalizedWish, tx.TxID, blockHeight)
						// Now that the contract is confirmed, reconcile again
						// so downloadSandboxArtifacts fires (it checks for
						// confirmed status before extracting).
						bm.reconcileOnChainArtifacts(normalizedWish, stegoHash)
					}
				} else {
					log.Printf("oracle reconcile: block %d tx %s: OP_RETURN wish=%s (no stego hash), skipping", blockHeight, tx.TxID, wishHash)
				}
			}
			break // one OP_RETURN match per tx
		}

		if match, matchType, matchedHash := matchWitnessHash(tx, primaryCandidates, fallbackCandidates); match != nil {
			if !isIdentityHash(match, matchedHash, bm.networkParams()) {
				log.Printf("oracle reconcile: rejecting witness_hash match for %s: hash %s is not an identity hash of the ingestion record", match.ID, matchedHash)
			} else if existingID, ok := matchedTxIDs[tx.TxID]; ok && existingID != match.ID {
				log.Printf("oracle reconcile: skipping %s match for %s (tx %s already matched by funding_txid)", matchType, match.ID, tx.TxID)
			} else {
				var imageFile, imagePath string
				destPath, err := bm.moveIngestionImageWithFilename(blockDir, match, blockImageFilename(match, tx.TxID))
				if err != nil {
					log.Printf("oracle reconcile: image unavailable for %s (will create contract without image): %v", match.ID, err)
				} else {
					imageFile = filepath.Base(destPath)
					imagePath = filepath.Join("images", imageFile)
				}
				bm.maybeReconcileStego(match)
				log.Printf("oracle reconcile: matched ingestion %s via %s=%s in tx %s witness", match.ID, matchType, matchedHash, tx.TxID)
				contractMeta := map[string]any{
					"tx_id":              tx.TxID,
					"block_height":       blockHeight,
					"match_type":         matchType,
					"match_hash":         matchedHash,
					"image_file":         imageFile,
					"image_path":         imagePath,
					"ingestion_id":       match.ID,
					"visible_pixel_hash": stringFromAny(match.Metadata["visible_pixel_hash"]),
				}
				mergeIngestionMetadata(contractMeta, match.Metadata)
				applyStegoMetadata(contractMeta, match.Metadata)
				smartContracts = upsertContractByID(smartContracts, SmartContractData{
					ContractID:  match.ID,
					BlockHeight: blockHeight,
					ImagePath:   imagePath,
					Confidence:  0,
					Metadata:    contractMeta,
				})
				bm.ensureMatchedContract(match.ID, match, tx.TxID, blockHeight, imagePath)
				bm.markIngestionConfirmed(match, tx.TxID, blockHeight, imageFile, imagePath)
				bm.updateTaskFundingProofsFromTx(match.ID, tx, blockHeight)
				bm.confirmContractTasks(match.ID, tx.TxID, blockHeight)
				for _, candidate := range candidatesByID[match.ID] {
					delete(primaryCandidates, candidate)
					delete(fallbackCandidates, candidate)
				}
			}
		}

		for outIdx, output := range tx.Outputs {
			match, matchType, matchedHash := matchOracleOutput(output.ScriptPubKey, bm.networkParams(), primaryCandidates)
			if match == nil {
				match, matchType, matchedHash = matchOracleOutput(output.ScriptPubKey, bm.networkParams(), fallbackCandidates)
			}
			if match == nil {
				continue
			}
			if existingID, ok := matchedTxIDs[tx.TxID]; ok && existingID != match.ID {
				log.Printf("oracle reconcile: skipping %s match for %s (tx %s already matched by funding_txid)", matchType, match.ID, tx.TxID)
				continue
			}

			var imageFile, imagePath string
			destPath, err := bm.moveIngestionImageWithFilename(blockDir, match, blockImageFilename(match, tx.TxID))
			if err != nil {
				log.Printf("oracle reconcile: image unavailable for %s (will create contract without image): %v", match.ID, err)
			} else {
				imageFile = filepath.Base(destPath)
				imagePath = filepath.Join("images", imageFile)
			}
			bm.maybeReconcileStego(match)
			log.Printf("oracle reconcile: matched ingestion %s via %s=%s in tx %s output %d", match.ID, matchType, matchedHash, tx.TxID, outIdx)

			contractMeta := map[string]any{
				"tx_id":              tx.TxID,
				"output_index":       outIdx,
				"block_height":       blockHeight,
				"match_type":         matchType,
				"match_hash":         matchedHash,
				"payout_script":      hex.EncodeToString(output.ScriptPubKey),
				"image_file":         imageFile,
				"image_path":         imagePath,
				"ingestion_id":       match.ID,
				"visible_pixel_hash": stringFromAny(match.Metadata["visible_pixel_hash"]),
			}
			mergeIngestionMetadata(contractMeta, match.Metadata)
			applyStegoMetadata(contractMeta, match.Metadata)

			smartContracts = upsertContractByID(smartContracts, SmartContractData{
				ContractID:  match.ID,
				BlockHeight: blockHeight,
				ImagePath:   imagePath,
				Confidence:  0,
				Metadata:    contractMeta,
			})
			bm.ensureMatchedContract(match.ID, match, tx.TxID, blockHeight, imagePath)
			bm.markIngestionConfirmed(match, tx.TxID, blockHeight, imageFile, imagePath)
			bm.updateTaskFundingProofsFromTx(match.ID, tx, blockHeight)
			bm.confirmContractTasks(match.ID, tx.TxID, blockHeight)

			for _, candidate := range candidatesByID[match.ID] {
				delete(primaryCandidates, candidate)
				delete(fallbackCandidates, candidate)
			}
		}
	}

	return smartContracts
}

// confirmContractTasks marks task proofs as confirmed for the given contract.
// This is the new-style path for OP_RETURN transactions where donation is paid
// directly — no sweeping is needed.
func (bm *BlockMonitor) confirmContractTasks(contractID, txid string, blockHeight int64) {
	if bm.sweepStore == nil || strings.TrimSpace(contractID) == "" || strings.TrimSpace(txid) == "" {
		return
	}
	twentyFourHoursAgo := time.Now().Add(-24 * time.Hour)
	tasks, err := bm.sweepStore.ListTasks(smart_contract.TaskFilter{
		ContractID:        contractID,
		LastActivitySince: &twentyFourHoursAgo,
	})
	if err != nil {
		log.Printf("oracle reconcile: failed to list tasks for %s: %v", contractID, err)
		return
	}
	for _, task := range tasks {
		proof := task.MerkleProof
		if proof == nil {
			proof = &smart_contract.MerkleProof{}
		}
		if proof.TxID == "" {
			proof.TxID = txid
		}
		if proof.ConfirmationStatus != "confirmed" {
			now := time.Now()
			proof.ConfirmationStatus = "confirmed"
			proof.ConfirmedAt = &now
			proof.BlockHeight = blockHeight
			// Mark sweep as not needed — donation was paid directly in the PSBT.
			proof.SweepStatus = "direct"
			if err := bm.sweepStore.UpdateTaskProof(context.Background(), task.TaskID, proof); err != nil {
				log.Printf("oracle reconcile: failed to confirm proof for %s: %v", task.TaskID, err)
			} else {
				log.Printf("oracle reconcile: confirmed task %s via OP_RETURN (direct donation, no sweep needed)", task.TaskID)
			}
		}
	}
}

func (bm *BlockMonitor) updateTaskFundingProofsFromTx(contractID string, tx Transaction, blockHeight int64) {
	if bm.sweepStore == nil || strings.TrimSpace(contractID) == "" {
		return
	}
	// Also filter by recent activity for efficiency, even though we're already filtering by contract
	twentyFourHoursAgo := time.Now().Add(-24 * time.Hour)
	tasks, err := bm.sweepStore.ListTasks(smart_contract.TaskFilter{
		ContractID:        contractID,
		LastActivitySince: &twentyFourHoursAgo,
	})
	if err != nil {
		log.Printf("oracle reconcile: failed to list tasks for funding update %s: %v", contractID, err)
		return
	}
	taskByWallet := make(map[string][]smart_contract.Task)
	for _, task := range tasks {
		wallet := strings.TrimSpace(task.ContractorWallet)
		if wallet == "" && task.MerkleProof != nil {
			wallet = strings.TrimSpace(task.MerkleProof.ContractorWallet)
		}
		if wallet == "" {
			continue
		}
		taskByWallet[wallet] = append(taskByWallet[wallet], task)
	}
	if len(taskByWallet) == 0 {
		return
	}
	now := time.Now()
	for _, output := range tx.Outputs {
		for _, addr := range outputAddresses(output.ScriptPubKey, bm.networkParams()) {
			candidates := taskByWallet[addr]
			if len(candidates) == 0 {
				continue
			}
			bestIdx := -1
			for i, task := range candidates {
				proof := task.MerkleProof
				if proof != nil && strings.TrimSpace(proof.TxID) != "" && strings.TrimSpace(proof.TxID) != strings.TrimSpace(tx.TxID) {
					continue
				}
				if task.BudgetSats > 0 && task.BudgetSats == output.Value {
					bestIdx = i
					break
				}
				if bestIdx == -1 {
					bestIdx = i
				}
			}
			if bestIdx < 0 {
				continue
			}
			task := candidates[bestIdx]
			taskByWallet[addr] = append(candidates[:bestIdx], candidates[bestIdx+1:]...)

			proof := task.MerkleProof
			if proof == nil {
				proof = &smart_contract.MerkleProof{}
			}
			proof.TxID = tx.TxID
			proof.BlockHeight = blockHeight
			proof.FundingAddress = addr
			proof.FundedAmountSats = output.Value
			if proof.ConfirmationStatus == "" || proof.ConfirmationStatus == "provisional" {
				proof.ConfirmationStatus = "confirmed"
			}
			if proof.ConfirmedAt == nil {
				proof.ConfirmedAt = &now
			}
			if proof.SeenAt.IsZero() {
				proof.SeenAt = now
			}
			if proof.ContractorWallet == "" {
				proof.ContractorWallet = addr
			}
			if err := bm.sweepStore.UpdateTaskProof(context.Background(), task.TaskID, proof); err != nil {
				log.Printf("oracle reconcile: failed to update funding proof for %s: %v", task.TaskID, err)
			}
		}
	}
}

func ingestionCandidateBuckets(rec services.IngestionRecord, params *chaincfg.Params) ([]string, []string) {
	var primary []string
	var fallback []string

	appendPrimary := func(value string) {
		value = normalizeHex(value)
		if len(value) != 40 && len(value) != 64 {
			return
		}
		primary = append(primary, value)
	}
	appendFallback := func(value string) {
		value = normalizeHex(value)
		if len(value) != 40 && len(value) != 64 {
			return
		}
		fallback = append(fallback, value)
	}

	if rec.ID != "" {
		appendFallback(rec.ID)
	}
	if prefix := hashPrefixFromFilename(rec.Filename); prefix != "" {
		appendPrimary(prefix)
	}
	if v := stringFromAny(rec.Metadata["visible_pixel_hash"]); v != "" {
		appendPrimary(v)
	}
	if v := commitmentScriptHashFromMeta(rec, params); v != "" {
		appendPrimary(v)
	}
	if v := stringFromAny(rec.Metadata["pixel_hash"]); v != "" {
		appendPrimary(v)
	}
	if v := stringFromAny(rec.Metadata["product_hash"]); v != "" {
		appendPrimary(v)
	}

	return primary, fallback
}

// ensureMatchedContract ensures a contract record exists in the MCP store for
// a matched ingestion.  reconcileOracleIngestions writes matches to the block
// inscriptions.json, but the MCP store may not have the contract yet — for
// example on a peer node that received the image via IPFS but hasn't run the
// full stego reconcile.  Without this, ConfirmContract (called from
// markIngestionConfirmed) silently fails when the row doesn't exist, leaving
// the frontend without proposal data or sandbox links.
func (bm *BlockMonitor) ensureMatchedContract(contractID string, match *services.IngestionRecord, txID string, blockHeight int64, imagePath string) {
	upserter, ok := bm.sweepStore.(contractUpserter)
	if !ok || upserter == nil {
		return
	}
	ctx := context.Background()

	// Build contract ID with wish- prefix for consistency.
	visibleHash := stringFromAny(match.Metadata["visible_pixel_hash"])
	if visibleHash == "" {
		visibleHash = contractID
	}
	normalizedID := contractID
	if len(normalizedID) == 64 {
		if _, err := hex.DecodeString(normalizedID); err == nil {
			normalizedID = "wish-" + normalizedID
		}
	}

	// Try to confirm the contract first — if the row already exists,
	// ConfirmContract will update it and we're done.
	_ = bm.sweepStore.ConfirmContract(ctx, normalizedID, int(blockHeight), txID)

	// Check if the row actually exists now — ConfirmContract doesn't return
	// "not found" explicitly, it just updates 0 rows.
	type contractGetter interface {
		GetContract(id string) (smart_contract.Contract, error)
	}
	if cg, ok := bm.sweepStore.(contractGetter); ok {
		if _, err := cg.GetContract(normalizedID); err == nil {
			return // already exists
		}
	}

	// Build title from ingestion metadata.
	title := stringFromAny(match.Metadata["embedded_message"])
	if title == "" {
		title = stringFromAny(match.Metadata["message"])
	}
	if title == "" && len(visibleHash) >= 8 {
		title = "Wish " + visibleHash[:8] + "..."
	}
	// Truncate title to first line for display.
	if idx := strings.Index(title, "\n"); idx > 0 {
		title = strings.TrimSpace(title[:idx])
	}
	if len(title) > 120 {
		title = title[:117] + "..."
	}

	bh := int(blockHeight)
	now := time.Now()
	meta := map[string]interface{}{
		"visible_pixel_hash": visibleHash,
		"confirmed_txid":     txID,
		"confirmed_height":   blockHeight,
	}
	// Merge key fields from ingestion metadata.
	for _, key := range []string{
		"ipfs_image_cid", "stego_image_cid", "embedded_message",
		"commitment_address", "commitment_sats", "commitment_vout",
		"funding_txid", "funding_txids",
		"stego_contract_id", "stego_type", "stego_probability",
	} {
		if v := match.Metadata[key]; v != nil {
			meta[key] = v
		}
	}

	stegoImageURL := ""
	if imagePath != "" {
		stegoImageURL = fmt.Sprintf("/api/block-image/%d/%s", blockHeight, filepath.Base(imagePath))
	}
	if cid := stringFromAny(match.Metadata["ipfs_image_cid"]); cid != "" && stegoImageURL == "" {
		stegoImageURL = fmt.Sprintf("/api/block-image/%d/%s", blockHeight, cid)
	}

	c := smart_contract.Contract{
		ContractID:           normalizedID,
		Title:                title,
		Status:               "confirmed",
		StegoImageURL:        stegoImageURL,
		ConfirmedBlockHeight: &bh,
		ConfirmedAt:          &now,
		Metadata:             meta,
		CreatedAt:            now,
	}
	if err := upserter.UpsertContractWithTasks(ctx, c, nil); err != nil {
		log.Printf("oracle reconcile: ensureMatchedContract %s: %v", normalizedID, err)
	} else {
		log.Printf("oracle reconcile: ensured matched contract %s in MCP store", normalizedID)
	}
}

// proposalCandidates builds synthetic IngestionRecord entries from proposals
// whose visible_pixel_hash isn't already present in the existing candidate map.
// This ensures the OP_RETURN matching works even when the peer received the
// proposal (via MCP sync) but not the ingestion record (via IPFS ingest sync).
func (bm *BlockMonitor) proposalCandidates(existing map[string]*services.IngestionRecord) map[string]*services.IngestionRecord {
	pl, ok := bm.sweepStore.(proposalLister)
	if !ok || pl == nil {
		log.Printf("proposalCandidates: sweepStore does not satisfy proposalLister (sweepStore=%T, ok=%v)", bm.sweepStore, ok)
		return nil
	}
	proposals, err := pl.ListProposals(context.Background(), smart_contract.ProposalFilter{
		MaxResults: 500,
	})
	if err != nil {
		log.Printf("proposalCandidates: ListProposals error: %v", err)
		return nil
	}
	log.Printf("proposalCandidates: %d proposals returned", len(proposals))
	out := make(map[string]*services.IngestionRecord)
	for _, p := range proposals {
		hash := normalizeHex(strings.TrimSpace(p.VisiblePixelHash))
		if hash == "" || len(hash) != 64 {
			continue
		}
		if _, covered := existing[hash]; covered {
			continue
		}
		// Use the visible_pixel_hash as the record ID (same convention as
		// ipfsIngestProcessManifest).
		id := hash
		if strings.TrimSpace(p.ID) != "" && !strings.HasPrefix(p.ID, "proposal-") && !strings.HasPrefix(p.ID, "wish-") {
			id = p.ID
		}
		rec := &services.IngestionRecord{
			ID:       id,
			Filename: "",
			Metadata: map[string]interface{}{
				"visible_pixel_hash": hash,
				"source":             "proposal",
			},
		}
		out[hash] = rec
	}
	return out
}

// contractCandidates builds synthetic IngestionRecord entries from contracts
// whose contract_id starts with "wish-" and thus encodes a visible_pixel_hash.
func (bm *BlockMonitor) contractCandidates(existing map[string]*services.IngestionRecord) map[string]*services.IngestionRecord {
	cl, ok := bm.sweepStore.(contractLister)
	if !ok || cl == nil {
		return nil
	}
	contracts, err := cl.ListContracts(smart_contract.ContractFilter{Limit: 500})
	if err != nil {
		log.Printf("contractCandidates: ListContracts error: %v", err)
		return nil
	}
	out := make(map[string]*services.IngestionRecord)
	for _, c := range contracts {
		hash := ""
		if strings.HasPrefix(c.ContractID, "wish-") {
			hash = normalizeHex(strings.TrimPrefix(c.ContractID, "wish-"))
		}
		if hash == "" {
			if v, ok := c.Metadata["visible_pixel_hash"].(string); ok {
				hash = normalizeHex(v)
			}
		}
		if hash == "" || len(hash) != 64 {
			continue
		}
		if _, covered := existing[hash]; covered {
			continue
		}
		out[hash] = &services.IngestionRecord{
			ID:       hash,
			Filename: "",
			Metadata: map[string]interface{}{
				"visible_pixel_hash": hash,
				"source":             "contract",
			},
		}
	}
	log.Printf("contractCandidates: %d contracts returned, %d new candidates", len(contracts), len(out))
	return out
}

func matchOracleOutput(script []byte, params *chaincfg.Params, candidates map[string]*services.IngestionRecord) (*services.IngestionRecord, string, string) {
	if len(script) == 0 {
		return nil, "", ""
	}

	// Try script hash matching first
	for _, hash := range []string{scriptHashHex(script), scriptHash160Hex(script)} {
		if match, ok := candidates[hash]; ok {
			return match, "script_hash", hash
		}
	}

	if len(candidates) > 0 {
		// Try script address hashes (P2SH, WitnessV0ScriptHash)
		for _, addrHash := range scriptAddressHashes(script, params) {
			if match, ok := candidates[addrHash]; ok {
				return match, "script_address", addrHash
			}
		}

		// Fallback: try direct address hashes for simple outputs (P2WPKH, P2PKH)
		class, addrs, _, err := txscript.ExtractPkScriptAddrs(script, params)
		if err == nil {
			for _, addr := range addrs {
				// For simple addresses (P2WPKH, P2PKH), try multiple hash formats
				if class == txscript.PubKeyHashTy || class == txscript.WitnessV0PubKeyHashTy {
					addrStr := addr.String()
					scriptAddrHash := hex.EncodeToString(addr.ScriptAddress())
					addrHash1 := scriptHashHex([]byte(addrStr))
					addrHash2 := scriptHashHex([]byte(scriptAddrHash))
					addrHash3 := scriptHash160Hex([]byte(addrStr))

					// Try hash of address string
					if match, ok := candidates[addrHash1]; ok {
						return match, "address_hash", addrHash1
					}
					// Try hash of script address
					if match, ok := candidates[addrHash2]; ok {
						return match, "script_address_hash", addrHash2
					}
					// Try 160 hash of address string
					if match, ok := candidates[addrHash3]; ok {
						return match, "address_160_hash", addrHash3
					}
				}
			}
		}
	}

	return nil, "", ""
}

func matchWitnessHash(tx Transaction, primaryCandidates, fallbackCandidates map[string]*services.IngestionRecord) (*services.IngestionRecord, string, string) {
	if len(tx.InputWitnesses) == 0 {
		return nil, "", ""
	}
	if match, matchType, matched := matchWitnessCandidates(tx.InputWitnesses, primaryCandidates); match != nil {
		return match, matchType, matched
	}
	return matchWitnessCandidates(tx.InputWitnesses, fallbackCandidates)
}

func matchWitnessCandidates(inputWitnesses [][][]byte, candidates map[string]*services.IngestionRecord) (*services.IngestionRecord, string, string) {
	if len(candidates) == 0 {
		return nil, "", ""
	}
	for _, stack := range inputWitnesses {
		for _, item := range stack {
			for _, candidate := range witnessHashes(item) {
				if match, ok := candidates[candidate]; ok {
					return match, "witness_hash", candidate
				}
			}
		}
	}
	return nil, "", ""
}

func witnessHashes(item []byte) []string {
	if len(item) == 0 {
		return nil
	}
	// Only emit the two hash variants that Stargate actually uses for matching:
	//  1. Raw hex of 32-byte items (the hashlock preimage = visible_pixel_hash).
	//  2. SHA256 of the item (matches commitment_script_hash for redeem scripts).
	//
	// Hash160 and the text-decode path were removed because they created a
	// broad false-positive surface where unrelated witness items could collide
	// with ingestion candidate hashes.
	seen := make(map[string]struct{}, 2)
	add := func(value string) {
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
	}

	// 32-byte items are SHA256 preimages (visible_pixel_hash) — emit raw hex.
	// 20-byte items are Hash160 values — emit raw hex.
	if len(item) == 32 || len(item) == 20 {
		add(hex.EncodeToString(item))
	}

	// Items > 32 bytes may be redeem scripts; SHA256 matches commitment_script_hash.
	if len(item) > 32 {
		sum := sha256.Sum256(item)
		add(hex.EncodeToString(sum[:]))
	}

	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	return out
}
