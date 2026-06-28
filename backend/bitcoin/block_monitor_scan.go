package bitcoin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"

	"stargate-backend/core"
	"stargate-backend/security"
	"stargate-backend/services"
)

// sanitizeExtractedImage removes opcode prefixes and stray metadata from payloads before persisting to disk.
func sanitizeExtractedImage(img ExtractedImageData) ExtractedImageData {
	cleaned := img
	data := img.Data
	mime := strings.ToLower(strings.TrimSpace(img.ContentType))
	if mime == "" && img.Format != "" {
		if strings.HasPrefix(img.Format, "text") || img.Format == "txt" {
			mime = "text/plain"
		} else {
			mime = fmt.Sprintf("image/%s", img.Format)
		}
	}

	if strings.HasPrefix(mime, "image/") {
		if strings.HasPrefix(mime, "image/svg") {
			// Treat SVG as text-like for cleanup: trim to first tag.
			if idx := bytes.IndexByte(data, '<'); idx >= 0 {
				data = data[idx:]
			}
		} else if trimmed := trimToImageSignatureLocal(data); len(trimmed) > 0 {
			data = trimmed
		}
	} else {
		// HTML and other text-like bodies may have leading metadata/prefix bytes before the first tag.
		if strings.HasPrefix(mime, "text/html") || strings.HasSuffix(strings.ToLower(img.FileName), ".html") || strings.HasPrefix(mime, "image/svg") {
			if idx := bytes.IndexByte(data, '<'); idx >= 0 {
				data = data[idx:]
			}
		}
	}

	cleaned.Data = data
	cleaned.SizeBytes = len(data)
	return cleaned
}

// sanitizeInscriptionsForDisk cleans inscription content before writing JSON summaries.
func sanitizeInscriptionsForDisk(inscriptions []InscriptionData) []InscriptionData {
	out := make([]InscriptionData, len(inscriptions))
	for i, ins := range inscriptions {
		cleaned := ins
		data := []byte(ins.Content)
		mime := strings.ToLower(strings.TrimSpace(ins.ContentType))

		if strings.HasPrefix(mime, "image/svg") {
			// Treat SVG as text-ish: trim to first tag.
			if idx := bytes.IndexByte(data, '<'); idx >= 0 {
				data = data[idx:]
			}
		} else if strings.HasPrefix(mime, "image/") {
			if trimmed := trimToImageSignatureLocal(data); len(trimmed) > 0 {
				data = trimmed
			}
		} else {
			if strings.HasPrefix(mime, "text/html") || strings.HasSuffix(strings.ToLower(ins.FileName), ".html") {
				if idx := bytes.IndexByte(data, '<'); idx >= 0 {
					data = data[idx:]
				}
			}
		}

		cleaned.Content = string(data)
		cleaned.SizeBytes = len(data)
		out[i] = cleaned
	}
	return out
}

// stripPushdataPrefixLocal removes a leading push opcode (OP_PUSH, OP_PUSHDATA1/2/4) from a payload when present.
func stripPushdataPrefixLocal(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	op := b[0]
	switch {
	case op <= 75:
		if len(b) > int(op) {
			return b[1:]
		}
	case op == 0x4c && len(b) > 1: // OP_PUSHDATA1
		l := int(b[1])
		if len(b) >= 2+l {
			return b[2:]
		}
	case op == 0x4d && len(b) > 2: // OP_PUSHDATA2
		l := int(b[1]) | int(b[2])<<8
		if len(b) >= 3+l {
			return b[3:]
		}
	case op == 0x4e && len(b) > 4: // OP_PUSHDATA4
		l := int(b[1]) | int(b[2])<<8 | int(b[3])<<16 | int(b[4])<<24
		if len(b) >= 5+l {
			return b[5:]
		}
	}
	return b
}

// stripNonPrintablePrefixLocal trims leading control bytes to get to the printable payload.
func stripNonPrintablePrefixLocal(b []byte) []byte {
	i := 0
	for i < len(b) {
		c := b[i]
		if c == '\n' || c == '\r' || c == '\t' || (c >= 32 && c < 127) {
			break
		}
		i++
	}
	if i > 0 && i < len(b) {
		return b[i:]
	}
	return b
}

// convertTransactions converts parsed transactions to transaction data format
func (bm *BlockMonitor) convertTransactions(transactions []Transaction) []TransactionData {
	var txData []TransactionData

	for _, tx := range transactions {
		witnessCount := 0
		for _, stack := range tx.InputWitnesses {
			witnessCount += len(stack)
		}
		data := TransactionData{
			TxID:       tx.TxID,
			Height:     0, // Will be set by caller
			Time:       int64(tx.Locktime),
			Status:     "confirmed",
			HasImages:  witnessCount > 0,
			ImageCount: witnessCount,
		}
		txData = append(txData, data)
	}

	return txData
}

