package bitcoin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

type raiseFundMockUTXO struct {
	utxos map[string][]AddressUTXO
	txs   map[string]*wire.MsgTx
}

func newRaiseFundMockUTXO() *raiseFundMockUTXO {
	return &raiseFundMockUTXO{
		utxos: make(map[string][]AddressUTXO),
		txs:   make(map[string]*wire.MsgTx),
	}
}

func (m *raiseFundMockUTXO) ListConfirmedUTXOs(address string) ([]AddressUTXO, error) {
	return m.utxos[address], nil
}

func (m *raiseFundMockUTXO) FetchTx(txid string) (*wire.MsgTx, error) {
	tx, ok := m.txs[txid]
	if !ok {
		return nil, fmt.Errorf("unknown tx %s", txid)
	}
	return tx, nil
}

func (m *raiseFundMockUTXO) FetchTxOutput(txid string, vout uint32) (*wire.MsgTx, *wire.TxOut, error) {
	tx, err := m.FetchTx(txid)
	if err != nil {
		return nil, nil, err
	}
	if int(vout) >= len(tx.TxOut) {
		return nil, nil, fmt.Errorf("vout %d missing on %s", vout, txid)
	}
	return tx, tx.TxOut[vout], nil
}

func (m *raiseFundMockUTXO) BroadcastTx(string) (string, error) {
	return "", fmt.Errorf("broadcast not used in tests")
}

func seedAddrUTXO(t *testing.T, client *raiseFundMockUTXO, addr btcutil.Address, value int64) {
	t.Helper()
	script, err := txscript.PayToAddrScript(addr)
	if err != nil {
		t.Fatalf("payout script: %v", err)
	}
	prev := wire.NewMsgTx(2)
	var dummy chainhash.Hash
	dummy[0] = 0x01
	prev.AddTxIn(wire.NewTxIn(wire.NewOutPoint(&dummy, 0), nil, nil))
	prev.AddTxOut(wire.NewTxOut(value, script))
	txid := prev.TxHash().String()
	client.txs[txid] = prev
	var u AddressUTXO
	u.TxID = txid
	u.Vout = 0
	u.Value = value
	u.Status.Confirmed = true
	key := addr.EncodeAddress()
	client.utxos[key] = append(client.utxos[key], u)
}

func mustP2WPKH(t *testing.T, params *chaincfg.Params, fill byte) btcutil.Address {
	t.Helper()
	addr, err := btcutil.NewAddressWitnessPubKeyHash(bytes.Repeat([]byte{fill}, 20), params)
	if err != nil {
		t.Fatalf("p2wpkh: %v", err)
	}
	return addr
}

func mustP2PKH(t *testing.T, params *chaincfg.Params, fill byte) btcutil.Address {
	t.Helper()
	addr, err := btcutil.NewAddressPubKeyHash(bytes.Repeat([]byte{fill}, 20), params)
	if err != nil {
		t.Fatalf("p2pkh: %v", err)
	}
	return addr
}

func unsignedTxFromPSBTHex(t *testing.T, encodedHex string) *wire.MsgTx {
	t.Helper()
	raw, err := hex.DecodeString(encodedHex)
	if err != nil {
		t.Fatalf("psbt hex: %v", err)
	}
	if len(raw) < 6 || !bytes.Equal(raw[:5], []byte{0x70, 0x73, 0x62, 0x74, 0xff}) {
		t.Fatal("not a PSBT")
	}
	r := bytes.NewReader(raw[5:])
	key, err := wire.ReadVarBytes(r, 0, 10, "psbt key")
	if err != nil {
		t.Fatalf("psbt key: %v", err)
	}
	if len(key) != 1 || key[0] != 0x00 {
		t.Fatalf("expected unsigned-tx key, got %x", key)
	}
	val, err := wire.ReadVarBytes(r, 0, 100_000, "unsigned tx")
	if err != nil {
		t.Fatalf("psbt tx: %v", err)
	}
	tx := wire.NewMsgTx(2)
	if err := tx.Deserialize(bytes.NewReader(val)); err != nil {
		t.Fatalf("deserialize unsigned tx: %v", err)
	}
	return tx
}

