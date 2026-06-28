package ipfs

import (
	"context"
	"fmt"
	"io"
	"log"
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
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	multiaddr "github.com/multiformats/go-multiaddr"
)

// defaultBootstrapPeers are the IPFS network bootstrap nodes operated by
// Protocol Labs.  Without these the embedded node is completely isolated and
// cannot discover or serve content to any other IPFS peer.
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
	RepoPath    string
	ListenAddrs []string
	Bootstrap   []string
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
	//    can both discover content and announce (provide) its own.
	var idht *dht.IpfsDHT
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(cfg.ListenAddrs...),
		libp2p.NATPortMap(),
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

	// 8. Initialize Reprovider — periodically announces all blocks in the
	//    blockstore to the DHT so other peers can discover and fetch them.
	bstoreKeyProvider := func(ctx context.Context) (<-chan cid.Cid, error) {
		return bstore.AllKeysChan(ctx)
	}
	reprov, err := provider.New(dstore,
		provider.Online(idht),
		provider.ReproviderInterval(22*time.Hour),
		provider.KeyProvider(bstoreKeyProvider),
	)
	if err != nil {
		log.Printf("Warning: failed to initialize reprovider: %v (content won't be announced)", err)
	}

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
	//    participate in the DHT and exchange blocks with the rest of the network.
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
// network so other peers can discover and fetch it.
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
