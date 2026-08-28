package bitcoin

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// BtcdBackend talks to a btcd (or bitcoind-compatible) JSON-RPC endpoint.
type BtcdBackend struct {
	client  *rpcclient.Client
	network string
	params  *chaincfg.Params
	mu      sync.RWMutex
	closed  bool
}

// BtcdRPCConfig holds connection settings for the RPC client.
type BtcdRPCConfig struct {
	Host         string // host:port
	User         string
	Pass         string
	Network      string
	DisableTLS   bool
	HTTPPostMode bool
}

// NewBtcdBackend connects to a btcd RPC endpoint.
func NewBtcdBackend(cfg BtcdRPCConfig) (*BtcdBackend, error) {
	if strings.TrimSpace(cfg.Host) == "" {
		return nil, fmt.Errorf("btcd RPC host required")
	}
	if cfg.Network == "" {
		cfg.Network = GetCurrentNetwork()
	}
	params := chainParamsForNetwork(cfg.Network)

	conn := &rpcclient.ConnConfig{
		Host:         cfg.Host,
		User:         cfg.User,
		Pass:         cfg.Pass,
		HTTPPostMode: cfg.HTTPPostMode,
		DisableTLS:   cfg.DisableTLS,
	}
	client, err := rpcclient.New(conn, nil)
	if err != nil {
		return nil, fmt.Errorf("btcd rpc connect: %w", err)
	}
	return &BtcdBackend{
		client:  client,
		network: cfg.Network,
		params:  params,
	}, nil
}

func chainParamsForNetwork(network string) *chaincfg.Params {
	return NetworkParams(network)
}

func (b *BtcdBackend) Network() string { return b.network }

func (b *BtcdBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	if b.client != nil {
		b.client.Shutdown()
	}
	return nil
}

func (b *BtcdBackend) rpc() (*rpcclient.Client, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed || b.client == nil {
		return nil, fmt.Errorf("btcd backend closed")
	}
	return b.client, nil
}

func (b *BtcdBackend) Ready(ctx context.Context) error {
	c, err := b.rpc()
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		_, err := c.GetBlockCount()
		done <- err
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("btcd not ready: %w", err)
		}
		return nil
	}
}

func (b *BtcdBackend) Synced(ctx context.Context) (bool, error) {
	c, err := b.rpc()
	if err != nil {
		return false, err
	}
	type res struct {
		synced bool
		err    error
	}
	ch := make(chan res, 1)
	go func() {
		info, err := c.GetBlockChainInfo()
		if err != nil {
			h, herr := c.GetBlockCount()
			ch <- res{h > 0 && herr == nil, herr}
			return
		}
		if info.InitialBlockDownload {
			ch <- res{false, nil}
			return
		}
		if info.Headers > info.Blocks+1 {
			ch <- res{false, nil}
			return
		}
		ch <- res{true, nil}
	}()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case r := <-ch:
		return r.synced, r.err
	}
}

func (b *BtcdBackend) GetTipHeight(ctx context.Context) (int64, error) {
	c, err := b.rpc()
	if err != nil {
		return 0, err
	}
	type res struct {
		h   int64
		err error
	}
	ch := make(chan res, 1)
	go func() {
		h, err := c.GetBlockCount()
		ch <- res{h, err}
	}()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case r := <-ch:
		return r.h, r.err
	}
}

func (b *BtcdBackend) GetBlockHash(ctx context.Context, height int64) (string, error) {
	c, err := b.rpc()
	if err != nil {
		return "", err
	}
	type res struct {
		h   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		hash, err := c.GetBlockHash(height)
		if err != nil {
			ch <- res{"", err}
			return
		}
		ch <- res{hash.String(), nil}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		return r.h, r.err
	}
}

