package ipfs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	apiURL string
	client *http.Client
}

func NewClientFromEnv() *Client {
	apiURL := os.Getenv("IPFS_API_URL")
	if apiURL == "" {
		apiURL = "http://127.0.0.1:5001"
	}
	timeout := 30 * time.Second
	if raw := os.Getenv("IPFS_HTTP_TIMEOUT_SEC"); raw != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && v > 0 {
			timeout = time.Duration(v) * time.Second
		}
	}
	return &Client{
		apiURL: strings.TrimRight(apiURL, "/"),
		client: &http.Client{Timeout: timeout},
	}
}

func (c *Client) AddBytes(ctx context.Context, name string, data []byte) (string, error) {
	return c.addStream(ctx, name, bytes.NewReader(data))
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
	if strings.TrimSpace(cid) == "" {
		return nil, fmt.Errorf("ipfs cat missing cid")
	}
	reqURL := fmt.Sprintf("%s/api/v0/cat?arg=%s", c.apiURL, url.QueryEscape(cid))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if len(body) == 0 {
			return nil, fmt.Errorf("ipfs cat failed: %s", resp.Status)
		}
		return nil, fmt.Errorf("ipfs cat failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) PubsubPublish(ctx context.Context, topic string, message []byte) error {
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
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if len(body) == 0 {
			return fmt.Errorf("ipfs pubsub publish failed: %s", resp.Status)
		}
		return fmt.Errorf("ipfs pubsub publish failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}
