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

// smartContractReplacer updates a stored block's contract list without
// rewriting the stego summary or the rest of the payload.
type smartContractReplacer interface {
	ReplaceSmartContracts(height int64, contracts []bitcoin.SmartContractData) error
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
			log.Printf("scan repair: block %d dropped %d foreign OP_RETURN contract(s)", height, n)
		}
	}
	return blocks, dropped
}

// repairStoredHeight rewrites one stored block. The count is the number of
// contract rows removed from the file or the storage payload, whichever is
// larger. The same row living in both places is not counted twice.
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
	var storeKept []bitcoin.SmartContractData
	if api.dataStorage != nil {
		if existing, err := api.dataStorage.GetBlockData(height); err == nil {
			if cache, ok := existing.(*storage.BlockDataCache); ok && cache != nil {
				storeKept, storeDropped = bitcoin.DropForeignOPReturnContracts(cache.SmartContracts)
			}
		}
	}
	// Only rewrite storage when that payload itself had a foreign row.
	// A clean database row must not be replaced by the file's contract list.
	if storeDropped > 0 {
		if err := api.replaceStoredContracts(height, storeKept); err != nil {
			return 0, err
		}
	}

	if storeDropped > fileDropped {
		return storeDropped, nil
	}
	return fileDropped, nil
}

func (api *DataAPI) replaceStoredContracts(height int64, contracts []bitcoin.SmartContractData) error {
	replacer, ok := api.dataStorage.(smartContractReplacer)
	if !ok || replacer == nil {
		return nil
	}
	if contracts == nil {
		contracts = []bitcoin.SmartContractData{}
	}
	return replacer.ReplaceSmartContracts(height, contracts)
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
	kept, dropped = bitcoin.DropForeignOPReturnContracts(contracts)
	if dropped == 0 {
		return 0, contracts, false, nil
	}
	if kept == nil {
		kept = []bitcoin.SmartContractData{}
	}
	doc["smart_contracts"] = kept
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
