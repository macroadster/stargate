package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"stargate-backend/bitcoin"
	"stargate-backend/security"
)

func (api *DataAPI) sniffContentType(height int64, filePath string) string {
	if strings.TrimSpace(filePath) == "" {
		return ""
	}
	base := strings.TrimRight(api.resolveBlocksDir(), "/")
	fsPath := filepath.Join(fmt.Sprintf("%s/%d_00000000", base, height), filePath)
	file, err := os.Open(fsPath)
	if err != nil {
		return ""
	}
	defer file.Close()
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	if n <= 0 {
		return ""
	}
	if detected := http.DetectContentType(buf[:n]); detected != "" {
		return detected
	}
	return ""
}

func buildContractInscriptions(contracts []bitcoin.SmartContractData, height int64) ([]bitcoin.InscriptionData, map[int]string, map[int]map[string]any) {
	var out []bitcoin.InscriptionData
	imageURLs := map[int]string{}
	metadata := map[int]map[string]any{}
	firstFundingTxID := func(meta map[string]any) string {
		if meta == nil {
			return ""
		}
		if v := strings.TrimSpace(stringFromAny(meta["funding_txid"])); v != "" {
			return v
		}
		switch values := meta["funding_txids"].(type) {
		case []string:
			if len(values) > 0 {
				return strings.TrimSpace(values[0])
			}
		case []any:
			for _, item := range values {
				if txid, ok := item.(string); ok && strings.TrimSpace(txid) != "" {
					return strings.TrimSpace(txid)
				}
			}
		}
		return ""
	}

	for _, contract := range contracts {
		if isSyntheticStegoContract(contract) {
			continue
		}
		meta := contract.Metadata
		fileName := strings.TrimSpace(stringFromAny(meta["image_file"]))
		if fileName == "" {
			fileName = filepath.Base(strings.TrimSpace(contract.ImagePath))
		}
		if fileName == "" {
			continue
		}
		filePath := strings.TrimSpace(contract.ImagePath)
		if filePath == "" {
			filePath = filepath.Join("images", fileName)
		}

		txID := strings.TrimSpace(stringFromAny(meta["tx_id"]))
		if !isLikelyTxID(txID) {
			if candidate := strings.TrimSpace(stringFromAny(meta["confirmed_txid"])); isLikelyTxID(candidate) {
				txID = candidate
			} else if candidate := strings.TrimSpace(stringFromAny(meta["funding_txid"])); isLikelyTxID(candidate) {
				txID = candidate
			} else if candidate := firstFundingTxID(meta); isLikelyTxID(candidate) {
				txID = candidate
			} else if candidate := strings.TrimSpace(stringFromAny(meta["match_hash"])); isLikelyTxID(candidate) {
				txID = candidate
			} else if isLikelyTxID(contract.ContractID) {
				txID = strings.TrimSpace(contract.ContractID)
			}
		}
		if !isLikelyTxID(txID) {
			lowerFile := strings.ToLower(fileName)
			lowerTx := strings.ToLower(txID)
			if strings.HasPrefix(lowerFile, "unknown_") || strings.HasSuffix(lowerFile, ".png") || strings.Contains(lowerTx, ".png") {
				continue
			}
		}
		inputIndex := 0
		if idx, ok := intFromAny(meta["input_index"]); ok {
			inputIndex = idx
		} else if idx, ok := intFromAny(meta["output_index"]); ok {
			inputIndex = idx
		}

		contentType := inferMime("", nil, fileName)
		out = append(out, bitcoin.InscriptionData{
			TxID:        txID,
			InputIndex:  inputIndex,
			FileName:    fileName,
			FilePath:    filePath,
			ContentType: contentType,
			SizeBytes:   0,
			Content:     "",
		})

		idx := len(out) - 1
		imageURLs[idx] = fmt.Sprintf("/api/block-image/%d/%s", height, fileName)
		if meta == nil {
			meta = make(map[string]any)
		}
		if contract.ContractID != "" {
			meta["contract_id"] = contract.ContractID
		}
		if len(meta) > 0 {
			metadata[idx] = meta
		}
	}

	return out, imageURLs, metadata
}

