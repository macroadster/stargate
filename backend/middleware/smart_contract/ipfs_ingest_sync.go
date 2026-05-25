package smart_contract

import (
	"bytes"
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

	"stargate-backend/core/smart_contract"
	"stargate-backend/storage/ipfs"
	"stargate-backend/services"
	"stargate-backend/stego"
)

type ipfsIngestSyncConfig struct {
	Enabled               bool
	Interval              time.Duration
	MaxEntries            int
	APIURL                string
	Topic                 string
	HTTPTimeout           time.Duration
	ReconcileRecentBlocks int
	ReconcileMinInterval  time.Duration
}

type ipfsIngestSyncState struct {
	lastSeen      map[string]int64
	repairChecked map[string]bool
	lastManifest  string
	lastReconcile time.Time
	queueWake     chan struct{}
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

type IngestReconcileFunc func(context.Context, int) error

type pubsubMessage struct {
	From string `json:"from"`
	Data string `json:"data"`
}

type pubsubAnnouncement struct {
	Type        string `json:"type"`
	ManifestCID string `json:"manifest_cid"`
	Origin      string `json:"origin"`
	Timestamp   int64  `json:"timestamp"`
}

type pendingIngestAnnouncement struct {
	Type             string `json:"type"`
	IngestionID      string `json:"ingestion_id"`
	ProposalID       string `json:"proposal_id,omitempty"`
	VisiblePixelHash string `json:"visible_pixel_hash,omitempty"`
	ImageCID         string `json:"image_cid"`
	Filename         string `json:"filename,omitempty"`
	Method           string `json:"method,omitempty"`
	Message          string `json:"message,omitempty"`
	Price            string `json:"price,omitempty"`
	PriceUnit        string `json:"price_unit,omitempty"`
	Address          string `json:"address,omitempty"`
	FundingMode      string `json:"funding_mode,omitempty"`
	Timestamp        int64  `json:"timestamp"`
	// Proposal/task data for peer replication (so peers don't depend on IPFS payload fetch)
	ProposalTitle string              `json:"proposal_title,omitempty"`
	ProposalDesc  string              `json:"proposal_desc,omitempty"`
	BudgetSats    int64               `json:"budget_sats,omitempty"`
	PayloadCID    string              `json:"payload_cid,omitempty"`
	Tasks         []announcementTask  `json:"tasks,omitempty"`
}

type announcementTask struct {
	TaskID           string   `json:"task_id"`
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	BudgetSats       int64    `json:"budget_sats"`
	Skills           []string `json:"skills,omitempty"`
	Status           string   `json:"status,omitempty"`
	ContractorWallet string   `json:"contractor_wallet,omitempty"`
}

type ingestUpdateAnnouncement struct {
	Type                 string   `json:"type"`
	IngestionID          string   `json:"ingestion_id"`
	ProposalID           string   `json:"proposal_id,omitempty"`
	VisiblePixelHash     string   `json:"visible_pixel_hash,omitempty"`
	FundingTxID          string   `json:"funding_txid,omitempty"`
	FundingTxIDs         []string `json:"funding_txids,omitempty"`
	CommitmentLockAddr   string   `json:"commitment_lock_address,omitempty"`
	CommitmentTarget     string   `json:"commitment_target,omitempty"`
	CommitmentAddress    string   `json:"commitment_address,omitempty"`
	CommitmentScript     string   `json:"commitment_script,omitempty"`
	CommitmentVout       uint32   `json:"commitment_vout,omitempty"`
	CommitmentSats       int64    `json:"commitment_sats,omitempty"`
	PayoutScript         string   `json:"payout_script,omitempty"`
	PayoutScripts        []string `json:"payout_scripts,omitempty"`
	PayoutScriptHashes   []string `json:"payout_script_hashes,omitempty"`
	PayoutScriptHash160s []string `json:"payout_script_hash160s,omitempty"`
	Timestamp            int64    `json:"timestamp"`
}

// IngestDownloadedFile is called by the IPFS mirror's OnFileDownloaded callback.
// It reads the file from disk (already downloaded), runs stego extraction, and
// creates an ingestion record + proposal if a valid manifest is found.
// This replaces the redundant pubsub→re-fetch path for mirrored files.
func IngestDownloadedFile(ctx context.Context, filePath string, cid string, ingest *services.IngestionService, store Store) {
	if ingest == nil {
		return
	}
	if !isImageFile(filePath) {
		return
	}
	blob, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("mirror ingest: read %s: %v", filePath, err)
		return
	}
	reconcileCfg := loadStegoReconcileConfig()
	rawBytes, err := extractStegoManifest(ctx, blob, reconcileCfg)
	if err != nil {
		return // not a stego image — normal, skip silently
	}
	stegoManifest, payload, err := stego.ParseEmbedded(rawBytes)
	if err != nil {
		return // not a proper manifest — skip
	}
	id := strings.TrimSpace(stegoManifest.VisiblePixelHash)
	if id == "" {
		id = strings.TrimSpace(stegoManifest.ProposalID)
	}
	if id == "" {
		return
	}
	// v1 fallback: payload was not inline — fetch from IPFS
	if payload.SchemaVersion == 0 && stegoManifest.PayloadCID != "" {
		client := ipfs.NewClientFromEnv()
		if client != nil {
			if loaded, err := fetchStegoPayload(ctx, client, stegoManifest.PayloadCID); err != nil {
				log.Printf("mirror ingest: payload fetch %s: %v", stegoManifest.PayloadCID, err)
			} else {
				payload = loaded
			}
		}
	}
	payloadMeta := payloadMetadataMap(payload)
	// Ensure proposal exists in MCP store
	if store != nil {
		if err := ensureProposalFromStegoPayload(ctx, store, cid, stegoManifest, payload); err != nil {
			log.Printf("mirror ingest: proposal upsert: %v", err)
		}
	}
	// Create or update ingestion record
	if existing, err := ingest.Get(id); err == nil && existing != nil {
		metaUpdates := map[string]interface{}{
			"stego_image_cid":            cid,
			"stego_payload_cid":          stegoManifest.PayloadCID,
			"stego_manifest_issuer":      stegoManifest.Issuer,
			"stego_manifest_created_at":  stegoManifest.CreatedAt,
			"stego_manifest_proposal_id": stegoManifest.ProposalID,
			"visible_pixel_hash":         stegoManifest.VisiblePixelHash,
		}
		for k, v := range payloadMeta {
			if _, ok := metaUpdates[k]; !ok {
				metaUpdates[k] = v
			}
		}
		_ = ingest.UpdateMetadata(id, metaUpdates)
		log.Printf("mirror ingest: updated %s (cid=%s)", id, cid)
		return
	}
	rec := services.IngestionRecord{
		ID:            id,
		Filename:      filepath.Base(filePath),
		Method:        getStegoMethodForFilename(filePath),
		MessageLength: len(rawBytes),
		ImageBase64:   base64.StdEncoding.EncodeToString(blob),
		Metadata: map[string]interface{}{
			"stego_image_cid":            cid,
			"stego_payload_cid":          stegoManifest.PayloadCID,
			"stego_manifest_issuer":      stegoManifest.Issuer,
			"stego_manifest_created_at":  stegoManifest.CreatedAt,
			"stego_manifest_proposal_id": stegoManifest.ProposalID,
			"visible_pixel_hash":         stegoManifest.VisiblePixelHash,
		},
		Status: "verified",
	}
	for k, v := range payloadMeta {
		if _, ok := rec.Metadata[k]; !ok {
			rec.Metadata[k] = v
		}
	}
	if err := ingest.Create(rec); err != nil {
		log.Printf("mirror ingest: create %s: %v", id, err)
	} else {
		log.Printf("mirror ingest: created %s (cid=%s)", id, cid)
	}
}