func opReturnPayloadLen(script []byte) int {
	if len(script) < 2 || script[0] != txscript.OP_RETURN {
		return 0
	}
	tokenizer := txscript.MakeScriptTokenizer(0, script[1:])
	var n int
	for tokenizer.Next() {
		n += len(tokenizer.Data())
	}
	return n
}

// TestBuildRaiseFundPSBTTable covers combined raise-fund × proof/txid variants.
func TestBuildRaiseFundPSBTTable(t *testing.T) {
	params := &chaincfg.TestNet4Params
	wishHash := bytes.Repeat([]byte{0xab}, 32)
	stegoHash := bytes.Repeat([]byte{0xcd}, 32)
	const payoutA, payoutB int64 = 10_000, 20_000

	type tc struct {
		name            string
		legacyB         bool
		stego           bool
		commitSats      int64
		donation        bool
		wantPayload     int
		wantCommitSats  int64
		wantOPReturn    bool
		wantHashlock    bool
		wantFundingTxID bool
	}
	cases := []tc{
		{
			name:  "funding+stego",
			stego: true, commitSats: 1000, donation: true,
			wantPayload: 64, wantCommitSats: 1000, wantOPReturn: true, wantFundingTxID: true,
		},
		{
			name:       "funding-no-stego",
			commitSats: 1000, donation: true,
			wantPayload: 32, wantCommitSats: 1000, wantOPReturn: true, wantFundingTxID: true,
		},
		{
			name:  "product/zero",
			stego: true, commitSats: 0, donation: true,
			wantPayload: 0, wantCommitSats: 0, wantOPReturn: false, wantHashlock: false, wantFundingTxID: true,
		},
		{
			name:  "all-SegWit",
			stego: true, commitSats: 1000, donation: true,
			wantPayload: 64, wantCommitSats: 1000, wantOPReturn: true, wantFundingTxID: true,
		},
		{
			name:    "mixed-legacy",
			legacyB: true, stego: true, commitSats: 1000, donation: true,
			wantPayload: 64, wantCommitSats: 1000, wantOPReturn: true, wantFundingTxID: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payerA := mustP2WPKH(t, params, 0x11)
			var payerB btcutil.Address
			if c.legacyB {
				payerB = mustP2PKH(t, params, 0x22)
			} else {
				payerB = mustP2WPKH(t, params, 0x22)
			}
			fundraiser := mustP2WPKH(t, params, 0x33)
			client := newRaiseFundMockUTXO()
			seedAddrUTXO(t, client, payerA, 50_000)
			seedAddrUTXO(t, client, payerB, 80_000)

			req := PSBTRequest{
				Payouts: []PayoutOutput{
					{Address: fundraiser, ValueSats: payoutA},
					{Address: fundraiser, ValueSats: payoutB},
				},
				PixelHash:         wishHash,
				CommitmentSats:    c.commitSats,
				CommitmentAddress: fundraiser,
				FeeRateSatPerVB:   2,
			}
			if c.donation {
				req.DonationAddress = fundraiser
			}
			if c.stego {
				req.ProductPixelHash = stegoHash
			}

			res, err := BuildRaiseFundPSBT(client, params, []PayerTarget{
				{Address: payerA, TargetSats: payoutA},
				{Address: payerB, TargetSats: payoutB},
			}, req)
			if err != nil {
				t.Fatalf("BuildRaiseFundPSBT: %v", err)
			}
			if c.wantOPReturn {
				if got := opReturnPayloadLen(res.OPReturnScript); got != c.wantPayload {
					t.Fatalf("OP_RETURN payload %d, want %d", got, c.wantPayload)
				}
				wish, stego, ok := parseOPReturnHashes(res.OPReturnScript)
				if !ok {
					t.Fatal("parseOPReturnHashes rejected raise-fund OP_RETURN")
				}
				if wish != hex.EncodeToString(wishHash) {
					t.Fatalf("wish hash %s", wish)
				}
				if c.stego && c.wantPayload == 64 && stego != hex.EncodeToString(stegoHash) {
					t.Fatalf("stego hash %s (wish-only OP_RETURN despite 32-byte ProductPixelHash)", stego)
				}
				if !c.stego && stego != "" {
					t.Fatalf("expected wish-only OP_RETURN, got stego %s", stego)
				}
			} else if len(res.OPReturnScript) != 0 {
				t.Fatal("product/zero must not emit OP_RETURN")
			}
			if res.CommitmentSats != c.wantCommitSats {
				t.Fatalf("commitment sats=%d want %d (product must not force dust)", res.CommitmentSats, c.wantCommitSats)
			}
			if c.wantHashlock && len(res.RedeemScript) == 0 {
				t.Fatal("expected hashlock redeem script")
			}
			if !c.wantHashlock && !c.wantOPReturn && len(res.RedeemScript) != 0 {
				t.Fatal("product/zero must not emit a hashlock")
			}
			tx := unsignedTxFromPSBTHex(t, res.EncodedHex)
			if c.wantFundingTxID {
				if res.FundingTxID != tx.TxHash().String() {
					t.Fatalf("FundingTxID=%q want unsigned tx hash %s", res.FundingTxID, tx.TxHash())
				}
			} else if res.FundingTxID != "" {
				t.Fatalf("mixed legacy must leave FundingTxID empty, got %s", res.FundingTxID)
			}
		})
	}
}

