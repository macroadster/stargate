package ipfs

import (
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ipfs/boxo/bitswap"
	"github.com/ipfs/boxo/bitswap/network/bsnet"
	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/blockstore"
	chunk "github.com/ipfs/boxo/chunker"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs/importer/balanced"
	"github.com/ipfs/boxo/ipld/unixfs/importer/helpers"
	unixfsio "github.com/ipfs/boxo/ipld/unixfs/io"
	"github.com/ipfs/boxo/provider"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	leveldb "github.com/ipfs/go-ds-leveldb"
	format "github.com/ipfs/go-ipld-format"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	connmgr "github.com/libp2p/go-libp2p/p2p/net/connmgr"
	multiaddr "github.com/multiformats/go-multiaddr"
)

// defaultStarlightBootstrap is used when IPFS_EMBEDDED_BOOTSTRAP is unset.
// Peer ID is resolved at dial time via GET /api/ipfs-mirror/status (see
// expandBootstrapPeers). Local identity is persisted under STARGATE_DATA_DIR
// so this node's PeerID is stable across restarts.
// Set IPFS_EMBEDDED_BOOTSTRAP=none for private mesh (mDNS only), or =public
// for the Protocol Labs public DHT (CPU-heavy).
const defaultStarlightBootstrap = "starlight-ai.freemyip.com"

// publicBootstrapPeers are Protocol Labs public IPFS bootstrap nodes.
// They are ONLY used when explicitly requested via IPFS_EMBEDDED_BOOTSTRAP=public
// (or IPFS_JOIN_PUBLIC_IPFS=true). Joining the public DHT by default burns
// multi-core CPU on continuous peer discovery, dialing, bitswap queues, and GC.
//
// Note: the full AllKeysChan reprovider is intentionally kept disabled
// because at 20k+ data blocks the periodic blockstore scan causes heavy
// filesystem I/O. Discovery and bitswap still work; only the expensive
// bulk re-announcement is skipped.
var publicBootstrapPeers = []string{
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt",
	"/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ",
}

// EmbeddedNode is a minimal IPFS node embedded in the application
type EmbeddedNode struct {
	ctx        context.Context
	cancel     func()
	host       host.Host
	dht        *dht.IpfsDHT
	ps         *pubsub.PubSub
	dstore     datastore.Batching
	bstore     blockstore.Blockstore
	bserv      blockservice.BlockService
	dag        format.DAGService
	reprovider provider.System
	topics     map[string]*pubsub.Topic
	topicsMu   sync.RWMutex
}

// DHT mode strings accepted by NodeConfig.DHTMode / IPFS_DHT_MODE.
const (
	DHTModeClient = "client"
	DHTModeServer = "server"
	DHTModeAuto   = "auto"
)

// NodeConfig holds configuration for the embedded node
type NodeConfig struct {
	RepoPath      string
	ListenAddrs   []string
	Bootstrap     []string
	ConnLowWater  int // low watermark for libp2p connection manager (0 = default)
	ConnHighWater int // high watermark for libp2p connection manager (0 = default)

	// DHTMode is "client" (default), "server", or "auto".
	// Client mode does not serve DHT queries for the wider network.
	DHTMode string

	// PeerExchange enables gossipsub peer exchange. Off by default; it
	// aggressively grows the mesh and is a major source of CPU/goroutine
	// growth when any public peers are reachable.
	PeerExchange bool

	// RoutingDiscovery enables DHT-based pubsub peer discovery. Only useful
	// when bootstrap peers populate the DHT routing table.
	RoutingDiscovery bool

	// EnableNATPortMap enables UPnP/NAT-PMP. Off by default for private mesh.
	EnableNATPortMap bool

	// ForcePrivateReachability tells libp2p not to run public reachability
	// probing (AutoNAT dial-backs). On by default for private mesh.
	ForcePrivateReachability bool

	// IdentityPath is the protobuf libp2p private-key file. Empty uses
	// IdentityPath() ($STARGATE_DATA_DIR/ipfs_identity.key).
	IdentityPath string

	// Identity is an already-loaded private key. When set it wins over IdentityPath.
	Identity crypto.PrivKey
}