// StartIPFSIngestionSync subscribes to IPFS mirror announcements and creates ingestion records for stego images.
func StartIPFSIngestionSync(ctx context.Context, ingest *services.IngestionService, store Store, reconcileFn IngestReconcileFunc) error {
	if ingest == nil {
		return fmt.Errorf("ipfs ingestion sync requires ingestion service")
	}
	cfg := loadIPFSIngestSyncConfig()
	if !cfg.Enabled {
		return nil
	}
	if !ipfs.IsEnabled() {
		log.Printf("ipfs ingestion sync: IPFS disabled, skipping")
		return nil
	}
	state := &ipfsIngestSyncState{
		lastSeen:      make(map[string]int64),
		repairChecked: make(map[string]bool),
		queueWake:     make(chan struct{}, 1),
	}
	client := ipfs.NewClientFromEnv()
	go ipfsIngestProcessUpdateQueue(ctx, ingest, store, reconcileFn, cfg, state)
	go func() {
		for {
			if err := ipfsIngestSubscribe(ctx, ingest, store, reconcileFn, cfg, state, client); err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("ipfs ingestion sync error: %v", err)
				time.Sleep(cfg.Interval)
			}
		}
	}()
	return nil
}

func loadIPFSIngestSyncConfig() ipfsIngestSyncConfig {
	// Default to enabled when IPFS is enabled; only disable with explicit "false"
	raw := strings.TrimSpace(os.Getenv("IPFS_INGEST_SYNC_ENABLED"))
	enabled := !strings.EqualFold(raw, "false")
	// Check global IPFS disable flag
	if !ipfs.IsEnabled() {
		enabled = false
	}
	interval := 60 * time.Second
	if raw := strings.TrimSpace(os.Getenv("IPFS_INGEST_SYNC_INTERVAL_SEC")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			interval = time.Duration(v) * time.Second
		}
	}
	maxEntries := 5000
	if raw := strings.TrimSpace(os.Getenv("IPFS_INGEST_SYNC_MAX_ENTRIES")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			maxEntries = v
		}
	}
	apiURL := strings.TrimSpace(os.Getenv("IPFS_API_URL"))
	if apiURL == "" {
		apiURL = "http://127.0.0.1:5001"
	}
	topic := strings.TrimSpace(os.Getenv("IPFS_MIRROR_TOPIC"))
	if topic == "" {
		topic = "stargate-uploads"
	}
	httpTimeout := 30 * time.Second
	if raw := strings.TrimSpace(os.Getenv("IPFS_HTTP_TIMEOUT_SEC")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			httpTimeout = time.Duration(v) * time.Second
		}
	}
	reconcileRecentBlocks := 6
	reconcileMinInterval := 5 * time.Minute

	// Native support: check if local IPFS node is reachable for pubsub
	if enabled {
		client := ipfs.NewClientFromEnv()
		if client == nil {
			enabled = false
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := client.CheckNode(ctx); err != nil {
				log.Printf("ipfs ingestion sync: local IPFS node not reachable (%v), disabling pubsub sync", err)
				enabled = false
			}
		}
	}

	return ipfsIngestSyncConfig{
		Enabled:               enabled,
		Interval:              interval,
		MaxEntries:            maxEntries,
		APIURL:                apiURL,
		Topic:                 topic,
		HTTPTimeout:           httpTimeout,
		ReconcileRecentBlocks: reconcileRecentBlocks,
		ReconcileMinInterval:  reconcileMinInterval,
	}
}

