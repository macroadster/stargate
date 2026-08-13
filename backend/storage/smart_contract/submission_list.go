package smart_contract

import (
	"strings"

	"stargate-backend/core/smart_contract"
)

func submissionTaskIDs(filter smart_contract.SubmissionFilter) []string {
	seen := make(map[string]struct{})
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	add(filter.TaskID)
	for _, id := range filter.TaskIDs {
		add(id)
	}
	return ids
}

// submissionContractIDs expands a caller-supplied contract id into the forms
// stored on tasks (bare, wish-, proposal-, contract-).
func submissionContractIDs(id string) []string {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	prefixes := []string{"wish-", "proposal-", "contract-", "task-"}
	bare := id
	for _, p := range prefixes {
		if strings.HasPrefix(strings.ToLower(bare), p) {
			bare = bare[len(p):]
			break
		}
	}
	seen := make(map[string]struct{})
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	add(id)
	add(bare)
	for _, p := range prefixes {
		add(p + bare)
	}
	return out
}

func contractIDMatches(stored, filter string) bool {
	if filter == "" {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(stored), strings.TrimSpace(filter)) {
		return true
	}
	for _, v := range submissionContractIDs(filter) {
		if strings.EqualFold(stored, v) {
			return true
		}
	}
	return false
}

func applySubmissionPage(subs []smart_contract.Submission, filter smart_contract.SubmissionFilter) []smart_contract.Submission {
	start := filter.Offset
	if start < 0 {
		start = 0
	}
	if start > len(subs) {
		return []smart_contract.Submission{}
	}
	end := len(subs)
	if filter.Limit > 0 && start+filter.Limit < end {
		end = start + filter.Limit
	}
	return subs[start:end]
}
