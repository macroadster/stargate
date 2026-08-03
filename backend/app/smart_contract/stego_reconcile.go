package smart_contract

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"

	"stargate-backend/core/identity"
	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	"stargate-backend/stego"
       "stargate-backend/storage/datadir"
	"stargate-backend/storage/ipfs"
	scstore "stargate-backend/storage/smart_contract"
)

type stegoReconcileRequest struct {
	StegoCID     string `json:"stego_cid"`
	ExpectedHash string `json:"expected_hash"`
}

type stegoReconcileResponse struct {
	ContractID       string `json:"contract_id"`
	StegoCID         string `json:"stego_cid"`
	PayloadCID       string `json:"payload_cid"`
	ManifestProposal string `json:"manifest_proposal_id"`
	VisiblePixelHash string `json:"visible_pixel_hash"`
}

type stegoReconcileConfig struct {
	ProxyBase   string
	APIKey      string
	ScanTimeout time.Duration
}

// generatePayoutScript creates a Bitcoin script for the given address.
func generatePayoutScript(address string) ([]byte, error) {
	// Default to mainnet parameters
	params := &chaincfg.MainNetParams
	addr, err := btcutil.DecodeAddress(address, params)
	if err != nil {
		return nil, fmt.Errorf("decode address failed: %w", err)
	}
	script, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, fmt.Errorf("create payout script failed: %w", err)
	}
	return script, nil
}

// stringFromAny safely converts interface{} to string
func stringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}

// getStegoMethodFromImage determines the appropriate steganography method based on image format
func getStegoMethodFromImage(imageBytes []byte, filename string) string {
	// Default to lsb if we can't determine format
	defaultMethod := "lsb"

	// Try to determine from file extension first
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png":
		return "alpha"
	case ".jpg", ".jpeg":
		return "exif"
	case ".gif":
		return "palette"
	}

	// If extension doesn't work, try to detect from image header
	if len(imageBytes) >= 8 {
		// PNG signature: 89 50 4E 47
		if imageBytes[0] == 0x89 && imageBytes[1] == 0x50 && imageBytes[2] == 0x4E && imageBytes[3] == 0x47 {
			return "alpha"
		}
		// JPEG signature: FF D8 FF
		if imageBytes[0] == 0xFF && imageBytes[1] == 0xD8 && imageBytes[2] == 0xFF {
			return "exif"
		}
		// GIF signature: GIF87a or GIF89a
		if len(imageBytes) >= 6 && string(imageBytes[:3]) == "GIF" {
			return "palette"
		}
	}

	return defaultMethod
}

func loadStegoReconcileConfig() stegoReconcileConfig {
	proxyBase := strings.TrimSpace(os.Getenv("STARGATE_PROXY_BASE"))
	timeout := 30 * time.Second
	if raw := strings.TrimSpace(os.Getenv("STARGATE_STEGO_SCAN_TIMEOUT_SEC")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			timeout = time.Duration(v) * time.Second
		}
	}
	return stegoReconcileConfig{
		ProxyBase:   proxyBase,
		APIKey:      strings.TrimSpace(os.Getenv("STARGATE_API_KEY")),
		ScanTimeout: timeout,
	}
}

func (s *Server) handleStegoReconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req stegoReconcileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.StegoCID = strings.TrimSpace(req.StegoCID)
	req.ExpectedHash = strings.TrimSpace(req.ExpectedHash)
	if req.StegoCID == "" {
		Error(w, http.StatusBadRequest, "stego_cid is required")
		return
	}
	// Public/IPFS: stage bytes only. SQL apply requires on-chain confirmation
	// (block monitor → ReconcileStego with applySQL=true).
	res, err := s.reconcileStegoFromIPFS(r.Context(), req.StegoCID, req.ExpectedHash, false)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, res)
}

// ReconcileStego applies stego payload into MCP SQL. Call only after on-chain
// evidence (funding match / OP_RETURN stego hash) via the block monitor.
// It first tries UPLOADS_DIR (by hash), falling back to IPFS for the blob only.
func (s *Server) ReconcileStego(ctx context.Context, stegoCID, expectedHash string) error {
	// Try local file first: if the stegoCID looks like a SHA256 hash,
	// look for UPLOADS_DIR/<hash> on disk (synced by IPFS mirror).
	if len(stegoCID) == 64 {
		if _, hexErr := hex.DecodeString(stegoCID); hexErr == nil {
			if err := s.reconcileStegoFromLocalFile(ctx, stegoCID, true); err == nil {
				return nil
			}
			// Fall through to IPFS if local file reconcile failed.
		}
	}
	_, err := s.reconcileStegoFromIPFS(ctx, stegoCID, expectedHash, true)
	return err
}

