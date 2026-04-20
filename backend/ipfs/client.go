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
	"time"
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
}

// IsEnabled returns true if IPFS is enabled globally
func IsEnabled() bool {
	// Check for global IPFS disable flag - only disable if explicitly set to "false"
	return strings.TrimSpace(os.Getenv("IPFS_ENABLED")) != "false"
}

func NewClientFromEnv() *Client {
	if !IsEnabled() {
		return nil
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
	return &Client{
		apiURL:     strings.TrimRight(apiURL, "/"),
		client:     &http.Client{Timeout: timeout},
		storageDir: storageDir,
	}
}

func (c *Client) AddBytes(ctx context.Context, name string, data []byte) (string, error) {
	if c == nil {
		return "", fmt.Errorf("IPFS client is disabled")
	}

	// Try local IPFS node first
	cid, err := c.addStream(ctx, name, bytes.NewReader(data))
	if err == nil {
		return cid, nil
	}

	// Fallback to local storage if node is missing/down
	log.Printf("IPFS node unavailable for add (%v), storing locally in %s", err, c.storageDir)

	// Simple SHA256-based CID simulation for local storage
	hash := sha256.Sum256(data)
	// We'll use a prefix that looks like a CID but is just our local hash
	localCID := fmt.Sprintf("local-%x", hash)

	target := filepath.Join(c.storageDir, localCID)
	if err := os.WriteFile(target, data, 0644); err != nil {
		return "", fmt.Errorf("failed to store IPFS object locally: %w", err)
	}

	return localCID, nil
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

func (c *Client) Cat(ctx context.Context, cid string) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("IPFS client is disabled")
	}
	if strings.TrimSpace(cid) == "" {
		return nil, fmt.Errorf("ipfs cat missing cid")
	}

	// 1. Try local filesystem storage (objects we "added" while node was down)
	localPath := filepath.Join(c.storageDir, cid)
	if data, err := os.ReadFile(localPath); err == nil {
		return data, nil
	}

	// 2. Try local IPFS node (if configured/reachable)
	reqURL := fmt.Sprintf("%s/api/v0/cat?arg=%s", c.apiURL, url.QueryEscape(cid))
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

	// 3. Try public IPFS gateways
	if !strings.HasPrefix(cid, "local-") {
		return c.catFromGateways(ctx, cid)
	}

	return nil, fmt.Errorf("ipfs cat failed: object %s not found locally or via gateways", cid)
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
