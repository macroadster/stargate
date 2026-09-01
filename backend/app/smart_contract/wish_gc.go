package smart_contract

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core/identity"
	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	"stargate-backend/storage/datadir"
	"stargate-backend/storage/ipfs"
)

const (
	defaultWishTTL      = 7 * 24 * time.Hour
	defaultWishGCPeriod = time.Hour
)

// StartWishGC periodically unpins and deletes inscribed wishes that have no
// engagement after the configured TTL (default 7 days).
func StartWishGC(ctx context.Context, ingest *services.IngestionService, store Store, unpinPath func(context.Context, string) error) {
	if ingest == nil && store == nil {
		return
	}
	ttl := wishTTLFromEnv()
	interval := wishGCIntervalFromEnv()
	log.Printf("wish gc: started (ttl=%s interval=%s)", ttl, interval)

	run := func() {
		n, err := expireUnengagedWishes(ctx, ingest, store, unpinPath, time.Now(), ttl)
		if err != nil {
			log.Printf("wish gc: sweep error: %v", err)
		} else if n > 0 {
			log.Printf("wish gc: expired %d unengaged wish(es)", n)
		}
	}

	go func() {
		// Small delay so identity / mirror / ingest have a chance to start.
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			run()
		}

		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				run()
			}
		}
	}()
}

func wishTTLFromEnv() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("IPFS_WISH_TTL")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	if raw := strings.TrimSpace(os.Getenv("IPFS_WISH_TTL_HOURS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return time.Duration(n) * time.Hour
		}
	}
	return defaultWishTTL
}

func wishGCIntervalFromEnv() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("IPFS_WISH_GC_INTERVAL_SEC")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return defaultWishGCPeriod
}

func expireUnengagedWishes(ctx context.Context, ingest *services.IngestionService, store Store, unpinPath func(context.Context, string) error, now time.Time, ttl time.Duration) (int, error) {
	if ttl <= 0 {
		ttl = defaultWishTTL
	}
	cutoff := now.Add(-ttl)
	expired := 0
	seen := make(map[string]struct{})

	consider := func(hash string, created time.Time, cid string) {
		hash = strings.TrimSpace(strings.TrimPrefix(hash, "wish-"))
		if hash == "" {
			return
		}
		if _, ok := seen[hash]; ok {
			return
		}
		seen[hash] = struct{}{}
		if !created.IsZero() && created.After(cutoff) {
			return
		}
		if wishHasEngagement(ctx, hash, ingest, store) {
			ipfs.UntrackWish(hash)
			return
		}
		if err := expireWish(ctx, hash, cid, ingest, store, unpinPath); err != nil {
			log.Printf("wish gc: expire %s: %v", hash, err)
			return
		}
		expired++
	}

	for _, rec := range ipfs.ListTrackedWishes() {
		created := time.Time{}
		if rec.CreatedAt > 0 {
			created = time.Unix(rec.CreatedAt, 0)
		}
		consider(rec.Hash, created, rec.CID)
	}

	if ingest != nil {
		pending, err := ingest.ListRecent("pending", 5000)
		if err != nil {
			return expired, err
		}
		for _, rec := range pending {
			cid := ""
			if rec.Metadata != nil {
				if v, ok := rec.Metadata["ipfs_image_cid"].(string); ok {
					cid = strings.TrimSpace(v)
				}
			}
			consider(rec.ID, rec.CreatedAt, cid)
		}
	}
	return expired, nil
}

// wishHasEngagement reports whether the wish has activity beyond the initial
// inscription (auto-created pending proposal/contract do not count).
func wishHasEngagement(ctx context.Context, hash string, ingest *services.IngestionService, store Store) bool {
	hash = strings.TrimSpace(strings.TrimPrefix(hash, "wish-"))
	if hash == "" {
		return false
	}

	if ingest != nil {
		rec, err := ingest.Get(hash)
		if err == nil && rec != nil {
			if hasIngestionPSBT(rec.Metadata) {
				return true
			}
			st := strings.ToLower(strings.TrimSpace(rec.Status))
			if st != "" && st != "pending" && st != "ignored" && st != "invalid" {
				return true
			}
		}
	}

	if store == nil {
		return false
	}

	wishID := identity.ToWishID(hash)
	if c, err := store.GetContract(wishID); err == nil {
		st := strings.ToLower(strings.TrimSpace(c.Status))
		if st != "" && st != "pending" {
			return true
		}
	}

	props, err := store.ListProposals(ctx, smart_contract.ProposalFilter{ContractID: wishID, Limit: 50})
	if err == nil {
		extra := 0
		for _, p := range props {
			st := strings.ToLower(strings.TrimSpace(p.Status))
			if st != "" && st != smart_contract.ProposalStatusPending {
				return true
			}
			pid := strings.TrimPrefix(strings.TrimSpace(p.ID), "wish-")
			if pid != hash && strings.TrimSpace(p.ID) != hash {
				extra++
			}
			for _, t := range p.Tasks {
				if taskShowsEngagement(t) {
					return true
				}
			}
		}
		if extra > 0 {
			return true
		}
	}

	tasks, err := store.ListTasks(smart_contract.TaskFilter{ContractID: wishID, Limit: 50})
	if err == nil {
		for _, t := range tasks {
			if taskShowsEngagement(t) {
				return true
			}
		}
	}

	subs, err := store.ListSubmissions(ctx, smart_contract.SubmissionFilter{ContractID: wishID, Limit: 1})
	if err == nil && len(subs) > 0 {
		return true
	}

	return false
}

func taskShowsEngagement(t smart_contract.Task) bool {
	if strings.TrimSpace(t.ClaimedBy) != "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(t.Status)) {
	case smart_contract.TaskStatusClaimed, smart_contract.TaskStatusSubmitted,
		smart_contract.TaskStatusApproved, smart_contract.TaskStatusPublished,
		smart_contract.TaskStatusCompleted:
		return true
	}
	return false
}

func expireWish(ctx context.Context, hash, cid string, ingest *services.IngestionService, store Store, unpinPath func(context.Context, string) error) error {
	if tracked, ok := ipfs.LookupWish(hash); ok && cid == "" {
		cid = tracked.CID
	}
	if ingest != nil && cid == "" {
		if rec, err := ingest.Get(hash); err == nil && rec != nil && rec.Metadata != nil {
			if v, ok := rec.Metadata["ipfs_image_cid"].(string); ok {
				cid = strings.TrimSpace(v)
			}
		}
	}

	if cid != "" {
		if err := ipfs.NewClientFromEnv().Unpin(ctx, cid); err != nil {
			log.Printf("wish gc: unpin cid %s: %v", cid, err)
		}
	}

	uploadsDir := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
	if uploadsDir != "" {
		path := datadir.PartResolve(uploadsDir, hash)
		if _, err := os.Stat(path); err == nil {
			if err := os.Remove(path); err != nil {
				log.Printf("wish gc: remove %s: %v", path, err)
			}
		}
		if unpinPath != nil {
			if err := unpinPath(ctx, hash); err != nil {
				log.Printf("wish gc: mirror unpin %s: %v", hash, err)
			}
		}
	}

	if ingest != nil {
		if err := ingest.Delete(ctx, hash); err != nil {
			log.Printf("wish gc: delete ingestion %s: %v", hash, err)
		}
	}
	if store != nil {
		if err := store.DeleteWish(ctx, hash); err != nil {
			log.Printf("wish gc: delete wish %s: %v", hash, err)
		}
	}
	ipfs.UntrackWish(hash)
	log.Printf("wish gc: deleted unengaged wish %s", hash)
	return nil
}