func ipfsIngestSubscribe(ctx context.Context, ingest *services.IngestionService, store Store, reconcileFn IngestReconcileFunc, cfg ipfsIngestSyncConfig, state *ipfsIngestSyncState, client *ipfs.Client) error {
	// Use the IPFS client's PubsubSubscribe (routes through embedded node when available)
	ch, err := client.PubsubSubscribe(ctx, cfg.Topic)
	if err != nil {
		return err
	}

	for data := range ch {
		// The channel delivers raw message payloads; try to decode as pubsub wrapper first
		var dataStr string
		var msg pubsubMessage
		if json.Unmarshal(data, &msg) == nil && msg.Data != "" {
			dataStr = msg.Data
		} else {
			dataStr = string(data)
		}

		announcement, err := parsePendingIngestAnnouncement(dataStr)
		if err != nil {
			log.Printf("ipfs ingestion sync message decode failed: %v", err)
			continue
		}
		if announcement != nil && strings.TrimSpace(announcement.ImageCID) != "" {
			if err := ipfsIngestProcessPending(ctx, ingest, state, client, announcement); err != nil {
				log.Printf("ipfs ingestion sync pending failed: %v", err)
			}
			continue
		}
		update, err := parseIngestUpdateAnnouncement(dataStr)
		if err != nil {
			log.Printf("ipfs ingestion sync message decode failed: %v", err)
			continue
		}
		if update != nil {
			if err := enqueueIngestUpdate(ctx, ingest, state, update); err != nil {
				log.Printf("ipfs ingestion sync update enqueue failed: %v", err)
			}
			signalIngestUpdateQueue(state)
			continue
		}
		manifestCID, err := extractManifestCID(dataStr)
		if err != nil {
			log.Printf("ipfs ingestion sync message decode failed: %v", err)
			continue
		}
		if manifestCID == "" || manifestCID == state.lastManifest {
			continue
		}
		if err := ipfsIngestProcessManifest(ctx, ingest, store, cfg, state, client, manifestCID); err != nil {
			log.Printf("ipfs ingestion sync failed: %v", err)
			continue
		}
		state.lastManifest = manifestCID
	}
	return nil
}

