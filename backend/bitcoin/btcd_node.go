package bitcoin

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// BtcdMode values for BTCD_MODE.
const (
	BtcdModeManaged  = "managed"
	BtcdModeExternal = "external"
	BtcdModeOff      = "off"
)

// EmbeddedBtcd manages a local btcd child process (no mining).
type EmbeddedBtcd struct {
	cfg     NodeConfig
	cmd     *exec.Cmd
	mu      sync.Mutex
	started bool
	stopped bool
	cancel  context.CancelFunc
}

// NodeConfig configures the managed btcd process and RPC connection.
type NodeConfig struct {
	Mode         string // managed | external | off
	Bin          string
	DataDir      string
	Network      string
	RPCHost      string // host:port for RPC (listen and dial)
	P2PListen    string // e.g. 0.0.0.0:48333
	RPCUser      string
	RPCPass      string
	TxIndex      bool
	AddrIndex    bool
	ExtraArgs    []string
	AllowMainnet bool
}

// LoadNodeConfigFromEnv builds NodeConfig from environment variables.
func LoadNodeConfigFromEnv() NodeConfig {
	network := GetCurrentNetwork()
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("BTCD_MODE")))
	if mode == "" {
		mode = BtcdModeManaged
	}

	dataDir := os.Getenv("BTCD_DATADIR")
	if dataDir == "" {
		// Keep bitcoin package free of storage imports (cycle risk).
		if base := os.Getenv("STARGATE_DATA_DIR"); base != "" {
			dataDir = filepath.Join(base, "btcd")
		} else {
			dataDir = filepath.Join("data", "btcd")
		}
	}

	rpcHost := os.Getenv("BTCD_RPC_HOST")
	if rpcHost == "" {
		rpcHost = defaultRPCHost(network)
	}

	p2p := os.Getenv("BTCD_P2P_LISTEN")
	if p2p == "" {
		p2p = defaultP2PListen(network)
	}

	bin := os.Getenv("BTCD_BIN")
	if bin == "" {
		bin = "btcd"
	}

	txIndex := envBoolDefault("BTCD_TXINDEX", true)
	addrIndex := envBoolDefault("BTCD_ADDRINDEX", true)

	user := os.Getenv("BTCD_RPC_USER")
	pass := os.Getenv("BTCD_RPC_PASS")

	var extra []string
	if v := strings.TrimSpace(os.Getenv("BTCD_EXTRA_ARGS")); v != "" {
		extra = strings.Fields(v)
	}

	allowMain := envBoolDefault("BTCD_ALLOW_MAINNET", false)

	return NodeConfig{
		Mode:         mode,
		Bin:          bin,
		DataDir:      dataDir,
		Network:      network,
		RPCHost:      rpcHost,
		P2PListen:    p2p,
		RPCUser:      user,
		RPCPass:      pass,
		TxIndex:      txIndex,
		AddrIndex:    addrIndex,
		ExtraArgs:    extra,
		AllowMainnet: allowMain,
	}
}

func envBoolDefault(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func defaultRPCHost(network string) string {
	switch network {
	case "mainnet", "main":
		return "127.0.0.1:8334"
	case "testnet", "testnet3":
		return "127.0.0.1:18334"
	case "signet":
		return "127.0.0.1:38332"
	case "simnet":
		return "127.0.0.1:18556"
	case "regtest":
		return "127.0.0.1:18334"
	default: // testnet4
		return "127.0.0.1:48334"
	}
}

func defaultP2PListen(network string) string {
	switch network {
	case "mainnet", "main":
		return "0.0.0.0:8333"
	case "testnet", "testnet3":
		return "0.0.0.0:18333"
	case "signet":
		return "0.0.0.0:38333"
	case "simnet":
		return "0.0.0.0:18555"
	case "regtest":
		return "0.0.0.0:18444"
	default:
		return "0.0.0.0:48333"
	}
}

func networkFlag(network string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(network)) {
	case "mainnet", "main":
		return "", nil // mainnet is default for btcd
	case "testnet", "testnet3":
		return "--testnet", nil
	case "testnet4":
		return "--testnet4", nil
	case "signet":
		return "--signet", nil
	case "simnet":
		return "--simnet", nil
	case "regtest":
		return "--regtest", nil
	default:
		return "", fmt.Errorf("unsupported BITCOIN_NETWORK %q for btcd", network)
	}
}

