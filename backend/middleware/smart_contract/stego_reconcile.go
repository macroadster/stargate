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

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"

	"stargate-backend/core/smart_contract"
	"stargate-backend/ipfs"
	"stargate-backend/services"
	"stargate-backend/stego"
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
	if proxyBase == "" {
		proxyBase = "http://localhost:8080"
	}
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

// ReconcileStego reconciles a stego image from IPFS and upserts contracts/tasks.
func (s *Server) ReconcileStego(ctx context.Context, stegoCID, expectedHash string) error {
	_, err := s.reconcileStegoFromIPFS(ctx, stegoCID, expectedHash)
	return err
}

// ReconcileStegoWithAnnouncement reconciles a stego image using embedded announcement data.
// This is for the new architecture where contract info is embedded in the stego image
// and passed via IPFS pubsub announcement.
func (s *Server) ReconcileStegoWithAnnouncement(ctx context.Context, ann *stegoAnnouncement) error {
	// Download stego image from IPFS
	ipfsClient := ipfs.NewClientFromEnv()
	stegoBytes, err := ipfsClient.Cat(ctx, ann.StegoCID)
	if err != nil {
		return fmt.Errorf("ipfs cat stego failed: %w", err)
	}

	// Write stego image to /data/uploads with hash-only filename for stealth
	uploadsDir := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}
	// Use the CID hash as filename (remove any extension for stealth)
	filename := ann.StegoCID
	if parts := strings.Split(ann.StegoCID, "."); len(parts) > 1 {
		filename = parts[0] // Remove extension if present
	}
	uploadPath := filepath.Join(uploadsDir, filename)
	if err := os.WriteFile(uploadPath, stegoBytes, 0644); err != nil {
		return fmt.Errorf("failed to write stego image: %w", err)
	}
	log.Printf("stego: wrote stego image to %s (%d bytes)", uploadPath, len(stegoBytes))

	// Build manifest from announcement data
	manifest := stego.Manifest{
		SchemaVersion:    1,
		ProposalID:       ann.ProposalID,
		VisiblePixelHash: ann.VisiblePixelHash,
		PayloadCID:       ann.PayloadCID,
		CreatedAt:        time.Now().Unix(),
		Issuer:           ann.Issuer,
	}

	// Download payload from IPFS
	var payload stego.Payload
	if ann.PayloadCID != "" {
		payloadBytes, err := ipfsClient.Cat(ctx, ann.PayloadCID)
		if err != nil {
			return fmt.Errorf("ipfs cat payload failed: %w", err)
		}
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return fmt.Errorf("payload json decode failed: %w", err)
		}
	}

	// Determine contract ID
	contractID := strings.TrimSpace(ann.ExpectedHash)
	if contractID == "" && ann.ProposalID != "" {
		contractID = ann.ProposalID
	}
	if contractID == "" {
		return fmt.Errorf("unable to determine contract ID from announcement")
	}

	// Create hash of stego image
	sum := sha256.Sum256(stegoBytes)
	stegoHash := hex.EncodeToString(sum[:])

	// Upsert contract from payload
	if err := s.upsertContractFromStegoPayload(ctx, contractID, ann.StegoCID, stegoHash, manifest, payload); err != nil {
		return fmt.Errorf("failed to upsert contract: %w", err)
	}

	// Ensure stego ingestion record
	s.ensureStegoIngestion(ctx, contractID, ann.StegoCID, stegoHash, stegoBytes, manifest)

	log.Printf("stego: reconciled from announcement: contract_id=%s, stego_cid=%s", contractID, ann.StegoCID)
	return nil
}

func (s *Server) reconcileStegoFromIPFS(ctx context.Context, stegoCID string, expectedHash string) (stegoReconcileResponse, error) {
	ipfsClient := ipfs.NewClientFromEnv()
	stegoBytes, err := ipfsClient.Cat(ctx, stegoCID)
	if err != nil {
		return stegoReconcileResponse{}, fmt.Errorf("ipfs cat stego failed: %w", err)
	}
	sum := sha256.Sum256(stegoBytes)
	stegoHash := hex.EncodeToString(sum[:])
	if expectedHash != "" && !strings.EqualFold(expectedHash, stegoHash) {
		return stegoReconcileResponse{}, fmt.Errorf("stego hash mismatch: expected %s got %s", expectedHash, stegoHash)
	}
	manifestBytes, err := extractStegoManifest(ctx, stegoBytes, loadStegoReconcileConfig())
	if err != nil {
		return stegoReconcileResponse{}, err
	}
	manifest, err := stego.ParseManifestYAML(manifestBytes)
	if err != nil {
		return stegoReconcileResponse{}, err
	}
	contractID := strings.TrimSpace(manifest.VisiblePixelHash)
	if contractID == "" {
		return stegoReconcileResponse{}, fmt.Errorf("manifest visible_pixel_hash missing")
	}
	payloadBytes, err := ipfsClient.Cat(ctx, manifest.PayloadCID)
	if err != nil {
		return stegoReconcileResponse{}, fmt.Errorf("ipfs cat payload failed: %w", err)
	}
	var payload stego.Payload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return stegoReconcileResponse{}, fmt.Errorf("payload json decode failed: %w", err)
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

func extractStegoManifest(ctx context.Context, image []byte, cfg stegoReconcileConfig) ([]byte, error) {
	if strings.TrimSpace(cfg.ProxyBase) == "" {
		return nil, fmt.Errorf("stego proxy base not configured")
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("image", "stego.png")
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, bytes.NewReader(image)); err != nil {
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
	contract := smart_contract.Contract{
		ContractID:      contractID,
		Title:           proposal.Title,
		TotalBudgetSats: proposal.BudgetSats,
		GoalsCount:      1,
		Status:          "active",
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
			// Generate hashlock commitment script for donation sweeping
			pixelHashBytes, err := hex.DecodeString(manifest.VisiblePixelHash)
			if err != nil {
				log.Printf("stego reconcile: failed to decode visible pixel hash for task %s: %v", t.TaskID, err)
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
						merkleProof = contractorProof
					} else {
						// Update contractor-specific fields while preserving existing data
						merkleProof.ContractorWallet = contractorProof.ContractorWallet
						merkleProof.CommitmentAddress = contractorProof.CommitmentAddress
						merkleProof.CommitmentRedeemScript = contractorProof.CommitmentRedeemScript
						merkleProof.CommitmentRedeemHash = contractorProof.CommitmentRedeemHash
						merkleProof.VisiblePixelHash = contractorProof.VisiblePixelHash
						if merkleProof.SeenAt.IsZero() {
							merkleProof.SeenAt = contractorProof.SeenAt
						}
					}
				}
			}
		}

		tasks = append(tasks, smart_contract.Task{
			TaskID:           t.TaskID,
			ContractID:       contractID,
			GoalID:           contractID,
			Title:            t.Title,
			Description:      t.Description,
			BudgetSats:       t.BudgetSats,
			Skills:           t.Skills,
			Status:           "available",
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