// scanBlockViaAPI calls the /scan/block API endpoint to scan a block
func (bm *BlockMonitor) scanBlockViaAPI(height int64) ([]map[string]any, error) {
	// Create the request for the /scan/block API
	request := core.BlockScanRequest{
		BlockHeight: int(height),
		ScanOptions: core.ScanOptions{
			ExtractMessage:      true,
			ConfidenceThreshold: 0.5,
			IncludeMetadata:     true,
		},
	}

	// Marshal the request
	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal scan request: %w", err)
	}

	// Make HTTP request to the Go backend's Bitcoin API
	client := &http.Client{Timeout: 300 * time.Second} // 5 minute timeout for large blocks
	req, err := http.NewRequest("POST", "http://localhost:3001/bitcoin/v1/scan/block", bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call scan API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("scan API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var scanResponse core.BlockScanResponse
	if err := json.NewDecoder(resp.Body).Decode(&scanResponse); err != nil {
		return nil, fmt.Errorf("failed to decode scan response: %w", err)
	}

	// Convert the API response to the expected format for block monitor
	var results []map[string]any
	for i, inscription := range scanResponse.Inscriptions {
		result := map[string]any{
			"tx_id":             inscription.TxID,
			"image_index":       i,
			"file_name":         inscription.FileName,
			"size_bytes":        inscription.SizeBytes,
			"format":            "unknown",
			"scanned_at":        time.Now().Unix(),
			"is_stego":          false,
			"confidence":        0.0,
			"stego_type":        "",
			"extracted_message": "",
			"scan_error":        "",
			"stego_details":     nil,
		}

		if inscription.ScanResult != nil {
			result["is_stego"] = inscription.ScanResult.IsStego
			result["confidence"] = inscription.ScanResult.Confidence
			if inscription.ScanResult.StegoType != "" {
				result["stego_type"] = inscription.ScanResult.StegoType
			}
			if inscription.ScanResult.ExtractedMessage != "" {
				result["extracted_message"] = inscription.ScanResult.ExtractedMessage
			}
			if inscription.ScanResult.ExtractionError != "" {
				result["scan_error"] = inscription.ScanResult.ExtractionError
			}
		}

		results = append(results, result)
	}

	return results, nil
}

// scanImagesDirectly scans images using the BitcoinAPI directly
func (bm *BlockMonitor) scanImagesDirectly(images []ExtractedImageData) ([]map[string]any, error) {
	log.Printf("scanImagesDirectly called with %d images", len(images))
	var results []map[string]any

	for i, image := range images {
		// Create scan result for this image
		result := map[string]any{
			"tx_id":             image.TxID,
			"image_index":       i,
			"file_name":         image.FileName,
			"size_bytes":        image.SizeBytes,
			"format":            image.Format,
			"scanned_at":        time.Now().Unix(),
			"is_stego":          false,
			"confidence":        0.0,
			"stego_type":        "",
			"extracted_message": "",
			"scan_error":        "",
			"stego_details":     nil,
		}

		// Try to scan the image using the scanner manager
		if bm.bitcoinAPI != nil && bm.bitcoinAPI.scannerManager != nil {
			log.Printf("Scanning image %d: %s (%d bytes)", i, image.FileName, len(image.Data))
			scanResult, err := bm.bitcoinAPI.scannerManager.ScanImage(image.Data, core.ScanOptions{
				ExtractMessage:      true,
				ConfidenceThreshold: 0.5,
				IncludeMetadata:     true,
			})
			if err != nil {
				log.Printf("Failed to scan image %s: %v", image.FileName, err)
				result["scan_error"] = err.Error()
			} else {
				log.Printf("Scanned image %s: is_stego=%v, confidence=%.2f", image.FileName, scanResult.IsStego, scanResult.Confidence)
				result["is_stego"] = scanResult.IsStego
				result["confidence"] = scanResult.Confidence
				if scanResult.StegoType != "" {
					result["stego_type"] = scanResult.StegoType
				}
				if scanResult.ExtractedMessage != "" {
					result["extracted_message"] = scanResult.ExtractedMessage
				}
				if scanResult.ExtractionError != "" {
					result["scan_error"] = scanResult.ExtractionError
				}
			}
		} else {
			log.Printf("Scanner not available for image %s", image.FileName)
			result["scan_error"] = "Scanner not available"
		}

		results = append(results, result)
	}

	log.Printf("scanImagesDirectly completed, scanned %d images", len(results))
	return results, nil
}

// createEmptyScanResults creates empty scan results for all images
func (bm *BlockMonitor) createEmptyScanResults(count int) []map[string]any {
	results := make([]map[string]any, count)
	for i := 0; i < count; i++ {
		results[i] = map[string]any{
			"tx_id":             "",
			"image_index":       i,
			"file_name":         "",
			"size_bytes":        0,
			"format":            "",
			"scanned_at":        time.Now().Unix(),
			"is_stego":          false,
			"confidence":        0.0,
			"stego_type":        "",
			"extracted_message": "",
			"scan_error":        "not_scanned",
			"stego_details":     nil,
		}
	}
	return results
}

// saveBlockSummaryWithScanResults saves block summary including steganography scan results
func (bm *BlockMonitor) saveBlockSummaryWithScanResults(blockDir string, parsedBlock *ParsedBlock, inscriptions []InscriptionData, scanResults []map[string]any, blockHeight int64, smartContracts []SmartContractData) error {
	// Count stego detections
	stegoCount := bm.countStegoImages(scanResults)

	// Clean payloads before persisting to disk so downstream consumers don't see opcode metadata.
	cleanedImages := make([]ExtractedImageData, len(parsedBlock.Images))
	for i, img := range parsedBlock.Images {
		cleanedImages[i] = sanitizeExtractedImage(img)
	}
	cleanedInscriptions := sanitizeInscriptionsForDisk(inscriptions)

	// Create optimized summary with minimal data
	summary := BlockInscriptionsResponse{
		BlockHeight:       blockHeight,
		BlockHash:         parsedBlock.Header.Hash,
		Timestamp:         int64(parsedBlock.Header.Timestamp),
		TotalTransactions: len(parsedBlock.Transactions),
		Inscriptions:      cleanedInscriptions,
		Images:            cleanedImages,
		SmartContracts:    smartContracts,
		ProcessingTime:    time.Now().Unix(),
		Success:           true,
	}

	// Create compact steganography summary (only once)
	steganographySummary := map[string]any{
		"total_images":   len(cleanedImages),
		"stego_detected": stegoCount > 0,
		"stego_count":    stegoCount,
		"scan_timestamp": time.Now().Unix(),
	}

	// Create enhanced images with scan results
	enhancedImages := make([]map[string]any, len(cleanedImages))
	for i, image := range cleanedImages {
		enhancedImage := map[string]any{
			"tx_id":      image.TxID,
			"format":     image.Format,
			"size_bytes": image.SizeBytes,
			"file_name":  image.FileName,
			"file_path":  image.FilePath,
		}

		// Add scan result if available
		if len(scanResults) > i {
			scanResult := scanResults[i]
			if scanResult != nil {
				enhancedImage["scan_result"] = scanResult
			} else {
				// Default scan result for unscanned images
				enhancedImage["scan_result"] = map[string]any{
					"is_stego":          false,
					"stego_probability": 0.0,
					"confidence":        0.0,
					"prediction":        "not_scanned",
				}
			}
		} else {
			// Default scan result for unscanned images
			enhancedImage["scan_result"] = map[string]any{
				"is_stego":          false,
				"stego_probability": 0.0,
				"confidence":        0.0,
				"prediction":        "not_scanned",
			}
		}

		enhancedImages[i] = enhancedImage
	}

	// Only include steganography data if detections exist
	var enhancedSummary map[string]any
	if stegoCount > 0 {
		enhancedSummary = map[string]any{
			"block_height":       summary.BlockHeight,
			"block_hash":         summary.BlockHash,
			"timestamp":          summary.Timestamp,
			"total_transactions": summary.TotalTransactions,
			"inscriptions":       summary.Inscriptions,
			"images":             enhancedImages,
			"smart_contracts":    summary.SmartContracts,
			"processing_time_ms": summary.ProcessingTime,
			"success":            summary.Success,
			"steganography_scan": steganographySummary,
		}
	} else {
		enhancedSummary = map[string]any{
			"block_height":       summary.BlockHeight,
			"block_hash":         summary.BlockHash,
			"timestamp":          summary.Timestamp,
			"total_transactions": summary.TotalTransactions,
			"inscriptions":       summary.Inscriptions,
			"images":             enhancedImages,
			"smart_contracts":    summary.SmartContracts,
			"processing_time_ms": summary.ProcessingTime,
			"success":            summary.Success,
		}
	}

	summaryJSON, err := json.MarshalIndent(enhancedSummary, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal enhanced summary: %w", err)
	}

	summaryFile := filepath.Join(blockDir, "inscriptions.json")
	if err := os.WriteFile(summaryFile, summaryJSON, 0644); err != nil {
		return fmt.Errorf("failed to write summary file: %w", err)
	}

	return nil
}

// createSmartContractsFromScanResults creates smart contract data from steganography scan results
func (bm *BlockMonitor) createSmartContractsFromScanResults(scanResults []map[string]any) []SmartContractData {
	var contracts []SmartContractData

	for _, result := range scanResults {
		if isStego, ok := result["is_stego"].(bool); ok && isStego {
			contract := SmartContractData{
				ContractID:  fmt.Sprintf("stego_%v_%d", result["image_index"], time.Now().Unix()),
				BlockHeight: 0, // Will be set by caller
				ImagePath:   fmt.Sprintf("%v", result["file_name"]),
				Confidence:  0.0,
				Metadata: map[string]any{
					"tx_id":             result["tx_id"],
					"image_index":       result["image_index"],
					"stego_type":        result["stego_type"],
					"extracted_message": result["extracted_message"],
					"scan_confidence":   result["confidence"],
					"scan_timestamp":    result["scanned_at"],
					"format":            result["format"],
					"size_bytes":        result["size_bytes"],
				},
			}

			if conf, ok := result["confidence"].(float64); ok {
				contract.Confidence = conf
			}

			contracts = append(contracts, contract)
		}
	}

	return contracts
}

// parseOPReturnHashes extracts wish_hash and stego_image_hash from an
// OP_RETURN output.  Returns (wishHash, stegoHash, ok).
// The expected formats are:
//
//	OP_RETURN <64 bytes: wish_hash(32) || stego_hash(32)>
//	OP_RETURN <32 bytes: wish_hash only>
//
// The sandbox_hash is not on-chain — it lives inside the stego v2 JSON
// payload.  Peers find the stego image by its on-chain hash, extract the
// payload, and read sandbox_hash from there.
func parseOPReturnHashes(script []byte) (string, string, bool) {
	if len(script) < 2 || script[0] != txscript.OP_RETURN {
		return "", "", false
	}
	// Disassemble the script to get the data pushes.
	tokenizer := txscript.MakeScriptTokenizer(0, script[1:])
	var data []byte
	for tokenizer.Next() {
		data = append(data, tokenizer.Data()...)
	}
	if tokenizer.Err() != nil {
		return "", "", false
	}
	switch len(data) {
	case 64: // wish_hash(32) || stego_hash(32)
		return hex.EncodeToString(data[:32]), hex.EncodeToString(data[32:64]), true
	case 32: // wish_hash only
		return hex.EncodeToString(data[:32]), "", true
	default:
		return "", "", false
	}
}

func (bm *BlockMonitor) findImageForScanResult(images []ExtractedImageData, result map[string]any) *ExtractedImageData {
	fileName := stringFromAny(result["file_name"])
	if fileName != "" {
		for i := range images {
			if images[i].FileName == fileName {
				return &images[i]
			}
		}
	}
	txID := stringFromAny(result["tx_id"])
	if txID != "" {
		for i := range images {
			if images[i].TxID == txID {
				return &images[i]
			}
		}
	}
	return nil
}

func parseScanPayload(result map[string]any) scanPayload {
	payload := scanPayload{
		message:          strings.TrimSpace(stringFromAny(result["extracted_message"])),
		payoutAddress:    strings.TrimSpace(stringFromAny(result["payout_address"])),
		payoutScript:     normalizeHex(stringFromAny(result["payout_script"])),
		payoutScriptHash: normalizeHex(stringFromAny(result["payout_script_hash"])),
	}
	if payload.payoutAddress == "" {
		payload.payoutAddress = strings.TrimSpace(stringFromAny(result["address"]))
	}

	raw := payload.message
	if raw == "" {
		raw = strings.TrimSpace(stringFromAny(result["embedded_message"]))
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		if payload.message == "" {
			payload.message = raw
		}
		return payload
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		if payload.message == "" {
			payload.message = raw
		}
		return payload
	}

	if msg := strings.TrimSpace(stringFromAny(parsed["message"])); msg != "" {
		payload.message = msg
	}
	if payload.message == "" {
		if msg := strings.TrimSpace(stringFromAny(parsed["embedded_message"])); msg != "" {
			payload.message = msg
		}
	}
	if payload.payoutAddress == "" {
		payload.payoutAddress = strings.TrimSpace(stringFromAny(parsed["address"]))
	}
	if payload.payoutAddress == "" {
		payload.payoutAddress = strings.TrimSpace(stringFromAny(parsed["payout_address"]))
	}
	if payload.payoutScript == "" {
		payload.payoutScript = normalizeHex(stringFromAny(parsed["payout_script"]))
	}
	if payload.payoutScriptHash == "" {
		payload.payoutScriptHash = normalizeHex(stringFromAny(parsed["payout_script_hash"]))
	}

	return payload
}

func (bm *BlockMonitor) matchPayoutScript(tx Transaction, payload scanPayload) ([]byte, bool) {
	if payload.payoutScript == "" && payload.payoutScriptHash == "" && payload.payoutAddress == "" {
		return nil, false
	}

	if payload.payoutScript != "" {
		if script, err := hex.DecodeString(payload.payoutScript); err == nil {
			for _, output := range tx.Outputs {
				if bytes.Equal(output.ScriptPubKey, script) {
					return script, true
				}
			}
		}
	}

	if payload.payoutAddress != "" {
		if script := bm.scriptForAddress(payload.payoutAddress); len(script) > 0 {
			for _, output := range tx.Outputs {
				if bytes.Equal(output.ScriptPubKey, script) {
					return script, true
				}
			}
		}
	}

	if payload.payoutScriptHash != "" {
		for _, output := range tx.Outputs {
			if scriptHashHex(output.ScriptPubKey) == payload.payoutScriptHash {
				return output.ScriptPubKey, true
			}
			if scriptHash160Hex(output.ScriptPubKey) == payload.payoutScriptHash {
				return output.ScriptPubKey, true
			}
		}
	}

	return nil, false
}

func (bm *BlockMonitor) scriptForAddress(address string) []byte {
	params := bm.networkParams()
	addr, err := btcutil.DecodeAddress(address, params)
	if err != nil {
		return nil
	}
	script, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil
	}
	return script
}