func ipfsIngestProcessManifest(ctx context.Context, ingest *services.IngestionService, store Store, cfg ipfsIngestSyncConfig, state *ipfsIngestSyncState, client *ipfs.Client, manifestCID string) error {
	data, err := client.Cat(ctx, manifestCID)
	if err != nil {
		return err
	}

	var manifest ipfsIngestManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	entries := manifest.Files
	if cfg.MaxEntries > 0 && len(entries) > cfg.MaxEntries {
		entries = entries[len(entries)-cfg.MaxEntries:]
	}

	reconcileCfg := loadStegoReconcileConfig()
	var processed int
	for _, entry := range entries {
		if entry.CID == "" || entry.Path == "" {
			continue
		}
		if !isImageFile(entry.Path) {
			continue
		}
		if last, ok := state.lastSeen[entry.CID]; ok && entry.ModTime <= last {
			if state.repairChecked[entry.CID] {
				continue
			}
			state.repairChecked[entry.CID] = true
		}

		blob, err := client.Cat(ctx, entry.CID)
		if err != nil {
			continue
		}
		rawBytes, err := extractStegoManifest(ctx, blob, reconcileCfg)
		if err != nil {
			log.Printf("ipfs ingestion sync: stego extraction failed for %s (%s): %v", entry.CID, entry.Path, err)
			state.lastSeen[entry.CID] = entry.ModTime
			continue
		}
		stegoManifest, payload, err := stego.ParseEmbedded(rawBytes)
		if err != nil {
			// Not a manifest — likely a plain-text wish message embedded via stego.
			state.lastSeen[entry.CID] = entry.ModTime
			continue
		}
		id := strings.TrimSpace(stegoManifest.VisiblePixelHash)
		if id == "" {
			id = strings.TrimSpace(stegoManifest.ProposalID)
		}
		if id == "" {
			state.lastSeen[entry.CID] = entry.ModTime
			continue
		}
		// v1 fallback: payload was not inline — fetch from IPFS
		if payload.SchemaVersion == 0 && stegoManifest.PayloadCID != "" {
			if loaded, err := fetchStegoPayload(ctx, client, stegoManifest.PayloadCID); err != nil {
				log.Printf("ipfs ingestion sync payload fetch failed: %v", err)
			} else {
				payload = loaded
			}
		}
		payloadMeta := payloadMetadataMap(payload)
		if store != nil {
			if err := ensureProposalFromStegoPayload(ctx, store, entry.CID, stegoManifest, payload); err != nil {
				log.Printf("ipfs ingestion sync proposal upsert failed: %v", err)
			}
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
			for k, v := range payloadMeta {
				if _, ok := metaUpdates[k]; !ok {
					metaUpdates[k] = v
				}
			}
			_ = ingest.UpdateMetadata(id, metaUpdates)
			state.lastSeen[entry.CID] = entry.ModTime
			processed++
			continue
		}

		rec := services.IngestionRecord{
			ID:            id,
			Filename:      filepath.Base(entry.Path),
			Method:        getStegoMethodForFilename(entry.Path),
			MessageLength: len(rawBytes),
			ImageBase64:   base64.StdEncoding.EncodeToString(blob),
			Metadata: map[string]interface{}{
				"stego_image_cid":            entry.CID,
				"stego_payload_cid":          stegoManifest.PayloadCID,
				"stego_manifest_issuer":      stegoManifest.Issuer,
				"stego_manifest_created_at":  stegoManifest.CreatedAt,
				"stego_manifest_proposal_id": stegoManifest.ProposalID,
				"visible_pixel_hash":         stegoManifest.VisiblePixelHash,
			},
			Status: "verified",
		}
		for k, v := range payloadMeta {
			if _, ok := rec.Metadata[k]; !ok {
				rec.Metadata[k] = v
			}
		}
		if err := ingest.Create(rec); err != nil {
			log.Printf("ipfs ingestion create failed for %s: %v", id, err)
		} else {
			processed++
		}
		state.lastSeen[entry.CID] = entry.ModTime
	}
	if processed > 0 {
		log.Printf("ipfs ingestion sync: manifest=%s processed=%d", manifestCID, processed)
	}
	return nil
}

func fetchStegoPayload(ctx context.Context, client *ipfs.Client, payloadCID string) (stego.Payload, error) {
	payloadCID = strings.TrimSpace(payloadCID)
	if payloadCID == "" {
		return stego.Payload{}, fmt.Errorf("payload_cid missing")
	}
	data, err := client.Cat(ctx, payloadCID)
	if err != nil {
		return stego.Payload{}, err
	}
	var payload stego.Payload
	if err := json.Unmarshal(data, &payload); err != nil {
		return stego.Payload{}, fmt.Errorf("payload decode failed: %w", err)
	}
	return payload, nil
}

