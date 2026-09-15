package smart_contract

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	core "stargate-backend/core/smart_contract"
	"stargate-backend/stego"
	scstore "stargate-backend/storage/smart_contract"
)

func TestUpsertContractFromStegoPayload(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	server := &Server{store: store}
	ctx := context.Background()

	payload := stego.Payload{
		SchemaVersion: 1,
		Proposal: stego.PayloadProposal{
			ID:               "proposal-stego-2",
			Title:            "Stego Contract",
			DescriptionMD:    "Proof of work",
			BudgetSats:       2000,
			VisiblePixelHash: strings.Repeat("b", 64),
			CreatedAt:        time.Now().Unix(),
		},
		Tasks: []stego.PayloadTask{
			{
				TaskID:           "task-stego-2",
				Title:            "Deliver",
				Description:      "Do the work",
				BudgetSats:       2000,
				Skills:           []string{"manual-review"},
				ContractorWallet: "tb1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh",
			},
		},
	}
	manifest := stego.Manifest{
		SchemaVersion:    1,
		ProposalID:       payload.Proposal.ID,
		VisiblePixelHash: payload.Proposal.VisiblePixelHash,
		PayloadCID:       "payloadcid456",
		CreatedAt:        time.Now().Unix(),
		Issuer:           "tester",
	}
	contractID := "contract-stego-2"

	if err := server.UpsertContractFromStegoPayload(ctx, contractID, "stegocid456", "stegohash456", manifest, payload); err != nil {
		t.Fatalf("UpsertContractFromStegoPayload error: %v", err)
	}

	contract, err := store.GetContract(contractID)
	if err != nil {
		t.Fatalf("contract not created: %v", err)
	}
	if contract.Status != "active" {
		t.Fatalf("expected contract status active, got %s", contract.Status)
	}

	proposal, err := store.GetProposal(ctx, payload.Proposal.ID)
	if err != nil {
		t.Fatalf("proposal not created: %v", err)
	}
	if proposal.Status != "pending" {
		t.Fatalf("expected pending proposal from stego (no auto-approve), got %s", proposal.Status)
	}
	if proposal.Metadata["stego_image_cid"] != "stegocid456" {
		t.Fatalf("stego metadata missing from proposal")
	}

	tasks, err := store.ListTasks(core.TaskFilter{ContractID: contractID})
	if err != nil {
		t.Fatalf("list tasks failed: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatalf("expected tasks to be stored for contract")
	}
}

// TestUpsertStegoSetsProductPixelHash verifies that the reconciler always sets
// ProductPixelHash to the stegoHash (product image) and CommitmentPixelHash to
// the visibleHash (wish image) for the two-phase recommitment sweep.
func TestUpsertStegoSetsProductPixelHash(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	server := &Server{store: store}
	ctx := context.Background()

	visibleHash := strings.Repeat("a", 64) // original wish image hash
	stegoHash := strings.Repeat("c", 64)   // delivered product image hash

	payload := stego.Payload{
		SchemaVersion: 1,
		Proposal: stego.PayloadProposal{
			ID:               "proposal-product-hash",
			Title:            "Product Hash Test",
			BudgetSats:       1000,
			VisiblePixelHash: visibleHash,
			CreatedAt:        time.Now().Unix(),
		},
		Tasks: []stego.PayloadTask{
			{
				TaskID:           "task-product-1",
				Title:            "Task 1",
				BudgetSats:       1000,
				ContractorWallet: "tb1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh",
			},
		},
	}
	manifest := stego.Manifest{
		SchemaVersion:    1,
		ProposalID:       payload.Proposal.ID,
		VisiblePixelHash: visibleHash,
		PayloadCID:       "bafypayload",
		CreatedAt:        time.Now().Unix(),
		Issuer:           "tester",
	}

	err := server.UpsertContractFromStegoPayload(ctx, "contract-product", "stegocid", stegoHash, manifest, payload)
	if err != nil {
		t.Fatalf("UpsertContractFromStegoPayload: %v", err)
	}

	tasks, err := store.ListTasks(core.TaskFilter{ContractID: "contract-product"})
	if err != nil || len(tasks) == 0 {
		t.Fatalf("no tasks created: %v", err)
	}

	proof := tasks[0].MerkleProof
	if proof == nil {
		t.Fatalf("expected MerkleProof to be set")
	}

	// CommitmentPixelHash should be the visibleHash (wish image) for the on-chain hashlock
	if proof.CommitmentPixelHash != visibleHash {
		t.Errorf("CommitmentPixelHash = %q, want visibleHash %q", proof.CommitmentPixelHash, visibleHash)
	}
	if proof.CommitmentSource != "wish" {
		t.Errorf("CommitmentSource = %q, want \"wish\"", proof.CommitmentSource)
	}
	// ProductPixelHash should be the stegoHash (product image) for two-phase recommitment
	if proof.ProductPixelHash != stegoHash {
		t.Errorf("ProductPixelHash = %q, want stegoHash %q", proof.ProductPixelHash, stegoHash)
	}
	// VisiblePixelHash should still reflect the original wish image
	if proof.VisiblePixelHash != visibleHash {
		t.Errorf("VisiblePixelHash = %q, want original %q", proof.VisiblePixelHash, visibleHash)
	}
}

// TestUpsertStegoPreservesWishHashWhenDonationExists verifies that when a task
// already has a funded donation commitment (CommitmentSats > 0), the reconciler
// preserves the original wish image hash for the hashlock.
func TestUpsertStegoPreservesWishHashWhenDonationExists(t *testing.T) {
	store := scstore.NewMemoryStore(0)
	server := &Server{store: store}
	ctx := context.Background()

	visibleHash := strings.Repeat("a", 64)
	stegoHash := strings.Repeat("c", 64)
	contractID := "contract-donation-exists"

	// Pre-create task with a funded donation commitment
	store.CreateProposal(ctx, core.Proposal{
		ID:               "proposal-donation",
		Title:            "Donation Test",
		VisiblePixelHash: visibleHash,
		BudgetSats:       1000,
		Status:           "approved",
		CreatedAt:        time.Now(),
	})
	store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: contractID,
		Title:      "Donation Test",
		Status:     "active",
	}, []core.Task{
		{
			TaskID:           "task-donation-1",
			ContractID:       contractID,
			GoalID:           contractID,
			Title:            "Funded Task",
			Status:           "available",
			ContractorWallet: "tb1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh",
			MerkleProof: &core.MerkleProof{
				CommitmentSats:      1000,
				VisiblePixelHash:    visibleHash,
				CommitmentPixelHash: visibleHash,
				CommitmentSource:    "wish",
			},
		},
	})

	payload := stego.Payload{
		SchemaVersion: 1,
		Proposal: stego.PayloadProposal{
			ID:               "proposal-donation",
			Title:            "Donation Test",
			BudgetSats:       1000,
			VisiblePixelHash: visibleHash,
			CreatedAt:        time.Now().Unix(),
		},
		Tasks: []stego.PayloadTask{
			{
				TaskID:           "task-donation-1",
				Title:            "Funded Task",
				BudgetSats:       1000,
				ContractorWallet: "tb1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh",
			},
		},
	}
	manifest := stego.Manifest{
		SchemaVersion:    1,
		ProposalID:       "proposal-donation",
		VisiblePixelHash: visibleHash,
		PayloadCID:       "bafypayload2",
		CreatedAt:        time.Now().Unix(),
		Issuer:           "tester",
	}

	err := server.UpsertContractFromStegoPayload(ctx, contractID, "stegocid2", stegoHash, manifest, payload)
	if err != nil {
		t.Fatalf("UpsertContractFromStegoPayload: %v", err)
	}

	tasks, _ := store.ListTasks(core.TaskFilter{ContractID: contractID})
	if len(tasks) == 0 {
		t.Fatalf("no tasks found")
	}

	proof := tasks[0].MerkleProof
	if proof == nil {
		t.Fatalf("expected MerkleProof")
	}

	// With funded donation, should keep the wish hash for the on-chain hashlock
	if proof.CommitmentSource != "wish" {
		t.Errorf("CommitmentSource = %q, want \"wish\" (donation was funded)", proof.CommitmentSource)
	}
	if proof.CommitmentPixelHash != visibleHash {
		t.Errorf("CommitmentPixelHash = %q, want visibleHash %q", proof.CommitmentPixelHash, visibleHash)
	}
	// ProductPixelHash should always be set to stegoHash for two-phase recommitment
	if proof.ProductPixelHash != stegoHash {
		t.Errorf("ProductPixelHash = %q, want stegoHash %q", proof.ProductPixelHash, stegoHash)
	}
	// CommitmentSats should be preserved
	if proof.CommitmentSats != 1000 {
		t.Errorf("CommitmentSats = %d, want 1000 (should be preserved)", proof.CommitmentSats)
	}
}