// resolveBootstrapPeers parses IPFS_EMBEDDED_BOOTSTRAP and optional public join flag.
//
//	"" / unset             → defaultStarlightBootstrap (starlight-ai.freemyip.com)
//	"none" / "off" / "private" / "local" → no bootstrap (private mesh; mDNS only)
//	"public" / "ipfs-public" → Protocol Labs public bootstrap set
//	full multiaddrs        → /ip4|dns4/.../tcp/4001/p2p/<PeerID> (used as-is)
//	partial multiaddrs     → /dns4/host/tcp/4001 (peer ID resolved via HTTP status)
//	http(s)://host         → peer ID from GET {url}/api/ipfs-mirror/status; dial host:4001
//	bare hostname          → same as https://hostname
//
// Incomplete entries are expanded at dial time by expandBootstrapPeers.
// joinPublic=true is equivalent to bootstrap="public" when bootstrap is empty.
//
// Note: the keyword "default" historically meant public IPFS; it still does for
// backward compatibility. The unset default (empty env) is Starlight's host.
func resolveBootstrapPeers(bootstrapEnv string, joinPublic bool) []string {
	raw := strings.TrimSpace(bootstrapEnv)
	if raw == "" {
		if joinPublic {
			return append([]string(nil), publicBootstrapPeers...)
		}
		return []string{defaultStarlightBootstrap}
	}
	lower := strings.ToLower(raw)
	if lower == "none" || lower == "off" || lower == "private" || lower == "local" {
		return nil
	}
	if lower == "public" || lower == "default" || lower == "ipfs-public" {
		return append([]string(nil), publicBootstrapPeers...)
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Allow mixing: "public,/ip4/..."
		if strings.EqualFold(p, "public") || strings.EqualFold(p, "default") || strings.EqualFold(p, "ipfs-public") {
			out = append(out, publicBootstrapPeers...)
			continue
		}
		if strings.EqualFold(p, "none") || strings.EqualFold(p, "off") ||
			strings.EqualFold(p, "private") || strings.EqualFold(p, "local") {
			continue
		}
		out = append(out, p)
	}
	return out
}

func dhtModeFromString(s string) dht.ModeOpt {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case DHTModeServer:
		return dht.ModeServer
	case DHTModeAuto, "autoserver", "auto-server":
		return dht.ModeAutoServer
	default:
		return dht.ModeClient
	}
}