// reconcileStegoFromLocalFile reads a stego image from UPLOADS_DIR/<hash>.
// When applySQL is true (chain-confirmed path), upserts contracts/tasks/ingestion.
func (s *Server) reconcileStegoFromLocalFile(ctx context.Context, stegoHash string, applySQL bool) error {
	uploadsDir := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
	if uploadsDir == "" {
		return fmt.Errorf("UPLOADS_DIR not set")
	}
	stegoPath := datadir.PartResolve(uploadsDir, stegoHash)
	stegoBytes, err := os.ReadFile(stegoPath)
	if err != nil {
		return fmt.Errorf("read local stego %s: %w", stegoPath, err)
	}
	// Verify hash.
	sum := sha256.Sum256(stegoBytes)
	actualHash := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actualHash, stegoHash) {
		return fmt.Errorf("stego hash mismatch: expected %s got %s", stegoHash, actualHash)
	}
	if !applySQL {
		log.Printf("stego: staged local file %s (%d bytes) — no SQL", stegoPath, len(stegoBytes))
		return nil
	}
	log.Printf("stego: applying from local file %s (%d bytes) after on-chain path", stegoPath, len(stegoBytes))

	rawBytes, err := extractStegoManifest(ctx, stegoBytes, loadStegoReconcileConfig())
	if err != nil {
		return fmt.Errorf("stego extraction failed: %w", err)
	}
	manifest, payload, err := stego.ParseEmbedded(rawBytes)
	if err != nil {
		return fmt.Errorf("stego parse failed: %w", err)
	}
	contractID := strings.TrimSpace(manifest.VisiblePixelHash)
	if contractID == "" {
		return fmt.Errorf("manifest visible_pixel_hash missing")
	}
	// v1 fallback: payload was not inline — fetch from IPFS.
	if payload.SchemaVersion == 0 && manifest.PayloadCID != "" {
		ipfsClient := ipfs.NewClientFromEnv()
		if ipfsClient == nil {
			return fmt.Errorf("v1 stego needs IPFS for payload fetch but IPFS is disabled")
		}
		payloadBytes, err := ipfsClient.Cat(ctx, manifest.PayloadCID)
		if err != nil {
			return fmt.Errorf("ipfs cat payload failed: %w", err)
		}
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return fmt.Errorf("payload json decode failed: %w", err)
		}
	}
	if err := s.UpsertContractFromStegoPayload(ctx, contractID, stegoHash, stegoHash, manifest, payload); err != nil {
		return err
	}
	s.ensureStegoIngestion(ctx, contractID, stegoHash, stegoHash, stegoBytes, manifest)

	// If the contract is already confirmed, kick off sandbox extraction.
	if c, err := s.store.GetContract(contractID); err == nil {
		if strings.EqualFold(strings.TrimSpace(c.Status), "confirmed") {
			go s.downloadSandboxArtifacts(context.Background(), contractID)
		}
	}
	log.Printf("stego: applied from local file: contract_id=%s, hash=%s", contractID, stegoHash)
	return nil
}

// ReconcileStegoWithAnnouncement stages a stego image from an IPFS pubsub
// announcement onto local disk only. It does not write SQL — IPFS is untrusted.
func (s *Server) ReconcileStegoWithAnnouncement(ctx context.Context, ann *stegoAnnouncement) error {
	// Download stego image from IPFS
	ipfsClient := ipfs.NewClientFromEnv()
	if ipfsClient == nil {
		return fmt.Errorf("IPFS client is disabled - cannot stage stego")
	}
	stegoBytes, err := ipfsClient.Cat(ctx, ann.StegoCID)
	if err != nil {
		return fmt.Errorf("ipfs cat stego failed: %w", err)
	}

	// Write stego image to /data/uploads using SHA256 as filename (matches
	// inscribeStego convention so mirror sync doesn't create duplicates).
	sum := sha256.Sum256(stegoBytes)
	stegoHash := hex.EncodeToString(sum[:])
	uploadsDir := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
	uploadPath := datadir.PartPath(uploadsDir, stegoHash)
	if err := os.MkdirAll(filepath.Dir(uploadPath), 0755); err != nil {
		return fmt.Errorf("failed to create partition dir: %w", err)
	}
	if err := os.WriteFile(uploadPath, stegoBytes, 0644); err != nil {
		return fmt.Errorf("failed to write stego image: %w", err)
	}
	// Also stage under expected hash / visible hash aliases if provided, so the
	// block monitor can resolve either OP_RETURN field once confirmation lands.
	for _, alias := range []string{ann.ExpectedHash, ann.VisiblePixelHash} {
		alias = strings.TrimSpace(alias)
		if alias == "" || strings.EqualFold(alias, stegoHash) {
			continue
		}
		if len(alias) != 64 {
			continue
		}
		if _, err := hex.DecodeString(alias); err != nil {
			continue
		}
		// Do not write wrong content under an unrelated hash; only symlink-style
		// alias when alias equals content hash (already handled). Skip otherwise.
	}
	log.Printf("stego: staged from IPFS announcement hash=%s cid=%s path=%s (no SQL until on-chain confirm)", stegoHash, ann.StegoCID, uploadPath)
	return nil
}

