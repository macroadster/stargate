package bitcoin

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/btcsuite/btcd/wire"
)

// MempoolClient provides lightweight access to mempool.space HTTP APIs for UTXO lookup.
type MempoolClient struct {
	baseURL string
	http    *http.Client
}

// NewMempoolClient builds a client using MEMPOOL_API_BASE or the testnet4 default.
func NewMempoolClient() *MempoolClient {
	base := os.Getenv("MEMPOOL_API_BASE")
	if base == "" {
		base = "https://mempool.space/testnet4/api"
	}
	return &MempoolClient{
		baseURL: base,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// AddressUTXO represents a mempool.space UTXO entry.
type AddressUTXO struct {
	TxID   string `json:"txid"`
	Vout   uint32 `json:"vout"`
	Value  int64  `json:"value"`
	Status struct {
		Confirmed bool `json:"confirmed"`
	} `json:"status"`
}

// ListConfirmedUTXOs returns confirmed UTXOs for an address.
func (c *MempoolClient) ListConfirmedUTXOs(address string) ([]AddressUTXO, error) {
	url := fmt.Sprintf("%s/address/%s/utxo", c.baseURL, address)
	resp, err := c.http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch utxos: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("fetch utxos: status %d: %s", resp.StatusCode, string(body))
	}
	var utxos []AddressUTXO
	if err := json.NewDecoder(resp.Body).Decode(&utxos); err != nil {
		return nil, fmt.Errorf("decode utxos: %w", err)
	}
	var confirmed []AddressUTXO
	for _, u := range utxos {
		if u.Status.Confirmed {
			confirmed = append(confirmed, u)
		}
	}
	return confirmed, nil
}

// FetchTx pulls and decodes a raw transaction by txid.
func (c *MempoolClient) FetchTx(txid string) (*wire.MsgTx, error) {
	url := fmt.Sprintf("%s/tx/%s/raw", c.baseURL, txid)
	resp, err := c.http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch tx: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("fetch tx: status %d: %s", resp.StatusCode, string(body))
	}
	rawHex, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read tx: %w", err)
	}
	raw, err := hex.DecodeString(string(rawHex))
	if err != nil {
		raw = rawHex // raw endpoint may already be bytes
	}
	msg := &wire.MsgTx{}
	if err := msg.Deserialize(bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("parse tx: %w", err)
	}
	return msg, nil
}

// FetchTxOutput returns the referenced output for the given utxo.
func (c *MempoolClient) FetchTxOutput(txid string, vout uint32) (*wire.MsgTx, *wire.TxOut, error) {
	msg, err := c.FetchTx(txid)
	if err != nil {
		return nil, nil, err
	}
	if int(vout) >= len(msg.TxOut) {
		return nil, nil, fmt.Errorf("tx %s missing vout %d", txid, vout)
	}
	return msg, msg.TxOut[vout], nil
}