// NewEmbeddedNode initializes and starts an embedded IPFS node
func NewEmbeddedNode(ctx context.Context, cfg NodeConfig) (*EmbeddedNode, error) {
	nodeCtx, cancel := context.WithCancel(ctx)

	// 1. Initialize Datastore
	dsPath := filepath.Join(cfg.RepoPath, "datastore")
	dstore, err := leveldb.NewDatastore(dsPath, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize datastore: %w", err)
	}

	// 2. Initialize Blockstore
	bstore := blockstore.NewBlockstore(dstore)
	bstore = blockstore.NewIdStore(bstore)

	// 3. Initialize libp2p Host.
	// Default is a private mesh (DHT client, no public bootstrap, no peer
	// exchange). Connection manager bounds peers so yamux/bitswap goroutines
	// cannot grow unbounded if discovery finds unexpected peers.
	low := cfg.ConnLowWater
	if low <= 0 {
		low = 10
	}
	high := cfg.ConnHighWater
	if high <= 0 {
		high = 40
	}
	if high <= low {
		high = low + 10
	}
	cm, err := connmgr.NewConnManager(low, high,
		connmgr.WithGracePeriod(30*time.Second),
	)
	if err != nil {
		dstore.Close()
		cancel()
		return nil, fmt.Errorf("failed to create connection manager: %w", err)
	}

	dhtMode := dhtModeFromString(cfg.DHTMode)
	bootstrapPeers := cfg.Bootstrap

	priv := cfg.Identity
	if priv == nil {
		idPath := strings.TrimSpace(cfg.IdentityPath)
		if idPath == "" {
			idPath = IdentityPath()
		}
		loaded, idErr := LoadOrCreateIdentity(idPath)
		if idErr != nil {
			dstore.Close()
			cancel()
			return nil, fmt.Errorf("failed to load IPFS identity: %w", idErr)
		}
		priv = loaded
	}

	var idht *dht.IpfsDHT
	opts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(cfg.ListenAddrs...),
		libp2p.ConnectionManager(cm),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			idht, err = dht.New(nodeCtx, h, dht.Mode(dhtMode))
			return idht, err
		}),
	}
	if cfg.EnableNATPortMap {
		opts = append(opts, libp2p.NATPortMap())
	}
	if cfg.ForcePrivateReachability {
		opts = append(opts, libp2p.ForceReachabilityPrivate())
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		dstore.Close()
		cancel()
		return nil, fmt.Errorf("failed to initialize libp2p host: %w", err)
	}

	// 4. Initialize Bitswap
	// NewFromIpfsHost returns BitSwapNetwork
	bsnetwork := bsnet.NewFromIpfsHost(h)
	// bitswap.New(ctx, network, providerFinder, blockstore, ...options)
	ex := bitswap.New(nodeCtx, bsnetwork, idht, bstore)

	// 5. Initialize BlockService
	bserv := blockservice.New(bstore, ex)

	// 6. Initialize DAGService
	dag := merkledag.NewDAGService(bserv)

	// 7. Initialize Pubsub. DHT discovery and peer exchange are opt-in —
	// both amplify mesh growth and CPU when public peers are reachable.
	// Topic guard in getTopic() caps participation to the uploads mirror
	// and the inscribed-wish topic.
	var psOpts []pubsub.Option
	if cfg.RoutingDiscovery && idht != nil {
		routingDiscovery := drouting.NewRoutingDiscovery(idht)
		psOpts = append(psOpts, pubsub.WithDiscovery(routingDiscovery))
	}
	if cfg.PeerExchange {
		psOpts = append(psOpts, pubsub.WithPeerExchange(true))
	}
	ps, err := pubsub.NewGossipSub(nodeCtx, h, psOpts...)
	if err != nil {
		h.Close()
		dstore.Close()
		cancel()
		return nil, fmt.Errorf("failed to initialize pubsub: %w", err)
	}

	// 8. Reprovider is intentionally disabled.
	//    The full AllKeysChan reprovider scans the entire blockstore every
	//    22 hours. At 20k+ data blocks this causes heavy filesystem I/O
	//    (LevelDB iteration) and CPU burn. Only explicit per-CID Provide()
	//    calls in Add() are used, keeping the cost proportional to new
	//    content rather than total content.
	var reprov provider.System

	node := &EmbeddedNode{
		ctx:        nodeCtx,
		cancel:     cancel,
		host:       h,
		dht:        idht,
		ps:         ps,
		dstore:     dstore,
		bstore:     bstore,
		bserv:      bserv,
		dag:        dag,
		reprovider: reprov,
		topics:     make(map[string]*pubsub.Topic),
	}

	// 9. Bootstrap peers (default: starlight-ai.freemyip.com; none = private mesh).
	if len(bootstrapPeers) == 0 {
		log.Printf("Embedded IPFS: private mesh mode (no bootstrap peers; unset IPFS_EMBEDDED_BOOTSTRAP defaults to %s, or set =public for public DHT)", defaultStarlightBootstrap)
	} else {
		go node.bootstrap(bootstrapPeers)
	}

	// 10. mDNS discovery — find stargate peers on the local network / k8s
	//     cluster without relying on the public DHT.
	mdnsSvc := mdns.NewMdnsService(h, "stargate-ipfs", &mdnsNotifee{host: h})
	if err := mdnsSvc.Start(); err != nil {
		log.Printf("Warning: mDNS discovery failed to start: %v", err)
	}

	// Join allowlisted file-mirror topics at start so peers see
	// stargate-uploads and stargate-wishes without waiting for the first
	// publish or subscribe.
	for _, name := range AllowedPubsubTopics() {
		if _, err := node.getTopic(name); err != nil {
			log.Printf("Embedded IPFS: failed to join topic %q: %v", name, err)
		}
	}

	log.Printf("Embedded IPFS node started. PeerID: %s, Addrs: %v, dht=%s, bootstrap=%d, peer_exchange=%v, routing_discovery=%v, connmgr=%d/%d, topics=%v",
		h.ID(), h.Addrs(), dhtModeLabel(dhtMode), len(bootstrapPeers), cfg.PeerExchange, cfg.RoutingDiscovery, low, high, node.JoinedTopics())
	return node, nil
}

func dhtModeLabel(m dht.ModeOpt) string {
	switch m {
	case dht.ModeServer:
		return DHTModeServer
	case dht.ModeAutoServer:
		return DHTModeAuto
	default:
		return DHTModeClient
	}
}

