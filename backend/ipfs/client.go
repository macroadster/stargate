package ipfs

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
)

var publicGateways = []string{
	"https://ipfs.io/ipfs/%s",
	"https://cloudflare-ipfs.com/ipfs/%s",
	"https://gateway.pinata.cloud/ipfs/%s",
	"https://dweb.link/ipfs/%s",
}

type Client struct {
	apiURL     string
	client     *http.Client
	storageDir string
	embedded   *EmbeddedNode
}

// IsEnabled returns true if IPFS is enabled globally
func IsEnabled() bool {
	// Check for global IPFS disable flag - only disable if explicitly set to "false"
	return strings.TrimSpace(os.Getenv("IPFS_ENABLED")) != "false"
}

var (
	globalClient *Client
	clientOnce   sync.Once
)

func NewClientFromEnv() *Client {
	clientOnce.Do(func() {
		if !IsEnabled() {
			return
		}
		apiURL := os.Getenv("IPFS_API_URL")
		if apiURL == "" {
			apiURL = "http://127.0.0.1:5001"
		}

		storageDir := os.Getenv("IPFS_STORAGE_DIR")
		if storageDir == "" {
			storageDir = "ipfs_objects"
		}
		_ = os.MkdirAll(storageDir, 0755)

		timeout := 30 * time.Second
		if raw := os.Getenv("IPFS_HTTP_TIMEOUT_SEC"); raw != "" {
			if v, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && v > 0 {
				timeout = time.Duration(v) * time.Second
			}
		}

		c := &Client{
			apiURL:     strings.TrimRight(apiURL, "/"),
			client:     &http.Client{Timeout: timeout},
			storageDir: storageDir,
		}

		// Check if embedded node should be started
		if strings.ToLower(os.Getenv("IPFS_EMBEDDED_ENABLED")) != "false" {
			repoPath := os.Getenv("IPFS_EMBEDDED_REPO")
			if repoPath == "" {
				repoPath = "ipfs_repo"
			}
			_ = os.MkdirAll(repoPath, 0755)

			listenAddr := os.Getenv("IPFS_EMBEDDED_LISTEN")
			if listenAddr == "" {
				listenAddr = "/ip4/0.0.0.0/tcp/4001"
			}

			var bootstrapPeers []string
			if envBootstrap := os.Getenv("IPFS_EMBEDDED_BOOTSTRAP"); envBootstrap != "" {
				bootstrapPeers = strings.Split(envBootstrap, ",")
			}

			node, err := NewEmbeddedNode(context.Background(), NodeConfig{
				RepoPath:    repoPath,
				ListenAddrs: []string{listenAddr},
				Bootstrap:   bootstrapPeers,
			})
			if err != nil {
				log.Printf("Warning: failed to start embedded IPFS node: %v", err)
			} else {
				c.embedded = node
			}
		}
		globalClient = c
	})

	return globalClient
}

func (c *Client) AddBytes(ctx context.Context, name string, data []byte) (string, error) {
	if c == nil {
		return "", fmt.Errorf("IPFS client is disabled")
	}

	// 1. Try embedded node first
	if c.embedded != nil {
		id, err := c.embedded.Add(ctx, bytes.NewReader(data))
		if err == nil {
			return id.String(), nil
		}
		log.Printf("Embedded IPFS add failed: %v, trying fallbacks", err)
	}

	// 2. Try local IPFS node API
	cid, err := c.addStream(ctx, name, bytes.NewReader(data))
	if err == nil {
		return cid, nil
	}

	// 3. Fallback to local storage
	log.Printf("IPFS node unavailable for add (%v), storing locally in %s", err, c.storageDir)
	hash := sha256.Sum256(data)
	localCID := fmt.Sprintf("local-%x", hash)
	target := filepath.Join(c.storageDir, localCID)
	if err := os.WriteFile(target, data, 0644); err != nil {
		return "", fmt.Errorf("failed to store IPFS object locally: %w", err)
	}

	return localCID, nil
}

