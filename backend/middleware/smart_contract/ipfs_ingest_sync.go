package smart_contract

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"stargate-backend/services"
	"stargate-backend/stego"
)

type ipfsIngestSyncConfig struct {
	Enabled          bool
	Interval         time.Duration
	UploadsDir       string
	ManifestFileName string
	MaxEntries       int
}

type ipfsIngestSyncState struct {
	lastSeen map[string]int64
}

type ipfsIngestManifest struct {
	Version   int                       `json:"version"`
	Origin    string                    `json:"origin"`
	CreatedAt int64                     `json:"created_at"`
	Files     []ipfsIngestManifestEntry `json:"files"`
}

type ipfsIngestManifestEntry struct {
	Path    string `json:"path"`
	CID     string `json:"cid"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
}

// StartIPFSIngestionSync watches the uploads manifest and creates ingestion records for stego images.
func StartIPFSIngestionSync(ctx context.Context, ingest *services.IngestionService) error {
	if ingest == nil {
		return fmt.Errorf("ipfs ingestion sync requires ingestion service")
	}
	cfg := loadIPFSIngestSyncConfig()
	if !cfg.Enabled {
		return nil
	}
	state := &ipfsIngestSyncState{lastSeen: make(map[string]int64)}
	go func() {
		t := time.NewTicker(cfg.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := ipfsIngestSyncOnce(ctx, ingest, cfg, state); err != nil {
					log.Printf("ipfs ingestion sync error: %v", err)
				}
			}
		}
	}()
	return nil
}

func loadIPFSIngestSyncConfig() ipfsIngestSyncConfig {
	enabled := strings.EqualFold(strings.TrimSpace(os.Getenv("IPFS_INGEST_SYNC_ENABLED")), "true")
	interval := 60 * time.Second
	if raw := strings.TrimSpace(os.Getenv("IPFS_INGEST_SYNC_INTERVAL_SEC")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			interval = time.Duration(v) * time.Second
		}
	}
	uploadsDir := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}
	manifest := strings.TrimSpace(os.Getenv("IPFS_INGEST_SYNC_MANIFEST"))
	if manifest == "" {
		manifest = "stargate-uploads-manifest.json"
	}
	maxEntries := 5000
	if raw := strings.TrimSpace(os.Getenv("IPFS_INGEST_SYNC_MAX_ENTRIES")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			maxEntries = v
		}
	}
	return ipfsIngestSyncConfig{
		Enabled:          enabled,
		Interval:         interval,
		UploadsDir:       uploadsDir,
		ManifestFileName: manifest,
		MaxEntries:       maxEntries,
	}
}

func ipfsIngestSyncOnce(ctx context.Context, ingest *services.IngestionService, cfg ipfsIngestSyncConfig, state *ipfsIngestSyncState) error {
	manifestPath := filepath.Join(cfg.UploadsDir, cfg.ManifestFileName)
	payload, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read manifest: %w", err)
	}
	var manifest ipfsIngestManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	entries := manifest.Files
	if cfg.MaxEntries > 0 && len(entries) > cfg.MaxEntries {
		entries = entries[len(entries)-cfg.MaxEntries:]
	}
	reconcileCfg := loadStegoReconcileConfig()
	var processed int
	for _, entry := range entries {
		relPath := strings.TrimSpace(entry.Path)
		if relPath == "" {
			continue
		}
		if !isImageFile(relPath) {
			continue
		}
		if last, ok := state.lastSeen[relPath]; ok && entry.ModTime <= last {
			continue
		}
		fullPath := filepath.Join(cfg.UploadsDir, relPath)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		manifestBytes, err := extractStegoManifest(ctx, data, reconcileCfg)
		if err != nil {
			state.lastSeen[relPath] = entry.ModTime
			continue
		}
		stegoManifest, err := stego.ParseManifestYAML(manifestBytes)
		if err != nil {
			state.lastSeen[relPath] = entry.ModTime
			continue
		}
		id := strings.TrimSpace(stegoManifest.VisiblePixelHash)
		if id == "" {
			id = strings.TrimSpace(stegoManifest.ProposalID)
		}
		if id == "" {
			state.lastSeen[relPath] = entry.ModTime
			continue
		}
		if existing, err := ingest.Get(id); err == nil && existing != nil {
			metaUpdates := map[string]interface{}{
				"stego_image_cid":            entry.CID,
				"stego_payload_cid":          stegoManifest.PayloadCID,
				"stego_manifest_issuer":      stegoManifest.Issuer,
				"stego_manifest_created_at":  stegoManifest.CreatedAt,
				"stego_manifest_proposal_id": stegoManifest.ProposalID,
				"visible_pixel_hash":         stegoManifest.VisiblePixelHash,
			}
			_ = ingest.UpdateMetadata(id, metaUpdates)
			state.lastSeen[relPath] = entry.ModTime
			processed++
			continue
		}
		rec := services.IngestionRecord{
			ID:            id,
			Filename:      filepath.Base(relPath),
			Method:        "stego",
			MessageLength: len(manifestBytes),
			ImageBase64:   base64.StdEncoding.EncodeToString(data),
			Metadata: map[string]interface{}{
				"stego_image_cid":            entry.CID,
				"stego_payload_cid":          stegoManifest.PayloadCID,
				"stego_manifest_issuer":      stegoManifest.Issuer,
				"stego_manifest_created_at":  stegoManifest.CreatedAt,
				"stego_manifest_proposal_id": stegoManifest.ProposalID,
				"stego_manifest_yaml":        string(manifestBytes),
				"visible_pixel_hash":         stegoManifest.VisiblePixelHash,
			},
			Status: "verified",
		}
		if err := ingest.Create(rec); err != nil {
			log.Printf("ipfs ingestion create failed for %s: %v", id, err)
		} else {
			processed++
		}
		state.lastSeen[relPath] = entry.ModTime
	}
	if processed > 0 {
		log.Printf("ipfs ingestion sync: processed=%d entries", processed)
	}
	return nil
}

func isImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	default:
		return false
	}
}
