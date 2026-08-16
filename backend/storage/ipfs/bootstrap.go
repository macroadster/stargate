package ipfs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	multiaddr "github.com/multiformats/go-multiaddr"
)

// Default swarm listen port used when an HTTP bootstrap URL does not encode one.
const defaultSwarmPort = 4001

// defaultHTTPStatusPath is appended to a Stargate HTTP base URL to discover
// the live embedded IPFS peer ID (stable across restarts when identity is
// persisted under STARGATE_DATA_DIR).
const defaultHTTPStatusPath = "/api/ipfs-mirror/status"

// expandBootstrapPeers resolves incomplete bootstrap entries (hostname, HTTP
// URL, or multiaddr without /p2p/) into full /…/p2p/<PeerID> multiaddrs by
// querying GET {base}/api/ipfs-mirror/status. Entries that already include a
// peer ID are returned unchanged.
//
// Supported entry forms:
//
//	/ip4/…/tcp/4001/p2p/12D3…     → as-is
//	/dns4/host/tcp/4001           → HTTP discover peer ID, append /p2p/…
//	https://starlight-ai.example  → GET …/api/ipfs-mirror/status, dial host:4001
//	starlight-ai.example          → same as https://starlight-ai.example
//	public / default              → already expanded by resolveBootstrapPeers
func expandBootstrapPeers(ctx context.Context, entries []string) []string {
	if len(entries) == 0 {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		full, err := resolveBootstrapEntry(ctx, e)
		if err != nil {
			// Keep the original entry so bootstrap() can log a precise parse error.
			logBootstrapResolve(e, err)
			out = append(out, e)
			continue
		}
		if full != e {
			logBootstrapResolveOK(e, full)
		}
		out = append(out, full)
	}
	return out
}

func logBootstrapResolve(entry string, err error) {
	log.Printf("IPFS bootstrap: failed to resolve %q: %v (will try raw entry)", entry, err)
}

func logBootstrapResolveOK(entry, full string) {
	log.Printf("IPFS bootstrap: resolved %q → %s", entry, full)
}

// resolveBootstrapEntry turns one bootstrap token into a full p2p multiaddr.
func resolveBootstrapEntry(ctx context.Context, entry string) (string, error) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return "", fmt.Errorf("empty bootstrap entry")
	}

	// Full multiaddr with peer ID — no HTTP round-trip.
	if strings.HasPrefix(entry, "/") {
		if hasP2PComponent(entry) {
			if _, err := multiaddr.NewMultiaddr(entry); err != nil {
				return "", fmt.Errorf("invalid multiaddr: %w", err)
			}
			return entry, nil
		}
		return resolveMultiaddrWithoutPeerID(ctx, entry)
	}

	// HTTP(S) Stargate base URL.
	if strings.HasPrefix(entry, "http://") || strings.HasPrefix(entry, "https://") {
		return resolveHTTPBootstrap(ctx, entry, defaultSwarmPort)
	}

	// Bare hostname or host:httpPort (not a multiaddr).
	// Treat as HTTPS Stargate base; swarm stays on defaultSwarmPort.
	base := "https://" + entry
	return resolveHTTPBootstrap(ctx, base, defaultSwarmPort)
}

func hasP2PComponent(maStr string) bool {
	// multiaddr component is "/p2p/<id>" (also legacy "/ipfs/<id>").
	return strings.Contains(maStr, "/p2p/") || strings.Contains(maStr, "/ipfs/")
}

func resolveMultiaddrWithoutPeerID(ctx context.Context, maStr string) (string, error) {
	ma, err := multiaddr.NewMultiaddr(maStr)
	if err != nil {
		return "", fmt.Errorf("invalid multiaddr: %w", err)
	}
	host, swarmPort, err := hostPortFromMultiaddr(ma)
	if err != nil {
		return "", err
	}
	peerID, err := discoverPeerID(ctx, host, swarmPort)
	if err != nil {
		return "", err
	}
	return appendPeerID(maStr, peerID)
}

func resolveHTTPBootstrap(ctx context.Context, baseURL string, swarmPort int) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid bootstrap URL: %w", err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("bootstrap URL missing host: %s", baseURL)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("bootstrap URL missing hostname: %s", baseURL)
	}

	// Prefer status from the given base (path may already include a prefix).
	statusURL := strings.TrimRight(baseURL, "/") + defaultHTTPStatusPath
	peerID, err := fetchMirrorPeerID(ctx, statusURL)
	if err != nil {
		// Only fall back on transport/HTTP failures. An authoritative empty or
		// invalid peer_id from a successful JSON response is definitive.
		if !isDiscoveryTransportError(err) {
			return "", fmt.Errorf("discover peer id for %s: %w", host, err)
		}
		fallbacks := peerIDDiscoveryURLs(host, u.Scheme)
		var last error = err
		for _, fu := range fallbacks {
			if fu == statusURL {
				continue
			}
			peerID, last = fetchMirrorPeerID(ctx, fu)
			if last == nil {
				err = nil
				break
			}
			if !isDiscoveryTransportError(last) {
				// Got a clear application-level answer from another endpoint.
				return "", fmt.Errorf("discover peer id for %s: %w", host, last)
			}
		}
		if err != nil {
			return "", fmt.Errorf("discover peer id for %s: %w", host, last)
		}
	}

	transport, err := transportMultiaddr(host, swarmPort)
	if err != nil {
		return "", err
	}
	return appendPeerID(transport, peerID)
}