// reconcileStegoFromIPFS downloads stego bytes (and optionally applies SQL).
// applySQL must be true only for the block-monitor confirmation path.
func (s *Server) reconcileStegoFromIPFS(ctx context.Context, stegoCID string, expectedHash string, applySQL bool) (stegoReconcileResponse, error) {
	ipfsClient := ipfs.NewClientFromEnv()
	if ipfsClient == nil {
		return stegoReconcileResponse{}, fmt.Errorf("IPFS client is disabled")
	}
	stegoBytes, err := ipfsClient.Cat(ctx, stegoCID)
	if err != nil {
		return stegoReconcileResponse{}, fmt.Errorf("ipfs cat stego failed: %w", err)
	}
	sum := sha256.Sum256(stegoBytes)
	stegoHash := hex.EncodeToString(sum[:])
	if expectedHash != "" && !strings.EqualFold(expectedHash, stegoHash) {
		return stegoReconcileResponse{}, fmt.Errorf("stego hash mismatch: expected %s got %s", expectedHash, stegoHash)
	}
	// Always stage on disk for later chain confirmation.
	uploadsDir := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
	if uploadsDir != "" {
		uploadPath := datadir.PartPath(uploadsDir, stegoHash)
		if err := os.MkdirAll(filepath.Dir(uploadPath), 0755); err == nil {
			_ = os.WriteFile(uploadPath, stegoBytes, 0644)
		}
	}

	rawBytes, err := extractStegoManifest(ctx, stegoBytes, loadStegoReconcileConfig())
	if err != nil {
		return stegoReconcileResponse{}, err
	}
	manifest, payload, err := stego.ParseEmbedded(rawBytes)
	if err != nil {
		return stegoReconcileResponse{}, err
	}
	contractID := strings.TrimSpace(manifest.VisiblePixelHash)
	if contractID == "" {
		return stegoReconcileResponse{}, fmt.Errorf("manifest visible_pixel_hash missing")
	}
	// v1 fallback: payload was not inline — fetch from IPFS
	if payload.SchemaVersion == 0 && manifest.PayloadCID != "" {
		payloadBytes, err := ipfsClient.Cat(ctx, manifest.PayloadCID)
		if err != nil {
			return stegoReconcileResponse{}, fmt.Errorf("ipfs cat payload failed: %w", err)
		}
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return stegoReconcileResponse{}, fmt.Errorf("payload json decode failed: %w", err)
		}
	}
	if applySQL {
		if err := s.UpsertContractFromStegoPayload(ctx, contractID, stegoCID, stegoHash, manifest, payload); err != nil {
			return stegoReconcileResponse{}, err
		}
		s.ensureStegoIngestion(ctx, contractID, stegoCID, stegoHash, stegoBytes, manifest)
	} else {
		log.Printf("stego: staged from IPFS cid=%s hash=%s (no SQL until on-chain confirm)", stegoCID, stegoHash)
	}
	return stegoReconcileResponse{
		ContractID:       contractID,
		StegoCID:         stegoCID,
		PayloadCID:       manifest.PayloadCID,
		ManifestProposal: manifest.ProposalID,
		VisiblePixelHash: manifest.VisiblePixelHash,
	}, nil
}

