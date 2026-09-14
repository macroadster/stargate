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
// the leftover wish- twin. If neither exists, this is a no-op (caller inserts).
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
		return canonical, nil
	}
	if !haveWish {
		return canonical, nil
	}

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
	return canonical, nil
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
// status rank; bare PK wins ties. Non-pixel ids are left untouched.
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
	n := identity.Normalize(contractID)
	if !identity.IsPixelHash(n) {
		return ""
	}
	return strings.ToLower(n)
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
