package bitcoin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveBtcdBinaryOrder(t *testing.T) {
	siblingDir := t.TempDir()
	pathDir := t.TempDir()
	sibling := filepath.Join(siblingDir, "btcd")
	onPath := filepath.Join(pathDir, "btcd")
	writeFakeBtcd(t, sibling)
	writeFakeBtcd(t, onPath)
	t.Setenv("PATH", pathDir)

	got, err := resolveBtcdBinary("btcd", siblingDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != sibling {
		t.Fatalf("sibling should win over PATH: got %q want %q", got, sibling)
	}

	got, err = resolveBtcdBinary("btcd", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != onPath {
		t.Fatalf("PATH fallback: got %q want %q", got, onPath)
	}

	got, err = resolveBtcdBinary("", siblingDir)
	if err != nil || got != sibling {
		t.Fatalf("empty name: got %q err %v", got, err)
	}
}

func TestResolveEmbeddedBtcdPrefersPackedBytes(t *testing.T) {
	prev := embeddedBtcd
	t.Cleanup(func() { embeddedBtcd = prev })
	embeddedBtcd = []byte("#!/bin/sh\necho embedded\n")

	data := t.TempDir()
	t.Setenv("STARGATE_DATA_DIR", data)
	siblingDir := t.TempDir()
	pathDir := t.TempDir()
	writeFakeBtcd(t, filepath.Join(siblingDir, "btcd"))
	writeFakeBtcd(t, filepath.Join(pathDir, "btcd"))
	writeFakeBtcd(t, filepath.Join(siblingDir, "other-btcd"))
	t.Setenv("PATH", pathDir)

	got, err := resolveBtcdBinary("btcd", siblingDir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(data, "bin", "btcd")
	if got != want {
		t.Fatalf("embedded btcd: got %q want %q", got, want)
	}
	body, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(embeddedBtcd) {
		t.Fatalf("extracted bytes = %q", body)
	}

	// Same bytes are left in place.
	got, err = resolveBtcdBinary("btcd", siblingDir)
	if err != nil || got != want {
		t.Fatalf("reuse: got %q err %v", got, err)
	}

	embeddedBtcd = []byte("#!/bin/sh\necho replaced\n")
	got, err = resolveBtcdBinary("btcd", siblingDir)
	if err != nil || got != want {
		t.Fatalf("rewrite: got %q err %v", got, err)
	}
	body, err = os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "#!/bin/sh\necho replaced\n" {
		t.Fatalf("rewritten bytes = %q", body)
	}

	got, err = resolveBtcdBinary("other-btcd", siblingDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(siblingDir, "other-btcd") {
		t.Fatalf("custom bare name should not use the embedded btcd: %q", got)
	}
}

func TestResolveBtcdBinaryExplicitPathDoesNotSearch(t *testing.T) {
	pathDir := t.TempDir()
	writeFakeBtcd(t, filepath.Join(pathDir, "btcd"))
	t.Setenv("PATH", pathDir)

	prev := embeddedBtcd
	t.Cleanup(func() { embeddedBtcd = prev })
	embeddedBtcd = []byte("#!/bin/sh\necho embedded\n")
	t.Setenv("STARGATE_DATA_DIR", t.TempDir())

	missing := filepath.Join(t.TempDir(), "btcd")
	if _, err := resolveBtcdBinary(missing, pathDir); err == nil {
		t.Fatal("missing explicit path should fail even when btcd is embedded and on PATH")
	}

	relDir := t.TempDir()
	writeFakeBtcd(t, filepath.Join(relDir, "btcd"))
	t.Chdir(filepath.Dir(relDir))
	rel := filepath.Join(filepath.Base(relDir), "btcd")
	got, err := resolveBtcdBinary(rel, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != rel {
		t.Fatalf("relative path: got %q want %q", got, rel)
	}

	embeddedBtcd = nil
	notExec := filepath.Join(t.TempDir(), "btcd")
	if err := os.WriteFile(notExec, []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	siblingDir := filepath.Dir(notExec)
	got, err = resolveBtcdBinary("btcd", siblingDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(pathDir, "btcd") {
		t.Fatalf("non-executable sibling should fall through to PATH: got %q", got)
	}
}

func TestResolveBtcdBinaryMissingNamesExits(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := resolveBtcdBinary("btcd", t.TempDir())
	if err == nil {
		t.Fatal("expected missing binary")
	}
	msg := err.Error()
	for _, want := range []string{"btcd binary not found (btcd)", "BTCD_BIN", "BTCD_MODE=external", "BTCD_MODE=off"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q in %q", want, msg)
		}
	}
}

func TestDisallowedMainnet(t *testing.T) {
	if err := DisallowedMainnet(NodeConfig{Network: "testnet4"}); err != nil {
		t.Fatal(err)
	}
	if err := DisallowedMainnet(NodeConfig{Network: "Mainnet"}); err == nil {
		t.Fatal("expected mixed-case mainnet to require opt-in")
	}
	if err := DisallowedMainnet(NodeConfig{Network: "main", AllowMainnet: true}); err != nil {
		t.Fatal(err)
	}
}

func TestStartManagedMissingBtcd(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	node := &EmbeddedBtcd{cfg: NodeConfig{
		Mode:    BtcdModeManaged,
		Bin:     "stargate-btcd-test-missing",
		DataDir: t.TempDir(),
		Network: "testnet4",
		RPCHost: "127.0.0.1:48334",
	}}
	err := node.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "BTCD_MODE=off") {
		t.Fatalf("got %v", err)
	}
}

func writeFakeBtcd(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

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

func TestDefaultP2PListenIsLocalhost(t *testing.T) {
	if got := defaultP2PListen("testnet4"); got != "127.0.0.1:48333" {
		t.Fatalf("testnet4 listen %q", got)
	}
	if got := defaultP2PListen("mainnet"); got != "127.0.0.1:8333" {
		t.Fatalf("mainnet listen %q", got)
	}
}

func TestLoadNodeConfigPeerPinning(t *testing.T) {
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	t.Setenv("BTCD_MODE", "managed")
	t.Setenv("BTCD_NOLISTEN", "true")
	t.Setenv("BTCD_CONNECT", "seed.example:48333, 10.0.0.2:48333")
	t.Setenv("BTCD_ADDPEER", "10.0.0.3:48333")
	t.Setenv("BTCD_MIN_PEERS", "4")
	t.Setenv("STARGATE_DATA_DIR", t.TempDir())

	cfg := LoadNodeConfigFromEnv()
	if !cfg.NoListen {
		t.Fatal("expected NoListen")
	}
	if len(cfg.Connect) != 2 || cfg.Connect[0] != "seed.example:48333" {
		t.Fatalf("connect=%v", cfg.Connect)
	}
	if len(cfg.AddPeer) != 1 || cfg.AddPeer[0] != "10.0.0.3:48333" {
		t.Fatalf("addpeer=%v", cfg.AddPeer)
	}
	if cfg.MinPeers != 4 {
		t.Fatalf("min peers=%d", cfg.MinPeers)
	}
	if cfg.P2PListen != "127.0.0.1:48333" {
		t.Fatalf("default listen should be localhost, got %q", cfg.P2PListen)
	}
}

func TestBtcdProcessArgsPinsPeersAndDisablesListen(t *testing.T) {
	cfg := NodeConfig{
		DataDir:   "/data",
		RPCUser:   "u",
		RPCPass:   "p",
		P2PListen: "0.0.0.0:48333",
		Connect:   []string{"seed:48333"},
		AddPeer:   []string{"friend:48333"},
		TxIndex:   true,
		AddrIndex: true,
	}
	args := cfg.btcdProcessArgs("127.0.0.1:48334", "--testnet4")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--nolisten") {
		t.Fatalf("expected --nolisten when --connect is set: %v", args)
	}
	if strings.Contains(joined, "--listen=") {
		t.Fatalf("must not listen inbound when peers are pinned: %v", args)
	}
	if !strings.Contains(joined, "--connect=seed:48333") {
		t.Fatalf("missing connect: %v", args)
	}
	if !strings.Contains(joined, "--addpeer=friend:48333") {
		t.Fatalf("missing addpeer: %v", args)
	}
	for _, a := range args {
		if a == "--generate" || strings.HasPrefix(a, "--generate=") {
			t.Fatal("mining must never be enabled")
		}
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
	if chainParamsForNetwork("signet").DefaultPort != "38333" {
		t.Fatalf("signet default port: got %q", chainParamsForNetwork("signet").DefaultPort)
	}
	if chainParamsForNetwork("mainnet").DefaultPort != "8333" {
		t.Fatalf("mainnet default port: got %q", chainParamsForNetwork("mainnet").DefaultPort)
	}
	if chainParamsForNetwork("testnet").DefaultPort != "18333" {
		t.Fatalf("testnet3 default port: got %q", chainParamsForNetwork("testnet").DefaultPort)
	}
}
