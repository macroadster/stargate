package smart_contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
	scservices "stargate-backend/app/smart_contract/services"
	"stargate-backend/services"
	"stargate-backend/storage/ipfs"
	scstore "stargate-backend/storage/smart_contract"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"golang.org/x/crypto/ripemd160"
)

// handleContractPSBT builds a PSBT to fund the contract payout using the caller's wallet UTXOs.
func (s *Server) handleContractPSBT(w http.ResponseWriter, r *http.Request, contractID string) {
	if r.Header.Get("Content-Type") != "" && !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	if s.apiKeys == nil || s.mempool == nil {
		Error(w, http.StatusServiceUnavailable, "psbt builder unavailable")
		return
	}
	payerKey := r.Header.Get("X-API-Key")
	payerRec, ok := s.apiKeys.Get(payerKey)
	if !ok {
		Error(w, http.StatusForbidden, "invalid api key")
		return
	}
	if strings.TrimSpace(payerRec.Wallet) == "" {
		Error(w, http.StatusForbidden, "api key missing wallet binding - please associate a Bitcoin wallet address with your API key")
		return
	}

	var body struct {
		ContractorAPIKey string   `json:"contractor_api_key"`
		ContractorWallet string   `json:"contractor_wallet"`
		PayerAddresses   []string `json:"payer_addresses"`
		ChangeAddress    string   `json:"change_address"`
		BudgetSats       int64    `json:"budget_sats"`
		PixelHash        string   `json:"pixel_hash"`
		ProductPixelHash string   `json:"product_pixel_hash"`
		CommitmentSats   int64    `json:"commitment_sats"`
		FeeRate          int64    `json:"fee_rate_sats_vb"`
		UsePixelHash     *bool    `json:"use_pixel_hash"`
		CommitmentTarget string   `json:"commitment_target"`
		TaskID           string   `json:"task_id"`
		SplitPSBT        bool     `json:"split_psbt"`
		Payouts          []struct {
			Address    string `json:"address"`
			AmountSats int64  `json:"amount_sats"`
		} `json:"payouts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}

	contract, err := s.store.GetContract(contractID)
	if err != nil {
		Error(w, http.StatusNotFound, err.Error())
		return
	}

	params := &chaincfg.TestNet4Params

	payerAddr, err := btcutil.DecodeAddress(payerRec.Wallet, params)
	if err != nil {
		Error(w, http.StatusBadRequest, fmt.Sprintf("invalid payer wallet: %v", err))
		return
	}

	var payerAddresses []btcutil.Address
	if len(body.PayerAddresses) > 0 {
		for _, addr := range body.PayerAddresses {
			if strings.TrimSpace(addr) == "" {
				Error(w, http.StatusBadRequest, "payer address required")
				return
			}
			decoded, err := btcutil.DecodeAddress(strings.TrimSpace(addr), params)
			if err != nil {
				Error(w, http.StatusBadRequest, fmt.Sprintf("invalid payer address: %v", err))
				return
			}
			payerAddresses = append(payerAddresses, decoded)
		}
	} else {
		payerAddresses = []btcutil.Address{payerAddr}
	}
	var changeAddr btcutil.Address
	if strings.TrimSpace(body.ChangeAddress) != "" {
		changeAddr, err = btcutil.DecodeAddress(strings.TrimSpace(body.ChangeAddress), params)
		if err != nil {
			Error(w, http.StatusBadRequest, fmt.Sprintf("invalid change address: %v", err))
			return
		}
	}

	target := body.BudgetSats
	if target <= 0 {
		target = contract.TotalBudgetSats
	}
	if target <= 0 {
		target = scstore.DefaultBudgetSats()
	}

	// Handle commitment_sats separately from budget_sats
	commitmentSats := body.CommitmentSats
	log.Printf("DEBUG: Initial body.CommitmentSats=%d, body.CommitmentTarget=%s", body.CommitmentSats, body.CommitmentTarget)
	if commitmentSats <= 0 {
		// If user is skipping donation (commitment_target != 'donation'), set commitment budget to 0
		// Otherwise fall back to contract budget for task payouts
		if body.CommitmentTarget != "donation" {
			commitmentSats = 0
			log.Printf("DEBUG: Setting commitmentSats=0 for skipped donation")
		} else {
			commitmentSats = 1000
			log.Printf("DEBUG: Setting commitmentSats=1000 for donation")
		}
	}
	// Only apply default for donation case if it was actually sent but empty
	if commitmentSats <= 0 && body.CommitmentTarget == "donation" && body.CommitmentSats == 0 {
		commitmentSats = scstore.DefaultBudgetSats()
		log.Printf("DEBUG: Setting commitmentSats=%d (default)", commitmentSats)
	}
	log.Printf("DEBUG: Final commitmentSats=%d", commitmentSats)

	fundingMode, fundingAddress := s.resolveFundingMode(r.Context(), contractID)
	primaryPayer := payerAddr
	var fundraiserAddr btcutil.Address
	var raiseFundPayers []bitcoin.PayerTarget
	var raiseFundPayerAddrs []btcutil.Address
	var raiseFundPayouts []bitcoin.PayoutOutput
	var raiseFundPayoutsByPayer map[string][]bitcoin.PayoutOutput
	var raiseFundPayerOrder []string
	var raiseFundPayersByWallet map[string]bitcoin.PayerTarget
	var raiseFundPayerTotals map[string]int64
	var raiseFundTaskIDs []string
	var raiseFundTasksByWallet map[string][]string
	if isRaiseFund(fundingMode) {
		rf, err := s.prepareRaiseFundContext(r.Context(), contractID, fundingAddress, params)
		if err != nil {
			Error(w, http.StatusBadRequest, err.Error())
			return
		}
		fundraiserAddr = rf.FundraiserAddr
		raiseFundPayouts = rf.Payouts
		raiseFundPayers = rf.Payers
		raiseFundPayerAddrs = rf.PayerAddrs
		raiseFundPayoutsByPayer = rf.PayoutsByPayer
		raiseFundPayersByWallet = rf.PayersByWallet
		raiseFundPayerOrder = rf.PayerOrder
		raiseFundPayerTotals = rf.PayerTotals
		raiseFundTaskIDs = rf.TaskIDs
		raiseFundTasksByWallet = rf.TasksByWallet
		target = rf.TargetSats
		payerAddresses = rf.PayerAddrs
		primaryPayer = rf.PayerAddrs[0]
		changeAddr = nil
	}
	if !isRaiseFund(fundingMode) && len(payerAddresses) > 1 && changeAddr == nil {
		Error(w, http.StatusBadRequest, "change_address required when using multiple payer addresses")
		return
	}

	var ingestionRec *services.IngestionRecord
	if s.ingestionSvc != nil {
		ingestionRec = s.resolveIngestionRecord(r.Context(), contractID)
	}
	usePixelHash := true
	if body.UsePixelHash != nil {
		usePixelHash = *body.UsePixelHash
	}
	pixelBytes, pixelSource, err := s.resolvePSBTPixel(contractID, body.PixelHash, usePixelHash, ingestionRec)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Prepare stego image + sandbox tarball BEFORE building the PSBT so
	// their SHA256 hashes can be inscribed in the OP_RETURN.  This lets
	// any peer confirm from on-chain data alone — no pubsub required.
	proposalID := s.resolveProposalIDForContract(r.Context(), contractID, ingestionRec)
	var publishArtifacts *PublishArtifacts
	var stegoHashBytes []byte
	if proposalID != "" {
		artifacts, err := s.PreparePublishArtifacts(r.Context(), proposalID)
		if err != nil {
			log.Printf("psbt: PreparePublishArtifacts failed for %s (continuing without): %v", proposalID, err)
		} else {
			publishArtifacts = artifacts
			stegoHashBytes = decodePixelHex(artifacts.StegoImageHash)
		}
	}

	// Fall back to explicit product pixel hash if stego preparation didn't produce one.
	var productPixelBytes []byte
	if stegoHashBytes != nil {
		productPixelBytes = stegoHashBytes
	} else if ph := strings.TrimSpace(body.ProductPixelHash); ph != "" {
		productPixelBytes = decodePixelHex(ph)
	}
	if productPixelBytes == nil {
		tasks, _ := s.store.ListTasks(smart_contract.TaskFilter{ContractID: contractID, Limit: 1})
		for _, t := range tasks {
			if t.MerkleProof != nil && len(strings.TrimSpace(t.MerkleProof.ProductPixelHash)) == 64 {
				productPixelBytes = decodePixelHex(t.MerkleProof.ProductPixelHash)
				break
			}
		}
	}

	commitmentTarget := strings.ToLower(strings.TrimSpace(body.CommitmentTarget))
	if commitmentTarget == "" {
		commitmentTarget = "funding"
	}
	var commitmentLockAddr btcutil.Address
	var donationAddr btcutil.Address // New: direct donation P2WPKH (no hashlock)
	switch commitmentTarget {
	case "donation":
		donation := strings.TrimSpace(os.Getenv("STARLIGHT_DONATION_ADDRESS"))
		if donation == "" {
			Error(w, http.StatusBadRequest, "donation address not configured")
			return
		}
		donationAddr, err = btcutil.DecodeAddress(donation, params)
		if err != nil {
			Error(w, http.StatusBadRequest, fmt.Sprintf("invalid donation address: %v", err))
			return
		}
		commitmentLockAddr = donationAddr // backward compat for metadata
	case "funding":
		if isRaiseFund(fundingMode) {
			if fundraiserAddr == nil {
				Error(w, http.StatusBadRequest, "missing fundraiser payout address")
				return
			}
			commitmentLockAddr = fundraiserAddr
		} else {
			commitmentLockAddr = primaryPayer
		}
	case "product":
		// Product commitment: hashlock is deferred to delivery time (stego reconciliation).
		// No commitment output in the funding PSBT; the hash will be based on the
		// final delivered product image rather than the original wish image.
		commitmentSats = 0
		commitmentLockAddr = primaryPayer
		log.Printf("DEBUG: commitment_target=product, deferring commitment to delivery (commitmentSats=0)")
	default:
		Error(w, http.StatusBadRequest, "invalid commitment_target")
		return
	}
	commitmentMeta := map[string]interface{}{
		"commitment_lock_address": addressOrEmpty(commitmentLockAddr),
		"commitment_target":       commitmentTarget,
	}

	var payouts []bitcoin.PayoutOutput
	payoutTotal := int64(0)
	if isRaiseFund(fundingMode) {
		payouts = raiseFundPayouts
		payoutTotal = target
	} else if len(body.Payouts) > 0 {
		for _, payout := range body.Payouts {
			if strings.TrimSpace(payout.Address) == "" {
				Error(w, http.StatusBadRequest, "payout address required")
				return
			}
			addr, err := btcutil.DecodeAddress(strings.TrimSpace(payout.Address), params)
			if err != nil {
				Error(w, http.StatusBadRequest, fmt.Sprintf("invalid payout address: %v", err))
				return
			}
			if payout.AmountSats <= 0 {
				Error(w, http.StatusBadRequest, "payout amount must be positive")
				return
			}
			payouts = append(payouts, bitcoin.PayoutOutput{
				Address:   addr,
				ValueSats: payout.AmountSats,
			})
		}
		for _, payout := range payouts {
			payoutTotal += payout.ValueSats
		}
		if payoutTotal > 0 {
			if target <= 0 {
				target = payoutTotal
			}
			if payoutTotal > target {
				Error(w, http.StatusBadRequest, "payout total exceeds budget_sats")
				return
			}
		}
	}

	var contractorAddr btcutil.Address
	if payoutTotal == 0 {
		if strings.TrimSpace(body.ContractorAPIKey) != "" {
			if rec, ok := s.apiKeys.Get(body.ContractorAPIKey); ok && strings.TrimSpace(rec.Wallet) != "" {
				contractorAddr, err = btcutil.DecodeAddress(rec.Wallet, params)
			}
		}
		if contractorAddr == nil && strings.TrimSpace(fundingAddress) != "" {
			contractorAddr, err = btcutil.DecodeAddress(strings.TrimSpace(fundingAddress), params)
		}
		if contractorAddr == nil && strings.TrimSpace(body.ContractorWallet) != "" {
			contractorAddr, err = btcutil.DecodeAddress(strings.TrimSpace(body.ContractorWallet), params)
		}
		if err != nil {
			Error(w, http.StatusBadRequest, fmt.Sprintf("invalid contractor wallet: %v", err))
			return
		}
		if contractorAddr == nil {
			Error(w, http.StatusBadRequest, "contractor wallet required when payouts are empty")
			return
		}
	}

	var res *bitcoin.PSBTResult
	splitRaiseFund := isRaiseFund(fundingMode) && body.SplitPSBT
	if splitRaiseFund {
		var psbtEntries []map[string]interface{}
		var fundingTxIDs []string
		var commitmentInfo *bitcoin.PSBTResult
		var payoutScripts [][]byte
		var payoutScriptHashes []string
		var payoutScriptHash160s []string
		for _, wallet := range raiseFundPayerOrder {
			target := raiseFundPayerTotals[wallet]
			payerTarget := raiseFundPayersByWallet[wallet]
			payerPayouts := raiseFundPayoutsByPayer[wallet]
			psbtReq := bitcoin.PSBTRequest{
				PayerAddress:      payerTarget.Address,
				TargetValueSats:   target,
				PixelHash:         pixelBytes,
				ProductPixelHash:  productPixelBytes,
				CommitmentSats:    commitmentSats,
				DonationAddress:   donationAddr,
				CommitmentAddress: commitmentLockAddr,
				Payouts:           payerPayouts,
				FeeRateSatPerVB:   body.FeeRate,
			}
			splitRes, err := bitcoin.BuildFundingPSBT(s.mempool, params, psbtReq)
			if err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			if splitRes.FundingTxID != "" {
				fundingTxIDs = append(fundingTxIDs, splitRes.FundingTxID)
			}
			if commitmentInfo == nil {
				commitmentInfo = splitRes
			}
			if len(splitRes.PayoutScripts) > 0 {
				payoutScripts = append(payoutScripts, splitRes.PayoutScripts...)
				shaHashes, hash160s := buildScriptHashes(splitRes.PayoutScripts)
				payoutScriptHashes = append(payoutScriptHashes, shaHashes...)
				payoutScriptHash160s = append(payoutScriptHash160s, hash160s...)
			}
			if len(raiseFundTasksByWallet[wallet]) > 0 {
				for _, taskID := range raiseFundTasksByWallet[wallet] {
					if err := s.updateTaskCommitmentProof(r.Context(), taskID, splitRes, pixelBytes, commitmentTarget); err != nil {
						log.Printf("psbt: failed to update task proof for %s: %v", taskID, err)
					}
				}
			}
			psbtEntries = append(psbtEntries, map[string]interface{}{
				"psbt":                    splitRes.EncodedHex,
				"psbt_hex":                splitRes.EncodedHex,
				"psbt_base64":             splitRes.EncodedBase64,
				"funding_txid":            splitRes.FundingTxID,
				"fee_sats":                splitRes.FeeSats,
				"change_sats":             splitRes.ChangeSats,
				"selected_sats":           splitRes.SelectedSats,
				"payout_script":           hex.EncodeToString(splitRes.PayoutScript),
				"payout_scripts":          hexSlice(splitRes.PayoutScripts),
				"payout_amounts":          splitRes.PayoutAmounts,
				"commitment_script":       hex.EncodeToString(splitRes.CommitmentScript),
				"commitment_sats":         splitRes.CommitmentSats,
				"commitment_vout":         splitRes.CommitmentVout,
				"redeem_script":           hex.EncodeToString(splitRes.RedeemScript),
				"redeem_script_hash":      hex.EncodeToString(splitRes.RedeemScriptHash),
				"commitment_address":      splitRes.CommitmentAddr,
				"commitment_lock_address": addressOrEmpty(commitmentLockAddr),
				"pixel_hash":              strings.TrimSpace(body.PixelHash),
				"payer_address":           payerTarget.Address.EncodeAddress(),
				"payer_addresses":         []string{payerTarget.Address.EncodeAddress()},
				"change_address":          firstString(splitRes.ChangeAddresses),
				"change_addresses":        splitRes.ChangeAddresses,
				"change_amounts":          splitRes.ChangeAmounts,
				"funding_mode":            fundingMode,
				"contract_id":             contractID,
				"pixel_source":            defaultPixelSource(pixelSource, pixelBytes),
				"budget_sats":             target,
				"contractor":              "",
				"network_params":          params.Name,
			})
		}
		// proposalID was resolved before artifact preparation above.
		if ingestionRec != nil && len(fundingTxIDs) > 0 {
			metadata := map[string]interface{}{
				"funding_txids":           fundingTxIDs,
				"funding_txid":            fundingTxIDs[0],
				"payout_scripts":          hexSlice(payoutScripts),
				"payout_script_hashes":    payoutScriptHashes,
				"payout_script_hash160s":  payoutScriptHash160s,
				"commitment_lock_address": addressOrEmpty(commitmentLockAddr),
				"commitment_target":       commitmentTarget,
			}
			if err := s.ingestionSvc.UpdateMetadata(ingestionRec.ID, metadata); err != nil {
				log.Printf("psbt: failed to store funding_txids for %s: %v", ingestionRec.ID, err)
			}
			s.publishIngestUpdate(r.Context(), proposalID, ingestionRec.ID, strings.TrimSpace(body.PixelHash), fundingTxIDs, commitmentInfo, commitmentLockAddr, commitmentTarget, payoutScripts, payoutScriptHashes, payoutScriptHash160s)
		}
		if proposalID != "" {
			s.updateProposalMetadataBestEffort(r.Context(), proposalID, commitmentMeta)
		}
		if proposalID != "" {
			publishPixelHash := strings.TrimSpace(body.PixelHash)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				if publishArtifacts != nil {
					s.FinalizePublishArtifacts(ctx, proposalID, publishArtifacts)
				} else {
					if err := s.maybePublishStegoForProposal(ctx, proposalID); err != nil {
						log.Printf("stego publish on split psbt failed for proposal %s: %v", proposalID, err)
					}
				}
				s.publishPendingStegoIngest(ctx, proposalID, publishPixelHash)
			}()
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"psbts":           psbtEntries,
			"funding_mode":    fundingMode,
			"contract_id":     contractID,
			"budget_sats":     target,
			"payer_addresses": addressSlice(raiseFundPayerAddrs),
			"network_params":  params.Name,
			"split_psbt":      true,
			"funding_txids":   fundingTxIDs,
		})
		return
	}

	if isRaiseFund(fundingMode) {
		res, err = bitcoin.BuildRaiseFundPSBT(
			s.mempool,
			params,
			raiseFundPayers,
			payouts,
			pixelBytes,
			commitmentSats,
			commitmentLockAddr,
			body.FeeRate,
		)
	} else {
		effectiveChangeAddr := changeAddr
		if effectiveChangeAddr == nil && len(payerAddresses) > 0 {
			effectiveChangeAddr = payerAddresses[0]
		}
		psbtReq := bitcoin.PSBTRequest{
			PayerAddress:      primaryPayer,
			PayerAddresses:    payerAddresses,
			TargetValueSats:   target,
			PixelHash:         pixelBytes,
			ProductPixelHash:  productPixelBytes,
			CommitmentSats:    commitmentSats,
			DonationAddress:   donationAddr,
			CommitmentAddress: commitmentLockAddr,
			ContractorAddress: contractorAddr,
			Payouts:           payouts,
			FeeRateSatPerVB:   body.FeeRate,
			ChangeAddress:     effectiveChangeAddr,
			UseAllPayers:      isRaiseFund(fundingMode),
		}
		res, err = bitcoin.BuildFundingPSBT(s.mempool, params, psbtReq)
		changeAddr = effectiveChangeAddr
	}
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	// proposalID was resolved before artifact preparation above.
	if ingestionRec != nil && res.FundingTxID != "" {
		scriptHashes, scriptHash160s := buildScriptHashes(res.PayoutScripts)
		if err := s.ingestionSvc.UpdateMetadata(ingestionRec.ID, map[string]interface{}{
			"funding_txids":           []string{res.FundingTxID},
			"funding_txid":            res.FundingTxID,
			"payout_scripts":          hexSlice(res.PayoutScripts),
			"payout_script_hashes":    scriptHashes,
			"payout_script_hash160s":  scriptHash160s,
			"commitment_lock_address": addressOrEmpty(commitmentLockAddr),
			"commitment_target":       commitmentTarget,
		}); err != nil {
			log.Printf("psbt: failed to store funding_txid for %s: %v", ingestionRec.ID, err)
		}
		s.publishIngestUpdate(r.Context(), proposalID, ingestionRec.ID, strings.TrimSpace(body.PixelHash), []string{res.FundingTxID}, res, commitmentLockAddr, commitmentTarget, res.PayoutScripts, nil, nil)
	}
	if proposalID != "" {
		s.updateProposalMetadataBestEffort(r.Context(), proposalID, commitmentMeta)
	}
	// Finalize: IPFS upload + pubsub announcement (best-effort, async).
	// The old maybePublishStegoForProposal is kept as fallback for the
	// legacy path but the primary flow now uses PreparePublishArtifacts.
	if proposalID != "" {
		publishPixelHash := strings.TrimSpace(body.PixelHash)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if publishArtifacts != nil {
				s.FinalizePublishArtifacts(ctx, proposalID, publishArtifacts)
			} else {
				// Fallback: old path for when artifact preparation failed.
				if err := s.maybePublishStegoForProposal(ctx, proposalID); err != nil {
					log.Printf("stego publish on psbt failed for proposal %s: %v", proposalID, err)
				}
			}
			s.publishPendingStegoIngest(ctx, proposalID, publishPixelHash)
		}()
	}
	if taskID := strings.TrimSpace(body.TaskID); taskID != "" {
		if err := s.updateTaskCommitmentProof(r.Context(), taskID, res, pixelBytes, commitmentTarget); err != nil {
			log.Printf("psbt: failed to update task proof for %s: %v", taskID, err)
		}
	} else if isRaiseFund(fundingMode) && len(raiseFundTaskIDs) > 0 {
		for _, taskID := range raiseFundTaskIDs {
			if err := s.updateTaskCommitmentProof(r.Context(), taskID, res, pixelBytes, commitmentTarget); err != nil {
				log.Printf("psbt: failed to update task proof for %s: %v", taskID, err)
			}
		}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"psbt":                    res.EncodedHex, // primary: hex for wallet import
		"psbt_hex":                res.EncodedHex,
		"psbt_base64":             res.EncodedBase64,
		"funding_txid":            res.FundingTxID,
		"fee_sats":                res.FeeSats,
		"change_sats":             res.ChangeSats,
		"selected_sats":           res.SelectedSats,
		"payout_script":           hex.EncodeToString(res.PayoutScript),
		"payout_scripts":          hexSlice(res.PayoutScripts),
		"payout_amounts":          res.PayoutAmounts,
		"commitment_script":       hex.EncodeToString(res.CommitmentScript),
		"commitment_sats":         res.CommitmentSats,
		"commitment_vout":         res.CommitmentVout,
		"redeem_script":           hex.EncodeToString(res.RedeemScript),
		"redeem_script_hash":      hex.EncodeToString(res.RedeemScriptHash),
		"commitment_address":      res.CommitmentAddr,
		"commitment_lock_address": addressOrEmpty(commitmentLockAddr),
		"pixel_hash":              hex.EncodeToString(pixelBytes),
		"pixel_source":            pixelSource,
		"payer_address":           primaryPayer.EncodeAddress(),
		"payer_addresses":         addressSlice(payerAddresses),
		"change_address":          addressOrEmpty(changeAddr),
		"change_addresses":        res.ChangeAddresses,
		"change_amounts":          res.ChangeAmounts,
		"funding_mode":            fundingMode,
		"contract_id":             contractID,
		"budget_sats":             target,
		"contractor":              contractorAddressFor(contractorAddr),
		"network_params":          params.Name,
	})
}



func normalizePixelBytes(b []byte) []byte {
	if l := len(b); l == 20 || l == 32 {
		return b
	}
	return nil
}

func decodePixelHex(value string) []byte {
	if value == "" {
		return nil
	}
	if b, err := hex.DecodeString(value); err == nil {
		return normalizePixelBytes(b)
	}
	return nil
}

func (s *Server) resolvePSBTPixel(contractID, pixelHash string, usePixelHash bool, ingestionRec *services.IngestionRecord) ([]byte, string, error) {
	if !usePixelHash {
		return nil, "", nil
	}
	var pixelBytes []byte
	pixelSource := ""
	if ph := strings.TrimSpace(pixelHash); ph != "" {
		pixelBytes = decodePixelHex(ph)
		if pixelBytes != nil {
			pixelSource = "pixel_hash"
		}
	}
	if pixelBytes == nil && ingestionRec != nil {
		pixelBytes = resolvePixelHashFromIngestion(ingestionRec, normalizePixelBytes)
		if pixelBytes != nil {
			pixelSource = "visible_pixel_hash"
		}
	}
	if pixelBytes == nil {
		pixelBytes = decodePixelHex(strings.TrimSpace(contractID))
		if pixelBytes != nil {
			pixelSource = "contract_id"
		}
	}
	if pixelBytes == nil {
		return nil, "", fmt.Errorf("missing 32-byte pixel hash for commitment output")
	}
	return pixelBytes, pixelSource, nil
}

// raiseFundContext holds precomputed raise_fund PSBT inputs.
type raiseFundContext struct {
	FundraiserAddr  btcutil.Address
	Payouts         []bitcoin.PayoutOutput
	Payers          []bitcoin.PayerTarget
	PayerAddrs      []btcutil.Address
	PayoutsByPayer  map[string][]bitcoin.PayoutOutput
	PayersByWallet  map[string]bitcoin.PayerTarget
	PayerOrder      []string
	PayerTotals     map[string]int64
	TaskIDs         []string
	TasksByWallet   map[string][]string
	TargetSats      int64
}

func (s *Server) prepareRaiseFundContext(ctx context.Context, contractID, fundingAddress string, params *chaincfg.Params) (*raiseFundContext, error) {
	if s.store == nil {
		return nil, fmt.Errorf("task store unavailable for raise_fund")
	}
	tasks, err := s.store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
	if err != nil {
		return nil, fmt.Errorf("failed to load tasks: %v", err)
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks available for raise_fund")
	}
	if strings.TrimSpace(fundingAddress) == "" {
		return nil, fmt.Errorf("missing fundraiser payout address")
	}
	fundAddr, err := btcutil.DecodeAddress(strings.TrimSpace(fundingAddress), params)
	if err != nil {
		return nil, fmt.Errorf("invalid fundraiser payout address: %v", err)
	}
	out := &raiseFundContext{
		FundraiserAddr: fundAddr,
		PayoutsByPayer: make(map[string][]bitcoin.PayoutOutput),
		PayersByWallet: make(map[string]bitcoin.PayerTarget),
		TasksByWallet:  make(map[string][]string),
		PayerTotals:    make(map[string]int64),
	}
	var payerOrder []string
	var payoutTotal int64
	for _, task := range tasks {
		if task.BudgetSats <= 0 {
			return nil, fmt.Errorf("task budget missing for %s", task.TaskID)
		}
		payoutTotal += task.BudgetSats
		out.TaskIDs = append(out.TaskIDs, task.TaskID)
		payout := bitcoin.PayoutOutput{Address: fundAddr, ValueSats: task.BudgetSats}
		out.Payouts = append(out.Payouts, payout)
		taskWallet := strings.TrimSpace(task.ContractorWallet)
		if taskWallet == "" && task.MerkleProof != nil {
			taskWallet = strings.TrimSpace(task.MerkleProof.ContractorWallet)
		}
		if taskWallet == "" {
			return nil, fmt.Errorf("missing contractor wallet for task %s", task.TaskID)
		}
		if _, ok := out.PayerTotals[taskWallet]; !ok {
			payerOrder = append(payerOrder, taskWallet)
		}
		out.PayerTotals[taskWallet] += task.BudgetSats
		out.PayoutsByPayer[taskWallet] = append(out.PayoutsByPayer[taskWallet], payout)
		out.TasksByWallet[taskWallet] = append(out.TasksByWallet[taskWallet], task.TaskID)
	}
	for _, wallet := range payerOrder {
		addr, err := btcutil.DecodeAddress(wallet, params)
		if err != nil {
			return nil, fmt.Errorf("invalid contractor wallet: %v", err)
		}
		payerTarget := bitcoin.PayerTarget{Address: addr, TargetSats: out.PayerTotals[wallet]}
		out.Payers = append(out.Payers, payerTarget)
		out.PayerAddrs = append(out.PayerAddrs, addr)
		out.PayersByWallet[wallet] = payerTarget
	}
	if len(out.Payers) == 0 {
		return nil, fmt.Errorf("no contractor wallets found for raise_fund")
	}
	out.TargetSats = payoutTotal
	out.PayerOrder = payerOrder
	return out, nil
}

func contractorAddressFor(addr btcutil.Address) string {
	if addr == nil {
		return ""
	}
	return addr.EncodeAddress()
}

func (s *Server) publishIngestUpdate(ctx context.Context, proposalID, ingestionID, visiblePixelHash string, fundingTxIDs []string, res *bitcoin.PSBTResult, commitmentLockAddr btcutil.Address, commitmentTarget string, payoutScripts [][]byte, payoutScriptHashes, payoutScriptHash160s []string) {
	topic := strings.TrimSpace(os.Getenv("IPFS_MIRROR_TOPIC"))
	if topic == "" {
		return
	}
	ingestionID = strings.TrimSpace(ingestionID)
	visiblePixelHash = strings.TrimSpace(visiblePixelHash)
	if ingestionID == "" && visiblePixelHash == "" {
		return
	}
	announcement := ingestUpdateAnnouncement{
		Type:               "ingest_update",
		IngestionID:        ingestionID,
		ProposalID:         strings.TrimSpace(proposalID),
		VisiblePixelHash:   visiblePixelHash,
		FundingTxIDs:       fundingTxIDs,
		CommitmentLockAddr: addressOrEmpty(commitmentLockAddr),
		CommitmentTarget:   strings.TrimSpace(commitmentTarget),
		Timestamp:          time.Now().Unix(),
	}
	if len(fundingTxIDs) > 0 {
		announcement.FundingTxID = fundingTxIDs[0]
	}
	if res != nil {
		announcement.CommitmentAddress = strings.TrimSpace(res.CommitmentAddr)
		if len(res.CommitmentScript) > 0 {
			announcement.CommitmentScript = hex.EncodeToString(res.CommitmentScript)
		}
		if res.CommitmentVout > 0 {
			announcement.CommitmentVout = res.CommitmentVout
		}
		if res.CommitmentSats > 0 {
			announcement.CommitmentSats = res.CommitmentSats
		}
		if len(res.PayoutScript) > 0 {
			announcement.PayoutScript = hex.EncodeToString(res.PayoutScript)
		}
		if len(res.PayoutScripts) > 0 {
			announcement.PayoutScripts = hexSlice(res.PayoutScripts)
		}
	}
	payload, err := json.Marshal(announcement)
	if err != nil {
		log.Printf("psbt: ingest update marshal failed: %v", err)
		return
	}
	client := ipfs.NewClientFromEnv()
	if client != nil {
		if err := client.PubsubPublish(ctx, topic, payload); err != nil {
			log.Printf("psbt: ingest update publish failed: %v", err)
		}
	} else {
		log.Printf("psbt: ingest update publish skipped - IPFS disabled")
	}
}

func (s *Server) publishPendingStegoIngest(ctx context.Context, proposalID, visiblePixelHash string) {
	topic := strings.TrimSpace(os.Getenv("IPFS_MIRROR_TOPIC"))
	if topic == "" {
		topic = "stargate-uploads"
	}
	if s.store == nil {
		return
	}
	proposalID = strings.TrimSpace(proposalID)
	if proposalID == "" {
		return
	}
	p, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return
	}
	meta := p.Metadata
	if meta == nil {
		return
	}
	stegoCID := strings.TrimSpace(toString(meta["stego_image_cid"]))
	if stegoCID == "" {
		return
	}
	visible := strings.TrimSpace(visiblePixelHash)
	if visible == "" {
		visible = strings.TrimSpace(p.VisiblePixelHash)
	}
	if visible == "" {
		visible = strings.TrimSpace(toString(meta["visible_pixel_hash"]))
	}
	if visible == "" {
		return
	}
	var message string
	if s.ingestionSvc != nil && visible != "" {
		rec, err := s.ingestionSvc.Get(visible)
		if (err != nil || rec == nil) && !strings.HasPrefix(visible, "wish-") {
			rec, err = s.ingestionSvc.Get("wish-" + visible)
		}
		if err == nil && rec != nil && rec.Metadata != nil {
			if v, ok := rec.Metadata["wish_text"].(string); ok && strings.TrimSpace(v) != "" {
				message = strings.TrimSpace(v)
			}
			if message == "" {
				if v, ok := rec.Metadata["embedded_message"].(string); ok && strings.TrimSpace(v) != "" {
					message = strings.TrimSpace(v)
				}
			}
			if message == "" {
				if v, ok := rec.Metadata["message"].(string); ok && strings.TrimSpace(v) != "" {
					message = strings.TrimSpace(v)
				}
			}
			if v, ok := rec.Metadata["price"].(string); ok && strings.TrimSpace(v) != "" {
				meta["price"] = v
			}
			if v, ok := rec.Metadata["price_unit"].(string); ok && strings.TrimSpace(v) != "" {
				meta["price_unit"] = v
			}
			if v, ok := rec.Metadata["address"].(string); ok && strings.TrimSpace(v) != "" {
				meta["funding_address"] = v
			}
			if v, ok := rec.Metadata["funding_mode"].(string); ok && strings.TrimSpace(v) != "" {
				meta["funding_mode"] = v
			}
		}
	}
	if message == "" {
		message = strings.TrimSpace(p.DescriptionMD)
	}
	announcement := pendingIngestAnnouncement{
		Type:             "pending_ingest",
		IngestionID:      visible,
		ProposalID:       proposalID,
		VisiblePixelHash: visible,
		ImageCID:         stegoCID,
		Filename:         "stego.png",
		Method:           getStegoMethodFromFilename("stego.png"), // Use appropriate method based on image format
		Message:          message,
		Price:            strings.TrimSpace(toString(meta["price"])),
		PriceUnit:        strings.TrimSpace(toString(meta["price_unit"])),
		Address:          strings.TrimSpace(toString(meta["funding_address"])),
		FundingMode:      strings.TrimSpace(toString(meta["funding_mode"])),
		Timestamp:        time.Now().Unix(),
		ProposalTitle:    strings.TrimSpace(p.Title),
		ProposalDesc:     strings.TrimSpace(p.DescriptionMD),
		BudgetSats:       p.BudgetSats,
		PayloadCID:       strings.TrimSpace(toString(meta["stego_payload_cid"])),
	}
	// Include structured task data so peers can create proper proposals
	// without depending on IPFS payload fetch
	for _, t := range p.Tasks {
		announcement.Tasks = append(announcement.Tasks, announcementTask{
			TaskID:           t.TaskID,
			Title:            t.Title,
			Description:      t.Description,
			BudgetSats:       t.BudgetSats,
			Skills:           t.Skills,
			Status:           t.Status,
			ContractorWallet: t.ContractorWallet,
		})
	}
	payload, err := json.Marshal(announcement)
	if err != nil {
		return
	}
	client := ipfs.NewClientFromEnv()
	if client != nil {
		if err := client.PubsubPublish(ctx, topic, payload); err != nil {
			log.Printf("psbt: pending ingest publish failed: %v", err)
		}
	} else {
		log.Printf("psbt: pending ingest publish skipped - IPFS disabled")
	}
}

func (s *Server) resolveFundingMode(ctx context.Context, contractID string) (string, string) {
	if s.psbtSvc == nil {
		return "", ""
	}
	return s.psbtSvc.ResolveFundingMode(ctx, contractID)
}

func (s *Server) resolveIngestionRecord(ctx context.Context, contractID string) *services.IngestionRecord {
	if s.psbtSvc == nil {
		return nil
	}
	return s.psbtSvc.ResolveIngestionRecord(ctx, contractID)
}

func (s *Server) resolveProposalIDForContract(ctx context.Context, contractID string, rec *services.IngestionRecord) string {
	if s.psbtSvc == nil {
		return strings.TrimSpace(contractID)
	}
	return s.psbtSvc.ResolveProposalIDForContract(ctx, contractID, rec)
}

func (s *Server) updateProposalMetadataBestEffort(ctx context.Context, proposalID string, updates map[string]interface{}) {
	if s.store == nil || strings.TrimSpace(proposalID) == "" || len(updates) == 0 {
		return
	}
	if err := s.store.UpdateProposalMetadata(ctx, proposalID, updates); err != nil {
		log.Printf("psbt: failed to update proposal metadata for %s: %v", proposalID, err)
	}
}

func (s *Server) ingestionFromProposalMeta(meta map[string]interface{}, visiblePixelHash string) *services.IngestionRecord {
	if s.psbtSvc == nil {
		return nil
	}
	return s.psbtSvc.IngestionFromProposalMeta(meta, visiblePixelHash)
}

func isRaiseFund(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "raise_fund", "fundraiser", "fundraise":
		return true
	default:
		return false
	}
}

func looksLikeRaiseFund(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(normalized, "fund raising") ||
		strings.Contains(normalized, "fundraising") ||
		strings.Contains(normalized, "raise fund") ||
		strings.Contains(normalized, "fundraise")
}

func fundingAddressFromMeta(meta map[string]interface{}) string {
	if meta == nil {
		return ""
	}
	if v := strings.TrimSpace(toString(meta["funding_address"])); v != "" {
		return v
	}
	if v := strings.TrimSpace(toString(meta["address"])); v != "" {
		return v
	}
	return ""
}

func toString(value interface{}) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func addressSlice(addrs []btcutil.Address) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if addr == nil {
			continue
		}
		out = append(out, addr.EncodeAddress())
	}
	return out
}

func addressOrEmpty(addr btcutil.Address) string {
	if addr == nil {
		return ""
	}
	return addr.EncodeAddress()
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *Server) resolveContractorPayers(ctx context.Context, contractID string, params *chaincfg.Params) ([]btcutil.Address, error) {
	if s.store == nil {
		return nil, fmt.Errorf("task store unavailable")
	}
	tasks, err := s.store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
	if err != nil {
		return nil, fmt.Errorf("load contractor wallets: %w", err)
	}
	seen := make(map[string]struct{})
	var addrs []btcutil.Address
	for _, task := range tasks {
		candidate := strings.TrimSpace(task.ContractorWallet)
		if candidate == "" && task.MerkleProof != nil {
			candidate = strings.TrimSpace(task.MerkleProof.ContractorWallet)
		}
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		addr, err := btcutil.DecodeAddress(candidate, params)
		if err != nil {
			return nil, fmt.Errorf("invalid contractor wallet: %v", err)
		}
		seen[candidate] = struct{}{}
		addrs = append(addrs, addr)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no contractor wallets available for funding inputs")
	}
	return addrs, nil
}

func resolvePixelHashFromIngestion(rec *services.IngestionRecord, normalize func([]byte) []byte) []byte {
	return scservices.ResolvePixelHashFromIngestion(rec, normalize)
}

func pixelSourceForBytes(pixel []byte) string {
	switch len(pixel) {
	case 32:
		return "witness_script_hash"
	case 20:
		return "script_hash"
	default:
		return ""
	}
}

func defaultPixelSource(source string, pixel []byte) string {
	if strings.TrimSpace(source) != "" {
		return source
	}
	return pixelSourceForBytes(pixel)
}

func hexSlice(payloads [][]byte) []string {
	if len(payloads) == 0 {
		return nil
	}
	out := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		out = append(out, hex.EncodeToString(payload))
	}
	return out
}

func hash160Hex(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	sha := sha256.Sum256(data)
	hasher := ripemd160.New()
	_, _ = hasher.Write(sha[:])
	return hex.EncodeToString(hasher.Sum(nil))
}

func hash256Hex(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func buildScriptHashes(scripts [][]byte) ([]string, []string) {
	if len(scripts) == 0 {
		return nil, nil
	}
	shaHashes := make([]string, 0, len(scripts))
	hash160s := make([]string, 0, len(scripts))
	for _, script := range scripts {
		shaHashes = append(shaHashes, hash256Hex(script))
		hash160s = append(hash160s, hash160Hex(script))
	}
	return shaHashes, hash160s
}

func (s *Server) updateTaskCommitmentProof(ctx context.Context, taskID string, res *bitcoin.PSBTResult, pixelBytes []byte, commitmentTarget string) error {
	if s.taskSvc == nil {
		return nil
	}
	return s.taskSvc.UpdateTaskCommitmentProof(ctx, taskID, res, pixelBytes, commitmentTarget)
}

func (s *Server) handleCommitmentPSBT(w http.ResponseWriter, r *http.Request, contractID string) {
	if r.Header.Get("Content-Type") != "" && !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	if s.mempool == nil {
		Error(w, http.StatusServiceUnavailable, "commitment builder unavailable")
		return
	}

	var body struct {
		TaskID             string `json:"task_id"`
		DestinationAddress string `json:"destination_address"`
		FeeRate            int64  `json:"fee_rate_sats_vb"`
		Preimage           string `json:"preimage"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}

	task, err := s.resolveCommitmentTask(contractID, strings.TrimSpace(body.TaskID))
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if task.MerkleProof == nil {
		Error(w, http.StatusBadRequest, "task missing merkle_proof commitment data")
		return
	}
	proof := task.MerkleProof

	redeemScriptHex := strings.TrimSpace(proof.CommitmentRedeemScript)
	if redeemScriptHex == "" {
		Error(w, http.StatusBadRequest, "missing commitment redeem script")
		return
	}
	redeemScript, err := hex.DecodeString(redeemScriptHex)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid commitment redeem script")
		return
	}

	preimageHex := strings.TrimSpace(body.Preimage)
	if preimageHex == "" {
		preimageHex = strings.TrimSpace(proof.CommitmentPixelHash)
	}
	preimage, err := hex.DecodeString(preimageHex)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid preimage hex")
		return
	}

	destAddress := strings.TrimSpace(body.DestinationAddress)
	if destAddress == "" {
		destAddress = strings.TrimSpace(os.Getenv("STARLIGHT_DONATION_ADDRESS"))
	}
	if destAddress == "" {
		Error(w, http.StatusBadRequest, "missing destination address")
		return
	}
	params := networkParamsFromEnv()
	destAddr, err := btcutil.DecodeAddress(destAddress, params)
	if err != nil {
		Error(w, http.StatusBadRequest, fmt.Sprintf("invalid destination address: %v", err))
		return
	}

	if proof.TxID == "" {
		Error(w, http.StatusBadRequest, "missing funding txid for commitment output")
		return
	}
	if proof.CommitmentVout == 0 {
		Error(w, http.StatusBadRequest, "missing commitment vout")
		return
	}

	res, err := bitcoin.BuildCommitmentSweepTx(s.mempool, params, proof.TxID, proof.CommitmentVout, redeemScript, preimage, destAddr, body.FeeRate)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"tx_hex":          res.RawTxHex,
		"fee_sats":        res.FeeSats,
		"input_sats":      res.InputSats,
		"output_sats":     res.OutputSats,
		"destination":     destAddr.EncodeAddress(),
		"contract_id":     contractID,
		"task_id":         task.TaskID,
		"funding_txid":    proof.TxID,
		"commitment_vout": proof.CommitmentVout,
	})
}

