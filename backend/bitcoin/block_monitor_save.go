package bitcoin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"stargate-backend/security"
)

func blocksDirFromEnv() string {
	if v := os.Getenv("BLOCKS_DIR"); v != "" {
		return v
	}
	return "blocks"
}

// partitionedBlockSubdir returns a 3-level partitioned path for the block dir
// e.g. "000/144/144001_abc12345" for height 144001.
// This greatly reduces the cost of directory scans.
func partitionedBlockSubdir(height int64, hash8 string) string {
	if len(hash8) > 8 {
		hash8 = hash8[:8]
	}
	if hash8 == "" {
		hash8 = "00000000"
	}
	l1 := height / 1000000
	l2 := (height / 1000) % 1000
	return fmt.Sprintf("%03d/%03d/%d_%s", l1, l2, height, hash8)
}

// BlockDir returns the full filesystem path using the partitioned layout.
func BlockDir(blocksRoot string, height int64, hash8 string) string {
	if blocksRoot == "" {
		blocksRoot = blocksDirFromEnv()
	}
	return filepath.Join(blocksRoot, partitionedBlockSubdir(height, hash8))
}

// FindBlockDir locates an existing block directory for a height.
// It checks the partitioned layout first, then falls back to the legacy flat layout
// (for migration / old data compatibility). Returns the full path or error.
func FindBlockDir(blocksRoot string, height int64) (string, error) {
	if blocksRoot == "" {
		blocksRoot = blocksDirFromEnv()
	}
	if blocksRoot == "" {
		return "", fmt.Errorf("no blocks dir configured")
	}

	// 1. Partitioned layout (new): glob inside the expected leaf dir
	l1 := height / 1000000
	l2 := (height / 1000) % 1000
	partBase := filepath.Join(blocksRoot, fmt.Sprintf("%03d/%03d", l1, l2))
	if matches, _ := filepath.Glob(filepath.Join(partBase, fmt.Sprintf("%d_*", height))); len(matches) > 0 {
		return matches[0], nil
	}

	// 2. Legacy flat layout at root: <height>_<hash8>
	if matches, _ := filepath.Glob(filepath.Join(blocksRoot, fmt.Sprintf("%d_*", height))); len(matches) > 0 {
		return matches[0], nil
	}

	// 3. Legacy hack some code used with explicit _00000000
	hack := filepath.Join(blocksRoot, fmt.Sprintf("%d_00000000", height))
	if fi, err := os.Stat(hack); err == nil && fi.IsDir() {
		return hack, nil
	}

	return "", fmt.Errorf("no block directory found for height %d under %s", height, blocksRoot)
}

// getBlockHashForDir returns the stored block hash for a block directory.
// Tries block.json first (for full data), falls back to inscriptions.json "block_hash"
// (works for lightweight "metadata only" boring blocks).
func getBlockHashForDir(blockDir string) (string, error) {
	// Prefer block.json
	if h, err := readBlockHeaderHash(filepath.Join(blockDir, "block.json")); err == nil && h != "" {
		return h, nil
	}
	// Fallback for light blocks
	ip := filepath.Join(blockDir, "inscriptions.json")
	data, err := os.ReadFile(ip)
	if err != nil {
		return "", err
	}
	var p struct {
		BlockHash string `json:"block_hash"`
	}
	if json.Unmarshal(data, &p) == nil && strings.TrimSpace(p.BlockHash) != "" {
		return strings.TrimSpace(p.BlockHash), nil
	}
	return "", fmt.Errorf("could not extract block hash from %s", blockDir)
}

func (bm *BlockMonitor) getCanonicalBlockHash(height int64) (string, error) {
	if bm.chain != nil {
		return bm.chain.GetBlockHash(context.Background(), height)
	}
	if bm.bitcoinClient != nil {
		return bm.bitcoinClient.GetBlockHash(int(height))
	}
	return "", fmt.Errorf("no chain backend configured for block hash")
}