func (bm *BlockMonitor) networkParams() *chaincfg.Params {
	switch bm.bitcoinClient.GetNetwork() {
	case "mainnet":
		return &chaincfg.MainNetParams
	case "signet":
		return &chaincfg.SigNetParams
	case "testnet":
		return &chaincfg.TestNet3Params
	case "testnet4":
		return &chaincfg.TestNet4Params
	default:
		return &chaincfg.TestNet4Params
	}
}

func (bm *BlockMonitor) moveIngestionImage(blockDir string, rec *services.IngestionRecord) (string, error) {
	return bm.moveIngestionImageWithFilename(blockDir, rec, "")
}

func (bm *BlockMonitor) moveIngestionImageWithFilename(blockDir string, rec *services.IngestionRecord, destFilename string) (string, error) {
	uploadsDir := os.Getenv("UPLOADS_DIR")
	filename := strings.TrimSpace(rec.Filename)
	if filename == "" {
		filename = "inscription" // Stealthy: no extension
	}
	if strings.TrimSpace(destFilename) == "" {
		destFilename = blockImageFilename(rec, "")
		if destFilename == "" {
			destFilename = filename
		}
	}

	destDir := filepath.Join(blockDir, "images")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create images dir: %w", err)
	}
	destPath := security.SafeFilePath(destDir, destFilename)
	if _, err := os.Stat(destPath); err == nil {
		// Already copied — keep upload files for IPFS mirror.
		return destPath, nil
	}

	if stegoPath, ok := bm.stegoImagePath(rec); ok {
		if err := bm.copyStegoToBlock(stegoPath, destPath); err == nil {
			// Keep stegoPath in uploads/ so the IPFS mirror continues serving it.
			return destPath, nil
		}
	}
	if bm.writeStegoToBlock(rec, destPath) {
		return destPath, nil
	}

	sourcePath := ""
	var candidates []string
	if filename != "" {
		candidates = append(candidates, filename)
	}
	if rec.ID != "" && filename != "" {
		candidates = append(candidates, fmt.Sprintf("%s_%s", rec.ID, filename))
	}
	if rec.ID != "" && filepath.Base(filename) != filename {
		candidates = append(candidates, fmt.Sprintf("%s_%s", rec.ID, filepath.Base(filename)))
	}
	for _, candidate := range candidates {
		path := filepath.Join(uploadsDir, candidate)
		if _, err := os.Stat(path); err == nil {
			sourcePath = path
			break
		}
	}
	if sourcePath == "" && rec.ID != "" {
		// First try hash-only filename (new stealth naming)
		hashPath := filepath.Join(uploadsDir, rec.ID)
		if _, err := os.Stat(hashPath); err == nil {
			sourcePath = hashPath
		} else {
			// Fallback to old pattern with prefix
			matches, _ := filepath.Glob(filepath.Join(uploadsDir, rec.ID+"_*"))
			if len(matches) > 0 {
				sort.Strings(matches)
				sourcePath = matches[0]
			}
		}
	}
	if sourcePath == "" {
		if len(rec.ImageBase64) == 0 {
			return "", fmt.Errorf("missing ingestion image for %s", rec.ID)
		}
		data, err := base64.StdEncoding.DecodeString(rec.ImageBase64)
		if err != nil {
			return "", fmt.Errorf("decode ingestion image: %w", err)
		}
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			return "", fmt.Errorf("write ingestion image: %w", err)
		}
		return destPath, nil
	}

	// Copy instead of move — keep the source in uploads/ so the IPFS mirror
	// continues to serve confirmed wish images to new nodes joining the network.
	if err := copyFile(sourcePath, destPath); err != nil {
		return "", err
	}
	return destPath, nil
}

