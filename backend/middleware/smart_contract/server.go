package smart_contract

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"golang.org/x/crypto/ripemd160"
	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	auth "stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"
)

// Server wires handlers for MCP endpoints.
type Server struct {
	store        Store
	apiKeys      auth.APIKeyValidator
	ingestionSvc *services.IngestionService
	events       []smart_contract.Event
	eventsMu     sync.Mutex
	listenersMu  sync.Mutex
	listeners    []chan smart_contract.Event
	mempool      *bitcoin.MempoolClient
}

// proposalCreateBody captures POST payload for creating proposals.
type ProposalCreateBody struct {
	ID               string                 `json:"id"`
	IngestionID      string                 `json:"ingestion_id"`
	ContractID       string                 `json:"contract_id"`
	Title            string                 `json:"title"`
	DescriptionMD    string                 `json:"description_md"`
	VisiblePixelHash string                 `json:"visible_pixel_hash"`
	BudgetSats       int64                  `json:"budget_sats"`
	Status           string                 `json:"status"`
	Metadata         map[string]interface{} `json:"metadata"`
	Tasks            []smart_contract.Task  `json:"tasks"`
}

// ProposalUpdateBody captures PATCH/PUT payload for updating proposals.
type ProposalUpdateBody struct {
	Title            *string                 `json:"title"`
	DescriptionMD    *string                 `json:"description_md"`
	VisiblePixelHash *string                 `json:"visible_pixel_hash"`
	BudgetSats       *int64                  `json:"budget_sats"`
	ContractID       *string                 `json:"contract_id"`
	Metadata         *map[string]interface{} `json:"metadata"`
	Tasks            *[]smart_contract.Task  `json:"tasks"`
}

// NewServer builds a Server with the given store.
func NewServer(store Store, apiKeys auth.APIKeyValidator, ingest *services.IngestionService) *Server {
	return &Server{
		store:        store,
		apiKeys:      apiKeys,
		ingestionSvc: ingest,
		mempool:      bitcoin.NewMempoolClient(),
	}
}

// RegisterRoutes attaches handlers to the mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/smart_contract/contracts", s.authWrap(s.handleContracts))
	mux.HandleFunc("/api/smart_contract/contracts/", s.authWrap(s.handleContracts))
	mux.HandleFunc("/api/smart_contract/tasks", s.authWrap(s.handleTasks))
	mux.HandleFunc("/api/smart_contract/tasks/", s.authWrap(s.handleTasks))
	mux.HandleFunc("/api/smart_contract/claims/", s.authWrap(s.handleClaims))
	mux.HandleFunc("/api/smart_contract/skills", s.authWrap(s.handleSkills))
	mux.HandleFunc("/api/smart_contract/discover", s.authWrap(s.handleDiscover))
	mux.HandleFunc("/api/smart_contract/proposals", s.authWrap(s.handleProposals))
	mux.HandleFunc("/api/smart_contract/proposals/", s.authWrap(s.handleProposals))
	mux.HandleFunc("/api/smart_contract/submissions", s.authWrap(s.handleSubmissions))
	mux.HandleFunc("/api/smart_contract/submissions/", s.authWrap(s.handleSubmissions))
	mux.HandleFunc("/api/smart_contract/events", s.authWrap(s.handleEvents))
	mux.HandleFunc("/api/smart_contract/config", s.authWrap(s.handleConfig))
}

