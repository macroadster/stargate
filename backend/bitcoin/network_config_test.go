package bitcoin

import (
	"testing"

	"github.com/btcsuite/btcd/chaincfg"
)

func TestNetworkParamsMatchesBtcdAndDoesNotConflateTestnets(t *testing.T) {
	cases := []struct {
		name    string
		aliases []string
		want    *chaincfg.Params
	}{
		{"testnet4", []string{"testnet4", "TESTNET4"}, &chaincfg.TestNet4Params},
		{"testnet3", []string{"testnet", "testnet3"}, &chaincfg.TestNet3Params},
		{"signet", []string{"signet", "Signet"}, &chaincfg.SigNetParams},
		{"mainnet", []string{"mainnet", "main", "MAINNET"}, &chaincfg.MainNetParams},
		{"simnet", []string{"simnet"}, &chaincfg.SimNetParams},
		{"regtest", []string{"regtest"}, &chaincfg.RegressionNetParams},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, alias := range tc.aliases {
				got := NetworkParams(alias)
				if got.Name != tc.want.Name || got.Net != tc.want.Net {
					t.Fatalf("NetworkParams(%q)=%s/%v want %s/%v", alias, got.Name, got.Net, tc.want.Name, tc.want.Net)
				}
				if chainParamsForNetwork(alias).Name != tc.want.Name {
					t.Fatalf("chainParamsForNetwork(%q) drifted from NetworkParams", alias)
				}
			}
		})
	}

	if NetworkParams("testnet").Net == NetworkParams("testnet4").Net {
		t.Fatal("testnet (testnet3) and testnet4 must use different wire magic; conflating them is a fund-loss class bug")
	}
	if NetworkParams("signet").Net == NetworkParams("testnet4").Net {
		t.Fatal("signet and testnet4 share address HRP but must not share wire params")
	}
	if NetworkParams("unknown").Name != chaincfg.TestNet4Params.Name {
		t.Fatalf("unknown network should default to testnet4, got %s", NetworkParams("unknown").Name)
	}
}

func TestGetCurrentNetworkAndCurrentParams(t *testing.T) {
	t.Setenv("BITCOIN_NETWORK", "")
	if got := GetCurrentNetwork(); got != "testnet4" {
		t.Fatalf("empty env: got %q want testnet4", got)
	}
	if CurrentNetworkParams().Name != chaincfg.TestNet4Params.Name {
		t.Fatalf("empty env params: got %s", CurrentNetworkParams().Name)
	}

	t.Setenv("BITCOIN_NETWORK", "signet")
	if got := GetCurrentNetwork(); got != "signet" {
		t.Fatalf("signet env: got %q", got)
	}
	if CurrentNetworkParams().Name != chaincfg.SigNetParams.Name {
		t.Fatalf("signet params: got %s", CurrentNetworkParams().Name)
	}

	t.Setenv("BITCOIN_NETWORK", "mainnet")
	if CurrentNetworkParams().Name != chaincfg.MainNetParams.Name {
		t.Fatalf("mainnet params: got %s", CurrentNetworkParams().Name)
	}
}

func TestNetworkNamePrefersChainBackend(t *testing.T) {
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	if got := NetworkName(nil); got != "testnet4" {
		t.Fatalf("nil backend: got %q", got)
	}
	if got := NetworkName(&mockChain{}); got != "testnet4" {
		t.Fatalf("mock backend: got %q", got)
	}

	t.Setenv("BITCOIN_NETWORK", "testnet4")
	if got := NetworkName(&mockChain{}); got != "testnet4" {
		t.Fatalf("backend network should win over a matching env, got %q", got)
	}
	// mockChain.Network is hardcoded testnet4. Prefer that over env=signet.
	t.Setenv("BITCOIN_NETWORK", "signet")
	if got := NetworkName(&mockChain{}); got != "testnet4" {
		t.Fatalf("backend must win over env: got %q want testnet4", got)
	}
}
