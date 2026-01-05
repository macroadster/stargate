package bitcoin

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"stargate-backend/core"
)

// RateLimiter manages API request rate limiting
type RateLimiter struct {
	requests    int
	maxRequests int
	windowStart time.Time
	windowSize  time.Duration
	minInterval time.Duration
	lastRequest time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(maxRequests int, windowSize time.Duration, minInterval time.Duration) *RateLimiter {
	return &RateLimiter{
		maxRequests: maxRequests,
		windowSize:  windowSize,
		windowStart: time.Now(),
		minInterval: minInterval,
		lastRequest: time.Time{},
	}
}

// AllowRequest checks if a request is allowed
func (rl *RateLimiter) AllowRequest() bool {
	now := time.Now()

	// Check minimum interval between requests
	if !rl.lastRequest.IsZero() && now.Sub(rl.lastRequest) < rl.minInterval {
		return false
	}

	rl.requests++

	// Reset window if needed
	if now.Sub(rl.windowStart) >= rl.windowSize {
		rl.requests = 1
		rl.windowStart = now
	}

	if rl.requests <= rl.maxRequests {
		rl.lastRequest = now
		return true
	}

	return false
}

// VOut represents a transaction output
type VOut struct {
	Value        int    `json:"value"`
	ScriptPubKey string `json:"scriptpubkey"`
}

// Vin represents a transaction input
type Vin struct {
	TxID      string   `json:"txid"`
	VOut      int      `json:"vout"`
	ScriptSig string   `json:"scriptsig"`
	Sequence  int64    `json:"sequence"`
	Witness   []string `json:"witness"`
}

// BitcoinNodeClient interfaces with Bitcoin blockchain APIs
type BitcoinNodeClient struct {
	httpClient  *http.Client
	baseURL     string
	rateLimiter *RateLimiter
	network     string
	mu          sync.RWMutex
}

// NewBitcoinNodeClient creates a new Bitcoin node client
func NewBitcoinNodeClient(baseURL string) *BitcoinNodeClient {
	network := "mainnet"
	if strings.Contains(baseURL, "testnet4") {
		network = "testnet4"
	} else if strings.Contains(baseURL, "testnet") {
		network = "testnet"
	} else if strings.Contains(baseURL, "signet") {
		network = "signet"
	}
	return &BitcoinNodeClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL:     baseURL,
		rateLimiter: NewRateLimiter(100, time.Hour, 1*time.Second),
		network:     network,
	}
}

// GetCurrentHeight gets the current blockchain height with retry logic
func (btc *BitcoinNodeClient) GetCurrentHeight() (int64, error) {
	return btc.getCurrentHeightWithRetry(3)
}

// getCurrentHeightWithRetry attempts to get height with exponential backoff
func (btc *BitcoinNodeClient) getCurrentHeightWithRetry(maxRetries int) (int64, error) {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Wait for rate limit with exponential backoff
		if attempt > 0 {
			waitTime := time.Duration(attempt*attempt) * time.Second
			if waitTime > 10*time.Second {
				waitTime = 10 * time.Second
			}
			time.Sleep(waitTime)
		}

		// Check rate limit
		if !btc.rateLimiter.AllowRequest() {
			lastErr = fmt.Errorf("rate limit exceeded (attempt %d/%d)", attempt+1, maxRetries)
			continue
		}

		resp, err := btc.httpClient.Get(btc.baseURL + "/blocks/tip/height")
		if err != nil {
			lastErr = fmt.Errorf("failed to get current height: %w", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("bitcoin node returned status %d", resp.StatusCode)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = fmt.Errorf("failed to read response: %w", err)
			continue
		}

		height, err := strconv.ParseInt(strings.TrimSpace(string(body)), 10, 64)
		if err != nil {
			lastErr = fmt.Errorf("failed to parse height: %w", err)
			continue
		}

		return height, nil
	}

	return 0, lastErr
}

// waitForRateLimit waits for rate limit to reset with exponential backoff
func (btc *BitcoinNodeClient) waitForRateLimit(attempt int) error {
	if attempt == 0 {
		if !btc.rateLimiter.AllowRequest() {
			return fmt.Errorf("rate limit exceeded")
		}
		return nil
	}

	// Exponential backoff with jitter
	waitTime := time.Duration(attempt*attempt) * time.Second
	if waitTime > 30*time.Second {
		waitTime = 30 * time.Second
	}

	// Add some jitter to avoid thundering herd
	jitter := time.Duration(float64(waitTime) * 0.1 * (0.5 + 0.5*float64(time.Now().UnixNano()%1000)/1000))
	time.Sleep(waitTime + jitter)

	if !btc.rateLimiter.AllowRequest() {
		return fmt.Errorf("rate limit exceeded after backoff")
	}

	return nil
}