func (b *BtcdBackend) GetRawBlockHex(ctx context.Context, height int64) (string, error) {
	c, err := b.rpc()
	if err != nil {
		return "", err
	}
	type res struct {
		hex string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		hash, err := c.GetBlockHash(height)
		if err != nil {
			ch <- res{"", fmt.Errorf("getblockhash %d: %w", height, err)}
			return
		}
		block, err := c.GetBlock(hash)
		if err != nil {
			ch <- res{"", fmt.Errorf("getblock %s: %w", hash, err)}
			return
		}
		var buf bytes.Buffer
		if err := block.Serialize(&buf); err != nil {
			ch <- res{"", fmt.Errorf("serialize block: %w", err)}
			return
		}
		ch <- res{hex.EncodeToString(buf.Bytes()), nil}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		return r.hex, r.err
	}
}

func (b *BtcdBackend) GetRawTx(ctx context.Context, txid string) (*wire.MsgTx, error) {
	c, err := b.rpc()
	if err != nil {
		return nil, err
	}
	hash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return nil, fmt.Errorf("invalid txid: %w", err)
	}
	type res struct {
		tx  *wire.MsgTx
		err error
	}
	ch := make(chan res, 1)
	go func() {
		tx, err := c.GetRawTransaction(hash)
		if err != nil {
			ch <- res{nil, err}
			return
		}
		ch <- res{tx.MsgTx(), nil}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		return r.tx, r.err
	}
}

func (b *BtcdBackend) GetTxStatus(ctx context.Context, txid string) (int64, bool, error) {
	c, err := b.rpc()
	if err != nil {
		return 0, false, err
	}
	hash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return 0, false, fmt.Errorf("invalid txid: %w", err)
	}
	type res struct {
		height    int64
		confirmed bool
		err       error
	}
	ch := make(chan res, 1)
	go func() {
		verbose, err := c.GetRawTransactionVerbose(hash)
		if err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "no information available") ||
				strings.Contains(msg, "not found") ||
				strings.Contains(msg, "no such") {
				ch <- res{0, false, nil}
				return
			}
			ch <- res{0, false, err}
			return
		}
		if verbose.BlockHash == "" || verbose.Confirmations < 1 {
			ch <- res{0, false, nil}
			return
		}
		bh, err := chainhash.NewHashFromStr(verbose.BlockHash)
		if err != nil {
			ch <- res{0, true, nil}
			return
		}
		header, err := c.GetBlockHeaderVerbose(bh)
		if err != nil {
			ch <- res{0, true, nil}
			return
		}
		ch <- res{int64(header.Height), true, nil}
	}()
	select {
	case <-ctx.Done():
		return 0, false, ctx.Err()
	case r := <-ch:
		return r.height, r.confirmed, r.err
	}
}

func (b *BtcdBackend) NodeStatus(ctx context.Context) (map[string]any, error) {
	c, err := b.rpc()
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"backend": "btcd",
		"network": b.network,
	}
	info, err := c.GetBlockChainInfo()
	if err != nil {
		out["error"] = err.Error()
		return out, nil
	}
	out["blocks"] = info.Blocks
	out["headers"] = info.Headers
	out["best_block_hash"] = info.BestBlockHash
	out["initial_block_download"] = info.InitialBlockDownload
	out["verification_progress"] = info.VerificationProgress
	out["pruned"] = info.Pruned
	out["size_on_disk"] = info.SizeOnDisk
	if peers, err := c.GetPeerInfo(); err == nil {
		out["peers"] = len(peers)
	}
	synced, _ := b.Synced(ctx)
	out["synced"] = synced
	// Overlay external tip lag so health consumers see network lag even when
	// local IBD is complete (see tip_lag.go).
	mergeTipLagIntoStatus(out)
	return out, nil
}

