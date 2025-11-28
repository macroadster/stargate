package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// RawBlockClient handles efficient raw block downloading and parsing
type RawBlockClient struct {
	httpClient    *http.Client
	rateLimiter   *RateLimiter
	connected     bool
	totalRequests int64
}

// NewRawBlockClient creates a new raw block client
func NewRawBlockClient() *RawBlockClient {
	return &RawBlockClient{
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // Longer timeout for large blocks
		},
		rateLimiter: NewRateLimiter(1000, time.Hour, 1*time.Second), // Conservative rate limiting
		connected:   false,
	}
}

// GetRawBlockHex downloads raw block data as hex from blockchain.info
func (rbc *RawBlockClient) GetRawBlockHex(blockHeight int64) (string, error) {
	rbc.totalRequests++

	// Apply rate limiting
	if !rbc.rateLimiter.AllowRequest() {
		return "", fmt.Errorf("rate limit exceeded")
	}

	// Use blockchain.info API for raw block hex
	url := fmt.Sprintf("https://blockchain.info/rawblock/%d?format=hex", blockHeight)

	log.Printf("Downloading raw block %d as hex from %s", blockHeight, url)

	resp, err := rbc.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch block: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return strings.TrimSpace(string(body)), nil
}

// ParseBlock parses raw Bitcoin block data and extracts images
func (rbc *RawBlockClient) ParseBlock(hexData string) (*ParsedBlock, error) {
	// Convert hex to bytes
	blockBytes, err := hex.DecodeString(hexData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode hex: %w", err)
	}

	parser := &BitcoinParser{}
	return parser.ParseBlock(blockBytes)
}

// BitcoinParser implements correct Bitcoin protocol parsing
type BitcoinParser struct{}

// Block represents a Bitcoin block
type Block struct {
	Header       BlockHeader
	Transactions []Transaction
}

// BlockHeader represents Bitcoin block header
type BlockHeader struct {
	Version    int32
	PrevBlock  string
	MerkleRoot string
	Timestamp  uint32
	Bits       uint32
	Nonce      uint32
	Hash       string
}

// Transaction represents a Bitcoin transaction
type Transaction struct {
	Version    int32
	Inputs     []TxInput
	Outputs    []TxOutput
	Witness    [][]byte
	Locktime   uint32
	HasWitness bool
	TxID       string
}

// TxInput represents a transaction input
type TxInput struct {
	PreviousTxID  string
	PreviousIndex uint32
	ScriptSig     []byte
	Sequence      uint32
}

// TxOutput represents a transaction output
type TxOutput struct {
	Value        int64
	ScriptPubKey []byte
}

// ParsedBlock represents parsed block with extracted images
type ParsedBlock struct {
	Height       int64
	Hash         string
	Header       BlockHeader
	Transactions []Transaction
	Images       []ExtractedImageData
}

// ExtractedImageData moved to block_monitor.go to avoid conflicts

func (p *BitcoinParser) ParseBlock(data []byte) (*ParsedBlock, error) {
	if len(data) < 80 {
		return nil, fmt.Errorf("block data too short")
	}

	block := &ParsedBlock{}
	reader := bytes.NewReader(data)

	// Parse block header
	header := make([]byte, 80)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, fmt.Errorf("failed to read block header: %w", err)
	}

	block.Header.Version = int32(binary.LittleEndian.Uint32(header[0:4]))
	block.Header.PrevBlock = hex.EncodeToString(reverseBytes(header[4:36]))
	block.Header.MerkleRoot = hex.EncodeToString(reverseBytes(header[36:68]))
	block.Header.Timestamp = binary.LittleEndian.Uint32(header[68:72])
	block.Header.Bits = binary.LittleEndian.Uint32(header[72:76])
	block.Header.Nonce = binary.LittleEndian.Uint32(header[76:80])

	// Calculate block hash
	hash := doubleSHA256(header)
	block.Header.Hash = hex.EncodeToString(reverseBytes(hash))
	block.Hash = block.Header.Hash

	// Read transaction count
	txCount, err := readVarInt(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read transaction count: %w", err)
	}

	log.Printf("Block contains %d transactions", txCount)

	// Parse transactions
	for i := 0; i < int(txCount); i++ {
		tx, err := p.parseTransaction(reader)
		if err != nil {
			log.Printf("Failed to parse transaction %d: %v", i, err)
			continue
		}
		block.Transactions = append(block.Transactions, *tx)
	}

	// Extract images from witness data
	block.Images = p.extractImages(block.Transactions)

	return block, nil
}