// ensureRPCAuth loads or generates RPC credentials under DataDir.
func (cfg *NodeConfig) ensureRPCAuth() error {
	if cfg.RPCUser != "" && cfg.RPCPass != "" {
		return nil
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	authPath := filepath.Join(cfg.DataDir, "rpc.auth")
	if data, err := os.ReadFile(authPath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "rpcuser=") {
				cfg.RPCUser = strings.TrimPrefix(line, "rpcuser=")
			}
			if strings.HasPrefix(line, "rpcpass=") {
				cfg.RPCPass = strings.TrimPrefix(line, "rpcpass=")
			}
		}
		if cfg.RPCUser != "" && cfg.RPCPass != "" {
			return nil
		}
	}
	// Generate new credentials.
	userBytes := make([]byte, 8)
	passBytes := make([]byte, 16)
	if _, err := rand.Read(userBytes); err != nil {
		return err
	}
	if _, err := rand.Read(passBytes); err != nil {
		return err
	}
	cfg.RPCUser = "stargate_" + hex.EncodeToString(userBytes)
	cfg.RPCPass = hex.EncodeToString(passBytes)
	content := fmt.Sprintf("rpcuser=%s\nrpcpass=%s\n", cfg.RPCUser, cfg.RPCPass)
	if err := os.WriteFile(authPath, []byte(content), 0o600); err != nil {
		return err
	}
	log.Printf("btcd: wrote RPC credentials to %s", authPath)
	return nil
}

// Start launches btcd when mode=managed. No-op for external.
func (n *EmbeddedBtcd) Start(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.started {
		return nil
	}
	cfg := n.cfg

	if cfg.Network == "mainnet" || cfg.Network == "main" {
		if !cfg.AllowMainnet {
			return fmt.Errorf("mainnet requires BTCD_ALLOW_MAINNET=true (large disk)")
		}
	}

	if err := cfg.ensureRPCAuth(); err != nil {
		return fmt.Errorf("rpc auth: %w", err)
	}
	n.cfg = cfg

	if cfg.Mode != BtcdModeManaged {
		n.started = true
		return nil
	}

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}

	// Must run while btcd is not holding ffldb. A prior invalidateblock can
	// leave Valid+Invalid set together; reconsiderblock is then a no-op and
	// the node never connects the next block.
	if n, err := ClearStickyInvalidFlags(cfg.DataDir, cfg.Network); err != nil {
		log.Printf("btcd: index repair skipped: %v", err)
	} else {
		log.Printf("btcd: cleared %d known-invalid index flag(s) before start", n)
	}

	bin, err := exec.LookPath(cfg.Bin)
	if err != nil {
		return fmt.Errorf("btcd binary not found (%s): %w — install btcd or set BTCD_BIN / BTCD_MODE=external", cfg.Bin, err)
	}

	netFlag, err := networkFlag(cfg.Network)
	if err != nil {
		return err
	}

	// RPC listen must be host:port without scheme; btcd wants --rpclisten=
	rpcListen := cfg.RPCHost
	if !strings.Contains(rpcListen, ":") {
		rpcListen = "127.0.0.1:" + rpcListen
	}
	// Prefer binding localhost for RPC security.
	if host, port, ok := strings.Cut(rpcListen, ":"); ok {
		if host == "" || host == "0.0.0.0" {
			rpcListen = "127.0.0.1:" + port
		}
	}

	args := []string{
		"--datadir=" + cfg.DataDir,
		"--rpclisten=" + rpcListen,
		"--rpcuser=" + cfg.RPCUser,
		"--rpcpass=" + cfg.RPCPass,
		"--notls",
		"--listen=" + cfg.P2PListen,
	}
	if netFlag != "" {
		args = append(args, netFlag)
	}
	if cfg.TxIndex {
		args = append(args, "--txindex")
	}
	if cfg.AddrIndex {
		args = append(args, "--addrindex")
	}
	// Explicitly do not enable mining.
	args = append(args, cfg.ExtraArgs...)

	// Detach process lifetime from the caller's start context so RPC wait
	// cancellation does not kill btcd.
	procCtx, procCancel := context.WithCancel(context.Background())
	n.cancel = procCancel
	cmd := exec.CommandContext(procCtx, bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start btcd: %w", err)
	}
	n.cmd = cmd
	n.started = true

	go pipeLog("btcd", stdout)
	go pipeLog("btcd", stderr)
	go func() {
		err := cmd.Wait()
		n.mu.Lock()
		n.stopped = true
		n.mu.Unlock()
		if err != nil && ctx.Err() == nil {
			log.Printf("btcd: process exited: %v", err)
		} else {
			log.Printf("btcd: process exited")
		}
	}()

	log.Printf("btcd: started pid=%d network=%s datadir=%s rpc=%s p2p=%s txindex=%v addrindex=%v mining=false",
		cmd.Process.Pid, cfg.Network, cfg.DataDir, rpcListen, cfg.P2PListen, cfg.TxIndex, cfg.AddrIndex)
	return nil
}

