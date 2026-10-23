package bitcoin

import (
	"fmt"
	"strings"

	btcaddr "github.com/btcsuite/btcd/address/v2"
	"github.com/btcsuite/btcd/chaincfg/v2"
)

// DecodeAddressForNetwork decodes addr and requires it to belong to params.
// btcaddr.DecodeAddress does not enforce the network for bech32 (a tb1
// address decodes even when params are mainnet), so PSBT builders must use
// this. Signet and testnet4 share the tb HRP — the advertised network string
// is what distinguishes them.
func DecodeAddressForNetwork(addr string, params *chaincfg.Params) (btcaddr.Address, error) {
	if params == nil {
		params = CurrentNetworkParams()
	}
	decoded, err := btcaddr.DecodeAddress(strings.TrimSpace(addr), params)
	if err != nil {
		return nil, err
	}
	if !decoded.IsForNet(params) {
		return nil, fmt.Errorf("address is not valid for %s", params.Name)
	}
	return decoded, nil
}
