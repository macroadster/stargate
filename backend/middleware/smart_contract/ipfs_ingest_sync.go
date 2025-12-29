package smart_contract

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
	"stargate-backend/ipfs"
	"stargate-backend/services"
	"stargate-backend/stego"
)

type ipfsIngestSyncConfig struct {
	Enabled     bool
	Interval    time.Duration
	MaxEntries  int
	APIURL      string
	Topic       string
	HTTPTimeout time.Duration
}

type ipfsIngestSyncState struct {
	lastSeen     map[string]int64
	lastManifest string
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
	VisiblePixelHash string `json:"visible_pixel_hash,omitempty"`
	ImageCID         string `json:"image_cid"`
	Filename         string `json:"filename,omitempty"`
	Method           string `json:"method,omitempty"`
	Message          string `json:"message,omitempty"`
	Price            string `json:"price,omitempty"`
	Address          string `json:"address,omitempty"`
	FundingMode      string `json:"funding_mode,omitempty"`
	Timestamp        int64  `json:"timestamp"`
}

// StartIPFSIngestionSync subscribes to IPFS mirror announcements and creates ingestion records for stego images.
func StartIPFSIngestionSync(ctx context.Context, ingest *services.IngestionService, store Store) error {
	if ingest == nil {
		return fmt.Errorf("ipfs ingestion sync requires ingestion service")
	}
	cfg := loadIPFSIngestSyncConfig()
	if !cfg.Enabled {
		return nil
	}
	state := &ipfsIngestSyncState{lastSeen: make(map[string]int64)}
	client := ipfs.NewClientFromEnv()
	streamClient := &http.Client{}
	go func() {
		for {
			if err := ipfsIngestSubscribe(ctx, ingest, store, cfg, state, client, streamClient); err != nil {
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
	enabled := strings.EqualFold(strings.TrimSpace(os.Getenv("IPFS_INGEST_SYNC_ENABLED")), "true")
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
	return ipfsIngestSyncConfig{
		Enabled:     enabled,
		Interval:    interval,
		MaxEntries:  maxEntries,
		APIURL:      apiURL,
		Topic:       topic,
		HTTPTimeout: httpTimeout,
	}
}

func ipfsIngestSubscribe(ctx context.Context, ingest *services.IngestionService, store Store, cfg ipfsIngestSyncConfig, state *ipfsIngestSyncState, client *ipfs.Client, streamClient *http.Client) error {
	reqURL := fmt.Sprintf("%s/api/v0/pubsub/sub?arg=%s", strings.TrimRight(cfg.APIURL, "/"), url.QueryEscape(multibaseEncodeString(cfg.Topic)))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return err
	}

	resp, err := streamClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if len(body) == 0 {
			return fmt.Errorf("pubsub subscribe failed: %s", resp.Status)
		}
		return fmt.Errorf("pubsub subscribe failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	decoder := json.NewDecoder(resp.Body)
	for {
		var msg pubsubMessage
		if err := decoder.Decode(&msg); err != nil {
			return err
		}
		announcement, err := parsePendingIngestAnnouncement(msg.Data)
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
		manifestCID, err := extractManifestCID(msg.Data)
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
			continue
		}

		blob, err := client.Cat(ctx, entry.CID)
		if err != nil {
			continue
		}
		manifestBytes, err := extractStegoManifest(ctx, blob, reconcileCfg)
		if err != nil {
			state.lastSeen[entry.CID] = entry.ModTime
			continue
		}
		stegoManifest, err := stego.ParseManifestYAML(manifestBytes)
		if err != nil {
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
		if store != nil {
			payload, err := fetchStegoPayload(ctx, client, stegoManifest.PayloadCID)
			if err != nil {
				log.Printf("ipfs ingestion sync payload fetch failed: %v", err)
			} else if err := ensureProposalFromStegoPayload(ctx, store, entry.CID, stegoManifest, payload); err != nil {
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
			_ = ingest.UpdateMetadata(id, metaUpdates)
			state.lastSeen[entry.CID] = entry.ModTime
			processed++
			continue
		}

		rec := services.IngestionRecord{
			ID:            id,
			Filename:      filepath.Base(entry.Path),
			Method:        "stego",
			MessageLength: len(manifestBytes),
			ImageBase64:   base64.StdEncoding.EncodeToString(blob),
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
	if proposalID == "" {
		if vph := strings.TrimSpace(manifest.VisiblePixelHash); vph != "" {
			proposalID = "proposal-" + vph
		}
	}
	if proposalID == "" {
		return fmt.Errorf("proposal id missing")
	}
	if existing, err := store.GetProposal(ctx, proposalID); err == nil && strings.TrimSpace(existing.ID) != "" {
		return nil
	}
	title := strings.TrimSpace(payload.Proposal.Title)
	if title == "" {
		title = "Proposal " + proposalID
	}
	createdAt := time.Now()
	if payload.Proposal.CreatedAt > 0 {
		createdAt = time.Unix(payload.Proposal.CreatedAt, 0)
	}
	tasks := make([]smart_contract.Task, 0, len(payload.Tasks))
	for _, t := range payload.Tasks {
		if strings.TrimSpace(t.TaskID) == "" {
			continue
		}
		tasks = append(tasks, smart_contract.Task{
			TaskID:           t.TaskID,
			ContractID:       proposalID,
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
		"visible_pixel_hash":         manifest.VisiblePixelHash,
	}
	proposal := smart_contract.Proposal{
		ID:               proposalID,
		Title:            title,
		DescriptionMD:    payload.Proposal.DescriptionMD,
		VisiblePixelHash: manifest.VisiblePixelHash,
		BudgetSats:       payload.Proposal.BudgetSats,
		Status:           "approved",
		CreatedAt:        createdAt,
		Tasks:            tasks,
		Metadata:         meta,
	}
	return store.CreateProposal(ctx, proposal)
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
		"address":            ann.Address,
		"funding_mode":       ann.FundingMode,
		"ingestion_id":       id,
		"visible_pixel_hash": ann.VisiblePixelHash,
		"ipfs_image_cid":     ann.ImageCID,
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
	default:
		return false
	}
}
