package ipfs

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	connmgr "github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	multiaddr "github.com/multiformats/go-multiaddr"
)

// defaultBootstrapPeers are the IPFS network bootstrap nodes operated by
// Protocol Labs. They allow the embedded node to join the public DHT for
// decentralised peer discovery — this avoids making any single server
// (e.g. starlight-ai.freemyip.com) the sole bottleneck.
//
// Note: the full AllKeysChan reprovider is intentionally kept disabled
// because at 20k+ data blocks the periodic blockstore scan causes heavy
// filesystem I/O. Discovery and bitswap still work; only the expensive
// bulk re-announcement is skipped.
//
// Additional bootstrap peers (e.g. your own stargate instances) can be
// supplied via IPFS_EMBEDDED_BOOTSTRAP (comma-separated multiaddrs).
var defaultBootstrapPeers = []string{
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

// NodeConfig holds configuration for the embedded node
type NodeConfig struct {
	RepoPath      string
	ListenAddrs   []string
	Bootstrap     []string
	ConnLowWater  int // low watermark for libp2p connection manager (0 = default)
	ConnHighWater int // high watermark for libp2p connection manager (0 = default)
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

	// 3. Initialize libp2p Host with DHT in auto-server mode so the node
	//    can discover content and announce (provide) its own CIDs.
	//    The full AllKeysChan reprovider is kept disabled to avoid heavy
	//    filesystem I/O at scale (20k+ blocks); only per-CID Provide()
	//    calls in Add() are used when a reprovider is present.
	//
	// Connection manager is configured to bound the number of peers/connections.
	// Without it the node accumulates yamux/bitswap goroutines as the DHT and
	// gossipsub peer exchange discover more peers over time.
	low := cfg.ConnLowWater
	if low <= 0 {
		low = 20
	}
	high := cfg.ConnHighWater
	if high <= 0 {
		high = 80
	}
	cm, err := connmgr.NewConnManager(low, high,
		connmgr.WithGracePeriod(1*time.Minute),
	)
	if err != nil {
		dstore.Close()
		cancel()
		return nil, fmt.Errorf("failed to create connection manager: %w", err)
	}

	var idht *dht.IpfsDHT
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(cfg.ListenAddrs...),
		libp2p.NATPortMap(),
		libp2p.ConnectionManager(cm),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			idht, err = dht.New(nodeCtx, h, dht.Mode(dht.ModeAutoServer))
			return idht, err
		}),
	)
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

	// 7. Initialize Pubsub with DHT-based peer discovery so nodes
	//    subscribed to the same topic can find and connect to each other.
	//    The topic guard in getTopic() still restricts participation to
	//    stargate-uploads only, so the node won't forward messages for
	//    arbitrary topics even though it can discover peers via the DHT.
	routingDiscovery := drouting.NewRoutingDiscovery(idht)
	ps, err := pubsub.NewGossipSub(nodeCtx, h,
		pubsub.WithDiscovery(routingDiscovery),
		pubsub.WithPeerExchange(true),
	)
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

	// 9. Bootstrap — connect to IPFS network peers so the node can
	//    participate in the DHT and exchange blocks with other peers.
	//    IPFS_EMBEDDED_BOOTSTRAP adds extra peers (e.g. your own stargate
	//    instances) alongside the Protocol Labs defaults.
	bootstrapPeers := cfg.Bootstrap
	if len(bootstrapPeers) == 0 {
		bootstrapPeers = defaultBootstrapPeers
	}
	go node.bootstrap(bootstrapPeers)

	// 10. mDNS discovery — find stargate peers on the local network / k8s
	//     cluster without relying on the public DHT.
	mdnsSvc := mdns.NewMdnsService(h, "stargate-ipfs", &mdnsNotifee{host: h})
	if err := mdnsSvc.Start(); err != nil {
		log.Printf("Warning: mDNS discovery failed to start: %v", err)
	}

	log.Printf("Embedded IPFS node started. PeerID: %s, Addrs: %v", h.ID(), h.Addrs())
	return node, nil
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
	for _, p := range peers {
		ma, err := multiaddr.NewMultiaddr(p)
		if err != nil {
			log.Printf("Invalid bootstrap multiaddr %s: %v", p, err)
			continue
		}
		pi, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			log.Printf("Invalid bootstrap addrinfo %s: %v", p, err)
			continue
		}
		if err := n.host.Connect(n.ctx, *pi); err != nil {
			log.Printf("Failed to connect to bootstrap peer %s: %v", pi.ID, err)
		} else {
			log.Printf("Connected to bootstrap peer %s", pi.ID)
		}
	}

	// Refresh DHT
	if n.dht != nil {
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

// PubsubSubscribe subscribes to a topic and returns a channel of messages
func (n *EmbeddedNode) PubsubSubscribe(ctx context.Context, topicName string) (<-chan []byte, error) {
	t, err := n.getTopic(topicName)
	if err != nil {
		return nil, err
	}

	sub, err := t.Subscribe()
	if err != nil {
		return nil, err
	}

	out := make(chan []byte, 100)
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
			out <- msg.Data
		}
	}()

	return out, nil
}

func (n *EmbeddedNode) getTopic(name string) (*pubsub.Topic, error) {
	n.topicsMu.Lock()
	defer n.topicsMu.Unlock()

	// Cap to stargate-uploads topic only.
	//
	// stargate-uploads (the file mirror) is the proven replication path.
	// Other topics (e.g. "stargate-stego") are secondary features that
	// have not been shown to be robust and are vulnerable to abuse.
	// Even though the node now participates in the public DHT for peer
	// discovery, we restrict pubsub to prevent forwarding messages for
	// arbitrary topics.
	allowed := os.Getenv("IPFS_MIRROR_TOPIC")
	if allowed == "" {
		allowed = "stargate-uploads"
	}
	if name != allowed {
		return nil, fmt.Errorf("refusing pubsub topic %q (node is capped to %s only; no forwarding of other data)", name, allowed)
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
