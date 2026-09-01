package smart_contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

func emptySQLiteStore(t *testing.T) *scstore.SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "stage.db")
	store, err := scstore.NewSQLiteStore(dbPath, time.Hour, false)
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

// Unparseable / non-wish IPFS blobs stay stage-only. Pending starlight-wish-v1
// apply is covered by wish_ingest_test.go.
func TestIngestDownloadedFileDoesNotWriteSQL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UPLOADS_DIR", dir)

	blob := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01, 0x02, 0x03}
	src := filepath.Join(dir, "mirrored.png")
	if err := os.WriteFile(src, blob, 0644); err != nil {
		t.Fatal(err)
	}

	store := emptySQLiteStore(t)
	IngestDownloadedFile(context.Background(), src, "bafy-test", nil, store)

	contracts, err := store.ListContracts(smart_contract.ContractFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts) != 0 {
		t.Fatalf("expected no contracts from IPFS stage, got %d", len(contracts))
	}
	proposals, err := store.ListProposals(context.Background(), smart_contract.ProposalFilter{MaxResults: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(proposals) != 0 {
		t.Fatalf("expected no proposals from IPFS stage, got %d", len(proposals))
	}

	sum := sha256.Sum256(blob)
	hash := hex.EncodeToString(sum[:])
	found := false
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Base(p) == hash {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatalf("expected staged content-addressed file for %s", hash)
	}
}

func TestApplyIngestUpdateDiscardsUntrustedIPFSFunding(t *testing.T) {
	store := emptySQLiteStore(t)
	ctx := context.Background()
	vph := strings.Repeat("ab", 32)
	if err := store.CreateProposal(ctx, smart_contract.Proposal{
		ID:               "p-fund",
		Title:            "local",
		Status:           "pending",
		VisiblePixelHash: vph,
		CreatedAt:        time.Now(),
		Metadata: map[string]interface{}{
			"visible_pixel_hash": vph,
		},
	}); err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	applied, err := applyIngestUpdate(ctx, nil, store, &ingestUpdateAnnouncement{
		IngestionID: "p-fund",
		FundingTxID: "deadbeef",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("expected discard to report applied so queue drains")
	}
	p, err := store.GetProposal(ctx, "p-fund")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Metadata["funding_txid"]; ok {
		t.Fatal("funding_txid must not be written from IPFS update")
	}
}