func (s *Server) authWrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.apiKeys != nil {
			key := r.Header.Get("X-API-Key")
			if key == "" || !s.apiKeys.Validate(key) {
				Error(w, http.StatusForbidden, "invalid api key")
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	JSON(w, http.StatusOK, map[string]string{
		"donation_address": strings.TrimSpace(os.Getenv("STARLIGHT_DONATION_ADDRESS")),
	})
}

func (s *Server) handleContracts(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/contracts")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" || path == "/" {
			status := r.URL.Query().Get("status")
			skills := splitCSV(r.URL.Query().Get("skills"))
			filter := smart_contract.ContractFilter{
				Status:       status,
				Skills:       skills,
				Creator:      r.URL.Query().Get("creator"),
				AiIdentifier: r.URL.Query().Get("ai_identifier"),
			}
			contracts, err := s.store.ListContracts(filter)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			JSON(w, http.StatusOK, map[string]interface{}{
				"contracts":   contracts,
				"total_count": len(contracts),
			})
			return
		}

		contractID := parts[0]
		if len(parts) > 1 && parts[1] == "funding" {
			contract, proofs, err := s.store.ContractFunding(contractID)
			if err != nil {
				Error(w, http.StatusNotFound, err.Error())
				return
			}
			JSON(w, http.StatusOK, map[string]interface{}{
				"contract": contract,
				"proofs":   proofs,
			})
			return
		}

		if len(parts) > 1 && parts[1] == "payment-details" {
			s.handlePaymentDetails(w, r, contractID)
			return
		}

		contract, err := s.store.GetContract(contractID)
		if err != nil {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		JSON(w, http.StatusOK, contract)
	case http.MethodPost:
		if len(parts) > 1 && parts[1] == "psbt" {
			contractID := parts[0]
			s.handleContractPSBT(w, r, contractID)
			return
		}
		if len(parts) > 1 && parts[1] == "commitment-psbt" {
			contractID := parts[0]
			s.handleCommitmentPSBT(w, r, contractID)
			return
		}
		if len(parts) > 1 && parts[1] == "payment-details" {
			contractID := parts[0]
			s.handlePaymentDetails(w, r, contractID)
			return
		}
		Error(w, http.StatusNotFound, "unknown contract action")
	default:
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

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
		CommitmentSats   int64    `json:"commitment_sats"`
		FeeRate          int64    `json:"fee_rate_sats_vb"`
		UsePixelHash     *bool    `json:"use_pixel_hash"`
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

	fundingMode, fundingAddress := s.resolveFundingMode(r.Context(), contractID)
	primaryPayer := payerAddr
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
		if s.store == nil {
			Error(w, http.StatusBadRequest, "task store unavailable for raise_fund")
			return
		}
		tasks, err := s.store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
		if err != nil {
			Error(w, http.StatusBadRequest, fmt.Sprintf("failed to load tasks: %v", err))
			return
		}
		if len(tasks) == 0 {
			Error(w, http.StatusBadRequest, "no tasks available for raise_fund")
			return
		}
		if strings.TrimSpace(fundingAddress) == "" {
			Error(w, http.StatusBadRequest, "missing fundraiser payout address")
			return
		}
		fundAddr, err := btcutil.DecodeAddress(strings.TrimSpace(fundingAddress), params)
		if err != nil {
			Error(w, http.StatusBadRequest, fmt.Sprintf("invalid fundraiser payout address: %v", err))
			return
		}
		payerTotals := make(map[string]int64)
		raiseFundPayoutsByPayer = make(map[string][]bitcoin.PayoutOutput)
		raiseFundPayersByWallet = make(map[string]bitcoin.PayerTarget)
		raiseFundTasksByWallet = make(map[string][]string)
		var payerOrder []string
		var payoutTotal int64
		for _, task := range tasks {
			if task.BudgetSats <= 0 {
				Error(w, http.StatusBadRequest, fmt.Sprintf("task budget missing for %s", task.TaskID))
				return
			}
			payoutTotal += task.BudgetSats
			raiseFundTaskIDs = append(raiseFundTaskIDs, task.TaskID)
			payout := bitcoin.PayoutOutput{
				Address:   fundAddr,
				ValueSats: task.BudgetSats,
			}
			raiseFundPayouts = append(raiseFundPayouts, payout)
			taskWallet := strings.TrimSpace(task.ContractorWallet)
			if taskWallet == "" && task.MerkleProof != nil {
				taskWallet = strings.TrimSpace(task.MerkleProof.ContractorWallet)
			}
			if taskWallet == "" {
				Error(w, http.StatusBadRequest, fmt.Sprintf("missing contractor wallet for task %s", task.TaskID))
				return
			}
			if _, ok := payerTotals[taskWallet]; !ok {
				payerOrder = append(payerOrder, taskWallet)
			}
			payerTotals[taskWallet] += task.BudgetSats
			raiseFundPayoutsByPayer[taskWallet] = append(raiseFundPayoutsByPayer[taskWallet], payout)
			raiseFundTasksByWallet[taskWallet] = append(raiseFundTasksByWallet[taskWallet], task.TaskID)
		}
		for _, wallet := range payerOrder {
			addr, err := btcutil.DecodeAddress(wallet, params)
			if err != nil {
				Error(w, http.StatusBadRequest, fmt.Sprintf("invalid contractor wallet: %v", err))
				return
			}
			payerTarget := bitcoin.PayerTarget{
				Address:    addr,
				TargetSats: payerTotals[wallet],
			}
			raiseFundPayers = append(raiseFundPayers, payerTarget)
			raiseFundPayerAddrs = append(raiseFundPayerAddrs, addr)
			raiseFundPayersByWallet[wallet] = payerTarget
		}
		if len(raiseFundPayers) == 0 {
			Error(w, http.StatusBadRequest, "no contractor wallets found for raise_fund")
			return
		}
		target = payoutTotal
		raiseFundPayerOrder = payerOrder
		raiseFundPayerTotals = payerTotals
		payerAddresses = raiseFundPayerAddrs
		primaryPayer = raiseFundPayerAddrs[0]
		changeAddr = nil
	}
	if !isRaiseFund(fundingMode) && len(payerAddresses) > 1 && changeAddr == nil {
		Error(w, http.StatusBadRequest, "change_address required when using multiple payer addresses")
		return
	}

	normalizePixel := func(b []byte) []byte {
		if l := len(b); l == 20 || l == 32 {
			return b
		}
		return nil
	}
	var pixelBytes []byte
	usePixelHash := true
	if body.UsePixelHash != nil {
		usePixelHash = *body.UsePixelHash
	}
	var ingestionRec *services.IngestionRecord
	if s.ingestionSvc != nil {
		if rec, err := s.ingestionSvc.Get(contractID); err == nil {
			ingestionRec = rec
		}
	}
	if usePixelHash {
		if ph := strings.TrimSpace(body.PixelHash); ph != "" {
			if b, err := hex.DecodeString(ph); err == nil {
				pixelBytes = normalizePixel(b)
			}
		}
		if pixelBytes == nil && ingestionRec != nil {
			pixelBytes = resolvePixelHashFromIngestion(ingestionRec, normalizePixel)
		}
	}
	if usePixelHash && pixelBytes == nil {
		if h, err := hex.DecodeString(strings.TrimSpace(contractID)); err == nil {
			pixelBytes = normalizePixel(h)
		}
	}
	if usePixelHash && pixelBytes == nil {
		Error(w, http.StatusBadRequest, "missing 32-byte pixel hash for commitment output")
		return
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
		var payoutScripts [][]byte
		var payoutScriptHashes []string
		var payoutScriptHash160s []string
		for _, wallet := range raiseFundPayerOrder {
			target := raiseFundPayerTotals[wallet]
			payerTarget := raiseFundPayersByWallet[wallet]
			payerPayouts := raiseFundPayoutsByPayer[wallet]
			psbtReq := bitcoin.PSBTRequest{
				PayerAddress:    payerTarget.Address,
				TargetValueSats: target,
				PixelHash:       pixelBytes,
				CommitmentSats:  body.CommitmentSats,
				Payouts:         payerPayouts,
				FeeRateSatPerVB: body.FeeRate,
			}
			splitRes, err := bitcoin.BuildFundingPSBT(s.mempool, params, psbtReq)
			if err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			if splitRes.FundingTxID != "" {
				fundingTxIDs = append(fundingTxIDs, splitRes.FundingTxID)
			}
			if len(splitRes.PayoutScripts) > 0 {
				payoutScripts = append(payoutScripts, splitRes.PayoutScripts...)
				shaHashes, hash160s := buildScriptHashes(splitRes.PayoutScripts)
				payoutScriptHashes = append(payoutScriptHashes, shaHashes...)
				payoutScriptHash160s = append(payoutScriptHash160s, hash160s...)
			}
			if len(raiseFundTasksByWallet[wallet]) > 0 {
				for _, taskID := range raiseFundTasksByWallet[wallet] {
					if err := s.updateTaskCommitmentProof(r.Context(), taskID, splitRes, pixelBytes); err != nil {
						log.Printf("psbt: failed to update task proof for %s: %v", taskID, err)
					}
				}
			}
			psbtEntries = append(psbtEntries, map[string]interface{}{
				"psbt":               splitRes.EncodedHex,
				"psbt_hex":           splitRes.EncodedHex,
				"psbt_base64":        splitRes.EncodedBase64,
				"funding_txid":       splitRes.FundingTxID,
				"fee_sats":           splitRes.FeeSats,
				"change_sats":        splitRes.ChangeSats,
				"selected_sats":      splitRes.SelectedSats,
				"payout_script":      hex.EncodeToString(splitRes.PayoutScript),
				"payout_scripts":     hexSlice(splitRes.PayoutScripts),
				"payout_amounts":     splitRes.PayoutAmounts,
				"commitment_script":  hex.EncodeToString(splitRes.CommitmentScript),
				"commitment_sats":    splitRes.CommitmentSats,
				"commitment_vout":    splitRes.CommitmentVout,
				"redeem_script":      hex.EncodeToString(splitRes.RedeemScript),
				"redeem_script_hash": hex.EncodeToString(splitRes.RedeemScriptHash),
				"commitment_address": splitRes.CommitmentAddr,
				"pixel_hash":         strings.TrimSpace(body.PixelHash),
				"payer_address":      payerTarget.Address.EncodeAddress(),
				"payer_addresses":    []string{payerTarget.Address.EncodeAddress()},
				"change_address":     "",
				"change_addresses":   splitRes.ChangeAddresses,
				"change_amounts":     splitRes.ChangeAmounts,
				"funding_mode":       fundingMode,
				"contract_id":        contractID,
				"pixel_source":       pixelSourceForBytes(pixelBytes),
				"budget_sats":        target,
				"contractor":         "",
				"network_params":     params.Name,
			})
		}
		if ingestionRec != nil && len(fundingTxIDs) > 0 {
			metadata := map[string]interface{}{
				"funding_txids":          fundingTxIDs,
				"funding_txid":           fundingTxIDs[0],
				"payout_scripts":         hexSlice(payoutScripts),
				"payout_script_hashes":   payoutScriptHashes,
				"payout_script_hash160s": payoutScriptHash160s,
			}
			if err := s.ingestionSvc.UpdateMetadata(ingestionRec.ID, metadata); err != nil {
				log.Printf("psbt: failed to store funding_txids for %s: %v", ingestionRec.ID, err)
			}
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
			body.CommitmentSats,
			body.FeeRate,
		)
	} else {
		psbtReq := bitcoin.PSBTRequest{
			PayerAddress:      primaryPayer,
			PayerAddresses:    payerAddresses,
			TargetValueSats:   target,
			PixelHash:         pixelBytes,
			CommitmentSats:    body.CommitmentSats,
			ContractorAddress: contractorAddr,
			Payouts:           payouts,
			FeeRateSatPerVB:   body.FeeRate,
			ChangeAddress:     changeAddr,
			UseAllPayers:      isRaiseFund(fundingMode),
		}
		res, err = bitcoin.BuildFundingPSBT(s.mempool, params, psbtReq)
	}
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if ingestionRec != nil && res.FundingTxID != "" {
		scriptHashes, scriptHash160s := buildScriptHashes(res.PayoutScripts)
		if err := s.ingestionSvc.UpdateMetadata(ingestionRec.ID, map[string]interface{}{
			"funding_txid":           res.FundingTxID,
			"payout_scripts":         hexSlice(res.PayoutScripts),
			"payout_script_hashes":   scriptHashes,
			"payout_script_hash160s": scriptHash160s,
		}); err != nil {
			log.Printf("psbt: failed to store funding_txid for %s: %v", ingestionRec.ID, err)
		}
	}
	if taskID := strings.TrimSpace(body.TaskID); taskID != "" {
		if err := s.updateTaskCommitmentProof(r.Context(), taskID, res, pixelBytes); err != nil {
			log.Printf("psbt: failed to update task proof for %s: %v", taskID, err)
		}
	} else if isRaiseFund(fundingMode) && len(raiseFundTaskIDs) > 0 {
		for _, taskID := range raiseFundTaskIDs {
			if err := s.updateTaskCommitmentProof(r.Context(), taskID, res, pixelBytes); err != nil {
				log.Printf("psbt: failed to update task proof for %s: %v", taskID, err)
			}
		}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"psbt":               res.EncodedHex, // primary: hex for wallet import
		"psbt_hex":           res.EncodedHex,
		"psbt_base64":        res.EncodedBase64,
		"funding_txid":       res.FundingTxID,
		"fee_sats":           res.FeeSats,
		"change_sats":        res.ChangeSats,
		"selected_sats":      res.SelectedSats,
		"payout_script":      hex.EncodeToString(res.PayoutScript),
		"payout_scripts":     hexSlice(res.PayoutScripts),
		"payout_amounts":     res.PayoutAmounts,
		"commitment_script":  hex.EncodeToString(res.CommitmentScript),
		"commitment_sats":    res.CommitmentSats,
		"commitment_vout":    res.CommitmentVout,
		"redeem_script":      hex.EncodeToString(res.RedeemScript),
		"redeem_script_hash": hex.EncodeToString(res.RedeemScriptHash),
		"commitment_address": res.CommitmentAddr,
		"pixel_hash":         strings.TrimSpace(body.PixelHash),
		"payer_address":      primaryPayer.EncodeAddress(),
		"payer_addresses":    addressSlice(payerAddresses),
		"change_address":     addressOrEmpty(changeAddr),
		"change_addresses":   res.ChangeAddresses,
		"change_amounts":     res.ChangeAmounts,
		"funding_mode":       fundingMode,
		"contract_id":        contractID,
		"pixel_source":       pixelSourceForBytes(pixelBytes),
		"budget_sats":        target,
		"contractor":         contractorAddressFor(contractorAddr),
		"network_params":     params.Name,
	})
}

