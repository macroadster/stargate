package smart_contract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
	"stargate-backend/stego"
	"stargate-backend/storage/ipfs"
)

func wishCoverPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 90, B: 140, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode cover: %v", err)
	}
	return buf.Bytes()
}

func inscribeWishPNG(t *testing.T, message string) (blob []byte, hash string) {
	t.Helper()
	res, err := stego.Inscribe(wishCoverPNG(t, 256, 256), message, "alpha")
	if err != nil {
		t.Fatalf("inscribe: %v", err)
	}
	return res.ImageBytes, res.ImageSHA256
}

func isolateWishIndex(t *testing.T) {
	t.Helper()
	ipfs.ResetWishIndexForTest(filepath.Join(t.TempDir(), "ipfs_wishes.json"))
	t.Cleanup(func() { ipfs.ResetWishIndexForTest("") })
}

func TestParsePendingWishPayload_WishV1(t *testing.T) {
	raw := []byte(`{"type":"starlight-wish-v1","title":"Commission StarTrek5 movie","body":"Hire coworker to make StarTrek5. Compensation is 1000 sats.","parties":["xai:abc","mszegedi@teachx.ai"],"created_by":"xai:abc","budget_sats":1000}`)
	got, ok := parsePendingWishPayload(raw, strings.Repeat("ab", 32))
	if !ok {
		t.Fatal("expected wish-v1 parse")
	}
	if got.Schema != starlightWishV1Type {
		t.Fatalf("schema=%q", got.Schema)
	}
	if got.Title != "Commission StarTrek5 movie" {
		t.Fatalf("title=%q", got.Title)
	}
	if got.Budget != 1000 {
		t.Fatalf("budget=%d", got.Budget)
	}
	if got.CreatedBy != "xai:abc" || len(got.Parties) != 2 {
		t.Fatalf("created_by=%q parties=%v", got.CreatedBy, got.Parties)
	}
}

func TestParsePendingWishPayload_RejectsV2Product(t *testing.T) {
	payload := stego.Payload{
		SchemaVersion:    2,
		ProposalID:       "p-1",
		VisiblePixelHash: strings.Repeat("cd", 32),
		Issuer:           "attacker",
		CreatedAt:        time.Now().Unix(),
		Proposal: stego.PayloadProposal{
			ID:               "p-1",
			Title:            "funded looking payload",
			BudgetSats:       999,
			VisiblePixelHash: strings.Repeat("cd", 32),
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := parsePendingWishPayload(raw, strings.Repeat("cd", 32)); ok {
		t.Fatal("v2 product stego must not parse as a pending wish")
	}
}

func TestParsePendingWishPayload_PlainRequiresTracked(t *testing.T) {
	isolateWishIndex(t)
	hash := strings.Repeat("11", 32)
	if _, ok := parsePendingWishPayload([]byte("# Build a starship\n\nPlease."), hash); ok {
		t.Fatal("untracked plain text must not ingest")
	}
	ipfs.TrackWish(hash, "bafy-test", time.Now())
	got, ok := parsePendingWishPayload([]byte("# Build a starship\n\nPlease.\n\n[stargate-ts:123]"), hash)
	if !ok {
		t.Fatal("tracked plain text should ingest")
	}
	if got.Schema != "plain" || got.Title != "Build a starship" {
		t.Fatalf("got %+v", got)
	}
	if strings.Contains(got.Body, "stargate-ts") {
		t.Fatalf("timestamp should be stripped: %q", got.Body)
	}
}

func TestIngestDownloadedFile_WishV1CreatesPendingContract(t *testing.T) {
	isolateWishIndex(t)
	dir := t.TempDir()
	t.Setenv("UPLOADS_DIR", dir)
	store := emptySQLiteStore(t)
	ingest := newTestIngestionService(t)

	msg := `{"type":"starlight-wish-v1","title":"Commission StarTrek5 movie","body":"Make StarTrek5. 1000 sats.","budget_sats":1000,"created_by":"xai:abc"}`
	blob, hash := inscribeWishPNG(t, msg)
	src := filepath.Join(dir, "remote.png")
	if err := os.WriteFile(src, blob, 0644); err != nil {
		t.Fatal(err)
	}

	IngestDownloadedFile(context.Background(), src, "bafy-wish-cid", ingest, store)

	c, err := store.GetContract("wish-" + hash)
	if err != nil {
		t.Fatalf("expected pending wish contract: %v", err)
	}
	if c.Status != "pending" {
		t.Fatalf("status=%q want pending", c.Status)
	}
	if c.Title != "Commission StarTrek5 movie" {
		t.Fatalf("title=%q", c.Title)
	}
	if c.TotalBudgetSats != 1000 {
		t.Fatalf("budget=%d", c.TotalBudgetSats)
	}
	if c.StegoImageURL != "/uploads/"+hash {
		t.Fatalf("stego url=%q", c.StegoImageURL)
	}

	p, err := store.GetProposal(context.Background(), hash)
	if err != nil {
		t.Fatalf("expected proposal: %v", err)
	}
	if p.Status != smart_contract.ProposalStatusPending {
		t.Fatalf("proposal status=%q", p.Status)
	}

	rec, err := ingest.Get(hash)
	if err != nil {
		t.Fatalf("expected ingestion: %v", err)
	}
	if rec.Status != "pending" {
		t.Fatalf("ingestion status=%q", rec.Status)
	}
	if rec.Metadata["wish_schema"] != starlightWishV1Type {
		t.Fatalf("wish_schema=%v", rec.Metadata["wish_schema"])
	}
	if rec.Metadata["ipfs_image_cid"] != "bafy-wish-cid" {
		t.Fatalf("cid=%v", rec.Metadata["ipfs_image_cid"])
	}
	if _, ok := rec.Metadata["funding_txid"]; ok {
		t.Fatal("funding_txid must not be written from wish ingest")
	}

	sum := sha256.Sum256(blob)
	if hex.EncodeToString(sum[:]) != hash {
		t.Fatal("hash mismatch")
	}
	if _, err := os.Stat(filepath.Join(dir, hash[:2], hash[2:4], hash[4:6], hash)); err != nil {
		// partitioned path may vary; walk
		found := false
		_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && filepath.Base(p) == hash {
				found = true
			}
			return nil
		})
		if !found {
			t.Fatal("expected staged content-addressed file")
		}
	}
}