func (bm *BlockMonitor) stegoImagePath(rec *services.IngestionRecord) (string, bool) {
	if bm == nil || rec == nil || rec.Metadata == nil {
		return "", false
	}
	stegoCID := stegoCIDFromRecord(rec)
	if stegoCID == "" {
		return "", false
	}
	uploadsDir := os.Getenv("UPLOADS_DIR")
	// First try hash-only filename (new stealth naming)
	hashPath := filepath.Join(uploadsDir, stegoCID)
	if _, err := os.Stat(hashPath); err == nil {
		return hashPath, true
	}
	// Fallback to old pattern with prefix
	if matches, _ := filepath.Glob(filepath.Join(uploadsDir, stegoCID+"*")); len(matches) > 0 {
		sort.Strings(matches)
		return matches[0], true
	}
	return "", false
}

func stegoCIDFromRecord(rec *services.IngestionRecord) string {
	if rec == nil || rec.Metadata == nil {
		return ""
	}
	stegoCID := strings.TrimSpace(stringFromAny(rec.Metadata["stego_image_cid"]))
	if stegoCID == "" {
		stegoCID = strings.TrimSpace(stringFromAny(rec.Metadata["stego_cid"]))
	}
	return stegoCID
}

func (bm *BlockMonitor) writeStegoToBlock(rec *services.IngestionRecord, destPath string) bool {
	if bm == nil || bm.ipfsClient == nil {
		return false
	}
	stegoCID := stegoCIDFromRecord(rec)
	if stegoCID == "" {
		return false
	}
	if destPath == "" {
		return false
	}
	stegoBytes, err := bm.ipfsClient.Cat(context.Background(), stegoCID)
	if err != nil || len(stegoBytes) == 0 {
		return false
	}
	if err := os.WriteFile(destPath, stegoBytes, 0644); err != nil {
		log.Printf("failed to write stego image to block dir: %v", err)
		return false
	}
	return true
}

