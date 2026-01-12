package smart_contract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
	"stargate-backend/ipfs"
	"stargate-backend/services"
	"stargate-backend/stego"
	scstore "stargate-backend/storage/smart_contract"
)

type stegoApprovalConfig struct {
	Enabled         bool
	ProxyBase       string
	APIKey          string
	DefaultMethod   string
	Issuer          string
	AnnounceEnabled bool
	AnnounceTopic   string
	IngestPoll      time.Duration
	IngestTimeout   time.Duration
	InscribeTimeout time.Duration
	PayloadSchema   int
	ManifestSchema  int
	PayloadMaxTasks int
}

type stegoPayload = stego.Payload
type stegoProposalPayload = stego.PayloadProposal
type stegoTaskPayload = stego.PayloadTask
type stegoMetadataEntry = stego.MetadataEntry

type stegoAnnouncement struct {
	Type             string `json:"type"`
	StegoCID         string `json:"stego_cid"`
	ExpectedHash     string `json:"expected_hash,omitempty"`
	ProposalID       string `json:"proposal_id,omitempty"`
	VisiblePixelHash string `json:"visible_pixel_hash,omitempty"`
	PayloadCID       string `json:"payload_cid,omitempty"`
	Issuer           string `json:"issuer,omitempty"`
	Timestamp        int64  `json:"timestamp"`
}

func loadStegoApprovalConfig() stegoApprovalConfig {
	enabled := strings.EqualFold(strings.TrimSpace(os.Getenv("STARGATE_STEGO_APPROVAL_ENABLED")), "true")
	proxyBase := strings.TrimSpace(os.Getenv("STARGATE_PROXY_BASE"))
	if proxyBase == "" {
		proxyBase = "http://localhost:8080"
	}
	method := strings.TrimSpace(os.Getenv("STARGATE_STEGO_METHOD"))
	if method == "" {
		method = "lsb"
	}
	announceTopic := strings.TrimSpace(os.Getenv("IPFS_STEGO_TOPIC"))
	if announceTopic == "" {
		announceTopic = "stargate-stego"
	}
	announceEnabled := enabled
	if raw := strings.TrimSpace(os.Getenv("IPFS_STEGO_ANNOUNCE_ENABLED")); raw != "" {
		announceEnabled = strings.EqualFold(raw, "true")
	}
	issuer := strings.TrimSpace(os.Getenv("STARGATE_STEGO_ISSUER"))
	if issuer == "" {
		issuer = strings.TrimSpace(os.Getenv("STARGATE_INSTANCE_ID"))
	}
	if issuer == "" {
		if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
			issuer = host
		} else {
			issuer = "stargate"
		}
	}
	ingestPoll := 2 * time.Second
	if raw := strings.TrimSpace(os.Getenv("STARGATE_STEGO_INGEST_POLL_SEC")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			ingestPoll = time.Duration(v) * time.Second
		}
	}
	ingestTimeout := 30 * time.Second
	if raw := strings.TrimSpace(os.Getenv("STARGATE_STEGO_INGEST_TIMEOUT_SEC")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			ingestTimeout = time.Duration(v) * time.Second
		}
	}
	inscribeTimeout := 60 * time.Second
	if raw := strings.TrimSpace(os.Getenv("STARGATE_STEGO_INSCRIBE_TIMEOUT_SEC")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			inscribeTimeout = time.Duration(v) * time.Second
		}
	}
	return stegoApprovalConfig{
		Enabled:         enabled,
		ProxyBase:       proxyBase,
		APIKey:          strings.TrimSpace(os.Getenv("STARGATE_API_KEY")),
		DefaultMethod:   method,
		Issuer:          issuer,
		AnnounceEnabled: announceEnabled,
		AnnounceTopic:   announceTopic,
		IngestPoll:      ingestPoll,
		IngestTimeout:   ingestTimeout,
		InscribeTimeout: inscribeTimeout,
		PayloadSchema:   1,
		ManifestSchema:  1,
		PayloadMaxTasks: 2000,
	}
}

func (s *Server) maybePublishStegoForProposal(ctx context.Context, proposalID string) error {
	cfg := loadStegoApprovalConfig()
	if !cfg.Enabled {
		return nil
	}
	timeout := cfg.InscribeTimeout + cfg.IngestTimeout + (5 * time.Second)
	runCtx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, timeout)
		defer cancel()
	}
	if err := s.publishStegoForProposal(runCtx, proposalID, cfg); err != nil {
		log.Printf("stego approval publish failed for proposal %s: %v", proposalID, err)
		return err
	}
	return nil
}

