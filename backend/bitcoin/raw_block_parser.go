package bitcoin

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
	network       string
}

// NewRawBlockClient creates a new raw block client
func NewRawBlockClient(network string) *RawBlockClient {
	if network == "" {
		network = "mainnet"
	}
	return &RawBlockClient{
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // Longer timeout for large blocks
		},
		rateLimiter: NewRateLimiter(30, time.Hour, 5*time.Second), // Ultra conservative: 30 requests/hour, min 5s between requests
		connected:   false,
		network:     network,
	}
}

// GetRawBlockHex downloads raw block data as hex from multiple sources
func (rbc *RawBlockClient) GetRawBlockHex(blockHeight int64) (string, error) {
	rbc.totalRequests++

	// Apply rate limiting
	if !rbc.rateLimiter.AllowRequest() {
		return "", fmt.Errorf("rate limit exceeded")
	}

	// Try multiple APIs for raw block hex
	apis := rbc.getRawBlockAPIs(blockHeight)

	for _, apiURL := range apis {
		log.Printf("Trying to download raw block %d from %s", blockHeight, apiURL)

		resp, err := rbc.httpClient.Get(apiURL)
		if err != nil {
			log.Printf("Failed to fetch from %s: %v", apiURL, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Printf("Failed to read response from %s: %v", apiURL, err)
				continue
			}

			var hexData string
			if strings.Contains(apiURL, "blockstream.info") || strings.Contains(apiURL, "mempool.space") {
				// Blockstream returns binary data, convert to hex
				hexData = hex.EncodeToString(body)
			} else {
				// blockchain.info returns hex string
				hexData = strings.TrimSpace(string(body))
			}

			if len(hexData) > 0 {
				log.Printf("Successfully downloaded raw block %d (%d hex chars)", blockHeight, len(hexData))
				return hexData, nil
			}
		} else {
			log.Printf("API %s returned status %d", apiURL, resp.StatusCode)
		}
	}

	return "", fmt.Errorf("failed to fetch raw block from all APIs")
}

// getRawBlockAPIs returns a list of APIs to try for raw block data
func (rbc *RawBlockClient) getRawBlockAPIs(blockHeight int64) []string {
	var apis []string

	// Primary: Blockstream/Mempool API (supports mainnet, testnet, testnet4, signet)
	var blockstreamBase string
	switch rbc.network {
	case "testnet4":
		blockstreamBase = "https://mempool.space/testnet4/api"
	case "testnet":
		blockstreamBase = "https://blockstream.info/testnet/api"
	case "signet":
		blockstreamBase = "https://mempool.space/signet/api"
	default:
		blockstreamBase = "https://blockstream.info/api"
	}

	// For blockstream, we need to get hash first, then raw
	if hash, err := rbc.getBlockHashFromBlockstream(blockstreamBase, blockHeight); err == nil {
		apis = append(apis, fmt.Sprintf("%s/block/%s/raw", blockstreamBase, hash))
	}

	// Fallback: blockchain.info (only for mainnet/testnet)
	if rbc.network == "mainnet" || rbc.network == "testnet" {
		var blockchainBase string
		if rbc.network == "testnet" {
			blockchainBase = "https://testnet.blockchain.info"
		} else {
			blockchainBase = "https://blockchain.info"
		}
		apis = append(apis, fmt.Sprintf("%s/rawblock/%d?format=hex", blockchainBase, blockHeight))
	}

	return apis
}

// getBlockHashFromBlockstream gets block hash from blockstream API
func (rbc *RawBlockClient) getBlockHashFromBlockstream(baseURL string, height int64) (string, error) {
	url := fmt.Sprintf("%s/block-height/%d", baseURL, height)

	resp, err := rbc.httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
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
	Version        int32
	Inputs         []TxInput
	Outputs        []TxOutput
	Witness        [][]byte   // flat list of all witness stack items (legacy)
	InputWitnesses [][][]byte // grouped by input: input -> stack items
	Locktime       uint32
	HasWitness     bool
	TxID           string
}

// TxInput represents a transaction input
type TxInput struct {
	PreviousTxID  string
	PreviousIndex uint32
	ScriptSig     []byte
	Sequence      uint32
}

