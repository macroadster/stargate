package api

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"stargate-backend/bitcoin"
	"stargate-backend/storage"
)

// blockPayloadWriter replaces one stored block payload, including its stego summary.
type blockPayloadWriter interface {
	SaveBlockPayload(cache *storage.BlockDataCache) error
}

// blockHeightLister enumerates heights that have a stored block payload.
type blockHeightLister interface {
	ListBlockHeights(start, end int64) ([]int64, error)
}

// repairStoredContracts drops false OP_RETURN contracts from stored blocks
// in [start, end]. It rewrites inscriptions.json / block.json and the
// storage payload the block rail reads. Missing heights are skipped.
func (api *DataAPI) repairStoredContracts(start, end int64) (blocks, dropped int) {
	if api == nil || start <= 0 || end < start {
		return 0, 0
	}
	heights := map[int64]struct{}{}
	base := strings.TrimRight(api.resolveBlocksDir(), "/")
	if base != "" {
		_ = filepath.WalkDir(base, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || d == nil || !d.IsDir() {
				return nil
			}
			if bitcoin.IsArchivedBlockPath(base, path) {
				return filepath.SkipDir
			}
			name := d.Name()
			idx := strings.Index(name, "_")
			if idx <= 0 {
				return nil
			}
			height, err := strconv.ParseInt(name[:idx], 10, 64)
			if err != nil || height < start || height > end {
				return nil
			}
			heights[height] = struct{}{}
			return nil
		})
	}
	if lister, ok := api.dataStorage.(blockHeightLister); ok && lister != nil {
		listed, err := lister.ListBlockHeights(start, end)
		if err != nil {
			log.Printf("scan repair: list stored heights %d-%d: %v", start, end, err)
		}
		for _, height := range listed {
			heights[height] = struct{}{}
		}
	}
	for height := range heights {
		n, err := api.repairStoredHeight(height)
		if err != nil {
			log.Printf("scan repair: block %d: %v", height, err)
			continue
		}
		if n > 0 {
			blocks++
			dropped += n
			log.Printf("scan repair: block %d dropped %d false contract row(s)", height, n)
		}
	}
	return blocks, dropped
}

// repairStoredHeight rewrites one stored block. The count is the number of
// false contract rows removed from the file or the storage payload, whichever
// is larger. The same row living in both places is not counted twice.
func (api *DataAPI) repairStoredHeight(height int64) (int, error) {
	fileDropped := 0
	if dir, err := bitcoin.FindBlockDir(api.resolveBlocksDir(), height); err == nil {
		for _, name := range []string{"inscriptions.json", "block.json"} {
			n, _, changed, err := repairSmartContractFile(filepath.Join(dir, name))
			if err != nil {
				return 0, err
			}
			if !changed {
				continue
			}
			if name == "inscriptions.json" || fileDropped == 0 {
				fileDropped = n
			}
		}
	}

	storeDropped := 0
	if api.dataStorage != nil {
		if existing, err := api.dataStorage.GetBlockData(height); err == nil {
			if cache, ok := existing.(*storage.BlockDataCache); ok && cache != nil {
				n, changed := scrubBlockCache(cache)
				if changed {
					storeDropped = n
					if err := api.saveBlockPayload(cache); err != nil {
						return 0, err
					}
				}
			}
		}
	}

	if storeDropped > fileDropped {
		return storeDropped, nil
	}
	return fileDropped, nil
}

func (api *DataAPI) saveBlockPayload(cache *storage.BlockDataCache) error {
	writer, ok := api.dataStorage.(blockPayloadWriter)
	if !ok || writer == nil {
		return nil
	}
	return writer.SaveBlockPayload(cache)
}