func ensureProposalFromStegoPayload(ctx context.Context, store Store, stegoCID string, manifest stego.Manifest, payload stego.Payload) error {
	if store == nil {
		return nil
	}
	proposalID := strings.TrimSpace(payload.Proposal.ID)
	if proposalID == "" {
		proposalID = strings.TrimSpace(manifest.ProposalID)
	}
	visibleHash := strings.TrimSpace(manifest.VisiblePixelHash)
	if visibleHash == "" {
		visibleHash = strings.TrimSpace(payload.Proposal.VisiblePixelHash)
	}
	if visibleHash != "" {
		proposalID = visibleHash
	}
	if proposalID == "" && visibleHash != "" {
		proposalID = "proposal-" + visibleHash
	}
	if proposalID == "" {
		return fmt.Errorf("proposal id missing")
	}
	if existing, err := store.GetProposal(ctx, proposalID); err == nil && strings.TrimSpace(existing.ID) != "" {
		updates := map[string]interface{}{
			"stego_image_cid":            stegoCID,
			"stego_payload_cid":          manifest.PayloadCID,
			"stego_manifest_issuer":      manifest.Issuer,
			"stego_manifest_created_at":  manifest.CreatedAt,
			"stego_manifest_proposal_id": manifest.ProposalID,
			"stego_manifest_schema":      manifest.SchemaVersion,
			"origin_proposal_id":         manifest.ProposalID,
		}
		if visibleHash != "" {
			updates["visible_pixel_hash"] = visibleHash
			updates["contract_id"] = visibleHash
			updates["ingestion_id"] = visibleHash
		}
		for k, v := range payloadMetadataMap(payload) {
			if _, ok := updates[k]; !ok {
				updates[k] = v
			}
		}
		_ = store.UpdateProposalMetadata(ctx, proposalID, updates)
		return nil
	}
	if visibleHash != "" {
		if _, err := store.GetContract("wish-" + visibleHash); err != nil {
			return nil
		}
	}
	title := strings.TrimSpace(payload.Proposal.Title)
	if title == "" {
		title = "Proposal " + proposalID
	}
	createdAt := time.Now()
	if payload.Proposal.CreatedAt > 0 {
		createdAt = time.Unix(payload.Proposal.CreatedAt, 0)
	}
	contractID := proposalID
	if visibleHash != "" {
		contractID = visibleHash
	}
	tasks := make([]smart_contract.Task, 0, len(payload.Tasks))
	for _, t := range payload.Tasks {
		if strings.TrimSpace(t.TaskID) == "" {
			continue
		}
		tasks = append(tasks, smart_contract.Task{
			TaskID:           t.TaskID,
			ContractID:       contractID,
			GoalID:           "wish",
			Title:            t.Title,
			Description:      t.Description,
			BudgetSats:       t.BudgetSats,
			Skills:           t.Skills,
			Status:           "available",
			ContractorWallet: t.ContractorWallet,
		})
	}
	meta := map[string]interface{}{
		"stego_image_cid":            stegoCID,
		"stego_payload_cid":          manifest.PayloadCID,
		"stego_manifest_issuer":      manifest.Issuer,
		"stego_manifest_created_at":  manifest.CreatedAt,
		"stego_manifest_proposal_id": manifest.ProposalID,
		"stego_manifest_schema":      manifest.SchemaVersion,
		"origin_proposal_id":         manifest.ProposalID,
		"visible_pixel_hash":         visibleHash,
	}
	if visibleHash != "" {
		meta["contract_id"] = visibleHash
		meta["ingestion_id"] = visibleHash
	}
	for k, v := range payloadMetadataMap(payload) {
		if _, ok := meta[k]; !ok {
			meta[k] = v
		}
	}
	proposal := smart_contract.Proposal{
		ID:               proposalID,
		Title:            title,
		DescriptionMD:    payload.Proposal.DescriptionMD,
		VisiblePixelHash: visibleHash,
		BudgetSats:       payload.Proposal.BudgetSats,
		Status:           "approved",
		CreatedAt:        createdAt,
		Tasks:            tasks,
		Metadata:         meta,
	}
	return store.CreateProposal(ctx, proposal)
}

