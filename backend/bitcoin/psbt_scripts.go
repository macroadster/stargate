package bitcoin

import (
	"bytes"
	"crypto/sha256"
	"fmt"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

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

// buildDonationOutputs builds a direct P2WPKH donation output and an OP_RETURN
// proof output.  The OP_RETURN payload is:
//
//	wish_hash(32) || stego_hash(32)  — 64 bytes (wish + stego image)
//	wish_hash(32)                    — 32 bytes (wish only, no stego)
//
// The sandbox_hash is carried inside the stego v2 JSON payload, not on-chain.
// Peers find the stego image by its on-chain hash, extract the embedded JSON,
// and read sandbox_hash from there.  This keeps OP_RETURN within Bitcoin's
// 80-byte recommendation.
func buildDonationOutputs(params *chaincfg.Params, wishHash, stegoHash []byte, donationAddr btcutil.Address) (*donationOutputs, error) {
	if donationAddr == nil {
		return nil, fmt.Errorf("donation address required")
	}

	// Build P2WPKH script for donation address.
	donationScript, err := txscript.PayToAddrScript(donationAddr)
	if err != nil {
		return nil, fmt.Errorf("donation script: %w", err)
	}

	// Build OP_RETURN: wish_hash(32) [|| stego_hash(32)].
	var payload []byte
	if len(wishHash) == 32 {
		payload = append(payload, wishHash...)
	}
	if len(stegoHash) == 32 {
		payload = append(payload, stegoHash...)
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("at least one 32-byte hash required for OP_RETURN")
	}

	opReturnBuilder := txscript.NewScriptBuilder()
	opReturnBuilder.AddOp(txscript.OP_RETURN)
	opReturnBuilder.AddData(payload)
	opReturnScript, err := opReturnBuilder.Script()
	if err != nil {
		return nil, fmt.Errorf("op_return script: %w", err)
	}

	return &donationOutputs{
		donationScript: donationScript,
		donationAddr:   donationAddr.EncodeAddress(),
		opReturnScript: opReturnScript,
	}, nil
}

// buildCommitmentScript is kept for backward compatibility with code that
// still references the old hashlock path.  New code should use buildDonationOutputs.
func buildCommitmentScript(params *chaincfg.Params, pixelHash []byte, commitmentAddress btcutil.Address) ([]byte, []byte, []byte, string, error) {
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
