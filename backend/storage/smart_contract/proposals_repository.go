package smart_contract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"stargate-backend/core/smart_contract"
)

// ProposalsRepository handles proposal-related database operations
type ProposalsRepository struct {
	pool *pgxpool.Pool
}

// NewProposalsRepository creates a new proposals repository
func NewProposalsRepository(pool *pgxpool.Pool) *ProposalsRepository {
	return &ProposalsRepository{pool: pool}
}

// List returns proposals filtered by criteria
func (r *ProposalsRepository) List(ctx context.Context, filter smart_contract.ProposalFilter) ([]smart_contract.Proposal, error) {
	query := `SELECT id, title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at FROM mcp_proposals`
	var args []interface{}
	if filter.Status != "" {
		query += " WHERE status=$1"
		args = append(args, filter.Status)
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []smart_contract.Proposal
	for rows.Next() {
		var p smart_contract.Proposal
		var meta []byte
		if err := rows.Scan(&p.ID, &p.Title, &p.DescriptionMD, &p.VisiblePixelHash, &p.BudgetSats, &p.Status, &meta, &p.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(meta, &p.Metadata)
		populateProposalTasks(&p)

		if filter.ContractID != "" {
			var candidates []string
			if v, ok := p.Metadata["contract_id"].(string); ok {
				candidates = append(candidates, v)
			}
			if v, ok := p.Metadata["ingestion_id"].(string); ok {
				candidates = append(candidates, v)
			}
			if v, ok := p.Metadata["visible_pixel_hash"].(string); ok {
				candidates = append(candidates, v)
			}
			candidates = append(candidates, p.VisiblePixelHash, p.ID)
			match := false
			for _, candidate := range candidates {
				if strings.TrimSpace(candidate) == strings.TrimSpace(filter.ContractID) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		if filter.MinBudget > 0 && p.BudgetSats < filter.MinBudget {
			continue
		}
		if len(filter.Skills) > 0 && !proposalHasSkills(p, filter.Skills) {
			continue
		}
		out = append(out, p)
	}

	if filter.Offset > 0 && filter.Offset < len(out) {
		out = out[filter.Offset:]
	}
	if filter.MaxResults > 0 && filter.MaxResults < len(out) {
		out = out[:filter.MaxResults]
	}
	return out, rows.Err()
}

// Get returns a proposal by ID
func (r *ProposalsRepository) Get(ctx context.Context, id string) (smart_contract.Proposal, error) {
	var p smart_contract.Proposal
	var meta []byte
	err := r.pool.QueryRow(ctx, `
SELECT id, title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at
FROM mcp_proposals WHERE id=$1
`, id).Scan(&p.ID, &p.Title, &p.DescriptionMD, &p.VisiblePixelHash, &p.BudgetSats, &p.Status, &meta, &p.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return smart_contract.Proposal{}, fmt.Errorf("proposal %s not found", id)
		}
		return smart_contract.Proposal{}, err
	}
	_ = json.Unmarshal(meta, &p.Metadata)
	populateProposalTasks(&p)
	return p, nil
}

// Create stores a new proposal with validation
func (r *ProposalsRepository) Create(ctx context.Context, p smart_contract.Proposal) error {
	if p.Metadata == nil {
		p.Metadata = map[string]interface{}{}
	}
	if strings.TrimSpace(p.VisiblePixelHash) != "" {
		if vph, ok := p.Metadata["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			p.Metadata["visible_pixel_hash"] = p.VisiblePixelHash
		}
	}
	if metaContract, ok := p.Metadata["contract_id"].(string); ok {
		metaContract = strings.TrimSpace(metaContract)
		if metaContract != "" {
			if metaHash, ok2 := p.Metadata["visible_pixel_hash"].(string); ok2 {
				metaHash = strings.TrimSpace(metaHash)
				if metaHash != "" && metaHash != metaContract {
					return fmt.Errorf("visible_pixel_hash must match contract_id when both are set")
				}
			}
		}
	}

	// Comprehensive security validation
	if err := ValidateProposalInput(&p); err != nil {
		return fmt.Errorf("proposal validation failed: %v", err)
	}

	// Validate status field
	if p.Status == "" {
		p.Status = "pending"
	} else if !isValidProposalStatus(p.Status) {
		return fmt.Errorf("invalid proposal status: %s (must be one of: pending, approved, rejected, published)", p.Status)
	}

	metaMap := p.Metadata
	if metaMap == nil {
		metaMap = map[string]interface{}{}
	}
	if len(p.Tasks) > 0 {
		metaMap["suggested_tasks"] = p.Tasks
	}
	meta, _ := json.Marshal(metaMap)
	_, err := r.pool.Exec(ctx, `
INSERT INTO mcp_proposals (id, title, description_md, visible_pixel_hash, budget_sats, status, metadata, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,COALESCE($8, now()))
ON CONFLICT (id) DO UPDATE SET
  status = EXCLUDED.status,
  metadata = EXCLUDED.metadata,
  title = EXCLUDED.title,
  description_md = EXCLUDED.description_md,
  visible_pixel_hash = EXCLUDED.visible_pixel_hash,
  budget_sats = EXCLUDED.budget_sats
`, p.ID, p.Title, p.DescriptionMD, p.VisiblePixelHash, p.BudgetSats, p.Status, string(meta), p.CreatedAt)
	return err
}

// UpdateMetadata updates proposal metadata without status restrictions
func (r *ProposalsRepository) UpdateMetadata(ctx context.Context, id string, updates map[string]interface{}) error {
	if strings.TrimSpace(id) == "" || len(updates) == 0 {
		return nil
	}

	var metaJSON []byte
	var visiblePixelHash string
	if err := r.pool.QueryRow(ctx, `
SELECT metadata, visible_pixel_hash
FROM mcp_proposals WHERE id=$1
`, id).Scan(&metaJSON, &visiblePixelHash); err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return fmt.Errorf("proposal %s not found", id)
		}
		return err
	}

	var meta map[string]interface{}
	_ = json.Unmarshal(metaJSON, &meta)
	if meta == nil {
		meta = map[string]interface{}{}
	}
	for k, v := range updates {
		meta[k] = v
	}
	if strings.TrimSpace(visiblePixelHash) != "" {
		if vph, ok := meta["visible_pixel_hash"].(string); !ok || strings.TrimSpace(vph) == "" {
			meta["visible_pixel_hash"] = visiblePixelHash
		}
	}
	metaOut, _ := json.Marshal(meta)

	_, err := r.pool.Exec(ctx, `
UPDATE mcp_proposals
SET metadata=$2
WHERE id=$1
`, id, string(metaOut))
	return err
}
