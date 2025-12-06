package mcp

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

	"stargate-backend/services"
)

// StartIngestionSync polls starlight_ingestions for pending records, validates embedded payloads,
// and upserts contracts/tasks into the MCP store. It requires a Postgres-backed store.
func StartIngestionSync(ctx context.Context, dsn string, store Store, interval time.Duration) error {
	pgStore, ok := store.(*PGStore)
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

func syncOnce(ctx context.Context, ingest *services.IngestionService, store *PGStore) error {
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

func processRecord(ctx context.Context, rec services.IngestionRecord, ingest *services.IngestionService, store *PGStore) error {
	meta := copyMeta(rec.Metadata)
	raw, _ := meta["embedded_message"].(string)
	if normalized, updatedMeta, err := normalizeEmbedded(raw, meta); err == nil {
		raw = normalized
		meta = updatedMeta
	}

	// Try JSON contract first.
	contract, tasks, err := parseEmbeddedContract(raw)
	if err != nil || contract.ContractID == "" || len(tasks) == 0 {
		// Fallback: treat embedded_message as markdown wish -> create proposal only.
		proposal, err := parseMarkdownProposal(rec.ID, raw, meta, rec.ImageBase64)
		if err != nil {
			return ingest.UpdateStatusWithNote(rec.ID, "invalid", fmt.Sprintf("parse error: %v", err))
		}
		if err := store.CreateProposal(ctx, proposal); err != nil {
			return ingest.UpdateStatusWithNote(rec.ID, "invalid", fmt.Sprintf("proposal upsert failed: %v", err))
		}
		return ingest.UpdateStatusWithNote(rec.ID, "verified", "proposal created; awaiting approval")
	}

	// If no visible_pixel_hash provided, derive from image for each task.
	for i, t := range tasks {
		if (t.MerkleProof == nil || t.MerkleProof.VisiblePixelHash == "") && rec.ImageBase64 != "" {
			if h, err := hashBase64(rec.ImageBase64); err == nil {
				if t.MerkleProof == nil {
					t.MerkleProof = &MerkleProof{}
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

func parseEmbeddedContract(raw string) (Contract, []Task, error) {
	if raw == "" {
		return Contract{}, nil, errors.New("no embedded message")
	}
	var embedded embeddedContract
	if err := json.Unmarshal([]byte(raw), &embedded); err != nil {
		return Contract{}, nil, err
	}
	contract := Contract{
		ContractID:          embedded.ContractID,
		Title:               embedded.Title,
		TotalBudgetSats:     embedded.TotalBudgetSats,
		GoalsCount:          embedded.GoalsCount,
		AvailableTasksCount: embedded.AvailableTasksCount,
		Status:              embedded.Status,
	}

	tasks := make([]Task, 0, len(embedded.Tasks))
	for _, et := range embedded.Tasks {
		t := Task{
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
func parseMarkdownProposal(ingestionID, markdown string, meta map[string]interface{}, imageBase64 string) (Proposal, error) {
	md := strings.TrimSpace(markdown)
	if md == "" {
		return Proposal{}, errors.New("empty markdown wish")
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

	contractID := fmt.Sprintf("wish-%s", ingestionID)
	budget := budgetFromMeta(meta)
	fundingAddr := fundingAddressFromMeta(meta)

	// Default proof placeholder with funding info (provisional).
	defaultProof := &MerkleProof{
		FundedAmountSats:   budget,
		FundingAddress:     fundingAddr,
		ConfirmationStatus: "provisional",
	}
	if h, err := hashBase64(imageBase64); err == nil {
		defaultProof.VisiblePixelHash = h
	}

	var tasks []Task
	for i, b := range bullets {
		taskID := fmt.Sprintf("%s-task-%d", contractID, i+1)
		tasks = append(tasks, Task{
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

	return Proposal{
		ID:               contractID,
		Title:            title,
		DescriptionMD:    md,
		VisiblePixelHash: defaultProof.VisiblePixelHash,
		BudgetSats:       budget,
		Status:           "pending",
		Tasks:            tasks,
		Metadata:         meta,
	}, nil
}

func budgetFromMeta(meta map[string]interface{}) int64 {
	if meta == nil {
		return defaultBudgetSats()
	}
	// budget_sats or funding_btc fields
	if v, ok := meta["budget_sats"]; ok {
		switch t := v.(type) {
		case float64:
			return int64(t)
		case int64:
			return t
		}
	}
	if sats := btcToSats(meta["funding_btc"]); sats > 0 {
		return sats
	}
	if sats := btcToSats(meta["price"]); sats > 0 {
		return sats
	}
	return defaultBudgetSats()
}

func decodeProof(v map[string]interface{}) *MerkleProof {
	if v == nil {
		return nil
	}
	jsonBytes, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var proof MerkleProof
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
		return raw, meta, nil
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
	if price, ok := obj["price"]; ok {
		updated["price"] = price
		if sats := btcToSats(price); sats > 0 {
			updated["budget_sats"] = sats
		}
	}
	return raw, updated, nil
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

func defaultBudgetSats() int64 {
	if raw := os.Getenv("MCP_DEFAULT_BUDGET_SATS"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			return v
		}
	}
	// default mock budget for simulations
	return 100_000
}

func fundingAddressFromMeta(meta map[string]interface{}) string {
	if meta != nil {
		if v, ok := meta["funding_address"].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
		if v, ok := meta["address"].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	if v := os.Getenv("MCP_DEFAULT_FUNDING_ADDRESS"); strings.TrimSpace(v) != "" {
		return v
	}
	return "bc1p-simulated-funding-address"
}