func (s *Server) publishStegoForProposal(ctx context.Context, proposalID string, cfg stegoApprovalConfig) error {
	if s.ingestionSvc == nil {
		return fmt.Errorf("ingestion service not configured")
	}
	if proposalID == "" {
		return fmt.Errorf("proposal id missing")
	}
	p, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return err
	}
	meta := copyMeta(p.Metadata)
	if meta == nil {
		meta = map[string]interface{}{}
	}
	commitmentLock := strings.TrimSpace(toString(meta["commitment_lock_address"]))
	stegoCommitmentLock := strings.TrimSpace(toString(meta["stego_commitment_lock_address"]))
	if strings.TrimSpace(toString(meta["stego_contract_id"])) != "" &&
		strings.TrimSpace(toString(meta["stego_image_cid"])) != "" &&
		(commitmentLock == "" || commitmentLock == stegoCommitmentLock) {
		return nil
	}
	ingestionID := strings.TrimSpace(toString(meta["ingestion_id"]))
	if ingestionID == "" {
		visible := strings.TrimSpace(p.VisiblePixelHash)
		if visible == "" {
			visible = strings.TrimSpace(toString(meta["visible_pixel_hash"]))
		}
		ingestionID = visible
		if ingestionID != "" {
			meta["ingestion_id"] = ingestionID
		} else {
			return fmt.Errorf("proposal %s missing ingestion_id metadata", proposalID)
		}
	}
	coverRec, err := s.ingestionSvc.Get(ingestionID)
	if err != nil || coverRec == nil {
		return fmt.Errorf("failed to load ingestion %s: %w", ingestionID, err)
	}
	if strings.TrimSpace(coverRec.ImageBase64) == "" {
		return fmt.Errorf("ingestion %s missing image data", ingestionID)
	}
	coverBytes, err := base64.StdEncoding.DecodeString(coverRec.ImageBase64)
	if err != nil {
		return fmt.Errorf("failed to decode ingestion image: %w", err)
	}
	mergeStegoLinkage(meta, coverRec.Metadata)
	addProposalStegoMeta(meta, p)
	addWishStegoMeta(meta, coverRec.Metadata)
	visibleHash := strings.TrimSpace(p.VisiblePixelHash)
	if visibleHash == "" {
		visibleHash = strings.TrimSpace(toString(meta["visible_pixel_hash"]))
	}
	if visibleHash == "" {
		return fmt.Errorf("proposal %s missing visible_pixel_hash", proposalID)
	}
	if p.VisiblePixelHash != visibleHash {
		p.VisiblePixelHash = visibleHash
	}
	payload := buildStegoPayload(p, meta, cfg.PayloadSchema, cfg.PayloadMaxTasks)
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal stego payload: %w", err)
	}
	ipfsClient := ipfs.NewClientFromEnv()
	payloadName := fmt.Sprintf("proposal-%s-payload.json", proposalID)
	payloadCID, err := ipfsClient.AddBytes(ctx, payloadName, payloadBytes)
	if err != nil {
		return fmt.Errorf("ipfs payload add failed: %w", err)
	}
	manifestCreatedAt := time.Now().Unix()
	if raw := strings.TrimSpace(toString(meta["stego_manifest_created_at"])); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			manifestCreatedAt = v
		}
	}
	manifestBytes, err := stego.BuildManifestYAML(stego.Manifest{
		SchemaVersion:    cfg.ManifestSchema,
		ProposalID:       proposalID,
		VisiblePixelHash: visibleHash,
		PayloadCID:       payloadCID,
		CreatedAt:        manifestCreatedAt,
		Issuer:           cfg.Issuer,
	})
	if err != nil {
		return fmt.Errorf("failed to build manifest: %w", err)
	}
	method := strings.TrimSpace(coverRec.Method)
	if method == "" {
		method = cfg.DefaultMethod
	}
	filename := strings.TrimSpace(coverRec.Filename)
	if filename == "" {
		filename = "cover.png"
	}
	stegoBytes, _, err := s.inscribeStego(ctx, cfg, coverBytes, filename, method, manifestBytes)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(stegoBytes)
	contractID := hex.EncodeToString(sum[:])
	stegoName := fmt.Sprintf("proposal-%s-stego%s", proposalID, filepath.Ext(filename))
	if strings.TrimSpace(stegoName) == fmt.Sprintf("proposal-%s-stego", proposalID) {
		stegoName = fmt.Sprintf("proposal-%s-stego.png", proposalID)
	}
	stegoCID, err := ipfsClient.AddBytes(ctx, stegoName, stegoBytes)
	if err != nil {
		return fmt.Errorf("ipfs stego add failed: %w", err)
	}
	meta["stego_payload_cid"] = payloadCID
	meta["stego_image_cid"] = stegoCID
	meta["stego_contract_id"] = contractID
	meta["stego_manifest_created_at"] = strconv.FormatInt(manifestCreatedAt, 10)
	meta["stego_request_id"] = visibleHash
	meta["stego_ingestion_id"] = visibleHash
	if commitmentLock != "" {
		meta["stego_commitment_lock_address"] = commitmentLock
	}
	if err := s.store.UpdateProposalMetadata(ctx, p.ID, meta); err != nil {
		return fmt.Errorf("failed to update proposal metadata: %w", err)
	}
	if s.ingestionSvc != nil && ingestionID != "" {
		updates := map[string]interface{}{
			"stego_payload_cid":          payloadCID,
			"stego_image_cid":            stegoCID,
			"stego_contract_id":          contractID,
			"stego_manifest_created_at":  strconv.FormatInt(manifestCreatedAt, 10),
			"stego_manifest_proposal_id": proposalID,
			"stego_manifest_issuer":      cfg.Issuer,
			"visible_pixel_hash":         visibleHash,
		}
		if err := s.ingestionSvc.UpdateMetadata(ingestionID, updates); err != nil {
			log.Printf("stego approval: failed to update ingestion %s: %v", ingestionID, err)
		}
	}
	if strings.EqualFold(strings.TrimSpace(p.Status), "approved") {
		s.archiveWishContract(ctx, visibleHash)
	}
	if cfg.AnnounceEnabled && strings.TrimSpace(cfg.AnnounceTopic) != "" {
		announce := stegoAnnouncement{
			Type:             "stego",
			StegoCID:         stegoCID,
			ExpectedHash:     visibleHash,
			ProposalID:       proposalID,
			VisiblePixelHash: visibleHash,
			PayloadCID:       payloadCID,
			Issuer:           cfg.Issuer,
			Timestamp:        time.Now().Unix(),
		}
		if payload, err := json.Marshal(announce); err != nil {
			log.Printf("stego announce marshal failed: %v", err)
		} else if err := ipfsClient.PubsubPublish(ctx, cfg.AnnounceTopic, payload); err != nil {
			log.Printf("stego announce publish failed: %v", err)
		}
	}
	s.recordEvent(smart_contract.Event{
		Type:      "stego_publish",
		EntityID:  proposalID,
		Actor:     "system",
		Message:   fmt.Sprintf("stego image published (cid=%s contract=%s)", stegoCID, contractID),
		CreatedAt: time.Now(),
	})
	return nil
}