// computeTxID serializes the transaction without witness data and hashes it (double SHA256).
func computeTxID(tx *Transaction) string {
	var buf bytes.Buffer

	// Version
	versionBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(versionBytes, uint32(tx.Version))
	buf.Write(versionBytes)

	// Input count
	buf.Write(encodeVarInt(len(tx.Inputs)))
	for _, in := range tx.Inputs {
		// prev tx hash (little endian)
		prev, _ := hex.DecodeString(in.PreviousTxID)
		buf.Write(reverseBytes(prev))
		// prev index
		idx := make([]byte, 4)
		binary.LittleEndian.PutUint32(idx, in.PreviousIndex)
		buf.Write(idx)
		// scriptSig
		buf.Write(encodeVarInt(len(in.ScriptSig)))
		buf.Write(in.ScriptSig)
		// sequence
		seq := make([]byte, 4)
		binary.LittleEndian.PutUint32(seq, in.Sequence)
		buf.Write(seq)
	}

	// Output count
	buf.Write(encodeVarInt(len(tx.Outputs)))
	for _, out := range tx.Outputs {
		val := make([]byte, 8)
		binary.LittleEndian.PutUint64(val, uint64(out.Value))
		buf.Write(val)
		buf.Write(encodeVarInt(len(out.ScriptPubKey)))
		buf.Write(out.ScriptPubKey)
	}

	// Locktime
	lock := make([]byte, 4)
	binary.LittleEndian.PutUint32(lock, tx.Locktime)
	buf.Write(lock)

	hash := doubleSHA256(buf.Bytes())
	return hex.EncodeToString(reverseBytes(hash))
}

