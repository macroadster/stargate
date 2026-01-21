package smart_contract

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
	"stargate-backend/ipfs"
	"stargate-backend/services"
	scstore "stargate-backend/storage/smart_contract"
)

// publishProposalEvent publishes a proposal creation event via IPFS for cross-instance sync
func publishProposalEvent(ctx context.Context, proposal smart_contract.Proposal) error {
	// Check if sync is enabled
	if os.Getenv("STARGATE_SYNC_ENABLE") == "false" {
		return nil
	}

	// Get sync configuration
	topic := "stargate-stego" // Default topic
	if envTopic := os.Getenv("STARGATE_SYNC_TOPIC"); envTopic != "" {
		topic = envTopic
	}

	// Create sync announcement for proposal creation
	ann := map[string]interface{}{
		"type":     "proposal_create",
		"proposal": &proposal,
		"issuer":   os.Getenv("STARGATE_SYNC_ISSUER"),
	}

	if ann["issuer"] == "" {
		ann["issuer"] = "stargate-backend"
	}
	ann["timestamp"] = time.Now().Unix()

	data, err := json.Marshal(ann)
	if err != nil {
		return err
	}

	// Use IPFS client for publishing
	client := ipfs.NewClientFromEnv()
	return client.PubsubPublish(ctx, topic, data)
}

// StartIngestionSync polls starlight_ingestions for pending records, validates embedded payloads,
// and upserts contracts/tasks into the MCP store. It requires a Postgres-backed store.
func StartIngestionSync(ctx context.Context, dsn string, store Store, interval time.Duration) error {
	pgStore, ok := store.(*scstore.PGStore)
	if !ok {
		return errors.New("ingestion sync requires Postgres store")
	}
	ingest, err := services.NewIngestionService(dsn)
	if err != nil {
		return fmt.Errorf("init ingestion service: %w", err)
	}

	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := syncOnce(ctx, ingest, pgStore); err != nil {
					log.Printf("ingestion sync error: %v", err)
				}
			}
		}
	}()
	return nil
}

type embeddedTask struct {
	TaskID         string                 `json:"task_id"`
	ContractID     string                 `json:"contract_id"`
	GoalID         string                 `json:"goal_id"`
	Title          string                 `json:"title"`
	Description    string                 `json:"description"`
	BudgetSats     int64                  `json:"budget_sats"`
	SkillsRequired []string               `json:"skills_required"`
	Status         string                 `json:"status"`
	ClaimedBy      *string                `json:"claimed_by"`
	ClaimExpiresAt *string                `json:"claim_expires_at"`
	MerkleProof    map[string]interface{} `json:"merkle_proof"`
}

type embeddedContract struct {
	ContractID          string         `json:"contract_id"`
	Title               string         `json:"title"`
	TotalBudgetSats     int64          `json:"total_budget_sats"`
	GoalsCount          int            `json:"goals_count"`
	AvailableTasksCount int            `json:"available_tasks_count"`
	Status              string         `json:"status"`
	Tasks               []embeddedTask `json:"tasks"`
}

func syncOnce(ctx context.Context, ingest *services.IngestionService, store *scstore.PGStore) error {
	recs, err := ingest.ListRecent("pending", 25)
	if err != nil {
		return err
	}
	if len(recs) == 0 {
		return nil
	}
	var okCount, errCount int
	for _, rec := range recs {
		if err := processRecord(ctx, rec, ingest, store); err != nil {
			log.Printf("ingestion %s failed: %v", rec.ID, err)
			errCount++
		} else {
			okCount++
		}
	}
	log.Printf("ingestion sync summary: processed=%d errors=%d pending_total=%d", okCount, errCount, len(recs))
	return nil
}