func (bm *BlockMonitor) copyStegoToBlock(src, dest string) error {
	if err := copyFile(src, dest); err != nil {
		return err
	}
	return nil
}

func (bm *BlockMonitor) unpinUploadPath(path string) {
	if bm == nil || strings.TrimSpace(path) == "" {
		return
	}
	if bm.unpinPath != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := bm.unpinPath(ctx, path); err != nil {
			log.Printf("ipfs unpin failed for %s: %v", path, err)
		}
	}
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if rel, err := filepath.Rel(uploadsDir, path); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".." {
		_ = os.Remove(path)
	}
}

func (bm *BlockMonitor) cleanupUploadArtifacts(rec *services.IngestionRecord) {
	if bm == nil || rec == nil {
		return
	}
	id := strings.TrimSpace(rec.ID)
	if id == "" {
		return
	}
	uploadsDir := os.Getenv("UPLOADS_DIR")
	// First try hash-only filename (new stealth naming)
	hashPath := filepath.Join(uploadsDir, id)
	if _, err := os.Stat(hashPath); err == nil {
		bm.unpinUploadPath(hashPath)
	}
	// Also cleanup old pattern files
	pattern := filepath.Join(uploadsDir, id+"_*")
	matches, _ := filepath.Glob(pattern)
	for _, match := range matches {
		bm.unpinUploadPath(match)
	}
}

