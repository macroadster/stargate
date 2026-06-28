package bitcoin

import (
	"fmt"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

type payerUTXO struct {
	address btcutil.Address
	utxo    AddressUTXO
}

// collectPayerUTXOs lists confirmed UTXOs for each payer address.
func collectPayerUTXOs(client *MempoolClient, payerAddrs []btcutil.Address) ([]payerUTXO, error) {
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
	return candidates, nil
}

// seedOneUTXOPerPayer prioritizes at least one UTXO per payer when UseAllPayers.
func seedOneUTXOPerPayer(payerAddrs []btcutil.Address, candidates []payerUTXO) ([]payerUTXO, error) {
	seeded := make([]payerUTXO, 0, len(payerAddrs))
	rest := append([]payerUTXO(nil), candidates...)
	for _, addr := range payerAddrs {
		found := false
		for i := 0; i < len(rest); i++ {
			if rest[i].address.EncodeAddress() != addr.EncodeAddress() {
				continue
			}
			seeded = append(seeded, rest[i])
			rest = append(rest[:i], rest[i+1:]...)
			found = true
			break
		}
		if !found {
			return nil, fmt.Errorf("no confirmed utxos for payer address %s", addr.EncodeAddress())
		}
	}
	return append(seeded, rest...), nil
}

// resolveFundingCommitment builds donation or legacy hashlock commitment outputs.
func resolveFundingCommitment(params *chaincfg.Params, req PSBTRequest) (
	commitmentScript []byte,
	commitmentSats int64,
	redeemScript []byte,
	redeemScriptHash []byte,
	commitmentAddr string,
	donation *donationOutputs,
	err error,
) {
	if req.DonationAddress != nil && len(req.PixelHash) > 0 && req.CommitmentSats > 0 {
		donation, err = buildDonationOutputs(params, req.PixelHash, req.ProductPixelHash, req.DonationAddress)
		if err != nil {
			return
		}
		commitmentSats = req.CommitmentSats
		if commitmentSats < 546 {
			commitmentSats = 546
		}
		return
	}
	if len(req.PixelHash) > 0 && req.CommitmentSats > 0 {
		commitmentScript, redeemScript, redeemScriptHash, commitmentAddr, err = buildCommitmentScript(params, req.PixelHash, req.CommitmentAddress)
		if err != nil {
			return
		}
		commitmentSats = req.CommitmentSats
		if commitmentSats < 546 {
			commitmentSats = 546
		}
	}
	return
}

// selectFundingUTXOs greedily selects UTXOs until payouts + commitment + fee are covered.
func selectFundingUTXOs(candidates []payerUTXO, payoutScriptCount int, donation *donationOutputs, commitmentScript []byte, requiredValue, feeRate int64, changeAddr btcutil.Address) ([]payerUTXO, int64, error) {
	var selected []payerUTXO
	var selectedValue int64
	var estimatedInputVBytes int64
	for _, u := range candidates {
		selected = append(selected, u)
		selectedValue += u.utxo.Value
		estimatedInputVBytes += estimateInputVBytes(u.address)
		outputCount := int64(payoutScriptCount)
		if donation != nil {
			outputCount += 2
		} else if commitmentScript != nil {
			outputCount++
		}
		if changeAddr != nil && selectedValue > requiredValue {
			outputCount++
		}
		estFee := estimateFee(estimatedInputVBytes, outputCount, feeRate)
		if selectedValue >= requiredValue+estFee {
			break
		}
	}
	if selectedValue < requiredValue {
		return nil, 0, fmt.Errorf("insufficient funds: need %d sats, selected %d", requiredValue, selectedValue)
	}
	return selected, selectedValue, nil
}

// fetchInputMeta loads previous outputs for selected UTXOs.
func fetchInputMeta(client *MempoolClient, selected []payerUTXO) (meta []inputMeta, actualInputVBytes int64, err error) {
	for _, u := range selected {
		prevMsg, prevOut, ferr := client.FetchTxOutput(u.utxo.TxID, u.utxo.Vout)
		if ferr != nil {
			return nil, 0, fmt.Errorf("fetch prev output %s:%d: %w", u.utxo.TxID, u.utxo.Vout, ferr)
		}
		actualInputVBytes += estimateInputVBytesFromPkScript(prevOut.PkScript)
		meta = append(meta, inputMeta{nonWitness: prevMsg, witness: prevOut})
	}
	return meta, actualInputVBytes, nil
}

// fundingTxIDIfAllSegWit returns txid when all selected inputs are SegWit (non-malleable).
func fundingTxIDIfAllSegWit(client *MempoolClient, params *chaincfg.Params, selected []payerUTXO, tx *wire.MsgTx) string {
	for _, u := range selected {
		_, prevOut, err := client.FetchTxOutput(u.utxo.TxID, u.utxo.Vout)
		if err != nil {
			return ""
		}
		_, addrs, _, err := txscript.ExtractPkScriptAddrs(prevOut.PkScript, params)
		if err != nil {
			return ""
		}
		isSegWitInput := false
		for _, addr := range addrs {
			switch addr.(type) {
			case *btcutil.AddressWitnessPubKeyHash, *btcutil.AddressWitnessScriptHash, *btcutil.AddressTaproot:
				isSegWitInput = true
			}
		}
		if !isSegWitInput {
			return ""
		}
	}
	return tx.TxHash().String()
}

// addNextRaiseFundUTXO appends the next candidate UTXO for a raise-fund payer selection.
func addNextRaiseFundUTXO(sel *payerSelection) error {
	if sel.nextIndex >= len(sel.candidates) {
		return fmt.Errorf("insufficient funds for payer %s", sel.address.EncodeAddress())
	}
	utxo := sel.candidates[sel.nextIndex]
	sel.nextIndex++
	sel.utxos = append(sel.utxos, utxo)
	sel.selected += utxo.Value
	return nil
}

// initRaiseFundSelections builds per-payer selection state with UTXOs sorted ascending.
func initRaiseFundSelections(client *MempoolClient, payers []PayerTarget) ([]payerSelection, error) {
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
		// sort by value ascending for deterministic selection
		for i := 0; i < len(utxos); i++ {
			for j := i + 1; j < len(utxos); j++ {
				if utxos[j].Value < utxos[i].Value {
					utxos[i], utxos[j] = utxos[j], utxos[i]
				}
			}
		}
		changeScript, err := txscript.PayToAddrScript(payer.Address)
		if err != nil {
			return nil, fmt.Errorf("build change script: %w", err)
		}
		selections = append(selections, payerSelection{
			address: payer.Address, target: payer.TargetSats,
			candidates: utxos, changeScript: changeScript,
		})
	}
	return selections, nil
}
