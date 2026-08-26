package smart_contract

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"
	"time"

	"stargate-backend/core/identity"
	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	"stargate-backend/stego"
	"stargate-backend/storage/ipfs"
	scstore "stargate-backend/storage/smart_contract"
)

const starlightWishV1Type = "starlight-wish-v1"

// starlightWishV1 is the alpha-channel JSON published for a pending human wish.
// Only these fields are trusted from the image itself (not from pubsub).
type starlightWishV1 struct {
	Type         string      `json:"type"`
	Title        string      `json:"title"`
	Body         string      `json:"body"`
	Summary      string      `json:"summary"`
	Parties      []string    `json:"parties"`
	CreatedBy    string      `json:"created_by"`
	BudgetSats   json.Number `json:"budget_sats"`
	Compensation json.Number `json:"compensation_sats"`
	Price        string      `json:"price"`
	PriceUnit    string      `json:"price_unit"`
}

type parsedPendingWish struct {
	Title     string
	Body      string
	Budget    int64
	CreatedBy string
	Parties   []string
	Schema    string // starlight-wish-v1 | plain
}

// ingestPendingWishFromImage writes pending ingestion + proposal + wish-<hash>
// when the staged image is a human wish. v2 product stego and YAML manifests
// are left on disk only (block monitor applies those after on-chain confirm).
//
// Status is always pending. Funding txids, approved/funded/confirmed, and
// contractor wallets are never taken from this path.
func ingestPendingWishFromImage(ctx context.Context, blob []byte, cid string, ingest *services.IngestionService, store Store) {
	if len(blob) == 0 {
		return
	}
	sum := sha256.Sum256(blob)
	hash := hex.EncodeToString(sum[:])
	if store != nil && pendingWishAlreadyApplied(store, hash) {
		return
	}

	raw, err := extractWishAlpha(blob)
	if err != nil || len(raw) == 0 {
		return
	}
	wish, ok := parsePendingWishPayload(raw, hash)
	if !ok {
		return
	}

	if store == nil && ingest == nil {
		return
	}
	if err := applyPendingWish(ctx, hash, cid, blob, wish, ingest, store); err != nil {
		log.Printf("wish ingest: apply hash=%s: %v", hash, err)
		return
	}
	log.Printf("wish ingest: pending wish-%s title=%q schema=%s cid=%s", hash, truncateWishLog(wish.Title), wish.Schema, cid)
}

func extractWishAlpha(blob []byte) ([]byte, error) {
	img, _, err := stego.DecodeImage(blob)
	if err != nil {
		return nil, err
	}
	return stego.ExtractAlpha(img)
}

// parsePendingWishPayload accepts starlight-wish-v1 JSON or plain-text wishes
// (what /api/inscribe embeds). Product v2 JSON and YAML manifests are rejected.
func parsePendingWishPayload(raw []byte, hash string) (parsedPendingWish, bool) {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return parsedPendingWish{}, false
	}
	// v2 product payload / YAML manifest — chain path only.
	if _, _, err := stego.ParseEmbedded(raw); err == nil {
		return parsedPendingWish{}, false
	}
	if looksLikeStegoManifestTextIngest(text) {
		return parsedPendingWish{}, false
	}

	var v1 starlightWishV1
	if json.Unmarshal(raw, &v1) == nil && strings.EqualFold(strings.TrimSpace(v1.Type), starlightWishV1Type) {
		title := strings.TrimSpace(v1.Title)
		body := strings.TrimSpace(v1.Body)
		if body == "" {
			body = strings.TrimSpace(v1.Summary)
		}
		if title == "" {
			title = firstWishLine(body)
		}
		budget := jsonNumberSats(v1.BudgetSats)
		if budget <= 0 {
			budget = jsonNumberSats(v1.Compensation)
		}
		if budget <= 0 && strings.EqualFold(strings.TrimSpace(v1.PriceUnit), "sats") {
			budget = priceSatsFromString(v1.Price)
		}
		parties := sanitizeWishParties(v1.Parties)
		return parsedPendingWish{
			Title:     title,
			Body:      body,
			Budget:    budget,
			CreatedBy: strings.TrimSpace(v1.CreatedBy),
			Parties:   parties,
			Schema:    starlightWishV1Type,
		}, title != "" || body != ""
	}

	// Plain text / markdown from /api/inscribe. Only ingest when the file is
	// a tracked ephemeral wish (stargate-wishes), not a random uploads blob.
	if !ipfs.IsTrackedWish(hash) {
		return parsedPendingWish{}, false
	}
	plain := stripEmbeddedWishTimestamp(text)
	title := firstWishLine(plain)
	if title == "" {
		title = "Wish " + hash
	}
	return parsedPendingWish{
		Title:  title,
		Body:   plain,
		Schema: "plain",
	}, true
}