type tarFile struct {
	name string
	body []byte
}

func gzipTar(t *testing.T, files ...tarFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		hdr := &tar.Header{Name: f.name, Mode: 0644, Size: int64(len(f.body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractSandboxTarball_WritesRegularFiles(t *testing.T) {
	dir := t.TempDir()
	s := &Server{}
	if err := s.extractSandboxTarball("wish-ok", gzipTar(t,
		tarFile{"index.html", []byte("<html>ok</html>")},
		tarFile{"notes.txt", []byte("hello")},
	), dir); err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, name := range []string{"index.html", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestExtractSandboxTarball_RejectsTraversal(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "results")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	if err := s.extractSandboxTarball("wish-trav", gzipTar(t,
		tarFile{"../escape.txt", []byte("nope")},
		tarFile{"ok.txt", []byte("yes")},
	), dir); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "escape.txt")); err == nil {
		t.Fatal("traversal wrote outside results dir")
	}
	if _, err := os.Stat(filepath.Join(dir, "ok.txt")); err != nil {
		t.Fatalf("safe file missing: %v", err)
	}
}

func countRegularFiles(t *testing.T, dir string) int {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range ents {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

func TestExtractSandboxTarball_CapsEntryCount(t *testing.T) {
	old := sandboxMaxEntries
	sandboxMaxEntries = 2
	t.Cleanup(func() { sandboxMaxEntries = old })

	dir := t.TempDir()
	s := &Server{}
	err := s.extractSandboxTarball("wish-cap", gzipTar(t,
		tarFile{"a.txt", []byte("a")},
		tarFile{"b.txt", []byte("b")},
		tarFile{"c.txt", []byte("c")},
	), dir)
	if err == nil {
		t.Fatal("entry cap should fail the extract")
	}
	if n := countRegularFiles(t, dir); n > 2 {
		t.Fatalf("entry cap ignored, wrote %d files", n)
	}
}

func TestExtractSandboxTarball_EntryCapCountsSkippedHeaders(t *testing.T) {
	old := sandboxMaxEntries
	sandboxMaxEntries = 2
	t.Cleanup(func() { sandboxMaxEntries = old })

	parent := t.TempDir()
	dir := filepath.Join(parent, "results")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	// Three skipped headers (traversal) then a valid file. The valid entry
	// must never be processed: the cap is on headers, not successful writes.
	err := s.extractSandboxTarball("wish-skipcap", gzipTar(t,
		tarFile{"../a.txt", []byte("nope")},
		tarFile{"../b.txt", []byte("nope")},
		tarFile{"../c.txt", []byte("nope")},
		tarFile{"ok.txt", []byte("yes")},
	), dir)
	if err == nil {
		t.Fatal("header cap should fail the extract")
	}
	if _, err := os.Stat(filepath.Join(dir, "ok.txt")); err == nil {
		t.Fatal("valid entry after >cap skipped headers was processed")
	}
	if _, err := os.Stat(filepath.Join(parent, "a.txt")); err == nil {
		t.Fatal("traversal wrote outside results dir")
	}
}

func TestExtractSandboxTarball_CapsFileBytes(t *testing.T) {
	old := sandboxMaxFileBytes
	sandboxMaxFileBytes = 8
	t.Cleanup(func() { sandboxMaxFileBytes = old })

	dir := t.TempDir()
	s := &Server{}
	if err := s.extractSandboxTarball("wish-filecap", gzipTar(t,
		tarFile{"ok.txt", []byte("12345678")},
		tarFile{"big.txt", []byte("123456789")},
		tarFile{"also.txt", []byte("ok")},
	), dir); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ok.txt")); err != nil {
		t.Fatalf("file at the cap should be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "also.txt")); err != nil {
		t.Fatalf("small sibling should be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "big.txt")); err == nil {
		t.Fatal("oversized entry was written")
	}
}

func TestExtractSandboxTarball_CapsTotalBytes(t *testing.T) {
	old := sandboxMaxTotalBytes
	sandboxMaxTotalBytes = 10
	t.Cleanup(func() { sandboxMaxTotalBytes = old })

	dir := t.TempDir()
	s := &Server{}
	err := s.extractSandboxTarball("wish-total", gzipTar(t,
		tarFile{"a.txt", []byte("12345678")},
		tarFile{"b.txt", []byte("12345678")},
	), dir)
	if err == nil {
		t.Fatal("total-size cap should fail the extract")
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatalf("first file under the total cap should be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err == nil {
		t.Fatal("second file should have tripped the total-size cap")
	}
}
