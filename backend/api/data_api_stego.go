package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"stargate-backend/bitcoin"
	"stargate-backend/storage"
)

func isSyntheticStegoContract(contract bitcoin.SmartContractData) bool {
	contractID := strings.ToLower(strings.TrimSpace(contract.ContractID))
	if strings.HasPrefix(contractID, "stego_") {
		return true
	}
	if contract.Metadata == nil {
		return false
	}
	txID := strings.ToLower(strings.TrimSpace(stringFromAny(contract.Metadata["tx_id"])))
	if strings.HasPrefix(txID, "unknown_") {
		return true
	}
	return false
}

// HandleStegoCallback ingests scan results from the Python scanner instead of filesystem writes.
func (api *DataAPI) HandleStegoCallback(w http.ResponseWriter, r *http.Request) {
	api.EnableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != http.MethodPost {
		log.Printf("stego-callback: invalid method %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("stego-callback: read body error: %v", err)
		http.Error(w, "unable to read body", http.StatusBadRequest)
		return
	}

	secret := os.Getenv("STARLIGHT_CALLBACK_SECRET")
	if secret != "" {
		if !api.verifySignature(secret, body, r.Header.Get("X-Starlight-Signature")) {
			log.Printf("stego-callback: signature verification failed")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Detect batch payload (block-level with inscriptions array)
	var batchProbe struct {
		Inscriptions []map[string]interface{} `json:"inscriptions"`
		BlockHeight  int64                    `json:"block_height"`
	}
	if err := json.Unmarshal(body, &batchProbe); err == nil && len(batchProbe.Inscriptions) > 0 && batchProbe.BlockHeight > 0 {
		log.Printf("stego-callback: batch payload height=%d count=%d", batchProbe.BlockHeight, len(batchProbe.Inscriptions))
		if err := api.handleStegoBatch(batchProbe.BlockHeight, body, w); err != nil {
			log.Printf("stego-callback: batch error height=%d: %v", batchProbe.BlockHeight, err)
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	var payload struct {
		RequestID     string                 `json:"request_id"`
		BlockHeight   int64                  `json:"block_height"`
		TxID          string                 `json:"tx_id"`
		FileName      string                 `json:"file_name"`
		ContentType   string                 `json:"content_type"`
		SizeBytes     int                    `json:"size_bytes"`
		ScanResult    map[string]interface{} `json:"scan_result"`
		Metadata      map[string]interface{} `json:"metadata"`
		ImageBytesB64 string                 `json:"image_bytes_b64"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("stego-callback: invalid JSON: %v", err)
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if payload.BlockHeight == 0 {
		log.Printf("stego-callback: missing block height")
		http.Error(w, "missing block height", http.StatusBadRequest)
		return
	}

	block, err := api.loadBlock(payload.BlockHeight)
	if err != nil {
		log.Printf("stego-callback: block %d not found: %v", payload.BlockHeight, err)
		http.Error(w, "block not found", http.StatusNotFound)
		return
	}

	idx := -1
	for i, ins := range block.Inscriptions {
		if payload.FileName != "" && ins.FileName == payload.FileName {
			idx = i
			break
		}
		if payload.TxID != "" && ins.TxID == payload.TxID {
			idx = i
			break
		}
	}

	if idx == -1 {
		// Append new inscription entry for completeness
		block.Inscriptions = append(block.Inscriptions, bitcoin.InscriptionData{
			TxID:        payload.TxID,
			ContentType: payload.ContentType,
			FileName:    payload.FileName,
			FilePath:    fmt.Sprintf("images/%s", payload.FileName),
			SizeBytes:   payload.SizeBytes,
			Content:     "",
		})
		idx = len(block.Inscriptions) - 1
	}

	for len(block.ScanResults) < len(block.Inscriptions) {
		block.ScanResults = append(block.ScanResults, map[string]interface{}{})
	}
	block.ScanResults[idx] = payload.ScanResult

	// Persist updated block data
	resp := &bitcoin.BlockInscriptionsResponse{
		BlockHeight:       block.BlockHeight,
		BlockHash:         block.BlockHash,
		Timestamp:         block.Timestamp,
		TotalTransactions: block.TxCount,
		Inscriptions:      block.Inscriptions,
		Images:            block.Images,
		SmartContracts:    block.SmartContracts,
		ProcessingTime:    block.ProcessingTime,
		Success:           block.Success,
	}
	if err := api.dataStorage.StoreBlockData(resp, block.ScanResults); err != nil {
		log.Printf("Failed to persist stego callback update: %v", err)
		http.Error(w, "failed to persist update", http.StatusInternalServerError)
		return
	}

	update := api.dataStorage.CreateRealtimeUpdate("scan_complete", block.BlockHeight, payload)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "accepted",
		"request": payload.RequestID,
		"update":  update,
	})
}

func (api *DataAPI) handleStegoBatch(blockHeight int64, body []byte, w http.ResponseWriter) error {
	var payload struct {
		RequestID    string `json:"request_id"`
		BlockHeight  int64  `json:"block_height"`
		BlockHash    string `json:"block_hash"`
		Timestamp    int64  `json:"timestamp"`
		Inscriptions []struct {
			TxID        string                 `json:"tx_id"`
			InputIndex  int                    `json:"input_index"`
			FileName    string                 `json:"file_name"`
			FilePath    string                 `json:"file_path"`
			ContentType string                 `json:"content_type"`
			SizeBytes   int                    `json:"size_bytes"`
			ScanResult  map[string]interface{} `json:"scan_result"`
		} `json:"inscriptions"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if payload.BlockHeight == 0 {
		return fmt.Errorf("missing block height")
	}

	block, err := api.loadBlock(payload.BlockHeight)
	if err != nil {
		block = &storage.BlockDataCache{
			BlockHeight:    payload.BlockHeight,
			BlockHash:      payload.BlockHash,
			Timestamp:      payload.Timestamp,
			Inscriptions:   []bitcoin.InscriptionData{},
			Images:         []bitcoin.ExtractedImageData{},
			SmartContracts: []bitcoin.SmartContractData{},
			ScanResults:    []map[string]interface{}{},
			ProcessingTime: 0,
			Success:        true,
			CacheTimestamp: time.Now(),
			SteganographySummary: &bitcoin.SteganographySummary{
				TotalImages:   len(payload.Inscriptions),
				StegoDetected: false,
				StegoCount:    0,
				ScanTimestamp: time.Now().Unix(),
				AvgConfidence: 0,
				StegoTypes:    []string{},
			},
		}
	}

	if block.BlockHash == "" {
		block.BlockHash = payload.BlockHash
	}
	if block.Timestamp == 0 {
		block.Timestamp = payload.Timestamp
	}

	for _, ins := range payload.Inscriptions {
		idx := -1
		for i, existing := range block.Inscriptions {
			if ins.FileName != "" && existing.FileName == ins.FileName {
				idx = i
				break
			}
			if ins.TxID != "" && existing.TxID == ins.TxID {
				idx = i
				break
			}
		}

		if idx == -1 {
			block.Inscriptions = append(block.Inscriptions, bitcoin.InscriptionData{
				TxID:        ins.TxID,
				InputIndex:  ins.InputIndex,
				ContentType: ins.ContentType,
				Content:     "",
				SizeBytes:   ins.SizeBytes,
				FileName:    ins.FileName,
				FilePath:    ins.FilePath,
			})
			idx = len(block.Inscriptions) - 1
		} else {
			block.Inscriptions[idx].ContentType = ins.ContentType
			block.Inscriptions[idx].SizeBytes = ins.SizeBytes
			if block.Inscriptions[idx].FileName == "" {
				block.Inscriptions[idx].FileName = ins.FileName
			}
			if block.Inscriptions[idx].FilePath == "" {
				block.Inscriptions[idx].FilePath = ins.FilePath
			}
		}

		for len(block.ScanResults) < len(block.Inscriptions) {
			block.ScanResults = append(block.ScanResults, map[string]interface{}{})
		}
		block.ScanResults[idx] = ins.ScanResult
	}

	resp := &bitcoin.BlockInscriptionsResponse{
		BlockHeight:       block.BlockHeight,
		BlockHash:         block.BlockHash,
		Timestamp:         block.Timestamp,
		TotalTransactions: block.TxCount,
		Inscriptions:      block.Inscriptions,
		Images:            block.Images,
		SmartContracts:    block.SmartContracts,
		ProcessingTime:    block.ProcessingTime,
		Success:           true,
	}
	if resp.TotalTransactions == 0 {
		resp.TotalTransactions = len(block.Inscriptions)
	}

	if err := api.dataStorage.StoreBlockData(resp, block.ScanResults); err != nil {
		return fmt.Errorf("persist batch update: %w", err)
	}

	update := api.dataStorage.CreateRealtimeUpdate("scan_complete", block.BlockHeight, map[string]interface{}{
		"mode":         "batch",
		"inscriptions": len(payload.Inscriptions),
		"request_id":   payload.RequestID,
		"block_height": block.BlockHeight,
		"block_hash":   block.BlockHash,
		"updated_at":   time.Now().Unix(),
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "accepted",
		"request": payload.RequestID,
		"update":  update,
	})

	return nil
}