func applyPendingWish(ctx context.Context, hash, cid string, blob []byte, wish parsedPendingWish, ingest *services.IngestionService, store Store) error {
	title := sanitizeWishTitle(wish.Title, hash)
	body := sanitizeWishBody(wish.Body)
	if body == "" {
		body = title
	}
	wishID := identity.ToWishID(hash)
	createdAt := time.Now()
	if rec, ok := ipfs.LookupWish(hash); ok && rec.CreatedAt > 0 {
		createdAt = time.Unix(rec.CreatedAt, 0)
	}

	meta := map[string]interface{}{
		"embedded_message":   body,
		"message":            body,
		"visible_pixel_hash": hash,
		"ingestion_id":       hash,
		"wish_schema":        wish.Schema,
		"replicated_wish":    true,
	}
	if cid != "" {
		meta["ipfs_image_cid"] = cid
		meta["ipfs_topic"] = ipfs.WishTopic()
	}
	if wish.Budget > 0 {
		meta["budget_sats"] = wish.Budget
	}
	if wish.CreatedBy != "" {
		meta["created_by"] = wish.CreatedBy
	}
	if len(wish.Parties) > 0 {
		meta["parties"] = wish.Parties
	}

	if ingest != nil {
		rec := services.IngestionRecord{
			ID:            hash,
			Filename:      hash,
			Method:        "alpha",
			MessageLength: len(body),
			ImageBase64:   base64.StdEncoding.EncodeToString(blob),
			Metadata:      meta,
			Status:        "pending",
			CreatedAt:     createdAt,
		}
		if err := ingest.Create(rec); err != nil {
			log.Printf("wish ingest: ingestion create %s: %v", hash, err)
		}
	}

	if store == nil {
		return nil
	}
	if pendingWishAlreadyApplied(store, hash) {
		return nil
	}

	if existing, err := store.GetProposal(ctx, hash); err != nil || strings.TrimSpace(existing.ID) == "" {
		proposal := smart_contract.Proposal{
			ID:               hash,
			Title:            title,
			DescriptionMD:    body,
			VisiblePixelHash: hash,
			BudgetSats:       wish.Budget,
			Status:           smart_contract.ProposalStatusPending,
			CreatedAt:        createdAt,
			Metadata: map[string]interface{}{
				"visible_pixel_hash": hash,
				"wish_schema":        wish.Schema,
				"replicated_wish":    true,
			},
		}
		if err := store.CreateProposal(ctx, proposal); err != nil {
			log.Printf("wish ingest: proposal create %s: %v", hash, err)
		}
	}

	contract := smart_contract.Contract{
		ContractID:      wishID,
		Title:           title,
		TotalBudgetSats: wish.Budget,
		GoalsCount:      0,
		Status:          "pending",
		StegoImageURL:   "/uploads/" + hash,
		CreatedAt:       createdAt,
		Metadata:        meta,
	}
	return store.UpsertContractWithTasks(ctx, contract, nil)
}

func pendingWishAlreadyApplied(store Store, hash string) bool {
	if store == nil || hash == "" {
		return false
	}
	for _, id := range identity.CandidateIDs(hash, hash) {
		if c, err := store.GetContract(id); err == nil && strings.TrimSpace(c.ContractID) != "" {
			return true
		}
	}
	return false
}

func sanitizeWishTitle(title, hash string) string {
	title = strings.TrimSpace(title)
	title, _ = scstore.SanitizeInput(title)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Wish " + hash
	}
	if len(title) > scstore.MaxProposalTitle {
		title = title[:scstore.MaxProposalTitle]
	}
	return title
}

func sanitizeWishBody(body string) string {
	body = strings.TrimSpace(body)
	body, _ = scstore.SanitizeInput(body)
	if len(body) > scstore.MaxProposalDesc {
		body = body[:scstore.MaxProposalDesc]
	}
	return body
}

func sanitizeWishParties(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, p := range in {
		p = strings.TrimSpace(p)
		p, _ = scstore.SanitizeInput(p)
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) > 200 {
			p = p[:200]
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
		if len(out) >= 16 {
			break
		}
	}
	return out
}

func firstWishLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "#")
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > scstore.MaxProposalTitle {
				return line[:scstore.MaxProposalTitle]
			}
			return line
		}
	}
	return ""
}

func stripEmbeddedWishTimestamp(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return message
	}
	idx := strings.LastIndex(message, "\n\n[stargate-ts:")
	if idx < 0 {
		return message
	}
	return strings.TrimSpace(message[:idx])
}

func jsonNumberSats(n json.Number) int64 {
	if n == "" {
		return 0
	}
	if i, err := n.Int64(); err == nil && i > 0 {
		return i
	}
	if f, err := n.Float64(); err == nil && f > 0 {
		return int64(f)
	}
	return 0
}

func truncateWishLog(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 80 {
		return s
	}
	return s[:80] + "…"
}
