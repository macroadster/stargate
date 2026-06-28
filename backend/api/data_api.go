package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"stargate-backend/bitcoin"
	"stargate-backend/storage"
)

// DataAPI handles enhanced API endpoints for block monitoring data
type DataAPI struct {
	dataStorage  storage.ExtendedDataStorage
	blockMonitor *bitcoin.BlockMonitor
	bitcoinAPI   *bitcoin.BitcoinAPI
	// simple in-memory index of tx -> block height for manifest/content lookup
	txIndex map[string]int64
	// reverse index so we can quickly know which txs (and thus content) belong to a height
	// even if the BlockDataCache.Inscriptions list for that height is currently empty.
	heightIndex map[int64][]string
	txMu        sync.RWMutex
}

// NewDataAPI creates a new data API instance
func NewDataAPI(dataStorage storage.ExtendedDataStorage, blockMonitor *bitcoin.BlockMonitor, bitcoinAPI *bitcoin.BitcoinAPI) *DataAPI {
	api := &DataAPI{
		dataStorage:  dataStorage,
		blockMonitor: blockMonitor,
		bitcoinAPI:   bitcoinAPI,
		txIndex:      make(map[string]int64),
		heightIndex:  make(map[int64][]string),
	}
	api.buildTxIndex()
	return api
}

type scriptOpLocal struct {
	opcode byte
	data   []byte
	isPush bool
}

func splitPath(path string) []string {
	path = path[1:] // Remove leading slash
	if path == "" {
		return []string{}
	}
	var parts []string
	for _, part := range strings.Split(path, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func (api *DataAPI) sendSSEUpdate(w http.ResponseWriter, update *storage.RealtimeUpdate) {
	data, err := json.Marshal(update)
	if err != nil {
		log.Printf("Failed to marshal SSE update: %v", err)
		return
	}

	fmt.Fprintf(w, "data: %s\n\n", data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func (api *DataAPI) monitorUpdates(updates chan *storage.RealtimeUpdate) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Send periodic statistics update
			stats := api.dataStorage.GetSteganographyStats()
			update := api.dataStorage.CreateRealtimeUpdate("stats_update", 0, stats)
			select {
			case updates <- update:
			default:
				// Channel full, skip this update
			}
		}
	}
}

// getTransactionCount returns a best-effort transaction count.
func (api *DataAPI) getTransactionCount(blockData *storage.BlockDataCache) int {
	if blockData == nil {
		return 0
	}
	if blockData.TxCount > 0 {
		return blockData.TxCount
	}
	if len(blockData.Images) > 0 {
		return len(blockData.Images)
	}
	if len(blockData.Inscriptions) > 0 {
		return len(blockData.Inscriptions)
	}
	return 0
}
