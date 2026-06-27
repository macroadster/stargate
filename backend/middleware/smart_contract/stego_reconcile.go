package smart_contract

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
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

	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	"stargate-backend/stego"
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
	res, err := s.reconcileStegoFromIPFS(r.Context(), req.StegoCID, req.ExpectedHash)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, res)
}

// ReconcileStego reconciles a stego image and upserts contracts/tasks.
// It first tries to read the image from the local UPLOADS_DIR (by hash),
// falling back to IPFS if not found locally.
func (s *Server) ReconcileStego(ctx context.Context, stegoCID, expectedHash string) error {
	// Try local file first: if the stegoCID looks like a SHA256 hash,
	// look for UPLOADS_DIR/<hash> on disk (synced by IPFS mirror).
	if len(stegoCID) == 64 {
		if _, hexErr := hex.DecodeString(stegoCID); hexErr == nil {
			if err := s.reconcileStegoFromLocalFile(ctx, stegoCID); err == nil {
				return nil
			}
			// Fall through to IPFS if local file reconcile failed.
		}
	}
	_, err := s.reconcileStegoFromIPFS(ctx, stegoCID, expectedHash)
	return err
}

// reconcileStegoFromLocalFile reads a stego image from UPLOADS_DIR/<hash>
// and runs the same reconciliation as reconcileStegoFromIPFS.
func (s *Server) reconcileStegoFromLocalFile(ctx context.Context, stegoHash string) error {
	uploadsDir := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
	if uploadsDir == "" {
		return fmt.Errorf("UPLOADS_DIR not set")
	}
	stegoPath := filepath.Join(uploadsDir, stegoHash)
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
	log.Printf("stego: reconciling from local file %s (%d bytes)", stegoPath, len(stegoBytes))

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
	if err := s.upsertContractFromStegoPayload(ctx, contractID, stegoHash, stegoHash, manifest, payload); err != nil {
		return err
	}
	s.ensureStegoIngestion(ctx, contractID, stegoHash, stegoHash, stegoBytes, manifest)

	// If the contract is already confirmed, kick off sandbox extraction.
	if c, err := s.store.GetContract(contractID); err == nil {
		if strings.EqualFold(strings.TrimSpace(c.Status), "confirmed") {
			go s.downloadSandboxArtifacts(context.Background(), contractID)
		}
	}
	log.Printf("stego: reconciled from local file: contract_id=%s, hash=%s", contractID, stegoHash)
	return nil
}

