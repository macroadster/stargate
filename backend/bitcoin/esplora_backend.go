package bitcoin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/btcsuite/btcd/wire"
)

// EsploraBackend is an HTTP explorer adapter used only when BTCD_MODE=off.
// Production should use BtcdBackend (local full node).
type EsploraBackend struct {
	node    *BitcoinNodeClient
	mempool *MempoolClient
	raw     *RawBlockClient
	network string
}

// NewEsploraBackend builds a backend from network explorer URLs.
func NewEsploraBackend(network string) *EsploraBackend {
	if network == "" {
		network = GetCurrentNetwork()
	}
	cfg := GetNetworkConfig(network)
	node := NewBitcoinNodeClient(cfg.BaseURL)
	// Align MEMPOOL base with network when not overridden.
	if os.Getenv("MEMPOOL_API_BASE") == "" {
		_ = os.Setenv("MEMPOOL_API_BASE", cfg.BaseURL)
	}
	return &EsploraBackend{
		node:    node,
		mempool: NewMempoolClient(),
		raw:     NewRawBlockClient(network),
		network: network,
	}
}

func (e *EsploraBackend) Network() string { return e.network }

func (e *EsploraBackend) Ready(ctx context.Context) error {
	if e.node.TestConnection() {
		return nil
	}
	return fmt.Errorf("esplora not reachable at %s", e.node.GetNodeURL())
}

func (e *EsploraBackend) Synced(ctx context.Context) (bool, error) {
	return e.Ready(ctx) == nil, nil
}

func (e *EsploraBackend) GetTipHeight(ctx context.Context) (int64, error) {
	return e.node.GetCurrentHeight()
}

func (e *EsploraBackend) GetBlockHash(ctx context.Context, height int64) (string, error) {
	return e.node.GetBlockHash(int(height))
}

func (e *EsploraBackend) GetRawBlockHex(ctx context.Context, height int64) (string, error) {
	return e.raw.GetRawBlockHex(height)
}

func (e *EsploraBackend) GetRawTx(ctx context.Context, txid string) (*wire.MsgTx, error) {
	return e.mempool.FetchTx(txid)
}

func (e *EsploraBackend) GetTxStatus(ctx context.Context, txid string) (int64, bool, error) {
	url := fmt.Sprintf("%s/tx/%s", strings.TrimSpace(e.node.baseURL), txid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, false, err
	}
	resp, err := e.node.httpClient.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return 0, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return 0, false, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, false, err
	}
	var txData map[string]any
	if err := json.Unmarshal(body, &txData); err != nil {
		return 0, false, err
	}
	statusMap, _ := txData["status"].(map[string]any)
	if statusMap == nil {
		return 0, false, nil
	}
	confirmed, _ := statusMap["confirmed"].(bool)
	if !confirmed {
		return 0, false, nil
	}
	height, _ := statusMap["block_height"].(float64)
	return int64(height), true, nil
}

func (e *EsploraBackend) NodeStatus(ctx context.Context) (map[string]any, error) {
	h, err := e.GetTipHeight(ctx)
	out := map[string]any{
		"backend": "esplora",
		"network": e.network,
		"url":     e.node.GetNodeURL(),
	}
	if err != nil {
		out["error"] = err.Error()
		out["synced"] = false
		return out, nil
	}
	out["blocks"] = h
	out["synced"] = true
	return out, nil
}

func (e *EsploraBackend) Close() error { return nil }

func (e *EsploraBackend) ListConfirmedUTXOs(address string) ([]AddressUTXO, error) {
	return e.mempool.ListConfirmedUTXOs(address)
}

func (e *EsploraBackend) FetchTx(txid string) (*wire.MsgTx, error) {
	return e.mempool.FetchTx(txid)
}

func (e *EsploraBackend) FetchTxOutput(txid string, vout uint32) (*wire.MsgTx, *wire.TxOut, error) {
	return e.mempool.FetchTxOutput(txid, vout)
}

func (e *EsploraBackend) BroadcastTx(rawHex string) (string, error) {
	return e.mempool.BroadcastTx(rawHex)
}

var _ ChainBackend = (*EsploraBackend)(nil)
