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
	if strings.TrimSpace(toString(meta["stego_contract_id"])) != "" && strings.TrimSpace(toString(meta["stego_image_cid"])) != "" {
		return nil
	}
	ingestionID := strings.TrimSpace(toString(meta["ingestion_id"]))
	if ingestionID == "" {
		return fmt.Errorf("proposal %s missing ingestion_id metadata", proposalID)
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
	stegoBytes, requestID, err := s.inscribeStego(ctx, cfg, coverBytes, filename, method, manifestBytes)
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
	meta["stego_request_id"] = requestID
	meta["stego_ingestion_id"] = requestID
	p.Metadata = meta
	if err := s.store.UpdateProposal(ctx, p); err != nil {
		return fmt.Errorf("failed to update proposal metadata: %w", err)
	}
	s.archiveWishContract(ctx, visibleHash)
	if cfg.AnnounceEnabled && strings.TrimSpace(cfg.AnnounceTopic) != "" {
		announce := stegoAnnouncement{
			Type:             "stego",
			StegoCID:         stegoCID,
			ExpectedHash:     contractID,
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, &buf)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	client := &http.Client{Timeout: cfg.InscribeTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("inscribe failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		RequestID string `json:"request_id"`
		ID        string `json:"id"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", fmt.Errorf("inscribe response parse failed: %w", err)
	}
	requestID := strings.TrimSpace(payload.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(payload.ID)
	}
	if requestID == "" {
		return nil, "", fmt.Errorf("inscribe response missing request id")
	}
	rec, err := s.waitForIngestion(ctx, requestID, cfg.IngestTimeout, cfg.IngestPoll)
	if err != nil {
		return nil, "", err
	}
	if rec.ImageBase64 == "" {
		return nil, "", fmt.Errorf("ingestion %s missing image payload", requestID)
	}
	stegoBytes, err := base64.StdEncoding.DecodeString(rec.ImageBase64)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode stego image: %w", err)
	}
	return stegoBytes, requestID, nil
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