// ListConfirmedUTXOs uses addrindex (searchrawtransactions) + gettxout.
func (b *BtcdBackend) ListConfirmedUTXOs(address string) ([]AddressUTXO, error) {
	c, err := b.rpc()
	if err != nil {
		return nil, err
	}
	addr, err := DecodeAddressForNetwork(address, b.params)
	if err != nil {
		return nil, fmt.Errorf("decode address: %w", err)
	}

	const pageSize = 100
	skip := 0
	var utxos []AddressUTXO
	seen := make(map[string]struct{})

	for {
		txs, err := c.SearchRawTransactionsVerbose(addr, skip, pageSize, true, false, nil)
		if err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "no information") ||
				strings.Contains(msg, "no tx") ||
				strings.Contains(msg, "not found") {
				break
			}
			return nil, fmt.Errorf("searchrawtransactions %s: %w (is --addrindex enabled?)", address, err)
		}
		if len(txs) == 0 {
			break
		}
		for _, tx := range txs {
			if tx.Confirmations < 1 {
				continue
			}
			for _, vout := range tx.Vout {
				if !voutPaysAddress(vout, address, b.params) {
					continue
				}
				key := fmt.Sprintf("%s:%d", tx.Txid, vout.N)
				if _, ok := seen[key]; ok {
					continue
				}
				th, err := chainhash.NewHashFromStr(tx.Txid)
				if err != nil {
					continue
				}
				txOut, err := c.GetTxOut(th, vout.N, false)
				if err != nil || txOut == nil {
					continue
				}
				if txOut.Confirmations < 1 {
					continue
				}
				u := AddressUTXO{
					TxID:  tx.Txid,
					Vout:  vout.N,
					Value: btcToSats(txOut.Value),
				}
				u.Status.Confirmed = true
				utxos = append(utxos, u)
				seen[key] = struct{}{}
			}
		}
		if len(txs) < pageSize {
			break
		}
		skip += pageSize
		if skip > 5000 {
			log.Printf("btcd: ListConfirmedUTXOs capped scan at skip=%d for %s", skip, address)
			break
		}
	}
	return utxos, nil
}

func voutPaysAddress(vout btcjson.Vout, address string, params *chaincfg.Params) bool {
	if vout.ScriptPubKey.Address == address {
		return true
	}
	for _, a := range vout.ScriptPubKey.Addresses {
		if a == address {
			return true
		}
	}
	// Fallback: decode script hex and extract addresses.
	if vout.ScriptPubKey.Hex == "" {
		return false
	}
	script, err := hex.DecodeString(vout.ScriptPubKey.Hex)
	if err != nil {
		return false
	}
	if params == nil {
		params = CurrentNetworkParams()
	}
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(script, params)
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if a.EncodeAddress() == address {
			return true
		}
	}
	return false
}

func btcToSats(btc float64) int64 {
	return int64(math.Round(btc * 1e8))
}

// FetchTx implements UTXOClient.
func (b *BtcdBackend) FetchTx(txid string) (*wire.MsgTx, error) {
	return b.GetRawTx(context.Background(), txid)
}

// FetchTxOutput implements UTXOClient.
func (b *BtcdBackend) FetchTxOutput(txid string, vout uint32) (*wire.MsgTx, *wire.TxOut, error) {
	msg, err := b.FetchTx(txid)
	if err != nil {
		return nil, nil, err
	}
	if int(vout) >= len(msg.TxOut) {
		return nil, nil, fmt.Errorf("tx %s missing vout %d", txid, vout)
	}
	return msg, msg.TxOut[vout], nil
}

// BroadcastTx implements UTXOClient via sendrawtransaction.
func (b *BtcdBackend) BroadcastTx(rawHex string) (string, error) {
	rawHex = strings.TrimSpace(rawHex)
	if rawHex == "" {
		return "", fmt.Errorf("raw tx hex required")
	}
	raw, err := hex.DecodeString(rawHex)
	if err != nil {
		return "", fmt.Errorf("decode raw tx hex: %w", err)
	}
	msg := &wire.MsgTx{}
	if err := msg.Deserialize(bytes.NewReader(raw)); err != nil {
		return "", fmt.Errorf("parse raw tx: %w", err)
	}
	c, err := b.rpc()
	if err != nil {
		return "", err
	}
	hash, err := c.SendRawTransaction(msg, false)
	if err != nil {
		return "", fmt.Errorf("sendrawtransaction: %w", err)
	}
	return hash.String(), nil
}
