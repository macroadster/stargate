package smart_contract

import (
	"encoding/json"
	"strings"

	"stargate-backend/core/smart_contract"
)

func populateProposalTasks(p *smart_contract.Proposal) {
	if p == nil {
		return
	}
	if p.BudgetSats == 0 {
		p.BudgetSats = DefaultBudgetSats()
		if p.Metadata == nil {
			p.Metadata = map[string]interface{}{}
		}
		p.Metadata["budget_sats"] = p.BudgetSats
	}
	if p.Metadata == nil {
		p.Metadata = map[string]interface{}{}
	}
	if _, ok := p.Metadata["funding_address"]; !ok {
		p.Metadata["funding_address"] = FundingAddressFromMeta(p.Metadata)
	}
	if tasksRaw, ok := p.Metadata["suggested_tasks"]; ok {
		var tasks []smart_contract.Task
		if b, err := json.Marshal(tasksRaw); err == nil {
			_ = json.Unmarshal(b, &tasks)
		}
		if len(tasks) > 0 {
			p.Tasks = tasks
			return
		}
	}
	if em, ok := p.Metadata["embedded_message"].(string); ok && em != "" && len(p.Tasks) == 0 {
		p.Tasks = BuildTasksFromMarkdown(p.ID, em, p.VisiblePixelHash, p.BudgetSats, FundingAddressFromMeta(p.Metadata))
	}
	if len(p.Tasks) == 0 {
		desc := strings.TrimSpace(p.DescriptionMD)
		if desc != "" {
			p.Tasks = BuildTasksFromMarkdown(p.ID, desc, p.VisiblePixelHash, p.BudgetSats, FundingAddressFromMeta(p.Metadata))
		}
	}
}
