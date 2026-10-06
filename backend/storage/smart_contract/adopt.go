package smart_contract

import (
	"context"
	"strings"

	"stargate-backend/core/identity"
	core "stargate-backend/core/smart_contract"
)

// forceSuperseder is implemented by SQL and Memory stores so adopt can collapse
// a leftover wish- twin even when that row is status=confirmed.
type forceSuperseder interface {
	ForceSupersedeContract(ctx context.Context, id string) error
}

type storeUnwrapper interface {
	Unwrap() Store
}

// AdoptPixelHashToCanonical makes the bare VPH the live contract row.
//
// If only wish-<hash> exists, copy it onto the bare PK, remap tasks, and
// supersede the prefix row. If both exist, keep the bare row and supersede
// the leftover wish- twin. A legacy contract-<64-hex> row is folded onto the
// bare PK as well: that alias was the publish primary key, so its status and
// tasks move with it. If neither alias exists, this is a no-op (caller inserts).
func AdoptPixelHashToCanonical(ctx context.Context, store Store, id string) (string, error) {
	canonical := identity.CanonicalContractID(id)
	if store == nil {
		return firstNonEmpty(canonical, strings.TrimSpace(id)), nil
	}
	if canonical == "" || !identity.IsPixelHash(canonical) {
		return firstNonEmpty(canonical, strings.TrimSpace(id)), nil
	}
	wishID := identity.ToWishID(canonical)

	bare, bareErr := store.GetContract(canonical)
	haveBare := bareErr == nil && strings.TrimSpace(bare.ContractID) != ""
	wish, wishErr := store.GetContract(wishID)
	haveWish := wishErr == nil && strings.TrimSpace(wish.ContractID) != ""

	if haveBare {
		if haveWish && !strings.EqualFold(strings.TrimSpace(wish.Status), "superseded") {
			if err := forceSupersedeContract(ctx, store, wishID); err != nil {
				return canonical, err
			}
		}
	} else if haveWish {
		tasks := ListSiblingTasks(store, wishID)
		adopted := wish
		adopted.ContractID = canonical
		for i := range tasks {
			tasks[i].ContractID = canonical
		}
		if err := store.UpsertContractWithTasks(ctx, adopted, tasks); err != nil {
			return canonical, err
		}
		if err := forceSupersedeContract(ctx, store, wishID); err != nil {
			return canonical, err
		}
	}

	if _, err := foldContractPrefixAlias(ctx, store, "contract-"+canonical, canonical); err != nil {
		return canonical, err
	}
	// The stored alias may not be the lowercase spelling Lookup uses.
	if isContractPixelAlias(id) && !strings.EqualFold(strings.TrimSpace(id), "contract-"+canonical) {
		if _, err := foldContractPrefixAlias(ctx, store, strings.TrimSpace(id), canonical); err != nil {
			return canonical, err
		}
	}
	return canonical, nil
}

// isContractPixelAlias reports whether id is the legacy contract-<64-hex> form.
// contract-001 and contract-osv1-local are not aliases.
func isContractPixelAlias(id string) bool {
	id = strings.TrimSpace(id)
	const legacy = "contract-"
	if len(id) <= len(legacy) || !strings.EqualFold(id[:len(legacy)], legacy) {
		return false
	}
	return identity.IsPixelHash(id[len(legacy):])
}

