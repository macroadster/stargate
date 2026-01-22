package starlight

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"stargate-backend/core"
)

// ProxyScanner forwards requests to Python Starlight API on port 8080
type ProxyScanner struct {
	apiURL      string
	apiKey      string
	client      *http.Client
	initialized bool
	maxRetries  int
	retryDelay  time.Duration
}

// NewProxyScanner creates a new proxy scanner
func NewProxyScanner(apiURL string, apiKey string) *ProxyScanner {
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}
	if apiKey == "" {
		apiKey = "demo-api-key"
	}

	return &ProxyScanner{
		apiURL:      apiURL,
		apiKey:      apiKey,
		client:      &http.Client{Timeout: 120 * time.Second},
		initialized: false,
		maxRetries:  3,
		retryDelay:  1 * time.Second,
	}
}

// Initialize initializes the proxy scanner by testing connection
func (p *ProxyScanner) Initialize() error {
	// Test health endpoint
	req, err := http.NewRequest("GET", p.apiURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("failed to create health request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Python API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("Python API returned status %d", resp.StatusCode)
	}

	// Parse health response
	var health map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return fmt.Errorf("failed to parse health response: %w", err)
	}

	// Check if scanner is available
	if scanner, ok := health["scanner"].(map[string]any); ok {
		if modelLoaded, ok := scanner["model_loaded"].(bool); ok && modelLoaded {
			p.initialized = true
			return nil
		}
	}

	return fmt.Errorf("Python API scanner not ready")
}

