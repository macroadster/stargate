package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// BitcoinNodeClient handles communication with Bitcoin blockchain APIs
type BitcoinNodeClient struct {
	nodeURL    string
	httpClient *http.Client
	connected  bool
}

// NewBitcoinNodeClient creates a new Bitcoin node client
func NewBitcoinNodeClient(nodeURL string) *BitcoinNodeClient {
	if nodeURL == "" {
		nodeURL = "https://blockstream.info/api"
	}

	return &BitcoinNodeClient{
		nodeURL: nodeURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		connected: true,
	}
}

// TransactionStatus represents the status field from Blockstream API
type TransactionStatus struct {
	Confirmed   bool   `json:"confirmed"`
	BlockHeight int    `json:"block_height"`
	BlockHash   string `json:"block_hash"`
	BlockTime   int64  `json:"block_time"`
}

// Transaction represents a Bitcoin transaction
type Transaction struct {
	TxID   string            `json:"txid"`
	Height int               `json:"-"` // Will be set from status.block_height
	Time   int64             `json:"time"`
	Status TransactionStatus `json:"status"`
	VOut   []VOut            `json:"vout"`
	VIn    []Vin             `json:"vin"`
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
	Prevout   Prevout  `json:"prevout"`
	ScriptSig string   `json:"scriptsig"`
	Witness   []string `json:"witness"`
}

// Prevout represents the previous output
type Prevout struct {
	ScriptPubKey string `json:"scriptpubkey"`
	Value        int    `json:"value"`
}

// GetTransaction retrieves transaction details from the Bitcoin node
func (btc *BitcoinNodeClient) GetTransaction(txID string) (*Transaction, error) {
	if !btc.connected {
		return nil, fmt.Errorf("bitcoin node not connected")
	}

	// Validate transaction ID format (64-character hex string)
	if len(txID) != 64 {
		return nil, fmt.Errorf("invalid transaction ID format")
	}

	// Try to validate hex format
	for _, c := range txID {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return nil, fmt.Errorf("invalid transaction ID format: must be hex")
		}
	}

	url := fmt.Sprintf("%s/tx/%s", btc.nodeURL, txID)

	resp, err := btc.httpClient.Get(url)
	if err != nil {
		btc.connected = false
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

	var tx Transaction
	if err := json.Unmarshal(body, &tx); err != nil {
		return nil, fmt.Errorf("failed to decode transaction: %w", err)
	}

	// Set the transaction ID and height from nested status
	tx.TxID = txID
	tx.Height = tx.Status.BlockHeight

	return &tx, nil
}

