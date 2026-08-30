package smart_contract

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"stargate-backend/core/smart_contract"
)

// BuildTasksFromMarkdown derives meaningful tasks from a markdown proposal.
// Only creates tasks from properly formatted task sections, not arbitrary bullet points.
func BuildTasksFromMarkdown(proposalID, markdown string, visibleHash string, budget int64, fundingAddress string) []smart_contract.Task {
	md := strings.TrimSpace(markdown)
	lines := strings.Split(md, "\n")

	// Determine the canonical contract ID for these tasks
	// Priority: wish-prefix hash > visible hash > proposal ID
	canonicalContractID := proposalID
	if visibleHash != "" {
		canonicalContractID = "wish-" + strings.TrimPrefix(visibleHash, "wish-")
	}

	// Extract structured tasks from proper task sections, then allocate
	// budgets so the sum never exceeds the original wish/proposal budget.
	type draftTask struct {
		title string
		desc  string
	}
	var drafts []draftTask

	for i, line := range lines {
		trim := strings.TrimSpace(line)

		// Look for proper task headers: "### Task X:" or "### Task X "
		if strings.HasPrefix(trim, "### Task ") {
			title := strings.TrimSpace(strings.TrimPrefix(trim, "### Task "))

			// Remove numbering and colon if present
			if colonIdx := strings.Index(title, ":"); colonIdx > 0 {
				title = strings.TrimSpace(title[colonIdx+1:])
			} else if spaceIdx := strings.Index(title, " "); spaceIdx > 0 {
				// Also handle "### Task 1 Title"
				firstWord := title[:spaceIdx]
				isNumeric := true
				for _, r := range firstWord {
					if !unicode.IsDigit(r) && !unicode.IsPunct(r) {
						isNumeric = false
						break
					}
				}
				if isNumeric {
					title = strings.TrimSpace(title[spaceIdx+1:])
				}
			}

			// Skip if title looks like metadata/budget/success criteria
			if isTaskTitle(title) {
				drafts = append(drafts, draftTask{
					title: title,
					desc:  extractTaskDescription(md, i),
				})
			}
		}
	}

	if len(drafts) == 0 {
		taskID := fmt.Sprintf("%s-task-1", proposalID)
		return []smart_contract.Task{{
			TaskID:      taskID,
			ContractID:  canonicalContractID,
			GoalID:      "wish",
			Title:       "Comprehensive Implementation",
			Description: md,
			BudgetSats:  budget,
			Skills:      []string{"planning", "development", "testing", "documentation"},
			Status:      "available",
			MerkleProof: &smart_contract.MerkleProof{
				VisiblePixelHash:   visibleHash,
				FundedAmountSats:   budget,
				FundingAddress:     fundingAddress,
				ConfirmationStatus: "provisional",
			},
		}}
	}

	titles := make([]string, len(drafts))
	explicit := make([]int64, len(drafts))
	for i, d := range drafts {
		titles[i] = d.title
		explicit[i] = parseExplicitTaskSats(d.title, d.desc)
	}
	amounts := AllocateTaskBudgets(titles, explicit, budget)

	tasks := make([]smart_contract.Task, 0, len(drafts))
	for i, d := range drafts {
		amount := amounts[i]
		tasks = append(tasks, smart_contract.Task{
			TaskID:      fmt.Sprintf("%s-task-%d", proposalID, i+1),
			ContractID:  canonicalContractID,
			GoalID:      "wish",
			Title:       d.title,
			Description: d.desc,
			BudgetSats:  amount,
			Skills:      extractTaskSkills(d.title),
			Status:      "available",
			MerkleProof: &smart_contract.MerkleProof{
				VisiblePixelHash:   visibleHash,
				FundedAmountSats:   amount,
				FundingAddress:     fundingAddress,
				ConfirmationStatus: "provisional",
			},
		})
	}

	return tasks
}

// isTaskTitle determines if a title represents a real task vs metadata/budget/success criteria
func isTaskTitle(title string) bool {
	title = strings.ToLower(strings.TrimSpace(title))
	if title == "" {
		return false
	}

	// Exclude metadata and information sections
	nonTaskPatterns := []string{
		"contract id", "contract details", "contract type", "created",
		"budget", "total budget", "budget breakdown", "cost", "price",
		"timeline", "schedule", "duration", "estimated", "phase", "milestone",
		"success metrics", "criteria", "kpi", "deliverables", "outcomes",
		"technical requirements", "requirements", "specifications", "scope",
		"skills", "expertise", "qualifications", "experience",
		"metadata", "information", "details", "notes",
	}

	for _, pattern := range nonTaskPatterns {
		if strings.Contains(title, pattern) {
			return false
		}
	}

	// Include meaningful task indicators
	taskPatterns := []string{
		"implement", "develop", "build", "create", "design", "integration",
		"analysis", "planning", "research", "assessment", "evaluation",
		"testing", "qa", "quality", "validation", "verification",
		"documentation", "writing", "guide", "manual", "instructions",
		"deployment", "setup", "configuration", "installation",
		"optimization", "improvement", "enhancement", "refactoring",
		"security", "hardening", "audit", "review", "assessment",
		"database", "schema", "migration", "setup", "optimization",
		"frontend", "ui", "interface", "user experience", "design",
		"backend", "api", "service", "server", "architecture",
		"work", "fix", "update", "add", "remove", "move",
	}

	for _, pattern := range taskPatterns {
		if strings.Contains(title, pattern) {
			return true
		}
	}

	// Default: treat as task if long enough and doesn't match non-task patterns
	return len(title) > 5
}