func (bm *BlockMonitor) pruneBlockDirsForHeight(height int64, canonicalHash string) (bool, error) {
	blocksDir := bm.blocksDir
	if blocksDir == "" {
		blocksDir = blocksDirFromEnv()
	}
	if blocksDir == "" {
		return false, nil
	}

	// Use targeted lookup instead of full linear ReadDir over all blocks.
	// This is a major IO win thanks to partitioning + find.
	candidateDirs := findCandidateDirsForHeight(blocksDir, height)

	var removed bool
	var hasCanonical bool
	reorgDir := filepath.Join(blocksDir, "reorgs")

	for _, dirPath := range candidateDirs {
		entryName := filepath.Base(dirPath)
		hash, err := getBlockHashForDir(dirPath)
		if err != nil || hash == "" {
			continue
		}
		if hash == canonicalHash {
			hasCanonical = true
			continue
		}
		log.Printf("Reorg cleanup: moving stale block dir %s to reorgs (hash=%s canonical=%s)", entryName, hash, canonicalHash)
		if err := os.MkdirAll(reorgDir, 0755); err != nil {
			return removed, err
		}

		// Place into partitioned layout under reorgs/ too (e.g. reorgs/000/144/144001_xxx)
		dest := filepath.Join(reorgDir, entryName) // default flat under reorgs
		if parts := strings.SplitN(entryName, "_", 2); len(parts) == 2 {
			if h, perr := strconv.ParseInt(parts[0], 10, 64); perr == nil {
				h8 := parts[1]
				if len(h8) > 8 {
					h8 = h8[:8]
				}
				dest = BlockDir(reorgDir, h, h8)
			}
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			log.Printf("reorg move: mkdir %s failed: %v", dest, err)
		}
		if err := os.Rename(dirPath, dest); err != nil {
			if err := copyDir(dirPath, dest); err != nil {
				return removed, err
			}
			if err := os.RemoveAll(dirPath); err != nil {
				return removed, err
			}
		}
		removed = true
	}
	if removed && !hasCanonical {
		return true, nil
	}
	return false, nil
}

// findCandidateDirsForHeight returns possible directories for a height using
// partitioned layout + legacy flat (no full root directory scan).
func findCandidateDirsForHeight(blocksDir string, height int64) []string {
	var candidates []string

	// New partitioned layout (preferred)
	l1 := height / 1000000
	l2 := (height / 1000) % 1000
	partBase := filepath.Join(blocksDir, fmt.Sprintf("%03d/%03d", l1, l2))
	if matches, _ := filepath.Glob(filepath.Join(partBase, fmt.Sprintf("%d_*", height))); len(matches) > 0 {
		candidates = append(candidates, matches...)
	}

	// Legacy flat layout at root
	if matches, _ := filepath.Glob(filepath.Join(blocksDir, fmt.Sprintf("%d_*", height))); len(matches) > 0 {
		candidates = append(candidates, matches...)
	}

	// Support old _00000000 hack in root
	hack := filepath.Join(blocksDir, fmt.Sprintf("%d_00000000", height))
	if fi, err := os.Stat(hack); err == nil && fi.IsDir() {
		candidates = append(candidates, hack)
	}

	// Dedup just in case
	seen := map[string]bool{}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

func copyDir(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if err := copyFile(path, target); err != nil {
			return err
		}
		return os.Chmod(target, info.Mode())
	})
}

func readBlockHeaderHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var payload struct {
		BlockHeader struct {
			Hash string `json:"Hash"`
		} `json:"block_header"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.BlockHeader.Hash), nil
}

// saveBlockData saves raw block data to files
func (bm *BlockMonitor) saveBlockData(blockDir string, parsedBlock *ParsedBlock, hexData string) error {
	// Save raw hex data
	hexFile := filepath.Join(blockDir, "block.hex")
	if err := os.WriteFile(hexFile, []byte(hexData), 0644); err != nil {
		return fmt.Errorf("failed to write hex file: %w", err)
	}

	// Save parsed block data as JSON
	blockData := BlockData{
		BlockHeader: BlockHeader{
			Version:    parsedBlock.Header.Version,
			PrevBlock:  parsedBlock.Header.PrevBlock,
			MerkleRoot: parsedBlock.Header.MerkleRoot,
			Timestamp:  parsedBlock.Header.Timestamp,
			Bits:       parsedBlock.Header.Bits,
			Nonce:      parsedBlock.Header.Nonce,
			Hash:       parsedBlock.Header.Hash,
		},
		Transactions:    bm.convertTransactions(parsedBlock.Transactions),
		ExtractedImages: parsedBlock.Images,
		Metadata: BlockMetadata{
			SourceFile:     fmt.Sprintf("block_%s.hex", parsedBlock.Header.Hash),
			FileSize:       int64(len(hexData)),
			ParserVersion:  "1.0.0",
			ProcessingTime: time.Now().Unix(),
		},
		ProcessingInfo: ProcessingInfo{
			StartedAt:   time.Now(),
			CompletedAt: time.Now(),
			Version:     "1.0.0",
			APISources:  []string{"blockchain.info", "raw_parser"},
			Success:     true,
		},
	}

	blockJSON, err := json.MarshalIndent(blockData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal block data: %w", err)
	}

	blockFile := filepath.Join(blockDir, "block.json")
	if err := os.WriteFile(blockFile, blockJSON, 0644); err != nil {
		return fmt.Errorf("failed to write block JSON: %w", err)
	}

	return nil
}

// saveImages saves extracted images to files
func (bm *BlockMonitor) saveImages(blockDir string, images []ExtractedImageData) error {
	if len(images) == 0 {
		return nil
	}

	imagesDir := filepath.Join(blockDir, "images")
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return fmt.Errorf("failed to create images directory: %w", err)
	}

	for _, image := range images {
		cleaned := sanitizeExtractedImage(image)
		imageFile := security.SafeFilePath(imagesDir, cleaned.FileName)
		// Save the actual image data
		if err := os.WriteFile(imageFile, cleaned.Data, 0644); err != nil {
			log.Printf("Failed to save image %s: %v", cleaned.FileName, err)
		} else {
			log.Printf("Successfully saved image %s (%d bytes)", cleaned.FileName, len(cleaned.Data))
		}
	}

	return nil
}

// createInscriptionsFromImages creates inscription data from extracted images
func (bm *BlockMonitor) createInscriptionsFromImages(images []ExtractedImageData) []InscriptionData {
	var inscriptions []InscriptionData

	for i, image := range images {
		cleaned := sanitizeExtractedImage(image)
		contentType := image.ContentType
		if contentType == "" {
			if strings.HasPrefix(image.Format, "text") || image.Format == "txt" {
				contentType = "text/plain"
			} else {
				contentType = fmt.Sprintf("image/%s", image.Format)
			}
		}
		content := ""
		if strings.HasPrefix(contentType, "text/") {
			content = string(cleaned.Data)
		} else {
			// Avoid storing binary blobs in the DB payload; rely on disk + /content API for retrieval.
			content = ""
		}

		inscription := InscriptionData{
			TxID:        image.TxID,
			InputIndex:  i,
			ContentType: contentType,
			Content:     content,
			SizeBytes:   cleaned.SizeBytes,
			FileName:    cleaned.FileName,
			FilePath:    cleaned.FilePath,
		}
		inscriptions = append(inscriptions, inscription)
	}

	return inscriptions
}

// saveBlockSummary saves a comprehensive block summary for frontend API

// calculateTransactionSize calculates the size of a transaction in bytes

// Version (4 bytes)

// Previous txid
// Previous index

// Sequence

// Value

// Marker
// Flag

// Locktime (4 bytes)

// encodeVarIntSize returns the size of a varint encoding for the given value
func encodeVarIntSize(value int) int {
	if value < 0xfd {
		return 1
	} else if value <= 0xffff {
		return 3
	} else if value <= 0xffffffff {
		return 5
	} else {
		return 9
	}
}

// MigrateOldBlockLayoutIfNeeded performs a one-time (or safe re-runnable) migration
// of legacy flat block directories (<height>_<hash8>) into the 3-level partitioned
// layout under the same blocksRoot.
//
// This reduces future directory scan costs for reorgs, recent summaries, and
// discovery. It is called early during startup.
//
// A marker file (.layout-v2) is written so we don't rescan on every restart.
func MigrateOldBlockLayoutIfNeeded(blocksRoot string) error {
	if blocksRoot == "" {
		blocksRoot = blocksDirFromEnv()
	}
	if blocksRoot == "" {
		return nil
	}

	marker := filepath.Join(blocksRoot, ".layout-v2")

	// Always clean up known obsolete top-level block_*.json files.
	// This is cheap and idempotent, so we do it on every start even if the
	// main dir migration marker is present (from a previous deployment on the
	// same volume).
	cleanupLegacyTopLevelBlockFiles(blocksRoot)

	if _, err := os.Stat(marker); err == nil {
		return nil // main dir migration already done
	}

	// Migrate direct children that look like height dirs
	migrated, err := migrateFlatHeightDirs(blocksRoot, blocksRoot)
	if err != nil {
		log.Printf("block layout migration: %v", err)
	}

	// Also migrate anything sitting flat inside reorgs/ (keep them under reorgs/ partitioned or flat)
	reorgRoot := filepath.Join(blocksRoot, "reorgs")
	if fi, _ := os.Stat(reorgRoot); fi != nil && fi.IsDir() {
		if m2, _ := migrateFlatHeightDirs(reorgRoot, reorgRoot); m2 > 0 {
			migrated += m2
		}
	}

	if migrated > 0 {
		log.Printf("Block dir layout migration: moved %d legacy flat directories into partitioned structure", migrated)
	}

	// Write marker (best effort)
	_ = os.WriteFile(marker, []byte("2\n"), 0644)
	return nil
}

// cleanupLegacyTopLevelBlockFiles removes old top-level block_*.json files
// that were written by the legacy DataStorage.saveBlockDataToFile.
// These are now superseded by the per-height directories (inscriptions.json etc.).
func cleanupLegacyTopLevelBlockFiles(blocksRoot string) {
	if blocksRoot == "" {
		blocksRoot = blocksDirFromEnv()
	}
	if blocksRoot == "" {
		return
	}
	matches, _ := filepath.Glob(filepath.Join(blocksRoot, "block_*.json"))
	for _, f := range matches {
		if err := os.Remove(f); err != nil {
			log.Printf("cleanup: failed to remove legacy %s: %v", f, err)
		} else {
			log.Printf("cleanup: removed legacy top-level block file %s", filepath.Base(f))
		}
	}
}

// migrateFlatHeightDirs scans srcRoot for direct flat height_ dirs and moves
// them under the partitioned layout relative to dstBase (usually same as srcRoot).
// Returns number migrated.
func migrateFlatHeightDirs(srcRoot, dstBase string) (int, error) {
	entries, err := os.ReadDir(srcRoot)
	if err != nil {
		return 0, nil
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 || len(parts[1]) < 8 {
			continue
		}
		height, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}
		hash8 := parts[1][:8]

		oldPath := filepath.Join(srcRoot, name)
		newPath := BlockDir(dstBase, height, hash8)

		if _, statErr := os.Stat(newPath); statErr == nil {
			// target already exists; skip (or could remove old if wanted)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
			log.Printf("migration mkdir failed for %s: %v", newPath, err)
			continue
		}

		if err := os.Rename(oldPath, newPath); err != nil {
			if cErr := copyDir(oldPath, newPath); cErr != nil {
				log.Printf("migration copy failed %s -> %s: %v", oldPath, newPath, cErr)
				continue
			}
			_ = os.RemoveAll(oldPath)
		}
		count++
	}
	return count, nil
}