func (bm *BlockMonitor) maybeReconcileStego(rec *services.IngestionRecord) {
	if bm.stegoReconciler == nil || rec == nil {
		return
	}
	meta := rec.Metadata
	if meta == nil {
		return
	}
	stegoCID := strings.TrimSpace(stringFromAny(meta["stego_image_cid"]))
	if stegoCID == "" {
		stegoCID = strings.TrimSpace(stringFromAny(meta["stego_cid"]))
	}
	if stegoCID == "" {
		stegoCID = strings.TrimSpace(stringFromAny(meta["ipfs_image_cid"]))
	}
	if stegoCID == "" {
		return
	}
	if strings.TrimSpace(stringFromAny(meta["stego_reconciled_at"])) != "" {
		return
	}
	expected := strings.TrimSpace(stringFromAny(meta["stego_contract_id"]))
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := bm.stegoReconciler.ReconcileStego(ctx, stegoCID, expected); err != nil {
		log.Printf("stego reconcile failed for %s (cid=%s): %v", rec.ID, stegoCID, err)
		return
	}
	if bm.ingestion != nil {
		now := strconv.FormatInt(time.Now().Unix(), 10)
		if err := bm.ingestion.UpdateMetadata(rec.ID, map[string]interface{}{
			"stego_reconciled_at": now,
		}); err != nil {
			log.Printf("stego reconcile: failed to mark ingestion %s reconciled: %v", rec.ID, err)
		}
	}
}

// reconcileOnChainArtifacts uses the stego_hash from the OP_RETURN to find
// the stego image in UPLOADS_DIR and trigger stego reconciliation + sandbox
// extraction.  sandbox_hash is inside the stego v2 JSON payload — the stego
// reconciler extracts it automatically.  This is the primary replication
// path — no pubsub or STARGATE_STEGO_APPROVAL_ENABLED needed.
func (bm *BlockMonitor) reconcileOnChainArtifacts(contractID, stegoHash string) {
	if contractID == "" {
		return
	}
	uploadsDir := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
	if uploadsDir == "" {
		uploadsDir = "data/uploads"
	}

	// Reconcile stego image: find UPLOADS_DIR/<stegoHash> and extract
	// the embedded v2 payload (proposal + tasks + sandbox_hash + metadata).
	if stegoHash != "" {
		stegoPath := filepath.Join(uploadsDir, stegoHash)
		if _, err := os.Stat(stegoPath); err == nil {
			log.Printf("oracle reconcile: found stego image on disk for %s at %s, reconciling", contractID, stegoPath)
			if bm.stegoReconciler != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				// Pass stegoHash as both CID and expected hash — reconcileStegoFromIPFS
				// will fail (no IPFS CID), so use the local-file reconcile path instead.
				// The stego reconciler already knows how to read from UPLOADS_DIR.
				if err := bm.stegoReconciler.ReconcileStego(ctx, stegoHash, stegoHash); err != nil {
					log.Printf("oracle reconcile: stego reconcile from on-chain hash failed for %s: %v", contractID, err)
				}
			}
		} else {
			log.Printf("oracle reconcile: stego image %s not yet on disk for %s (will arrive via mirror)", stegoHash, contractID)
		}
	}
}