// ExtractImages extracts images from transaction inputs (witness data)
func (btc *BitcoinNodeClient) ExtractImages(txID string) ([]ImageInfo, error) {
	tx, err := btc.GetTransaction(txID)
	if err != nil {
		return nil, err
	}

	var images []ImageInfo

	log.Printf("Extracting images from transaction %s with %d inputs", txID, len(tx.VIn))

	// Look through transaction inputs for image data in witness
	for i, vin := range tx.VIn {
		log.Printf("Input %d has %d witness entries", i, len(vin.Witness))
		for j, witnessData := range vin.Witness {
			log.Printf("Witness %d length: %d", j, len(witnessData))

			// Look for image data patterns in witness data
			if len(witnessData) > 100 { // Only check substantial data
				// Convert to lowercase for case-insensitive matching
				lowerData := strings.ToLower(witnessData)

				log.Printf("Checking witness %d for image signatures...", j)

				// Look for common image signatures in hex (case insensitive)
				if strings.Contains(lowerData, "ffd8ffe0") || // JPEG
					strings.Contains(lowerData, "89504e47") || // PNG
					strings.Contains(lowerData, "47494638") || // GIF
					strings.Contains(lowerData, "52494646") { // RIFF (WEBP/AVI)

					log.Printf("Found image signature in witness %d", j)
					// Try to extract and parse the image data
					imageData, format, err := btc.extractImageFromWitness(witnessData)
					if err != nil {
						log.Printf("Failed to extract image from input %d, witness %d: %v", i, j, err)
						continue
					}

					if len(imageData) > 0 {
						log.Printf("Successfully extracted %s image of %d bytes", format, len(imageData))
						images = append(images, ImageInfo{
							Index:     i,
							SizeBytes: len(imageData),
							Format:    format,
							DataURL: fmt.Sprintf("data:image/%s;base64,%s", format,
								btc.encodeBase64(imageData)),
						})
					}
				}

				// Also check for embedded image markers (case insensitive)
				if strings.Contains(lowerData, "image") ||
					strings.Contains(lowerData, "png") ||
					strings.Contains(lowerData, "jpeg") ||
					strings.Contains(lowerData, "webp") ||
					strings.Contains(lowerData, "riff") {

					log.Printf("Found image marker in witness %d", j)
					// Try to extract image data from the witness
					imageData, format, err := btc.extractImageFromWitness(witnessData)
					if err != nil {
						log.Printf("Failed to extract image from input %d, witness %d (marker): %v", i, j, err)
						continue
					}

					if len(imageData) > 0 {
						log.Printf("Successfully extracted %s image (via marker) of %d bytes", format, len(imageData))
						images = append(images, ImageInfo{
							Index:     i,
							SizeBytes: len(imageData),
							Format:    format,
							DataURL: fmt.Sprintf("data:image/%s;base64,%s", format,
								btc.encodeBase64(imageData)),
						})
					}
				}
			}
		}
	}

	log.Printf("Found %d images total", len(images))
	return images, nil
}

// extractImageFromWitness extracts image data from witness hex string
func (btc *BitcoinNodeClient) extractImageFromWitness(witnessData string) ([]byte, string, error) {
	// Look for image signatures within the witness data
	data := strings.ToLower(witnessData)

	// WebP signature: RIFF....WEBP
	if riffIndex := strings.Index(data, "52494646"); riffIndex >= 0 {
		// Look for WEBP signature after RIFF
		webpIndex := strings.Index(data[riffIndex:], "57454250")
		if webpIndex >= 0 {
			// Extract from RIFF to end of data (WebP files can be variable length)
			startHex := witnessData[riffIndex:]
			imageData, err := btc.hexToBytes(startHex)
			if err != nil {
				return nil, "", err
			}

			// Validate it's a proper WebP by checking header
			if len(imageData) >= 12 {
				// RIFF signature (4 bytes) + file size (4 bytes) + WEBP signature (4 bytes)
				if string(imageData[0:4]) == "RIFF" && string(imageData[8:12]) == "WEBP" {
					return imageData, "webp", nil
				}
			}
		}
	}

	// JPEG signature
	if jpegIndex := strings.Index(data, "ffd8ffe0"); jpegIndex >= 0 {
		startHex := witnessData[jpegIndex:]
		imageData, err := btc.hexToBytes(startHex)
		if err != nil {
			return nil, "", err
		}

		// Basic JPEG validation
		if len(imageData) >= 4 && imageData[0] == 0xFF && imageData[1] == 0xD8 {
			return imageData, "jpg", nil
		}
	}

	// PNG signature
	if pngIndex := strings.Index(data, "89504e47"); pngIndex >= 0 {
		startHex := witnessData[pngIndex:]
		imageData, err := btc.hexToBytes(startHex)
		if err != nil {
			return nil, "", err
		}

		// Basic PNG validation
		if len(imageData) >= 8 && string(imageData[1:4]) == "PNG" {
			return imageData, "png", nil
		}
	}

	// GIF signature
	if gifIndex := strings.Index(data, "47494638"); gifIndex >= 0 {
		startHex := witnessData[gifIndex:]
		imageData, err := btc.hexToBytes(startHex)
		if err != nil {
			return nil, "", err
		}

		// Basic GIF validation
		if len(imageData) >= 6 && string(imageData[0:6]) == "GIF87a" || string(imageData[0:6]) == "GIF89a" {
			return imageData, "gif", nil
		}
	}

	return nil, "", fmt.Errorf("no valid image signature found in witness data")
}