// ReconcileStegoWithAnnouncement reconciles a stego image using embedded announcement data.
// This is for the new architecture where contract info is embedded in the stego image
// and passed via IPFS pubsub announcement.
func (s *Server) ReconcileStegoWithAnnouncement(ctx context.Context, ann *stegoAnnouncement) error {
	// Download stego image from IPFS
	ipfsClient := ipfs.NewClientFromEnv()
	if ipfsClient == nil {
		return fmt.Errorf("IPFS client is disabled - cannot reconcile stego")
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
	uploadPath := filepath.Join(uploadsDir, stegoHash)
	if err := os.WriteFile(uploadPath, stegoBytes, 0644); err != nil {
		return fmt.Errorf("failed to write stego image: %w", err)
	}
	log.Printf("stego: wrote stego image to %s (%d bytes)", uploadPath, len(stegoBytes))

	// Extract manifest + payload directly from the stego image.
	// v2 images contain the full payload inline (JSON); v1 images
	// only have a YAML manifest and require an IPFS fetch for the payload.
	rawBytes, err := extractStegoManifest(ctx, stegoBytes, loadStegoReconcileConfig())
	if err != nil {
		return fmt.Errorf("stego extraction failed: %w", err)
	}
	manifest, payload, err := stego.ParseEmbedded(rawBytes)
	if err != nil {
		return fmt.Errorf("stego parse failed: %w", err)
	}
	// Fall back to announcement fields when extraction gives empty manifest
	if manifest.ProposalID == "" {
		manifest.ProposalID = ann.ProposalID
	}
	if manifest.VisiblePixelHash == "" {
		manifest.VisiblePixelHash = ann.VisiblePixelHash
	}
	if manifest.Issuer == "" {
		manifest.Issuer = ann.Issuer
	}
	if manifest.CreatedAt <= 0 {
		manifest.CreatedAt = time.Now().Unix()
	}
	// v1 fallback: payload was not inline — fetch from IPFS
	if payload.SchemaVersion == 0 && manifest.PayloadCID != "" {
		payloadBytes, err := ipfsClient.Cat(ctx, manifest.PayloadCID)
		if err != nil {
			return fmt.Errorf("ipfs cat payload failed: %w", err)
		}
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return fmt.Errorf("payload json decode failed: %w", err)
		}
	}

	// Determine contract ID. Prefer wish hash (ExpectedHash / visible), fall back to
	// the stego image's own content hash so either can trigger the ingestion record.
	contractID := strings.TrimSpace(ann.ExpectedHash)
	if contractID == "" && ann.ProposalID != "" {
		contractID = ann.ProposalID
	}
	if contractID == "" {
		// stegoHash was computed above from the downloaded bytes.
		contractID = stegoHash
	}
	if contractID == "" {
		return fmt.Errorf("unable to determine contract ID from announcement")
	}

	// Upsert contract from payload
	if err := s.upsertContractFromStegoPayload(ctx, contractID, ann.StegoCID, stegoHash, manifest, payload); err != nil {
		return fmt.Errorf("failed to upsert contract: %w", err)
	}

	// Ensure stego ingestion record
	s.ensureStegoIngestion(ctx, contractID, ann.StegoCID, stegoHash, stegoBytes, manifest)

	// If the announcement carries sandbox_tarball_cid that wasn't in the
	// payload metadata, persist it so downloadSandboxArtifacts can find it.
	if cid := strings.TrimSpace(ann.SandboxTarballCID); cid != "" {
		proposalID := strings.TrimSpace(manifest.ProposalID)
		if proposalID == "" {
			proposalID = contractID
		}
		if p, err := s.store.GetProposal(ctx, proposalID); err == nil {
			pmeta := copyMeta(p.Metadata)
			if pmeta == nil {
				pmeta = map[string]interface{}{}
			}
			if strings.TrimSpace(toString(pmeta["sandbox_tarball_cid"])) == "" {
				pmeta["sandbox_tarball_cid"] = cid
				_ = s.store.UpdateProposalMetadata(ctx, p.ID, pmeta)
			}
		}
	}

	log.Printf("stego: reconciled from announcement: contract_id=%s, stego_cid=%s", contractID, ann.StegoCID)

	// If the contract is already confirmed (e.g. via prior chain observation or sync),
	// ensure AI artifacts are decompressed into the sandbox for serving.
	// This is especially relevant when donation funding was chosen:
	// the post-PSBT stego publish carries the sandbox tarball cid+hash, and on
	// confirmation remotes must have the files under UPLOADS_DIR/results/<id>/
	// so /sandbox/ and /uploads/results/ can serve them.
	if c, err := s.store.GetContract(contractID); err == nil {
		if strings.EqualFold(strings.TrimSpace(c.Status), "confirmed") {
			go s.downloadSandboxArtifacts(context.Background(), contractID)
		}
	}
	return nil
}