// mdnsNotifee automatically connects to peers discovered on the local network.
type mdnsNotifee struct {
	host host.Host
}

func (n *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.host.ID() {
		return
	}
	if err := n.host.Connect(context.Background(), pi); err != nil {
		log.Printf("mDNS: failed to connect to discovered peer %s: %v", pi.ID.String(), err)
	} else {
		log.Printf("mDNS: connected to discovered peer %s", pi.ID.String())
	}
}

func (n *EmbeddedNode) bootstrap(peers []string) {
	// Resolve hostname/URL / multiaddr-without-p2p entries to full multiaddrs
	// using GET /api/ipfs-mirror/status so peer IDs that change on server
	// restart do not require operators to update bootstrap env vars.
	peers = expandBootstrapPeers(n.ctx, peers)

	connected := 0
	for _, p := range peers {
		ma, err := multiaddr.NewMultiaddr(p)
		if err != nil {
			log.Printf("Invalid bootstrap multiaddr %s: %v", p, err)
			continue
		}
		pi, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			log.Printf("Invalid bootstrap addrinfo %s: %v (need /p2p/<PeerID> or resolvable host/URL)", p, err)
			continue
		}
		if err := n.host.Connect(n.ctx, *pi); err != nil {
			log.Printf("Failed to connect to bootstrap peer %s: %v", pi.ID, err)
		} else {
			connected++
			log.Printf("Connected to bootstrap peer %s", pi.ID)
		}
	}

	// Only run DHT bootstrap when we actually reached at least one peer.
	// An empty routing table + DHT bootstrap against the public network is
	// what drove multi-core CPU and thousands of goroutines in production.
	if connected > 0 && n.dht != nil {
		go func() {
			if err := n.dht.Bootstrap(n.ctx); err != nil {
				log.Printf("DHT bootstrap error: %v", err)
			}
		}()
	}
}

// Add adds data to the embedded IPFS node and announces the CID to the
// DHT so other peers can discover and fetch it. The full reprovider is
// disabled to avoid filesystem I/O at scale; only this per-CID announce
// is used.
func (n *EmbeddedNode) Add(ctx context.Context, r io.Reader) (cid.Cid, error) {
	params := helpers.DagBuilderParams{
		Dagserv:   n.dag,
		RawLeaves: true,
		Maxlinks:  helpers.DefaultLinksPerBlock,
		NoCopy:    false,
	}

	spl := chunk.NewSizeSplitter(r, chunk.DefaultBlockSize)
	dbh, err := params.New(spl)
	if err != nil {
		return cid.Undef, err
	}

	root, err := balanced.Layout(dbh)
	if err != nil {
		return cid.Undef, err
	}

	// Announce the new CID to the DHT so other nodes can find it.
	c := root.Cid()
	if n.reprovider != nil {
		if err := n.reprovider.Provide(ctx, c, true); err != nil {
			log.Printf("Warning: failed to provide CID %s to DHT: %v", c, err)
		}
	}

	return c, nil
}

// Cat retrieves data from the embedded IPFS node
func (n *EmbeddedNode) Cat(ctx context.Context, c cid.Cid) (io.ReadCloser, error) {
	nd, err := n.dag.Get(ctx, c)
	if err != nil {
		return nil, err
	}

	dr, err := unixfsio.NewDagReader(ctx, nd, n.dag)
	if err != nil {
		return nil, err
	}

	return dr, nil
}

// PubsubPublish publishes a message to a topic
func (n *EmbeddedNode) PubsubPublish(ctx context.Context, topicName string, data []byte) error {
	t, err := n.getTopic(topicName)
	if err != nil {
		return err
	}
	return t.Publish(ctx, data)
}

// PubsubInbound is one gossipsub message plus the mesh neighbor and publisher.
type PubsubInbound struct {
	Data         []byte
	ReceivedFrom string
	Publisher    string
}

// PubsubSubscribe subscribes to a topic and returns a channel of messages
func (n *EmbeddedNode) PubsubSubscribe(ctx context.Context, topicName string) (<-chan []byte, error) {
	ch, err := n.PubsubSubscribeDetailed(ctx, topicName)
	if err != nil {
		return nil, err
	}
	out := make(chan []byte, 100)
	go func() {
		defer close(out)
		for msg := range ch {
			out <- msg.Data
		}
	}()
	return out, nil
}