func (p *BitcoinParser) parseTransaction(reader *bytes.Reader) (*Transaction, error) {
	startPos, _ := reader.Seek(0, io.SeekCurrent)

	tx := &Transaction{}

	// Read version (4 bytes)
	versionBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, versionBytes); err != nil {
		return nil, fmt.Errorf("failed to read version: %w", err)
	}
	tx.Version = int32(binary.LittleEndian.Uint32(versionBytes))

	// Check for SegWit marker
	markerPos, _ := reader.Seek(0, io.SeekCurrent)
	markerBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, markerBytes); err != nil {
		return nil, fmt.Errorf("failed to read potential marker: %w", err)
	}

	hasWitness := false
	if markerBytes[0] == 0x00 && markerBytes[1] == 0x01 {
		hasWitness = true
		tx.HasWitness = true
	} else {
		// Not SegWit, seek back
		reader.Seek(markerPos, io.SeekStart)
	}

	// Read input count
	inputCount, err := readVarInt(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read input count: %w", err)
	}

	// Parse inputs
	for i := 0; i < int(inputCount); i++ {
		input, err := p.parseInput(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input %d: %w", i, err)
		}
		tx.Inputs = append(tx.Inputs, *input)
	}

	// Read output count
	outputCount, err := readVarInt(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read output count: %w", err)
	}

	// Parse outputs
	for i := 0; i < int(outputCount); i++ {
		output, err := p.parseOutput(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to parse output %d: %w", i, err)
		}
		tx.Outputs = append(tx.Outputs, *output)
	}

	// Parse witness data if SegWit
	if hasWitness {
		for i := 0; i < len(tx.Inputs); i++ {
			witnessCount, err := readVarInt(reader)
			if err != nil {
				return nil, fmt.Errorf("failed to read witness count: %w", err)
			}

			for j := 0; j < int(witnessCount); j++ {
				witnessData, err := readVarInt(reader)
				if err != nil {
					return nil, fmt.Errorf("failed to read witness data: %w", err)
				}

				witnessBytes := make([]byte, witnessData)
				if _, err := io.ReadFull(reader, witnessBytes); err != nil {
					return nil, fmt.Errorf("failed to read witness bytes: %w", err)
				}

				tx.Witness = append(tx.Witness, witnessBytes)
			}
		}
	}

	// Read locktime
	locktimeBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, locktimeBytes); err != nil {
		return nil, fmt.Errorf("failed to read locktime: %w", err)
	}
	tx.Locktime = binary.LittleEndian.Uint32(locktimeBytes)

	// Calculate transaction ID (simplified - would need full serialization)
	endPos, _ := reader.Seek(0, io.SeekCurrent)
	txData := make([]byte, endPos-startPos)
	reader.Seek(startPos, io.SeekStart)
	io.ReadFull(reader, txData)
	reader.Seek(endPos, io.SeekStart)

	txHash := doubleSHA256(txData)
	tx.TxID = hex.EncodeToString(reverseBytes(txHash))

	return tx, nil
}

func (p *BitcoinParser) parseInput(reader *bytes.Reader) (*TxInput, error) {
	input := &TxInput{}

	// Read previous tx hash
	prevTxHashBytes := make([]byte, 32)
	if _, err := io.ReadFull(reader, prevTxHashBytes); err != nil {
		return nil, fmt.Errorf("failed to read previous tx hash: %w", err)
	}
	input.PreviousTxID = hex.EncodeToString(reverseBytes(prevTxHashBytes))

	// Read previous output index
	indexBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, indexBytes); err != nil {
		return nil, fmt.Errorf("failed to read previous output index: %w", err)
	}
	input.PreviousIndex = binary.LittleEndian.Uint32(indexBytes)

	// Read script length
	scriptLen, err := readVarInt(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read script length: %w", err)
	}

	// Read script signature
	if scriptLen > 0 {
		input.ScriptSig = make([]byte, scriptLen)
		if _, err := io.ReadFull(reader, input.ScriptSig); err != nil {
			return nil, fmt.Errorf("failed to read script signature: %w", err)
		}
	}

	// Read sequence
	sequenceBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, sequenceBytes); err != nil {
		return nil, fmt.Errorf("failed to read sequence: %w", err)
	}
	input.Sequence = binary.LittleEndian.Uint32(sequenceBytes)

	return input, nil
}