func TestBuildRaiseFundPSBTWithoutDonationFallsToHashlock(t *testing.T) {
	params := &chaincfg.TestNet4Params
	payer := mustP2WPKH(t, params, 0x44)
	fundraiser := mustP2WPKH(t, params, 0x55)
	client := newRaiseFundMockUTXO()
	seedAddrUTXO(t, client, payer, 50_000)

	res, err := BuildRaiseFundPSBT(client, params, []PayerTarget{
		{Address: payer, TargetSats: 10_000},
	}, PSBTRequest{
		Payouts:           []PayoutOutput{{Address: fundraiser, ValueSats: 10_000}},
		PixelHash:         bytes.Repeat([]byte{0xef}, 32),
		CommitmentSats:    1_000,
		CommitmentAddress: fundraiser,
		FeeRateSatPerVB:   1,
	})
	if err != nil {
		t.Fatalf("BuildRaiseFundPSBT: %v", err)
	}
	if len(res.OPReturnScript) != 0 {
		t.Fatal("legacy hashlock path should not emit OP_RETURN")
	}
	if len(res.CommitmentScript) == 0 {
		t.Fatal("expected hashlock commitment script")
	}
}

// TestBuildHashlockRedeemScript verifies that the commitment script is constructed correctly.
// It specifically checks that we are hashing the preimage in the script (OP_SHA256 <HASH> OP_EQUAL),
// rather than using the raw preimage (OP_SHA256 <PREIMAGE> OP_EQUAL).
func TestBuildHashlockRedeemScript(t *testing.T) {
	// 1. Setup a sample preimage (simulating a visible pixel hash)
	preimageHex := "c228825a2730c5849f2baae8d46e9088efa80dea069aa5d45c1fa26f26e2c9cb"
	preimage, err := hex.DecodeString(preimageHex)
	if err != nil {
		t.Fatalf("failed to decode preimage: %v", err)
	}

	// 2. Build the script
	script, err := buildHashlockRedeemScript(preimage)
	if err != nil {
		t.Fatalf("buildHashlockRedeemScript failed: %v", err)
	}

	// 3. Expected construction: OP_SHA256 <SHA256(preimage)> OP_EQUAL
	// OP_SHA256 = 0xa8
	// Push 32 bytes = 0x20
	// OP_EQUAL = 0x87

	// Calculate expected hash (SHA256 of the preimage)
	expectedHash := sha256.Sum256(preimage)

	// Basic length check: 1 byte OP + 1 byte len + 32 bytes hash + 1 byte OP = 35 bytes
	if len(script) != 35 {
		t.Errorf("expected script length 35, got %d", len(script))
	}

	// Verify OP_SHA256
	if script[0] != txscript.OP_SHA256 {
		t.Errorf("expected OP_SHA256 (0xa8) at index 0, got 0x%x", script[0])
	}

	// Verify push data length
	if script[1] != 0x20 {
		t.Errorf("expected push data length 0x20 at index 1, got 0x%x", script[1])
	}

	// Verify the data pushed is the HASH of the preimage, NOT the preimage itself
	data := script[2:34]
	if !bytes.Equal(data, expectedHash[:]) {
		t.Errorf("Script data mismatch.\nExpected (SHA256 of preimage): %x\nGot (in script): %x", expectedHash, data)

		// Specific regression check
		if bytes.Equal(data, preimage) {
			t.Error("CRITICAL FAILURE: Script contains raw preimage! This causes 'Witness program hash mismatch'.")
		}
	}

	// Verify OP_EQUAL
	if script[34] != txscript.OP_EQUAL {
		t.Errorf("expected OP_EQUAL (0x87) at index 34, got 0x%x", script[34])
	}

	t.Logf("Successfully verified hashlock script construction.")
	t.Logf("Script: %x", script)
}

