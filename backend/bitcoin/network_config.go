package bitcoin

import (
	"log"
	"os"
)

// NetworkConfig holds configuration for different Bitcoin networks
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

// NewBitcoinNodeClientForNetwork creates a client for the specified network
func NewBitcoinNodeClientForNetwork(network string) *BitcoinNodeClient {
	config := GetNetworkConfig(network)
	log.Printf("Creating Bitcoin client for %s: %s", config.Name, config.BaseURL)
	return NewBitcoinNodeClient(config.BaseURL)
}
