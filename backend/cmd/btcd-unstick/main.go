// Command btcd-unstick clears sticky Valid+Invalid flags in a stopped btcd
// ffldb. Use when adopt/invalidate left the explorer chain KnownInvalid and
// reconsiderblock logs "is valid, nothing to reconsider".
//
// btcd must not be running (it holds the ffldb lock).
package main

import (
	"flag"
	"fmt"
	"os"

	"stargate-backend/bitcoin"
)

func main() {
	dataDir := flag.String("datadir", "", "btcd datadir (parent of <network>/blocks_ffldb)")
	network := flag.String("network", "testnet4", "bitcoin network name")
	flag.Parse()
	if *dataDir == "" {
		fmt.Fprintln(os.Stderr, "usage: btcd-unstick -datadir /data/btcd [-network testnet4]")
		os.Exit(2)
	}
	n, err := bitcoin.ClearStickyInvalidFlags(*dataDir, *network)
	if err != nil {
		fmt.Fprintf(os.Stderr, "btcd-unstick: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("cleared %d sticky Valid+Invalid block-index entries\n", n)
}