func processRecord(ctx context.Context, rec services.IngestionRecord, ingest *services.IngestionService, store *scstore.PGStore) error {
	meta := copyMeta(rec.Metadata)
	raw, _ := meta["embedded_message"].(string)
	if normalized, updatedMeta, err := normalizeEmbedded(raw, meta); err == nil {
		raw = normalized
		meta = updatedMeta
	}
	if looksLikeStegoManifestTextIngest(raw) {
		return ingest.UpdateStatusWithNote(rec.ID, "ignored", "stego manifest metadata")
	}
	if strings.TrimSpace(rec.ImageBase64) == "" {
		if strings.TrimSpace(metaString(meta["ipfs_image_cid"])) == "" {
			return ingest.UpdateStatusWithNote(rec.ID, "ignored", "missing image data")
		}
	}

	if store != nil {
		visible := strings.TrimSpace(metaString(meta["visible_pixel_hash"]))
		if visible == "" {
			visible = strings.TrimSpace(rec.ID)
		}
		if visible != "" {
			if existing, err := store.GetProposal(ctx, visible); err == nil && strings.TrimSpace(existing.ID) != "" {
				return ingest.UpdateStatusWithNote(rec.ID, "ignored", "proposal already exists for visible hash")
			}
			if _, err := store.GetContract(visible); err == nil {
				return ingest.UpdateStatusWithNote(rec.ID, "ignored", "contract already exists for visible hash")
			}
		}
	}

	// Try JSON contract first.
	contract, tasks, err := parseEmbeddedContract(raw)
	if err != nil || contract.ContractID == "" || len(tasks) == 0 {
		// Fallback: treat embedded_message as markdown wish -> create proposal AND contract.
		proposal, err := parseMarkdownProposal(rec.ID, raw, meta, rec.ImageBase64)
		if err != nil {
			return ingest.UpdateStatusWithNote(rec.ID, "invalid", fmt.Sprintf("parse error: %v", err))
		}
		if err := store.CreateProposal(ctx, proposal); err != nil {
			return ingest.UpdateStatusWithNote(rec.ID, "invalid", fmt.Sprintf("proposal upsert failed: %v", err))
		}
		// Publish proposal creation event to sync across instances
		if err := publishProposalEvent(ctx, proposal); err != nil {
			log.Printf("failed to publish proposal create event for %s: %v", proposal.ID, err)
		}
		// Also create contract for wishes so they show up in contracts list
		if proposal.ID != "" {
			wishContract := smart_contract.Contract{
				ContractID:          proposal.ID,
				Title:               proposal.Title,
				TotalBudgetSats:     proposal.BudgetSats,
				GoalsCount:          len(proposal.Tasks),
				AvailableTasksCount: len(proposal.Tasks),
				Status:              proposal.Status,
				Skills:              proposal.Tasks[0].Skills,
			}
			if err := store.UpsertContractWithTasks(ctx, wishContract, proposal.Tasks); err != nil {
				log.Printf("wish contract creation failed for %s: %v", proposal.ID, err)
			}
		}
		return ingest.UpdateStatusWithNote(rec.ID, "verified", "proposal and contract created; awaiting approval")
	}

	// If no visible_pixel_hash provided, derive from image for each task.
	for i, t := range tasks {
		if (t.MerkleProof == nil || t.MerkleProof.VisiblePixelHash == "") && rec.ImageBase64 != "" {
			if h, err := hashBase64(rec.ImageBase64); err == nil {
				if t.MerkleProof == nil {
					t.MerkleProof = &smart_contract.MerkleProof{}
				}
				t.MerkleProof.VisiblePixelHash = h
			}
		}
		// Mark unverified if proof is still empty.
		if t.MerkleProof == nil {
			t.Status = "unverified"
		}
		tasks[i] = t
	}

	if err := store.UpsertContractWithTasks(ctx, contract, tasks); err != nil {
		return ingest.UpdateStatusWithNote(rec.ID, "invalid", fmt.Sprintf("upsert failed: %v", err))
	}

	return ingest.UpdateStatusWithNote(rec.ID, "verified", "ingested into MCP")
}

func parseEmbeddedContract(raw string) (smart_contract.Contract, []smart_contract.Task, error) {
	if raw == "" {
		return smart_contract.Contract{}, nil, errors.New("no embedded message")
	}
	var embedded embeddedContract
	if err := json.Unmarshal([]byte(raw), &embedded); err != nil {
		return smart_contract.Contract{}, nil, err
	}
	contract := smart_contract.Contract{
		ContractID:          embedded.ContractID,
		Title:               embedded.Title,
		TotalBudgetSats:     embedded.TotalBudgetSats,
		GoalsCount:          embedded.GoalsCount,
		AvailableTasksCount: embedded.AvailableTasksCount,
		Status:              embedded.Status,
	}

	tasks := make([]smart_contract.Task, 0, len(embedded.Tasks))
	for _, et := range embedded.Tasks {
		t := smart_contract.Task{
			TaskID:      et.TaskID,
			ContractID:  et.ContractID,
			GoalID:      et.GoalID,
			Title:       et.Title,
			Description: et.Description,
			BudgetSats:  et.BudgetSats,
			Skills:      et.SkillsRequired,
			Status:      et.Status,
			MerkleProof: decodeProof(et.MerkleProof),
		}
		tasks = append(tasks, t)
	}
	return contract, tasks, nil
}