func (p *BitcoinParser) parseOutput(reader *bytes.Reader) (*TxOutput, error) {
	output := &TxOutput{}

	// Read value
	valueBytes := make([]byte, 8)
	if _, err := io.ReadFull(reader, valueBytes); err != nil {
		return nil, fmt.Errorf("failed to read value: %w", err)
	}
	output.Value = int64(binary.LittleEndian.Uint64(valueBytes))

	// Read script length
	scriptLen, err := readVarInt(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read script length: %w", err)
	}

	// Read script public key
	if scriptLen > 0 {
		output.ScriptPubKey = make([]byte, scriptLen)
		if _, err := io.ReadFull(reader, output.ScriptPubKey); err != nil {
			return nil, fmt.Errorf("failed to read script public key: %w", err)
		}
	}

	return output, nil
}

func (p *BitcoinParser) extractImages(transactions []Transaction) []ExtractedImageData {
	var images []ExtractedImageData

	for _, tx := range transactions {
		for i, witness := range tx.Witness {
			imageType, imageData := detectImage(witness)
			if imageType != "" {
				// Image extraction using block_monitor structs
				image := ExtractedImageData{
					TxID:      tx.TxID,
					Format:    imageType,
					SizeBytes: len(imageData),
					FileName:  fmt.Sprintf("%s_img_%d.%s", tx.TxID[:16], i, imageType),
					FilePath:  fmt.Sprintf("images/%s", fmt.Sprintf("%s_img_%d.%s", tx.TxID[:16], i, imageType)),
				}
				images = append(images, image)
				log.Printf("Found %s image in tx %s: %d bytes", imageType, tx.TxID[:16], len(imageData))
			}
		}
	}

	return images
}

func detectImage(data []byte) (string, []byte) {
	if len(data) < 10 {
		return "", nil
	}

	// JPEG signature: FF D8 FF
	if len(data) >= 3 &&
		data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "jpeg", data
	}

	// PNG signature: 89 50 4E 47 0D 0A 1A 0A
	if len(data) >= 8 &&
		data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 &&
		data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A {
		return "png", data
	}

	// GIF signature: 47 49 46 38 39 61
	if len(data) >= 6 &&
		data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 &&
		data[3] == 0x38 && data[4] == 0x39 && data[5] == 0x61 {
		return "gif", data
	}

	// WebP signature: 52 49 46 46 ... 57 45 42 50
	if len(data) >= 12 &&
		data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 && // RIFF
		data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50 { // WEBP
		return "webp", data
	}

	return "", nil
}

func readVarInt(reader *bytes.Reader) (uint64, error) {
	firstByte, err := reader.ReadByte()
	if err != nil {
		return 0, err
	}

	if firstByte < 0xFD {
		return uint64(firstByte), nil
	}

	if firstByte == 0xFD {
		bytes := make([]byte, 2)
		if _, err := io.ReadFull(reader, bytes); err != nil {
			return 0, err
		}
		return uint64(binary.LittleEndian.Uint16(bytes)), nil
	}

	if firstByte == 0xFE {
		bytes := make([]byte, 4)
		if _, err := io.ReadFull(reader, bytes); err != nil {
			return 0, err
		}
		return uint64(binary.LittleEndian.Uint32(bytes)), nil
	}

	// 0xFF
	bytes := make([]byte, 8)
	if _, err := io.ReadFull(reader, bytes); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(bytes), nil
}

func reverseBytes(data []byte) []byte {
	reversed := make([]byte, len(data))
	for i, b := range data {
		reversed[len(data)-1-i] = b
	}
	return reversed
}

func doubleSHA256(data []byte) []byte {
	hash1 := sha256.Sum256(data)
	hash2 := sha256.Sum256(hash1[:])
	return hash2[:]
}