// parseImageData attempts to parse hex data as an image
func (btc *BitcoinNodeClient) parseImageData(hexData string) ([]byte, string, error) {
	// Remove any non-hex characters
	cleanHex := strings.ReplaceAll(hexData, " ", "")
	cleanHex = strings.ReplaceAll(cleanHex, "\n", "")

	// Check if it starts with common image signatures
	signatures := map[string]string{
		"89504e47": "png",  // PNG signature
		"ffd8ffe":  "jpg",  // JPEG signature (multiple variants)
		"47494638": "gif",  // GIF signature
		"424d":     "bmp",  // BMP signature
		"52494646": "webp", // WebP signature
	}

	// Convert hex to bytes
	data, err := btc.hexToBytes(cleanHex)
	if err != nil {
		return nil, "", err
	}

	// Check image signatures
	if len(data) >= 8 {
		hexSig := fmt.Sprintf("%x", data[:8])
		for sig, format := range signatures {
			if strings.HasPrefix(hexSig, sig) {
				return data, format, nil
			}
		}
	}

	// If no signature matches, try to detect by other means
	if len(data) > 0 {
		// Default to PNG if we can't determine the format
		return data, "png", nil
	}

	return nil, "", fmt.Errorf("no valid image data found")
}

// hexToBytes converts hex string to bytes
func (btc *BitcoinNodeClient) hexToBytes(hexStr string) ([]byte, error) {
	// Ensure even length
	if len(hexStr)%2 != 0 {
		hexStr = "0" + hexStr
	}

	result := make([]byte, len(hexStr)/2)
	for i := 0; i < len(hexStr); i += 2 {
		b1, err := strconv.ParseUint(hexStr[i:i+1], 16, 4)
		if err != nil {
			return nil, err
		}
		b2, err := strconv.ParseUint(hexStr[i+1:i+2], 16, 4)
		if err != nil {
			return nil, err
		}
		result[i/2] = byte(b1<<4 | b2)
	}

	return result, nil
}

// encodeBase64 encodes bytes to base64 (simple implementation)
func (btc *BitcoinNodeClient) encodeBase64(data []byte) string {
	// This is a simplified base64 encoder
	// In production, use encoding/base64 package
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

	var result strings.Builder

	for i := 0; i < len(data); i += 3 {
		b := make([]byte, 3)
		copy(b, data[i:])

		// Convert 3 bytes to 4 base64 characters
		combined := uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])

		for j := 0; j < 4; j++ {
			var index int
			switch j {
			case 0:
				index = int((combined >> 18) & 0x3F)
			case 1:
				index = int((combined >> 12) & 0x3F)
			case 2:
				if i+1 < len(data) {
					index = int((combined >> 6) & 0x3F)
				} else {
					result.WriteByte('=')
					continue
				}
			case 3:
				if i+2 < len(data) {
					index = int(combined & 0x3F)
				} else {
					result.WriteByte('=')
					continue
				}
			}
			result.WriteByte(chars[index])
		}
	}

	return result.String()
}

// GetBlockHeight retrieves the current block height
func (btc *BitcoinNodeClient) GetBlockHeight() (int, error) {
	if !btc.connected {
		return 0, fmt.Errorf("bitcoin node not connected")
	}

	url := fmt.Sprintf("%s/blocks/tip/height", btc.nodeURL)

	resp, err := btc.httpClient.Get(url)
	if err != nil {
		btc.connected = false
		return 0, fmt.Errorf("failed to fetch block height: %w", err)
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
		return 0, fmt.Errorf("invalid block height format: %w", err)
	}

	return height, nil
}