func (s *Server) reconcileStegoFromIPFS(ctx context.Context, stegoCID string, expectedHash string) (stegoReconcileResponse, error) {
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
	if err := s.upsertContractFromStegoPayload(ctx, contractID, stegoCID, stegoHash, manifest, payload); err != nil {
		return stegoReconcileResponse{}, err
	}
	s.ensureStegoIngestion(ctx, contractID, stegoCID, stegoHash, stegoBytes, manifest)
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
	img, _, err := image.Decode(bytes.NewReader(imageData))
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

func (s *Server) upsertContractFromStegoPayload(ctx context.Context, contractID, stegoCID, stegoHash string, manifest stego.Manifest, payload stego.Payload) error {
	if contractID == "" {
		return fmt.Errorf("contract id missing")
	}
	proposalID := strings.TrimSpace(manifest.ProposalID)
	if proposalID == "" {
		proposalID = contractID
	}
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
	}
	// Propagate sandbox artifact metadata so peers can download on confirmation.
	if manifest.SandboxHash != "" {
		meta["sandbox_hash"] = manifest.SandboxHash
	}
	for _, entry := range payload.Metadata {
		if entry.Key == "sandbox_tarball_cid" && strings.TrimSpace(entry.Value) != "" {
			meta["sandbox_tarball_cid"] = entry.Value
		}
	}
	// Merge payload metadata (includes funding_txid etc from PSBT time) so
	// hasIngestionPSBT can detect funded state for setting contract status.
	for k, v := range payloadMetadataMap(payload) {
		if _, ok := meta[k]; !ok {
			meta[k] = v
		}
	}
	if payload.Proposal.Title == "" {
		payload.Proposal.Title = "Stego Contract " + contractID
	}
	proposal := smart_contract.Proposal{
		ID:               proposalID,
		Title:            payload.Proposal.Title,
		DescriptionMD:    payload.Proposal.DescriptionMD,
		VisiblePixelHash: manifest.VisiblePixelHash,
		BudgetSats:       payload.Proposal.BudgetSats,
		Status:           "approved",
		CreatedAt:        time.Unix(payload.Proposal.CreatedAt, 0),
		Metadata:         meta,
	}
	if proposal.CreatedAt.IsZero() {
		proposal.CreatedAt = time.Now()
	}
	if err := s.store.CreateProposal(ctx, proposal); err != nil {
		return fmt.Errorf("create proposal failed: %w", err)
	}
	// For wish-style contracts (identified by 64-hex visible pixel hash), normalize
	// the stored ContractID to "wish-<hash>" for consistency with wish creation,
	// open-contracts listings, and GetContract calls. Non-wish or test contractIDs
	// (e.g. "contract-foo") are left as provided.
	vh := strings.TrimSpace(manifest.VisiblePixelHash)
	if vh == "" {
		vh = strings.TrimSpace(payload.Proposal.VisiblePixelHash)
	}
	if vh != "" && looksLikeHash(contractID) {
		contractID = "wish-" + strings.TrimPrefix(vh, "wish-")
	}
	// If the stego was published after PSBT (funding info present in meta), mark as funded
	// so finished contracts (PSBT built + artifacts) show as work completed / waiting
	// for on-chain confirmation.
	contractStatus := "active"
	if hasIngestionPSBT(meta) {
		contractStatus = "funded"
	}
	if existing, err := s.store.GetContract(contractID); err == nil {
		switch strings.ToLower(existing.Status) {
		case "confirmed", "completed", "superseded":
			contractStatus = existing.Status
		}
	}
	contract := smart_contract.Contract{
		ContractID:      contractID,
		Title:           proposal.Title,
		TotalBudgetSats: proposal.BudgetSats,
		GoalsCount:      1,
		Status:          contractStatus,
	}
	tasks := make([]smart_contract.Task, 0, len(payload.Tasks))
	for _, t := range payload.Tasks {
		if strings.TrimSpace(t.TaskID) == "" {
			continue
		}

		// Load existing task to preserve existing merkle_proof
		existingTask, err := s.store.GetTask(t.TaskID)
		var merkleProof *smart_contract.MerkleProof
		if err == nil && existingTask.MerkleProof != nil {
			// Preserve existing merkle_proof to avoid overwriting with nil
			merkleProof = existingTask.MerkleProof
		}

		// Update MerkleProof for commitment script - use hashlock script for donation sweeping
		// Do NOT overwrite with P2WPKH since contractors are paid directly via PSBT payouts
		if strings.TrimSpace(t.ContractorWallet) != "" {
			// The PSBT donation commitment uses the wish image hash (VisiblePixelHash).
			// The product image hash (stegoHash) is stored separately in ProductPixelHash
			// for the two-phase sweep: wish-hashlock → product-hashlock → donation addr.
			commitmentHashHex := manifest.VisiblePixelHash
			pixelHashBytes, err := hex.DecodeString(commitmentHashHex)
			if err != nil {
				log.Printf("stego reconcile: failed to decode commitment hash for task %s: %v", t.TaskID, err)
			} else {
				// Build hashlock script locally (same logic as PSBT builder)
				lockHash := sha256.Sum256(pixelHashBytes)
				builder := txscript.NewScriptBuilder()
				builder.AddOp(txscript.OP_SHA256)
				builder.AddData(lockHash[:])
				builder.AddOp(txscript.OP_EQUAL)
				redeemScript, err := builder.Script()
				if err != nil {
					log.Printf("stego reconcile: failed to build hashlock redeem script for task %s: %v", t.TaskID, err)
				} else {
					// Calculate script hashes for proper redemption matching
					scriptHash := sha256.Sum256(redeemScript)
					contractorProof := &smart_contract.MerkleProof{
						VisiblePixelHash:       manifest.VisiblePixelHash,
						CommitmentPixelHash:    commitmentHashHex,
						CommitmentSource:       "wish",
						ProductPixelHash:       stegoHash, // product hash for two-phase recommitment sweep
						ContractorWallet:       t.ContractorWallet,
						CommitmentAddress:      t.ContractorWallet, // Use contractor wallet as display address
						CommitmentRedeemScript: hex.EncodeToString(redeemScript),
						CommitmentRedeemHash:   hex.EncodeToString(scriptHash[:]),
						ConfirmationStatus:     "provisional",
						SeenAt:                 time.Now(),
					}

					// Merge with existing proof or use new one
					if merkleProof == nil {
						// Preserve funding fields from existing task if available
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
						// Fill funding fields from ingestion metadata if still
						// missing (peer nodes receive funding info via IPFS
						// announcement but the task proof may not have it yet).
						if contractorProof.TxID == "" || contractorProof.CommitmentVout == 0 {
							s.fillProofFromIngestion(contractID, contractorProof)
						}
						merkleProof = contractorProof
					} else {
						// Update contractor-specific fields while preserving existing data
						merkleProof.ContractorWallet = contractorProof.ContractorWallet
						merkleProof.CommitmentAddress = contractorProof.CommitmentAddress
						merkleProof.CommitmentRedeemScript = contractorProof.CommitmentRedeemScript
						merkleProof.CommitmentRedeemHash = contractorProof.CommitmentRedeemHash
						merkleProof.VisiblePixelHash = contractorProof.VisiblePixelHash
						merkleProof.CommitmentPixelHash = contractorProof.CommitmentPixelHash
						merkleProof.CommitmentSource = contractorProof.CommitmentSource
						merkleProof.ProductPixelHash = stegoHash
						if merkleProof.SeenAt.IsZero() {
							merkleProof.SeenAt = contractorProof.SeenAt
						}
						// Backfill funding fields that may have arrived later via IPFS.
						if merkleProof.TxID == "" || merkleProof.CommitmentVout == 0 {
							s.fillProofFromIngestion(contractID, merkleProof)
						}
					}
				}
			}
		}

		status := t.Status
		if status == "" {
			status = "available"
		}
		tasks = append(tasks, smart_contract.Task{
			TaskID:           t.TaskID,
			ContractID:       contractID,
			GoalID:           contractID,
			Title:            t.Title,
			Description:      t.Description,
			BudgetSats:       t.BudgetSats,
			Skills:           t.Skills,
			Status:           status,
			ContractorWallet: t.ContractorWallet,
			MerkleProof:      merkleProof,
		})
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
	resultsDir := filepath.Join(uploadsDir, "results", normalizedID)

	// If results already exist and match the expected hash, skip extraction.
	if info, err := os.Stat(resultsDir); err == nil && info.IsDir() {
		if err := stego.VerifySandboxHash(resultsDir, sandboxHash); err == nil {
			log.Printf("sandbox: artifacts already present and verified for %s", contractID)
			return
		}
		log.Printf("sandbox: artifacts present but hash mismatch for %s, re-extracting", contractID)
	}

	// Try to read the tarball from the local uploads directory first.
	// The publisher stores it as UPLOADS_DIR/<sandbox_hash> and the IPFS
	// mirror replicates it to peers using the same hash-based filename.
	tarballBytes, err := os.ReadFile(filepath.Join(uploadsDir, sandboxHash))
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
	tr := tar.NewReader(bytes.NewReader(tarballBytes))
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