func payloadMetadataMap(payload stego.Payload) map[string]interface{} {
	if len(payload.Metadata) == 0 {
		return nil
	}
	meta := make(map[string]interface{}, len(payload.Metadata))
	for _, entry := range payload.Metadata {
		key := strings.TrimSpace(entry.Key)
		val := strings.TrimSpace(entry.Value)
		if key == "" || val == "" {
			continue
		}
		meta[key] = val
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func ipfsIngestProcessPending(ctx context.Context, ingest *services.IngestionService, state *ipfsIngestSyncState, client *ipfs.Client, ann *pendingIngestAnnouncement) error {
	if ann == nil || strings.TrimSpace(ann.ImageCID) == "" {
		return nil
	}
	seenAt := ann.Timestamp
	if seenAt <= 0 {
		seenAt = time.Now().Unix()
	}
	if last, ok := state.lastSeen[ann.ImageCID]; ok && seenAt <= last {
		return nil
	}
	imageBytes, err := client.Cat(ctx, ann.ImageCID)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(ann.IngestionID)
	if id == "" {
		id = strings.TrimSpace(ann.VisiblePixelHash)
	}
	if id == "" {
		state.lastSeen[ann.ImageCID] = seenAt
		return nil
	}
	meta := map[string]interface{}{
		"embedded_message":   ann.Message,
		"message":            ann.Message,
		"price":              ann.Price,
		"price_unit":         ann.PriceUnit,
		"address":            ann.Address,
		"funding_mode":       ann.FundingMode,
		"ingestion_id":       id,
		"visible_pixel_hash": ann.VisiblePixelHash,
		"ipfs_image_cid":     ann.ImageCID,
	}
	method := strings.ToLower(strings.TrimSpace(ann.Method))
	if method == "stego" || method == "alpha" || method == "lsb" || method == "exif" || method == "palette" ||
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(ann.Filename)), "stego") {
		meta["stego_image_cid"] = ann.ImageCID
	}
	if strings.EqualFold(ann.PriceUnit, "sats") {
		meta["budget_sats"] = priceSatsFromString(ann.Price)
	}
	// Store proposal/task data from announcement for peer replication
	if ann.ProposalTitle != "" {
		meta["proposal_title"] = ann.ProposalTitle
	}
	if ann.ProposalDesc != "" {
		meta["proposal_description_md"] = ann.ProposalDesc
	}
	if ann.BudgetSats > 0 {
		meta["proposal_budget_sats"] = fmt.Sprintf("%d", ann.BudgetSats)
	}
	if ann.PayloadCID != "" {
		meta["stego_payload_cid"] = ann.PayloadCID
	}
	if len(ann.Tasks) > 0 {
		if taskJSON, err := json.Marshal(ann.Tasks); err == nil {
			meta["proposal_tasks_json"] = string(taskJSON)
		}
	}
	// Write wish image to disk so the /uploads/ endpoint can serve it.
	uploadsDir := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}
	_ = os.MkdirAll(uploadsDir, 0755)
	uploadPath := filepath.Join(uploadsDir, id)
	if _, statErr := os.Stat(uploadPath); statErr != nil {
		if writeErr := os.WriteFile(uploadPath, imageBytes, 0644); writeErr != nil {
			log.Printf("ipfs ingestion sync: failed to write wish image to %s: %v", uploadPath, writeErr)
		} else {
			log.Printf("ipfs ingestion sync: wrote wish image to %s (%d bytes)", uploadPath, len(imageBytes))
		}
	}

	if existing, err := ingest.Get(id); err == nil && existing != nil {
		_ = ingest.UpdateMetadata(id, meta)
		state.lastSeen[ann.ImageCID] = seenAt
		return nil
	}
	rec := services.IngestionRecord{
		ID:            id,
		Filename:      ann.Filename,
		Method:        ann.Method,
		MessageLength: len(ann.Message),
		ImageBase64:   base64.StdEncoding.EncodeToString(imageBytes),
		Metadata:      meta,
		Status:        "pending",
	}
	if rec.Filename == "" {
		rec.Filename = "inscription.png"
	}
	if err := ingest.Create(rec); err != nil {
		log.Printf("ipfs ingestion create failed for %s: %v", id, err)
	} else {
		log.Printf("ipfs ingestion sync: pending=%s", id)
	}
	state.lastSeen[ann.ImageCID] = seenAt
	return nil
}

const ingestUpdateBatchSize = 25
const ingestUpdateMaxAttempts = 10

func enqueueIngestUpdate(ctx context.Context, ingest *services.IngestionService, state *ipfsIngestSyncState, ann *ingestUpdateAnnouncement) error {
	if ann == nil || ingest == nil || state == nil {
		return nil
	}
	id := strings.TrimSpace(ann.IngestionID)
	if id == "" {
		id = strings.TrimSpace(ann.VisiblePixelHash)
	}
	if id == "" {
		return nil
	}
	seenAt := ann.Timestamp
	if seenAt <= 0 {
		seenAt = time.Now().Unix()
	}
	seenKey := "update:" + id
	if last, ok := state.lastSeen[seenKey]; ok && seenAt <= last {
		return nil
	}
	payload, err := json.Marshal(ann)
	if err != nil {
		return err
	}
	if err := ingest.EnqueueIngestUpdate(ctx, id, strings.TrimSpace(ann.VisiblePixelHash), strings.TrimSpace(ann.ProposalID), payload); err != nil {
		return err
	}
	return nil
}

// getStegoMethodForFilename determines appropriate steganography method based on image format
func getStegoMethodForFilename(filename string) string {
	// Default to lsb if we can't determine format
	defaultMethod := "lsb"

	// Try to determine from file extension
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png":
		return "alpha"
	case ".jpg", ".jpeg":
		return "exif"
	case ".gif":
		return "palette"
	}

	return defaultMethod
}

func signalIngestUpdateQueue(state *ipfsIngestSyncState) {
	if state == nil || state.queueWake == nil {
		return
	}
	select {
	case state.queueWake <- struct{}{}:
	default:
	}
}

func ipfsIngestProcessUpdateQueue(ctx context.Context, ingest *services.IngestionService, store Store, reconcileFn IngestReconcileFunc, cfg ipfsIngestSyncConfig, state *ipfsIngestSyncState) {
	if ingest == nil {
		return
	}
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-state.queueWake:
		}
		updated, err := processIngestUpdateBatch(ctx, ingest, store)
		if err != nil {
			log.Printf("ipfs ingestion sync update retry failed: %v", err)
		}
		if updated {
			maybeTriggerReconcile(ctx, reconcileFn, cfg, state)
		}
	}
}

