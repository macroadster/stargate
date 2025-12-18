package bitcoin

import (
	"bytes"
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
	ContractorAddress btcutil.Address
	FeeRateSatPerVB   int64
}

// PSBTResult summarizes the built PSBT.
type PSBTResult struct {
	EncodedBase64 string
	EncodedHex    string
	FeeSats       int64
	ChangeSats    int64
	SelectedSats  int64
	PayoutScript  []byte
}

// BuildFundingPSBT selects confirmed UTXOs, estimates fees at the provided feerate, and builds a PSBT.
// It prefers pixel-hash-based scripts when provided; otherwise uses contractor address.
func BuildFundingPSBT(client *MempoolClient, params *chaincfg.Params, req PSBTRequest) (*PSBTResult, error) {
	if req.TargetValueSats <= 0 {
		return nil, fmt.Errorf("target value must be positive")
	}
	if req.FeeRateSatPerVB < 0 {
		req.FeeRateSatPerVB = 0
	}

	utxos, err := client.ListConfirmedUTXOs(req.PayerAddress.EncodeAddress())
	if err != nil {
		return nil, err
	}
	if len(utxos) == 0 {
		return nil, fmt.Errorf("no confirmed utxos for address")
	}

	payoutScript, _, err := buildPayoutScript(params, req)
	if err != nil {
		return nil, err
	}

	var selected []AddressUTXO
	var selectedValue int64
	// Greedy selection: accumulate until budget+fee is covered.
	for _, u := range utxos {
		selected = append(selected, u)
		selectedValue += u.Value
		inputVBytes := int64(len(selected)) * estimateInputVBytes(req.PayerAddress)
		// Two outputs when change is expected, otherwise one.
		outputCount := int64(1)
		if selectedValue > req.TargetValueSats {
			outputCount = 2
		}
		estFee := estimateFee(inputVBytes, outputCount, req.FeeRateSatPerVB)
		if selectedValue >= req.TargetValueSats+estFee {
			break
		}
	}

	if selectedValue < req.TargetValueSats {
		return nil, fmt.Errorf("insufficient funds: need %d sats, selected %d", req.TargetValueSats, selectedValue)
	}

	changeScript, err := txscript.PayToAddrScript(req.PayerAddress)
	if err != nil {
		return nil, fmt.Errorf("build change script: %w", err)
	}

	inputVBytes := int64(len(selected)) * estimateInputVBytes(req.PayerAddress)
	outputCount := int64(1) // payout
	fee := estimateFee(inputVBytes, outputCount, req.FeeRateSatPerVB)
	change := selectedValue - req.TargetValueSats - fee
	// Add change output if not dust.
	if change >= 546 {
		outputCount++
		fee = estimateFee(inputVBytes, outputCount, req.FeeRateSatPerVB)
		change = selectedValue - req.TargetValueSats - fee
	}
	if change < 0 {
		return nil, fmt.Errorf("insufficient funds after fee: %d sats short", -change)
	}

	tx := wire.NewMsgTx(2)
	for _, u := range selected {
		hash, err := chainhashFromStr(u.TxID)
		if err != nil {
			return nil, err
		}
		tx.AddTxIn(&wire.TxIn{
			PreviousOutPoint: wire.OutPoint{Hash: hash, Index: u.Vout},
		})
	}
	tx.AddTxOut(&wire.TxOut{Value: req.TargetValueSats, PkScript: payoutScript})
	if change >= 546 {
		tx.AddTxOut(&wire.TxOut{Value: change, PkScript: changeScript})
	}

	var meta []inputMeta
	for _, u := range selected {
		prevMsg, prevOut, err := client.FetchTxOutput(u.TxID, u.Vout)
		if err != nil {
			return nil, fmt.Errorf("fetch prev output %s:%d: %w", u.TxID, u.Vout, err)
		}
		meta = append(meta, inputMeta{
			nonWitness: prevMsg,
			witness:    prevOut,
		})
	}

	psbtBytes, err := encodePSBT(tx, meta)
	if err != nil {
		return nil, fmt.Errorf("serialize psbt: %w", err)
	}

	return &PSBTResult{
		EncodedBase64: base64.StdEncoding.EncodeToString(psbtBytes),
		EncodedHex:    hex.EncodeToString(psbtBytes),
		FeeSats:       fee,
		ChangeSats:    change,
		SelectedSats:  selectedValue,
		PayoutScript:  payoutScript,
	}, nil
}

func buildPayoutScript(params *chaincfg.Params, req PSBTRequest) ([]byte, btcutil.Address, error) {
	if len(req.PixelHash) > 0 {
		switch len(req.PixelHash) {
		case 20:
			addr, err := btcutil.NewAddressScriptHashFromHash(req.PixelHash, params)
			if err != nil {
				return nil, nil, fmt.Errorf("pixel hash p2sh: %w", err)
			}
			script, _ := txscript.PayToAddrScript(addr)
			return script, addr, nil
		case 32:
			addr, err := btcutil.NewAddressWitnessScriptHash(req.PixelHash, params)
			if err != nil {
				return nil, nil, fmt.Errorf("pixel hash p2wsh: %w", err)
			}
			script, _ := txscript.PayToAddrScript(addr)
			return script, addr, nil
		default:
			return nil, nil, fmt.Errorf("pixel hash must be 20 or 32 bytes for script hash")
		}
	}
	if req.ContractorAddress == nil {
		return nil, nil, fmt.Errorf("contractor address required when pixel hash not provided")
	}
	script, err := txscript.PayToAddrScript(req.ContractorAddress)
	if err != nil {
		return nil, nil, fmt.Errorf("contractor script: %w", err)
	}
	return script, req.ContractorAddress, nil
}

func estimateFee(inputVBytes, outputs int64, feeRate int64) int64 {
	// Basic vsize estimate: overhead ~10 vbytes + inputVBytes + outputs*34.
	vsize := int64(10) + inputVBytes + outputs*34
	return vsize * feeRate
}

func estimateInputVBytes(addr btcutil.Address) int64 {
	switch addr.(type) {
	case *btcutil.AddressWitnessPubKeyHash:
		return 68
	case *btcutil.AddressTaproot:
		return 58
	case *btcutil.AddressScriptHash:
		return 109
	default:
		return 148 // P2PKH fallback
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
