package mcp

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	"stargate-backend/starlight"
	"stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

type psbtMockUTXO struct {
	utxos map[string][]bitcoin.AddressUTXO
	txs   map[string]*wire.MsgTx
}

func newPSBTMockUTXO() *psbtMockUTXO {
	return &psbtMockUTXO{
		utxos: make(map[string][]bitcoin.AddressUTXO),
		txs:   make(map[string]*wire.MsgTx),
	}
}

func (m *psbtMockUTXO) ListConfirmedUTXOs(address string) ([]bitcoin.AddressUTXO, error) {
	return m.utxos[address], nil
}

func (m *psbtMockUTXO) FetchTx(txid string) (*wire.MsgTx, error) {
	tx, ok := m.txs[txid]
	if !ok {
		return nil, fmt.Errorf("unknown tx %s", txid)
	}
	return tx, nil
}

func (m *psbtMockUTXO) FetchTxOutput(txid string, vout uint32) (*wire.MsgTx, *wire.TxOut, error) {
	tx, err := m.FetchTx(txid)
	if err != nil {
		return nil, nil, err
	}
	if int(vout) >= len(tx.TxOut) {
		return nil, nil, fmt.Errorf("vout %d missing on %s", vout, txid)
	}
	return tx, tx.TxOut[vout], nil
}

func (m *psbtMockUTXO) BroadcastTx(string) (string, error) {
	return "", fmt.Errorf("broadcast not used in tests")
}

func seedPSBTUTXO(t *testing.T, client *psbtMockUTXO, addr btcutil.Address, value int64) {
	t.Helper()
	script, err := txscript.PayToAddrScript(addr)
	if err != nil {
		t.Fatalf("script: %v", err)
	}
	prev := wire.NewMsgTx(2)
	var dummy chainhash.Hash
	dummy[0] = 0x01
	prev.AddTxIn(wire.NewTxIn(wire.NewOutPoint(&dummy, 0), nil, nil))
	prev.AddTxOut(wire.NewTxOut(value, script))
	txid := prev.TxHash().String()
	client.txs[txid] = prev
	var u bitcoin.AddressUTXO
	u.TxID = txid
	u.Vout = 0
	u.Value = value
	u.Status.Confirmed = true
	client.utxos[addr.EncodeAddress()] = append(client.utxos[addr.EncodeAddress()], u)
}

func mustTestP2WPKH(t *testing.T, fill byte) btcutil.Address {
	t.Helper()
	addr, err := btcutil.NewAddressWitnessPubKeyHash(bytes.Repeat([]byte{fill}, 20), &chaincfg.TestNet4Params)
	if err != nil {
		t.Fatalf("p2wpkh: %v", err)
	}
	return addr
}

func mustTestP2PKH(t *testing.T, fill byte) btcutil.Address {
	t.Helper()
	addr, err := btcutil.NewAddressPubKeyHash(bytes.Repeat([]byte{fill}, 20), &chaincfg.TestNet4Params)
	if err != nil {
		t.Fatalf("p2pkh: %v", err)
	}
	return addr
}

