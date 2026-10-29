package bitcoin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	btcaddr "github.com/btcsuite/btcd/address/v2"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/txscript/v2"

	"stargate-backend/core"
	"stargate-backend/core/identity"
	"stargate-backend/security"
	"stargate-backend/services"
	"stargate-backend/storage/datadir"
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

// OP_PUSHDATA1

// OP_PUSHDATA2

// OP_PUSHDATA4

// stripNonPrintablePrefixLocal trims leading control bytes to get to the printable payload.

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

// 5 minute timeout for large blocks

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
// The expected formats are a single data push:
//
//	OP_RETURN <64 bytes: wish_hash(32) || stego_hash(32)>
//	OP_RETURN <32 bytes: wish_hash only>
//
// Both halves must look like raw SHA-256 digests. Text protocols that also
// use a 64-byte OP_RETURN (CRC-20 JSON, Omni, ASCII hex) are rejected, as
// are concatenated pushes and a payload whose two halves are identical.
// The sandbox_hash is not on-chain — it lives inside the stego v2 JSON
// payload.  Peers find the stego image by its on-chain hash, extract the
// payload, and read sandbox_hash from there.
func parseOPReturnHashes(script []byte) (string, string, bool) {
	payload, ok := opReturnSinglePush(script)
	if !ok || !commitmentBytesOK(payload) {
		return "", "", false
	}
	switch len(payload) {
	case 64: // wish_hash(32) || stego_hash(32)
		return hex.EncodeToString(payload[:32]), hex.EncodeToString(payload[32:64]), true
	case 32: // wish_hash only
		return hex.EncodeToString(payload), "", true
	default:
		return "", "", false
	}
}

// opReturnSinglePush returns the data of an OP_RETURN that is exactly one
// push. Extra pushes and non-push opcodes (Rune OP_13, for example) are
// rejected so unrelated scripts cannot be glued into a 64-byte payload.
func opReturnSinglePush(script []byte) ([]byte, bool) {
	if len(script) < 2 || script[0] != txscript.OP_RETURN {
		return nil, false
	}
	tokenizer := txscript.MakeScriptTokenizer(0, script[1:])
	var data []byte
	pushes := 0
	for tokenizer.Next() {
		if tokenizer.Opcode() > txscript.OP_PUSHDATA4 {
			return nil, false
		}
		pushes++
		data = append(data, tokenizer.Data()...)
	}
	if tokenizer.Err() != nil || pushes != 1 {
		return nil, false
	}
	return data, true
}

// commitmentBytesOK reports whether payload is one or two raw digest halves.
// A Stargate commitment is wish_hash||stego_hash, each a SHA-256 of image
// bytes. Foreign 64-byte OP_RETURNs are usually text, ASCII-hex, zero-padded,
// or the same 32 bytes twice.
func commitmentBytesOK(payload []byte) bool {
	switch len(payload) {
	case 32:
		return rawDigestHalf(payload)
	case 64:
		if bytes.Equal(payload[:32], payload[32:]) {
			return false
		}
		if !rawDigestHalf(payload[:32]) || !rawDigestHalf(payload[32:]) {
			return false
		}
		return printableRatio(payload) <= 0.60
	default:
		return false
	}
}

func rawDigestHalf(b []byte) bool {
	if len(b) != 32 {
		return false
	}
	printable := 0
	hexish := true
	for i, c := range b {
		if c >= 0x20 && c < 0x7f {
			printable++
		}
		if !isASCIIHex(c) {
			hexish = false
		}
		if i+4 <= len(b) && b[i] == 0 && b[i+1] == 0 && b[i+2] == 0 && b[i+3] == 0 {
			return false
		}
	}
	if hexish || printable*4 > len(b)*3 { // more than 3/4 printable
		return false
	}
	for _, magic := range [][]byte{
		[]byte("{"),
		[]byte("omni"),
		[]byte("ord"),
		[]byte("BONO"),
		[]byte("VIA_"),
		[]byte("=:"),
	} {
		if bytes.HasPrefix(b, magic) {
			return false
		}
	}
	return true
}

func isASCIIHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func printableRatio(b []byte) float64 {
	if len(b) == 0 {
		return 0
	}
	n := 0
	for _, c := range b {
		if c >= 0x20 && c < 0x7f {
			n++
		}
	}
	return float64(n) / float64(len(b))
}

// DropForeignOPReturnContracts removes smart-contract rows that are not a
// Stargate commitment. Text OP_RETURNs fail the hash-pair check. A raw
// wish_hash||stego_hash pair is also dropped unless UPLOADS_DIR holds the
// wish image; that pair alone is generic pay-to-anchor. Rows matched by
// funding_txid or a witness hash are left alone.
func DropForeignOPReturnContracts(contracts []SmartContractData) (kept []SmartContractData, dropped int) {
	if len(contracts) == 0 {
		return contracts, 0
	}
	kept = make([]SmartContractData, 0, len(contracts))
	for _, contract := range contracts {
		if foreignOPReturnContract(contract) {
			dropped++
			continue
		}
		kept = append(kept, contract)
	}
	if dropped == 0 {
		return contracts, 0
	}
	return kept, dropped
}

func foreignOPReturnContract(contract SmartContractData) bool {
	matchType := ""
	if contract.Metadata != nil {
		matchType = strings.ToLower(strings.TrimSpace(stringFromAny(contract.Metadata["match_type"])))
	}
	if matchType != "" && matchType != "op_return" {
		return false
	}
	// The wish file is the contract. A 64-byte pair and a 32-byte wish-only
	// anchor use the same rule. A 32-byte OP_RETURN with no wish file used to
	// stay because a missing stego half made this check give up and keep it.
	if wish := wishHexFromContract(contract); wishImageOnDisk(wish) {
		return false
	}
	if matchType == "op_return" {
		return true
	}
	// Untyped rows that are not a hash pair stay. A parsed wish||stego pair
	// with no wish file is a generic anchor, including text protocols whose
	// bytes happen to be hex.
	_, _, ok := storedCommitmentHalves(contract)
	return ok
}

// wishHexFromContract is the wish image hash stored on a contract row.
// The contract id is included because a wish-only anchor uses that hash as its id.
func wishHexFromContract(contract SmartContractData) string {
	var candidates []string
	if contract.Metadata != nil {
		candidates = append(candidates,
			stringFromAny(contract.Metadata["visible_pixel_hash"]),
			stringFromAny(contract.Metadata["wish_hash"]),
			stringFromAny(contract.Metadata["pixel_hash"]),
		)
	}
	candidates = append(candidates, contract.ContractID)
	for _, candidate := range candidates {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		candidate = strings.TrimPrefix(candidate, "wish-")
		if _, ok := digestHex(candidate); ok {
			return candidate
		}
	}
	return ""
}

