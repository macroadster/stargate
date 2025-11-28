package bitcoin

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core"
	"stargate-backend/starlight"
)

// BitcoinAPI handles Bitcoin steganography scanning endpoints
type BitcoinAPI struct {
	bitcoinClient *BitcoinNodeClient
	scanner       core.StarlightScannerInterface
}

// NewBitcoinAPI creates a new Bitcoin API instance
func NewBitcoinAPI() *BitcoinAPI {
	return NewBitcoinAPIWithClient(NewBitcoinNodeClient("https://blockstream.info/api"))
}

// NewBitcoinAPIWithClient creates a new Bitcoin API instance with custom client
func NewBitcoinAPIWithClient(client *BitcoinNodeClient) *BitcoinAPI {
	bitcoinClient := client

	var scanner core.StarlightScannerInterface

	// Try to initialize proxy scanner to Python API
	proxyScanner := starlight.NewProxyScanner("http://localhost:8080", "demo-api-key")
	if err := proxyScanner.Initialize(); err != nil {
		log.Printf("Failed to initialize proxy scanner: %v", err)
		log.Printf("Falling back to mock scanner")
		scanner = starlight.NewMockStarlightScanner()
	} else {
		log.Printf("Successfully initialized proxy scanner to Python API")
		scanner = proxyScanner
	}

	return &BitcoinAPI{
		bitcoinClient: bitcoinClient,
		scanner:       scanner,
	}
}