// extractTaskDescription extracts relevant description for a task from markdown
func extractTaskDescription(markdown string, startIndex int) string {
	lines := strings.Split(markdown, "\n")
	if startIndex >= len(lines) {
		return markdown
	}

	// Skip the task header line and collect content
	var descLines []string

	for i := startIndex + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		// Stop at next task or major section
		if strings.HasPrefix(line, "### Task ") || strings.HasPrefix(line, "## ") {
			break
		}

		// Collect non-empty lines
		if line != "" {
			descLines = append(descLines, lines[i])
		}
	}

	if len(descLines) == 0 {
		// If no content found, return default description
		return "Task implementation as described"
	}

	return strings.Join(descLines, "\n")
}

// extractTaskSkills determines appropriate skills based on task title
func extractTaskSkills(title string) []string {
	title = strings.ToLower(strings.TrimSpace(title))

	skillsMap := map[string][]string{
		"implement":     {"development", "implementation", "coding"},
		"develop":       {"development", "implementation", "coding"},
		"build":         {"development", "implementation", "coding"},
		"create":        {"development", "implementation", "design"},
		"design":        {"design", "ux", "ui", "architecture"},
		"analysis":      {"planning", "analysis", "research", "evaluation"},
		"planning":      {"planning", "analysis", "project-management"},
		"research":      {"planning", "analysis", "research", "evaluation"},
		"testing":       {"testing", "qa", "validation", "quality-assurance"},
		"qa":            {"testing", "qa", "validation", "quality-assurance"},
		"documentation": {"documentation", "technical-writing", "communication"},
		"deployment":    {"devops", "deployment", "infrastructure", "configuration"},
		"security":      {"security", "audit", "hardening", "review"},
		"database":      {"database", "backend", "data-management"},
		"frontend":      {"frontend", "ui", "ux", "design"},
		"backend":       {"backend", "api", "server", "architecture"},
		"api":           {"backend", "api", "server", "architecture"},
	}

	for keyword, skills := range skillsMap {
		if strings.Contains(title, keyword) {
			return skills
		}
	}

	return []string{"planning", "development", "testing"}
}

// calculateTaskBudget assigns a share of totalBudget. Prefer AllocateTaskBudgets
// when splitting a wish across several tasks so the sum cannot exceed the cap.
func calculateTaskBudget(title string, totalBudget int64, taskCount int) int64 {
	if totalBudget <= 0 || taskCount <= 0 {
		return totalBudget
	}
	amounts := AllocateTaskBudgets([]string{title}, nil, totalBudget)
	if len(amounts) == 0 {
		return totalBudget / int64(taskCount)
	}
	return amounts[0]
}

// parseExplicitTaskSats reads a caller-specified sat amount from a task title or body.
// Accepts "Budget: 500 sats", "budget_sats: 500", or "(500 sats)".
func parseExplicitTaskSats(title, description string) int64 {
	for _, text := range []string{title, description} {
		if n := firstSatsAmount(text); n > 0 {
			return n
		}
	}
	return 0
}

func firstSatsAmount(text string) int64 {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	lower := strings.ToLower(text)
	keys := []string{"budget_sats", "budget", "amount_sats", "amount", "price"}
	for _, key := range keys {
		idx := strings.Index(lower, key)
		if idx < 0 {
			continue
		}
		rest := text[idx+len(key):]
		rest = strings.TrimLeft(rest, " \t:=-")
		if n, ok := leadingInt64(rest); ok && n > 0 {
			return n
		}
	}
	if i := strings.Index(lower, "sats"); i > 0 {
		start := i - 1
		for start >= 0 && (unicode.IsSpace(rune(text[start])) || text[start] == '(') {
			start--
		}
		end := start + 1
		for start >= 0 && unicode.IsDigit(rune(text[start])) {
			start--
		}
		num := strings.TrimSpace(text[start+1 : end])
		if n, err := strconv.ParseInt(num, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func leadingInt64(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && unicode.IsDigit(rune(s[i])) {
		i++
	}
	if i == 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(s[:i], 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
