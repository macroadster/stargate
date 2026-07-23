package bitcoin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadNodeConfigFromEnvDefaults(t *testing.T) {
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	t.Setenv("BTCD_MODE", "")
	t.Setenv("BTCD_DATADIR", "")
	t.Setenv("BTCD_RPC_HOST", "")
	t.Setenv("STARGATE_DATA_DIR", t.TempDir())

	cfg := LoadNodeConfigFromEnv()
	if cfg.Mode != BtcdModeManaged {
		t.Fatalf("mode: got %q want managed", cfg.Mode)
	}
	if cfg.Network != "testnet4" {
		t.Fatalf("network: got %q", cfg.Network)
	}
	if cfg.RPCHost != "127.0.0.1:48334" {
		t.Fatalf("rpc host: got %q", cfg.RPCHost)
	}
	if !cfg.TxIndex || !cfg.AddrIndex {
		t.Fatalf("indexes should default on")
	}
	wantData := filepath.Join(os.Getenv("STARGATE_DATA_DIR"), "btcd")
	if cfg.DataDir != wantData {
		t.Fatalf("datadir: got %q want %q", cfg.DataDir, wantData)
	}
}

func TestNetworkFlag(t *testing.T) {
	f, err := networkFlag("testnet4")
	if err != nil || f != "--testnet4" {
		t.Fatalf("testnet4: %q %v", f, err)
	}
	f, err = networkFlag("mainnet")
	if err != nil || f != "" {
		t.Fatalf("mainnet: %q %v", f, err)
	}
	if _, err := networkFlag("bogus"); err == nil {
		t.Fatal("expected error for bogus network")
	}
}

func TestEnsureRPCAuthPersists(t *testing.T) {
	dir := t.TempDir()
	cfg := NodeConfig{DataDir: dir}
	if err := cfg.ensureRPCAuth(); err != nil {
		t.Fatal(err)
	}
	if cfg.RPCUser == "" || cfg.RPCPass == "" {
		t.Fatal("credentials not generated")
	}
	user, pass := cfg.RPCUser, cfg.RPCPass
	cfg2 := NodeConfig{DataDir: dir}
	if err := cfg2.ensureRPCAuth(); err != nil {
		t.Fatal(err)
	}
	if cfg2.RPCUser != user || cfg2.RPCPass != pass {
		t.Fatalf("credentials not stable: %s/%s vs %s/%s", user, pass, cfg2.RPCUser, cfg2.RPCPass)
	}
}

func TestChainParamsForNetwork(t *testing.T) {
	p := chainParamsForNetwork("testnet4")
	if p == nil || p.Name == "" {
		t.Fatal("unexpected empty params for testnet4")
	}
	if p.DefaultPort != "48333" {
		t.Fatalf("testnet4 default port: got %q", p.DefaultPort)
	}
}
