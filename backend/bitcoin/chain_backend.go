package bitcoin

import (
	"context"

	"github.com/btcsuite/btcd/wire"
)

// UTXOClient is the chain surface used by PSBT builders and commitment sweeps
// (formerly backed by mempool.space HTTP).
type UTXOClient interface {
	ListConfirmedUTXOs(address string) ([]AddressUTXO, error)
	FetchTx(txid string) (*wire.MsgTx, error)
	FetchTxOutput(txid string, vout uint32) (*wire.MsgTx, *wire.TxOut, error)
	BroadcastTx(rawHex string) (string, error)
}

// ChainBackend is the primary Bitcoin data source for Stargate.
// Production default is a local btcd full node (no mining).
type ChainBackend interface {
	UTXOClient

	Network() string
	// Ready reports whether RPC is up. Does not wait for full IBD.
	Ready(ctx context.Context) error
	// Synced is true when the node is past initial block download
	// (headers catch up to blocks, or InitialBlockDownload is false).
	Synced(ctx context.Context) (bool, error)
	GetTipHeight(ctx context.Context) (int64, error)
	GetBlockHash(ctx context.Context, height int64) (string, error)
	GetRawBlockHex(ctx context.Context, height int64) (string, error)
	GetRawTx(ctx context.Context, txid string) (*wire.MsgTx, error)
	// GetTxStatus returns confirmation height and whether the tx is confirmed.
	// If the tx is unknown, confirmed=false and err=nil.
	GetTxStatus(ctx context.Context, txid string) (height int64, confirmed bool, err error)
	// Status snapshot for health endpoints.
	NodeStatus(ctx context.Context) (map[string]any, error)
	Close() error
}

// Ensure MempoolClient and BtcdBackend satisfy UTXOClient at compile time.
var (
	_ UTXOClient   = (*MempoolClient)(nil)
	_ UTXOClient   = (*BtcdBackend)(nil)
	_ ChainBackend = (*BtcdBackend)(nil)
)
