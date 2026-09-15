package smart_contract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	core "stargate-backend/core/smart_contract"
	"stargate-backend/stego"
	"stargate-backend/storage/datadir"
	scstore "stargate-backend/storage/smart_contract"
)

func sandboxCoverPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 40, B: 80, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode cover: %v", err)
	}
	return buf.Bytes()
}

func stageSandboxTarball(t *testing.T, uploadsDir, contents string) (hash, tarballPath string) {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte(contents), 0o600); err != nil {
		t.Fatalf("write sandbox file: %v", err)
	}
	tmpTar := filepath.Join(t.TempDir(), "sandbox.tgz")
	hash, err := stego.WriteSandboxTarball(src, tmpTar)
	if err != nil {
		t.Fatalf("WriteSandboxTarball: %v", err)
	}
	tarballPath = datadir.PartPath(uploadsDir, hash)
	if err := os.MkdirAll(filepath.Dir(tarballPath), 0o755); err != nil {
		t.Fatalf("mkdir tarball dest: %v", err)
	}
	data, err := os.ReadFile(tmpTar)
	if err != nil {
		t.Fatalf("read tarball: %v", err)
	}
	if err := os.WriteFile(tarballPath, data, 0o600); err != nil {
		t.Fatalf("stage tarball: %v", err)
	}
	return hash, tarballPath
}

func resultsDir(uploadsDir, contractID string) string {
	return datadir.PartResolve(filepath.Join(uploadsDir, "results"), scstore.NormalizeContractID(contractID))
}

func seedContractWithSandbox(t *testing.T, store *scstore.MemoryStore, contractID, status, sandboxHash string) {
	t.Helper()
	ctx := context.Background()
	c := core.Contract{
		ContractID: contractID,
		Status:     status,
		Metadata:   map[string]interface{}{"sandbox_hash": sandboxHash},
	}
	if err := store.UpsertContractWithTasks(ctx, c, nil); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
}

func TestProcessEventConfirmDoesNotExtractSandbox(t *testing.T) {
	uploads := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploads)
	hash, _ := stageSandboxTarball(t, uploads, "<html>local-confirm</html>")

	store := scstore.NewMemoryStore(0)
	srv := NewServer(store, nil, nil)
	seedContractWithSandbox(t, store, "contract-osv1-local", "confirmed", hash)

	srv.processEvent(core.Event{
		Type:      "contract_confirmed",
		EntityID:  "contract-osv1-local",
		Actor:     "oracle",
		CreatedAt: time.Now(),
	}, false)

	if _, err := os.Stat(resultsDir(uploads, "contract-osv1-local")); err == nil {
		t.Fatal("processEvent extracted results/ on contract_confirmed")
	}
}

func TestReconcileSyncAnnouncementConfirmDoesNotExtract(t *testing.T) {
	uploads := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploads)
	hash, _ := stageSandboxTarball(t, uploads, "<html>sync-confirm</html>")

	store := scstore.NewMemoryStore(0)
	srv := NewServer(store, nil, nil)
	ctx := context.Background()

	// Local row has no sandbox_hash yet — merge must copy it without extracting.
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: "contract-osv1-sync",
		Status:     "active",
	}, nil); err != nil {
		t.Fatalf("seed contract: %v", err)
	}

	ann := &syncAnnouncement{
		Type:   "contract_confirmed",
		Issuer: "peer",
		Contract: &core.Contract{
			ContractID: "contract-osv1-sync",
			Status:     "confirmed",
			Metadata:   map[string]interface{}{"sandbox_hash": hash},
		},
	}
	if err := srv.ReconcileSyncAnnouncement(ctx, ann); err != nil {
		t.Fatalf("ReconcileSyncAnnouncement: %v", err)
	}

	got, err := store.GetContract("contract-osv1-sync")
	if err != nil {
		t.Fatalf("GetContract: %v", err)
	}
	if !strings.EqualFold(got.Status, "confirmed") {
		t.Fatalf("status=%q, want confirmed", got.Status)
	}
	if toString(got.Metadata["sandbox_hash"]) != hash {
		t.Fatalf("sandbox_hash not merged: %#v", got.Metadata)
	}
	if _, err := os.Stat(resultsDir(uploads, "contract-osv1-sync")); err == nil {
		t.Fatal("synced confirm extracted results/")
	}
}

