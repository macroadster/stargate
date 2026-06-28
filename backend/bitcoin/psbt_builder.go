package bitcoin

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// PSBTRequest captures the inputs needed to craft a payout PSBT.
type PSBTRequest struct {
	PayerAddress      btcutil.Address
	PayerAddresses    []btcutil.Address
	TargetValueSats   int64
	PixelHash         []byte          // Wish image hash (32 bytes) — used for OP_RETURN proof
	ProductPixelHash  []byte          // Stego image hash (32 bytes) — used for OP_RETURN proof
	CommitmentSats    int64           // Sats sent directly to DonationAddress
	DonationAddress   btcutil.Address // Direct P2WPKH donation recipient
	CommitmentAddress btcutil.Address // Deprecated: kept for backward compat, use DonationAddress
	ContractorAddress btcutil.Address
	Payouts           []PayoutOutput
	FeeRateSatPerVB   int64
	ChangeAddress     btcutil.Address
	UseAllPayers      bool
}

// PayoutOutput defines a payout destination and amount.
type PayoutOutput struct {
	Address   btcutil.Address
	ValueSats int64
}

// PSBTResult summarizes the built PSBT.
type PSBTResult struct {
	EncodedBase64    string
	EncodedHex       string
	FeeSats          int64
	ChangeSats       int64
	ChangeAddresses  []string
	ChangeAmounts    []int64
	SelectedSats     int64
	PayoutScript     []byte
	PayoutScripts    [][]byte
	PayoutAmounts    []int64
	CommitmentSats   int64
	CommitmentScript []byte // Deprecated: was P2WSH hashlock, now donation P2WPKH script
	CommitmentVout   uint32 // Deprecated: use DonationVout
	RedeemScript     []byte // Deprecated: no longer used (no hashlock)
	RedeemScriptHash []byte // Deprecated: no longer used (no hashlock)
	CommitmentAddr   string // Deprecated: use DonationAddr
	DonationVout     uint32 // Vout index of the direct donation P2WPKH output
	DonationAddr     string // Donation address (P2WPKH)
	OPReturnScript   []byte // OP_RETURN script with wish_hash(32) || stego_hash(32)
	OPReturnVout     uint32 // Vout index of the OP_RETURN output
	FundingTxID      string
}

// PayerTarget defines a funding contribution for a specific payer address.
type PayerTarget struct {
	Address    btcutil.Address
	TargetSats int64
}

type payerSelection struct {
	address       btcutil.Address
	target        int64
	selected      int64
	feeShare      int64
	change        int64
	utxos         []AddressUTXO
	candidates    []AddressUTXO
	nextIndex     int
	changeScript  []byte
	changeAllowed bool
}