// isDiscoveryTransportError reports whether peer-id discovery failed because
// the status endpoint was unreachable or returned a non-authoritative error
// (worth trying alternate URLs), versus a successful JSON body that clearly
// says peer_id is missing/invalid (do not fall back — that would paper over
// "mirror disabled" on the intended host by probing random ports).
func isDiscoveryTransportError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "empty peer_id") ||
		strings.Contains(msg, "invalid peer_id") ||
		strings.Contains(msg, "decode:") {
		return false
	}
	return true
}

// peerIDDiscoveryURLs lists candidate status endpoints for a host.
func peerIDDiscoveryURLs(host, preferredScheme string) []string {
	schemes := []string{}
	if preferredScheme == "http" || preferredScheme == "https" {
		schemes = append(schemes, preferredScheme)
	}
	for _, s := range []string{"https", "http"} {
		if s != preferredScheme {
			schemes = append(schemes, s)
		}
	}
	var out []string
	for _, s := range schemes {
		out = append(out, fmt.Sprintf("%s://%s%s", s, host, defaultHTTPStatusPath))
		// Local / in-cluster Stargate often serves HTTP on 3001.
		out = append(out, fmt.Sprintf("%s://%s:3001%s", s, host, defaultHTTPStatusPath))
	}
	return out
}

// discoverPeerID tries HTTPS then HTTP status endpoints for a swarm host.
func discoverPeerID(ctx context.Context, host string, swarmPort int) (string, error) {
	_ = swarmPort // reserved if status ever embeds swarm info per-port
	var last error
	for _, u := range peerIDDiscoveryURLs(host, "https") {
		id, err := fetchMirrorPeerID(ctx, u)
		if err == nil {
			return id, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("no discovery URLs")
	}
	return "", last
}

type mirrorStatusPeerJSON struct {
	PeerID  string `json:"peer_id"`
	Enabled bool   `json:"enabled"`
}

func fetchMirrorPeerID(ctx context.Context, statusURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: HTTP %d", statusURL, resp.StatusCode)
	}
	var st mirrorStatusPeerJSON
	if err := json.Unmarshal(body, &st); err != nil {
		return "", fmt.Errorf("%s: decode: %w", statusURL, err)
	}
	id := strings.TrimSpace(st.PeerID)
	if id == "" {
		return "", fmt.Errorf("%s: empty peer_id (mirror disabled or IPFS not ready)", statusURL)
	}
	// Validate peer ID encoding early.
	if _, err := peer.Decode(id); err != nil {
		return "", fmt.Errorf("%s: invalid peer_id %q: %w", statusURL, id, err)
	}
	return id, nil
}

func hostPortFromMultiaddr(ma multiaddr.Multiaddr) (host string, port int, err error) {
	port = defaultSwarmPort
	multiaddr.ForEach(ma, func(c multiaddr.Component) bool {
		switch c.Protocol().Code {
		case multiaddr.P_IP4, multiaddr.P_IP6, multiaddr.P_DNS, multiaddr.P_DNS4, multiaddr.P_DNS6:
			host = c.Value()
		case multiaddr.P_TCP, multiaddr.P_UDP:
			if p, e := strconv.Atoi(c.Value()); e == nil && p > 0 {
				port = p
			}
		}
		return true
	})
	if host == "" {
		return "", 0, fmt.Errorf("multiaddr has no host component: %s", ma)
	}
	return host, port, nil
}

func transportMultiaddr(host string, port int) (string, error) {
	if port <= 0 {
		port = defaultSwarmPort
	}
	var proto string
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			proto = "ip4"
		} else {
			proto = "ip6"
		}
	} else {
		proto = "dns4"
	}
	s := fmt.Sprintf("/%s/%s/tcp/%d", proto, host, port)
	if _, err := multiaddr.NewMultiaddr(s); err != nil {
		return "", fmt.Errorf("build transport multiaddr: %w", err)
	}
	return s, nil
}

func appendPeerID(transportMA, peerID string) (string, error) {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return "", fmt.Errorf("empty peer id")
	}
	if _, err := peer.Decode(peerID); err != nil {
		return "", fmt.Errorf("invalid peer id: %w", err)
	}
	// Strip any trailing /p2p already present.
	base := strings.TrimRight(transportMA, "/")
	if hasP2PComponent(base) {
		// Replace existing p2p component.
		if i := strings.Index(base, "/p2p/"); i >= 0 {
			base = base[:i]
		} else if i := strings.Index(base, "/ipfs/"); i >= 0 {
			base = base[:i]
		}
	}
	full := base + "/p2p/" + peerID
	if _, err := multiaddr.NewMultiaddr(full); err != nil {
		return "", err
	}
	return full, nil
}