// PubsubSubscribeDetailed is PubsubSubscribe plus received_from / publisher.
func (n *EmbeddedNode) PubsubSubscribeDetailed(ctx context.Context, topicName string) (<-chan PubsubInbound, error) {
	t, err := n.getTopic(topicName)
	if err != nil {
		return nil, err
	}

	sub, err := t.Subscribe()
	if err != nil {
		return nil, err
	}

	out := make(chan PubsubInbound, 100)
	go func() {
		defer sub.Cancel()
		for {
			msg, err := sub.Next(n.ctx)
			if err != nil {
				if n.ctx.Err() == nil {
					log.Printf("Pubsub subscription to %s error: %v", topicName, err)
				}
				close(out)
				return
			}
			// Don't process our own messages
			if msg.ReceivedFrom == n.host.ID() {
				continue
			}
			in := PubsubInbound{
				Data:         msg.Data,
				ReceivedFrom: msg.ReceivedFrom.String(),
				Publisher:    msg.GetFrom().String(),
			}
			select {
			case out <- in:
			case <-n.ctx.Done():
				close(out)
				return
			}
		}
	}()

	return out, nil
}

// TopicPeers returns gossipsub mesh peers for an already-joined topic.
func (n *EmbeddedNode) TopicPeers(topicName string) []string {
	if n == nil {
		return nil
	}
	n.topicsMu.RLock()
	t := n.topics[strings.TrimSpace(topicName)]
	n.topicsMu.RUnlock()
	if t == nil {
		return nil
	}
	ids := t.ListPeers()
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	sort.Strings(out)
	return out
}

// SwarmPeers returns currently connected libp2p peers.
func (n *EmbeddedNode) SwarmPeers() []string {
	if n == nil || n.host == nil {
		return nil
	}
	ids := n.host.Network().Peers()
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	sort.Strings(out)
	return out
}

func (n *EmbeddedNode) getTopic(name string) (*pubsub.Topic, error) {
	n.topicsMu.Lock()
	defer n.topicsMu.Unlock()

	// Cap to the durable uploads mirror and the inscribed-wish topic.
	// Other topics (e.g. "stargate-stego") are not joined or forwarded.
	if !IsAllowedPubsubTopic(name) {
		return nil, fmt.Errorf("refusing pubsub topic %q (node is capped to %s; no forwarding of other data)", name, strings.Join(AllowedPubsubTopics(), ", "))
	}

	if t, ok := n.topics[name]; ok {
		return t, nil
	}

	t, err := n.ps.Join(name)
	if err != nil {
		return nil, err
	}
	n.topics[name] = t
	return t, nil
}

// JoinedTopics returns allowlisted pubsub topics this node has Joined.
func (n *EmbeddedNode) JoinedTopics() []string {
	if n == nil {
		return nil
	}
	n.topicsMu.RLock()
	defer n.topicsMu.RUnlock()
	out := make([]string, 0, len(n.topics))
	for name := range n.topics {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Close shuts down the embedded node
func (n *EmbeddedNode) Close() error {
	n.cancel()
	if n.reprovider != nil {
		n.reprovider.Close()
	}
	n.topicsMu.Lock()
	for _, t := range n.topics {
		t.Close()
	}
	n.topicsMu.Unlock()
	n.host.Close()
	if n.dstore != nil {
		n.dstore.Close()
	}
	return nil
}

// PeerID returns the host's peer ID
func (n *EmbeddedNode) PeerID() string {
	return n.host.ID().String()
}

// Unpin removes a CID and its reachable DAG blocks from the local blockstore.
func (n *EmbeddedNode) Unpin(ctx context.Context, c cid.Cid) error {
	if !c.Defined() {
		return nil
	}
	seen := make(map[string]struct{})
	return n.removeDAG(ctx, c, seen)
}

func (n *EmbeddedNode) removeDAG(ctx context.Context, c cid.Cid, seen map[string]struct{}) error {
	key := c.String()
	if _, ok := seen[key]; ok {
		return nil
	}
	seen[key] = struct{}{}

	nd, err := n.dag.Get(ctx, c)
	if err != nil {
		return n.bstore.DeleteBlock(ctx, c)
	}
	for _, l := range nd.Links() {
		_ = n.removeDAG(ctx, l.Cid, seen)
	}
	return n.bstore.DeleteBlock(ctx, c)
}
