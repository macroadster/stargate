package bitcoin

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// PSBTRequest captures the inputs needed to craft a payout PSBT.
type PSBTRequest struct {
	PayerAddress      btcutil.Address
	PayerAddresses    []btcutil.Address
	TargetValueSats   int64
	PixelHash         []byte
	CommitmentSats    int64
	CommitmentAddress btcutil.Address
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
	CommitmentScript []byte
	CommitmentVout   uint32
	RedeemScript     []byte
	RedeemScriptHash []byte
	CommitmentAddr   string
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

// allPayerSelectionsAreSegWit checks if all selected UTXOs are SegWit types (P2WPKH, P2WSH, Taproot).
// Returns true only if all inputs are SegWit, which means the TxID is non-malleable.
func allPayerSelectionsAreSegWit(selections []payerSelection, client *MempoolClient, params *chaincfg.Params) bool {
	for _, sel := range selections {
		for _, u := range sel.utxos {
			_, prevOut, err := client.FetchTxOutput(u.TxID, u.Vout)
			if err != nil {
				return false // If we can't fetch, assume not safe
			}

			_, addrs, _, err := txscript.ExtractPkScriptAddrs(prevOut.PkScript, params)
			if err != nil {
				return false // If we can't extract addresses, assume not safe
			}

			// Check each address from the script
			for _, addr := range addrs {
				switch addr.(type) {
				case *btcutil.AddressWitnessPubKeyHash:
					// P2WPKH - SegWit native
				case *btcutil.AddressWitnessScriptHash:
					// P2WSH - SegWit native
				case *btcutil.AddressTaproot:
					// Taproot - SegWit v1
				default:
					// Any other type (P2PKH, P2SH, etc.) makes TxID malleable
					return false
				}
			}
		}
	}
	return true
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
	// Only create commitment when BOTH pixelHash is provided AND commitmentSats > 0
	if len(req.PixelHash) > 0 && req.CommitmentSats > 0 {
		commitmentScript, redeemScript, redeemScriptHash, commitmentAddr, err = buildCommitmentScript(params, req.PixelHash, req.CommitmentAddress)
		if err != nil {
			return nil, err
		}
		commitmentSats = req.CommitmentSats
		// Apply minimum for donations (must be > 0 to reach here)
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
		// Two outputs when change is expected, otherwise one.
		outputCount := int64(len(payoutScripts))
		if commitmentScript != nil {
			outputCount++
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
	if commitmentScript != nil {
		outputCount++
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
	// Only add commitment output if commitment_sats > 0 (avoids dust outputs)
	if commitmentScript != nil && commitmentSats > 0 {
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

	return &PSBTResult{
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
	}, nil
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

func buildPayoutScripts(req PSBTRequest) ([][]byte, []int64, error) {
	if len(req.Payouts) > 0 {
		var scripts [][]byte
		var amounts []int64
		for _, payout := range req.Payouts {
			if payout.Address == nil {
				return nil, nil, fmt.Errorf("payout address required")
			}
			if payout.ValueSats <= 0 {
				return nil, nil, fmt.Errorf("payout amount must be positive")
			}
			script, err := txscript.PayToAddrScript(payout.Address)
			if err != nil {
				return nil, nil, fmt.Errorf("payout script: %w", err)
			}
			scripts = append(scripts, script)
			amounts = append(amounts, payout.ValueSats)
		}
		return scripts, amounts, nil
	}
	if req.ContractorAddress == nil {
		if req.TargetValueSats <= 0 {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("contractor address required")
	}
	if req.TargetValueSats <= 0 {
		return nil, nil, fmt.Errorf("target value must be positive")
	}
	script, err := txscript.PayToAddrScript(req.ContractorAddress)
	if err != nil {
		return nil, nil, fmt.Errorf("contractor script: %w", err)
	}
	return [][]byte{script}, []int64{req.TargetValueSats}, nil
}

func buildCommitmentScript(params *chaincfg.Params, pixelHash []byte, commitmentAddress btcutil.Address) ([]byte, []byte, []byte, string, error) {
	if len(pixelHash) != 32 {
		return nil, nil, nil, "", fmt.Errorf("pixel hash must be 32 bytes for P2WSH hashlock")
	}
	var redeemScript []byte
	var err error
	// Both donation and contractor commitments should be hashlock-only (no signature required)
	// commitmentAddress is used for display purposes only, not for script construction
	redeemScript, err = buildHashlockRedeemScript(pixelHash)
	if err != nil {
		return nil, nil, nil, "", err
	}
	if err != nil {
		return nil, nil, nil, "", err
	}
	hash := sha256.Sum256(redeemScript)
	addr, err := btcutil.NewAddressWitnessScriptHash(hash[:], params)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("pixel hash p2wsh: %w", err)
	}
	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, nil, nil, "", err
	}
	return pkScript, redeemScript, hash[:], addr.EncodeAddress(), nil
}

func buildHashlockRedeemScript(pixelHash []byte) ([]byte, error) {
	builder := txscript.NewScriptBuilder()
	builder.AddOp(txscript.OP_SHA256)
	// The pixelHash passed in is actually the preimage (visible pixel hash or stego hash).
	// We must hash it so the script becomes OP_SHA256 <SHA256(preimage)> OP_EQUAL.
	hash := sha256.Sum256(pixelHash)
	builder.AddData(hash[:])
	builder.AddOp(txscript.OP_EQUAL)
	return builder.Script()
}

func buildHashlockP2PKHRedeemScript(pixelHash []byte, addr btcutil.Address) ([]byte, error) {
	if addr == nil {
		return nil, fmt.Errorf("commitment address required")
	}
	pubKeyHash := addr.ScriptAddress()
	if len(pubKeyHash) != 20 {
		return nil, fmt.Errorf("commitment address must be P2PKH/P2WPKH")
	}
	builder := txscript.NewScriptBuilder()
	builder.AddOp(txscript.OP_SHA256)
	builder.AddData(pixelHash[:])
	builder.AddOp(txscript.OP_EQUALVERIFY)
	builder.AddOp(txscript.OP_DUP)
	builder.AddOp(txscript.OP_HASH160)
	builder.AddData(pubKeyHash)
	builder.AddOp(txscript.OP_EQUALVERIFY)
	builder.AddOp(txscript.OP_CHECKSIG)
	return builder.Script()
}

func estimateFee(inputVBytes, outputs int64, feeRate int64) int64 {
	// Basic vsize estimate: overhead ~10 vbytes + inputVBytes + outputs*34.
	vsize := int64(10) + inputVBytes + outputs*34
	fee := vsize * feeRate
	if feeRate > 0 {
		fee += 3 * feeRate
	}
	return fee
}

func sumAmounts(amounts []int64) int64 {
	var total int64
	for _, v := range amounts {
		total += v
	}
	return total
}

func firstScript(scripts [][]byte) []byte {
	if len(scripts) == 0 {
		return nil
	}
	return scripts[0]
}

func sumFeeShares(selections []payerSelection) int64 {
	var total int64
	for _, sel := range selections {
		total += sel.feeShare
	}
	return total
}

func estimateInputVBytes(addr btcutil.Address) int64 {
	switch addr.(type) {
	case *btcutil.AddressWitnessPubKeyHash:
		return 69
	case *btcutil.AddressTaproot:
		return 58
	case *btcutil.AddressScriptHash:
		return 109
	default:
		return 148 // P2PKH fallback
	}
}

func estimateInputVBytesFromPkScript(pkScript []byte) int64 {
	switch txscript.GetScriptClass(pkScript) {
	case txscript.WitnessV0PubKeyHashTy:
		return 69
	case txscript.WitnessV0ScriptHashTy:
		return 140
	case txscript.WitnessV1TaprootTy:
		return 58
	case txscript.ScriptHashTy:
		return 109
	case txscript.PubKeyHashTy:
		return 148
	default:
		return 148
	}
}

func chainhashFromStr(hash string) (chainhash.Hash, error) {
	h, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		return chainhash.Hash{}, err
	}
	return *h, nil
}

type inputMeta struct {
	nonWitness *wire.MsgTx
	witness    *wire.TxOut
}

// encodePSBT emits a minimal BIP-174 packet with unsigned tx and per-input utxo data.
func encodePSBT(tx *wire.MsgTx, inputs []inputMeta) ([]byte, error) {
	var buf bytes.Buffer
	// Magic bytes
	buf.Write([]byte{0x70, 0x73, 0x62, 0x74, 0xff})

	// Global map: unsigned tx
	if err := writeKeyVal(&buf, []byte{0x00}, serializeUnsigned(tx)); err != nil {
		return nil, err
	}
	buf.WriteByte(0x00) // end of global map

	for _, in := range inputs {
		if in.nonWitness != nil {
			if err := writeKeyVal(&buf, []byte{0x00}, serializeMsgTx(in.nonWitness)); err != nil {
				return nil, err
			}
		}
		if in.witness != nil {
			witBytes, err := serializeTxOut(in.witness)
			if err != nil {
				return nil, err
			}
			if err := writeKeyVal(&buf, []byte{0x01}, witBytes); err != nil {
				return nil, err
			}
		}
		buf.WriteByte(0x00) // end of input map
	}

	for range tx.TxOut {
		buf.WriteByte(0x00) // empty output map
	}

	return buf.Bytes(), nil
}

func writeKeyVal(w *bytes.Buffer, key []byte, val []byte) error {
	if err := wire.WriteVarBytes(w, 0, key); err != nil {
		return err
	}
	return wire.WriteVarBytes(w, 0, val)
}

func serializeUnsigned(tx *wire.MsgTx) []byte {
	var buf bytes.Buffer
	_ = tx.SerializeNoWitness(&buf)
	return buf.Bytes()
}

func serializeMsgTx(tx *wire.MsgTx) []byte {
	var buf bytes.Buffer
	_ = tx.Serialize(&buf)
	return buf.Bytes()
}

func serializeTxOut(txOut *wire.TxOut) ([]byte, error) {
	var buf bytes.Buffer
	err := wire.WriteTxOut(&buf, 0, 0, txOut)
	return buf.Bytes(), err
}
