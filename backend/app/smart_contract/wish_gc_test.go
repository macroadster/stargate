package smart_contract

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	"stargate-backend/storage/ipfs"
	scstore "stargate-backend/storage/smart_contract"
)

func TestWishHasEngagement(t *testing.T) {
	ctx := context.Background()
	hash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	t.Run("none", func(t *testing.T) {
		if wishHasEngagement(ctx, hash, nil, nil) {
			t.Fatal("empty inputs should not count as engagement")
		}
	})

	t.Run("auto pending proposal is not engagement", func(t *testing.T) {
		store := scstore.NewMemoryStore(0)
		if err := store.CreateProposal(ctx, smart_contract.Proposal{
			ID:               hash,
			Title:            "Wish",
			VisiblePixelHash: hash,
			Status:           "pending",
			CreatedAt:        time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertContractWithTasks(ctx, smart_contract.Contract{
			ContractID: "wish-" + hash,
			Title:      "Wish",
			Status:     "pending",
		}, nil); err != nil {
			t.Fatal(err)
		}
		if wishHasEngagement(ctx, hash, nil, store) {
			t.Fatal("auto-created pending proposal/contract is not engagement")
		}
	})

	t.Run("psbt is engagement", func(t *testing.T) {
		ingest := newTestIngestionService(t)
		if err := ingest.Create(services.IngestionRecord{
			ID:          hash,
			Filename:    hash,
			Method:      "alpha",
			ImageBase64: base64.StdEncoding.EncodeToString([]byte("img")),
			Metadata:    map[string]interface{}{"funding_txid": "abc"},
			Status:      "pending",
		}); err != nil {
			t.Fatal(err)
		}
		if !wishHasEngagement(ctx, hash, ingest, nil) {
			t.Fatal("funding_txid should count as engagement")
		}
	})

	t.Run("approved proposal is engagement", func(t *testing.T) {
		store := scstore.NewMemoryStore(0)
		if err := store.CreateProposal(ctx, smart_contract.Proposal{
			ID:               hash,
			Title:            "Wish",
			VisiblePixelHash: hash,
			Status:           smart_contract.ProposalStatusApproved,
			CreatedAt:        time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
		if !wishHasEngagement(ctx, hash, nil, store) {
			t.Fatal("approved proposal should count as engagement")
		}
	})

	t.Run("second proposal is engagement", func(t *testing.T) {
		store := scstore.NewMemoryStore(0)
		if err := store.CreateProposal(ctx, smart_contract.Proposal{
			ID:               hash,
			Title:            "Wish",
			VisiblePixelHash: hash,
			Status:           "pending",
			CreatedAt:        time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.CreateProposal(ctx, smart_contract.Proposal{
			ID:               "someone-else-" + hash[:8],
			Title:            "Alt",
			VisiblePixelHash: hash,
			Status:           "pending",
			CreatedAt:        time.Now(),
			Metadata:         map[string]any{"visible_pixel_hash": hash},
		}); err != nil {
			t.Fatal(err)
		}
		if !wishHasEngagement(ctx, hash, nil, store) {
			t.Fatal("additional proposal should count as engagement")
		}
	})
}

func TestExpireUnengagedWishes(t *testing.T) {
	t.Setenv("IPFS_ENABLED", "false")
	uploads := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploads)
	ipfs.ResetWishIndexForTest(filepath.Join(t.TempDir(), "ipfs_wishes.json"))

	ctx := context.Background()
	store := scstore.NewMemoryStore(0)
	ingest := newTestIngestionService(t)

	oldHash := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	freshHash := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	engagedHash := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

	mustWrite := func(hash string) {
		t.Helper()
		p := filepath.Join(uploads, hash)
		if err := os.WriteFile(p, []byte("img-"+hash[:8]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustCreate := func(hash string, created time.Time, meta map[string]interface{}) {
		t.Helper()
		if err := ingest.Create(services.IngestionRecord{
			ID:          hash,
			Filename:    hash,
			Method:      "alpha",
			ImageBase64: base64.StdEncoding.EncodeToString([]byte("img")),
			Metadata:    meta,
			Status:      "pending",
			CreatedAt:   created,
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.CreateProposal(ctx, smart_contract.Proposal{
			ID:               hash,
			Title:            "Wish",
			VisiblePixelHash: hash,
			Status:           "pending",
			CreatedAt:        created,
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertContractWithTasks(ctx, smart_contract.Contract{
			ContractID: "wish-" + hash,
			Title:      "Wish",
			Status:     "pending",
		}, nil); err != nil {
			t.Fatal(err)
		}
		ipfs.TrackWish(hash, "", created)
		mustWrite(hash)
	}

	now := time.Now()
	mustCreate(oldHash, now.Add(-8*24*time.Hour), nil)
	mustCreate(freshHash, now.Add(-2*24*time.Hour), nil)
	mustCreate(engagedHash, now.Add(-8*24*time.Hour), map[string]interface{}{"funding_txid": "txid1"})

	n, err := expireUnengagedWishes(ctx, ingest, store, nil, now, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if n != 1 {
		t.Fatalf("expired %d want 1", n)
	}

	if _, err := ingest.Get(oldHash); err == nil {
		t.Fatal("old unengaged wish should be deleted from ingestions")
	}
	if _, err := store.GetProposal(ctx, oldHash); err == nil {
		t.Fatal("old unengaged wish proposal should be deleted")
	}
	if _, err := os.Stat(filepath.Join(uploads, oldHash)); err == nil {
		t.Fatal("old unengaged wish file should be deleted")
	}
	if ipfs.IsTrackedWish(oldHash) {
		t.Fatal("old unengaged wish should be untracked")
	}

	if _, err := ingest.Get(freshHash); err != nil {
		t.Fatal("fresh wish should remain")
	}
	if _, err := ingest.Get(engagedHash); err != nil {
		t.Fatal("engaged wish should remain")
	}
	if ipfs.IsTrackedWish(engagedHash) {
		t.Fatal("engaged wish should be promoted off the ephemeral index")
	}
}