func processIngestUpdateBatch(ctx context.Context, ingest *services.IngestionService, store Store) (bool, error) {
	rows, err := ingest.ClaimIngestUpdates(ctx, ingestUpdateBatchSize)
	if err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	anyUpdated := false
	for _, row := range rows {
		if row.Attempts > ingestUpdateMaxAttempts {
			_ = ingest.MarkIngestUpdateFailed(ctx, row.ID, "max retries exceeded")
			continue
		}
		var ann ingestUpdateAnnouncement
		if err := json.Unmarshal(row.Payload, &ann); err != nil {
			_ = ingest.MarkIngestUpdateFailed(ctx, row.ID, "invalid payload")
			continue
		}
		applied, err := applyIngestUpdate(ctx, ingest, store, &ann)
		if err != nil {
			delay := ingestUpdateRetryDelay(row.Attempts)
			_ = ingest.MarkIngestUpdateRetry(ctx, row.ID, err.Error(), delay)
			continue
		}
		if !applied {
			delay := ingestUpdateRetryDelay(row.Attempts)
			_ = ingest.MarkIngestUpdateRetry(ctx, row.ID, "ingestion/proposal missing", delay)
			continue
		}
		_ = ingest.MarkIngestUpdateApplied(ctx, row.ID)
		anyUpdated = true
	}
	return anyUpdated, nil
}

func applyIngestUpdate(ctx context.Context, ingest *services.IngestionService, store Store, ann *ingestUpdateAnnouncement) (bool, error) {
	if ann == nil {
		return false, nil
	}
	id := strings.TrimSpace(ann.IngestionID)
	if id == "" {
		id = strings.TrimSpace(ann.VisiblePixelHash)
	}
	if id == "" {
		return false, nil
	}

	meta := make(map[string]interface{})
	if v := strings.TrimSpace(ann.VisiblePixelHash); v != "" {
		meta["visible_pixel_hash"] = v
	}
	if v := strings.TrimSpace(ann.FundingTxID); v != "" {
		meta["funding_txid"] = v
	}
	if len(ann.FundingTxIDs) > 0 {
		meta["funding_txids"] = ann.FundingTxIDs
	}
	if v := strings.TrimSpace(ann.CommitmentLockAddr); v != "" {
		meta["commitment_lock_address"] = v
	}
	if v := strings.TrimSpace(ann.CommitmentTarget); v != "" {
		meta["commitment_target"] = v
	}
	if v := strings.TrimSpace(ann.CommitmentAddress); v != "" {
		meta["commitment_address"] = v
	}
	if v := strings.TrimSpace(ann.CommitmentScript); v != "" {
		meta["commitment_script"] = v
	}
	if ann.CommitmentVout > 0 {
		meta["commitment_vout"] = ann.CommitmentVout
	}
	if ann.CommitmentSats > 0 {
		meta["commitment_sats"] = ann.CommitmentSats
	}
	if v := strings.TrimSpace(ann.PayoutScript); v != "" {
		meta["payout_script"] = v
	}
	if len(ann.PayoutScripts) > 0 {
		meta["payout_scripts"] = ann.PayoutScripts
	}
	if len(ann.PayoutScriptHashes) > 0 {
		meta["payout_script_hashes"] = ann.PayoutScriptHashes
	}
	if len(ann.PayoutScriptHash160s) > 0 {
		meta["payout_script_hash160s"] = ann.PayoutScriptHash160s
	}
	if len(meta) == 0 {
		return false, fmt.Errorf("empty ingest update")
	}

	updated := false
	var rec *services.IngestionRecord
	if ingest != nil {
		if existing, err := ingest.Get(id); err == nil && existing != nil {
			rec = existing
			_ = ingest.UpdateMetadata(id, meta)
			updated = true
		}
	}

	if store != nil {
		proposalIDs := resolveProposalIDsForIngestUpdate(ann, rec)
		for _, proposalID := range proposalIDs {
			if proposalID == "" {
				continue
			}
			if existing, err := store.GetProposal(ctx, proposalID); err == nil && strings.TrimSpace(existing.ID) != "" {
				_ = store.UpdateProposalMetadata(ctx, proposalID, meta)
				updated = true
			}
		}
	}

	return updated, nil
}

func ingestUpdateRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 10 * time.Second
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= 15*time.Minute {
			return 15 * time.Minute
		}
	}
	return delay
}

func maybeTriggerReconcile(ctx context.Context, reconcileFn IngestReconcileFunc, cfg ipfsIngestSyncConfig, state *ipfsIngestSyncState) {
	if reconcileFn == nil || cfg.ReconcileRecentBlocks <= 0 {
		return
	}
	if !state.lastReconcile.IsZero() && time.Since(state.lastReconcile) < cfg.ReconcileMinInterval {
		return
	}
	state.lastReconcile = time.Now()
	if err := reconcileFn(ctx, cfg.ReconcileRecentBlocks); err != nil {
		log.Printf("ipfs ingestion sync reconcile failed: %v", err)
	}
}

