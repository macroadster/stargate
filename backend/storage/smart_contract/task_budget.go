package smart_contract

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"stargate-backend/core/identity"
	"stargate-backend/core/smart_contract"
)

// BudgetSnapshot is the wish-price cap and current task allocations for a contract.
// RemainingSats is unallocated (wish - sum of all task budgets).
// ClaimableSats is the max this task may take (wish - sum of other tasks).
type BudgetSnapshot struct {
	Contract       smart_contract.Contract
	Task           smart_contract.Task
	Siblings       []smart_contract.Task
	WishBudgetSats int64
	AllocatedSats  int64
	RemainingSats  int64
	ClaimableSats  int64
}

// WishBudgetFromContract returns the original wish price cap.
func WishBudgetFromContract(c smart_contract.Contract) int64 {
	if c.TotalBudgetSats > 0 {
		return c.TotalBudgetSats
	}
	if c.Metadata == nil {
		return 0
	}
	for _, key := range []string{"wish_price_sats", "budget_sats"} {
		if n := metaToInt64(c.Metadata[key]); n > 0 {
			return n
		}
	}
	return 0
}

// SumTaskBudgets sums BudgetSats, optionally excluding one task.
func SumTaskBudgets(tasks []smart_contract.Task, excludeTaskID string) int64 {
	var sum int64
	excludeTaskID = strings.TrimSpace(excludeTaskID)
	for _, t := range tasks {
		if excludeTaskID != "" && t.TaskID == excludeTaskID {
			continue
		}
		if t.BudgetSats > 0 {
			sum += t.BudgetSats
		}
	}
	return sum
}

// LookupContract resolves a contract id across wish-/bare-hash aliases.
func LookupContract(store Store, contractID string) (smart_contract.Contract, error) {
	contractID = strings.TrimSpace(contractID)
	if store == nil || contractID == "" {
		return smart_contract.Contract{}, fmt.Errorf("contract not found")
	}
	seen := map[string]struct{}{}
	var last error
	try := func(id string) (smart_contract.Contract, bool) {
		id = strings.TrimSpace(id)
		if id == "" {
			return smart_contract.Contract{}, false
		}
		if _, ok := seen[id]; ok {
			return smart_contract.Contract{}, false
		}
		seen[id] = struct{}{}
		c, err := store.GetContract(id)
		if err != nil {
			last = err
			return smart_contract.Contract{}, false
		}
		if strings.TrimSpace(c.ContractID) == "" {
			return smart_contract.Contract{}, false
		}
		return c, true
	}
	if c, ok := try(contractID); ok {
		return c, nil
	}
	for _, id := range identity.CandidateIDs(contractID, "") {
		if c, ok := try(id); ok {
			return c, nil
		}
	}
	if n := identity.Normalize(contractID); n != "" {
		if c, ok := try(identity.ToWishID(n)); ok {
			return c, nil
		}
		if c, ok := try(n); ok {
			return c, nil
		}
	}
	if last != nil {
		return smart_contract.Contract{}, last
	}
	return smart_contract.Contract{}, fmt.Errorf("contract not found")
}

// ListSiblingTasks returns unique tasks for a contract and its id aliases.
func ListSiblingTasks(store Store, contractID string) []smart_contract.Task {
	if store == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []smart_contract.Task
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		tasks, err := store.ListTasks(smart_contract.TaskFilter{ContractID: id})
		if err != nil {
			return
		}
		for _, t := range tasks {
			if strings.TrimSpace(t.TaskID) == "" {
				continue
			}
			if _, ok := seen[t.TaskID]; ok {
				continue
			}
			seen[t.TaskID] = struct{}{}
			out = append(out, t)
		}
	}
	add(contractID)
	for _, id := range identity.CandidateIDs(contractID, "") {
		add(id)
	}
	if n := identity.Normalize(contractID); n != "" {
		add(identity.ToWishID(n))
		add(n)
	}
	return out
}