// parseMarkdownWish converts a markdown wish + metadata (budget/funding address) into a contract+tasks.
// parseMarkdownProposal builds a proposal from markdown wish and suggested tasks.
func parseMarkdownProposal(ingestionID, markdown string, meta map[string]interface{}, imageBase64 string) (smart_contract.Proposal, error) {
	md := strings.TrimSpace(markdown)
	if md == "" {
		return smart_contract.Proposal{}, errors.New("empty markdown wish")
	}

	lines := strings.Split(md, "\n")
	title := strings.TrimSpace(lines[0])
	if strings.HasPrefix(title, "#") {
		title = strings.TrimLeft(title, "# ")
	}
	if title == "" {
		title = fmt.Sprintf("Wish %s", ingestionID)
	}

	var bullets []string
	for _, l := range lines {
		trim := strings.TrimSpace(l)
		if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") || strings.HasPrefix(trim, "+ ") {
			bullets = append(bullets, strings.TrimSpace(trim[2:]))
		}
	}
	if len(bullets) == 0 {
		// fallback single task
		bullets = []string{"Fulfill the wish described in the markdown"}
	}

	// Prefer visible pixel hash (from image scan) or the ingestionID directly to avoid duplicate wish-* wrappers.
	contractIDBase := strings.TrimSpace(ingestionID)
	var visibleHash string
	if meta != nil {
		if v, ok := meta["visible_pixel_hash"].(string); ok && strings.TrimSpace(v) != "" {
			visibleHash = strings.TrimSpace(v)
			contractIDBase = visibleHash
		}
	}
	contractID := contractIDBase
	if contractID == "" {
		contractID = fmt.Sprintf("wish-%s", ingestionID)
	}
	// Always use wish- prefix for contract ID when visible hash is available
	if visibleHash != "" && !strings.HasPrefix(contractID, "wish-") {
		contractID = fmt.Sprintf("wish-%s", visibleHash)
	}
	budget := budgetFromMeta(meta)
	fundingAddr := scstore.FundingAddressFromMeta(meta)

	// Default proof placeholder with funding info (provisional).
	defaultProof := &smart_contract.MerkleProof{
		VisiblePixelHash:   visibleHash,
		FundedAmountSats:   budget,
		FundingAddress:     fundingAddr,
		ConfirmationStatus: "provisional",
	}

	var tasks []smart_contract.Task
	for i, b := range bullets {
		taskID := fmt.Sprintf("%s-task-%d", contractID, i+1)
		tasks = append(tasks, smart_contract.Task{
			TaskID:      taskID,
			ContractID:  contractID,
			GoalID:      "wish",
			Title:       b,
			Description: md,
			BudgetSats:  budget / int64(len(bullets)),
			Skills:      []string{"planning", "manual-review", "proposal-discovery"},
			Status:      "available",
			MerkleProof: defaultProof,
		})
	}
	// Capture budget/address back into metadata for downstream display.
	if meta == nil {
		meta = map[string]interface{}{}
	}
	meta["budget_sats"] = budget
	meta["funding_address"] = fundingAddr
	if visibleHash != "" {
		meta["visible_pixel_hash"] = visibleHash
		meta["stego_contract_id"] = visibleHash
	}

	return smart_contract.Proposal{
		ID:               contractID,
		Title:            title,
		DescriptionMD:    md,
		VisiblePixelHash: defaultProof.VisiblePixelHash,
		BudgetSats:       budget,
		Status:           "pending",
		CreatedAt:        time.Now(),
		Tasks:            tasks,
		Metadata:         meta,
	}, nil
}

