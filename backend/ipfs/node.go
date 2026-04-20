package ipfs

import (
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"sync"

	"github.com/ipfs/boxo/bitswap"
	"github.com/ipfs/boxo/bitswap/network/bsnet"
	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/blockstore"
	chunk "github.com/ipfs/boxo/chunker"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs/importer/balanced"
	"github.com/ipfs/boxo/ipld/unixfs/importer/helpers"
	unixfsio "github.com/ipfs/boxo/ipld/unixfs/io"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	leveldb "github.com/ipfs/go-ds-leveldb"
	format "github.com/ipfs/go-ipld-format"
	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	multiaddr "github.com/multiformats/go-multiaddr"
)

// EmbeddedNode is a minimal IPFS node embedded in the application
type EmbeddedNode struct {
	ctx      context.Context
	cancel   func()
	host     host.Host
	dht      *dht.IpfsDHT
	ps       *pubsub.PubSub
	dstore   datastore.Batching
	bstore   blockstore.Blockstore
	bserv    blockservice.BlockService
	dag      format.DAGService
	topics   map[string]*pubsub.Topic
	topicsMu sync.RWMutex
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

	// 3. Initialize libp2p Host
	var idht *dht.IpfsDHT
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(cfg.ListenAddrs...),
		libp2p.NATPortMap(),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			idht, err = dht.New(nodeCtx, h)
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

	// 7. Initialize Pubsub
	ps, err := pubsub.NewGossipSub(nodeCtx, h)
	if err != nil {
		h.Close()
		dstore.Close()
		cancel()
		return nil, fmt.Errorf("failed to initialize pubsub: %w", err)
	}

	node := &EmbeddedNode{
		ctx:    nodeCtx,
		cancel: cancel,
		host:   h,
		dht:    idht,
		ps:     ps,
		dstore: dstore,
		bstore: bstore,
		bserv:  bserv,
		dag:    dag,
		topics: make(map[string]*pubsub.Topic),
	}

	// 8. Bootstrap
	if len(cfg.Bootstrap) > 0 {
		go node.bootstrap(cfg.Bootstrap)
	}

	log.Printf("Embedded IPFS node started. PeerID: %s, Addrs: %v", h.ID(), h.Addrs())
	return node, nil
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

// Add adds data to the embedded IPFS node
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

	return root.Cid(), nil
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
func (n *EmbeddedNode) PubsubPublish(ctx context.Context, topic string, data []byte) error {
	t, err := n.getTopic(topic)
	if err != nil {
		return err
	}
	return t.Publish(ctx, data)
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
