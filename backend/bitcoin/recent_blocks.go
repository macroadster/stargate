package bitcoin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// GetAllRecentBlocks returns recent blocks with inscriptions
func (bm *BlockMonitor) GetAllRecentBlocks() (map[string]any, error) {
	indexFile := filepath.Join(bm.blocksDir, "global_inscriptions.json")

	data, err := os.ReadFile(indexFile)
	if err != nil {
		return map[string]any{
			"blocks": []any{},
			"total":  0,
		}, nil
	}

	var globalIndex map[string]any
	if err := json.Unmarshal(data, &globalIndex); err != nil {
		return map[string]any{
			"blocks": []any{},
			"total":  0,
		}, nil
	}

	// Convert to array format for frontend
	var blocks []any
	for _, blockData := range globalIndex {
		blocks = append(blocks, blockData)
	}

	// Sort blocks by height (highest first)
	sort.Slice(blocks, func(i, j int) bool {
		blockI := blocks[i].(map[string]any)
		blockJ := blocks[j].(map[string]any)

		heightI := int64(blockI["height"].(float64))
		heightJ := int64(blockJ["height"].(float64))

		return heightI > heightJ
	})

	return map[string]any{
		"blocks": blocks,
		"total":  len(blocks),
	}, nil
}