// handlePaymentDetails returns all necessary information for building a PSBT,
// including recipient addresses, total amounts, and contract details.
func (s *Server) handlePaymentDetails(w http.ResponseWriter, r *http.Request, contractID string) {
	if r.Method != http.MethodGet {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Authenticate the caller
	if s.apiKeys == nil {
		Error(w, http.StatusServiceUnavailable, "api key validation unavailable")
		return
	}
	payerKey := r.Header.Get("X-API-Key")
	payerRec, ok := s.apiKeys.Get(payerKey)
	if !ok {
		Error(w, http.StatusForbidden, "invalid api key")
		return
	}
	if strings.TrimSpace(payerRec.Wallet) == "" {
		Error(w, http.StatusForbidden, "api key missing wallet binding - please associate a Bitcoin wallet address with your API key")
		return
	}

	ctx := r.Context()

	// Get contract tasks to calculate payment details
	tasks, err := s.store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
	if err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("failed to load tasks: %v", err))
		return
	}

	if len(tasks) == 0 {
		Error(w, http.StatusNotFound, "no tasks found for contract")
		return
	}

	// Calculate total payout amount
	var totalPayoutSats int64
	var approvedTasks int
	var missingWallets int
	payouts := make(map[string]int64)

	for _, task := range tasks {
		if task.Status == "approved" {
			approvedTasks++
			totalPayoutSats += task.BudgetSats
			// Use the contractor's claimed wallet or the wallet from the task
			wallet := strings.TrimSpace(task.ContractorWallet)
			if wallet == "" && task.MerkleProof != nil {
				wallet = strings.TrimSpace(task.MerkleProof.ContractorWallet)
			}
			if wallet == "" {
				missingWallets++
				continue
			}
			payouts[wallet] += task.BudgetSats
		}
	}

	if approvedTasks == 0 {
		Error(w, http.StatusBadRequest, "no approved tasks with payouts found")
		return
	}
	if missingWallets > 0 {
		Error(w, http.StatusBadRequest, fmt.Sprintf("approved tasks missing contractor wallet (%d missing)", missingWallets))
		return
	}

	// Convert payouts map to response format
	payoutAddresses := make([]string, 0, len(payouts))
	for wallet := range payouts {
		payoutAddresses = append(payoutAddresses, wallet)
	}
	sort.Strings(payoutAddresses)
	payoutAmounts := make([]int64, 0, len(payoutAddresses))
	for _, wallet := range payoutAddresses {
		payoutAmounts = append(payoutAmounts, payouts[wallet])
	}

	// Get proposal metadata for additional context
	var proposal smart_contract.Proposal
	if proposals, err := s.store.ListProposals(ctx, smart_contract.ProposalFilter{ContractID: contractID}); err == nil && len(proposals) > 0 {
		proposal = proposals[0]
	}
	contractStatus := proposal.Status
	if contract, err := s.store.GetContract(contractID); err == nil {
		contractStatus = contract.Status
	}

	// Return comprehensive payment details
	JSON(w, http.StatusOK, map[string]interface{}{
		"contract_id":       contractID,
		"total_payout_sats": totalPayoutSats,
		"payout_addresses":  payoutAddresses,
		"payout_amounts":    payoutAmounts,
		"approved_tasks":    approvedTasks,
		"payer_wallet":      strings.TrimSpace(payerRec.Wallet),
		"contract_status":   contractStatus,
		"proposal_metadata": proposal.Metadata,
		"currency":          "sats",
		"network":           "testnet", // TODO: Get from config
	})
}

func (s *Server) resolveCommitmentTask(contractID, taskID string) (smart_contract.Task, error) {
	if taskID != "" {
		return s.store.GetTask(taskID)
	}
	tasks, err := s.store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
	if err != nil {
		return smart_contract.Task{}, err
	}
	for _, t := range tasks {
		if t.MerkleProof == nil {
			continue
		}
		if t.MerkleProof.CommitmentRedeemScript != "" && t.MerkleProof.CommitmentVout > 0 {
			return t, nil
		}
	}
	return smart_contract.Task{}, fmt.Errorf("no task with commitment metadata")
}

func contractIDFromMeta(meta map[string]interface{}, fallback string) string {
	if meta != nil {
		if v, ok := meta["visible_pixel_hash"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		if v, ok := meta["contract_id"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		if v, ok := meta["ingestion_id"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return fallback
}

func networkParamsFromEnv() *chaincfg.Params {
	switch bitcoin.GetCurrentNetwork() {
	case "mainnet":
		return &chaincfg.MainNetParams
	case "signet":
		return &chaincfg.SigNetParams
	case "testnet":
		return &chaincfg.TestNet3Params
	default:
		return &chaincfg.TestNet4Params
	}
}