// CheckNode returns nil if the IPFS node is reachable, otherwise returns an error.
func (c *Client) CheckNode(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("IPFS client is disabled")
	}
	if c.embedded != nil {
		return nil
	}
	reqURL := fmt.Sprintf("%s/api/v0/id", c.apiURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("IPFS node returned status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) addStream(ctx context.Context, name string, reader io.Reader) (string, error) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()
		part, err := writer.CreateFormFile("file", name)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, reader); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = writer.Close()
	}()

	reqURL := fmt.Sprintf("%s/api/v0/add?pin=true&cid-version=1", c.apiURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, pr)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if len(body) == 0 {
			return "", fmt.Errorf("ipfs add failed: %s", resp.Status)
		}
		return "", fmt.Errorf("ipfs add failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var lastHash string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var entry struct {
			Hash string `json:"Hash"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err == nil && entry.Hash != "" {
			lastHash = entry.Hash
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if lastHash == "" {
		return "", fmt.Errorf("ipfs add returned empty hash")
	}
	return lastHash, nil
}

func (c *Client) Cat(ctx context.Context, cidStr string) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("IPFS client is disabled")
	}
	if strings.TrimSpace(cidStr) == "" {
		return nil, fmt.Errorf("ipfs cat missing cid")
	}

	// 1. Try embedded node first
	if c.embedded != nil && !strings.HasPrefix(cidStr, "local-") {
		id, err := cid.Parse(cidStr)
		if err == nil {
			rc, err := c.embedded.Cat(ctx, id)
			if err == nil {
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
	}

	// 2. Try local filesystem storage (objects we "added" while node was down)
	localPath := filepath.Join(c.storageDir, cidStr)
	if data, err := os.ReadFile(localPath); err == nil {
		return data, nil
	}

	// 3. Try local IPFS node API (if configured/reachable)
	reqURL := fmt.Sprintf("%s/api/v0/cat?arg=%s", c.apiURL, url.QueryEscape(cidStr))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err == nil {
		resp, err := c.client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return io.ReadAll(resp.Body)
			}
		}
	}

	// 4. Try public IPFS gateways
	if !strings.HasPrefix(cidStr, "local-") {
		return c.catFromGateways(ctx, cidStr)
	}

	return nil, fmt.Errorf("ipfs cat failed: object %s not found locally or via gateways", cidStr)
}

func (c *Client) catFromGateways(ctx context.Context, cid string) ([]byte, error) {
	for _, gwTemplate := range publicGateways {
		gwURL := fmt.Sprintf(gwTemplate, cid)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, gwURL, nil)
		if err != nil {
			continue
		}

		resp, err := c.client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			data, err := io.ReadAll(resp.Body)
			if err == nil {
				// Cache locally for future use
				_ = os.WriteFile(filepath.Join(c.storageDir, cid), data, 0644)
				return data, nil
			}
		}
	}
	return nil, fmt.Errorf("failed to fetch CID %s from all public gateways", cid)
}

func (c *Client) PubsubPublish(ctx context.Context, topic string, message []byte) error {
	if c == nil {
		return fmt.Errorf("IPFS client is disabled")
	}
	if strings.TrimSpace(topic) == "" {
		return fmt.Errorf("ipfs pubsub missing topic")
	}

	// 1. Try embedded node first
	if c.embedded != nil {
		if err := c.embedded.PubsubPublish(ctx, topic, message); err == nil {
			return nil
		}
	}

	// 2. Try local IPFS node API
	encodedTopic := url.QueryEscape(multibaseEncodeString(topic))
	reqURL := fmt.Sprintf("%s/api/v0/pubsub/pub?arg=%s", c.apiURL, encodedTopic)

	body, contentType, err := multipartBody("data", "data", message)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.client.Do(req)
	if err != nil {
		log.Printf("IPFS pubsub publish skipped: local node unavailable (%v)", err)
		return nil // Non-blocking if node is missing
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("IPFS pubsub publish failed (status %d): %s", resp.StatusCode, string(body))
		return nil // Non-blocking
	}
	return nil
}

// PubsubSubscribe subscribes to a topic and returns a channel of message data.
// It handles both embedded node and external HTTP API streaming.
func (c *Client) PubsubSubscribe(ctx context.Context, topic string) (<-chan []byte, error) {
	if c == nil {
		return nil, fmt.Errorf("IPFS client is disabled")
	}

	// 1. Try embedded node first
	if c.embedded != nil {
		return c.embedded.PubsubSubscribe(ctx, topic)
	}

	// 2. Fallback to HTTP API streaming
	encodedTopic := url.QueryEscape(multibaseEncodeString(topic))
	reqURL := fmt.Sprintf("%s/api/v0/pubsub/sub?arg=%s", c.apiURL, encodedTopic)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("IPFS pubsub sub failed with status %d", resp.StatusCode)
	}

	out := make(chan []byte, 100)
	go func() {
		defer resp.Body.Close()
		defer close(out)

		decoder := json.NewDecoder(resp.Body)
		for {
			var msg struct {
				Data string `json:"data"`
			}
			if err := decoder.Decode(&msg); err != nil {
				if ctx.Err() == nil {
					log.Printf("IPFS HTTP pubsub decoder error: %v", err)
				}
				return
			}

			// External API usually sends base64/multibase data
			data := decodeMultibasePayload([]byte(msg.Data))
			if data == nil {
				data = decodeBase64Payload(msg.Data)
			}
			if data == nil {
				data = []byte(msg.Data)
			}
			out <- data
		}
	}()

	return out, nil
}

// Close shuts down the IPFS client and embedded node
func (c *Client) Close() error {
	if c.embedded != nil {
		return c.embedded.Close()
	}
	return nil
}

// PeerID returns the peer ID of the embedded node or local API node
func (c *Client) PeerID(ctx context.Context) string {
	if c.embedded != nil {
		return c.embedded.PeerID()
	}

	reqURL := fmt.Sprintf("%s/api/v0/id", c.apiURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return ""
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var payload struct {
		ID string `json:"ID"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
		return payload.ID
	}
	return ""
}