// FoldContractPrefixTwins moves every live contract-<64-hex> row onto the bare
// pixel hash. Safe to run on every startup. Non-hash contract ids are skipped.
// The returned count is the number of alias rows that were folded.
func FoldContractPrefixTwins(ctx context.Context, store Store) (int, error) {
	if store == nil {
		return 0, nil
	}
	list, err := store.ListContracts(core.ContractFilter{})
	if err != nil {
		return 0, err
	}
	n := 0
	seen := map[string]struct{}{}
	for _, c := range list {
		alias := strings.TrimSpace(c.ContractID)
		if !isContractPixelAlias(alias) {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		folded, err := foldContractPrefixAlias(ctx, store, alias, identity.CanonicalContractID(alias))
		if err != nil {
			return n, err
		}
		if folded {
			n++
		}
	}
	return n, nil
}

// foldContractPrefixAlias copies aliasID onto canonical when aliasID is the
// live contract-<hash> row, remaps its tasks, and supersedes the alias.
// A second call is a no-op.
func foldContractPrefixAlias(ctx context.Context, store Store, aliasID, canonical string) (bool, error) {
	aliasID = strings.TrimSpace(aliasID)
	canonical = strings.TrimSpace(canonical)
	if store == nil || aliasID == "" || !identity.IsPixelHash(canonical) || strings.EqualFold(aliasID, canonical) {
		return false, nil
	}
	if !isContractPixelAlias(aliasID) {
		return false, nil
	}
	alias, err := store.GetContract(aliasID)
	if err != nil || strings.TrimSpace(alias.ContractID) == "" {
		return false, nil
	}
	bare, bareErr := store.GetContract(canonical)
	haveBare := bareErr == nil && strings.TrimSpace(bare.ContractID) != ""

	tasks := tasksForContractIDs(store, canonical, alias.ContractID)
	move := false
	for i := range tasks {
		if tasks[i].ContractID != canonical {
			tasks[i].ContractID = canonical
			move = true
		}
	}

	// A superseded alias has already been folded. Only finish a leftover task move.
	if strings.EqualFold(strings.TrimSpace(alias.Status), "superseded") {
		if haveBare && move {
			if err := store.UpsertContractWithTasks(ctx, bare, tasks); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, nil
	}

	adopted := alias
	if haveBare {
		// The alias won publish, so it is often the live row (active) and the
		// bare wish is still the pending shell. Keep whichever status is further
		// along, but the stored primary key is always the bare hash.
		adopted = PreferContract(bare, alias)
		if !bare.CreatedAt.IsZero() {
			adopted.CreatedAt = bare.CreatedAt
		}
	}
	adopted.ContractID = canonical
	if err := store.UpsertContractWithTasks(ctx, adopted, tasks); err != nil {
		return false, err
	}
	if err := forceSupersedeContract(ctx, store, alias.ContractID); err != nil {
		return false, err
	}
	return true, nil
}

func tasksForContractIDs(store Store, ids ...string) []core.Task {
	seen := map[string]struct{}{}
	var out []core.Task
	for _, id := range ids {
		for _, t := range ListSiblingTasks(store, id) {
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
	return out
}

func forceSupersedeContract(ctx context.Context, store Store, id string) error {
	id = strings.TrimSpace(id)
	if store == nil || id == "" {
		return nil
	}
	inner := unwrapStore(store)
	if fs, ok := inner.(forceSuperseder); ok {
		return fs.ForceSupersedeContract(ctx, id)
	}
	c, err := store.GetContract(id)
	if err != nil || strings.TrimSpace(c.ContractID) == "" {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(c.Status), "superseded") {
		return nil
	}
	c.Status = "superseded"
	return store.UpsertContractWithTasks(ctx, c, nil)
}

func unwrapStore(store Store) Store {
	for store != nil {
		u, ok := store.(storeUnwrapper)
		if !ok {
			return store
		}
		next := u.Unwrap()
		if next == nil || next == store {
			return store
		}
		store = next
	}
	return store
}

// CollapsePixelHashTwins keeps one row per 64-hex VPH. Winner is the higher
// status rank; bare PK wins ties. wish-<hash> and contract-<64-hex> group with
// the bare hash. Other ids, including contract-001, are left untouched.
func CollapsePixelHashTwins(contracts []core.Contract) []core.Contract {
	if len(contracts) < 2 {
		return contracts
	}
	type pick struct {
		idx int
		c   core.Contract
	}
	best := map[string]pick{}
	drop := map[int]struct{}{}
	for i, c := range contracts {
		key := pixelHashGroupKey(c.ContractID)
		if key == "" {
			continue
		}
		if prev, ok := best[key]; ok {
			winner := PreferContract(prev.c, c)
			if winner.ContractID == c.ContractID {
				drop[prev.idx] = struct{}{}
				best[key] = pick{idx: i, c: c}
			} else {
				drop[i] = struct{}{}
			}
			continue
		}
		best[key] = pick{idx: i, c: c}
	}
	if len(drop) == 0 {
		return contracts
	}
	out := make([]core.Contract, 0, len(contracts)-len(drop))
	for i, c := range contracts {
		if _, skip := drop[i]; skip {
			continue
		}
		out = append(out, c)
	}
	return out
}

func pixelHashGroupKey(contractID string) string {
	n := identity.CanonicalContractID(contractID)
	if !identity.IsPixelHash(n) {
		return ""
	}
	return n
}

// PreferContract picks the live row of a wish-/bare pair.
func PreferContract(a, b core.Contract) core.Contract {
	ra, rb := contractStatusRank(a.Status), contractStatusRank(b.Status)
	if ra != rb {
		if ra > rb {
			return a
		}
		return b
	}
	aBare := isBarePixelHashID(a.ContractID)
	bBare := isBarePixelHashID(b.ContractID)
	if aBare != bBare {
		if aBare {
			return a
		}
		return b
	}
	if a.CreatedAt.After(b.CreatedAt) {
		return a
	}
	return b
}

func isBarePixelHashID(id string) bool {
	id = strings.TrimSpace(id)
	return identity.IsPixelHash(id)
}

func contractStatusRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "confirmed", "completed":
		return 50
	case "funded":
		return 40
	case "active", "published":
		return 30
	case "pending", "created":
		return 20
	case "superseded":
		return 0
	default:
		return 10
	}
}