func TestIngestDownloadedFile_WishV1Idempotent(t *testing.T) {
	isolateWishIndex(t)
	dir := t.TempDir()
	t.Setenv("UPLOADS_DIR", dir)
	store := emptySQLiteStore(t)
	ingest := newTestIngestionService(t)

	blob, hash := inscribeWishPNG(t, `{"type":"starlight-wish-v1","title":"Once","body":"Only once"}`)
	src := filepath.Join(dir, "w.png")
	if err := os.WriteFile(src, blob, 0644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	IngestDownloadedFile(ctx, src, "cid-1", ingest, store)
	IngestDownloadedFile(ctx, src, "cid-2", ingest, store)

	list, err := store.ListContracts(smart_contract.ContractFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 contract, got %d", len(list))
	}
	if list[0].ContractID != "wish-"+hash {
		t.Fatalf("id=%s", list[0].ContractID)
	}
}

func TestIngestDownloadedFile_V2ProductStegoNoSQL(t *testing.T) {
	isolateWishIndex(t)
	dir := t.TempDir()
	t.Setenv("UPLOADS_DIR", dir)
	store := emptySQLiteStore(t)
	ingest := newTestIngestionService(t)

	vph := strings.Repeat("ab", 32)
	payload := stego.Payload{
		SchemaVersion:    2,
		ProposalID:       "p-v2",
		VisiblePixelHash: vph,
		Issuer:           "peer",
		CreatedAt:        time.Now().Unix(),
		Proposal: stego.PayloadProposal{
			ID:               "p-v2",
			Title:            "Should not become open contract",
			BudgetSats:       5000,
			VisiblePixelHash: vph,
		},
	}
	raw, _ := json.Marshal(payload)
	blob, _ := inscribeWishPNG(t, string(raw))
	src := filepath.Join(dir, "v2.png")
	if err := os.WriteFile(src, blob, 0644); err != nil {
		t.Fatal(err)
	}
	IngestDownloadedFile(context.Background(), src, "bafy-v2", ingest, store)

	contracts, err := store.ListContracts(smart_contract.ContractFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts) != 0 {
		t.Fatalf("v2 product must not create contracts, got %d", len(contracts))
	}
}

func TestIngestDownloadedFile_DoesNotOverwriteProtectedContract(t *testing.T) {
	isolateWishIndex(t)
	dir := t.TempDir()
	t.Setenv("UPLOADS_DIR", dir)
	store := emptySQLiteStore(t)

	blob, hash := inscribeWishPNG(t, `{"type":"starlight-wish-v1","title":"Attacker title","body":"nope","budget_sats":1}`)
	if err := store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID:      "wish-" + hash,
		Title:           "Legitimate funded wish",
		TotalBudgetSats: 50_000,
		Status:          "active",
	}, nil); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "atk.png")
	if err := os.WriteFile(src, blob, 0644); err != nil {
		t.Fatal(err)
	}
	IngestDownloadedFile(context.Background(), src, "bafy-atk", newTestIngestionService(t), store)

	c, err := store.GetContract("wish-" + hash)
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "Legitimate funded wish" || c.Status != "active" || c.TotalBudgetSats != 50_000 {
		t.Fatalf("protected contract mutated: %+v", c)
	}
}

func TestIngestDownloadedFile_PlainTextTrackedCreatesContract(t *testing.T) {
	isolateWishIndex(t)
	dir := t.TempDir()
	t.Setenv("UPLOADS_DIR", dir)
	store := emptySQLiteStore(t)
	ingest := newTestIngestionService(t)

	blob, hash := inscribeWishPNG(t, "# Paint the ship\n\nRed livery, 200 sats.")
	ipfs.TrackWish(hash, "bafy-plain", time.Now())
	src := filepath.Join(dir, "plain.png")
	if err := os.WriteFile(src, blob, 0644); err != nil {
		t.Fatal(err)
	}
	IngestDownloadedFile(context.Background(), src, "bafy-plain", ingest, store)

	c, err := store.GetContract("wish-" + hash)
	if err != nil {
		t.Fatalf("expected contract for tracked plain wish: %v", err)
	}
	if c.Status != "pending" || c.Title != "Paint the ship" {
		t.Fatalf("got status=%q title=%q", c.Status, c.Title)
	}
}