func TestAllPayerSelectionsAreSegWit(t *testing.T) {
	params := &chaincfg.TestNet4Params
	client := newRaiseFundMockUTXO()
	if !allPayerSelectionsAreSegWit(nil, client, params) {
		t.Fatal("empty selections are vacuously SegWit")
	}
}

func fundingPSBTForAddr(t *testing.T, addr btcutil.Address) *PSBTResult {
	t.Helper()
	params := &chaincfg.TestNet4Params
	dest := mustP2WPKH(t, params, 0x99)
	client := newRaiseFundMockUTXO()
	seedAddrUTXO(t, client, addr, 50_000)
	res, err := BuildFundingPSBT(client, params, PSBTRequest{
		PayerAddress:      addr,
		TargetValueSats:   10_000,
		ContractorAddress: dest,
		FeeRateSatPerVB:   1,
	})
	if err != nil {
		t.Fatalf("BuildFundingPSBT: %v", err)
	}
	return res
}

func TestTxIDPreCalculationSegWit(t *testing.T) {
	params := &chaincfg.TestNet4Params
	res := fundingPSBTForAddr(t, mustP2WPKH(t, params, 0x11))
	tx := unsignedTxFromPSBTHex(t, res.EncodedHex)
	if res.FundingTxID != tx.TxHash().String() {
		t.Fatalf("SegWit FundingTxID=%q want %s", res.FundingTxID, tx.TxHash())
	}
}

func TestBuildFundingPSBTKeepsOPReturnInUnsignedHex(t *testing.T) {
	params := &chaincfg.TestNet4Params
	payer := mustP2WPKH(t, params, 0x11)
	dest := mustP2WPKH(t, params, 0x99)
	donation := mustP2WPKH(t, params, 0x33)
	client := newRaiseFundMockUTXO()
	seedAddrUTXO(t, client, payer, 50_000)
	wishHash := bytes.Repeat([]byte{0xab}, 32)

	res, err := BuildFundingPSBT(client, params, PSBTRequest{
		PayerAddress:    payer,
		Payouts:         []PayoutOutput{{Address: dest, ValueSats: 10_000}},
		PixelHash:       wishHash,
		CommitmentSats:  1000,
		DonationAddress: donation,
		FeeRateSatPerVB: 1,
		ChangeAddress:   payer,
	})
	if err != nil {
		t.Fatalf("BuildFundingPSBT: %v", err)
	}
	if len(res.OPReturnScript) == 0 || res.OPReturnScript[0] != txscript.OP_RETURN {
		t.Fatal("result missing OP_RETURN script")
	}
	tx := unsignedTxFromPSBTHex(t, res.EncodedHex)
	found := false
	for _, txOut := range tx.TxOut {
		if len(txOut.PkScript) > 0 && txOut.PkScript[0] == txscript.OP_RETURN {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("unsigned hex has %d outputs and no OP_RETURN (payout+change scar)", len(tx.TxOut))
	}
	if res.CommitmentSats != 1000 {
		t.Fatalf("commitment sats=%d", res.CommitmentSats)
	}
}

func TestTxIDPreCalculationLegacy(t *testing.T) {
	params := &chaincfg.TestNet4Params
	res := fundingPSBTForAddr(t, mustP2PKH(t, params, 0x44))
	if res.FundingTxID != "" {
		t.Fatalf("P2PKH FundingTxID must be empty, got %s", res.FundingTxID)
	}
}

func TestZeroCostFundingIntegration(t *testing.T) {
	params := &chaincfg.TestNet4Params
	seg := fundingPSBTForAddr(t, mustP2WPKH(t, params, 0x11))
	if seg.FundingTxID == "" {
		t.Fatal("SegWit funding should precompute txid")
	}
	legacy := fundingPSBTForAddr(t, mustP2PKH(t, params, 0x44))
	if legacy.FundingTxID != "" {
		t.Fatal("legacy funding must not precompute a malleable txid")
	}
}