func pipeLog(prefix string, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		log.Printf("%s: %s", prefix, line)
	}
}

// Stop terminates the managed btcd process.
func (n *EmbeddedBtcd) Stop() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.cmd == nil || n.cmd.Process == nil {
		n.started = false
		return nil
	}
	log.Printf("btcd: stopping pid=%d", n.cmd.Process.Pid)
	if n.cancel != nil {
		n.cancel()
	}
	// Signal process group.
	pgid, err := syscall.Getpgid(n.cmd.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = n.cmd.Process.Signal(syscall.SIGTERM)
	}
	done := make(chan error, 1)
	go func() { done <- n.cmd.Wait() }()
	select {
	case <-time.After(15 * time.Second):
		log.Printf("btcd: force kill")
		if err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			_ = n.cmd.Process.Kill()
		}
	case <-done:
	}
	n.cmd = nil
	n.started = false
	return nil
}

// Restart stops a managed btcd child (if any) and starts it again.
// Used by tip-lag recovery when the node is wedged behind the network tip.
func (n *EmbeddedBtcd) Restart(ctx context.Context) error {
	if n == nil {
		return fmt.Errorf("nil EmbeddedBtcd")
	}
	if err := n.Stop(); err != nil {
		log.Printf("btcd restart: stop warning: %v", err)
	}
	// Brief pause so ports/datadir locks release.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
	}
	return n.Start(ctx)
}