// GetBlockHash gets the block hash for a given block height
func (btc *BitcoinNodeClient) GetBlockHash(height int) (string, error) {
	if !btc.connected {
		return "", fmt.Errorf("bitcoin node not connected")
	}

	url := fmt.Sprintf("%s/block-height/%d", btc.nodeURL, height)

	resp, err := btc.httpClient.Get(url)
	if err != nil {
		btc.connected = false
		return "", fmt.Errorf("failed to fetch block hash: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bitcoin node returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	hash := strings.TrimSpace(string(body))
	return hash, nil
}

// GetBlockHeightFromHash gets the block height for a given block hash
func (btc *BitcoinNodeClient) GetBlockHeightFromHash(hash string) (int, error) {
	if !btc.connected {
		return 0, fmt.Errorf("bitcoin node not connected")
	}

	url := fmt.Sprintf("%s/block/%s", btc.nodeURL, hash)

	resp, err := btc.httpClient.Get(url)
	if err != nil {
		btc.connected = false
		return 0, fmt.Errorf("failed to fetch block info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("bitcoin node returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	var blockInfo struct {
		Height int `json:"height"`
	}
	if err := json.Unmarshal(body, &blockInfo); err != nil {
		return 0, fmt.Errorf("failed to parse block info: %w", err)
	}

	return blockInfo.Height, nil
}

// GetBlockTransactions gets all transaction IDs in a block
func (btc *BitcoinNodeClient) GetBlockTransactions(blockHash string) ([]string, error) {
	if !btc.connected {
		return nil, fmt.Errorf("bitcoin node not connected")
	}

	url := fmt.Sprintf("%s/block/%s/txs", btc.nodeURL, blockHash)

	resp, err := btc.httpClient.Get(url)
	if err != nil {
		btc.connected = false
		return nil, fmt.Errorf("failed to fetch block transactions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bitcoin node returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse transaction objects and extract txids
	var txObjects []map[string]interface{}
	if err := json.Unmarshal(body, &txObjects); err != nil {
		return nil, fmt.Errorf("failed to parse transactions: %w", err)
	}

	var txIDs []string
	for _, tx := range txObjects {
		if txid, ok := tx["txid"].(string); ok {
			txIDs = append(txIDs, txid)
		}
	}

	return txIDs, nil
}

// IsConnected returns the connection status
func (btc *BitcoinNodeClient) IsConnected() bool {
	return btc.connected
}

// GetNodeURL returns the configured node URL
func (btc *BitcoinNodeClient) GetNodeURL() string {
	return btc.nodeURL
}

// TestConnection tests the connection to the Bitcoin node
func (btc *BitcoinNodeClient) TestConnection() bool {
	_, err := btc.GetBlockHeight()
	if err != nil {
		btc.connected = false
		return false
	}

	btc.connected = true
	return true
}

// GetTransactionInfo returns formatted transaction information
func (btc *BitcoinNodeClient) GetTransactionInfo(txID string, includeImages bool, imageFormat string) (*TransactionInfo, error) {
	tx, err := btc.GetTransaction(txID)
	if err != nil {
		return nil, err
	}

	info := &TransactionInfo{
		TransactionID: txID,
		BlockHeight:   tx.Height,
		Timestamp:     time.Unix(tx.Time, 0).UTC().Format(time.RFC3339),
		Status:        "confirmed",
		TotalImages:   0,
	}

	if includeImages {
		images, err := btc.ExtractImages(txID)
		if err != nil {
			return nil, err
		}

		if imageFormat == "info" {
			// Only include metadata, not the actual image data
			for _, img := range images {
				info.Images = append(info.Images, ImageInfo{
					Index:     img.Index,
					SizeBytes: img.SizeBytes,
					Format:    img.Format,
				})
			}
		} else {
			// Include full image data
			info.Images = images
		}

		info.TotalImages = len(images)
	}

	return info, nil
}