func (s *Server) archiveWishContract(ctx context.Context, visibleHash string) {
	if s.store == nil {
		return
	}
	visibleHash = strings.TrimSpace(visibleHash)
	if visibleHash == "" {
		return
	}
	wishID := "wish-" + visibleHash
	contract, err := s.store.GetContract(wishID)
	if err != nil {
		return
	}
	contract.Status = "superseded"
	type upserter interface {
		UpsertContractWithTasks(context.Context, smart_contract.Contract, []smart_contract.Task) error
	}
	if u, ok := s.store.(upserter); ok {
		if err := u.UpsertContractWithTasks(ctx, contract, nil); err != nil {
			log.Printf("failed to archive wish contract %s: %v", wishID, err)
		}
	}
}

func buildStegoPayload(p smart_contract.Proposal, meta map[string]interface{}, schemaVersion int, maxTasks int) stegoPayload {
	payload := stegoPayload{
		SchemaVersion: schemaVersion,
		Proposal: stegoProposalPayload{
			ID:               p.ID,
			Title:            p.Title,
			DescriptionMD:    p.DescriptionMD,
			BudgetSats:       p.BudgetSats,
			VisiblePixelHash: p.VisiblePixelHash,
			CreatedAt:        p.CreatedAt.Unix(),
		},
	}
	tasks := buildStegoTasks(p, meta, maxTasks)
	if len(tasks) > 0 {
		payload.Tasks = tasks
	}
	metaEntries := extractStegoMetadata(meta)
	if len(metaEntries) > 0 {
		payload.Metadata = metaEntries
	}
	return payload
}

