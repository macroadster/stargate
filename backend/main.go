package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"

	"stargate-backend/bitcoin"
)

type InscriptionRequest struct {
	ImageData string  `json:"imageData"`
	Text      string  `json:"text"`
	Price     float64 `json:"price"`
	Timestamp int64   `json:"timestamp"`
	ID        string  `json:"id"`
	Status    string  `json:"status"`
}

type SmartContractImage struct {
	ContractID   string                 `json:"contract_id"`
	BlockHeight  int64                  `json:"block_height"`
	StegoImage   string                 `json:"stego_image_url"`
	ContractType string                 `json:"contract_type"`
	Metadata     map[string]interface{} `json:"metadata"`
}

type ContractMetadata struct {
	Name        string `json:"name"`
	Symbol      string `json:"symbol"`
	Decimals    int    `json:"decimals,omitempty"`
	TotalSupply string `json:"total_supply,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

var (
	pendingInscriptions = []InscriptionRequest{}
	smartContracts      = []SmartContractImage{}
	mu                  sync.Mutex
	inscriptionsFile    = "inscriptions.json"
	contractsFile       = "smart_contracts.json"
)

var (
	bitcoinAPI   *bitcoin.BitcoinAPI
	blockMonitor *bitcoin.BlockMonitor
)

func enableCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func loadInscriptions() {
	file, err := os.Open(inscriptionsFile)
	if err != nil {
		if os.IsNotExist(err) {
			pendingInscriptions = []InscriptionRequest{}
			return
		}
		log.Printf("Error opening inscriptions file: %v", err)
		return
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&pendingInscriptions); err != nil {
		log.Printf("Error decoding inscriptions: %v", err)
		pendingInscriptions = []InscriptionRequest{}
	}
}

func saveInscriptions() {
	file, err := os.Create(inscriptionsFile)
	if err != nil {
		log.Printf("Error creating inscriptions file: %v", err)
		return
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(pendingInscriptions); err != nil {
		log.Printf("Error encoding inscriptions: %v", err)
	}
}

func loadSmartContracts() {
	file, err := os.Open(contractsFile)
	if err != nil {
		if os.IsNotExist(err) {
			smartContracts = []SmartContractImage{}
			return
		}
		log.Printf("Error opening smart contracts file: %v", err)
		return
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&smartContracts); err != nil {
		log.Printf("Error decoding smart contracts: %v", err)
		smartContracts = []SmartContractImage{}
	}
}

func saveSmartContracts() {
	file, err := os.Create(contractsFile)
	if err != nil {
		log.Printf("Error creating smart contracts file: %v", err)
		return
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(smartContracts); err != nil {
		log.Printf("Error encoding smart contracts: %v", err)
	}
}

func extractWitnessImagesFromBlock(blockHash string) ([]interface{}, error) {
	// Get transactions for this block
	txIDs, err := bitcoinAPI.GetBitcoinClient().GetBlockTransactions(blockHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get block transactions: %w", err)
	}

	var witnessImages []interface{}

	// Process each transaction to extract witness images
	for _, txID := range txIDs {
		images, err := bitcoinAPI.GetBitcoinClient().ExtractImages(txID)
		if err != nil {
			log.Printf("Failed to extract images from transaction %s: %v", txID, err)
			continue
		}

		// Convert images to response format
		for _, img := range images {
			witnessImage := map[string]interface{}{
				"tx_id":      txID,
				"index":      img.Index,
				"size_bytes": img.SizeBytes,
				"format":     img.Format,
				"data_url":   img.DataURL,
			}
			witnessImages = append(witnessImages, witnessImage)
		}
	}

	log.Printf("Extracted %d witness images from block %s", len(witnessImages), blockHash)
	return witnessImages, nil
}

func handleBlocks(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	blocksResp, err := http.Get("https://mempool.space/api/v1/blocks")
	if err != nil {
		http.Error(w, "Failed to fetch blocks", http.StatusInternalServerError)
		return
	}
	defer blocksResp.Body.Close()

	body, err := io.ReadAll(blocksResp.Body)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	// Add future block with smart contracts
	var blocks []interface{}
	if err := json.Unmarshal(body, &blocks); err == nil && len(blocks) > 0 {
		futureBlock := map[string]interface{}{
			"id":              "future",
			"height":          blocks[0].(map[string]interface{})["height"].(float64) + 1,
			"timestamp":       time.Now().Unix() + 600,
			"hash":            "pending...",
			"tx_count":        len(pendingInscriptions),
			"smart_contracts": getContractsForBlock(int64(blocks[0].(map[string]interface{})["height"].(float64) + 1)),
		}
		blocks = append([]interface{}{futureBlock}, blocks...)

		// Add smart contracts to existing blocks
		for i, block := range blocks[1:] {
			if blockMap, ok := block.(map[string]interface{}); ok {
				height := int64(blockMap["height"].(float64))
				blockMap["smart_contracts"] = getContractsForBlock(height)
				blocks[i+1] = blockMap
			}
		}

		body, _ = json.Marshal(blocks)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func handleBlocksWithContracts(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	resp, err := http.Get("https://mempool.space/api/v1/blocks")
	if err != nil {
		http.Error(w, "Failed to fetch blocks", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	var blocks []interface{}
	if err := json.Unmarshal(body, &blocks); err == nil {
		enhancedBlocks := make([]map[string]interface{}, len(blocks))
		for i, block := range blocks {
			blockMap := block.(map[string]interface{})
			height := int64(blockMap["height"].(float64))
			enhancedBlocks[i] = map[string]interface{}{
				"id":              blockMap["id"],
				"height":          blockMap["height"],
				"timestamp":       blockMap["timestamp"],
				"tx_count":        blockMap["tx_count"],
				"size":            blockMap["size"],
				"smart_contracts": getContractsForBlock(height),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"blocks": enhancedBlocks,
		})
		return
	}

	http.Error(w, "Failed to process blocks", http.StatusInternalServerError)
}

func getContractsForBlock(blockHeight int64) int {
	// Get block data to count inscriptions
	blockResponse, err := blockMonitor.GetBlockInscriptions(blockHeight)
	if err != nil {
		log.Printf("Failed to get inscriptions for block %d: %v", blockHeight, err)
		return 0
	}

	if !blockResponse.Success {
		log.Printf("Failed to get inscriptions for block %d: %s", blockHeight, blockResponse.Error)
		return 0
	}

	// Return the count of inscriptions as "smart_contracts"
	log.Printf("Block %d has %d inscriptions (counting as smart_contracts)", blockHeight, len(blockResponse.Inscriptions))
	return len(blockResponse.Inscriptions)
}

func handleInscriptions(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	resp, err := http.Get("https://api.hiro.so/ordinals/v1/inscriptions?order_by=number&order=desc&limit=20")
	if err != nil {
		http.Error(w, "Failed to fetch inscriptions", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func handleInscribe(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form
	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	text := r.FormValue("text")
	priceStr := r.FormValue("price")
	price, _ := strconv.ParseFloat(priceStr, 64)

	// Handle file upload
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Failed to get image file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Create uploads directory if it doesn't exist
	uploadsDir := "uploads"
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		http.Error(w, "Failed to create uploads directory", http.StatusInternalServerError)
		return
	}

	// Generate filename
	timestamp := time.Now().Unix()
	filename := fmt.Sprintf("%d_%s", timestamp, header.Filename)

	// Save file
	imagePath := filepath.Join(uploadsDir, filename)
	dst, err := os.Create(imagePath)
	if err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, "Failed to copy file", http.StatusInternalServerError)
		return
	}

	req := InscriptionRequest{
		ImageData: imagePath,
		Text:      text,
		Price:     price,
		Timestamp: timestamp,
		ID:        fmt.Sprintf("pending_%d", timestamp),
		Status:    "pending",
	}

	log.Printf("Created inscription: %s, image: %s", req.ID, imagePath)

	mu.Lock()
	pendingInscriptions = append(pendingInscriptions, req)
	saveInscriptions()
	mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "id": req.ID})
}

func handlePendingTransactions(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pendingInscriptions)
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" || strings.ToLower(query) == "block" || strings.ToLower(query) == "blocks" {
		// Return recent blocks
		resp, err := http.Get("https://mempool.space/api/v1/blocks")
		blocks := []interface{}{}
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			json.Unmarshal(body, &blocks)
			// Limit to 5 blocks
			if len(blocks) > 5 {
				blocks = blocks[:5]
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"inscriptions": pendingInscriptions,
			"blocks":       blocks,
		})
		return
	}

	// Search pending inscriptions
	var inscriptionResults []InscriptionRequest
	for _, insc := range pendingInscriptions {
		if strings.Contains(strings.ToLower(insc.Text), strings.ToLower(query)) ||
			strings.Contains(strings.ToLower(insc.ID), strings.ToLower(query)) {
			inscriptionResults = append(inscriptionResults, insc)
		}
	}

	// Search blocks
	resp, err := http.Get("https://mempool.space/api/v1/blocks")
	blocks := []interface{}{}
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		json.Unmarshal(body, &blocks)
	}

	var blockResults []interface{}
	for _, block := range blocks {
		blockMap := block.(map[string]interface{})
		heightStr := fmt.Sprintf("%v", blockMap["height"])
		hash := fmt.Sprintf("%v", blockMap["id"])
		if strings.Contains(heightStr, query) || strings.Contains(strings.ToLower(hash), strings.ToLower(query)) {
			blockResults = append(blockResults, block)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"inscriptions": inscriptionResults,
		"blocks":       blockResults,
	})
}

func handleInscriptionContent(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Extract ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/api/inscription/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "content" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	id := parts[0]

	resp, err := http.Get(fmt.Sprintf("https://api.hiro.so/ordinals/v1/inscriptions/%s/content", id))
	if err != nil {
		http.Error(w, "Failed to fetch inscription content", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	// Set content type from response
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Write(body)
}

func generateQRCode(address, amount string) ([]byte, error) {
	// Generate QR code
	qr, err := qrcode.New(address+"?amount="+amount, qrcode.Medium)
	if err != nil {
		return nil, err
	}

	// Convert to PNG
	buf := new(bytes.Buffer)
	err = png.Encode(buf, qr.Image(256))
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func handleQRCode(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	address := r.URL.Query().Get("address")
	amount := r.URL.Query().Get("amount")

	if address == "" {
		http.Error(w, "Address parameter required", http.StatusBadRequest)
		return
	}

	qrData, err := generateQRCode(address, amount)
	if err != nil {
		http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(qrData)
}

func proxyToStegoAPI(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Construct the URL to the real steganography API
	targetURL := "http://localhost:8080" + r.URL.Path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Create new request to steganography API
	req, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// Copy headers
	for name, values := range r.Header {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to proxy request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}

	// Copy response status and body
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleContractStego(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Extract contract ID from URL
	path := strings.TrimPrefix(r.URL.Path, "/api/contract-stego/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 {
		http.Error(w, "Invalid contract ID", http.StatusBadRequest)
		return
	}
	contractID := parts[0]

	// Find the contract
	var targetContract *SmartContractImage
	for _, contract := range smartContracts {
		if contract.ContractID == contractID {
			targetContract = &contract
			break
		}
	}

	if targetContract == nil {
		http.Error(w, "Contract not found", http.StatusNotFound)
		return
	}

	if len(parts) > 1 && parts[1] == "analyze" {
		// Proxy to steganography API for analysis
		analysisURL := fmt.Sprintf("http://localhost:8080/analyze/%s", contractID)
		resp, err := http.Get(analysisURL)
		if err != nil {
			http.Error(w, "Failed to analyze contract", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, "Failed to read analysis response", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
		return
	}

	// Return contract metadata
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(targetContract)
}

func handleCreateContractStego(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var contract SmartContractImage
	if err := json.NewDecoder(r.Body).Decode(&contract); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("Received contract: %+v", contract)

	// Generate stego image filename
	stegoFilename := fmt.Sprintf("stego_%s.png", contract.ContractID)
	stegoURL := fmt.Sprintf("/uploads/stego-images/%s", stegoFilename)
	contract.StegoImage = stegoURL

	log.Printf("Generated stego filename: %s", stegoFilename)
	log.Printf("Set stego URL to: %s", stegoURL)
	log.Printf("Contract.StegoImage after assignment: %s", contract.StegoImage)
	log.Printf("Full contract struct: %+v", contract)

	// Save contract
	mu.Lock()
	smartContracts = append(smartContracts, contract)
	saveSmartContracts()
	mu.Unlock()

	// Debug: check what we're about to encode
	responseData, _ := json.Marshal(contract)
	log.Printf("About to send response: %s", string(responseData))

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseData)
}

func handleSmartContracts(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(map[string]interface{}{
		"results": smartContracts,
		"total":   len(smartContracts),
	})
}

func handleBlockInscriptions(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	w.Header().Set("Content-Type", "application/json")

	height := r.URL.Query().Get("height")
	if height == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "height parameter required",
		})
		return
	}

	// Return mock data for now - TODO: Integrate with real inscription parsing
	json.NewEncoder(w).Encode(map[string]interface{}{
		"block_height": height,
		"results":      []SmartContractImage{},
		"total":        0,
		"message":      "Real inscription parsing integration needed",
	})
}

func handleBlockImages(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	w.Header().Set("Content-Type", "application/json")

	heightStr := r.URL.Query().Get("height")
	if heightStr == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "height parameter required",
		})
		return
	}

	height, err := strconv.ParseInt(heightStr, 10, 64)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "invalid height parameter",
		})
		return
	}

	// Get block inscriptions from block monitor
	response, err := blockMonitor.GetBlockInscriptions(height)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	if !response.Success {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":  response.Error,
			"images": []interface{}{},
			"total":  0,
		})
		return
	}

	var images []map[string]interface{}
	for _, inscription := range response.Inscriptions {
		// Only include inscriptions that have image files
		if inscription.FileName != "" && inscription.FilePath != "" {
			image := map[string]interface{}{
				"tx_id":        inscription.TxID,
				"file_name":    inscription.FileName,
				"file_path":    inscription.FilePath,
				"content_type": inscription.ContentType,
				"size_bytes":   inscription.SizeBytes,
				"content":      inscription.Content,
			}
			images = append(images, image)
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"images": images,
		"total":  len(images),
	})
}

func handleBlockImageFile(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	// Extract height and filename from URL path
	path := strings.TrimPrefix(r.URL.Path, "/api/block-image/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid URL format", http.StatusBadRequest)
		return
	}

	heightStr := parts[0]
	filename := parts[1]

	// Validate height
	_, err := strconv.ParseInt(heightStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid height parameter", http.StatusBadRequest)
		return
	}

	// Construct file path
	blockDir := fmt.Sprintf("blocks/%s_00000000", heightStr)
	filePath := filepath.Join(blockDir, "images", filename)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Serve the file
	http.ServeFile(w, r, filePath)
}

func main() {
	loadInscriptions()
	loadSmartContracts()

	// Initialize Bitcoin API
	bitcoinAPI = bitcoin.NewBitcoinAPI()

	// Initialize and start block monitor
	blockMonitor = bitcoin.NewBlockMonitor(bitcoinAPI.GetBitcoinClient())
	if err := blockMonitor.Start(); err != nil {
		log.Printf("Failed to start block monitor: %v", err)
	} else {
		log.Println("Block monitor started successfully")
	}

	// Serve uploaded images
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads/"))))

	// Serve frontend files
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "../index.html")
			return
		}
		if r.URL.Path == "/app.js" {
			http.ServeFile(w, r, "../app.js")
			return
		}
		http.NotFound(w, r)
	})

	// Original stargate endpoints
	http.HandleFunc("/api/blocks", handleBlocks)
	http.HandleFunc("/api/blocks-with-contracts", handleBlocksWithContracts)
	http.HandleFunc("/api/inscriptions", handleInscriptions)
	http.HandleFunc("/api/inscribe", handleInscribe)
	http.HandleFunc("/api/pending-transactions", handlePendingTransactions)
	http.HandleFunc("/api/search", handleSearch)
	http.HandleFunc("/api/inscription/", handleInscriptionContent)
	http.HandleFunc("/api/qrcode", handleQRCode)

	// Smart contract steganography endpoints
	http.HandleFunc("/api/contract-stego/", handleContractStego)
	http.HandleFunc("/api/contract-stego", handleCreateContractStego)

	// Cleaned version endpoints
	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "healthy",
			"message":   "Backend is running (restored version with full functionality)",
			"timestamp": time.Now().Unix(),
		})
	})
	http.HandleFunc("/api/smart-contracts", handleSmartContracts)
	http.HandleFunc("/api/block-inscriptions", handleBlockInscriptions)
	http.HandleFunc("/api/block-images", handleBlockImages)
	http.HandleFunc("/api/block-image/", handleBlockImageFile)

	// Proxy to real steganography API (port 8080)
	http.HandleFunc("/stego/", proxyToStegoAPI)
	http.HandleFunc("/analyze/", proxyToStegoAPI)
	http.HandleFunc("/generate/", proxyToStegoAPI)

	// Bitcoin steganography scanning endpoints
	http.HandleFunc("/bitcoin/v1/health", bitcoinAPI.HandleHealth)
	http.HandleFunc("/bitcoin/v1/info", bitcoinAPI.HandleInfo)
	http.HandleFunc("/bitcoin/v1/scan/transaction", bitcoinAPI.HandleScanTransaction)
	http.HandleFunc("/bitcoin/v1/scan/image", bitcoinAPI.HandleScanImage)
	http.HandleFunc("/bitcoin/v1/scan/block", bitcoinAPI.HandleBlockScan)
	http.HandleFunc("/bitcoin/v1/extract", bitcoinAPI.HandleExtract)
	http.HandleFunc("/bitcoin/v1/transaction/", bitcoinAPI.HandleGetTransaction)

	fmt.Println("Server starting on :3001")
	fmt.Println("Frontend available at: http://localhost:3001")
	fmt.Println("Stargate API endpoints at: http://localhost:3001/api/")
	fmt.Println("Bitcoin steganography API at: http://localhost:3001/bitcoin/v1/")
	fmt.Println("Smart contract steganography at: http://localhost:3001/api/contract-stego/")
	fmt.Println("Proxy to steganography API (port 8080) at: http://localhost:3001/stego/")
	log.Fatal(http.ListenAndServe(":3001", nil))
}