// ScanImage scans an image by proxying to Python API
func (p *ProxyScanner) ScanImage(imageData []byte, options core.ScanOptions) (*core.ScanResult, error) {
	if !p.initialized {
		return nil, fmt.Errorf("steganography scanner not available - ensure Python backend is running on port 8080")
	}

	// Create multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add image file
	part, err := writer.CreateFormFile("image", "image.png")
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	_, err = part.Write(imageData)
	if err != nil {
		return nil, fmt.Errorf("failed to write image data: %w", err)
	}

	// Add form fields
	writer.WriteField("extract_message", fmt.Sprintf("%t", options.ExtractMessage))
	writer.WriteField("confidence_threshold", fmt.Sprintf("%.2f", options.ConfidenceThreshold))
	writer.WriteField("include_metadata", fmt.Sprintf("%t", options.IncludeMetadata))

	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create request
	req, err := http.NewRequest("POST", p.apiURL+"/scan/image", &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to ScanResult
	scanResult := &core.ScanResult{}
	if scanData, ok := result["scan_result"].(map[string]any); ok {
		if isStego, ok := scanData["is_stego"].(bool); ok {
			scanResult.IsStego = isStego
		}
		if prob, ok := scanData["stego_probability"].(float64); ok {
			scanResult.StegoProbability = prob
		}
		if conf, ok := scanData["confidence"].(float64); ok {
			scanResult.Confidence = conf
		}
		if pred, ok := scanData["prediction"].(string); ok {
			scanResult.Prediction = pred
		}
		if stegoType, ok := scanData["stego_type"].(string); ok && stegoType != "" {
			scanResult.StegoType = stegoType
		}
		if msg, ok := scanData["extracted_message"].(string); ok && msg != "" {
			scanResult.ExtractedMessage = msg
		}
		if err, ok := scanData["extraction_error"].(string); ok && err != "" {
			scanResult.ExtractionError = err
		}
	}

	return scanResult, nil
}

// ScanBlock scans an entire block using the Python API
// NOTE: The Python API has a native /scan/block endpoint that efficiently scans
// entire blocks in one request and automatically updates the inscriptions.json file.
// However, we implement block scanning in the Go backend for architectural consistency.
func (p *ProxyScanner) ScanBlock(blockHeight int64, options core.ScanOptions) (*core.BlockScanResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("steganography scanner not available - ensure Python backend is running on port 8080")
	}

	// Create request payload
	request := map[string]interface{}{
		"block_height": blockHeight,
		"scan_options": map[string]interface{}{
			"extract_message":      options.ExtractMessage,
			"confidence_threshold": options.ConfidenceThreshold,
			"include_metadata":     options.IncludeMetadata,
		},
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	req, err := http.NewRequest("POST", p.apiURL+"/scan/block", bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var response core.BlockScanResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

// ExtractMessage extracts message by proxying to Python API
func (p *ProxyScanner) ExtractMessage(imageData []byte, method string) (*core.ExtractionResult, error) {
	if !p.initialized {
		return nil, fmt.Errorf("steganography scanner not available - ensure Python backend is running on port 8080")
	}

	// Create multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add image file
	part, err := writer.CreateFormFile("image", "image.png")
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	_, err = part.Write(imageData)
	if err != nil {
		return nil, fmt.Errorf("failed to write image data: %w", err)
	}

	// Add method field
	writer.WriteField("method", method)

	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create request
	req, err := http.NewRequest("POST", p.apiURL+"/extract", &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to ExtractionResult
	extractionResult := &core.ExtractionResult{
		MessageFound: false,
		ExtractionDetails: map[string]any{
			"bits_extracted":      0,
			"encoding":            "utf-8",
			"corruption_detected": false,
		},
	}

	if extractionData, ok := result["extraction_result"].(map[string]any); ok {
		if msgFound, ok := extractionData["message_found"].(bool); ok {
			extractionResult.MessageFound = msgFound
		}
		if msg, ok := extractionData["message"].(string); ok && msg != "" {
			extractionResult.Message = msg
		}
		if method, ok := extractionData["method_used"].(string); ok && method != "" {
			extractionResult.MethodUsed = method
		}
		if conf, ok := extractionData["method_confidence"].(float64); ok {
			extractionResult.MethodConfidence = conf
		}
		if details, ok := extractionData["extraction_details"].(map[string]any); ok {
			extractionResult.ExtractionDetails = details
		}
	}

	return extractionResult, nil
}

// GetScannerInfo returns info about proxied scanner
func (p *ProxyScanner) GetScannerInfo() core.ScannerInfo {
	// Get real info from Python API
	req, err := http.NewRequest("GET", p.apiURL+"/health", nil)
	if err != nil {
		return core.ScannerInfo{
			ModelLoaded:  false,
			ModelVersion: "unknown",
			ModelPath:    "proxy",
			Device:       "proxy",
		}
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return core.ScannerInfo{
			ModelLoaded:  false,
			ModelVersion: "unknown",
			ModelPath:    "proxy",
			Device:       "proxy",
		}
	}
	defer resp.Body.Close()

	var health map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return core.ScannerInfo{
			ModelLoaded:  false,
			ModelVersion: "unknown",
			ModelPath:    "proxy",
			Device:       "proxy",
		}
	}

	// Extract scanner info
	if scanner, ok := health["scanner"].(map[string]any); ok {
		info := core.ScannerInfo{
			ModelLoaded: true,
			ModelPath:   "proxy-to-python-api",
			Device:      "proxy",
		}

		if version, ok := scanner["model_version"].(string); ok {
			info.ModelVersion = version
		}
		if path, ok := scanner["model_path"].(string); ok {
			info.ModelPath = fmt.Sprintf("proxy -> %s", path)
		}
		if device, ok := scanner["device"].(string); ok {
			info.Device = fmt.Sprintf("proxy -> %s", device)
		}

		return info
	}

	return core.ScannerInfo{
		ModelLoaded:  p.initialized,
		ModelVersion: "proxy-v1.0",
		ModelPath:    "proxy-to-python-api",
		Device:       "proxy",
	}
}

// IsInitialized returns initialization status
func (p *ProxyScanner) IsInitialized() bool {
	return p.initialized
}

// doRequestWithRetry executes an HTTP request with exponential backoff retry logic
func (p *ProxyScanner) doRequestWithRetry(req *http.Request) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: base delay * 2^(attempt-1) with jitter
			backoffDelay := p.retryDelay * time.Duration(1<<(attempt-1))
			// Add jitter to prevent thundering herd
			jitter := time.Duration(float64(backoffDelay) * 0.1 * (0.5 + 0.5*float64(time.Now().UnixNano()%1000)/1000))
			delay := backoffDelay + jitter

			fmt.Printf("Retrying request to Python API (attempt %d/%d) after %v delay\n", attempt, p.maxRetries, delay)
			time.Sleep(delay)
		}

		resp, err := p.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		// Check for server errors that might be transient
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", p.maxRetries+1, lastErr)
}