func filterSmartContractsForUI(contracts []bitcoin.SmartContractData) []bitcoin.SmartContractData {
	if len(contracts) == 0 {
		return contracts
	}
	out := make([]bitcoin.SmartContractData, 0, len(contracts))
	for _, contract := range contracts {
		if isSyntheticStegoContract(contract) {
			continue
		}
		out = append(out, contract)
	}
	return out
}

// HandleContent routes content requests to raw or manifest responses.
func (api *DataAPI) HandleContent(w http.ResponseWriter, r *http.Request) {
	api.EnableCORS(w, r)
	if r.Method == http.MethodOptions {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/content/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "missing txid", http.StatusBadRequest)
		return
	}
	txid := normalizeTxID(parts[0])
	isManifest := len(parts) > 1 && parts[1] == "manifest"

	if isManifest {
		api.handleContentManifest(w, r, txid)
		return
	}
	api.handleContentRaw(w, r, txid)
}

// handleContentRaw returns raw payload for a txid (optionally by witness).
func (api *DataAPI) handleContentRaw(w http.ResponseWriter, r *http.Request, txid string) {
	witnessParam := r.URL.Query().Get("witness")
	var witnessIndex *int
	if witnessParam != "" {
		if wi, err := strconv.Atoi(witnessParam); err == nil {
			witnessIndex = &wi
		}
	}

	height, insList, err := api.findInscriptionsByTx(txid)
	if err != nil || len(insList) == 0 {
		if height, filePath, ok := api.findContractImageByTx(txid); ok {
			api.serveBlockImage(w, height, filePath)
			return
		}
		http.Error(w, "inscription not found", http.StatusNotFound)
		return
	}

	inscription := insList[0]
	if witnessIndex != nil {
		for _, ins := range insList {
			if ins.InputIndex == *witnessIndex {
				inscription = ins
				break
			}
		}
	} else {
		// No explicit witness requested: prefer an image-like part if this tx
		// has mixed content (text + image). This makes bare /content/{tx} (as
		// used for some block thumbnails and direct links) return a renderable
		// image when one is present in the tx's witnesses.
		if img := pickImageLikeInscription(insList); img != nil {
			inscription = *img
		}
	}

	content, mimeType := api.loadInscriptionContent(height, inscription)
	log.Printf("content served tx=%s len=%d first8=%x", txid, len(content), func() []byte {
		if len(content) >= 8 {
			return content[:8]
		}
		return content
	}())
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
	w.Header().Set("X-Inscription-Mime", mimeType)
	w.Header().Set("X-Inscription-Size", fmt.Sprintf("%d", len(content)))
	w.Header().Set("X-Inscription-Hash", sha256Hex(content))
	w.Write(content)
}