// LoadBudgetSnapshotForContract builds a snapshot without a specific task.
func LoadBudgetSnapshotForContract(store Store, contractID string) (BudgetSnapshot, error) {
	var snap BudgetSnapshot
	c, err := LookupContract(store, contractID)
	if err != nil {
		return snap, err
	}
	snap.Contract = c
	snap.WishBudgetSats = WishBudgetFromContract(c)
	snap.Siblings = ListSiblingTasks(store, firstNonEmpty(c.ContractID, contractID))
	snap.recompute()
	return snap, nil
}

// LoadBudgetSnapshot loads the wish cap and sibling allocations for a task.
func LoadBudgetSnapshot(store Store, taskID string) (BudgetSnapshot, error) {
	var snap BudgetSnapshot
	if store == nil {
		return snap, fmt.Errorf("store unavailable")
	}
	task, err := store.GetTask(taskID)
	if err != nil {
		return snap, err
	}
	snap.Task = task
	c, err := LookupContract(store, task.ContractID)
	if err == nil {
		snap.Contract = c
		snap.WishBudgetSats = WishBudgetFromContract(c)
	}
	snap.Siblings = ListSiblingTasks(store, firstNonEmpty(c.ContractID, task.ContractID))
	// Ensure the current task is present even if ListTasks missed an alias.
	found := false
	for i, t := range snap.Siblings {
		if t.TaskID == task.TaskID {
			snap.Siblings[i] = task
			found = true
			break
		}
	}
	if !found {
		snap.Siblings = append(snap.Siblings, task)
	}
	snap.recompute()
	return snap, nil
}

func (s *BudgetSnapshot) recompute() {
	others := SumTaskBudgets(s.Siblings, s.Task.TaskID)
	self := s.Task.BudgetSats
	if self < 0 {
		self = 0
	}
	s.AllocatedSats = others + self
	if s.WishBudgetSats <= 0 {
		s.RemainingSats = 0
		s.ClaimableSats = 0
		return
	}
	s.RemainingSats = s.WishBudgetSats - s.AllocatedSats
	if s.RemainingSats < 0 {
		s.RemainingSats = 0
	}
	s.ClaimableSats = s.WishBudgetSats - others
	if s.ClaimableSats < 0 {
		s.ClaimableSats = 0
	}
}

// ValidateAmount checks a proposed task amount against the wish cap.
func (s BudgetSnapshot) ValidateAmount(amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("amount_sats must be a positive number")
	}
	if s.WishBudgetSats > 0 && amount > s.ClaimableSats {
		return fmt.Errorf("amount_sats %d exceeds remaining wish budget %d (wish %d sats)", amount, s.ClaimableSats, s.WishBudgetSats)
	}
	return nil
}

// ValidateNewTaskBudget checks a brand-new task against unallocated remainder.
func (s BudgetSnapshot) ValidateNewTaskBudget(amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("budget_sats must be a positive number")
	}
	if s.WishBudgetSats > 0 && amount > s.RemainingSats {
		return fmt.Errorf("budget_sats %d exceeds remaining wish budget %d (wish %d sats)", amount, s.RemainingSats, s.WishBudgetSats)
	}
	return nil
}

// ApplyTaskAmount sets a task's budget_sats if it fits the wish remainder.
func ApplyTaskAmount(ctx context.Context, store Store, taskID string, amount int64) (BudgetSnapshot, error) {
	snap, err := LoadBudgetSnapshot(store, taskID)
	if err != nil {
		return snap, err
	}
	if err := snap.ValidateAmount(amount); err != nil {
		return snap, err
	}
	task := snap.Task
	task.BudgetSats = amount
	if task.MerkleProof == nil {
		task.MerkleProof = &smart_contract.MerkleProof{}
	} else {
		cp := *task.MerkleProof
		task.MerkleProof = &cp
	}
	task.MerkleProof.FundedAmountSats = amount
	if err := store.UpsertTask(ctx, task); err != nil {
		return snap, err
	}
	snap.Task = task
	for i, t := range snap.Siblings {
		if t.TaskID == task.TaskID {
			snap.Siblings[i] = task
			break
		}
	}
	snap.recompute()
	return snap, nil
}

