package bitcoin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// CommitmentSweepResult summarizes the built sweep transaction.
type CommitmentSweepResult struct {
	RawTxHex   string
	FeeSats    int64
	InputSats  int64
	OutputSats int64
}

// BuildCommitmentSweepTx builds a signed-less hashlock sweep transaction with the preimage witness.
func BuildCommitmentSweepTx(client *MempoolClient, params *chaincfg.Params, txid string, vout uint32, redeemScript, preimage []byte, dest btcutil.Address, feeRate int64) (*CommitmentSweepResult, error) {
	log.Printf("commitment sweep DEBUG: BuildCommitmentSweepTx called with txid=%s, vout=%d, preimage_len=%d", txid, vout, len(preimage))
	if client == nil {
		return nil, fmt.Errorf("mempool client required")
	}
	if len(redeemScript) == 0 {
		return nil, fmt.Errorf("redeem script required")
	}
	if len(preimage) == 0 {
		return nil, fmt.Errorf("preimage required")
	}
	if feeRate <= 0 {
		feeRate = 1
	}

	log.Printf("commitment sweep DEBUG: fetching txid=%s to find commitment output by script hash", txid)
	msg, err := client.FetchTx(txid)
	if err != nil {
		log.Printf("commitment sweep ERROR: failed to fetch txid=%s: %v", txid, err)
		return nil, fmt.Errorf("fetch commitment tx: %w", err)
	}
	log.Printf("commitment sweep DEBUG: fetched txid=%s successfully, num_outputs=%d", txid, len(msg.TxOut))

	// Use the provided commitment vout directly (no script hash matching needed)
	if vout >= uint32(len(msg.TxOut)) {
		return nil, fmt.Errorf("invalid commitment vout %d for tx with %d outputs", vout, len(msg.TxOut))
	}

	commitmentOutput := msg.TxOut[vout]
	if commitmentOutput == nil {
		return nil, fmt.Errorf("commitment output vout %d not found in tx %s", vout, txid)
	}

	log.Printf("commitment sweep DEBUG: using provided commitment vout=%d, value=%d", vout, commitmentOutput.Value)

	destScript, err := txscript.PayToAddrScript(dest)
	if err != nil {
		return nil, fmt.Errorf("destination script: %w", err)
	}

	inputVBytes := estimateHashlockInputVBytes(redeemScript, preimage)
	vbytes := int64(10) + inputVBytes + 34
	fee := vbytes * feeRate
	outputValue := commitmentOutput.Value - fee
	if outputValue < 546 {
		return nil, fmt.Errorf("output below dust after fee: %d sats", outputValue)
	}

	hash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return nil, fmt.Errorf("invalid txid: %w", err)
	}

	tx := wire.NewMsgTx(2)
	tx.AddTxIn(&wire.TxIn{PreviousOutPoint: wire.OutPoint{Hash: *hash, Index: vout}})
	tx.AddTxOut(&wire.TxOut{Value: outputValue, PkScript: destScript})
	tx.TxIn[0].Witness = wire.TxWitness{preimage, redeemScript}

	var buf bytes.Buffer
	if err := tx.Serialize(&buf); err != nil {
		return nil, fmt.Errorf("serialize tx: %w", err)
	}

	return &CommitmentSweepResult{
		RawTxHex:   hex.EncodeToString(buf.Bytes()),
		FeeSats:    fee,
		InputSats:  commitmentOutput.Value,
		OutputSats: outputValue,
	}, nil
}

// BuildRegularSweepTx builds a regular sweep transaction (no commitment script)
func BuildRegularSweepTx(client *MempoolClient, params *chaincfg.Params, txid string, vout uint32, redeemScript, preimage []byte, dest btcutil.Address, feeRate int64) (*CommitmentSweepResult, error) {
	if client == nil {
		return nil, fmt.Errorf("mempool client required")
	}
	if len(redeemScript) == 0 {
		return nil, fmt.Errorf("redeem script required")
	}
	if len(preimage) == 0 {
		return nil, fmt.Errorf("preimage required")
	}
	if feeRate <= 0 {
		feeRate = 1
	}

	// Get the output to sweep
	msg, err := client.FetchTx(txid)
	if err != nil {
		return nil, fmt.Errorf("fetch sweep tx: %w", err)
	}
	if vout >= uint32(len(msg.TxOut)) {
		return nil, fmt.Errorf("invalid vout %d for tx with %d outputs", vout, len(msg.TxOut))
	}

	output := msg.TxOut[vout]
	if output == nil {
		return nil, fmt.Errorf("sweep output vout %d not found in tx %s", vout, txid)
	}

	// Build regular sweep transaction (no commitment script needed)
	outputValue := output.Value
	inputVBytes := estimateRegularInputVBytes(redeemScript, preimage)
	vbytes := int64(10) + int64(len(inputVBytes)) + 34
	fee := vbytes * feeRate
	valueAfterFee := outputValue - fee
	if valueAfterFee < 546 {
		return nil, fmt.Errorf("output below dust after fee: %d sats", valueAfterFee)
	}

	hash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return nil, fmt.Errorf("invalid txid: %w", err)
	}

	tx := wire.NewMsgTx(1)
	tx.AddTxIn(&wire.TxIn{PreviousOutPoint: wire.OutPoint{Hash: *hash, Index: vout}})
	tx.AddTxOut(&wire.TxOut{Value: valueAfterFee, PkScript: redeemScript})

	var buf bytes.Buffer
	if err := tx.Serialize(&buf); err != nil {
		return nil, fmt.Errorf("serialize tx: %w", err)
	}

	return &CommitmentSweepResult{
		RawTxHex:   hex.EncodeToString(buf.Bytes()),
		FeeSats:    fee,
		InputSats:  output.Value,
		OutputSats: valueAfterFee,
	}, nil
}

func estimateRegularInputVBytes(script []byte, preimage []byte) []byte {
	// Regular sweep doesn't need commitment script, just the preimage
	return preimage
}

func isHashlockOnlyRedeemScript(script []byte) bool {
	if len(script) == 0 {
		return false
	}
	tokenizer := txscript.MakeScriptTokenizer(0, script)
	var ops []byte
	var data [][]byte
	for tokenizer.Next() {
		ops = append(ops, tokenizer.Opcode())
		data = append(data, tokenizer.Data())
	}
	if tokenizer.Err() != nil {
		return false
	}
	if len(ops) != 3 {
		return false
	}
	if ops[0] != txscript.OP_SHA256 {
		return false
	}
	if ops[2] != txscript.OP_EQUAL {
		return false
	}
	if len(data) < 2 || len(data[1]) != 32 {
		return false
	}
	return true
}

func buildCommitmentP2WSHScript(params *chaincfg.Params, redeemScript []byte) ([]byte, error) {
	hash := sha256.Sum256(redeemScript)
	addr, err := btcutil.NewAddressWitnessScriptHash(hash[:], params)
	if err != nil {
		return nil, fmt.Errorf("commitment address: %w", err)
	}
	return txscript.PayToAddrScript(addr)
}

func estimateHashlockInputVBytes(redeemScript, preimage []byte) int64 {
	witnessSize := wire.VarIntSerializeSize(2) +
		wire.VarIntSerializeSize(uint64(len(preimage))) + len(preimage) +
		wire.VarIntSerializeSize(uint64(len(redeemScript))) + len(redeemScript)
	weight := int64(41*4) + int64(witnessSize)
	return (weight + 3) / 4
}