func contractorAddressFor(addr btcutil.Address) string {
	if addr == nil {
		return ""
	}
	return addr.EncodeAddress()
}

func (s *Server) resolveFundingMode(ctx context.Context, contractID string) (string, string) {
	var meta map[string]interface{}
	var proposal *smart_contract.Proposal
	if s.store != nil {
		if stored, err := s.store.GetProposal(ctx, contractID); err == nil {
			proposal = &stored
			meta = stored.Metadata
		} else if proposals, err := s.store.ListProposals(ctx, smart_contract.ProposalFilter{ContractID: contractID}); err == nil && len(proposals) > 0 {
			proposal = &proposals[0]
			meta = proposals[0].Metadata
		}
	}
	if meta == nil && s.ingestionSvc != nil {
		if rec, err := s.ingestionSvc.Get(contractID); err == nil && rec != nil {
			meta = rec.Metadata
		}
	}
	mode := strings.ToLower(strings.TrimSpace(toString(meta["funding_mode"])))
	if mode == "" && proposal != nil {
		if looksLikeRaiseFund(proposal.Title) || looksLikeRaiseFund(proposal.DescriptionMD) {
			mode = "raise_fund"
		}
	}
	return mode, fundingAddressFromMeta(meta)
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

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/tasks")
	path = strings.Trim(path, "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" {
			filter := smart_contract.TaskFilter{
				Skills:        splitCSV(r.URL.Query().Get("skills")),
				MaxDifficulty: r.URL.Query().Get("max_difficulty"),
				Status:        r.URL.Query().Get("status"),
				Limit:         intFromQuery(r, "limit", 50),
				Offset:        intFromQuery(r, "offset", 0),
				MinBudgetSats: int64FromQuery(r, "min_budget_sats", 0),
				ContractID:    r.URL.Query().Get("contract_id"),
				ClaimedBy:     r.URL.Query().Get("claimed_by"),
			}
			tasks, err := s.store.ListTasks(filter)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			// hydrate submissions for these tasks
			var taskIDs []string
			for _, t := range tasks {
				taskIDs = append(taskIDs, t.TaskID)
			}
			subs, _ := s.store.ListSubmissions(r.Context(), taskIDs)
			JSON(w, http.StatusOK, map[string]interface{}{
				"tasks":         tasks,
				"total_matches": len(tasks),
				"submissions":   subs,
			})
			return
		}

		parts := strings.Split(path, "/")
		taskID := parts[0]

		// Nested resources
		if len(parts) > 1 && parts[1] == "merkle-proof" {
			proof, err := s.store.GetTaskProof(taskID)
			if err != nil {
				Error(w, http.StatusNotFound, err.Error())
				return
			}
			JSON(w, http.StatusOK, proof)
			return
		}

		if len(parts) > 1 && parts[1] == "status" {
			status, err := s.store.TaskStatus(taskID)
			if err != nil {
				Error(w, http.StatusNotFound, err.Error())
				return
			}
			JSON(w, http.StatusOK, status)
			return
		}

		task, err := s.store.GetTask(taskID)
		if err != nil {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		JSON(w, http.StatusOK, task)
	case http.MethodPost:
		parts := strings.Split(path, "/")
		if len(parts) < 2 {
			Error(w, http.StatusBadRequest, "expected /tasks/{task_id}/claim")
			return
		}
		taskID := parts[0]
		switch parts[1] {
		case "claim":
			s.handleClaimTask(w, r, taskID)
		default:
			Error(w, http.StatusNotFound, "unknown task action")
		}
	default:
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func resolvePixelHashFromIngestion(rec *services.IngestionRecord, normalize func([]byte) []byte) []byte {
	if rec == nil {
		return nil
	}

	for _, key := range []string{"pixel_hash", "payout_script_hash", "visible_pixel_hash"} {
		if v, ok := rec.Metadata[key].(string); ok {
			if b, err := hex.DecodeString(strings.TrimSpace(v)); err == nil {
				if normalized := normalize(b); normalized != nil {
					return normalized
				}
			}
		}
	}

	message := ""
	if v, ok := rec.Metadata["embedded_message"].(string); ok {
		message = v
	}
	if message == "" {
		if v, ok := rec.Metadata["message"].(string); ok {
			message = v
		}
	}
	if rec.ImageBase64 == "" {
		return nil
	}
	imageBytes, err := base64.StdEncoding.DecodeString(rec.ImageBase64)
	if err != nil {
		return nil
	}

	if message != "" {
		sum := sha256.Sum256(append(imageBytes, []byte(message)...))
		return normalize(sum[:])
	}

	sum := sha256.Sum256(imageBytes)
	return normalize(sum[:])
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

func (s *Server) updateTaskCommitmentProof(ctx context.Context, taskID string, res *bitcoin.PSBTResult, pixelBytes []byte) error {
	task, err := s.store.GetTask(taskID)
	if err != nil {
		return err
	}
	proof := task.MerkleProof
	if proof == nil {
		proof = &smart_contract.MerkleProof{}
	}
	if len(pixelBytes) == 32 {
		proof.VisiblePixelHash = hex.EncodeToString(pixelBytes)
	}
	if res.FundingTxID != "" {
		proof.TxID = res.FundingTxID
	}
	if proof.ConfirmationStatus == "" {
		proof.ConfirmationStatus = "provisional"
	}
	if proof.SeenAt.IsZero() {
		proof.SeenAt = time.Now()
	}
	if len(res.RedeemScript) > 0 {
		proof.CommitmentRedeemScript = hex.EncodeToString(res.RedeemScript)
	}
	if len(res.RedeemScriptHash) > 0 {
		proof.CommitmentRedeemHash = hex.EncodeToString(res.RedeemScriptHash)
	}
	if res.CommitmentAddr != "" {
		proof.CommitmentAddress = res.CommitmentAddr
	}
	if res.CommitmentVout > 0 {
		proof.CommitmentVout = res.CommitmentVout
	}
	if res.CommitmentSats > 0 {
		proof.CommitmentSats = res.CommitmentSats
	}
	return s.store.UpdateTaskProof(ctx, taskID, proof)
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
		preimageHex = strings.TrimSpace(proof.VisiblePixelHash)
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

func (s *Server) handleClaimTask(w http.ResponseWriter, r *http.Request, taskID string) {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "application/json") {
		Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	var body struct {
		AiIdentifier        string     `json:"ai_identifier"`
		Wallet              string     `json:"wallet_address,omitempty"`
		EstimatedCompletion *time.Time `json:"estimated_completion,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.AiIdentifier == "" {
		Error(w, http.StatusBadRequest, "ai_identifier required")
		return
	}

	contractorWallet := strings.TrimSpace(body.Wallet)
	if s.apiKeys != nil {
		key := r.Header.Get("X-API-Key")
		if contractorWallet != "" {
			if updater, ok := s.apiKeys.(auth.APIKeyWalletUpdater); ok {
				if _, err := updater.UpdateWallet(key, contractorWallet); err != nil {
					Error(w, http.StatusBadRequest, "failed to bind wallet to api key")
					return
				}
			}
		}
		if rec, ok := s.apiKeys.Get(key); ok && strings.TrimSpace(rec.Wallet) != "" {
			contractorWallet = strings.TrimSpace(rec.Wallet)
		}
	}

	claim, err := s.store.ClaimTask(taskID, body.AiIdentifier, contractorWallet, body.EstimatedCompletion)
	if err != nil {
		if err == ErrTaskNotFound {
			// Attempt to publish tasks lazily from proposals that reference this task id, then retry.
			if s.tryPublishTasksForTaskID(r.Context(), taskID) == nil {
				if retry, retryErr := s.store.ClaimTask(taskID, body.AiIdentifier, contractorWallet, body.EstimatedCompletion); retryErr == nil {
					claim = retry
					err = nil
				} else {
					err = retryErr
				}
			}
			if err == nil {
				goto claim_success
			}
			if err == ErrTaskNotFound {
				Error(w, http.StatusNotFound, err.Error())
				return
			}
		}
		if err == ErrTaskTaken || err == ErrTaskUnavailable || err.Error() == ErrTaskUnavailable.Error() {
			Error(w, http.StatusConflict, err.Error())
			return
		}
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
claim_success:

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"claim_id":   claim.ClaimID,
		"expires_at": claim.ExpiresAt,
		"message":    "Task reserved. Submit work before expiration.",
	})

	s.recordEvent(smart_contract.Event{
		Type:      "claim",
		EntityID:  taskID,
		Actor:     body.AiIdentifier,
		Message:   "task claimed",
		CreatedAt: time.Now(),
	})
}

func (s *Server) handleClaims(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/claims/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		Error(w, http.StatusBadRequest, "claim id required")
		return
	}
	claimID := parts[0]

	if len(parts) < 2 || parts[1] != "submit" {
		Error(w, http.StatusNotFound, "unknown claim action")
		return
	}

	if r.Method != http.MethodPost {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "application/json") {
		Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}

	var body struct {
		Deliverables    map[string]interface{} `json:"deliverables"`
		CompletionProof map[string]interface{} `json:"completion_proof"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}

	sub, err := s.store.SubmitWork(claimID, body.Deliverables, body.CompletionProof)
	if err != nil {
		if err == ErrClaimNotFound {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	actor := "claimant"
	if who, ok := body.Deliverables["submitted_by"].(string); ok && who != "" {
		actor = who
	}
	s.recordEvent(smart_contract.Event{
		Type:      "submit",
		EntityID:  claimID,
		Actor:     actor,
		Message:   "submission created",
		CreatedAt: time.Now(),
	})

	JSON(w, http.StatusOK, sub)
}

// handleSkills returns a unique list of skills across all tasks for quick capability checks by agents.
func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tasks, err := s.store.ListTasks(smart_contract.TaskFilter{})
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	skillSet := make(map[string]struct{})
	// Add default skills
	skillSet["contract_bidding"] = struct{}{}
	skillSet["get_open_contracts"] = struct{}{}

	for _, t := range tasks {
		for _, skill := range t.Skills {
			key := strings.ToLower(strings.TrimSpace(skill))
			if key == "" {
				continue
			}
			skillSet[key] = struct{}{}
		}
	}
	skills := make([]string, 0, len(skillSet))
	for k := range skillSet {
		skills = append(skills, k)
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"skills": skills,
		"count":  len(skills),
	})
}

// handleDiscover advertises API endpoints and MCP tool surface for clients.
func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	base := fmt.Sprintf("http://%s", r.Host)
	resp := map[string]interface{}{
		"version": "1.0",
		"base_urls": map[string]string{
			"api": base + "/api/smart_contract",
			"mcp": base + "/mcp",
		},
		"endpoints": []string{
			"/api/smart_contract/contracts",
			"/api/smart_contract/tasks",
			"/api/smart_contract/claims",
			"/api/smart_contract/submissions",
			"/api/smart_contract/events",
			"/api/open-contracts",
		},
		"tools": []string{
			"list_contracts", "get_contract", "get_contract_funding", "get_open_contracts",
			"list_tasks", "get_task", "claim_task", "submit_work", "get_task_proof", "get_task_status",
			"list_skills",
			"list_proposals", "get_proposal", "create_proposal", "approve_proposal", "publish_proposal",
			"list_submissions", "get_submission", "review_submission", "rework_submission",
			"list_events",
			"scan_image", "scan_block", "extract_message", "get_scanner_info",
		},
		"authentication": map[string]string{
			"type":        "api_key",
			"header_name": "X-API-Key",
			"required":    fmt.Sprintf("%t", s.apiKeys != nil),
		},
		"rate_limits": map[string]interface{}{
			"enabled":       false,
			"notes":         "rate limiting planned; not enforced by default",
			"recommended":   "10 rps claim, 5 rps submit (see roadmap)",
			"burst_example": 100,
		},
	}
	JSON(w, http.StatusOK, resp)
}

func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func intFromQuery(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}

func int64FromQuery(r *http.Request, key string, def int64) int64 {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return def
	}
	return v
}

func includeConfirmed(r *http.Request) bool {
	raw := strings.TrimSpace(r.URL.Query().Get("include_confirmed"))
	if raw == "" {
		return false
	}
	return strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes") || raw == "1"
}

func proposalMetaConfirmed(meta map[string]interface{}) bool {
	if meta == nil {
		return false
	}
	if txid, ok := meta["confirmed_txid"].(string); ok && strings.TrimSpace(txid) != "" {
		return true
	}
	if status, ok := meta["confirmation_status"].(string); ok && strings.EqualFold(strings.TrimSpace(status), "confirmed") {
		return true
	}
	if height, ok := meta["confirmed_height"].(float64); ok && height > 0 {
		return true
	}
	return false
}

// recordEvent appends an event to the in-memory log with a small bounded buffer.
func (s *Server) recordEvent(evt smart_contract.Event) {
	const maxEvents = 200
	if evt.CreatedAt.IsZero() {
		evt.CreatedAt = time.Now()
	}
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	s.events = append([]smart_contract.Event{evt}, s.events...)
	if len(s.events) > maxEvents {
		s.events = s.events[:maxEvents]
	}
	s.broadcastEvent(evt)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	filterType := strings.TrimSpace(r.URL.Query().Get("type"))
	filterActor := strings.TrimSpace(r.URL.Query().Get("actor"))
	filterEntity := strings.TrimSpace(r.URL.Query().Get("entity_id"))

	// SSE support
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		flusher, ok := w.(http.Flusher)
		if !ok {
			Error(w, http.StatusInternalServerError, "streaming unsupported")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Send recent buffer first
		s.eventsMu.Lock()
		initial := make([]smart_contract.Event, len(s.events))
		copy(initial, s.events)
		s.eventsMu.Unlock()
		for i := len(initial) - 1; i >= 0; i-- { // oldest first
			if !eventMatches(initial[i], filterType, filterActor, filterEntity) {
				continue
			}
			b, _ := json.Marshal(initial[i])
			w.Write([]byte("event: mcp\n"))
			w.Write([]byte("data: " + string(b) + "\n\n"))
		}
		flusher.Flush()

		ch := make(chan smart_contract.Event, 10)
		s.listenersMu.Lock()
		s.listeners = append(s.listeners, ch)
		s.listenersMu.Unlock()

		notify := r.Context().Done()
		for {
			select {
			case <-notify:
				s.removeListener(ch)
				return
			case evt := <-ch:
				if !eventMatches(evt, filterType, filterActor, filterEntity) {
					continue
				}
				b, _ := json.Marshal(evt)
				w.Write([]byte("event: mcp\n"))
				w.Write([]byte("data: " + string(b) + "\n\n"))
				flusher.Flush()
			}
		}
	}

	limit := intFromQuery(r, "limit", 50)
	if limit < 0 {
		limit = 0
	}
	s.eventsMu.Lock()
	events := make([]smart_contract.Event, len(s.events))
	copy(events, s.events)
	s.eventsMu.Unlock()
	filtered := make([]smart_contract.Event, 0, len(events))
	for _, evt := range events {
		if eventMatches(evt, filterType, filterActor, filterEntity) {
			filtered = append(filtered, evt)
		}
	}
	if limit > 0 && limit < len(filtered) {
		filtered = filtered[:limit]
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"events": filtered,
		"total":  len(filtered),
	})
}

// broadcastEvent pushes an event to connected listeners without blocking.
func (s *Server) broadcastEvent(evt smart_contract.Event) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	for _, ch := range s.listeners {
		select {
		case ch <- evt:
		default:
			// drop if slow consumer
		}
	}
}

// tryPublishTasksForTaskID attempts to find a proposal that contains the given taskID and publish its tasks.
func (s *Server) tryPublishTasksForTaskID(ctx context.Context, taskID string) error {
	proposals, err := s.store.ListProposals(ctx, smart_contract.ProposalFilter{})
	if err != nil {
		return err
	}
	for _, p := range proposals {
		for _, t := range p.Tasks {
			if t.TaskID == taskID {
				return s.publishProposalTasks(ctx, p.ID)
			}
		}
	}
	return ErrTaskNotFound
}

func (s *Server) removeListener(ch chan smart_contract.Event) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	for i, c := range s.listeners {
		if c == ch {
			close(c)
			s.listeners = append(s.listeners[:i], s.listeners[i+1:]...)
			break
		}
	}
}

