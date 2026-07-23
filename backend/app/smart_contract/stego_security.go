package smart_contract

// Trust model (IPFS vs chain):
//
//   IPFS / pubsub / mirror  → stage bytes on disk only (UPLOADS_DIR). No SQL.
//   Block monitor on-chain  → ReconcileStego(applySQL) → UpsertContractFromStegoPayload
//
// IPFS is public and untrusted. Open contracts must not appear from IPFS alone.

import (
	"log"
	"strings"

	"stargate-backend/core/smart_contract"
	"stargate-backend/stego"
	scstore "stargate-backend/storage/smart_contract"
)

// stegoPayloadMetadataAllowlist is the only payload.Metadata keys accepted from
// untrusted stego/IPFS carriers. Funding and status-bearing keys are excluded —
// those must arrive via chain observation or authenticated ingest updates.
var stegoPayloadMetadataAllowlist = map[string]struct{}{
	"sandbox_tarball_cid": {},
	"sandbox_hash":        {},
	"product_pixel_hash":  {},
	"result_summary":      {},
}

// stegoAllowedTaskStatuses limits task.status from stego payloads.
var stegoAllowedTaskStatuses = map[string]struct{}{
	"available": {},
	"pending":   {},
}

// filterStegoPayloadMetadata copies only allowlisted keys from payload metadata.
func filterStegoPayloadMetadata(payload stego.Payload) map[string]interface{} {
	raw := payloadMetadataMap(payload)
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(raw))
	for k, v := range raw {
		key := strings.ToLower(strings.TrimSpace(k))
		if _, ok := stegoPayloadMetadataAllowlist[key]; !ok {
			continue
		}
		out[key] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sanitizeStegoTask applies length/status/wallet validation for stego-sourced tasks.
// Invalid contractor wallets are stripped (not fatal) so replication still works.
func sanitizeStegoTask(t stego.PayloadTask) (smart_contract.Task, bool) {
	taskID := strings.TrimSpace(t.TaskID)
	if taskID == "" {
		return smart_contract.Task{}, false
	}
	title := strings.TrimSpace(t.Title)
	if len(title) > scstore.MaxTaskTitle {
		title = title[:scstore.MaxTaskTitle]
	}
	title, _ = scstore.SanitizeInput(title)

	desc := strings.TrimSpace(t.Description)
	if len(desc) > scstore.MaxTaskDescription {
		desc = desc[:scstore.MaxTaskDescription]
	}
	desc, _ = scstore.SanitizeInput(desc)

	status := strings.ToLower(strings.TrimSpace(t.Status))
	if status == "" {
		status = "available"
	}
	if _, ok := stegoAllowedTaskStatuses[status]; !ok {
		status = "available"
	}

	wallet := strings.TrimSpace(t.ContractorWallet)
	if wallet != "" {
		if err := scstore.ValidateBitcoinAddress(wallet); err != nil {
			log.Printf("stego security: dropping invalid contractor_wallet on task %s: %v", taskID, err)
			wallet = ""
		}
	}

	// Cap skills length / count to limit metadata abuse.
	skills := make([]string, 0, len(t.Skills))
	for _, sk := range t.Skills {
		sk = strings.TrimSpace(sk)
		if sk == "" {
			continue
		}
		if len(sk) > 64 {
			sk = sk[:64]
		}
		skills = append(skills, sk)
		if len(skills) >= 16 {
			break
		}
	}

	budget := t.BudgetSats
	if budget < 0 {
		budget = 0
	}

	task := smart_contract.Task{
		TaskID:           taskID,
		Title:            title,
		Description:      desc,
		BudgetSats:       budget,
		Skills:           skills,
		Status:           status,
		ContractorWallet: wallet,
	}
	// Final pass through ValidateTaskInput when wallet present (already validated).
	if err := scstore.ValidateTaskInput(task); err != nil {
		// Title/desc dangerous patterns: strip wallet-less retry after SanitizeInput already ran.
		log.Printf("stego security: task %s failed validation (%v); storing with empty title/desc if needed", taskID, err)
		if task.Title == "" && task.Description == "" {
			return smart_contract.Task{}, false
		}
	}
	return task, true
}

// stegoContractStatus chooses status for a contract created/updated from stego.
// Untrusted stego never promotes to funded/confirmed; existing protected statuses win.
func stegoContractStatus(existing *smart_contract.Contract) string {
	if existing != nil {
		st := strings.ToLower(strings.TrimSpace(existing.Status))
		switch st {
		case "confirmed", "completed", "superseded", "funded", "active":
			return existing.Status
		}
	}
	// New or unknown — replicated but not chain-verified.
	return "active"
}

// stegoProposalStatus never auto-approves from stego. Preserve higher trust if present.
func stegoProposalStatus(existing *smart_contract.Proposal) string {
	if existing != nil {
		st := strings.ToLower(strings.TrimSpace(existing.Status))
		switch st {
		case "approved", "published", "confirmed":
			return existing.Status
		}
	}
	return "pending"
}

// shouldPreserveContractFields is true when stego must not overwrite title/budget.
func shouldPreserveContractFields(existing *smart_contract.Contract) bool {
	if existing == nil {
		return false
	}
	st := strings.ToLower(strings.TrimSpace(existing.Status))
	switch st {
	case "confirmed", "completed", "superseded", "funded", "active":
		return true
	default:
		return false
	}
}