// handleContentManifest returns a JSON manifest of all inscription parts for a txid.
func (api *DataAPI) handleContentManifest(w http.ResponseWriter, r *http.Request, txid string) {
	height, insList, err := api.findInscriptionsByTx(txid)
	if err != nil {
		http.Error(w, "inscription not found", http.StatusNotFound)
		return
	}

	parts := []map[string]interface{}{}
	for _, ins := range insList {
		content, mimeType := api.loadInscriptionContent(height, ins)
		parts = append(parts, map[string]interface{}{
			"witness_index": ins.InputIndex,
			"size_bytes":    len(content),
			"mime_type":     mimeType,
			"hash":          sha256Hex(content),
			"primary":       ins.InputIndex == insList[0].InputIndex,
			"url":           fmt.Sprintf("/content/%s?witness=%d", txid, ins.InputIndex),
		})
	}

	resp := map[string]interface{}{
		"tx_id":        txid,
		"block_height": height,
		"parts":        parts,
		"stitch_hint":  "unknown",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// findInscriptionsByTx scans known blocks to locate a txid.
func (api *DataAPI) findInscriptionsByTx(txid string) (int64, []bitcoin.InscriptionData, error) {
	if height, ok := api.lookupTxHeight(txid); ok {
		if block, err := api.loadBlock(height); err == nil {
			inscriptions := block.Inscriptions
			if len(inscriptions) == 0 && len(block.SmartContracts) > 0 {
				inscriptions, _, _ = buildContractInscriptions(block.SmartContracts, block.BlockHeight)
			}
			var hits []bitcoin.InscriptionData
			for _, ins := range inscriptions {
				if normalizeTxID(ins.TxID) == txid {
					hits = append(hits, ins)
				}
			}
			if len(hits) > 0 {
				return height, hits, nil
			}
		}
	}

	heights := api.listAvailableBlockHeights()
	for _, h := range heights {
		block, err := api.loadBlock(h)
		if err != nil {
			continue
		}
		inscriptions := block.Inscriptions
		if len(inscriptions) == 0 && len(block.SmartContracts) > 0 {
			inscriptions, _, _ = buildContractInscriptions(block.SmartContracts, block.BlockHeight)
		}
		var hits []bitcoin.InscriptionData
		for _, ins := range inscriptions {
			if normalizeTxID(ins.TxID) == txid {
				hits = append(hits, ins)
			}
		}
		if len(hits) > 0 {
			api.txMu.Lock()
			api.txIndex[txid] = h
			api.heightIndex[h] = appendIfNotPresent(api.heightIndex[h], txid)
			api.txMu.Unlock()
			return h, hits, nil
		}
	}
	return 0, nil, fmt.Errorf("not found")
}

func (api *DataAPI) findContractImageByTx(txid string) (int64, string, bool) {
	txid = normalizeTxID(txid)
	if txid == "" {
		return 0, "", false
	}
	heights := api.listAvailableBlockHeights()
	for _, height := range heights {
		block, err := api.loadBlock(height)
		if err != nil {
			continue
		}
		for _, contract := range block.SmartContracts {
			meta := contract.Metadata
			if !contractMatchesTx(meta, txid) {
				continue
			}
			filePath := strings.TrimSpace(contract.ImagePath)
			if filePath == "" {
				filePath = filepath.Join("images", filepath.Base(strings.TrimSpace(stringFromAny(meta["image_file"]))))
			}
			if strings.TrimSpace(filePath) != "" {
				return height, filePath, true
			}
		}
	}
	return 0, "", false
}

func contractMatchesTx(meta map[string]any, txid string) bool {
	if meta == nil || txid == "" {
		return false
	}
	candidates := []string{
		stringFromAny(meta["tx_id"]),
		stringFromAny(meta["confirmed_txid"]),
		stringFromAny(meta["funding_txid"]),
		stringFromAny(meta["match_hash"]),
	}
	switch values := meta["funding_txids"].(type) {
	case []string:
		candidates = append(candidates, values...)
	case []any:
		for _, item := range values {
			if v, ok := item.(string); ok {
				candidates = append(candidates, v)
			}
		}
	case string:
		candidates = append(candidates, strings.Split(values, ",")...)
	}
	for _, candidate := range candidates {
		if normalizeTxID(candidate) == txid {
			return true
		}
	}
	return false
}

// loadInscriptionContent fetches inscription payload and inferred MIME.
func (api *DataAPI) loadInscriptionContent(height int64, ins bitcoin.InscriptionData) ([]byte, string) {
	content := []byte(ins.Content)
	mimeType := inferMime(ins.ContentType, content, ins.FileName)

	base := strings.TrimRight(api.resolveBlocksDir(), "/")
	blockDir := fmt.Sprintf("%s/%d_00000000", base, height)
	safePath, err := security.SanitizePath(blockDir, ins.FilePath)
	if err == nil {
		if data, err := os.ReadFile(safePath); err == nil {
			// Prefer filesystem copy whenever it exists; it's the source of truth.
			content = data
			mimeType = inferMime(ins.ContentType, content, ins.FileName)
		}
	}

	// If content is base64-encoded (used for binary payloads in DB), decode it.
	if strings.HasPrefix(mimeType, "image/") && len(ins.Content) > 0 && looksBase64(ins.Content) && len(content) == len(ins.Content) {
		if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(ins.Content)); err == nil && len(decoded) > 0 {
			content = decoded
			mimeType = inferMime(ins.ContentType, content, ins.FileName)
		}
	}

	// Attempt to rebuild from pushdatas for text-like payloads only.
	if !strings.HasPrefix(mimeType, "image/") {
		if rebuilt, ok := extractPushPayload(content); ok {
			content = rebuilt
			mimeType = inferMime(ins.ContentType, content, ins.FileName)
		}
	}

	// If this is an image but the payload has leading garbage, trim to the first valid signature.
	if strings.HasPrefix(mimeType, "image/") {
		if trimmed := trimToImageSignature(content); len(trimmed) > 0 {
			content = trimmed
		}
		// Re-evaluate MIME after trimming.
		mimeType = inferMime(ins.ContentType, content, ins.FileName)
	}

	// Trim trailing single 'h' artifact on text payloads.
	if strings.HasPrefix(mimeType, "text/") && len(content) > 0 && content[len(content)-1] == 'h' {
		content = content[:len(content)-1]
	}

	// Strip leading pushdata opcodes that may have been preserved in extraction.
	if strings.HasPrefix(mimeType, "text/") {
		if cleaned := stripPushdataPrefix(content); len(cleaned) > 0 {
			content = cleaned
		}
		if cleaned := stripNonPrintablePrefix(content); len(cleaned) > 0 {
			content = cleaned
		}
	}

	// For HTML payloads, trim any stray metadata bytes that appear before the first tag.
	if strings.HasPrefix(mimeType, "text/html") {
		if idx := bytes.IndexByte(content, '<'); idx > 0 {
			content = content[idx:]
		}
	}

	return content, mimeType
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// inferMime attempts to produce a better content type when missing or generic.
func inferMime(current string, content []byte, fileName string) string {
	m := strings.ToLower(strings.TrimSpace(current))
	if m == "image/txt" {
		m = "text/plain"
	}
	if m == "image/avif" {
		return "image/avif"
	}
	if m == "image/svg" {
		m = "image/svg+xml"
	}

	// Normalize bare format strings (e.g. "jpeg", "png") that some extractors
	// store directly in ContentType instead of a full MIME type. This ensures
	// the block-inscriptions API and cards always see proper "image/..." types
	// so that "hide text" filtering keeps real images and type displays aren't "unknown".
	switch m {
	case "jpeg", "jpg":
		m = "image/jpeg"
	case "png":
		m = "image/png"
	case "gif":
		m = "image/gif"
	case "webp":
		m = "image/webp"
	case "avif":
		m = "image/avif"
	case "svg":
		m = "image/svg+xml"
	case "bmp":
		m = "image/bmp"
	}

	// Enhanced content detection for files without extensions
	if len(content) > 0 && (m == "" || m == "application/octet-stream") {
		sample := content
		if len(sample) > 512 {
			sample = sample[:512]
		}
		if detected := http.DetectContentType(sample); detected != "" && detected != "application/octet-stream" {
			m = detected
		}
	}

	// Prefer explicit image types by filename if type is missing or generic.
	if m == "" || m == "application/octet-stream" {
		lowerName := strings.ToLower(fileName)
		switch {
		case strings.HasSuffix(lowerName, ".avif"):
			m = "image/avif"
		case strings.HasSuffix(lowerName, ".jpg"), strings.HasSuffix(lowerName, ".jpeg"):
			m = "image/jpeg"
		case strings.HasSuffix(lowerName, ".png"):
			m = "image/png"
		case strings.HasSuffix(lowerName, ".gif"):
			m = "image/gif"
		case strings.HasSuffix(lowerName, ".webp"):
			m = "image/webp"
		case strings.HasSuffix(lowerName, ".svg"):
			m = "image/svg+xml"
		case strings.HasSuffix(lowerName, ".bmp"):
			m = "image/bmp"
		case strings.HasSuffix(lowerName, ".html"), strings.HasSuffix(lowerName, ".htm"):
			m = "text/html"
		case strings.HasSuffix(lowerName, ".json"):
			m = "application/json"
		case strings.HasSuffix(lowerName, ".js"):
			m = "text/javascript"
		case strings.HasSuffix(lowerName, ".css"):
			m = "text/css"
		}
	}

	if m == "" {
		trim := strings.TrimSpace(string(content))
		lower := strings.ToLower(trim)
		if strings.HasPrefix(lower, "<!doctype") || strings.HasPrefix(lower, "<html") {
			m = "text/html"
		} else if strings.HasPrefix(lower, "<svg") {
			m = "image/svg+xml"
		} else if json.Valid(content) {
			m = "application/json"
		} else if isMostlyPrintable(trim) {
			m = "text/plain"
		} else {
			m = "application/octet-stream"
		}
	}
	return m
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		return v.String()
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
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
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func isAVIF(b []byte) bool {
	if len(b) < 12 {
		return false
	}
	// ISO BMFF: size(4) 'ftyp' then brand.
	if b[4] == 'f' && b[5] == 't' && b[6] == 'y' && b[7] == 'p' {
		brand := string(b[8:12])
		if brand == "avif" || brand == "avis" {
			return true
		}
	}
	// Search for ftypavif within the first 64 bytes.
	if idx := bytes.Index(b, []byte("ftypavif")); idx >= 0 && idx < 64 {
		return true
	}
	return false
}

// extractPushPayload rebuilds a script blob by concatenating its pushdatas in order.
// Non-push opcodes are discarded; if parsing fails, the original buffer is returned.
func extractPushPayload(script []byte) ([]byte, bool) {
	ops, ok := parseScriptOpsLocal(script)
	if !ok || len(ops) == 0 {
		return script, false
	}
	var out bytes.Buffer
	pushes := 0
	for _, op := range ops {
		if op.isPush {
			out.Write(op.data)
			pushes++
		}
	}
	if pushes == 0 {
		return script, false
	}

	total := 0
	for _, op := range ops {
		if op.isPush {
			total += len(op.data)
		}
	}
	// Require that total pushed bytes are at least as large as the script; otherwise
	// we likely mis-parsed plain text and should preserve the original.
	if total < len(script) && total < 256 { // tiny total suggests bad parse
		return script, false
	}

	return out.Bytes(), true
}

// looksBase64 returns true if the string is plausibly base64-encoded.
func looksBase64(s string) bool {
	trim := strings.TrimSpace(s)
	if len(trim) == 0 || len(trim)%4 != 0 {
		return false
	}
	for i := 0; i < len(trim); i++ {
		c := trim[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=') {
			return false
		}
	}
	return true
}

// trimToImageSignature scans for known image headers and slices from the first match.
func trimToImageSignature(b []byte) []byte {
	if len(b) < 8 {
		return b
	}
	sigs := [][]byte{
		{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, // PNG
		{0xFF, 0xD8, 0xFF},       // JPEG
		[]byte("RIFF"),           // WEBP (we also check WEBP marker)
		{0x47, 0x49, 0x46, 0x38}, // GIF
		[]byte("ftypavif"),       // AVIF (ISO BMFF)
	}
	for _, sig := range sigs {
		if idx := bytes.Index(b, sig); idx >= 0 {
			// For RIFF/WEBP, ensure WEBP marker exists.
			if sig[0] == 'R' && len(b) >= idx+12 {
				if !(b[idx+8] == 'W' && b[idx+9] == 'E' && b[idx+10] == 'B' && b[idx+11] == 'P') {
					continue
				}
			}
			if string(sig) == "ftypavif" {
				// Ensure we return from the start of the ftyp box.
				if idx >= 4 {
					return b[idx-4:]
				}
			}
			return b[idx:]
		}
	}
	return b
}

// Minimal script parser (duplicate of bitcoin.parseScriptOps) to avoid dependency loops.
// Returns ops and ok==true if the entire script was consumed successfully.
func parseScriptOpsLocal(script []byte) ([]scriptOpLocal, bool) {
	var ops []scriptOpLocal
	i := 0
	for i < len(script) {
		op := script[i]
		i++

		switch {
		case op <= 75:
			l := int(op)
			if l < 0 || i+l > len(script) {
				return ops, false
			}
			ops = append(ops, scriptOpLocal{opcode: op, data: script[i : i+l], isPush: true})
			i += l
		case op == 0x4c: // OP_PUSHDATA1
			if i >= len(script) {
				return ops, false
			}
			l := int(script[i])
			i++
			if l < 0 || i+l > len(script) {
				return ops, false
			}
			ops = append(ops, scriptOpLocal{opcode: op, data: script[i : i+l], isPush: true})
			i += l
		case op == 0x4d: // OP_PUSHDATA2
			if i+1 >= len(script) {
				return ops, false
			}
			l := int(script[i]) | int(script[i+1])<<8
			i += 2
			if l < 0 || i+l > len(script) {
				return ops, false
			}
			ops = append(ops, scriptOpLocal{opcode: op, data: script[i : i+l], isPush: true})
			i += l
		case op == 0x4e: // OP_PUSHDATA4
			if i+3 >= len(script) {
				return ops, false
			}
			l := int(script[i]) | int(script[i+1])<<8 | int(script[i+2])<<16 | int(script[i+3])<<24
			i += 4
			if l < 0 || i+l > len(script) {
				return ops, false
			}
			ops = append(ops, scriptOpLocal{opcode: op, data: script[i : i+l], isPush: true})
			i += l
		default:
			ops = append(ops, scriptOpLocal{opcode: op, isPush: false})
		}
	}
	return ops, true
}

func isMostlyPrintable(s string) bool {
	if s == "" {
		return false
	}
	printable := 0
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			printable++
			continue
		}
		if r >= 32 && r < 127 {
			printable++
		}
	}
	return printable >= len(s)/2
}

// normalizeTxID trims ordinal-style suffixes (e.g., i0) to compare canonical txids.
func normalizeTxID(txid string) string {
	if idx := strings.Index(txid, "i"); idx > 0 {
		if len(txid)-idx <= 4 { // common patterns like i0, i00
			return txid[:idx]
		}
	}
	return txid
}

func isLikelyTxID(txid string) bool {
	txid = strings.TrimSpace(normalizeTxID(strings.ToLower(txid)))
	if len(txid) != 64 {
		return false
	}
	_, err := hex.DecodeString(txid)
	return err == nil
}

// stripPushdataPrefix removes a leading push opcode (OP_PUSH, OP_PUSHDATA1/2/4) from a payload when present.
func stripPushdataPrefix(b []byte) []byte {
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

// stripNonPrintablePrefix trims leading control bytes to get to the printable payload.
func stripNonPrintablePrefix(b []byte) []byte {
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

// pickImageLikeInscription returns the first inscription in the list that looks
// like a renderable image (based on ContentType or FileName extension).
// This ensures the thumbnail_url we put in block-summaries points at something
// that will successfully load as <img> in BlockCard instead of a .txt that
// causes onError and falls back to the pickaxe.
func pickImageLikeInscription(ins []bitcoin.InscriptionData) *bitcoin.InscriptionData {
	if len(ins) == 0 {
		return nil
	}
	for i := range ins {
		if isImageLikeInscription(ins[i]) {
			return &ins[i]
		}
	}
	// No obvious image; fall back to first (may still be non-image, but at least
	// consistent with prior behavior; the card will try <img> and may emoji-fallback).
	return &ins[0]
}

func isImageLikeInscription(ins bitcoin.InscriptionData) bool {
	ct := strings.ToLower(ins.ContentType)
	fn := strings.ToLower(ins.FileName)
	if strings.HasPrefix(ct, "image/") {
		return true
	}
	if strings.HasSuffix(fn, ".png") || strings.HasSuffix(fn, ".jpg") ||
		strings.HasSuffix(fn, ".jpeg") || strings.HasSuffix(fn, ".gif") ||
		strings.HasSuffix(fn, ".webp") || strings.HasSuffix(fn, ".svg") ||
		strings.HasSuffix(fn, ".avif") || strings.HasSuffix(fn, ".bmp") {
		return true
	}
	return false
}

// Helper functions

func (api *DataAPI) verifySignature(secret string, body []byte, signature string) bool {
	if signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(signature)), []byte(strings.ToLower(expected)))
}