func budgetFromMeta(meta map[string]interface{}) int64 {
	if meta == nil {
		return scstore.DefaultBudgetSats()
	}
	if unit := strings.ToLower(strings.TrimSpace(metaString(meta["price_unit"]))); unit == "sats" {
		if sats := satsFromMetaValue(meta["price"]); sats > 0 {
			return sats
		}
	}
	// budget_sats or funding_btc fields
	if v, ok := meta["budget_sats"]; ok {
		switch t := v.(type) {
		case float64:
			return int64(t)
		case int64:
			return t
		case int:
			return int64(t)
		case json.Number:
			if i, err := t.Int64(); err == nil {
				return i
			}
		}
	}
	if sats := btcToSats(meta["funding_btc"]); sats > 0 {
		return sats
	}
	if sats := btcToSats(meta["price"]); sats > 0 {
		return sats
	}
	return scstore.DefaultBudgetSats()
}

func metaString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(t, 10)
	case int:
		return strconv.Itoa(t)
	case json.Number:
		return t.String()
	default:
		return ""
	}
}

func satsFromMetaValue(v interface{}) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
	case string:
		raw := strings.TrimSpace(t)
		if raw == "" {
			return 0
		}
		if strings.Contains(raw, ".") {
			if f, err := strconv.ParseFloat(raw, 64); err == nil {
				return int64(f)
			}
			return 0
		}
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return i
		}
	}
	return 0
}

func decodeProof(v map[string]interface{}) *smart_contract.MerkleProof {
	if v == nil {
		return nil
	}
	jsonBytes, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var proof smart_contract.MerkleProof
	if err := json.Unmarshal(jsonBytes, &proof); err != nil {
		return nil
	}
	return &proof
}

func hashBase64(data string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:]), nil
}

func copyMeta(meta map[string]interface{}) map[string]interface{} {
	if meta == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	return out
}

// normalizeEmbedded detects JSON wishes carrying message/price/address and flattens them into metadata.
func normalizeEmbedded(raw string, meta map[string]interface{}) (string, map[string]interface{}, error) {
	if raw == "" {
		return raw, meta, nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		cleaned, updated := stripWishTimestamp(raw, meta)
		return cleaned, updated, nil
	}
	updated := copyMeta(meta)
	if msg, ok := obj["message"].(string); ok && strings.TrimSpace(msg) != "" {
		raw = msg
		updated["embedded_message"] = msg
	}
	if addr, ok := obj["address"].(string); ok && strings.TrimSpace(addr) != "" {
		updated["funding_address"] = addr
		updated["address"] = addr
	}
	if unit, ok := obj["price_unit"].(string); ok && strings.TrimSpace(unit) != "" {
		updated["price_unit"] = strings.ToLower(strings.TrimSpace(unit))
	}
	if budget, ok := obj["budget_sats"]; ok {
		if sats := satsFromMetaValue(budget); sats > 0 {
			updated["budget_sats"] = sats
		}
	}
	if price, ok := obj["price"]; ok {
		updated["price"] = price
		unit := strings.ToLower(strings.TrimSpace(metaString(updated["price_unit"])))
		if unit == "sats" {
			if sats := satsFromMetaValue(price); sats > 0 {
				updated["budget_sats"] = sats
			}
		} else if sats := btcToSats(price); sats > 0 {
			updated["budget_sats"] = sats
		}
	}
	raw, updated = stripWishTimestamp(raw, updated)
	return raw, updated, nil
}

func stripWishTimestamp(message string, meta map[string]interface{}) (string, map[string]interface{}) {
	message = strings.TrimSpace(message)
	if message == "" {
		return message, meta
	}
	idx := strings.LastIndex(message, "\n\n[stargate-ts:")
	if idx < 0 {
		return message, meta
	}
	tsPart := strings.TrimSuffix(message[idx+len("\n\n[stargate-ts:"):], "]")
	if tsPart != "" {
		if ts, err := strconv.ParseInt(strings.TrimSpace(tsPart), 10, 64); err == nil {
			if meta == nil {
				meta = map[string]interface{}{}
			}
			if _, exists := meta["wish_timestamp"]; !exists {
				meta["wish_timestamp"] = ts
			}
		}
	}
	return strings.TrimSpace(message[:idx]), meta
}

func looksLikeStegoManifestTextIngest(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "schema_version:") &&
		strings.Contains(lower, "proposal_id:") &&
		strings.Contains(lower, "visible_pixel_hash:")
}

func btcToSats(v interface{}) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t * 1e8)
	case int64:
		return t * 1e8
	case string:
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return int64(f * 1e8)
		}
	}
	return 0
}