func extractStegoManifest(ctx context.Context, imageData []byte, cfg stegoReconcileConfig) ([]byte, error) {
	// Try native Go extraction first (no external proxy needed).
	if payload, err := extractStegoNative(imageData); err == nil && len(payload) > 0 {
		return payload, nil
	}

	// Fall back to HTTP proxy scanner if configured.
	if strings.TrimSpace(cfg.ProxyBase) == "" {
		return nil, fmt.Errorf("native extraction found no message and stego proxy not configured")
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("image", "stego.png")
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, bytes.NewReader(imageData)); err != nil {
		return nil, err
	}
	writer.WriteField("extract_message", "true")
	writer.WriteField("confidence_threshold", "0.1")
	writer.WriteField("include_metadata", "true")
	if err := writer.Close(); err != nil {
		return nil, err
	}
	reqURL := fmt.Sprintf("%s/scan/image", strings.TrimRight(cfg.ProxyBase, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	client := &http.Client{Timeout: cfg.ScanTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("stego scan failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("stego scan response decode failed: %w", err)
	}
	extracted := ""
	if scan, ok := decoded["scan_result"].(map[string]interface{}); ok {
		if msg, ok := scan["extracted_message"].(string); ok {
			extracted = msg
		}
		if extracted == "" {
			if errMsg, ok := scan["extraction_error"].(string); ok && strings.TrimSpace(errMsg) != "" {
				return nil, fmt.Errorf("stego extraction error: %s", errMsg)
			}
		}
	}
	if extracted == "" {
		if msg, ok := decoded["extracted_message"].(string); ok {
			extracted = msg
		}
	}
	if strings.TrimSpace(extracted) == "" {
		return nil, fmt.Errorf("stego extract returned empty message")
	}
	return []byte(extracted), nil
}

// extractStegoNative uses the built-in Go alpha-channel extractor.
func extractStegoNative(imageData []byte) ([]byte, error) {
	img, _, err := stego.DecodeImage(imageData)
	if err != nil {
		return nil, err
	}
	return stego.ExtractAlpha(img)
}

func (s *Server) ensureStegoIngestion(ctx context.Context, contractID, stegoCID, stegoHash string, stegoBytes []byte, manifest stego.Manifest) {
	if s.ingestionSvc == nil || contractID == "" || len(stegoBytes) == 0 {
		return
	}
	meta := map[string]interface{}{
		"stego_image_cid":           stegoCID,
		"stego_contract_id":         stegoHash,
		"stego_manifest_issuer":     manifest.Issuer,
		"stego_manifest_created_at": manifest.CreatedAt,
		"origin_proposal_id":        manifest.ProposalID,
		"visible_pixel_hash":        manifest.VisiblePixelHash,
	}
	// Determine appropriate steganography method based on image format
	stegoMethod := getStegoMethodFromImage(stegoBytes, "stego.png")

	rec := services.IngestionRecord{
		ID:            contractID,
		Filename:      "stego.png",
		Method:        stegoMethod,
		MessageLength: 0,
		ImageBase64:   base64.StdEncoding.EncodeToString(stegoBytes),
		Metadata:      meta,
		Status:        "verified",
	}
	if existing, err := s.ingestionSvc.Get(contractID); err == nil && existing != nil {
		_ = s.ingestionSvc.UpdateFromIngest(contractID, rec)
		return
	}
	if err := s.ingestionSvc.Create(rec); err != nil {
		log.Printf("stego reconcile: failed to create ingestion %s: %v", contractID, err)
	}
}

// fillProofFromIngestion populates missing funding fields on a MerkleProof
// from the ingestion record's metadata.  Peer nodes receive funding_txid,
// commitment_vout, and commitment_sats via IPFS announcement into the
// ingestion record, but these are not present in the stego manifest used to
// build the initial proof.
func (s *Server) fillProofFromIngestion(contractID string, proof *smart_contract.MerkleProof) {
	if s.ingestionSvc == nil || proof == nil || contractID == "" {
		return
	}
	rec, err := s.ingestionSvc.Get(contractID)
	if err != nil || rec == nil || rec.Metadata == nil {
		return
	}
	if proof.TxID == "" {
		if v := stringFromAny(rec.Metadata["funding_txid"]); v != "" {
			proof.TxID = v
			log.Printf("stego reconcile: filled TxID=%s from ingestion for %s", v, contractID)
		}
	}
	if proof.CommitmentVout == 0 {
		switch v := rec.Metadata["commitment_vout"].(type) {
		case float64:
			if v > 0 {
				proof.CommitmentVout = uint32(v)
			}
		case int:
			if v > 0 {
				proof.CommitmentVout = uint32(v)
			}
		case int64:
			if v > 0 {
				proof.CommitmentVout = uint32(v)
			}
		}
	}
	if proof.CommitmentSats == 0 {
		switch v := rec.Metadata["commitment_sats"].(type) {
		case float64:
			if v > 0 {
				proof.CommitmentSats = int64(v)
			}
		case int:
			if v > 0 {
				proof.CommitmentSats = int64(v)
			}
		case int64:
			if v > 0 {
				proof.CommitmentSats = v
			}
		}
	}
}

// UpsertContractFromStegoPayload writes proposal/contract/task rows from a
// decoded stego payload. Callers must only invoke this after on-chain evidence
// (block monitor ReconcileStego). IPFS mirror/pubsub paths must not call this.
func (s *Server) UpsertContractFromStegoPayload(ctx context.Context, contractID, stegoCID, stegoHash string, manifest stego.Manifest, payload stego.Payload) error {
	if contractID == "" {
		return fmt.Errorf("contract id missing")
	}
	proposalID := strings.TrimSpace(manifest.ProposalID)
	if proposalID == "" {
		proposalID = contractID
	}

	// System metadata written by the reconciler (not attacker payload keys).
	meta := map[string]interface{}{
		"stego_contract_id":         stegoHash,
		"stego_image_cid":           stegoCID,
		"stego_payload_cid":         manifest.PayloadCID,
		"stego_tasks_cid":           manifest.TasksCID,
		"stego_manifest_issuer":     manifest.Issuer,
		"stego_manifest_created_at": manifest.CreatedAt,
		"stego_manifest_schema":     manifest.SchemaVersion,
		"origin_proposal_id":        manifest.ProposalID,
		"visible_pixel_hash":        manifest.VisiblePixelHash,
		"stego_replicated":          true,
	}
	// Propagate sandbox artifact metadata so peers can download on confirmation.
	if manifest.SandboxHash != "" {
		meta["sandbox_hash"] = manifest.SandboxHash
	}
	// Allowlisted payload metadata only — never import funding_txid etc. from stego.
	for k, v := range filterStegoPayloadMetadata(payload) {
		if _, ok := meta[k]; !ok {
			meta[k] = v
		}
	}

	title := strings.TrimSpace(payload.Proposal.Title)
	if title == "" {
		title = "Stego Contract " + contractID
	}
	if len(title) > scstore.MaxProposalTitle {
		title = title[:scstore.MaxProposalTitle]
	}
	title, _ = scstore.SanitizeInput(title)
	desc := payload.Proposal.DescriptionMD
	if len(desc) > scstore.MaxProposalDesc {
		desc = desc[:scstore.MaxProposalDesc]
	}
	desc, _ = scstore.SanitizeInput(desc)

	// For wish-style contracts (identified by 64-hex visible pixel hash), normalize
	// the stored ContractID to "wish-<hash>" for consistency with wish creation.
	vh := strings.TrimSpace(manifest.VisiblePixelHash)
	if vh == "" {
		vh = strings.TrimSpace(payload.Proposal.VisiblePixelHash)
	}
	if vh != "" && identity.IsPixelHash(contractID) {
		contractID = "wish-" + strings.TrimPrefix(vh, "wish-")
	}

	// Load existing rows before write so we never demote trust or overwrite protected fields.
	var existingContract *smart_contract.Contract
	if c, err := s.store.GetContract(contractID); err == nil {
		existingContract = &c
	}
	var existingProposal *smart_contract.Proposal
	if p, err := s.store.GetProposal(ctx, proposalID); err == nil {
		existingProposal = &p
	}

	proposalStatus := stegoProposalStatus(existingProposal)
	// If proposal already exists, only refresh stego metadata — do not demote status
	// or re-trigger CreateProposal supersede side effects with approved status.
	if existingProposal != nil && strings.TrimSpace(existingProposal.ID) != "" {
		updates := map[string]interface{}{}
		for k, v := range meta {
			updates[k] = v
		}
		_ = s.store.UpdateProposalMetadata(ctx, proposalID, updates)
	} else {
		proposal := smart_contract.Proposal{
			ID:               proposalID,
			Title:            title,
			DescriptionMD:    desc,
			VisiblePixelHash: manifest.VisiblePixelHash,
			BudgetSats:       payload.Proposal.BudgetSats,
			Status:           proposalStatus, // pending — never auto-approved from stego
			CreatedAt:        time.Unix(payload.Proposal.CreatedAt, 0),
			Metadata:         meta,
		}
		if proposal.CreatedAt.IsZero() {
			proposal.CreatedAt = time.Now()
		}
		if err := s.store.CreateProposal(ctx, proposal); err != nil {
			return fmt.Errorf("create proposal failed: %w", err)
		}
	}

	// Contract status: never "funded" from stego metadata alone.
	contractStatus := stegoContractStatus(existingContract)
	contractTitle := title
	contractBudget := payload.Proposal.BudgetSats
	if shouldPreserveContractFields(existingContract) {
		// Active/funded/confirmed/etc. keep identity fields from the existing row.
		contractTitle = existingContract.Title
		contractBudget = existingContract.TotalBudgetSats
		if strings.TrimSpace(contractTitle) == "" {
			contractTitle = title
		}
	}

	contract := smart_contract.Contract{
		ContractID:      contractID,
		Title:           contractTitle,
		TotalBudgetSats: contractBudget,
		GoalsCount:      1,
		Status:          contractStatus,
		Metadata:        meta,
	}

	tasks := make([]smart_contract.Task, 0, len(payload.Tasks))
	for _, rawTask := range payload.Tasks {
		t, ok := sanitizeStegoTask(rawTask)
		if !ok {
			continue
		}
		t.ContractID = contractID
		t.GoalID = contractID

		// Load existing task to preserve existing merkle_proof
		existingTask, err := s.store.GetTask(t.TaskID)
		var merkleProof *smart_contract.MerkleProof
		if err == nil && existingTask.MerkleProof != nil {
			merkleProof = existingTask.MerkleProof
		}

		// Build hashlock proof only when a validated contractor wallet is present.
		if strings.TrimSpace(t.ContractorWallet) != "" {
			commitmentHashHex := manifest.VisiblePixelHash
			pixelHashBytes, err := hex.DecodeString(commitmentHashHex)
			if err != nil {
				log.Printf("stego reconcile: failed to decode commitment hash for task %s: %v", t.TaskID, err)
			} else {
				lockHash := sha256.Sum256(pixelHashBytes)
				builder := txscript.NewScriptBuilder()
				builder.AddOp(txscript.OP_SHA256)
				builder.AddData(lockHash[:])
				builder.AddOp(txscript.OP_EQUAL)
				redeemScript, err := builder.Script()
				if err != nil {
					log.Printf("stego reconcile: failed to build hashlock redeem script for task %s: %v", t.TaskID, err)
				} else {
					scriptHash := sha256.Sum256(redeemScript)
					contractorProof := &smart_contract.MerkleProof{
						VisiblePixelHash:       manifest.VisiblePixelHash,
						CommitmentPixelHash:    commitmentHashHex,
						CommitmentSource:       "wish",
						ProductPixelHash:       stegoHash,
						ContractorWallet:       t.ContractorWallet,
						CommitmentAddress:      t.ContractorWallet,
						CommitmentRedeemScript: hex.EncodeToString(redeemScript),
						CommitmentRedeemHash:   hex.EncodeToString(scriptHash[:]),
						ConfirmationStatus:     "provisional",
						SeenAt:                 time.Now(),
					}

					if merkleProof == nil {
						if existingTask.MerkleProof != nil {
							contractorProof.TxID = existingTask.MerkleProof.TxID
							contractorProof.BlockHeight = existingTask.MerkleProof.BlockHeight
							contractorProof.BlockHeaderMerkleRoot = existingTask.MerkleProof.BlockHeaderMerkleRoot
							contractorProof.ProofPath = existingTask.MerkleProof.ProofPath
							contractorProof.FundingAddress = existingTask.MerkleProof.FundingAddress
							contractorProof.FundedAmountSats = existingTask.MerkleProof.FundedAmountSats
							contractorProof.CommitmentVout = existingTask.MerkleProof.CommitmentVout
							contractorProof.CommitmentSats = existingTask.MerkleProof.CommitmentSats
						}
						if contractorProof.TxID == "" || contractorProof.CommitmentVout == 0 {
							s.fillProofFromIngestion(contractID, contractorProof)
						}
						merkleProof = contractorProof
					} else {
						// Preserve existing contractor wallet on protected/funded tasks —
						// do not let stego redirect payouts if wallet already set.
						if strings.TrimSpace(merkleProof.ContractorWallet) == "" {
							merkleProof.ContractorWallet = contractorProof.ContractorWallet
							merkleProof.CommitmentAddress = contractorProof.CommitmentAddress
						}
						merkleProof.CommitmentRedeemScript = contractorProof.CommitmentRedeemScript
						merkleProof.CommitmentRedeemHash = contractorProof.CommitmentRedeemHash
						merkleProof.VisiblePixelHash = contractorProof.VisiblePixelHash
						merkleProof.CommitmentPixelHash = contractorProof.CommitmentPixelHash
						merkleProof.CommitmentSource = contractorProof.CommitmentSource
						merkleProof.ProductPixelHash = stegoHash
						if merkleProof.SeenAt.IsZero() {
							merkleProof.SeenAt = contractorProof.SeenAt
						}
						if merkleProof.TxID == "" || merkleProof.CommitmentVout == 0 {
							s.fillProofFromIngestion(contractID, merkleProof)
						}
					}
				}
			}
		} else if merkleProof != nil {
			// Always stamp product pixel hash for two-phase sweep even without new wallet.
			merkleProof.ProductPixelHash = stegoHash
		}

		t.MerkleProof = merkleProof
		tasks = append(tasks, t)
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].TaskID < tasks[j].TaskID
	})
	contract.AvailableTasksCount = len(tasks)
	if upserter, ok := s.store.(interface {
		UpsertContractWithTasks(context.Context, smart_contract.Contract, []smart_contract.Task) error
	}); ok {
		if err := upserter.UpsertContractWithTasks(ctx, contract, tasks); err != nil {
			return fmt.Errorf("upsert contract failed: %w", err)
		}
		return nil
	}
	return fmt.Errorf("store does not support contract upsert")
}

// looksLikeHash returns true for typical 64-hex visible pixel / stego hashes
// used as wish/contract identifiers. Used to decide when to apply "wish-" prefix.
func looksLikeHash(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

// downloadSandboxArtifacts fetches and extracts the sandbox tarball for a
// confirmed contract.  It looks up sandbox_hash from the associated proposal
// or contract metadata, then searches for the tarball on the local filesystem
// (UPLOADS_DIR/<sandbox_hash>) first — the IPFS mirror syncs tarballs between
// peers using hash-based filenames.  Falls back to IPFS Cat if the file isn't
// available locally yet.
//
// The function is idempotent — if the results directory already exists and
// passes hash verification, the extraction is skipped.
func (s *Server) downloadSandboxArtifacts(ctx context.Context, contractID string) {
	if s.store == nil || contractID == "" {
		return
	}
	normalizedID := scstore.NormalizeContractID(contractID)
	if normalizedID == "" {
		return
	}

	sandboxHash := s.findSandboxHash(ctx, contractID, normalizedID)
	if sandboxHash == "" {
		log.Printf("sandbox: no sandbox_hash found for contract %s, skipping", contractID)
		return
	}

	uploadsDir := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
       resultsDir := datadir.PartResolve(filepath.Join(uploadsDir, "results"), normalizedID)

	// If results already exist and match the expected hash, skip extraction.
	if info, err := os.Stat(resultsDir); err == nil && info.IsDir() {
		if err := stego.VerifySandboxHash(resultsDir, sandboxHash); err == nil {
			log.Printf("sandbox: artifacts already present and verified for %s", contractID)
			return
		}
		log.Printf("sandbox: artifacts present but hash mismatch for %s, re-extracting", contractID)
	}

	// Try to read the tarball from the local uploads directory first.
       // The publisher stores it as UPLOADS_DIR/ab/cd/ef/<sandbox_hash> and
       // the IPFS mirror replicates it to peers using the same hash-based filename.
       tarballBytes, err := os.ReadFile(datadir.PartResolve(uploadsDir, sandboxHash))
	if err != nil {
		// Not available locally — try IPFS content-addressed fetch.
		// Also try sandbox_tarball_cid for backward compat with older publishers.
		sandboxCID := s.findSandboxCID(ctx, contractID, normalizedID)
		fetchKey := sandboxHash
		if sandboxCID != "" {
			fetchKey = sandboxCID
		}
		ipfsClient := ipfs.NewClientFromEnv()
		if ipfsClient == nil {
			log.Printf("sandbox: tarball not on disk and IPFS disabled for %s", contractID)
			return
		}
		tarballBytes, err = ipfsClient.Cat(ctx, fetchKey)
		if err != nil {
			log.Printf("sandbox: tarball %s not on disk and IPFS fetch failed for %s: %v", fetchKey, contractID, err)
			return
		}
		log.Printf("sandbox: fetched tarball from IPFS for %s (%d bytes)", contractID, len(tarballBytes))
	} else {
		log.Printf("sandbox: found tarball on disk for %s (%d bytes)", contractID, len(tarballBytes))
	}

	// Verify tarball hash before extracting.
	sum := sha256.Sum256(tarballBytes)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, sandboxHash) {
		log.Printf("sandbox: hash mismatch for %s: expected %s got %s", contractID, sandboxHash, actual)
		return
	}

	s.extractSandboxTarball(contractID, tarballBytes, resultsDir)
}

// findSandboxHash searches proposal and contract metadata for sandbox_hash.
// It tries multiple ID variations to handle the wish-<hash> / proposalID /
// visible_pixel_hash mismatch.
func (s *Server) findSandboxHash(ctx context.Context, contractID, normalizedID string) string {
	// 1. Direct proposal lookup by contractID.
	if p, err := s.store.GetProposal(ctx, contractID); err == nil && p.Metadata != nil {
		if h := strings.TrimSpace(toString(p.Metadata["sandbox_hash"])); h != "" {
			return h
		}
	}
	// 2. Proposal lookup by normalizedID (wish-<hash>).
	if normalizedID != contractID {
		if p, err := s.store.GetProposal(ctx, normalizedID); err == nil && p.Metadata != nil {
			if h := strings.TrimSpace(toString(p.Metadata["sandbox_hash"])); h != "" {
				return h
			}
		}
	}
	// 3. Strip wish- prefix and search by raw visible_pixel_hash.
	vph := strings.TrimPrefix(normalizedID, "wish-")
	if vph != normalizedID && vph != contractID {
		if p, err := s.store.GetProposal(ctx, vph); err == nil && p.Metadata != nil {
			if h := strings.TrimSpace(toString(p.Metadata["sandbox_hash"])); h != "" {
				return h
			}
		}
	}
	// 4. List proposals filtering by contract ID.
	if proposals, err := s.store.ListProposals(ctx, smart_contract.ProposalFilter{ContractID: contractID}); err == nil {
		for _, p := range proposals {
			if h := strings.TrimSpace(toString(p.Metadata["sandbox_hash"])); h != "" {
				return h
			}
		}
	}
	// 5. Check contract metadata directly.
	if c, err := s.store.GetContract(contractID); err == nil && c.Metadata != nil {
		if h := strings.TrimSpace(toString(c.Metadata["sandbox_hash"])); h != "" {
			return h
		}
	}
	// 6. Try with origin_proposal_id from contract metadata.
	if c, err := s.store.GetContract(contractID); err == nil && c.Metadata != nil {
		if opID := strings.TrimSpace(toString(c.Metadata["origin_proposal_id"])); opID != "" {
			if p, err := s.store.GetProposal(ctx, opID); err == nil && p.Metadata != nil {
				if h := strings.TrimSpace(toString(p.Metadata["sandbox_hash"])); h != "" {
					return h
				}
			}
		}
	}
	return ""
}

// findSandboxCID returns sandbox_tarball_cid for backward compatibility with
// older publishers that stored a CID instead of relying on hash-based lookup.
func (s *Server) findSandboxCID(ctx context.Context, contractID, normalizedID string) string {
	for _, id := range []string{contractID, normalizedID, strings.TrimPrefix(normalizedID, "wish-")} {
		if p, err := s.store.GetProposal(ctx, id); err == nil && p.Metadata != nil {
			if cid := strings.TrimSpace(toString(p.Metadata["sandbox_tarball_cid"])); cid != "" {
				return cid
			}
		}
	}
	if c, err := s.store.GetContract(contractID); err == nil && c.Metadata != nil {
		if cid := strings.TrimSpace(toString(c.Metadata["sandbox_tarball_cid"])); cid != "" {
			return cid
		}
	}
	return ""
}

// extractSandboxTarball extracts a tar archive to the results directory.
func (s *Server) extractSandboxTarball(contractID string, tarballBytes []byte, resultsDir string) {
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		log.Printf("sandbox: failed to create results dir %s: %v", resultsDir, err)
		return
	}
	gr, err := gzip.NewReader(bytes.NewReader(tarballBytes))
	if err != nil {
		log.Printf("sandbox: gzip open failed for %s: %v", contractID, err)
		return
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	fileCount := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("sandbox: tar read error for %s: %v", contractID, err)
			return
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		outPath := filepath.Join(resultsDir, filepath.FromSlash(hdr.Name))
		// Guard against path traversal.
		if !strings.HasPrefix(filepath.Clean(outPath), filepath.Clean(resultsDir)) {
			log.Printf("sandbox: skipping path traversal in tarball: %s", hdr.Name)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			log.Printf("sandbox: mkdir failed for %s: %v", outPath, err)
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			log.Printf("sandbox: read entry %s failed: %v", hdr.Name, err)
			continue
		}
		if err := os.WriteFile(outPath, data, 0644); err != nil {
			log.Printf("sandbox: write %s failed: %v", outPath, err)
			continue
		}
		fileCount++
	}

	log.Printf("sandbox: extracted %d files for contract %s to %s", fileCount, contractID, resultsDir)
}