func resolveProposalIDsForIngestUpdate(ann *ingestUpdateAnnouncement, rec *services.IngestionRecord) []string {
	out := make([]string, 0, 3)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range out {
			if existing == value {
				return
			}
		}
		out = append(out, value)
	}
	if ann != nil {
		add(ann.ProposalID)
		if ann.VisiblePixelHash != "" {
			add("proposal-" + ann.VisiblePixelHash)
		}
	}
	if rec != nil && rec.Metadata != nil {
		if v, ok := rec.Metadata["stego_manifest_proposal_id"].(string); ok {
			add(v)
		}
		if v, ok := rec.Metadata["origin_proposal_id"].(string); ok {
			add(v)
		}
	}
	return out
}

func extractManifestCID(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}

	candidates := make([][]byte, 0, 4)
	candidates = append(candidates, []byte(encoded))
	if decoded := decodeMultibasePayload([]byte(encoded)); len(decoded) > 0 {
		candidates = append(candidates, decoded)
	}
	if decoded := decodeBase64Payload(encoded); len(decoded) > 0 {
		candidates = append(candidates, decoded)
		if decoded2 := decodeMultibasePayload(decoded); len(decoded2) > 0 {
			candidates = append(candidates, decoded2)
		}
	}

	for _, payload := range candidates {
		if cid := parseAnnouncementPayload(payload); cid != "" {
			return cid, nil
		}
	}

	return "", nil
}

func parseAnnouncementPayload(payload []byte) string {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return ""
	}

	var ann pubsubAnnouncement
	if err := json.Unmarshal(payload, &ann); err == nil && ann.ManifestCID != "" {
		return ann.ManifestCID
	}

	return ""
}

func parsePendingIngestAnnouncement(encoded string) (*pendingIngestAnnouncement, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}
	candidates := make([][]byte, 0, 3)
	candidates = append(candidates, []byte(encoded))
	if decoded := decodeMultibasePayload([]byte(encoded)); len(decoded) > 0 {
		candidates = append(candidates, decoded)
	}
	if decoded := decodeBase64Payload(encoded); len(decoded) > 0 {
		candidates = append(candidates, decoded)
		if decoded2 := decodeMultibasePayload(decoded); len(decoded2) > 0 {
			candidates = append(candidates, decoded2)
		}
	}
	for _, payload := range candidates {
		payload = bytes.TrimSpace(payload)
		if len(payload) == 0 {
			continue
		}
		var ann pendingIngestAnnouncement
		if err := json.Unmarshal(payload, &ann); err == nil && ann.Type == "pending_ingest" {
			return &ann, nil
		}
	}
	return nil, nil
}

func priceSatsFromString(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if strings.Contains(raw, ".") {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			return int64(v * 1e8)
		}
		return 0
	}
	if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return v
	}
	return 0
}

func parseIngestUpdateAnnouncement(encoded string) (*ingestUpdateAnnouncement, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}
	candidates := make([][]byte, 0, 3)
	candidates = append(candidates, []byte(encoded))
	if decoded := decodeMultibasePayload([]byte(encoded)); len(decoded) > 0 {
		candidates = append(candidates, decoded)
	}
	if decoded := decodeBase64Payload(encoded); len(decoded) > 0 {
		candidates = append(candidates, decoded)
		if decoded2 := decodeMultibasePayload(decoded); len(decoded2) > 0 {
			candidates = append(candidates, decoded2)
		}
	}
	for _, payload := range candidates {
		payload = bytes.TrimSpace(payload)
		if len(payload) == 0 {
			continue
		}
		var ann ingestUpdateAnnouncement
		if err := json.Unmarshal(payload, &ann); err == nil && ann.Type == "ingest_update" {
			return &ann, nil
		}
	}
	return nil, nil
}

func multibaseEncodeString(value string) string {
	return multibaseEncodeBytes([]byte(value))
}

func multibaseEncodeBytes(value []byte) string {
	encoded := base64.RawURLEncoding.EncodeToString(value)
	return "u" + encoded
}

func decodeMultibasePayload(raw []byte) []byte {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != 'u' {
		return nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(string(raw[1:]))
	if err != nil {
		return nil
	}
	return decoded
}

func decodeBase64Payload(encoded string) []byte {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, enc := range encodings {
		if decoded, err := enc.DecodeString(encoded); err == nil {
			return decoded
		}
	}
	return nil
}

func isImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	case "":
		// Stego images in uploads are hash-named without extensions
		// (e.g. "341a6c2b..."). Treat extensionless files as potential
		// images so the stego extraction pipeline can inspect them.
		return true
	default:
		return false
	}
}