// encodeVarInt writes a Bitcoin-style varint.
func encodeVarInt(n int) []byte {
	switch {
	case n < 0xfd:
		return []byte{byte(n)}
	case n <= 0xffff:
		b := []byte{0xfd, 0, 0}
		binary.LittleEndian.PutUint16(b[1:], uint16(n))
		return b
	case n <= 0xffffffff:
		b := []byte{0xfe, 0, 0, 0, 0}
		binary.LittleEndian.PutUint32(b[1:], uint32(n))
		return b
	default:
		b := []byte{0xff, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.LittleEndian.PutUint64(b[1:], uint64(n))
		return b
	}
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

// ExtractedImageData represents an image extracted from witness data
type ExtractedImageData struct {
	TxID        string `json:"tx_id"`
	Format      string `json:"format"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int    `json:"size_bytes"`
	FileName    string `json:"file_name"`
	FilePath    string `json:"file_path"`
	Data        []byte `json:"-"` // Actual image data (not serialized to JSON)
}

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

			var stackItems [][]byte
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
				stackItems = append(stackItems, witnessBytes)
			}
			tx.InputWitnesses = append(tx.InputWitnesses, stackItems)
		}
	}

	// Read locktime
	locktimeBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, locktimeBytes); err != nil {
		return nil, fmt.Errorf("failed to read locktime: %w", err)
	}
	tx.Locktime = binary.LittleEndian.Uint32(locktimeBytes)

	// Calculate transaction ID (non-witness serialization per consensus).
	tx.TxID = computeTxID(tx)

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
	seen := make(map[string]bool) // content hash dedupe

	log.Printf("extractImages: Processing %d transactions", len(transactions))
	for txIndex, tx := range transactions {
		log.Printf("extractImages: Transaction %d has %d inputs with witness", txIndex, len(tx.InputWitnesses))
		for inIdx, stack := range tx.InputWitnesses {
			for itemIdx, witness := range stack {
				log.Printf("extractImages: Input %d witness %d has %d bytes", inIdx, itemIdx, len(witness))

				if ordinalPayloads := extractOrdinalPayloads(witness); len(ordinalPayloads) > 0 {
					for ordIdx, payload := range ordinalPayloads {
						format := formatFromContentType(payload.contentType)
						image := ExtractedImageData{
							TxID:        tx.TxID,
							Format:      format,
							ContentType: payload.contentType,
							SizeBytes:   len(payload.payload),
							FileName:    fmt.Sprintf("%s_in%d_w%d_i%d.%s", tx.TxID[:16], inIdx, itemIdx, ordIdx, format),
							FilePath:    fmt.Sprintf("images/%s", fmt.Sprintf("%s_in%d_w%d_i%d.%s", tx.TxID[:16], inIdx, itemIdx, ordIdx, format)),
							Data:        payload.payload,
						}
						if dedupImage(&image, seen) {
							images = append(images, image)
						}
					}
					continue
				}

				// Fallback to image detection for non-Ordinals payloads.
				if imageType, imageData := detectImage(witness); imageType != "" {
					image := ExtractedImageData{
						TxID:      tx.TxID,
						Format:    imageType,
						SizeBytes: len(imageData),
						FileName:  fmt.Sprintf("%s_img_%d.%s", tx.TxID[:16], itemIdx, imageType),
						FilePath:  fmt.Sprintf("images/%s", fmt.Sprintf("%s_img_%d.%s", tx.TxID[:16], itemIdx, imageType)),
						Data:      imageData,
					}
					if dedupImage(&image, seen) {
						images = append(images, image)
					}
				}
			}
		}

		// Legacy flat witness handling (fallback).
		for i, witness := range tx.Witness {
			// First try to detect Ordinals inscriptions (text, BRC-20, etc.)
			contentType, content, ok := parseOrdinals(witness)
			if ok {
				log.Printf("extractImages: Found Ordinals inscription: %s", contentType)
				format := formatFromContentType(contentType)

				image := ExtractedImageData{
					TxID:        tx.TxID,
					Format:      format,
					ContentType: contentType,
					SizeBytes:   len(content),
					FileName:    fmt.Sprintf("%s_inscription_%d.%s", tx.TxID[:16], i, format),
					FilePath:    fmt.Sprintf("images/%s", fmt.Sprintf("%s_inscription_%d.%s", tx.TxID[:16], i, format)),
					Data:        content,
				}
				if dedupImage(&image, seen) {
					images = append(images, image)
				}
				log.Printf("Found Ordinals inscription (%s) in tx %s: %d bytes", contentType, tx.TxID[:16], len(content))
				continue
			}

			// Fallback to image detection for non-Ordinals
			imageType, imageData := detectImage(witness)
			if imageType != "" {
				image := ExtractedImageData{
					TxID:      tx.TxID,
					Format:    imageType,
					SizeBytes: len(imageData),
					FileName:  fmt.Sprintf("%s_img_%d.%s", tx.TxID[:16], i, imageType),
					FilePath:  fmt.Sprintf("images/%s", fmt.Sprintf("%s_img_%d.%s", tx.TxID[:16], i, imageType)),
					Data:      imageData,
				}
				if dedupImage(&image, seen) {
					images = append(images, image)
				}
				log.Printf("Found %s image in tx %s: %d bytes", imageType, tx.TxID[:16], len(imageData))
			}
		}

		// Inspect scriptSig for legacy inscriptions.
		for inIdx, input := range tx.Inputs {
			if len(input.ScriptSig) == 0 {
				continue
			}
			if contentType, content, ok := parseOrdinals(input.ScriptSig); ok {
				format := formatFromContentType(contentType)
				image := ExtractedImageData{
					TxID:        tx.TxID,
					Format:      format,
					ContentType: contentType,
					SizeBytes:   len(content),
					FileName:    fmt.Sprintf("%s_input_%d.%s", tx.TxID[:16], inIdx, format),
					FilePath:    fmt.Sprintf("images/%s", fmt.Sprintf("%s_input_%d.%s", tx.TxID[:16], inIdx, format)),
					Data:        content,
				}
				images = append(images, image)
				log.Printf("Found Ordinals inscription in scriptSig of tx %s input %d", tx.TxID[:16], inIdx)
			}
		}

		// Also inspect OP_RETURN outputs for embedded data (e.g., starlight/stargate ingest)
		for outIdx, output := range tx.Outputs {
			if len(output.ScriptPubKey) == 0 || output.ScriptPubKey[0] != 0x6a { // OP_RETURN
				continue
			}

			payloads := extractOpReturnPayloads(output.ScriptPubKey)
			for pIdx, payload := range payloads {
				if len(payload) == 0 {
					continue
				}

				// Attempt Ordinals-style payload parse first
				if contentType, content, ok := parseOrdinals(payload); ok {
					format := formatFromContentType(contentType)

					image := ExtractedImageData{
						TxID:        tx.TxID,
						Format:      format,
						ContentType: contentType,
						SizeBytes:   len(content),
						FileName:    fmt.Sprintf("%s_opret_%d_%d.%s", tx.TxID[:16], outIdx, pIdx, format),
						FilePath:    fmt.Sprintf("images/%s", fmt.Sprintf("%s_opret_%d_%d.%s", tx.TxID[:16], outIdx, pIdx, format)),
						Data:        content,
					}
					images = append(images, image)
					log.Printf("Found OP_RETURN Ordinals payload (%s) in tx %s output %d", contentType, tx.TxID[:16], outIdx)
					continue
				}

				// Fallback to raw image signature detection
				if imageType, imageData := detectImage(payload); imageType != "" {
					image := ExtractedImageData{
						TxID:      tx.TxID,
						Format:    imageType,
						SizeBytes: len(imageData),
						FileName:  fmt.Sprintf("%s_opret_%d_%d.%s", tx.TxID[:16], outIdx, pIdx, imageType),
						FilePath:  fmt.Sprintf("images/%s", fmt.Sprintf("%s_opret_%d_%d.%s", tx.TxID[:16], outIdx, pIdx, imageType)),
						Data:      imageData,
					}
					images = append(images, image)
					log.Printf("Found OP_RETURN %s image in tx %s output %d", imageType, tx.TxID[:16], outIdx)
				}
			}
		}
	}

	return images
}

func extractOpReturnPayloads(script []byte) [][]byte {
	if len(script) == 0 || script[0] != 0x6a { // OP_RETURN
		return nil
	}

	var payloads [][]byte
	pos := 1
	for pos < len(script) {
		opcode := script[pos]
		pos++

		var dataLen int
		switch {
		case opcode <= 75:
			dataLen = int(opcode)
		case opcode == 0x4c: // OP_PUSHDATA1
			if pos >= len(script) {
				return payloads
			}
			dataLen = int(script[pos])
			pos++
		case opcode == 0x4d: // OP_PUSHDATA2
			if pos+1 >= len(script) {
				return payloads
			}
			dataLen = int(script[pos]) | int(script[pos+1])<<8
			pos += 2
		case opcode == 0x4e: // OP_PUSHDATA4
			if pos+3 >= len(script) {
				return payloads
			}
			dataLen = int(script[pos]) | int(script[pos+1])<<8 | int(script[pos+2])<<16 | int(script[pos+3])<<24
			pos += 4
		default:
			continue
		}

		if dataLen < 0 || pos+dataLen > len(script) {
			return payloads
		}

		payload := script[pos : pos+dataLen]
		payloads = append(payloads, payload)
		pos += dataLen
	}

	return payloads
}

func parseOrdinals(data []byte) (string, []byte, bool) {
	payloads := extractOrdinalPayloads(data)
	if len(payloads) == 0 {
		return "", nil, false
	}
	return payloads[0].contentType, payloads[0].payload, true
}

func formatFromContentType(contentType string) string {
	format := "txt"
	switch {
	case strings.Contains(contentType, "jpeg") || strings.Contains(contentType, "jpg"):
		format = "jpeg"
	case strings.Contains(contentType, "png"):
		format = "png"
	case strings.Contains(contentType, "gif"):
		format = "gif"
	case strings.Contains(contentType, "webp"):
		format = "webp"
	case strings.Contains(contentType, "avif"):
		format = "avif"
	case strings.Contains(contentType, "svg"):
		format = "svg"
	case strings.Contains(contentType, "cbrc-20"):
		format = "brc20"
	}
	return format
}

// isLikelyMIME performs a small sanity check to avoid mistaking URLs for MIME types.
func isLikelyMIME(s string) bool {
	if strings.Contains(s, "://") || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	parts := strings.Split(s, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

// selectPayloadChunk concatenates all payload chunks in order.
// Many inscriptions split bodies across multiple pushdatas; always join them.
func selectPayloadChunk(chunks [][]byte) []byte {
	return bytes.Join(chunks, []byte{})
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

	// AVIF: ISO BMFF with ftyp brand avif/avis
	if len(data) >= 12 &&
		data[4] == 'f' && data[5] == 't' && data[6] == 'y' && data[7] == 'p' {
		brand := string(data[8:12])
		if brand == "avif" || brand == "avis" {
			return "avif", data
		}
	}

	return "", nil
}

// trimToImageSignatureLocal mirrors the API-level helper to avoid dependency cycles.
func trimToImageSignatureLocal(b []byte) []byte {
	if len(b) < 8 {
		return b
	}
	sigs := [][]byte{
		{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, // PNG
		{0xFF, 0xD8, 0xFF},       // JPEG
		[]byte("RIFF"),           // WEBP (check WEBP marker)
		{0x47, 0x49, 0x46, 0x38}, // GIF
		[]byte("ftypavif"),       // AVIF
	}
	for _, sig := range sigs {
		if idx := bytes.Index(b, sig); idx >= 0 {
			if sig[0] == 'R' && len(b) >= idx+12 {
				if !(b[idx+8] == 'W' && b[idx+9] == 'E' && b[idx+10] == 'B' && b[idx+11] == 'P') {
					continue
				}
			}
			if string(sig) == "ftypavif" {
				if idx >= 4 {
					return b[idx-4:]
				}
			}
			return b[idx:]
		}
	}
	return b
}

// dedupImage returns true if the image is new; false if we've already seen the same payload.
func dedupImage(img *ExtractedImageData, seen map[string]bool) bool {
	if len(img.Data) == 0 {
		return false
	}

	// If the payload looks like an image but has leading metadata, trim to the first valid signature.
	if strings.HasPrefix(img.ContentType, "image/") || strings.HasPrefix(img.Format, "png") || strings.HasPrefix(img.Format, "webp") || strings.HasPrefix(img.Format, "jpeg") {
		if trimmed := trimToImageSignatureLocal(img.Data); len(trimmed) > 0 {
			img.Data = trimmed
			img.SizeBytes = len(trimmed)
		}
	}

	hash := sha256.Sum256(img.Data)
	key := fmt.Sprintf("%s|%s|%x", img.TxID, img.ContentType, hash)
	if seen[key] {
		return false
	}
	seen[key] = true
	return true
}

// scriptOp represents a parsed script operation.
type scriptOp struct {
	opcode byte
	data   []byte
	isPush bool
}

// ordinalPayload represents a parsed Ordinals inscription inside a script.
type ordinalPayload struct {
	contentType string
	payload     []byte
}

// extractOrdinalPayloads walks a script and returns all Ordinal envelopes it finds.
func extractOrdinalPayloads(data []byte) []ordinalPayload {
	ops := parseScriptOps(data)
	var results []ordinalPayload

	for i := 0; i < len(ops); i++ {
		op := ops[i]
		if !op.isPush || !bytes.Equal(op.data, []byte("ord")) {
			continue
		}

		var contentType string
		var chunks [][]byte
		started := false

		for j := i + 1; j < len(ops); j++ {
			next := ops[j]
			if !next.isPush {
				// Stop once we hit control opcodes after payload starts (e.g., OP_ENDIF).
				if started {
					i = j
					break
				}
				continue
			}

			if contentType == "" && isLikelyMIME(string(next.data)) {
				contentType = string(next.data)
				continue
			}

			if contentType != "" {
				if len(next.data) == 0 && !started {
					continue
				}
				chunks = append(chunks, next.data)
				if len(next.data) > 0 {
					started = true
				}
			}
		}

		if contentType != "" && len(chunks) > 0 {
			payload := selectPayloadChunk(chunks)
			results = append(results, ordinalPayload{
				contentType: contentType,
				payload:     payload,
			})
		}
	}

	return results
}

// parseScriptOps walks a script/witness blob and returns ordered operations,
// preserving both push data and control opcodes so we can reconstruct Ord envelopes.
func parseScriptOps(script []byte) []scriptOp {
	var ops []scriptOp
	i := 0
	for i < len(script) {
		op := script[i]
		i++

		switch {
		case op <= 75:
			// Raw push of N bytes
			dataLen := int(op)
			if dataLen < 0 || i+dataLen > len(script) {
				return ops
			}
			ops = append(ops, scriptOp{opcode: op, data: script[i : i+dataLen], isPush: true})
			i += dataLen
		case op == 0x4c: // OP_PUSHDATA1
			if i >= len(script) {
				return ops
			}
			dataLen := int(script[i])
			i++
			if dataLen < 0 || i+dataLen > len(script) {
				return ops
			}
			ops = append(ops, scriptOp{opcode: op, data: script[i : i+dataLen], isPush: true})
			i += dataLen
		case op == 0x4d: // OP_PUSHDATA2
			if i+1 >= len(script) {
				return ops
			}
			dataLen := int(script[i]) | int(script[i+1])<<8
			i += 2
			if dataLen < 0 || i+dataLen > len(script) {
				return ops
			}
			ops = append(ops, scriptOp{opcode: op, data: script[i : i+dataLen], isPush: true})
			i += dataLen
		case op == 0x4e: // OP_PUSHDATA4
			if i+3 >= len(script) {
				return ops
			}
			dataLen := int(script[i]) | int(script[i+1])<<8 | int(script[i+2])<<16 | int(script[i+3])<<24
			i += 4
			if dataLen < 0 || i+dataLen > len(script) {
				return ops
			}
			ops = append(ops, scriptOp{opcode: op, data: script[i : i+dataLen], isPush: true})
			i += dataLen
		default:
			// Non-push opcode (OP_IF/ENDIF/OP_1 etc)
			ops = append(ops, scriptOp{opcode: op, isPush: false})
		}
	}
	return ops
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
