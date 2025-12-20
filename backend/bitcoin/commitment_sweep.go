package bitcoin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

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

	_, prevOut, err := client.FetchTxOutput(txid, vout)
	if err != nil {
		return nil, fmt.Errorf("fetch commitment output: %w", err)
	}

	expectedPkScript, err := buildCommitmentP2WSHScript(params, redeemScript)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(prevOut.PkScript, expectedPkScript) {
		return nil, fmt.Errorf("commitment output script mismatch")
	}

	destScript, err := txscript.PayToAddrScript(dest)
	if err != nil {
		return nil, fmt.Errorf("destination script: %w", err)
	}

	inputVBytes := estimateHashlockInputVBytes(redeemScript, preimage)
	vbytes := int64(10) + inputVBytes + 34
	fee := vbytes * feeRate
	outputValue := prevOut.Value - fee
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
		InputSats:  prevOut.Value,
		OutputSats: outputValue,
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
