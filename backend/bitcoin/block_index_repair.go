package bitcoin

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/btcsuite/btcd/database"
	_ "github.com/btcsuite/btcd/database/ffldb"
	"github.com/btcsuite/btcd/wire"
)

// btcd block-index status bits (blockchain.blockStatus). Duplicated here so
// we can patch ffldb without importing the unexported blockchain types.
const (
	idxStatusDataStored      byte = 1 << 0
	idxStatusValid           byte = 1 << 1
	idxStatusValidateFailed  byte = 1 << 2
	idxStatusInvalidAncestor byte = 1 << 3
)

const (
	// Must match blockchain.blockIndexBucketName in btcd v0.25.
	btcdBlockIndexBucket = "blockheaderidx"
	btcdBlockHeaderSize  = 80 // wire.MaxBlockHeaderPayload
	btcdBlocksDBName     = "blocks_ffldb"
)

// ClearStickyInvalidFlags opens the managed btcd ffldb (btcd must be stopped)
// and clears ValidateFailed / InvalidAncestor on every block-index entry.
//
// InvalidateBlock can leave Valid+Invalid set together, or Invalid alone.
// ReconsiderBlock then no-ops and peers reject the continuation as
// "previous block is known to be invalid". A later connect will re-validate
// genuinely bad blocks and mark them failed again.
func ClearStickyInvalidFlags(dataDir, network string) (int, error) {
	dbPath, netMagic, err := btcdBlocksDB(dataDir, network)
	if err != nil {
		return 0, err
	}
	if _, err := os.Stat(dbPath); err != nil {
		return 0, fmt.Errorf("btcd block db %s: %w", dbPath, err)
	}
	db, err := database.Open("ffldb", dbPath, netMagic)
	if err != nil {
		return 0, fmt.Errorf("open ffldb %s: %w", dbPath, err)
	}
	defer db.Close()

	cleared := 0
	err = db.Update(func(tx database.Tx) error {
		bucket := tx.Metadata().Bucket([]byte(btcdBlockIndexBucket))
		if bucket == nil {
			return fmt.Errorf("missing %s bucket", btcdBlockIndexBucket)
		}
		type patch struct {
			key   []byte
			value []byte
		}
		var patches []patch
		err := bucket.ForEach(func(k, v []byte) error {
			if len(v) < btcdBlockHeaderSize+1 {
				return nil
			}
			status := v[btcdBlockHeaderSize]
			invalid := status & (idxStatusValidateFailed | idxStatusInvalidAncestor)
			if invalid == 0 {
				return nil
			}
			nv := make([]byte, len(v))
			copy(nv, v)
			nv[btcdBlockHeaderSize] = status &^ (idxStatusValidateFailed | idxStatusInvalidAncestor)
			nk := make([]byte, len(k))
			copy(nk, k)
			patches = append(patches, patch{key: nk, value: nv})
			return nil
		})
		if err != nil {
			return err
		}
		for _, p := range patches {
			if err := bucket.Put(p.key, p.value); err != nil {
				return err
			}
			cleared++
		}
		return nil
	})
	if err != nil {
		return cleared, err
	}
	if cleared > 0 {
		log.Printf("btcd index repair: cleared sticky Valid+Invalid flags on %d block(s) in %s", cleared, dbPath)
	}
	return cleared, nil
}

func btcdBlocksDB(dataDir, network string) (string, wire.BitcoinNet, error) {
	dataDir = filepath.Clean(dataDir)
	if dataDir == "" || dataDir == "." {
		return "", 0, fmt.Errorf("empty btcd datadir")
	}
	params := NetworkParams(network)
	if params == nil {
		return "", 0, fmt.Errorf("unknown network %q", network)
	}
	netDir := params.Name
	if network == "testnet" || network == "testnet3" {
		// btcd stores testnet3 under "testnet", not "testnet3".
		netDir = "testnet"
	}
	return filepath.Join(dataDir, netDir, btcdBlocksDBName), params.Net, nil
}