// AnnotateTasksWithBudget fills computed wish/remaining fields for API responses.
func AnnotateTasksWithBudget(store Store, tasks []smart_contract.Task) []smart_contract.Task {
	if store == nil || len(tasks) == 0 {
		return tasks
	}
	cache := map[string]BudgetSnapshot{}
	for i := range tasks {
		cid := strings.TrimSpace(tasks[i].ContractID)
		snap, ok := cache[cid]
		if !ok {
			loaded, err := LoadBudgetSnapshotForContract(store, cid)
			if err != nil {
				continue
			}
			snap = loaded
			cache[cid] = snap
		}
		others := SumTaskBudgets(snap.Siblings, tasks[i].TaskID)
		tasks[i].WishBudgetSats = snap.WishBudgetSats
		tasks[i].AllocatedSats = others + tasks[i].BudgetSats
		if snap.WishBudgetSats > 0 {
			claimable := snap.WishBudgetSats - others
			if claimable < 0 {
				claimable = 0
			}
			tasks[i].RemainingBudgetSats = claimable
		}
	}
	return tasks
}

// AllocateTaskBudgets splits total across tasks so the sum never exceeds total.
// Positive entries in explicit keep those amounts (scaled down if they overflow).
func AllocateTaskBudgets(titles []string, explicit []int64, total int64) []int64 {
	n := len(titles)
	out := make([]int64, n)
	if n == 0 {
		return out
	}
	if total <= 0 {
		for i := range out {
			if i < len(explicit) && explicit[i] > 0 {
				out[i] = explicit[i]
			}
		}
		return out
	}
	if len(explicit) < n {
		tmp := make([]int64, n)
		copy(tmp, explicit)
		explicit = tmp
	}

	var reserved int64
	unset := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if explicit[i] > 0 {
			out[i] = explicit[i]
			reserved += explicit[i]
		} else {
			unset = append(unset, i)
		}
	}
	if reserved > total {
		// Scale explicit amounts so they fit the wish cap.
		scaled := int64(0)
		for i := 0; i < n; i++ {
			if out[i] <= 0 {
				continue
			}
			out[i] = total * out[i] / reserved
			if out[i] < 1 {
				out[i] = 1
			}
			scaled += out[i]
		}
		if scaled > total {
			out[lastPositive(out)] -= scaled - total
		}
		return out
	}
	remaining := total - reserved
	if len(unset) == 0 {
		return out
	}
	weights := make([]int64, len(unset))
	var weightSum int64
	for i, idx := range unset {
		w := taskBudgetWeight(titles[idx])
		if w <= 0 {
			w = 1
		}
		weights[i] = w
		weightSum += w
	}
	if weightSum <= 0 {
		weightSum = int64(len(unset))
		for i := range weights {
			weights[i] = 1
		}
	}
	used := int64(0)
	for i, idx := range unset {
		if i == len(unset)-1 {
			share := remaining - used
			if share < 0 {
				share = 0
			}
			out[idx] = share
			break
		}
		share := remaining * weights[i] / weightSum
		if share < 1 && remaining-used > 0 {
			share = 1
		}
		out[idx] = share
		used += share
	}
	return out
}

func taskBudgetWeight(title string) int64 {
	title = strings.ToLower(strings.TrimSpace(title))
	switch {
	case strings.Contains(title, "planning") || strings.Contains(title, "analysis"):
		return 20
	case strings.Contains(title, "implement") || strings.Contains(title, "develop") || strings.Contains(title, "build"):
		return 50
	case strings.Contains(title, "test") || strings.Contains(title, "qa") || strings.Contains(title, "validation"):
		return 20
	case strings.Contains(title, "document") || strings.Contains(title, "guide"):
		return 10
	default:
		return 25
	}
}

func lastPositive(vals []int64) int {
	for i := len(vals) - 1; i >= 0; i-- {
		if vals[i] > 0 {
			return i
		}
	}
	if len(vals) == 0 {
		return 0
	}
	return len(vals) - 1
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func metaToInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return i
		}
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		if err == nil {
			return i
		}
	}
	return 0
}