// ContractImageName is the file the block card should show for a contract.
// That file is the wish image when this node has it. A stego hash or an IPFS
// CID is only a fallback for a row whose wish file is not here yet.
func ContractImageName(contract SmartContractData) string {
	if wish := wishHexFromContract(contract); wishImageOnDisk(wish) {
		return wish
	}
	if contract.Metadata != nil {
		if name := strings.TrimSpace(stringFromAny(contract.Metadata["image_file"])); name != "" && name != "." {
			return filepath.Base(name)
		}
	}
	base := filepath.Base(strings.TrimSpace(contract.ImagePath))
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

func storedCommitmentHalves(contract SmartContractData) (wish, stego []byte, ok bool) {
	var wishHex, stegoHex string
	if contract.Metadata != nil {
		wishHex = stringFromAny(contract.Metadata["wish_hash"])
		if wishHex == "" {
			wishHex = stringFromAny(contract.Metadata["visible_pixel_hash"])
		}
		stegoHex = stringFromAny(contract.Metadata["stego_hash"])
		if stegoHex == "" {
			stegoHex = stringFromAny(contract.Metadata["image_file"])
		}
	}
	if wishHex == "" {
		wishHex = contract.ContractID
	}
	if stegoHex == "" {
		stegoHex = contract.ImagePath
	}
	wish, okW := digestHex(wishHex)
	stego, okS := digestHex(filepath.Base(stegoHex))
	if !okS {
		stego, okS = digestHex(stegoHex)
	}
	if !okW || !okS {
		return nil, nil, false
	}
	return wish, stego, true
}

func digestHex(s string) ([]byte, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "wish-")
	if len(s) != 64 {
		return nil, false
	}
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) != 32 {
		return nil, false
	}
	return raw, true
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
	addr, err := DecodeAddressForNetwork(address, params)
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
	return NetworkParams(bm.bitcoinClient.GetNetwork())
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
		// Try partitioned and flat hash-only locations.
		hashPath := datadir.PartResolve(uploadsDir, rec.ID)
		if info, err := os.Stat(hashPath); err == nil && !info.IsDir() {
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
	if sourcePath == "" && rec.Metadata != nil {
		// Older funding txs have no OP_RETURN. The picture is named by the
		// wish hash in metadata, not by the ingestion id or a stego CID.
		for _, key := range []string{"visible_pixel_hash", "wish_hash", "pixel_hash"} {
			hash := strings.ToLower(strings.TrimSpace(stringFromAny(rec.Metadata[key])))
			hash = strings.TrimPrefix(hash, "wish-")
			if _, ok := digestHex(hash); !ok {
				continue
			}
			hashPath := datadir.PartResolve(uploadsDir, hash)
			info, err := os.Stat(hashPath)
			if err == nil && !info.IsDir() {
				sourcePath = hashPath
				if strings.TrimSpace(destFilename) == "" || !strings.EqualFold(destFilename, hash) {
					destFilename = hash
					destPath = security.SafeFilePath(destDir, destFilename)
				}
				break
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
	// Try partitioned and flat hash-only locations.
	hashPath := datadir.PartResolve(uploadsDir, stegoCID)
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

// Try partitioned and flat hash-only locations.

// Also cleanup old pattern files

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

// errStegoNotOnDisk means the OP_RETURN stego hash is not in UPLOADS_DIR yet.
// The commitment stays pending until the IPFS mirror writes that file.
var errStegoNotOnDisk = errors.New("stego image not on disk")

func uploadsDirForStego() string {
	uploadsDir := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
	if uploadsDir == "" {
		return "data/uploads"
	}
	return uploadsDir
}

func stegoFileOnDisk(stegoHash string) bool {
	return uploadHashOnDisk(stegoHash)
}

// wishImageOnDisk reports whether UPLOADS_DIR holds the wish image for hash.
// The file is named by the wish hash (visible pixel hash), partitioned or flat.
// An OP_RETURN hash pair without that file is not a Stargate smart contract:
// other software uses the same pay-to-anchor shape.
func wishImageOnDisk(wishHash string) bool {
	return uploadHashOnDisk(wishHash)
}

// placeWishImage copies UPLOADS_DIR/<wish_hash> into the block's images
// directory under that same hash. The card loads this file, not the stego hash.
func placeWishImage(blockDir, wishHash string) (imageFile, imagePath string, err error) {
	wishHash = strings.ToLower(strings.TrimSpace(wishHash))
	wishHash = strings.TrimPrefix(wishHash, "wish-")
	if !wishImageOnDisk(wishHash) {
		return "", "", fmt.Errorf("wish image %s is not in uploads", wishHash)
	}
	src := datadir.PartResolve(uploadsDirForStego(), wishHash)
	destDir := filepath.Join(blockDir, "images")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", "", err
	}
	dest := filepath.Join(destDir, wishHash)
	if _, statErr := os.Stat(dest); statErr != nil {
		if err := copyFile(src, dest); err != nil {
			return "", "", err
		}
	}
	return wishHash, filepath.Join("images", wishHash), nil
}

func uploadHashOnDisk(hash string) bool {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if len(hash) != 64 {
		return false
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return false
	}
	info, err := os.Stat(datadir.PartResolve(uploadsDirForStego(), hash))
	return err == nil && info != nil && !info.IsDir()
}

func countWishImagesOnDisk(pending []chainCommitment) int {
	n := 0
	for _, c := range pending {
		if wishImageOnDisk(c.WishHash) {
			n++
		}
	}
	return n
}

// reconcileOnChainArtifacts uses the stego_hash from the OP_RETURN to find
// the stego image in UPLOADS_DIR and apply the v2 payload (proposal, tasks,
// sandbox_hash). Metadata only — it does not unpack the sandbox tarball.
// This is the primary replication path — no pubsub or
// STARGATE_STEGO_APPROVAL_ENABLED needed.
func (bm *BlockMonitor) reconcileOnChainArtifacts(contractID, stegoHash string) error {
	if contractID == "" || strings.TrimSpace(stegoHash) == "" {
		return fmt.Errorf("missing contract or stego hash")
	}
	if !stegoFileOnDisk(stegoHash) {
		log.Printf("oracle reconcile: stego image %s not yet on disk for %s (will retry when the mirror delivers it)", stegoHash, contractID)
		return errStegoNotOnDisk
	}
	stegoPath := datadir.PartResolve(uploadsDirForStego(), stegoHash)
	log.Printf("oracle reconcile: found stego image on disk for %s at %s, reconciling", contractID, stegoPath)
	if bm.stegoReconciler == nil {
		return fmt.Errorf("no stego reconciler")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	// Pass stegoHash as both CID and expected hash. ReconcileStego treats a
	// 64-hex id as UPLOADS_DIR/<hash>, which is where the mirror writes it.
	if err := bm.stegoReconciler.ReconcileStego(ctx, stegoHash, stegoHash); err != nil {
		log.Printf("oracle reconcile: stego reconcile from on-chain hash failed for %s: %v", contractID, err)
		return err
	}
	return nil
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
		if prevHeight, ok := meta["confirmed_height"].(float64); ok && int64(prevHeight) > 0 {
			if int64(prevHeight) > height {
				// Do not rewrite a newer confirm down to a historical catch-up height.
				delete(updates, "confirmed_height")
			} else if int64(prevHeight) != height {
				updates["reorg_from_height"] = int64(prevHeight)
			}
		} else if prevHeightStr, ok := meta["confirmed_height"].(string); ok {
			if prevHeightInt, err := strconv.ParseInt(strings.TrimSpace(prevHeightStr), 10, 64); err == nil && prevHeightInt > 0 {
				if prevHeightInt > height {
					delete(updates, "confirmed_height")
				} else if prevHeightInt != height {
					updates["reorg_from_height"] = prevHeightInt
				}
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
		// Canonical stored id is the bare VPH. Confirm adopts a leftover wish-
		// row onto that PK rather than minting a prefix twin.
		contractID := strings.TrimSpace(rec.ID)
		if vph := strings.TrimSpace(stringFromAny(rec.Metadata["visible_pixel_hash"])); vph != "" {
			contractID = vph
		}
		if contractID != "" {
			if identity.IsPixelHash(identity.Normalize(contractID)) {
				contractID = identity.CanonicalContractID(contractID)
			}
			if !bm.scanMayConfirm(height) {
				log.Printf("oracle reconcile: contract %s seen in block %d — waiting for %d confirmations before ConfirmContract",
					contractID, height, SettlementConfirmations())
			} else if err := bm.confirmContractOnChain(context.Background(), contractID, int(height), txid); err != nil {
				log.Printf("oracle reconcile: failed to confirm contract %s: %v", contractID, err)
			} else {
				log.Printf("oracle reconcile: successfully confirmed contract %s with stego_image_url calculated by storage layer", contractID)
			}
		}
	}
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
	return hex.EncodeToString(btcaddr.Hash160(script))
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

// First, try to find existing block data

// Read inscriptions.json

// findBlockDirectory finds the directory for a given block height using
// the partitioned-aware FindBlockDir (no more full root scan).
func (bm *BlockMonitor) findBlockDirectory(height int64) (string, error) {
	return FindBlockDir(bm.blocksDir, height)
}