// BuildFundingPSBT selects confirmed UTXOs, estimates fees at the provided feerate, and builds a PSBT.
// When a pixel hash is provided, a small commitment output is added alongside the contractor payout.
func BuildFundingPSBT(client *MempoolClient, params *chaincfg.Params, req PSBTRequest) (*PSBTResult, error) {
	if req.FeeRateSatPerVB < 0 {
		req.FeeRateSatPerVB = 0
	}

	payerAddrs := req.PayerAddresses
	if len(payerAddrs) == 0 {
		if req.PayerAddress == nil {
			return nil, fmt.Errorf("payer address required")
		}
		payerAddrs = []btcutil.Address{req.PayerAddress}
	}
	changeAddr := req.ChangeAddress
	if changeAddr == nil {
		changeAddr = payerAddrs[0]
	}

	type payerUTXO struct {
		address btcutil.Address
		utxo    AddressUTXO
	}
	var candidates []payerUTXO
	for _, addr := range payerAddrs {
		if addr == nil {
			return nil, fmt.Errorf("payer address required")
		}
		utxos, err := client.ListConfirmedUTXOs(addr.EncodeAddress())
		if err != nil {
			return nil, err
		}
		for _, u := range utxos {
			candidates = append(candidates, payerUTXO{address: addr, utxo: u})
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no confirmed utxos for address")
	}

	if req.UseAllPayers && len(payerAddrs) > 1 {
		seeded := make([]payerUTXO, 0, len(payerAddrs))
		remaining := make([]payerUTXO, 0, len(candidates))
		for _, addr := range payerAddrs {
			found := false
			for i := 0; i < len(candidates); i++ {
				if candidates[i].address.EncodeAddress() != addr.EncodeAddress() {
					continue
				}
				seeded = append(seeded, candidates[i])
				candidates[i] = candidates[len(candidates)-1]
				candidates = candidates[:len(candidates)-1]
				found = true
				break
			}
			if !found {
				return nil, fmt.Errorf("no confirmed utxos for payer address %s", addr.EncodeAddress())
			}
		}
		remaining = append(remaining, candidates...)
		candidates = append(seeded, remaining...)
	}

	payoutScripts, payoutAmounts, err := buildPayoutScripts(req)
	if err != nil {
		return nil, err
	}

	var commitmentScript []byte
	var commitmentSats int64
	var redeemScript []byte
	var redeemScriptHash []byte
	var commitmentAddr string
	var donation *donationOutputs
	// New path: direct donation + OP_RETURN proof (no hashlock, no sweeping)
	if req.DonationAddress != nil && len(req.PixelHash) > 0 && req.CommitmentSats > 0 {
		donation, err = buildDonationOutputs(params, req.PixelHash, req.ProductPixelHash, req.DonationAddress)
		if err != nil {
			return nil, err
		}
		commitmentSats = req.CommitmentSats
		if commitmentSats < 546 {
			commitmentSats = 546
		}
	} else if len(req.PixelHash) > 0 && req.CommitmentSats > 0 {
		// Legacy path: P2WSH hashlock (backward compat for old callers)
		commitmentScript, redeemScript, redeemScriptHash, commitmentAddr, err = buildCommitmentScript(params, req.PixelHash, req.CommitmentAddress)
		if err != nil {
			return nil, err
		}
		commitmentSats = req.CommitmentSats
		if commitmentSats < 546 {
			commitmentSats = 546
		}
	}

	requiredValue := sumAmounts(payoutAmounts) + commitmentSats
	if len(payoutScripts) == 0 && commitmentScript == nil {
		return nil, fmt.Errorf("no payout or commitment outputs requested")
	}

	var selected []payerUTXO
	var selectedValue int64
	var estimatedInputVBytes int64
	// Greedy selection: accumulate until budget+fee is covered.
	for _, u := range candidates {
		selected = append(selected, u)
		selectedValue += u.utxo.Value
		estimatedInputVBytes += estimateInputVBytes(u.address)
		outputCount := int64(len(payoutScripts))
		if donation != nil {
			outputCount += 2 // donation P2WPKH + OP_RETURN
		} else if commitmentScript != nil {
			outputCount++ // legacy hashlock
		}
		if changeAddr != nil && selectedValue > requiredValue {
			outputCount++
		}
		estFee := estimateFee(estimatedInputVBytes, outputCount, req.FeeRateSatPerVB)
		if selectedValue >= requiredValue+estFee {
			break
		}
	}

	if selectedValue < requiredValue {
		return nil, fmt.Errorf("insufficient funds: need %d sats, selected %d", requiredValue, selectedValue)
	}

	var meta []inputMeta
	var actualInputVBytes int64
	for _, u := range selected {
		prevMsg, prevOut, err := client.FetchTxOutput(u.utxo.TxID, u.utxo.Vout)
		if err != nil {
			return nil, fmt.Errorf("fetch prev output %s:%d: %w", u.utxo.TxID, u.utxo.Vout, err)
		}
		actualInputVBytes += estimateInputVBytesFromPkScript(prevOut.PkScript)
		meta = append(meta, inputMeta{
			nonWitness: prevMsg,
			witness:    prevOut,
		})
	}

	var changeScript []byte
	if changeAddr != nil {
		var err error
		changeScript, err = txscript.PayToAddrScript(changeAddr)
		if err != nil {
			return nil, fmt.Errorf("build change script: %w", err)
		}
	}

	outputCount := int64(len(payoutScripts))
	if donation != nil {
		outputCount += 2 // donation P2WPKH + OP_RETURN
	} else if commitmentScript != nil {
		outputCount++ // legacy hashlock
	}
	fee := estimateFee(actualInputVBytes, outputCount, req.FeeRateSatPerVB)
	change := selectedValue - requiredValue - fee
	// Add change output if not dust.
	if changeScript != nil && change >= 546 {
		outputCount++
		fee = estimateFee(actualInputVBytes, outputCount, req.FeeRateSatPerVB)
		change = selectedValue - requiredValue - fee
	}
	if change < 0 {
		return nil, fmt.Errorf("insufficient funds after fee: %d sats short", -change)
	}

	tx := wire.NewMsgTx(2)
	var commitmentVout uint32
	var donationVout uint32
	var opReturnVout uint32
	for _, u := range selected {
		hash, err := chainhashFromStr(u.utxo.TxID)
		if err != nil {
			return nil, err
		}
		tx.AddTxIn(&wire.TxIn{
			PreviousOutPoint: wire.OutPoint{Hash: hash, Index: u.utxo.Vout},
		})
	}
	for i, script := range payoutScripts {
		tx.AddTxOut(&wire.TxOut{Value: payoutAmounts[i], PkScript: script})
	}
	if donation != nil && commitmentSats > 0 {
		// New path: direct donation P2WPKH + OP_RETURN proof
		donationVout = uint32(len(tx.TxOut))
		commitmentVout = donationVout // backward compat alias
		tx.AddTxOut(&wire.TxOut{Value: commitmentSats, PkScript: donation.donationScript})
		opReturnVout = uint32(len(tx.TxOut))
		tx.AddTxOut(&wire.TxOut{Value: 0, PkScript: donation.opReturnScript})
		commitmentScript = donation.donationScript
		commitmentAddr = donation.donationAddr
	} else if commitmentScript != nil && commitmentSats > 0 {
		// Legacy hashlock path
		commitmentVout = uint32(len(tx.TxOut))
		tx.AddTxOut(&wire.TxOut{Value: commitmentSats, PkScript: commitmentScript})
	}
	var changeAddresses []string
	var changeAmounts []int64
	if changeScript != nil && change >= 546 {
		tx.AddTxOut(&wire.TxOut{Value: change, PkScript: changeScript})
		changeAddresses = append(changeAddresses, changeAddr.EncodeAddress())
		changeAmounts = append(changeAmounts, change)
	}

	psbtBytes, err := encodePSBT(tx, meta)
	if err != nil {
		return nil, fmt.Errorf("serialize psbt: %w", err)
	}

	// Check if all inputs are SegWit to determine if we can pre-calculate TxID
	allSegWit := true
	for _, u := range selected {
		_, prevOut, err := client.FetchTxOutput(u.utxo.TxID, u.utxo.Vout)
		if err != nil {
			allSegWit = false
			break
		}

		_, addrs, _, err := txscript.ExtractPkScriptAddrs(prevOut.PkScript, params)
		if err != nil {
			allSegWit = false
			break
		}

		// Check each address from the script
		isSegWitInput := false
		for _, addr := range addrs {
			switch addr.(type) {
			case *btcutil.AddressWitnessPubKeyHash:
				// P2WPKH - SegWit native
				isSegWitInput = true
			case *btcutil.AddressWitnessScriptHash:
				// P2WSH - SegWit native
				isSegWitInput = true
			case *btcutil.AddressTaproot:
				// Taproot - SegWit v1
				isSegWitInput = true
			}
		}
		if !isSegWitInput {
			allSegWit = false
			break
		}
	}

	var fundingTxID string
	if allSegWit {
		// All inputs are SegWit, so TxID is non-malleable and can be pre-calculated
		fundingTxID = tx.TxHash().String()
	}

	result := &PSBTResult{
		EncodedBase64:    base64.StdEncoding.EncodeToString(psbtBytes),
		EncodedHex:       hex.EncodeToString(psbtBytes),
		FeeSats:          fee,
		ChangeSats:       change,
		ChangeAddresses:  changeAddresses,
		ChangeAmounts:    changeAmounts,
		SelectedSats:     selectedValue,
		PayoutScript:     firstScript(payoutScripts),
		PayoutScripts:    payoutScripts,
		PayoutAmounts:    payoutAmounts,
		CommitmentSats:   commitmentSats,
		CommitmentScript: commitmentScript,
		CommitmentVout:   commitmentVout,
		RedeemScript:     redeemScript,
		RedeemScriptHash: redeemScriptHash,
		CommitmentAddr:   commitmentAddr,
		FundingTxID:      fundingTxID,
	}
	if donation != nil {
		result.DonationVout = donationVout
		result.DonationAddr = donation.donationAddr
		result.OPReturnScript = donation.opReturnScript
		result.OPReturnVout = opReturnVout
	}
	return result, nil
}

// BuildRaiseFundPSBT builds a multi-payer PSBT with per-payer change outputs.
func BuildRaiseFundPSBT(client *MempoolClient, params *chaincfg.Params, payers []PayerTarget, payouts []PayoutOutput, pixelHash []byte, commitmentSats int64, commitmentAddress btcutil.Address, feeRate int64) (*PSBTResult, error) {
	if feeRate < 0 {
		feeRate = 0
	}
	if len(payers) == 0 {
		return nil, fmt.Errorf("payer targets required")
	}
	if len(payouts) == 0 && len(pixelHash) == 0 {
		return nil, fmt.Errorf("no payout or commitment outputs requested")
	}

	payoutScripts, payoutAmounts, err := buildPayoutScripts(PSBTRequest{Payouts: payouts})
	if err != nil {
		return nil, err
	}

	selections := make([]payerSelection, 0, len(payers))
	for _, payer := range payers {
		if payer.Address == nil {
			return nil, fmt.Errorf("payer address required")
		}
		if payer.TargetSats <= 0 {
			return nil, fmt.Errorf("payer target must be positive")
		}
		utxos, err := client.ListConfirmedUTXOs(payer.Address.EncodeAddress())
		if err != nil {
			return nil, err
		}
		if len(utxos) == 0 {
			return nil, fmt.Errorf("no confirmed utxos for payer address %s", payer.Address.EncodeAddress())
		}
		sort.Slice(utxos, func(i, j int) bool { return utxos[i].Value < utxos[j].Value })
		changeScript, err := txscript.PayToAddrScript(payer.Address)
		if err != nil {
			return nil, fmt.Errorf("build change script: %w", err)
		}
		selections = append(selections, payerSelection{
			address:      payer.Address,
			target:       payer.TargetSats,
			candidates:   utxos,
			changeScript: changeScript,
		})
	}

	var commitmentScript []byte
	var redeemScript []byte
	var redeemScriptHash []byte
	var commitmentAddr string
	var donation *donationOutputs
	// Note: BuildRaiseFundPSBT doesn't yet receive DonationAddress/ProductPixelHash
	// so it always falls through to the legacy path.  When the caller is updated,
	// it will use the donation path automatically.
	if len(pixelHash) > 0 {
		commitmentScript, redeemScript, redeemScriptHash, commitmentAddr, err = buildCommitmentScript(params, pixelHash, commitmentAddress)
		if err != nil {
			return nil, err
		}
		if commitmentSats <= 0 {
			commitmentSats = 1000
		}
		if commitmentSats < 546 {
			commitmentSats = 546
		}
	}
	_ = donation // will be used when BuildRaiseFundPSBT is updated to accept DonationAddress

	addNextUTXO := func(sel *payerSelection) error {
		if sel.nextIndex >= len(sel.candidates) {
			return fmt.Errorf("insufficient funds for payer %s", sel.address.EncodeAddress())
		}
		utxo := sel.candidates[sel.nextIndex]
		sel.nextIndex++
		sel.utxos = append(sel.utxos, utxo)
		sel.selected += utxo.Value
		return nil
	}

	for i := range selections {
		for selections[i].selected < selections[i].target {
			if err := addNextUTXO(&selections[i]); err != nil {
				return nil, err
			}
		}
	}

	var meta []inputMeta
	var actualInputVBytes int64
	for attempt := 0; attempt < 5; attempt++ {
		var totalSelected int64
		var estimatedInputVBytes int64
		for i := range selections {
			totalSelected += selections[i].selected
			estimatedInputVBytes += estimateInputVBytes(selections[i].address) * int64(len(selections[i].utxos))
		}

		for {
			outputCount := int64(len(payoutScripts))
			if commitmentScript != nil {
				outputCount++
			}
			outputCount += int64(len(selections))
			fee := estimateFee(estimatedInputVBytes, outputCount, feeRate)
			var allocated int64
			for i := range selections {
				selections[i].feeShare = fee * selections[i].selected / totalSelected
				allocated += selections[i].feeShare
			}
			if diff := fee - allocated; diff != 0 {
				selections[len(selections)-1].feeShare += diff
			}

			expanded := false
			for i := range selections {
				needed := selections[i].target + selections[i].feeShare
				for selections[i].selected < needed {
					if err := addNextUTXO(&selections[i]); err != nil {
						return nil, err
					}
					estimatedInputVBytes += estimateInputVBytes(selections[i].address)
					totalSelected += selections[i].utxos[len(selections[i].utxos)-1].Value
					expanded = true
				}
			}
			if !expanded {
				break
			}
		}

		meta = nil
		actualInputVBytes = 0
		for _, sel := range selections {
			for _, u := range sel.utxos {
				prevMsg, prevOut, err := client.FetchTxOutput(u.TxID, u.Vout)
				if err != nil {
					return nil, fmt.Errorf("fetch prev output %s:%d: %w", u.TxID, u.Vout, err)
				}
				actualInputVBytes += estimateInputVBytesFromPkScript(prevOut.PkScript)
				meta = append(meta, inputMeta{
					nonWitness: prevMsg,
					witness:    prevOut,
				})
			}
		}

		changeableCount := len(selections)
		needsMore := false
		for {
			outputCount := int64(len(payoutScripts))
			if commitmentScript != nil {
				outputCount++
			}
			outputCount += int64(changeableCount)
			fee := estimateFee(actualInputVBytes, outputCount, feeRate)
			var allocated int64
			for i := range selections {
				selections[i].feeShare = fee * selections[i].selected / totalSelected
				allocated += selections[i].feeShare
			}
			if diff := fee - allocated; diff != 0 {
				selections[len(selections)-1].feeShare += diff
			}

			newChangeable := 0
			for i := range selections {
				selections[i].change = selections[i].selected - selections[i].target - selections[i].feeShare
				if selections[i].change < 0 {
					if err := addNextUTXO(&selections[i]); err != nil {
						return nil, err
					}
					needsMore = true
					break
				}
				selections[i].changeAllowed = selections[i].change >= 546
				if selections[i].changeAllowed {
					newChangeable++
				}
			}
			if needsMore {
				break
			}
			if newChangeable == changeableCount {
				break
			}
			changeableCount = newChangeable
		}
		if !needsMore {
			break
		}
		if attempt == 4 {
			return nil, fmt.Errorf("unable to satisfy funding targets with available utxos")
		}
	}

	var totalSelected int64
	for _, sel := range selections {
		totalSelected += sel.selected
	}

	tx := wire.NewMsgTx(2)
	var commitmentVout uint32
	for _, sel := range selections {
		for _, u := range sel.utxos {
			hash, err := chainhashFromStr(u.TxID)
			if err != nil {
				return nil, err
			}
			tx.AddTxIn(&wire.TxIn{
				PreviousOutPoint: wire.OutPoint{Hash: hash, Index: u.Vout},
			})
		}
	}
	for i, script := range payoutScripts {
		tx.AddTxOut(&wire.TxOut{Value: payoutAmounts[i], PkScript: script})
	}
	if commitmentScript != nil {
		commitmentVout = uint32(len(tx.TxOut))
		tx.AddTxOut(&wire.TxOut{Value: commitmentSats, PkScript: commitmentScript})
	}
	var totalChange int64
	var changeAddresses []string
	var changeAmounts []int64
	for _, sel := range selections {
		if sel.changeAllowed {
			tx.AddTxOut(&wire.TxOut{Value: sel.change, PkScript: sel.changeScript})
			totalChange += sel.change
			changeAddresses = append(changeAddresses, sel.address.EncodeAddress())
			changeAmounts = append(changeAmounts, sel.change)
		}
	}

	psbtBytes, err := encodePSBT(tx, meta)
	if err != nil {
		return nil, fmt.Errorf("serialize psbt: %w", err)
	}

	// Check if all inputs are SegWit to determine if we can pre-calculate TxID
	allSegWit := allPayerSelectionsAreSegWit(selections, client, params)

	var fundingTxID string
	if allSegWit {
		// All inputs are SegWit, so TxID is non-malleable and can be pre-calculated
		fundingTxID = tx.TxHash().String()
	}

	outputTotal := sumAmounts(payoutAmounts) + commitmentSats + totalChange
	actualFee := totalSelected - outputTotal
	if actualFee < 0 {
		actualFee = 0
	}

	return &PSBTResult{
		EncodedBase64:    base64.StdEncoding.EncodeToString(psbtBytes),
		EncodedHex:       hex.EncodeToString(psbtBytes),
		FeeSats:          actualFee,
		ChangeSats:       totalChange,
		ChangeAddresses:  changeAddresses,
		ChangeAmounts:    changeAmounts,
		SelectedSats:     totalSelected,
		PayoutScript:     firstScript(payoutScripts),
		PayoutScripts:    payoutScripts,
		PayoutAmounts:    payoutAmounts,
		CommitmentSats:   commitmentSats,
		CommitmentScript: commitmentScript,
		CommitmentVout:   commitmentVout,
		RedeemScript:     redeemScript,
		RedeemScriptHash: redeemScriptHash,
		CommitmentAddr:   commitmentAddr,
		FundingTxID:      fundingTxID,
	}, nil
}

// donationOutputs holds the two outputs replacing the old P2WSH hashlock:
// a direct P2WPKH payment to the donation address and an OP_RETURN proof.
type donationOutputs struct {
	donationScript []byte // P2WPKH pkScript for the donation address
	donationAddr   string
	opReturnScript []byte // OP_RETURN <wish_hash(32)><stego_hash(32)>
}

type inputMeta struct {
	nonWitness *wire.MsgTx
	witness    *wire.TxOut
}