func TestReconcileStegoDoesNotExtractWhenConfirmed(t *testing.T) {
	uploads := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploads)
	sandboxHash, _ := stageSandboxTarball(t, uploads, "<html>stego-confirm</html>")

	vph := strings.Repeat("ab", 32)
	payload := stego.Payload{
		SchemaVersion:    2,
		ProposalID:       "proposal-osv2",
		VisiblePixelHash: vph,
		Issuer:           "tester",
		CreatedAt:        time.Now().Unix(),
		SandboxHash:      sandboxHash,
		Proposal: stego.PayloadProposal{
			ID:               "proposal-osv2",
			Title:            "OSV2 Stego",
			DescriptionMD:    "metadata only",
			BudgetSats:       1000,
			VisiblePixelHash: vph,
			CreatedAt:        time.Now().Unix(),
		},
		Tasks: []stego.PayloadTask{{
			TaskID:      "task-osv2",
			Title:       "Deliver",
			Description: "Do the work",
			BudgetSats:  1000,
			Skills:      []string{"manual-review"},
		}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	needPixels := (len(raw)+len(stego.AlphaPrefix)+1)*8 + 64
	side := 64
	for side*side < needPixels {
		side *= 2
	}
	inscribed, err := stego.Inscribe(sandboxCoverPNG(t, side, side), string(raw), "alpha")
	if err != nil {
		t.Fatalf("inscribe: %v", err)
	}
	stegoHash := inscribed.ImageSHA256
	stegoPath := datadir.PartPath(uploads, stegoHash)
	if err := os.MkdirAll(filepath.Dir(stegoPath), 0o755); err != nil {
		t.Fatalf("mkdir stego: %v", err)
	}
	if err := os.WriteFile(stegoPath, inscribed.ImageBytes, 0o600); err != nil {
		t.Fatalf("write stego: %v", err)
	}

	store := scstore.NewMemoryStore(0)
	srv := NewServer(store, nil, nil)
	ctx := context.Background()
	wishID := "wish-" + vph
	if err := store.UpsertContractWithTasks(ctx, core.Contract{
		ContractID: wishID,
		Title:      "pre-confirmed",
		Status:     "confirmed",
	}, nil); err != nil {
		t.Fatalf("seed confirmed contract: %v", err)
	}

	if err := srv.ReconcileStego(ctx, stegoHash, stegoHash); err != nil {
		t.Fatalf("ReconcileStego: %v", err)
	}

	got, err := store.GetContract(wishID)
	if err != nil {
		t.Fatalf("GetContract: %v", err)
	}
	if !strings.EqualFold(got.Status, "confirmed") {
		t.Fatalf("status=%q, want confirmed", got.Status)
	}
	if toString(got.Metadata["sandbox_hash"]) != sandboxHash {
		t.Fatalf("sandbox_hash not upserted: %#v", got.Metadata)
	}
	if _, err := store.GetProposal(ctx, "proposal-osv2"); err != nil {
		t.Fatalf("proposal not upserted: %v", err)
	}
	tasks, err := store.ListTasks(core.TaskFilter{ContractID: wishID})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("tasks not upserted")
	}
	if _, err := os.Stat(resultsDir(uploads, wishID)); err == nil {
		t.Fatal("ReconcileStego extracted results/ for a confirmed contract")
	}
}

func TestDownloadSandboxArtifactsRefusesUnconfirmed(t *testing.T) {
	uploads := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploads)
	hash, _ := stageSandboxTarball(t, uploads, "<html>unconfirmed</html>")

	store := scstore.NewMemoryStore(0)
	srv := NewServer(store, nil, nil)
	seedContractWithSandbox(t, store, "contract-osv4-open", "funded", hash)

	err := srv.downloadSandboxArtifacts(context.Background(), "contract-osv4-open")
	if !errors.Is(err, errSandboxNotConfirmed) {
		t.Fatalf("err=%v, want errSandboxNotConfirmed", err)
	}
	if _, err := os.Stat(resultsDir(uploads, "contract-osv4-open")); err == nil {
		t.Fatal("unconfirmed pull extracted results/")
	}
}

func TestDownloadSandboxArtifactsExtractsWhenConfirmed(t *testing.T) {
	uploads := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploads)
	hash, _ := stageSandboxTarball(t, uploads, "<html>confirmed-pull</html>")

	store := scstore.NewMemoryStore(0)
	srv := NewServer(store, nil, nil)
	seedContractWithSandbox(t, store, "contract-osv4-ok", "confirmed", hash)

	if err := srv.downloadSandboxArtifacts(context.Background(), "contract-osv4-ok"); err != nil {
		t.Fatalf("downloadSandboxArtifacts: %v", err)
	}
	got := filepath.Join(resultsDir(uploads, "contract-osv4-ok"), "index.html")
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("expected extracted file: %v", err)
	}

	// Second pull is a no-op when the hash matches.
	if err := srv.downloadSandboxArtifacts(context.Background(), "contract-osv4-ok"); err != nil {
		t.Fatalf("idempotent pull: %v", err)
	}
}

func TestRESTSandboxPullRefusesUnconfirmed(t *testing.T) {
	uploads := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploads)
	hash, _ := stageSandboxTarball(t, uploads, "<html>rest-unconfirmed</html>")

	store := scstore.NewMemoryStore(0)
	srv := NewServer(store, nil, nil)
	seedContractWithSandbox(t, store, "contract-osv4-rest-open", "active", hash)

	req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/contracts/contract-osv4-rest-open/sandbox/pull", nil)
	rec := httptest.NewRecorder()
	srv.handleContracts(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(resultsDir(uploads, "contract-osv4-rest-open")); err == nil {
		t.Fatal("REST pull extracted unconfirmed results/")
	}
}

func TestRESTSandboxPullExtractsWhenConfirmed(t *testing.T) {
	uploads := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploads)
	hash, _ := stageSandboxTarball(t, uploads, "<html>rest-confirmed</html>")

	store := scstore.NewMemoryStore(0)
	srv := NewServer(store, nil, nil)
	seedContractWithSandbox(t, store, "contract-osv4-rest-ok", "confirmed", hash)

	req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/contracts/contract-osv4-rest-ok/sandbox/pull", nil)
	rec := httptest.NewRecorder()
	srv.handleContracts(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	got := filepath.Join(resultsDir(uploads, "contract-osv4-rest-ok"), "index.html")
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("expected extracted file: %v", err)
	}
}

func TestSandboxTarballHashMatchesStagedFile(t *testing.T) {
	// Guard the fixture: downloadSandboxArtifacts compares sha256(tarball)
	// to sandbox_hash. A helper that hashed the dir instead would make
	// extract tests pass for the wrong reason.
	uploads := t.TempDir()
	hash, path := stageSandboxTarball(t, uploads, "<html>hash-check</html>")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read staged tarball: %v", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != hash {
		t.Fatalf("staged tarball hash %s != WriteSandboxTarball %s", hex.EncodeToString(sum[:]), hash)
	}
}