func eventMatches(evt smart_contract.Event, t string, actor string, entity string) bool {
	if t != "" && !strings.EqualFold(evt.Type, t) {
		return false
	}
	if actor != "" && !strings.EqualFold(evt.Actor, actor) {
		return false
	}
	if entity != "" && evt.EntityID != entity {
		return false
	}
	return true
}

// publishProposalTasks publishes the tasks stored in a proposal into MCP tasks.
func (s *Server) publishProposalTasks(ctx context.Context, proposalID string) error {
	p, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return err
	}
	if len(p.Tasks) == 0 {
		// Try to derive tasks from metadata embedded_message.
		if em, ok := p.Metadata["embedded_message"].(string); ok && em != "" {
			p.Tasks = scstore.BuildTasksFromMarkdown(p.ID, em, p.VisiblePixelHash, p.BudgetSats, scstore.FundingAddressFromMeta(p.Metadata))
		}
		if len(p.Tasks) == 0 {
			return nil
		}
	}
	// Build a contract from the proposal, then upsert tasks.
	contract := smart_contract.Contract{
		ContractID:          p.ID,
		Title:               p.Title,
		TotalBudgetSats:     p.BudgetSats,
		GoalsCount:          1,
		AvailableTasksCount: len(p.Tasks),
		Status:              "active",
	}
	// Preserve hashes/funding if present.
	fundingAddr := scstore.FundingAddressFromMeta(p.Metadata)
	tasks := make([]smart_contract.Task, 0, len(p.Tasks))
	for _, t := range p.Tasks {
		task := t
		if task.ContractID == "" {
			task.ContractID = p.ID
		}
		if task.MerkleProof == nil && p.VisiblePixelHash != "" {
			task.MerkleProof = &smart_contract.MerkleProof{
				VisiblePixelHash:   p.VisiblePixelHash,
				FundedAmountSats:   p.BudgetSats / int64(len(p.Tasks)),
				FundingAddress:     fundingAddr,
				ConfirmationStatus: "provisional",
			}
		}
		if task.MerkleProof != nil && task.MerkleProof.FundingAddress == "" {
			task.MerkleProof.FundingAddress = fundingAddr
		}
		tasks = append(tasks, task)
	}
	if pg, ok := s.store.(interface {
		UpsertContractWithTasks(context.Context, smart_contract.Contract, []smart_contract.Task) error
	}); ok {
		if err := pg.UpsertContractWithTasks(ctx, contract, tasks); err != nil {
			return err
		}
		s.recordEvent(smart_contract.Event{
			Type:      "publish",
			EntityID:  proposalID,
			Actor:     "system",
			Message:   "proposal tasks published",
			CreatedAt: time.Now(),
		})
		return nil
	}
	return nil
}