func mergeStegoLinkage(meta map[string]interface{}, ingestMeta map[string]interface{}) {
	if meta == nil || ingestMeta == nil {
		return
	}
	if strings.TrimSpace(toString(meta["funding_txid"])) == "" {
		if v := strings.TrimSpace(toString(ingestMeta["funding_txid"])); v != "" {
			meta["funding_txid"] = v
		} else if list := fundingTxIDsFromMeta(ingestMeta); len(list) > 0 {
			meta["funding_txid"] = list[0]
		}
	}
	// Do not embed block_height; rely on funding_txid and on-chain verification instead.
}

func addProposalStegoMeta(meta map[string]interface{}, p smart_contract.Proposal) {
	if meta == nil {
		return
	}
	if strings.TrimSpace(p.Title) != "" {
		meta["proposal_title"] = p.Title
	}
	if strings.TrimSpace(p.DescriptionMD) != "" {
		meta["proposal_description_md"] = p.DescriptionMD
	}
	if p.BudgetSats > 0 {
		meta["proposal_budget_sats"] = strconv.FormatInt(p.BudgetSats, 10)
	}
}

func addWishStegoMeta(meta map[string]interface{}, ingestMeta map[string]interface{}) {
	if meta == nil || ingestMeta == nil {
		return
	}
	wishText := strings.TrimSpace(toString(ingestMeta["embedded_message"]))
	if wishText == "" {
		wishText = strings.TrimSpace(toString(ingestMeta["message"]))
	}
	if wishText != "" {
		meta["wish_text"] = wishText
	}
	sats := satsFromMeta(ingestMeta["budget_sats"])
	if sats == 0 {
		sats = satsFromMeta(ingestMeta["price"])
	}
	if sats > 0 {
		meta["wish_price_sats"] = strconv.FormatInt(sats, 10)
	}
}

func satsFromMeta(value interface{}) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		if v < 1e6 {
			return int64(v * 1e8)
		}
		return int64(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		if f, err := v.Float64(); err == nil {
			if f < 1e6 {
				return int64(f * 1e8)
			}
			return int64(f)
		}
	case string:
		raw := strings.TrimSpace(v)
		if raw == "" {
			return 0
		}
		if strings.Contains(raw, ".") {
			if f, err := strconv.ParseFloat(raw, 64); err == nil {
				return int64(f * 1e8)
			}
		}
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return i
		}
	}
	return 0
}

func fundingTxIDsFromMeta(meta map[string]interface{}) []string {
	var txids []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range txids {
			if existing == value {
				return
			}
		}
		txids = append(txids, value)
	}
	if meta == nil {
		return txids
	}
	if txid := strings.TrimSpace(toString(meta["funding_txid"])); txid != "" {
		add(txid)
	}
	switch v := meta["funding_txids"].(type) {
	case []string:
		for _, txid := range v {
			add(txid)
		}
	case []any:
		for _, item := range v {
			if txid, ok := item.(string); ok {
				add(txid)
			}
		}
	case string:
		for _, part := range strings.Split(v, ",") {
			add(part)
		}
	}
	return txids
}

func buildStegoTasks(p smart_contract.Proposal, meta map[string]interface{}, maxTasks int) []stegoTaskPayload {
	tasks := p.Tasks
	if len(tasks) == 0 {
		if em, ok := meta["embedded_message"].(string); ok && em != "" {
			tasks = scstore.BuildTasksFromMarkdown(p.ID, em, p.VisiblePixelHash, p.BudgetSats, scstore.FundingAddressFromMeta(meta))
		}
	}
	if len(tasks) == 0 {
		return nil
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].TaskID == tasks[j].TaskID {
			return tasks[i].Title < tasks[j].Title
		}
		return tasks[i].TaskID < tasks[j].TaskID
	})
	if maxTasks > 0 && len(tasks) > maxTasks {
		tasks = tasks[:maxTasks]
	}
	out := make([]stegoTaskPayload, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, stegoTaskPayload{
			TaskID:           t.TaskID,
			Title:            t.Title,
			Description:      t.Description,
			BudgetSats:       t.BudgetSats,
			Skills:           t.Skills,
			ContractorWallet: t.ContractorWallet,
		})
	}
	return out
}