// GetBlockData gets comprehensive block data
func (btc *BitcoinNodeClient) GetBlockData(height int64) (any, error) {
	if !btc.rateLimiter.AllowRequest() {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	url := fmt.Sprintf("%s/block-height/%d", btc.baseURL, height)

	resp, err := btc.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch block: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bitcoin node returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var blockData any
	if err := json.Unmarshal(body, &blockData); err != nil {
		return nil, fmt.Errorf("failed to decode block data: %w", err)
	}

	return blockData, nil
}

// GetTransaction gets transaction data
func (btc *BitcoinNodeClient) GetTransaction(txID string) (any, error) {
	if !btc.rateLimiter.AllowRequest() {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	url := fmt.Sprintf("%s/tx/%s", btc.baseURL, txID)

	resp, err := btc.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch transaction: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("transaction not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bitcoin node returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var txData any
	if err := json.Unmarshal(body, &txData); err != nil {
		return nil, fmt.Errorf("failed to decode transaction data: %w", err)
	}

	return txData, nil
}

// GetTransactionInfo gets detailed transaction information
func (btc *BitcoinNodeClient) GetTransactionInfo(txID string, includeImages bool, imageFormat string) (*core.TransactionInfo, error) {
	if !btc.rateLimiter.AllowRequest() {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	url := fmt.Sprintf("%s/tx/%s", btc.baseURL, txID)

	resp, err := btc.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch transaction: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("transaction not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bitcoin node returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var txData map[string]any
	if err := json.Unmarshal(body, &txData); err != nil {
		return nil, fmt.Errorf("failed to decode transaction info: %w", err)
	}

	txInfo := &core.TransactionInfo{
		TransactionID: txID,
		Status:        "confirmed",
		TotalImages:   0,
	}

	// Extract block height
	if blockHeight, ok := txData["status"].(map[string]any)["block_height"].(float64); ok {
		txInfo.BlockHeight = int(blockHeight)
	}

	// Extract timestamp
	if blockTime, ok := txData["status"].(map[string]any)["block_time"].(float64); ok {
		txInfo.Timestamp = fmt.Sprintf("%d", int64(blockTime))
	}

	// Extract images if requested
	if includeImages {
		images, err := btc.ExtractImages(txID)
		if err == nil {
			txInfo.Images = images
			txInfo.TotalImages = len(images)
		}
	}

	return txInfo, nil
}

// TestConnection tests the connection to the Bitcoin node
func (btc *BitcoinNodeClient) TestConnection() bool {
	resp, err := btc.httpClient.Get(btc.baseURL + "/blocks/tip/height")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// GetNodeURL returns the base URL of the Bitcoin node
func (btc *BitcoinNodeClient) GetNodeURL() string {
	return btc.baseURL
}

// GetNetwork returns the network of the Bitcoin node
func (btc *BitcoinNodeClient) GetNetwork() string {
	return btc.network
}

// GetBlockHeight gets the current block height
func (btc *BitcoinNodeClient) GetBlockHeight() (int, error) {
	height, err := btc.GetCurrentHeight()
	if err != nil {
		return 0, err
	}
	return int(height), nil
}

// GetBlockHash gets the block hash for a given height
func (btc *BitcoinNodeClient) GetBlockHash(height int) (string, error) {
	if !btc.rateLimiter.AllowRequest() {
		return "", fmt.Errorf("rate limit exceeded")
	}

	url := fmt.Sprintf("%s/block-height/%d", btc.baseURL, height)
	resp, err := btc.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to get block hash: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bitcoin node returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return strings.TrimSpace(string(body)), nil
}

// GetBlockHeightFromHash gets the block height for a given hash
func (btc *BitcoinNodeClient) GetBlockHeightFromHash(blockHash string) (int, error) {
	if !btc.rateLimiter.AllowRequest() {
		return 0, fmt.Errorf("rate limit exceeded")
	}

	url := fmt.Sprintf("%s/block/%s/height", btc.baseURL, blockHash)
	resp, err := btc.httpClient.Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to get block height: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("bitcoin node returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	height, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		return 0, fmt.Errorf("failed to parse height: %w", err)
	}

	return height, nil
}

// GetBlockTransactions gets all transaction IDs in a block
func (btc *BitcoinNodeClient) GetBlockTransactions(blockHash string) ([]string, error) {
	if !btc.rateLimiter.AllowRequest() {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	url := fmt.Sprintf("%s/block/%s/txs", btc.baseURL, blockHash)
	resp, err := btc.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get block transactions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bitcoin node returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Blockstream API returns an array of transaction objects, not strings
	var transactionObjects []map[string]any
	if err := json.Unmarshal(body, &transactionObjects); err != nil {
		return nil, fmt.Errorf("failed to decode transactions: %w", err)
	}

	// Extract transaction IDs from the objects
	var transactions []string
	for _, txObj := range transactionObjects {
		if txid, ok := txObj["txid"].(string); ok {
			transactions = append(transactions, txid)
		}
	}

	return transactions, nil
}

// ExtractImages extracts images from a transaction's witness data
func (btc *BitcoinNodeClient) ExtractImages(txID string) ([]core.ImageInfo, error) {
	if !btc.rateLimiter.AllowRequest() {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	// Get transaction data
	url := fmt.Sprintf("%s/tx/%s", btc.baseURL, txID)
	resp, err := btc.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bitcoin node returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var txData map[string]any
	if err := json.Unmarshal(body, &txData); err != nil {
		return nil, fmt.Errorf("failed to decode transaction: %w", err)
	}

	var images []core.ImageInfo

	// Extract images from witness data
	if vins, ok := txData["vin"].([]any); ok {
		for _, vin := range vins {
			vinMap, ok := vin.(map[string]any)
			if !ok {
				continue
			}

			// Check for witness data
			if witness, ok := vinMap["witness"].([]any); ok && len(witness) > 0 {
				for _, witnessItem := range witness {
					if witnessStr, ok := witnessItem.(string); ok {
						// Try to detect image data in witness
						if imageData := btc.extractImageFromWitness(witnessStr); imageData != nil {
							images = append(images, core.ImageInfo{
								Index:     len(images),
								SizeBytes: len(imageData),
								Format:    btc.detectImageFormat(imageData),
								DataURL:   fmt.Sprintf("data:image/%s;base64,%s", btc.detectImageFormat(imageData), base64.StdEncoding.EncodeToString(imageData)),
							})
						}
					}
				}
			}
		}
	}

	return images, nil
}

// extractImageFromWitness attempts to extract image data from witness string
func (btc *BitcoinNodeClient) extractImageFromWitness(witnessStr string) []byte {
	// Try to decode hex data
	data, err := hex.DecodeString(witnessStr)
	if err != nil {
		return nil
	}

	// Look for common image signatures
	signatures := [][]byte{
		{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, // PNG
		{0xFF, 0xD8, 0xFF},       // JPEG
		{0x47, 0x49, 0x46, 0x38}, // GIF
		{0x42, 0x4D},             // BMP
		{0x52, 0x49, 0x46, 0x46}, // WEBP (RIFF)
	}

	for _, sig := range signatures {
		if bytes.HasPrefix(data, sig) {
			return data
		}
	}

	// Also check for embedded images within the data
	for i := 0; i < len(data)-100; i++ {
		for _, sig := range signatures {
			if bytes.HasPrefix(data[i:], sig) {
				// Find end of image data
				end := i + 1000 // Default size
				if i+end > len(data) {
					end = len(data) - i
				}

				// For PNG, look for IEND chunk
				if bytes.HasPrefix(data[i:], []byte{0x89, 0x50, 0x4E, 0x47}) {
					if iend := bytes.Index(data[i:], []byte("IEND\xae\x42\x60\x82")); iend > 0 {
						end = iend + 8
					}
				}

				return data[i : i+end]
			}
		}
	}

	return nil
}

// detectImageFormat detects the format of image data
func (btc *BitcoinNodeClient) detectImageFormat(data []byte) string {
	if len(data) < 8 {
		return "unknown"
	}

	switch {
	case bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}):
		return "png"
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return "jpg"
	case bytes.HasPrefix(data, []byte{0x47, 0x49, 0x46, 0x38}):
		return "gif"
	case bytes.HasPrefix(data, []byte{0x42, 0x4D}):
		return "bmp"
	case bytes.HasPrefix(data, []byte{0x52, 0x49, 0x46, 0x46}) && len(data) > 12 && bytes.HasPrefix(data[8:12], []byte{0x57, 0x45, 0x42, 0x50}):
		return "webp"
	default:
		return "unknown"
	}
}