// scrubBlockCache drops false OP_RETURN contracts and classifier hits that
// extracted no message. A hit with an empty message does not keep the block
// marked as a smart contract. Returns the number of contract rows removed.
func scrubBlockCache(cache *storage.BlockDataCache) (dropped int, changed bool) {
	if cache == nil {
		return 0, false
	}
	kept, foreign := bitcoin.DropForeignOPReturnContracts(cache.SmartContracts)
	next := make([]bitcoin.SmartContractData, 0, len(kept))
	for _, contract := range kept {
		if isSyntheticStegoContract(contract) && contractExtractedMessage(contract) == "" {
			dropped++
			continue
		}
		next = append(next, contract)
	}
	dropped += foreign
	if dropped > 0 {
		cache.SmartContracts = next
		changed = true
	}

	// Keep a real contract's scan summary. Only a block with no contract and no
	// extracted payload loses the classifier hit that was flagging it.
	if len(filterSmartContractsForUI(cache.SmartContracts)) == 0 && !blockHasExtractedStego(cache) {
		for i, result := range cache.ScanResults {
			if !scanResultIsStego(result) {
				continue
			}
			if strings.TrimSpace(stringFromAny(result["extracted_message"])) != "" {
				continue
			}
			result["is_stego"] = false
			cache.ScanResults[i] = result
			changed = true
		}
		if cache.SteganographySummary != nil {
			summary := cache.SteganographySummary
			if summary.StegoDetected || summary.StegoCount > 0 || len(summary.StegoTypes) > 0 {
				summary.StegoDetected = false
				summary.StegoCount = 0
				summary.AvgConfidence = 0
				summary.StegoTypes = nil
				changed = true
			}
		}
	}
	if changed && dropped == 0 {
		// The block lost its contract flag even though no contract row was removed.
		dropped = 1
	}
	return dropped, changed
}

// repairSmartContractFile rewrites smart_contracts in one block JSON file.
// Other keys are preserved. A clean file is left byte-for-byte unchanged.
func repairSmartContractFile(path string) (dropped int, kept []bitcoin.SmartContractData, changed bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil, false, nil
		}
		return 0, nil, false, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return 0, nil, false, err
	}
	raw, ok := doc["smart_contracts"]
	if !ok || raw == nil {
		return 0, nil, false, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return 0, nil, false, err
	}
	var contracts []bitcoin.SmartContractData
	if err := json.Unmarshal(encoded, &contracts); err != nil {
		return 0, nil, false, err
	}
	cache := &storage.BlockDataCache{SmartContracts: contracts}
	if rawResults, ok := doc["scan_results"]; ok && rawResults != nil {
		encodedResults, err := json.Marshal(rawResults)
		if err != nil {
			return 0, nil, false, err
		}
		if err := json.Unmarshal(encodedResults, &cache.ScanResults); err != nil {
			return 0, nil, false, err
		}
	}
	for _, key := range []string{"steganography_summary", "steganography_scan"} {
		rawSummary, ok := doc[key]
		if !ok || rawSummary == nil {
			continue
		}
		encodedSummary, err := json.Marshal(rawSummary)
		if err != nil {
			return 0, nil, false, err
		}
		var summary bitcoin.SteganographySummary
		if err := json.Unmarshal(encodedSummary, &summary); err != nil {
			return 0, nil, false, err
		}
		cache.SteganographySummary = &summary
		break
	}
	dropped, changed = scrubBlockCache(cache)
	if !changed {
		return 0, contracts, false, nil
	}
	kept = cache.SmartContracts
	if kept == nil {
		kept = []bitcoin.SmartContractData{}
	}
	doc["smart_contracts"] = kept
	if _, ok := doc["scan_results"]; ok {
		doc["scan_results"] = cache.ScanResults
	}
	for _, key := range []string{"steganography_summary", "steganography_scan"} {
		if _, ok := doc[key]; ok && cache.SteganographySummary != nil {
			doc[key] = cache.SteganographySummary
		}
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return 0, nil, false, err
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return 0, nil, false, err
	}
	return dropped, kept, true, nil
}