// WaitRPC polls until the backend answers getblockcount or timeout.
func WaitRPC(ctx context.Context, backend *BtcdBackend, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		last = backend.Ready(cctx)
		cancel()
		if last == nil {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	if last == nil {
		last = fmt.Errorf("timeout")
	}
	return fmt.Errorf("btcd RPC not ready after %s: %w", timeout, last)
}

// ChainRuntime holds the process (optional) and RPC backend.
type ChainRuntime struct {
	Node    *EmbeddedBtcd
	Backend ChainBackend
	Mode    string
}

// StartChainFromEnv starts managed btcd (if needed) and returns a ChainBackend.
// When BTCD_MODE=off, returns an Esplora-backed adapter for tests/dev only.
func StartChainFromEnv(ctx context.Context) (*ChainRuntime, error) {
	cfg := LoadNodeConfigFromEnv()
	log.Printf("bitcoin chain: mode=%s network=%s rpc=%s", cfg.Mode, cfg.Network, cfg.RPCHost)

	switch cfg.Mode {
	case BtcdModeOff:
		log.Printf("bitcoin chain: BTCD_MODE=off — using Esplora HTTP fallback (not recommended for production)")
		return &ChainRuntime{
			Backend: NewEsploraBackend(cfg.Network),
			Mode:    BtcdModeOff,
		}, nil

	case BtcdModeExternal, BtcdModeManaged:
		node := &EmbeddedBtcd{cfg: cfg}
		if err := node.Start(ctx); err != nil {
			return nil, err
		}
		if err := cfg.ensureRPCAuth(); err != nil {
			_ = node.Stop()
			return nil, err
		}
		backend, err := NewBtcdBackend(BtcdRPCConfig{
			Host:         cfg.RPCHost,
			User:         cfg.RPCUser,
			Pass:         cfg.RPCPass,
			Network:      cfg.Network,
			DisableTLS:   true,
			HTTPPostMode: true,
		})
		if err != nil {
			_ = node.Stop()
			return nil, err
		}
		wait := 2 * time.Minute
		if cfg.Mode == BtcdModeExternal {
			wait = 30 * time.Second
		}
		if err := WaitRPC(ctx, backend, wait); err != nil {
			_ = backend.Close()
			_ = node.Stop()
			return nil, err
		}
		rt := &ChainRuntime{
			Node:    node,
			Backend: backend,
			Mode:    cfg.Mode,
		}
		// Log sync status + external tip lag without blocking forever.
		go logSyncProgress(ctx, rt)

		return rt, nil

	default:
		return nil, fmt.Errorf("unknown BTCD_MODE %q (want managed|external|off)", cfg.Mode)
	}
}

func logSyncProgress(ctx context.Context, rt *ChainRuntime) {
	if rt == nil || rt.Backend == nil {
		return
	}
	backend := rt.Backend
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			st, err := backend.NodeStatus(ctx)
			if err != nil {
				log.Printf("btcd sync: status error: %v", err)
				continue
			}
			log.Printf("btcd sync: blocks=%v headers=%v peers=%v ibd=%v progress=%v synced=%v",
				st["blocks"], st["headers"], st["peers"], st["initial_block_download"],
				st["verification_progress"], st["synced"])

			// External tip lag check (mempool.space / blockstream reference).
			// Surfaces stuck nodes that report local synced=true but lag the network
			// (e.g. future-timestamp block rejects on testnet4).
			if tipLagExternalCheckEnabled() {
				localTip, _ := toInt64(st["blocks"])
				if localTip == 0 {
					if h, herr := backend.GetTipHeight(ctx); herr == nil {
						localTip = h
					}
				}
				cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
				extTip, extErr := FetchExternalTipHeight(cctx, backend.Network())
				cancel()
				lag := EvaluateTipLag(localTip, extTip, tipLagThreshold(), extErr, time.Now())
				if extErr != nil {
					log.Printf("btcd sync: external tip check failed: %v", extErr)
				} else if lag.Lagging {
					log.Printf("btcd sync: TIP LAG local=%d external=%d lag=%d threshold=%d duration=%s — node may be stuck (future-timestamp rejects or peer stall)",
						lag.LocalTip, lag.ExternalTip, lag.LagBlocks, lag.Threshold, lag.LagDuration)
					if rt.Mode == BtcdModeManaged {
						maybeRestartManagedBtcd(ctx, rt.Node, lag)
					}
				} else if lag.LagBlocks > 0 {
					log.Printf("btcd sync: external tip=%d local=%d lag=%d (within threshold=%d)",
						lag.ExternalTip, lag.LocalTip, lag.LagBlocks, lag.Threshold)
				}
			}
		}
	}
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	case uint64:
		return int64(n), true
	case uint32:
		return int64(n), true
	default:
		return 0, false
	}
}

// Close shuts down backend and managed process.
func (r *ChainRuntime) Close() error {
	var errs []string
	if r.Backend != nil {
		if err := r.Backend.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if r.Node != nil {
		if err := r.Node.Stop(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("chain close: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ParseRPCPort extracts port for diagnostics.
