package bitcoin

import (
	"log"
	"os"
	"strings"

	"github.com/btcsuite/btcd/chaincfg"
)

// NetworkConfig holds configuration for different Bitcoin networks.
// BaseURL/HeightURL are used for explorer links and BTCD_MODE=off Esplora fallback only.
// Production chain data comes from local btcd (see ADR 0006 / StartChainFromEnv).
type NetworkConfig struct {
	Name        string
	BaseURL     string
	ExplorerURL string
	FaucetURL   string
	HeightURL   string
}

// GetNetworkConfig returns configuration for the specified network
func GetNetworkConfig(network string) *NetworkConfig {
	switch network {
	case "testnet4":
		return &NetworkConfig{
			Name:        "Bitcoin Testnet4",
			BaseURL:     "https://mempool.space/testnet4/api",
			ExplorerURL: "https://mempool.space/testnet4",
			FaucetURL:   "",
			HeightURL:   "https://mempool.space/testnet4/api/blocks/tip/height",
		}
	case "testnet":
		return &NetworkConfig{
			Name:        "Bitcoin Testnet",
			BaseURL:     "https://blockstream.info/testnet/api",
			ExplorerURL: "https://blockstream.info/testnet",
			FaucetURL:   "https://coinfaucet.eu/en/btc-testnet/",
			HeightURL:   "https://blockstream.info/testnet/api/blocks/tip/height",
		}
	case "mainnet":
		return &NetworkConfig{
			Name:        "Bitcoin Mainnet",
			BaseURL:     "https://blockstream.info/api",
			ExplorerURL: "https://blockstream.info",
			FaucetURL:   "",
			HeightURL:   "https://blockstream.info/api/blocks/tip/height",
		}
	case "signet":
		return &NetworkConfig{
			Name:        "Bitcoin Signet",
			BaseURL:     "https://mempool.space/signet/api",
			ExplorerURL: "https://mempool.space/signet",
			FaucetURL:   "https://signetfaucet.com/",
			HeightURL:   "https://mempool.space/signet/api/blocks/tip/height",
		}
	default:
		log.Printf("Unknown network '%s', defaulting to testnet4", network)
		return GetNetworkConfig("testnet4")
	}
}

// GetCurrentNetwork returns the current network from environment variable
func GetCurrentNetwork() string {
	network := os.Getenv("BITCOIN_NETWORK")
	if network == "" {
		network = "testnet4"
	}
	return network
}

// NetworkName prefers a live ChainBackend network (btcd / Esplora) and
// falls back to BITCOIN_NETWORK. Payment JSON and PSBT builders must use
// this so a wrong advertised network cannot pair with a correct PSBT.
func NetworkName(backend ChainBackend) string {
	if backend != nil {
		if n := strings.TrimSpace(backend.Network()); n != "" {
			return n
		}
	}
	return GetCurrentNetwork()
}

// NetworkParams returns btcd chaincfg params for a Stargate network name.
// This is the single mapper used by PSBT builders, payment JSON, and
// ChainBackend / managed btcd. "testnet" is testnet3; it is not testnet4.
func NetworkParams(network string) *chaincfg.Params {
	switch strings.ToLower(strings.TrimSpace(network)) {
	case "mainnet", "main":
		return &chaincfg.MainNetParams
	case "testnet", "testnet3":
		return &chaincfg.TestNet3Params
	case "testnet4":
		return &chaincfg.TestNet4Params
	case "signet":
		return &chaincfg.SigNetParams
	case "simnet":
		return &chaincfg.SimNetParams
	case "regtest":
		return &chaincfg.RegressionNetParams
	default:
		return &chaincfg.TestNet4Params
	}
}

// CurrentNetworkParams returns chaincfg params for GetCurrentNetwork().
func CurrentNetworkParams() *chaincfg.Params {
	return NetworkParams(GetCurrentNetwork())
}

// NewBitcoinNodeClientForNetwork creates a client for the specified network
func NewBitcoinNodeClientForNetwork(network string) *BitcoinNodeClient {
	config := GetNetworkConfig(network)
	log.Printf("Creating Bitcoin client for %s: %s", config.Name, config.BaseURL)
	return NewBitcoinNodeClient(config.BaseURL)
}