func extractStegoMetadata(meta map[string]interface{}) []stegoMetadataEntry {
	if meta == nil {
		return nil
	}
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]stegoMetadataEntry, 0, len(keys))
	for _, k := range keys {
		v := meta[k]
		val, ok := formatStegoMetaValue(v)
		if !ok {
			continue
		}
		out = append(out, stegoMetadataEntry{Key: k, Value: val})
	}
	return out
}

func formatStegoMetaValue(value interface{}) (string, bool) {
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return "", false
		}
		return v, true
	case fmt.Stringer:
		return v.String(), true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

func (s *Server) inscribeStego(ctx context.Context, cfg stegoApprovalConfig, cover []byte, filename, method string, message []byte) ([]byte, string, error) {
	if strings.TrimSpace(cfg.ProxyBase) == "" {
		return nil, "", fmt.Errorf("stego proxy base not configured")
	}

	// Validate inputs before sending request
	if len(cover) == 0 {
		return nil, "", fmt.Errorf("cover image cannot be empty")
	}
	if strings.TrimSpace(filename) == "" {
		return nil, "", fmt.Errorf("filename cannot be empty")
	}
	if strings.TrimSpace(method) == "" {
		return nil, "", fmt.Errorf("method cannot be empty")
	}
	if len(message) == 0 {
		return nil, "", fmt.Errorf("message cannot be empty")
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("image", filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(part, bytes.NewReader(cover)); err != nil {
		return nil, "", err
	}
	if err := writer.WriteField("message", string(message)); err != nil {
		return nil, "", err
	}
	if err := writer.WriteField("method", method); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	reqURL := fmt.Sprintf("%s/inscribe", strings.TrimRight(cfg.ProxyBase, "/"))

	var lastErr error
	var resp *http.Response
	var body []byte

	// Retry inscribe with exponential backoff for transient failures
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			waitTime := time.Duration(attempt*attempt) * time.Second
			if waitTime > 10*time.Second {
				waitTime = 10 * time.Second
			}
			log.Printf("inscribe retry attempt %d/%d after %v wait", attempt+1, 3, waitTime)
			time.Sleep(waitTime)
		}

		// Create fresh request for each attempt
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		part, err := writer.CreateFormFile("image", filename)
		if err != nil {
			lastErr = err
			continue
		}
		if _, err := io.Copy(part, bytes.NewReader(cover)); err != nil {
			lastErr = err
			continue
		}
		if err := writer.WriteField("message", string(message)); err != nil {
			lastErr = err
			continue
		}
		if err := writer.WriteField("method", method); err != nil {
			lastErr = err
			continue
		}
		if err := writer.Close(); err != nil {
			lastErr = err
			continue
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, &buf)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		if cfg.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		}

		client := &http.Client{Timeout: cfg.InscribeTimeout}
		resp, err = client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// Success!
			break
		}

		// Log failure and continue to retry
		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		log.Printf("inscribe attempt %d failed - URL: %s, Status: %s, Response: %s",
			attempt+1, reqURL, resp.Status, strings.TrimSpace(string(body)))
		log.Printf("inscribe request details - filename: %s, method: %s, message length: %d", filename, method, len(message))

		// Don't retry on client errors (4xx) except 429 (rate limit)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
			break
		}
	}

	if lastErr != nil {
		return nil, "", fmt.Errorf("inscribe failed after retries: %w", lastErr)
	}
	var payload struct {
		RequestID   string `json:"request_id"`
		ID          string `json:"id"`
		ImageSHA256 string `json:"image_sha256"`
		ImageBase64 string `json:"image_base64"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", fmt.Errorf("inscribe response parse failed: %w", err)
	}
	ingestionID := strings.TrimSpace(payload.ImageSHA256)
	if ingestionID == "" {
		ingestionID = strings.TrimSpace(payload.ID)
	}
	if ingestionID == "" {
		return nil, "", fmt.Errorf("inscribe response missing image_sha256 or id")
	}
	requestID := strings.TrimSpace(payload.RequestID)
	if requestID == "" {
		requestID = ingestionID
	}

	var stegoBytes []byte

	if strings.TrimSpace(payload.ImageBase64) != "" {
		log.Printf("stego: using image_base64 from inscribe response for %s", ingestionID)
		var decodeErr error
		stegoBytes, decodeErr = base64.StdEncoding.DecodeString(payload.ImageBase64)
		if decodeErr != nil {
			return nil, "", fmt.Errorf("failed to decode stego image from response: %w", decodeErr)
		}

		if s.ingestionSvc != nil {
			ingestionRec := &services.IngestionRecord{
				ID:            ingestionID,
				ImageBase64:   payload.ImageBase64,
				Status:        "verified",
				MessageLength: len(message),
				Filename:      filename,
				Method:        method,
				Metadata: map[string]interface{}{
					"stego_request_id":   requestID,
					"stego_ingestion_id": ingestionID,
				},
			}
			if createErr := s.ingestionSvc.Create(*ingestionRec); createErr != nil {
				log.Printf("stego: failed to create ingestion record for %s: %v", ingestionID, createErr)
			} else {
				log.Printf("stego: created ingestion record for %s with image data from response", ingestionID)
			}

			uploadsDir := os.Getenv("UPLOADS_DIR")
			if uploadsDir == "" {
				uploadsDir = "/data/uploads"
			}
			uploadPath := filepath.Join(uploadsDir, filename)
			if writeErr := os.WriteFile(uploadPath, stegoBytes, 0644); writeErr != nil {
				log.Printf("stego: failed to write stego image to %s: %v", uploadPath, writeErr)
			} else {
				log.Printf("stego: wrote stego image to %s (%d bytes)", uploadPath, len(stegoBytes))
			}

			topic := strings.TrimSpace(os.Getenv("IPFS_STEGO_TOPIC"))
			if topic == "" {
				topic = "stargate-stego"
			}
			if strings.TrimSpace(topic) != "" && len(stegoBytes) > 0 {
				pubCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				client := ipfs.NewClientFromEnv()
				ext := filepath.Ext(filename)
				if ext == "" {
					ext = ".png"
				}
				name := fmt.Sprintf("stego-%s%s", ingestionID, ext)
				imageCID, err := client.AddBytes(pubCtx, name, stegoBytes)
				if err != nil {
					log.Printf("stego: ipfs add failed for %s: %v", ingestionID, err)
				} else {
					log.Printf("stego: ipfs added %s -> %s", ingestionID, imageCID)
					ann := struct {
						Type        string `json:"type"`
						IngestionID string `json:"ingestion_id"`
						ImageCID    string `json:"image_cid"`
						Filename    string `json:"filename,omitempty"`
						Method      string `json:"method,omitempty"`
						Message     string `json:"message,omitempty"`
						Timestamp   int64  `json:"timestamp"`
					}{
						Type:        "stego_ingest",
						IngestionID: ingestionID,
						ImageCID:    imageCID,
						Filename:    filename,
						Method:      method,
						Message:     string(message),
						Timestamp:   time.Now().Unix(),
					}
					if payload, err := json.Marshal(ann); err != nil {
						log.Printf("stego: announce marshal failed: %v", err)
					} else if err := client.PubsubPublish(pubCtx, topic, payload); err != nil {
						log.Printf("stego: announce publish failed: %v", err)
					} else {
						log.Printf("stego: published announcement to topic %s", topic)
					}
				}
			}
		}
	} else {
		rec, waitErr := s.waitForIngestion(ctx, ingestionID, cfg.IngestTimeout, cfg.IngestPoll)
		if waitErr != nil {
			return nil, "", waitErr
		}
		if rec.ImageBase64 == "" {
			return nil, "", fmt.Errorf("ingestion %s missing image payload", ingestionID)
		}
		var decodeErr error
		stegoBytes, decodeErr = base64.StdEncoding.DecodeString(rec.ImageBase64)
		if decodeErr != nil {
			return nil, "", fmt.Errorf("failed to decode stego image: %w", decodeErr)
		}
	}
	return stegoBytes, ingestionID, nil
}

func (s *Server) waitForIngestion(ctx context.Context, id string, timeout time.Duration, poll time.Duration) (*services.IngestionRecord, error) {
	if id == "" {
		return nil, fmt.Errorf("missing ingestion id")
	}
	if s.ingestionSvc == nil {
		return nil, fmt.Errorf("ingestion service unavailable")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if poll <= 0 {
		poll = 2 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		rec, err := s.ingestionSvc.Get(id)
		if err == nil && rec != nil && rec.ImageBase64 != "" {
			return rec, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for ingestion %s", id)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(poll):
		}
	}
}
