package smart_contract

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"stargate-backend/services"
	scstore "stargate-backend/storage/smart_contract"
)

func TestHasIngestionPSBT(t *testing.T) {
	t.Run("funding txid", func(t *testing.T) {
		if !hasIngestionPSBT(map[string]interface{}{"funding_txid": "abc123"}) {
			t.Fatal("expected funding_txid to indicate PSBT")
		}
	})
	t.Run("funding txids slice", func(t *testing.T) {
		if !hasIngestionPSBT(map[string]interface{}{"funding_txids": []string{"abc123"}}) {
			t.Fatal("expected funding_txids to indicate PSBT")
		}
	})
	t.Run("missing funding metadata", func(t *testing.T) {
		if hasIngestionPSBT(map[string]interface{}{"address": "bc1qtest"}) {
			t.Fatal("expected missing funding metadata to mean no PSBT")
		}
	})
}

func TestProcessRecordDefersContractUntilPSBT(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	ingest := newTestIngestionService(t)
	ctx := context.Background()

	ingestionID := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	rec := services.IngestionRecord{
		ID:            ingestionID,
		Filename:      ingestionID,
		Method:        "alpha",
		MessageLength: 18,
		ImageBase64:   base64.StdEncoding.EncodeToString([]byte("fake-image-bytes")),
		Metadata: map[string]interface{}{
			"embedded_message":   "* Build starship\n* Launch",
			"visible_pixel_hash": ingestionID,
			"budget_sats":        int64(1000),
		},
		Status: "pending",
	}
	if err := ingest.Create(rec); err != nil {
		t.Fatalf("create ingestion: %v", err)
	}

	if err := processRecord(ctx, rec, ingest, store); err != nil {
		t.Fatalf("processRecord without PSBT: %v", err)
	}

	proposal, err := store.GetProposal(ctx, "wish-"+ingestionID)
	if err != nil {
		t.Fatalf("expected proposal to be created before PSBT: %v", err)
	}
	if proposal.ID == "" {
		t.Fatal("expected proposal id")
	}

	for _, contractID := range candidateContractIDs(ingestionID, ingestionID) {
		if _, err := store.GetContract(contractID); err == nil {
			t.Fatalf("contract %s should not exist before PSBT", contractID)
		}
	}

	updated, err := ingest.Get(ingestionID)
	if err != nil {
		t.Fatalf("get ingestion: %v", err)
	}
	if updated.Status != "pending" {
		t.Fatalf("expected ingestion to stay pending without PSBT, got %s", updated.Status)
	}

	if err := ingest.UpdateMetadata(ingestionID, map[string]interface{}{
		"funding_txid":  "fundingtxid123",
		"funding_txids": []string{"fundingtxid123"},
	}); err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	updated, err = ingest.Get(ingestionID)
	if err != nil {
		t.Fatalf("get updated ingestion: %v", err)
	}

	if err := processRecord(ctx, *updated, ingest, store); err != nil {
		t.Fatalf("processRecord with PSBT: %v", err)
	}

	contract, err := store.GetContract("wish-" + ingestionID)
	if err != nil {
		t.Fatalf("expected contract after PSBT: %v", err)
	}
	if contract.ContractID == "" {
		t.Fatal("expected contract id after PSBT")
	}

	final, err := ingest.Get(ingestionID)
	if err != nil {
		t.Fatalf("get final ingestion: %v", err)
	}
	if final.Status != "verified" {
		t.Fatalf("expected verified after contract load, got %s", final.Status)
	}
}

func newTestIngestionService(t *testing.T) *services.IngestionService {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ingestion.db")
	ingest, err := services.NewIngestionService(dbPath)
	if err != nil {
		t.Fatalf("init ingestion service: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(dbPath)
	})
	return ingest
}