func txidImageFilename(txid, fallback string) string {
	ext := filepath.Ext(fallback)
	if ext == "" {
		// Don't assume file type - preserve original or use no extension
		return txid
	}
	return fmt.Sprintf("%s%s", txid, ext)
}

func blockImageFilename(rec *services.IngestionRecord, fallbackTxid string) string {
	if rec != nil && rec.Metadata != nil {
		cid := strings.TrimSpace(stringFromAny(rec.Metadata["stego_image_cid"]))
		if cid == "" {
			cid = strings.TrimSpace(stringFromAny(rec.Metadata["ipfs_image_cid"]))
		}
		if cid == "" {
			cid = strings.TrimSpace(stringFromAny(rec.Metadata["stego_cid"]))
		}
		if cid != "" {
			return cid
		}
	}
	if strings.TrimSpace(fallbackTxid) != "" {
		return txidImageFilename(fallbackTxid, rec.Filename)
	}
	return strings.TrimSpace(rec.Filename)
}

func mergeIngestionMetadata(target map[string]any, meta map[string]interface{}) {
	if len(meta) == 0 {
		return
	}
	for key, value := range meta {
		if _, exists := target[key]; exists {
			continue
		}
		target[key] = value
	}
}

func applyStegoMetadata(target map[string]any, meta map[string]interface{}) {
	if len(target) == 0 || len(meta) == 0 {
		return
	}
	if !hasStegoMetadata(meta) {
		return
	}
	if _, exists := target["is_stego"]; !exists {
		target["is_stego"] = true
	}
	if _, exists := target["stego_probability"]; !exists {
		target["stego_probability"] = 1.0
	}
	if _, exists := target["stego_type"]; !exists {
		if method := strings.TrimSpace(stringFromAny(meta["stego_method"])); method != "" {
			target["stego_type"] = method
		} else {
			target["stego_type"] = "stego"
		}
	}
	if _, exists := target["extracted_message"]; !exists {
		if manifest := strings.TrimSpace(stringFromAny(meta["stego_manifest_yaml"])); manifest != "" {
			target["extracted_message"] = manifest
		}
	}
}

func hasStegoMetadata(meta map[string]interface{}) bool {
	if meta == nil {
		return false
	}
	keys := []string{
		"stego_image_cid",
		"stego_payload_cid",
		"stego_manifest_yaml",
		"stego_manifest_proposal_id",
		"stego_contract_id",
	}
	for _, key := range keys {
		if strings.TrimSpace(stringFromAny(meta[key])) != "" {
			return true
		}
	}
	return false
}

func (bm *BlockMonitor) markIngestionConfirmed(rec *services.IngestionRecord, txid string, height int64, imageFile, imagePath string) {
	if bm.ingestion == nil || rec == nil {
		return
	}
	updates := map[string]interface{}{
		"confirmed_txid":   txid,
		"confirmed_height": height,
		"image_file":       imageFile,
		"image_path":       imagePath,
	}
	if meta := rec.Metadata; meta != nil {
		if prevHeight, ok := meta["confirmed_height"].(float64); ok && int64(prevHeight) != height {
			updates["reorg_from_height"] = int64(prevHeight)
		} else if prevHeightStr, ok := meta["confirmed_height"].(string); ok {
			if prevHeightInt, err := strconv.ParseInt(strings.TrimSpace(prevHeightStr), 10, 64); err == nil && prevHeightInt != height {
				updates["reorg_from_height"] = prevHeightInt
			}
		}
		if prevTxid, ok := meta["confirmed_txid"].(string); ok && strings.TrimSpace(prevTxid) != "" && strings.TrimSpace(prevTxid) != txid {
			updates["reorg_from_txid"] = prevTxid
		}
	}
	if err := bm.ingestion.UpdateMetadata(rec.ID, updates); err != nil {
		log.Printf("oracle reconcile: failed to update ingestion metadata for %s: %v", rec.ID, err)
	}
	if err := bm.ingestion.UpdateStatusWithNote(rec.ID, "confirmed", fmt.Sprintf("confirmed in block %d", height)); err != nil {
		log.Printf("oracle reconcile: failed to update ingestion status for %s: %v", rec.ID, err)
	}

	if bm.sweepStore != nil {
		// Use ingestion ID directly to match contract creation logic in reconcileOracleIngestions
		contractID := strings.TrimSpace(rec.ID)
		if contractID != "" {
			if err := bm.sweepStore.ConfirmContract(context.Background(), contractID, int(height), txid); err != nil {
				log.Printf("oracle reconcile: failed to confirm contract %s: %v", contractID, err)
			} else {
				log.Printf("oracle reconcile: successfully confirmed contract %s with stego_image_url calculated by storage layer", contractID)
			}
		}
	}
}