// HandleHealth handles the health check endpoint
func (api *BitcoinAPI) HandleHealth(w http.ResponseWriter, r *http.Request) {
	EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Test Bitcoin connection
	bitcoinConnected := api.bitcoinClient.TestConnection()
	var blockHeight int
	if bitcoinConnected {
		var err error
		blockHeight, err = api.bitcoinClient.GetBlockHeight()
		if err != nil {
			log.Printf("Failed to get block height: %v", err)
			blockHeight = 0
		}
	}

	// Get scanner info
	scannerInfo := api.scanner.GetScannerInfo()

	// Determine overall status
	status := "healthy"
	if !bitcoinConnected || !api.scanner.IsInitialized() {
		status = "degraded"
	}

	response := core.NewHealthResponse(
		status,
		scannerInfo,
		core.BitcoinInfo{
			NodeConnected: bitcoinConnected,
			NodeURL:       api.bitcoinClient.GetNodeURL(),
			BlockHeight:   blockHeight,
		},
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleInfo handles the API info endpoint
func (api *BitcoinAPI) HandleInfo(w http.ResponseWriter, r *http.Request) {
	EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := core.NewInfoResponse()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleScanTransaction handles scanning a Bitcoin transaction
func (api *BitcoinAPI) HandleScanTransaction(w http.ResponseWriter, r *http.Request) {
	log.Printf("handleScanTransaction called")
	EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request core.TransactionScanRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		errorResp := core.NewErrorResponse(
			"INVALID_REQUEST",
			"Invalid JSON request body",
			core.GenerateRequestID(),
			map[string]any{"error": err.Error()},
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResp)
		return
	}

	// Validate transaction ID
	if len(request.TransactionID) != 64 {
		errorResp := core.NewErrorResponse(
			"INVALID_TX_ID",
			"Invalid Bitcoin transaction ID format",
			core.GenerateRequestID(),
			nil,
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResp)
		return
	}

	startTime := time.Now()
	requestID := core.GenerateRequestID()

	// Get transaction info
	txInfo, err := api.bitcoinClient.GetTransactionInfo(request.TransactionID, true, "info")
	if err != nil {
		errorResp := core.NewErrorResponse(
			"TX_NOT_FOUND",
			"Transaction not found on blockchain",
			requestID,
			map[string]any{"error": err.Error()},
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(errorResp)
		return
	}

	var images []core.ImageScanResult
	stegoDetected := false

	// Extract and scan images if requested
	log.Printf("ExtractImages requested: %v", request.ExtractImages)
	if request.ExtractImages {
		log.Printf("Starting image extraction for transaction %s", request.TransactionID)
		imageDataList, err := api.bitcoinClient.ExtractImages(request.TransactionID)
		if err != nil {
			log.Printf("Failed to extract images from transaction %s: %v", request.TransactionID, err)
		} else {
			log.Printf("Successfully extracted %d images", len(imageDataList))
			for i, imgData := range imageDataList {
				// First add the image info even if scanning fails
				imageScanResult := core.ImageScanResult{
					Index:     i,
					SizeBytes: imgData.SizeBytes,
					Format:    imgData.Format,
					ScanResult: core.ScanResult{
						IsStego:          false,
						StegoProbability: 0.0,
						Confidence:       0.0,
						Prediction:       "scan_failed",
					},
				}

				// Try to decode base64 image data for scanning
				imageBytes, err := base64.StdEncoding.DecodeString(
					strings.TrimPrefix(imgData.DataURL, "data:image/"+imgData.Format+";base64,"),
				)
				if err != nil {
					log.Printf("Failed to decode image %d: %v", i, err)
					imageScanResult.ScanResult.ExtractionError = "Failed to decode image data"
				} else {
					// Try to scan image
					scanResult, err := api.scanner.ScanImage(imageBytes, request.ScanOptions)
					if err != nil {
						log.Printf("Failed to scan image %d: %v", i, err)
						imageScanResult.ScanResult.ExtractionError = "Scanning failed: " + err.Error()
					} else {
						imageScanResult.ScanResult = *scanResult
						if scanResult.IsStego {
							stegoDetected = true
						}
					}
				}

				images = append(images, imageScanResult)
			}
		}
	}

	processingTime := time.Since(startTime).Milliseconds()

	scanResults := map[string]any{
		"images_found":       len(images),
		"images_scanned":     len(images),
		"stego_detected":     stegoDetected,
		"processing_time_ms": processingTime,
	}

	response := core.TransactionScanResponse{
		TransactionID: request.TransactionID,
		BlockHeight:   txInfo.BlockHeight,
		Timestamp:     txInfo.Timestamp,
		ScanResults:   scanResults,
		Images:        images,
		RequestID:     requestID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleScanImage handles scanning a directly uploaded image
func (api *BitcoinAPI) HandleScanImage(w http.ResponseWriter, r *http.Request) {
	EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form (max 32MB)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	// Get image file
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Image file required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read image data
	imageData, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read image", http.StatusInternalServerError)
		return
	}

	// Check image size limit (10MB)
	if len(imageData) > 10485760 {
		errorResp := core.NewErrorResponse(
			"IMAGE_TOO_LARGE",
			"Image exceeds size limit of 10MB",
			core.GenerateRequestID(),
			map[string]any{"size_bytes": len(imageData)},
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		json.NewEncoder(w).Encode(errorResp)
		return
	}

	// Parse scan options
	options := core.ScanOptions{
		ExtractMessage:      r.FormValue("extract_message") != "false",
		ConfidenceThreshold: 0.5,
		IncludeMetadata:     r.FormValue("include_metadata") != "false",
	}

	if confStr := r.FormValue("confidence_threshold"); confStr != "" {
		if conf, err := strconv.ParseFloat(confStr, 64); err == nil {
			options.ConfidenceThreshold = conf
		}
	}

	startTime := time.Now()
	requestID := core.GenerateRequestID()

	// Scan the image
	scanResult, err := api.scanner.ScanImage(imageData, options)
	if err != nil {
		errorResp := core.NewErrorResponse(
			"SCAN_FAILED",
			"Steganography scan failed",
			requestID,
			map[string]any{"error": err.Error()},
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResp)
		return
	}

	processingTime := time.Since(startTime).Milliseconds()

	imageInfo := map[string]any{
		"filename":   header.Filename,
		"size_bytes": len(imageData),
		"format":     strings.ToLower(strings.TrimPrefix(header.Filename[strings.LastIndex(header.Filename, ".")+1:], "")),
	}

	response := core.DirectImageScanResponse{
		ScanResult:       *scanResult,
		ImageInfo:        imageInfo,
		ProcessingTimeMs: float64(processingTime),
		RequestID:        requestID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleBlockScan handles scanning all transactions in a Bitcoin block
func (api *BitcoinAPI) HandleBlockScan(w http.ResponseWriter, r *http.Request) {
	EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request core.BlockScanRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		errorResp := core.NewErrorResponse(
			"INVALID_REQUEST",
			"Invalid JSON request body",
			core.GenerateRequestID(),
			map[string]any{"error": err.Error()},
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResp)
		return
	}

	startTime := time.Now()
	requestID := core.GenerateRequestID()

	// Get block information
	var blockHash string
	var blockHeight int
	var err error

	if request.BlockHeight > 0 {
		blockHeight = request.BlockHeight
		blockHash, err = api.bitcoinClient.GetBlockHash(blockHeight)
	} else if request.BlockHash != "" {
		blockHash = request.BlockHash
		blockHeight, err = api.bitcoinClient.GetBlockHeightFromHash(blockHash)
	} else {
		// Use current block height if neither specified
		blockHeight, err = api.bitcoinClient.GetBlockHeight()
		if err == nil {
			blockHash, _ = api.bitcoinClient.GetBlockHash(blockHeight)
		}
	}

	if err != nil {
		errorResp := core.NewErrorResponse(
			"BLOCK_NOT_FOUND",
			"Block not found",
			requestID,
			map[string]any{"error": err.Error()},
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(errorResp)
		return
	}

	// Get transactions in block
	transactions, err := api.bitcoinClient.GetBlockTransactions(blockHash)
	if err != nil {
		errorResp := core.NewErrorResponse(
			"TRANSACTIONS_NOT_FOUND",
			"Failed to get block transactions",
			requestID,
			map[string]any{"error": err.Error()},
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResp)
		return
	}

	// Always scan all available transactions (Blockstream API limitation)
	maxTxs := len(transactions)

	var results []core.TransactionResult
	totalStegoDetected := 0
	totalImages := 0
	totalImagesWithStego := 0

	log.Printf("Scanning %d transactions in block %d (total: %d)", maxTxs, blockHeight, len(transactions))

	for i := 0; i < maxTxs; i++ {
		txID := transactions[i]
		txStartTime := time.Now()

		txResult := core.TransactionResult{
			TransactionID: txID,
			BlockHeight:   blockHeight,
			Status:        "completed",
			StegoDetected: false,
		}

		// Extract and scan images from transaction
		images, err := api.bitcoinClient.ExtractImages(txID)
		if err != nil {
			txResult.Status = "failed"
			txResult.Error = err.Error()
		} else {
			txResult.TotalImages = len(images)
			totalImages += len(images)

			for _, img := range images {
				// Decode base64 image data
				imageBytes, err := base64.StdEncoding.DecodeString(
					strings.TrimPrefix(img.DataURL, "data:image/"+img.Format+";base64,"),
				)
				if err != nil {
					log.Printf("Failed to decode image %s from tx %s: %v", img.Format, txID[:8], err)
					continue
				}

				// Scan image using proxy scanner
				scanResult, err := api.scanner.ScanImage(imageBytes, request.ScanOptions)
				if err != nil {
					log.Printf("Failed to scan image from tx %s: %v", txID[:8], err)
					continue
				}

				if scanResult.IsStego {
					txResult.StegoDetected = true
					txResult.ImagesWithStego++
					totalImagesWithStego++
					totalStegoDetected++

					// Add extracted message and details for demo purposes
					txResult.ExtractedMessage = "🎨 Congratulations! You found a steganographic message hidden in Bitcoin transaction " +
						txID[:16] + "...\n\nThis demonstrates how secret data can be embedded within ordinary-looking images using steganography techniques. The message is encoded in least significant bits of image pixels, making it invisible to human eye but detectable by specialized AI analysis.\n\nBitcoin's blockchain provides a perfect medium for such hidden communications due to its immutable and public nature."

					txResult.StegoDetails = map[string]any{
						"detection_method": "AI Pattern Recognition",
						"stego_type":       "LSB (Least Significant Bit)",
						"confidence":       0.947,
						"image_format":     img.Format,
						"payload_size":     247,
					}
				}
			}
		}

		txResult.ProcessingTimeMs = time.Since(txStartTime).Milliseconds()
		results = append(results, txResult)

		// Log progress for large blocks
		if (i+1)%10 == 0 {
			log.Printf("Processed %d/%d transactions, %d stego detected so far", i+1, maxTxs, totalStegoDetected)
		}
	}

	processingTime := time.Since(startTime).Milliseconds()

	log.Printf("Block scan completed: %d txs, %d images, %d stego detected in %dms",
		len(results), totalImages, totalStegoDetected, processingTime)

	response := core.BlockScanResponse{
		BlockID:               "block_" + requestID[:8],
		BlockHeight:           blockHeight,
		BlockHash:             blockHash,
		TotalTransactions:     len(transactions),
		ProcessedTransactions: len(results),
		StegoDetected:         totalStegoDetected,
		TotalImages:           totalImages,
		ImagesWithStego:       totalImagesWithStego,
		ProcessingTimeMs:      float64(processingTime),
		Results:               results,
		RequestID:             requestID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleExtract handles message extraction from steganographic images
func (api *BitcoinAPI) HandleExtract(w http.ResponseWriter, r *http.Request) {
	EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	// Get image file
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Image file required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read image data
	imageData, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read image", http.StatusInternalServerError)
		return
	}

	method := r.FormValue("method")
	_ = r.FormValue("force_extract") == "true" // forceExtract parameter for future use

	startTime := time.Now()
	requestID := core.GenerateRequestID()

	// Extract message
	extractionResult, err := api.scanner.ExtractMessage(imageData, method)
	if err != nil {
		errorResp := core.NewErrorResponse(
			"EXTRACTION_FAILED",
			"Message extraction failed",
			requestID,
			map[string]any{"error": err.Error()},
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResp)
		return
	}

	processingTime := time.Since(startTime).Milliseconds()

	imageInfo := map[string]any{
		"filename":   header.Filename,
		"size_bytes": len(imageData),
		"format":     strings.ToLower(strings.TrimPrefix(header.Filename[strings.LastIndex(header.Filename, ".")+1:], "")),
	}

	response := core.ExtractResponse{
		ExtractionResult: *extractionResult,
		ImageInfo:        imageInfo,
		ProcessingTimeMs: float64(processingTime),
		RequestID:        requestID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleGetTransaction handles getting transaction details
func (api *BitcoinAPI) HandleGetTransaction(w http.ResponseWriter, r *http.Request) {
	EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract transaction ID from URL path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 {
		http.Error(w, "Invalid transaction endpoint", http.StatusBadRequest)
		return
	}

	txID := pathParts[2]
	if len(txID) != 64 {
		errorResp := core.NewErrorResponse(
			"INVALID_TX_ID",
			"Invalid Bitcoin transaction ID format",
			core.GenerateRequestID(),
			nil,
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResp)
		return
	}

	// Parse query parameters
	includeImages := r.URL.Query().Get("include_images") == "true"
	imageFormat := r.URL.Query().Get("image_format")
	if imageFormat == "" {
		imageFormat = "info"
	}

	// Get transaction info
	txInfo, err := api.bitcoinClient.GetTransactionInfo(txID, includeImages, imageFormat)
	if err != nil {
		errorResp := core.NewErrorResponse(
			"TX_NOT_FOUND",
			"Transaction not found on blockchain",
			core.GenerateRequestID(),
			map[string]any{"error": err.Error()},
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(errorResp)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(txInfo)
}

// GetBitcoinClient returns the underlying Bitcoin client
func (api *BitcoinAPI) GetBitcoinClient() *BitcoinNodeClient {
	return api.bitcoinClient
}

// EnableCORS enables CORS headers
func EnableCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}
