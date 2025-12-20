package bitcoin

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// PSBTRequest captures the inputs needed to craft a payout PSBT.
type PSBTRequest struct {
	PayerAddress      btcutil.Address
	TargetValueSats   int64
	PixelHash         []byte
	CommitmentSats    int64
	ContractorAddress btcutil.Address
	Payouts           []PayoutOutput
	FeeRateSatPerVB   int64
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

// BuildFundingPSBT selects confirmed UTXOs, estimates fees at the provided feerate, and builds a PSBT.
// When a pixel hash is provided, a small commitment output is added alongside the contractor payout.
func BuildFundingPSBT(client *MempoolClient, params *chaincfg.Params, req PSBTRequest) (*PSBTResult, error) {
	if req.TargetValueSats <= 0 {
		return nil, fmt.Errorf("target value must be positive")
	}
	if req.FeeRateSatPerVB < 0 {
		req.FeeRateSatPerVB = 0
	}
	if req.ContractorAddress == nil {
		return nil, fmt.Errorf("contractor address required")
	}

	utxos, err := client.ListConfirmedUTXOs(req.PayerAddress.EncodeAddress())
	if err != nil {
		return nil, err
	}
	if len(utxos) == 0 {
		return nil, fmt.Errorf("no confirmed utxos for address")
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
	if len(req.PixelHash) > 0 {
		commitmentScript, redeemScript, redeemScriptHash, commitmentAddr, err = buildCommitmentScript(params, req.PixelHash)
		if err != nil {
			return nil, err
		}
		commitmentSats = req.CommitmentSats
		if commitmentSats <= 0 {
			commitmentSats = 546
		}
		if commitmentSats < 546 {
			commitmentSats = 546
		}
	}

	requiredValue := sumAmounts(payoutAmounts) + commitmentSats

	var selected []AddressUTXO
	var selectedValue int64
	// Greedy selection: accumulate until budget+fee is covered.
	for _, u := range utxos {
		selected = append(selected, u)
		selectedValue += u.Value
		inputVBytes := int64(len(selected)) * estimateInputVBytes(req.PayerAddress)
		// Two outputs when change is expected, otherwise one.
		outputCount := int64(len(payoutScripts))
		if commitmentScript != nil {
			outputCount++
		}
		if selectedValue > requiredValue {
			outputCount++
		}
		estFee := estimateFee(inputVBytes, outputCount, req.FeeRateSatPerVB)
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

	changeScript, err := txscript.PayToAddrScript(req.PayerAddress)
	if err != nil {
		return nil, fmt.Errorf("build change script: %w", err)
	}

	outputCount := int64(len(payoutScripts))
	if commitmentScript != nil {
		outputCount++
	}
	fee := estimateFee(actualInputVBytes, outputCount, req.FeeRateSatPerVB)
	change := selectedValue - requiredValue - fee
	// Add change output if not dust.
	if change >= 546 {
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
		hash, err := chainhashFromStr(u.TxID)
		if err != nil {
			return nil, err
		}
		tx.AddTxIn(&wire.TxIn{
			PreviousOutPoint: wire.OutPoint{Hash: hash, Index: u.Vout},
		})
	}
	for i, script := range payoutScripts {
		tx.AddTxOut(&wire.TxOut{Value: payoutAmounts[i], PkScript: script})
	}
	if commitmentScript != nil {
		commitmentVout = uint32(len(tx.TxOut))
		tx.AddTxOut(&wire.TxOut{Value: commitmentSats, PkScript: commitmentScript})
	}
	if change >= 546 {
		tx.AddTxOut(&wire.TxOut{Value: change, PkScript: changeScript})
	}

	psbtBytes, err := encodePSBT(tx, meta)
	if err != nil {
		return nil, fmt.Errorf("serialize psbt: %w", err)
	}

	return &PSBTResult{
		EncodedBase64:    base64.StdEncoding.EncodeToString(psbtBytes),
		EncodedHex:       hex.EncodeToString(psbtBytes),
		FeeSats:          fee,
		ChangeSats:       change,
		SelectedSats:     selectedValue,
		PayoutScript:     payoutScripts[0],
		PayoutScripts:    payoutScripts,
		PayoutAmounts:    payoutAmounts,
		CommitmentSats:   commitmentSats,
		CommitmentScript: commitmentScript,
		CommitmentVout:   commitmentVout,
		RedeemScript:     redeemScript,
		RedeemScriptHash: redeemScriptHash,
		CommitmentAddr:   commitmentAddr,
		FundingTxID:      tx.TxHash().String(),
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

func buildCommitmentScript(params *chaincfg.Params, pixelHash []byte) ([]byte, []byte, []byte, string, error) {
	if len(pixelHash) != 32 {
		return nil, nil, nil, "", fmt.Errorf("pixel hash must be 32 bytes for P2WSH hashlock")
	}
	redeemScript, err := buildHashlockRedeemScript(pixelHash)
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
	lockHash := sha256.Sum256(pixelHash)
	builder := txscript.NewScriptBuilder()
	builder.AddOp(txscript.OP_SHA256)
	builder.AddData(lockHash[:])
	builder.AddOp(txscript.OP_EQUAL)
	return builder.Script()
}

func estimateFee(inputVBytes, outputs int64, feeRate int64) int64 {
	// Basic vsize estimate: overhead ~10 vbytes + inputVBytes + outputs*34.
	vsize := int64(10) + inputVBytes + outputs*34
	fee := vsize * feeRate
	if feeRate > 0 {
		fee += 2 * feeRate
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
