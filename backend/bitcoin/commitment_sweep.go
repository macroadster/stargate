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

	msg, err := client.FetchTx(txid)
	if err != nil {
		log.Printf("commitment sweep ERROR: failed to fetch txid=%s: %v", txid, err)
		return nil, fmt.Errorf("fetch commitment tx: %w", err)
	}

	// Use the provided commitment vout directly (no script hash matching needed)
	if vout >= uint32(len(msg.TxOut)) {
		return nil, fmt.Errorf("invalid commitment vout %d for tx with %d outputs", vout, len(msg.TxOut))
	}

	commitmentOutput := msg.TxOut[vout]
	if commitmentOutput == nil {
		return nil, fmt.Errorf("commitment output vout %d not found in tx %s", vout, txid)
	}

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

// RecommitSweepResult summarizes the built re-commitment transaction.
type RecommitSweepResult struct {
	RawTxHex         string
	FeeSats          int64
	InputSats        int64
	OutputSats       int64
	RedeemScript     []byte // product-hash hashlock redeem script
	RedeemScriptHash []byte // SHA256 of the redeem script
	P2WSHAddr        string // product-hash P2WSH address
	Vout             uint32 // output index of the product hashlock
}

// BuildRecommitSweepTx sweeps a wish-hash hashlock UTXO and re-locks the funds
// into a new P2WSH hashlock keyed to the product image hash. This is phase 1 of
// the two-phase donation sweep: wish-hashlock → product-hashlock → donation addr.
func BuildRecommitSweepTx(client *MempoolClient, params *chaincfg.Params, txid string, vout uint32, wishRedeemScript, wishPreimage, productHash []byte, feeRate int64) (*RecommitSweepResult, error) {
	if client == nil {
		return nil, fmt.Errorf("mempool client required")
	}
	if len(wishRedeemScript) == 0 {
		return nil, fmt.Errorf("wish redeem script required")
	}
	if len(wishPreimage) == 0 {
		return nil, fmt.Errorf("wish preimage required")
	}
	if len(productHash) != 32 {
		return nil, fmt.Errorf("product hash must be 32 bytes")
	}
	if feeRate <= 0 {
		feeRate = 1
	}

	msg, err := client.FetchTx(txid)
	if err != nil {
		return nil, fmt.Errorf("fetch commitment tx: %w", err)
	}
	if vout >= uint32(len(msg.TxOut)) {
		return nil, fmt.Errorf("invalid commitment vout %d for tx with %d outputs", vout, len(msg.TxOut))
	}
	commitmentOutput := msg.TxOut[vout]
	if commitmentOutput == nil {
		return nil, fmt.Errorf("commitment output vout %d not found in tx %s", vout, txid)
	}

	// Build product-hash hashlock redeem script and P2WSH output.
	productRedeemScript, err := buildHashlockRedeemScript(productHash)
	if err != nil {
		return nil, fmt.Errorf("build product redeem script: %w", err)
	}
	productScriptHash := sha256.Sum256(productRedeemScript)
	productAddr, err := btcutil.NewAddressWitnessScriptHash(productScriptHash[:], params)
	if err != nil {
		return nil, fmt.Errorf("product P2WSH address: %w", err)
	}
	destScript, err := txscript.PayToAddrScript(productAddr)
	if err != nil {
		return nil, fmt.Errorf("product dest script: %w", err)
	}

	inputVBytes := estimateHashlockInputVBytes(wishRedeemScript, wishPreimage)
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
	tx.TxIn[0].Witness = wire.TxWitness{wishPreimage, wishRedeemScript}

	var buf bytes.Buffer
	if err := tx.Serialize(&buf); err != nil {
		return nil, fmt.Errorf("serialize tx: %w", err)
	}

	return &RecommitSweepResult{
		RawTxHex:         hex.EncodeToString(buf.Bytes()),
		FeeSats:          fee,
		InputSats:        commitmentOutput.Value,
		OutputSats:       outputValue,
		RedeemScript:     productRedeemScript,
		RedeemScriptHash: productScriptHash[:],
		P2WSHAddr:        productAddr.EncodeAddress(),
		Vout:             0, // single output tx
	}, nil
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
