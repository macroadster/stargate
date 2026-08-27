package bitcoin

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/database"
	_ "github.com/btcsuite/btcd/database/ffldb"
)

func TestClearStickyInvalidFlags(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "testnet4", btcdBlocksDBName)
	params := &chaincfg.TestNet4Params
	db, err := database.Create("ffldb", dbPath, params.Net)
	if err != nil {
		t.Fatal(err)
	}

	put := func(height uint32, hashTail byte, status byte) {
		t.Helper()
		key := make([]byte, 36)
		binary.BigEndian.PutUint32(key[0:4], height)
		key[35] = hashTail
		val := append(bytes.Repeat([]byte{0xaa}, btcdBlockHeaderSize), status)
		if err := db.Update(func(tx database.Tx) error {
			b, err := tx.Metadata().CreateBucketIfNotExists([]byte(btcdBlockIndexBucket))
			if err != nil {
				return err
			}
			return b.Put(key, val)
		}); err != nil {
			t.Fatal(err)
		}
	}

	// stored+valid (healthy)
	put(100, 1, idxStatusDataStored|idxStatusValid)
	// sticky Valid+ValidateFailed (the bug)
	put(101, 2, idxStatusDataStored|idxStatusValid|idxStatusValidateFailed)
	// sticky Valid+InvalidAncestor
	put(102, 3, idxStatusDataStored|idxStatusValid|idxStatusInvalidAncestor)
	// truly invalid, never accepted as valid — leave alone
	put(103, 4, idxStatusDataStored|idxStatusValidateFailed)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	n, err := ClearStickyInvalidFlags(dataDir, "testnet4")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("cleared=%d want 2", n)
	}

	db, err = database.Open("ffldb", dbPath, params.Net)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	got := map[byte]byte{}
	if err := db.View(func(tx database.Tx) error {
		b := tx.Metadata().Bucket([]byte(btcdBlockIndexBucket))
		return b.ForEach(func(k, v []byte) error {
			got[k[35]] = v[btcdBlockHeaderSize]
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
	if got[1] != idxStatusDataStored|idxStatusValid {
		t.Fatalf("healthy node mutated: %02x", got[1])
	}
	if got[2] != idxStatusDataStored|idxStatusValid {
		t.Fatalf("sticky failed not cleared: %02x", got[2])
	}
	if got[3] != idxStatusDataStored|idxStatusValid {
		t.Fatalf("sticky ancestor not cleared: %02x", got[3])
	}
	if got[4] != idxStatusDataStored|idxStatusValidateFailed {
		t.Fatalf("true-invalid should stay: %02x", got[4])
	}
}
