package bitcoin

import (
	"context"
	"testing"

	"github.com/btcsuite/btcd/wire"
)

type mockChain struct {
	height int64
	hash   string
	hex    string
}

func (m *mockChain) Network() string { return "testnet4" }
func (m *mockChain) Ready(ctx context.Context) error { return nil }
func (m *mockChain) Synced(ctx context.Context) (bool, error) { return true, nil }
func (m *mockChain) GetTipHeight(ctx context.Context) (int64, error) { return m.height, nil }
func (m *mockChain) GetBlockHash(ctx context.Context, height int64) (string, error) { return m.hash, nil }
func (m *mockChain) GetRawBlockHex(ctx context.Context, height int64) (string, error) { return m.hex, nil }
func (m *mockChain) GetRawTx(ctx context.Context, txid string) (*wire.MsgTx, error) {
	return nil, nil
}
func (m *mockChain) GetTxStatus(ctx context.Context, txid string) (int64, bool, error) {
	return 0, false, nil
}
func (m *mockChain) NodeStatus(ctx context.Context) (map[string]any, error) {
	return map[string]any{"backend": "mock"}, nil
}
func (m *mockChain) Close() error { return nil }
func (m *mockChain) ListConfirmedUTXOs(address string) ([]AddressUTXO, error) { return nil, nil }
func (m *mockChain) FetchTx(txid string) (*wire.MsgTx, error) { return nil, nil }
func (m *mockChain) FetchTxOutput(txid string, vout uint32) (*wire.MsgTx, *wire.TxOut, error) {
	return nil, nil, nil
}
func (m *mockChain) BroadcastTx(rawHex string) (string, error) { return "", nil }

func TestRawBlockClientPrefersChainBackend(t *testing.T) {
	rbc := NewRawBlockClient("testnet4")
	mock := &mockChain{height: 100, hash: "abcd", hex: "010203"}
	rbc.SetChainBackend(mock)
	got, err := rbc.GetRawBlockHex(100)
	if err != nil {
		t.Fatal(err)
	}
	if got != "010203" {
		t.Fatalf("got %q", got)
	}
}

func TestBitcoinNodeClientUsesChain(t *testing.T) {
	c := NewBitcoinNodeClient("https://example.invalid")
	c.SetChainBackend(&mockChain{height: 42, hash: "hh"})
	h, err := c.GetCurrentHeight()
	if err != nil || h != 42 {
		t.Fatalf("height %d %v", h, err)
	}
	if !c.TestConnection() {
		t.Fatal("expected ready")
	}
}