func unsignedTxFromPSBTHexMCP(t *testing.T, encodedHex string) *wire.MsgTx {
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

func TestHandleBuildPSBTKeepsOPReturnCommitment(t *testing.T) {
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	t.Setenv("STARLIGHT_DONATION_ADDRESS", "")

	payer := mustTestP2WPKH(t, 0x11)
	contractor := mustTestP2WPKH(t, 0x99)
	pixelHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	contractID := "wish-" + pixelHash

	store := scstore.NewMemoryStore(72 * time.Hour)
	ctx := context.Background()
	if err := store.UpsertContractWithTasks(ctx, smart_contract.Contract{
		ContractID:      contractID,
		Title:           "Commitment scar",
		TotalBudgetSats: 10_000,
		Status:          "active",
	}, []smart_contract.Task{{
		TaskID:           contractID + "-task-1",
		ContractID:       contractID,
		Title:            "Approved payout",
		BudgetSats:       10_000,
		Status:           "approved",
		ContractorWallet: contractor.EncodeAddress(),
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	server := NewHTTPMCPServer(store, walletValidator{wallet: payer.EncodeAddress()}, nil, &services.IngestionService{}, &starlight.ScannerManager{}, nil, auth.NewChallengeStore(10*time.Minute))
	mock := newPSBTMockUTXO()
	seedPSBTUTXO(t, mock, payer, 50_000)
	server.utxoClient = mock

	call := func(t *testing.T, args map[string]interface{}) map[string]interface{} {
		t.Helper()
		body, err := json.Marshal(MCPRequest{Tool: "build_psbt", Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-API-Key", "test-key")
		server.handleToolCall(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var resp MCPResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !resp.Success {
			t.Fatalf("build_psbt failed: %s (%s)", resp.Error, resp.Message)
		}
		raw, _ := json.Marshal(resp.Result)
		var out map[string]interface{}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("result: %v", err)
		}
		return out
	}

	// The live scar: omit commitment_sats (or pass it as a Go/JSON int).
	// Official handleBuildPSBT used to emit payout+change only.
	for _, name := range []string{"omitted", "int", "float"} {
		t.Run(name, func(t *testing.T) {
			args := map[string]interface{}{"pixel_hash": pixelHash}
			switch name {
			case "int":
				args["commitment_sats"] = 1000
			case "float":
				args["commitment_sats"] = float64(1000)
			}
			out := call(t, args)
			scriptHex, _ := out["op_return_script"].(string)
			if scriptHex == "" {
				t.Fatalf("missing op_return_script: %#v", out)
			}
			script, err := hex.DecodeString(scriptHex)
			if err != nil || len(script) == 0 || script[0] != 0x6a {
				t.Fatalf("op_return_script %q is not OP_RETURN", scriptHex)
			}
			psbtHex, _ := out["psbt_hex"].(string)
			tx := unsignedTxFromPSBTHexMCP(t, psbtHex)
			found := false
			for _, txOut := range tx.TxOut {
				if len(txOut.PkScript) > 0 && txOut.PkScript[0] == txscript.OP_RETURN {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("signed/unsigned hex has %d outputs and no OP_RETURN", len(tx.TxOut))
			}
			if out["commitment_sats"] != float64(1000) {
				t.Fatalf("commitment_sats=%v", out["commitment_sats"])
			}
		})
	}
}

func TestHandleBuildPSBTPayerAddressesCoversSmallP2WPKH(t *testing.T) {
	// Live scar (probe 44f7844c): API-key P2WPKH holds only the 1000-sat payout
	// leftover. Required is 1000 payout + 546 commitment. Without payer_addresses
	// MCP fails need 1546 / selected 1000. REST already accepted the extra P2PKH.
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	t.Setenv("STARLIGHT_DONATION_ADDRESS", "")

	payer := mustTestP2WPKH(t, 0x11)
	legacy := mustTestP2PKH(t, 0x22)
	contractor := mustTestP2WPKH(t, 0x99)
	pixelHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	contractID := "wish-" + pixelHash

	store := scstore.NewMemoryStore(72 * time.Hour)
	ctx := context.Background()
	if err := store.UpsertContractWithTasks(ctx, smart_contract.Contract{
		ContractID:      contractID,
		Title:           "Payer address scar",
		TotalBudgetSats: 1000,
		Status:          "active",
	}, []smart_contract.Task{{
		TaskID:           contractID + "-task-1",
		ContractID:       contractID,
		Title:            "Approved payout",
		BudgetSats:       1000,
		Status:           "approved",
		ContractorWallet: contractor.EncodeAddress(),
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	server := NewHTTPMCPServer(store, walletValidator{wallet: payer.EncodeAddress()}, nil, &services.IngestionService{}, &starlight.ScannerManager{}, nil, auth.NewChallengeStore(10*time.Minute))
	mock := newPSBTMockUTXO()
	seedPSBTUTXO(t, mock, payer, 1000)
	seedPSBTUTXO(t, mock, legacy, 3754)
	server.utxoClient = mock

	call := func(t *testing.T, args map[string]interface{}) (map[string]interface{}, MCPResponse) {
		t.Helper()
		body, err := json.Marshal(MCPRequest{Tool: "build_psbt", Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-API-Key", "test-key")
		server.handleToolCall(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var resp MCPResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		var out map[string]interface{}
		if resp.Result != nil {
			raw, _ := json.Marshal(resp.Result)
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatalf("result: %v", err)
			}
		}
		return out, resp
	}

	args := map[string]interface{}{
		"pixel_hash":      pixelHash,
		"commitment_sats": 546,
	}
	_, resp := call(t, args)
	if resp.Success {
		t.Fatal("expected single-payer build to fail when P2WPKH is only 1000 sats")
	}
	if !strings.Contains(resp.Error, "need 1546") && !strings.Contains(resp.Message, "need 1546") && !strings.Contains(resp.Error, "selected 1000") {
		t.Fatalf("expected need 1546 / selected 1000, got error=%q message=%q", resp.Error, resp.Message)
	}

	args["payer_addresses"] = []interface{}{payer.EncodeAddress(), legacy.EncodeAddress()}
	out, resp := call(t, args)
	if !resp.Success {
		t.Fatalf("build_psbt with payer_addresses failed: %s (%s)", resp.Error, resp.Message)
	}
	scriptHex, _ := out["op_return_script"].(string)
	script, err := hex.DecodeString(scriptHex)
	if err != nil || len(script) == 0 || script[0] != 0x6a {
		t.Fatalf("op_return_script %q is not OP_RETURN", scriptHex)
	}
	psbtHex, _ := out["psbt_hex"].(string)
	tx := unsignedTxFromPSBTHexMCP(t, psbtHex)
	found := false
	for _, txOut := range tx.TxOut {
		if len(txOut.PkScript) > 0 && txOut.PkScript[0] == txscript.OP_RETURN {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("unsigned hex has %d outputs and no OP_RETURN", len(tx.TxOut))
	}
	selected, _ := out["selected_sats"].(float64)
	if selected < 1546 {
		t.Fatalf("selected_sats=%v, want at least 1546", selected)
	}
	rawPayers, _ := json.Marshal(out["payer_addresses"])
	if !bytes.Contains(rawPayers, []byte(legacy.EncodeAddress())) {
		t.Fatalf("payer_addresses missing legacy: %s", rawPayers)
	}
}

func TestMcpStringSliceAcceptsJSONArray(t *testing.T) {
	got, ok := mcpStringSlice([]interface{}{"a", "b"})
	if !ok || len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("[]interface{}: %v %v", got, ok)
	}
	got, ok = mcpStringSlice([]string{"c"})
	if !ok || len(got) != 1 || got[0] != "c" {
		t.Fatalf("[]string: %v %v", got, ok)
	}
	if _, ok := mcpStringSlice("nope"); ok {
		t.Fatal("string should not parse as slice")
	}
}

func TestMcpInt64AcceptsJSONIntegerAndFloat(t *testing.T) {
	if v, ok := mcpInt64(1000); !ok || v != 1000 {
		t.Fatalf("int: %d %v", v, ok)
	}
	if v, ok := mcpInt64(float64(1000)); !ok || v != 1000 {
		t.Fatalf("float64: %d %v", v, ok)
	}
	if v, ok := mcpInt64(json.Number("1000")); !ok || v != 1000 {
		t.Fatalf("json.Number: %d %v", v, ok)
	}
	if _, ok := mcpInt64("1000"); ok {
		t.Fatal("string should not parse here")
	}
}