func contractIDFromIngestion(rec *services.IngestionRecord) string {
	if rec == nil {
		return ""
	}
	if meta := rec.Metadata; meta != nil {
		if contractID, ok := meta["contract_id"].(string); ok {
			if trimmed := strings.TrimSpace(contractID); trimmed != "" {
				return trimmed
			}
		}
		if visibleHash, ok := meta["visible_pixel_hash"].(string); ok {
			if trimmed := strings.TrimSpace(visibleHash); trimmed != "" {
				return trimmed
			}
		}
	}
	return strings.TrimSpace(rec.ID)
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func visiblePixelHash(imageBytes []byte, message string) string {
	if len(imageBytes) == 0 || message == "" {
		return ""
	}
	sum := sha256.Sum256(imageBytes)
	return fmt.Sprintf("%x", sum[:])
}

func normalizeHex(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "0x")
	return strings.ToLower(value)
}

func scriptHashHex(script []byte) string {
	if len(script) == 0 {
		return ""
	}
	sum := sha256.Sum256(script)
	return hex.EncodeToString(sum[:])
}

func scriptHash160Hex(script []byte) string {
	if len(script) == 0 {
		return ""
	}
	return hex.EncodeToString(btcutil.Hash160(script))
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}

func intFromAny(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return int(parsed), true
		}
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func confidenceFromAny(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case json.Number:
		if parsed, err := v.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return parsed
		}
	}
	return 0.0
}

func buildContractMetadata(result map[string]any) map[string]any {
	return map[string]any{
		"tx_id":             result["tx_id"],
		"image_index":       result["image_index"],
		"stego_type":        result["stego_type"],
		"extracted_message": result["extracted_message"],
		"scan_confidence":   result["confidence"],
		"scan_timestamp":    result["scanned_at"],
		"format":            result["format"],
		"size_bytes":        result["size_bytes"],
	}
}

func updateContractEntry(contracts []SmartContractData, result map[string]any, updated SmartContractData) bool {
	txID := stringFromAny(result["tx_id"])
	imageIndex, hasIndex := intFromAny(result["image_index"])
	for i := range contracts {
		if contracts[i].Metadata == nil {
			continue
		}
		metaTx := stringFromAny(contracts[i].Metadata["tx_id"])
		if metaTx == "" || metaTx != txID {
			continue
		}
		if hasIndex {
			if metaIndex, ok := intFromAny(contracts[i].Metadata["image_index"]); ok && metaIndex != imageIndex {
				continue
			}
		}
		if contracts[i].Metadata == nil {
			contracts[i].Metadata = map[string]any{}
		}
		if updated.Metadata != nil {
			contracts[i].Metadata = updated.Metadata
		}
		contracts[i].ContractID = updated.ContractID
		contracts[i].BlockHeight = updated.BlockHeight
		contracts[i].ImagePath = updated.ImagePath
		if updated.Confidence > 0 {
			contracts[i].Confidence = updated.Confidence
		}
		return true
	}
	return false
}

func upsertContractByID(contracts []SmartContractData, updated SmartContractData) []SmartContractData {
	for i := range contracts {
		if contracts[i].ContractID == updated.ContractID {
			contracts[i].BlockHeight = updated.BlockHeight
			contracts[i].ImagePath = updated.ImagePath
			if updated.Metadata != nil {
				contracts[i].Metadata = updated.Metadata
			}
			if updated.Confidence > 0 {
				contracts[i].Confidence = updated.Confidence
			}
			return contracts
		}
	}
	return append(contracts, updated)
}

// GetBlockInscriptions retrieves inscriptions for a specific block height
func (bm *BlockMonitor) GetBlockInscriptions(height int64) (*BlockInscriptionsResponse, error) {
	// First, try to find existing block data
	blockDir, err := bm.findBlockDirectory(height)
	if err != nil {
		return &BlockInscriptionsResponse{
			BlockHeight: height,
			Success:     false,
			Error:       "Block not found",
		}, nil
	}

	// Read inscriptions.json
	inscriptionsFile := filepath.Join(blockDir, "inscriptions.json")
	data, err := os.ReadFile(inscriptionsFile)
	if err != nil {
		return &BlockInscriptionsResponse{
			BlockHeight: height,
			Success:     false,
			Error:       "Inscriptions data not found",
		}, nil
	}

	var response BlockInscriptionsResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return &BlockInscriptionsResponse{
			BlockHeight: height,
			Success:     false,
			Error:       "Failed to parse inscriptions data",
		}, nil
	}

	return &response, nil
}

// findBlockDirectory finds the directory for a given block height
func (bm *BlockMonitor) findBlockDirectory(height int64) (string, error) {
	entries, err := os.ReadDir(bm.blocksDir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Extract height from directory name (format: height_hash)
			parts := strings.Split(entry.Name(), "_")
			if len(parts) >= 1 {
				if dirHeight, err := strconv.ParseInt(parts[0], 10, 64); err == nil && dirHeight == height {
					return filepath.Join(bm.blocksDir, entry.Name()), nil
				}
			}
		}
	}

	return "", fmt.Errorf("block directory not found for height %d", height)
}