// handleProposals supports listing, getting, and approving proposals.
func (s *Server) handleProposals(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/proposals")
	path = strings.Trim(path, "/")

	switch r.Method {
	case http.MethodPost:
		// POST /mcp/v1/proposals/{id}/approve is handled separately.
		parts := strings.Split(path, "/")
		if len(parts) == 2 && parts[1] == "approve" {
			id := parts[0]
			proposal, err := s.store.GetProposal(r.Context(), id)
			if err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			meta := proposal.Metadata
			if meta == nil {
				meta = map[string]interface{}{}
			}
			fundingMode := strings.ToLower(strings.TrimSpace(toString(meta["funding_mode"])))
			if fundingMode == "" && (looksLikeRaiseFund(proposal.Title) || looksLikeRaiseFund(proposal.DescriptionMD)) {
				fundingMode = "raise_fund"
				meta["funding_mode"] = fundingMode
			}
			if isRaiseFund(fundingMode) {
				payoutAddr := strings.TrimSpace(toString(meta["payout_address"]))
				fundingAddr := strings.TrimSpace(toString(meta["funding_address"]))
				if payoutAddr == "" || fundingAddr == "" {
					if s.apiKeys == nil {
						Error(w, http.StatusBadRequest, "missing payout address; API key wallet binding required for fundraiser")
						return
					}
					apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
					rec, ok := s.apiKeys.Get(apiKey)
					if !ok || strings.TrimSpace(rec.Wallet) == "" {
						Error(w, http.StatusBadRequest, "missing payout address; API key wallet binding required for fundraiser")
						return
					}
					meta["payout_address"] = rec.Wallet
					meta["funding_address"] = rec.Wallet
				}
			}
			proposal.Metadata = meta
			if err := s.store.UpdateProposal(r.Context(), proposal); err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			if err := s.store.ApproveProposal(r.Context(), id); err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			// Publish tasks for this proposal if available.
			if err := s.publishProposalTasks(r.Context(), id); err != nil {
				log.Printf("failed to publish tasks for proposal %s: %v", id, err)
			}
			s.recordEvent(smart_contract.Event{
				Type:      "approve",
				EntityID:  id,
				Actor:     "approver",
				Message:   "proposal approved",
				CreatedAt: time.Now(),
			})
			JSON(w, http.StatusOK, map[string]interface{}{
				"proposal_id": id,
				"status":      "approved",
				"message":     "Proposal approved; tasks published.",
			})
			return
		}
		if len(parts) == 2 && parts[1] == "publish" {
			id := parts[0]
			if err := s.store.PublishProposal(r.Context(), id); err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			s.recordEvent(smart_contract.Event{
				Type:      "publish",
				EntityID:  id,
				Actor:     "approver",
				Message:   "proposal published",
				CreatedAt: time.Now(),
			})
			JSON(w, http.StatusOK, map[string]interface{}{
				"proposal_id": id,
				"status":      "published",
				"message":     "Proposal published.",
			})
			return
		}

		// Create a proposal, optionally derived from a pending ingestion.
		if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "application/json") {
			Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}
		var body ProposalCreateBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			Error(w, http.StatusBadRequest, "invalid json")
			return
		}

		// If an ingestion_id is provided, pull message/token/budget from that pending record.
		if body.IngestionID != "" && s.ingestionSvc != nil {
			rec, err := s.ingestionSvc.Get(body.IngestionID)
			if err != nil {
				Error(w, http.StatusNotFound, "ingestion not found")
				return
			}
			proposal, err := BuildProposalFromIngestion(body, rec)
			if err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			metaContractID, _ := proposal.Metadata["contract_id"].(string)
			metaVisiblePixelHash, _ := proposal.Metadata["visible_pixel_hash"].(string)
			if strings.TrimSpace(metaContractID) == "" || strings.TrimSpace(metaVisiblePixelHash) == "" {
				Error(w, http.StatusBadRequest, "contract_id and visible_pixel_hash are required for proposal creation so the UI can display it; set both to the same 64-char hash if needed")
				return
			}
			if err := s.store.CreateProposal(r.Context(), proposal); err != nil {
				Error(w, http.StatusBadRequest, err.Error())
				return
			}
			JSON(w, http.StatusCreated, map[string]interface{}{
				"proposal_id": proposal.ID,
				"status":      proposal.Status,
				"message":     "proposal created from pending ingestion",
			})
			return
		}

		// Manual creation path (not tied to ingestion).
		if strings.TrimSpace(body.Title) == "" {
			Error(w, http.StatusBadRequest, "title is required")
			return
		}
		if body.ID == "" {
			body.ID = "proposal-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
		if body.Status == "" {
			body.Status = "pending"
		}
		if body.BudgetSats == 0 {
			body.BudgetSats = scstore.DefaultBudgetSats()
		}
		if body.Metadata == nil {
			body.Metadata = map[string]interface{}{}
		}
		if body.ContractID != "" {
			body.Metadata["contract_id"] = body.ContractID
		}
		if strings.TrimSpace(body.VisiblePixelHash) != "" {
			body.Metadata["visible_pixel_hash"] = body.VisiblePixelHash
		}
		contractID := strings.TrimSpace(body.ContractID)
		if contractID == "" {
			if v, ok := body.Metadata["contract_id"].(string); ok {
				contractID = strings.TrimSpace(v)
			}
		}
		visiblePixelHash := strings.TrimSpace(body.VisiblePixelHash)
		if visiblePixelHash == "" {
			if v, ok := body.Metadata["visible_pixel_hash"].(string); ok {
				visiblePixelHash = strings.TrimSpace(v)
			}
		}
		if contractID == "" || visiblePixelHash == "" {
			Error(w, http.StatusBadRequest, "contract_id and visible_pixel_hash are required for proposal creation so the UI can display it; set both to the same 64-char hash if needed")
			return
		}
		for i := range body.Tasks {
			if body.Tasks[i].TaskID == "" {
				body.Tasks[i].TaskID = body.ID + "-task-" + strconv.Itoa(i+1)
			}
			if body.Tasks[i].ContractID == "" && body.ContractID != "" {
				body.Tasks[i].ContractID = body.ContractID
			}
			if body.Tasks[i].Status == "" {
				body.Tasks[i].Status = "available"
			}
		}
		p := smart_contract.Proposal{
			ID:               body.ID,
			Title:            body.Title,
			DescriptionMD:    body.DescriptionMD,
			VisiblePixelHash: body.VisiblePixelHash,
			BudgetSats:       body.BudgetSats,
			Status:           body.Status,
			CreatedAt:        time.Now(),
			Tasks:            body.Tasks,
			Metadata:         body.Metadata,
		}
		if err := s.store.CreateProposal(r.Context(), p); err != nil {
			Error(w, http.StatusBadRequest, err.Error())
			return
		}
		JSON(w, http.StatusCreated, map[string]interface{}{
			"proposal_id": p.ID,
			"status":      p.Status,
			"tasks":       len(p.Tasks),
			"budget_sats": p.BudgetSats,
		})
		return
	case http.MethodPut, http.MethodPatch:
		parts := strings.Split(path, "/")
		if len(parts) < 1 || parts[0] == "" {
			Error(w, http.StatusBadRequest, "proposal id required")
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "application/json") {
			Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}
		var body ProposalUpdateBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			Error(w, http.StatusBadRequest, "invalid json")
			return
		}
		id := parts[0]
		existing, err := s.store.GetProposal(r.Context(), id)
		if err != nil {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		if !strings.EqualFold(existing.Status, "pending") {
			Error(w, http.StatusBadRequest, fmt.Sprintf("proposal %s must be pending to update, current status: %s", id, existing.Status))
			return
		}
		updated := existing
		changed := false

		if body.Title != nil {
			if strings.TrimSpace(*body.Title) == "" {
				Error(w, http.StatusBadRequest, "title cannot be empty")
				return
			}
			updated.Title = *body.Title
			changed = true
		}
		if body.DescriptionMD != nil {
			updated.DescriptionMD = *body.DescriptionMD
			changed = true
		}
		if body.VisiblePixelHash != nil {
			if strings.TrimSpace(*body.VisiblePixelHash) == "" {
				Error(w, http.StatusBadRequest, "visible_pixel_hash cannot be empty")
				return
			}
			updated.VisiblePixelHash = strings.TrimSpace(*body.VisiblePixelHash)
			changed = true
		}
		if body.BudgetSats != nil {
			updated.BudgetSats = *body.BudgetSats
			changed = true
		}
		if body.Metadata != nil {
			updated.Metadata = copyMeta(*body.Metadata)
			changed = true
		}

		if updated.Metadata == nil {
			updated.Metadata = map[string]interface{}{}
		}
		if body.ContractID != nil && strings.TrimSpace(*body.ContractID) != "" {
			updated.Metadata["contract_id"] = strings.TrimSpace(*body.ContractID)
			changed = true
		}
		if strings.TrimSpace(updated.VisiblePixelHash) != "" {
			if vph, ok := updated.Metadata["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
				updated.Metadata["visible_pixel_hash"] = updated.VisiblePixelHash
			}
		}
		if metaContract, ok := updated.Metadata["contract_id"].(string); ok {
			metaContract = strings.TrimSpace(metaContract)
			if metaContract != "" {
				if metaHash, ok2 := updated.Metadata["visible_pixel_hash"].(string); ok2 {
					metaHash = strings.TrimSpace(metaHash)
					if metaHash != "" && metaHash != metaContract {
						Error(w, http.StatusBadRequest, "visible_pixel_hash must match contract_id when both are set")
						return
					}
				}
			}
		}

		if body.Tasks != nil {
			updated.Tasks = *body.Tasks
			contractID := contractIDFromMeta(updated.Metadata, updated.ID)
			for i := range updated.Tasks {
				if updated.Tasks[i].TaskID == "" {
					updated.Tasks[i].TaskID = updated.ID + "-task-" + strconv.Itoa(i+1)
				}
				if updated.Tasks[i].ContractID == "" && contractID != "" {
					updated.Tasks[i].ContractID = contractID
				}
				if updated.Tasks[i].Status == "" {
					updated.Tasks[i].Status = "available"
				}
			}
			changed = true
		}

		if !changed {
			Error(w, http.StatusBadRequest, "no updates provided")
			return
		}

		if err := s.store.UpdateProposal(r.Context(), updated); err != nil {
			Error(w, http.StatusBadRequest, err.Error())
			return
		}
		s.recordEvent(smart_contract.Event{
			Type:      "update",
			EntityID:  updated.ID,
			Actor:     "editor",
			Message:   "proposal updated",
			CreatedAt: time.Now(),
		})
		JSON(w, http.StatusOK, map[string]interface{}{
			"proposal_id": updated.ID,
			"status":      updated.Status,
			"message":     "Proposal updated.",
		})
		return
	case http.MethodGet:
		if path == "" {
			minBudget := int64FromQuery(r, "min_budget_sats", 0)
			filter := smart_contract.ProposalFilter{
				Status:     r.URL.Query().Get("status"),
				Skills:     splitCSV(r.URL.Query().Get("skills")),
				MinBudget:  minBudget,
				ContractID: r.URL.Query().Get("contract_id"),
				MaxResults: intFromQuery(r, "limit", 0),
				Offset:     intFromQuery(r, "offset", 0),
			}
			proposals, err := s.store.ListProposals(r.Context(), filter)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			if !includeConfirmed(r) {
				ingestionStatus := make(map[string]string)
				filtered := make([]smart_contract.Proposal, 0, len(proposals))
				for _, p := range proposals {
					if proposalMetaConfirmed(p.Metadata) {
						continue
					}
					if s.ingestionSvc != nil {
						if ingestionID, ok := p.Metadata["ingestion_id"].(string); ok && strings.TrimSpace(ingestionID) != "" {
							status, cached := ingestionStatus[ingestionID]
							if !cached {
								if rec, err := s.ingestionSvc.Get(ingestionID); err == nil && rec != nil {
									status = rec.Status
								}
								ingestionStatus[ingestionID] = status
							}
							if strings.EqualFold(status, "confirmed") {
								continue
							}
						}
					}
					filtered = append(filtered, p)
				}
				proposals = filtered
			}
			// hydrate submissions alongside tasks
			var taskIDs []string
			for _, p := range proposals {
				for _, t := range p.Tasks {
					taskIDs = append(taskIDs, t.TaskID)
				}
			}
			subs, _ := s.store.ListSubmissions(r.Context(), taskIDs)
			JSON(w, http.StatusOK, map[string]interface{}{
				"proposals":   proposals,
				"total":       len(proposals),
				"submissions": subs,
			})
			return
		}
		// get single
		id := path
		p, err := s.store.GetProposal(r.Context(), id)
		if err != nil {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		JSON(w, http.StatusOK, p)
	default:
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// buildProposalFromIngestion derives a proposal from a pending ingestion record.
func BuildProposalFromIngestion(body ProposalCreateBody, rec *services.IngestionRecord) (smart_contract.Proposal, error) {
	meta := copyMeta(rec.Metadata)
	if meta == nil {
		meta = map[string]interface{}{}
	}
	// Ensure ingestion reference is present for traceability.
	meta["ingestion_id"] = rec.ID
	if body.ContractID != "" {
		meta["contract_id"] = body.ContractID
	}
	if em, ok := meta["embedded_message"].(string); ok && em != "" {
		// keep as-is
	} else {
		meta["embedded_message"] = ""
	}

	id := body.ID
	if id == "" {
		id = "proposal-" + rec.ID
	}
	title := body.Title
	if strings.TrimSpace(title) == "" {
		if em, _ := meta["embedded_message"].(string); em != "" {
			title = strings.Fields(em)[0]
			if title == "" {
				title = "Proposal " + rec.ID
			}
		} else {
			title = "Proposal " + rec.ID
		}
	}
	desc := body.DescriptionMD
	if desc == "" {
		if em, _ := meta["embedded_message"].(string); em != "" {
			desc = em
		}
	}
	budget := body.BudgetSats
	if budget == 0 {
		budget = budgetFromMeta(meta)
	}
	visible := body.VisiblePixelHash
	if visible == "" && rec.ImageBase64 != "" {
		if h, err := hashBase64(rec.ImageBase64); err == nil {
			visible = h
		}
	}
	if strings.TrimSpace(visible) != "" {
		if vph, ok := meta["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			meta["visible_pixel_hash"] = visible
		}
	}
	status := body.Status
	if status == "" {
		status = "pending"
	}

	tasks := body.Tasks
	if len(tasks) == 0 {
		if em, _ := meta["embedded_message"].(string); em != "" {
			tasks = scstore.BuildTasksFromMarkdown(id, em, visible, budget, scstore.FundingAddressFromMeta(meta))
		}
	}

	p := smart_contract.Proposal{
		ID:               id,
		Title:            title,
		DescriptionMD:    desc,
		VisiblePixelHash: visible,
		BudgetSats:       budget,
		Status:           status,
		CreatedAt:        time.Now(),
		Tasks:            tasks,
		Metadata:         meta,
	}
	return p, nil
}

// submissionReviewBody captures POST payload for reviewing submissions.
type submissionReviewBody struct {
	Action        string `json:"action"` // review | approve | reject
	Notes         string `json:"notes"`
	RejectionType string `json:"rejection_type"`
}

// submissionReworkBody captures POST payload for reworking submissions.
type submissionReworkBody struct {
	Deliverables map[string]interface{} `json:"deliverables"`
	Notes        string                 `json:"notes"`
}

// handleSubmissions manages submission endpoints for review and rework.
func (s *Server) handleSubmissions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/smart_contract/submissions")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" || path == "/" {
			// List submissions with optional filters
			contractID := r.URL.Query().Get("contract_id")
			taskIDs := splitCSV(r.URL.Query().Get("task_ids"))
			status := r.URL.Query().Get("status")

			var submissions []smart_contract.Submission
			var err error

			if len(taskIDs) > 0 {
				submissions, err = s.store.ListSubmissions(r.Context(), taskIDs)
			} else if contractID != "" {
				// Get tasks for contract, then submissions for those tasks
				tasks, err := s.store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
				if err != nil {
					Error(w, http.StatusInternalServerError, err.Error())
					return
				}
				taskIDs = make([]string, len(tasks))
				for i, task := range tasks {
					taskIDs[i] = task.TaskID
				}
				submissions, err = s.store.ListSubmissions(r.Context(), taskIDs)
			} else {
				// Get all tasks, then all submissions
				tasks, err := s.store.ListTasks(smart_contract.TaskFilter{})
				if err != nil {
					Error(w, http.StatusInternalServerError, err.Error())
					return
				}
				taskIDs = make([]string, len(tasks))
				for i, task := range tasks {
					taskIDs[i] = task.TaskID
				}
				submissions, err = s.store.ListSubmissions(r.Context(), taskIDs)
			}

			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			// Filter by status if provided
			if status != "" {
				filtered := make([]smart_contract.Submission, 0)
				for _, sub := range submissions {
					if strings.EqualFold(sub.Status, status) {
						filtered = append(filtered, sub)
					}
				}
				submissions = filtered
			}

			// Convert to map for easier frontend consumption
			submissionMap := make(map[string]smart_contract.Submission)
			for _, sub := range submissions {
				submissionMap[sub.SubmissionID] = sub
			}

			JSON(w, http.StatusOK, map[string]interface{}{
				"submissions": submissionMap,
				"total":       len(submissions),
			})
			return
		}

		// GET /mcp/v1/submissions/{submissionId}
		if len(parts) >= 1 && parts[0] != "" {
			submissionID := parts[0]
			log.Printf("GET submission by ID: %s", submissionID)

			// We need to get all submissions to find the specific one
			// This is not optimal but works for the current store interface
			tasks, err := s.store.ListTasks(smart_contract.TaskFilter{})
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			taskIDs := make([]string, len(tasks))
			for i, task := range tasks {
				taskIDs[i] = task.TaskID
			}

			submissions, err := s.store.ListSubmissions(r.Context(), taskIDs)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			log.Printf("Found %d submissions for contract", len(submissions))
			for _, sub := range submissions {
				log.Printf("Checking submission ID: %s == %s ?", sub.SubmissionID, submissionID)
				if sub.SubmissionID == submissionID {
					log.Printf("Found matching submission: %s", submissionID)
					JSON(w, http.StatusOK, sub)
					return
				}
			}

			log.Printf("No matching submission found for ID: %s", submissionID)
			Error(w, http.StatusNotFound, "submission not found")
			return
		}

		Error(w, http.StatusBadRequest, "invalid submission endpoint")
		return

	case http.MethodPost:
		if len(parts) >= 2 && parts[1] == "review" {
			// POST /mcp/v1/submissions/{submissionId}/review
			submissionID := parts[0]

			var body submissionReviewBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				Error(w, http.StatusBadRequest, "invalid json")
				return
			}

			if body.Action == "" {
				Error(w, http.StatusBadRequest, "action is required")
				return
			}

			// Validate action
			validActions := map[string]bool{
				"review":  true,
				"approve": true,
				"reject":  true,
			}
			if !validActions[body.Action] {
				Error(w, http.StatusBadRequest, "invalid action. must be: review, approve, or reject")
				return
			}

			// Update submission status
			var newStatus string
			switch body.Action {
			case "review":
				newStatus = "reviewed"
			case "approve":
				newStatus = "approved"
			case "reject":
				newStatus = "rejected"
			}

			ctx := r.Context()
			rejectionType := ""
			reviewNotes := ""
			if body.Action == "reject" {
				reviewNotes = body.Notes
				rejectionType = body.RejectionType
			}
			err := s.store.UpdateSubmissionStatus(ctx, submissionID, newStatus, reviewNotes, rejectionType)
			if err != nil {
				if strings.Contains(err.Error(), "not found") {
					Error(w, http.StatusNotFound, "submission not found")
					return
				}
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			// Record event
			s.recordEvent(smart_contract.Event{
				Type:      "review",
				EntityID:  submissionID,
				Actor:     "reviewer",
				Message:   fmt.Sprintf("submission %s", body.Action),
				CreatedAt: time.Now(),
			})

			JSON(w, http.StatusOK, map[string]interface{}{
				"message":       fmt.Sprintf("submission %sd successfully", body.Action),
				"status":        newStatus,
				"submission_id": submissionID,
			})
			return
		}

		if len(parts) >= 2 && parts[1] == "rework" {
			// POST /mcp/v1/submissions/{submissionId}/rework
			submissionID := parts[0]

			var body submissionReworkBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				Error(w, http.StatusBadRequest, "invalid json")
				return
			}

			if body.Deliverables == nil && body.Notes == "" {
				Error(w, http.StatusBadRequest, "deliverables or notes must be provided")
				return
			}

			// Get the original submission to update it
			tasks, err := s.store.ListTasks(smart_contract.TaskFilter{})
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			taskIDs := make([]string, len(tasks))
			for i, task := range tasks {
				taskIDs[i] = task.TaskID
			}

			submissions, err := s.store.ListSubmissions(r.Context(), taskIDs)
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			var originalSubmission *smart_contract.Submission
			for _, sub := range submissions {
				if sub.SubmissionID == submissionID {
					originalSubmission = &sub
					break
				}
			}

			if originalSubmission == nil {
				Error(w, http.StatusNotFound, "submission not found")
				return
			}

			// Update deliverables if provided
			if body.Deliverables != nil {
				originalSubmission.Deliverables = body.Deliverables
			}

			// Add rework notes to deliverables
			if body.Notes != "" {
				if originalSubmission.Deliverables == nil {
					originalSubmission.Deliverables = make(map[string]interface{})
				}
				originalSubmission.Deliverables["rework_notes"] = body.Notes
				originalSubmission.Deliverables["reworked_at"] = time.Now().Format(time.RFC3339)
			}

			// Reset status to pending_review
			ctx := r.Context()
			err = s.store.UpdateSubmissionStatus(ctx, submissionID, "pending_review", "", "")
			if err != nil {
				Error(w, http.StatusInternalServerError, err.Error())
				return
			}

			// Record event
			s.recordEvent(smart_contract.Event{
				Type:      "rework",
				EntityID:  submissionID,
				Actor:     "claimant",
				Message:   "submission reworked",
				CreatedAt: time.Now(),
			})

			JSON(w, http.StatusOK, map[string]interface{}{
				"message":       "rework submitted successfully",
				"status":        "pending_review",
				"submission_id": submissionID,
			})
			return
		}

		Error(w, http.StatusBadRequest, "invalid submission endpoint")
		return

	default:
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
}
