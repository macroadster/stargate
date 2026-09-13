package smart_contract

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	auth "stargate-backend/storage/auth"
	"stargate-backend/storage/ingestion"
	scstore "stargate-backend/storage/smart_contract"
)

const (
	testWishHash    = "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"
	testCreatorKey  = "key-creator"
	testStrangerKey = "key-stranger"
	testCreatorWlt  = "bc1qcreator"
	testStrangerWlt = "bc1qstranger"
)

// authzFixture wires a Server over a memory store plus a sqlite-backed ingestion
// service holding one wish whose creator_wallet is testCreatorWlt.
func authzFixture(t *testing.T) (*Server, scstore.Store) {
	t.Helper()

	ingestSvc, err := services.NewIngestionService(filepath.Join(t.TempDir(), "ingest.db"))
	if err != nil {
		t.Fatalf("ingestion service: %v", err)
	}
	if err := ingestSvc.Create(ingestion.IngestionRecord{
		ID:        testWishHash,
		Filename:  "wish.png",
		Status:    "approved",
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{"creator_wallet": testCreatorWlt},
	}); err != nil {
		t.Fatalf("seed ingestion: %v", err)
	}

	store := scstore.NewMemoryStore(time.Hour)
	keys := &mockAPIKeyStore{keys: map[string]auth.APIKey{
		testCreatorKey:  {Key: testCreatorKey, Wallet: testCreatorWlt},
		testStrangerKey: {Key: testStrangerKey, Wallet: testStrangerWlt},
	}}
	return NewServer(store, keys, ingestSvc), store
}

// seedSubmission creates contract wish-<hash> with one task and a submission on it.
func seedSubmission(t *testing.T, store scstore.Store, submissionID, taskID string) {
	t.Helper()
	ctx := context.Background()

	contractID := "wish-" + testWishHash
	if err := store.UpsertContractWithTasks(ctx,
		smart_contract.Contract{ContractID: contractID, Title: "wish", Status: "active"},
		[]smart_contract.Task{{TaskID: taskID, ContractID: contractID, Title: "task", Status: "submitted"}},
	); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if err := store.SyncSubmission(ctx, smart_contract.Submission{
		SubmissionID: submissionID,
		TaskID:       taskID,
		Status:       "pending_review",
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("seed submission: %v", err)
	}
}

// authorizeReview exercises the gate the server hands to SubmissionService,
// which is where submission review authorization is enforced. The wallet it
// returns is covered by the service tests that assert on the review event.
func (s *Server) authorizeReview(ctx context.Context, apiKey, submissionID string) error {
	_, err := s.submissionGate().AuthorizeSubmissionReview(ctx, apiKey, submissionID)
	return err
}

func TestAuthorizeSubmissionReviewRejectsStranger(t *testing.T) {
	srv, store := authzFixture(t)
	seedSubmission(t, store, "sub-1", "task-1")

	if err := srv.authorizeReview(context.Background(), testStrangerKey, "sub-1"); err == nil {
		t.Fatal("expected a wallet that does not own the wish to be denied, got nil")
	}
}

func TestAuthorizeSubmissionReviewAllowsWishCreator(t *testing.T) {
	srv, store := authzFixture(t)
	seedSubmission(t, store, "sub-2", "task-2")

	if err := srv.authorizeReview(context.Background(), testCreatorKey, "sub-2"); err != nil {
		t.Fatalf("expected the wish creator to be allowed, got %v", err)
	}
}

func TestAuthorizeSubmissionReviewRejectsUnknownKey(t *testing.T) {
	srv, store := authzFixture(t)
	seedSubmission(t, store, "sub-3", "task-3")

	if err := srv.authorizeReview(context.Background(), "not-a-key", "sub-3"); err == nil {
		t.Fatal("expected an unbound api key to be denied, got nil")
	}
}

// A submission with neither task nor claim cannot be traced to a wish, so it
// must fail closed rather than fall through to the missing-creator allowance.
func TestAuthorizeSubmissionReviewFailsClosedWithoutTask(t *testing.T) {
	srv, store := authzFixture(t)
	if err := store.SyncSubmission(context.Background(), smart_contract.Submission{
		SubmissionID: "sub-orphan",
		Status:       "pending_review",
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("seed submission: %v", err)
	}

	if err := srv.authorizeReview(context.Background(), testCreatorKey, "sub-orphan"); err == nil {
		t.Fatal("expected an unresolvable submission to be denied, got nil")
	}
}

// TaskID is omitempty, so a submission may reach us carrying only a ClaimID.
// Failing closed must not deny those: the wish is reachable via the claim.
func TestAuthorizeSubmissionReviewResolvesViaClaim(t *testing.T) {
	srv, store := authzFixture(t)
	ctx := context.Background()
	seedSubmission(t, store, "sub-claim-seed", "task-claim")

	if err := store.SyncClaim(ctx, smart_contract.Claim{
		ClaimID:   "claim-1",
		TaskID:    "task-claim",
		Status:    "submitted",
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed claim: %v", err)
	}
	// No TaskID: the claim is the only route to the wish.
	if err := store.SyncSubmission(ctx, smart_contract.Submission{
		SubmissionID: "sub-via-claim",
		ClaimID:      "claim-1",
		Status:       "pending_review",
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("seed submission: %v", err)
	}

	if err := srv.authorizeReview(ctx, testCreatorKey, "sub-via-claim"); err != nil {
		t.Fatalf("expected the wish creator to be allowed via the claim walk, got %v", err)
	}
	if err := srv.authorizeReview(ctx, testStrangerKey, "sub-via-claim"); err == nil {
		t.Fatal("expected a stranger to be denied via the claim walk, got nil")
	}
}

func TestAuthorizeSubmissionReviewRejectsMissingSubmission(t *testing.T) {
	srv, _ := authzFixture(t)

	if err := srv.authorizeReview(context.Background(), testCreatorKey, "nope"); err == nil {
		t.Fatal("expected a missing submission to be denied, got nil")
	}
}

func TestSubmissionWishHashStripsContractPrefix(t *testing.T) {
	_, store := authzFixture(t)
	seedSubmission(t, store, "sub-4", "task-4")

	sub, err := store.GetSubmission(context.Background(), "sub-4")
	if err != nil {
		t.Fatalf("get submission: %v", err)
	}
	got, err := SubmissionWishHash(store, sub)
	if err != nil {
		t.Fatalf("resolve hash: %v", err)
	}
	if got != testWishHash {
		t.Fatalf("hash = %q, want %q", got, testWishHash)
	}
}

func TestWishCreatorAuthorizerRequiresWalletBinding(t *testing.T) {
	a := WishCreatorAuthorizer{keysWithoutWallet(), nil}

	if _, err := a.Authorize("key-nowallet", testWishHash, "proposal p1", AllowOnMissingCreator); err == nil {
		t.Fatal("expected a key with no wallet binding to be denied, got nil")
	}
}

func TestWishCreatorAuthorizerAllowsGlobalAuditor(t *testing.T) {
	t.Setenv("STARLIGHT_DONATION_ADDRESS", testStrangerWlt)
	a := WishCreatorAuthorizer{Keys: &mockAPIKeyStore{keys: map[string]auth.APIKey{
		testStrangerKey: {Key: testStrangerKey, Wallet: testStrangerWlt},
	}}}

	wallet, err := a.Authorize(testStrangerKey, testWishHash, "proposal p1", DenyOnMissingCreator)
	if err != nil {
		t.Fatalf("expected the donation address to act as global auditor, got %v", err)
	}
	// The auditor's own wallet, not the creator's: callers record who acted.
	if wallet != testStrangerWlt {
		t.Fatalf("authorized wallet = %q, want the auditor %q", wallet, testStrangerWlt)
	}
}

// The two policies must diverge on an unresolvable creator, since that is the
// whole point of the distinction: proposals keep the pre-existing allowance,
// payouts do not.
func TestMissingCreatorPolicyDivergesOnUnknownWish(t *testing.T) {
	srv, _ := authzFixture(t)
	a := srv.authorizer()
	const unknown = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	if _, err := a.Authorize(testStrangerKey, unknown, "proposal p1", AllowOnMissingCreator); err != nil {
		t.Fatalf("proposal approval should retain its allowance until irl.2, got %v", err)
	}
	if _, err := a.Authorize(testStrangerKey, unknown, "submission s1", DenyOnMissingCreator); err == nil {
		t.Fatal("expected payout review to deny an unresolvable creator, got nil")
	}
}

// Replicated wishes are the realistic way to reach the missing-creator branch,
// and the denial should name that cause so operators do not chase absent data.
func TestDenyOnMissingCreatorExplainsReplicatedWish(t *testing.T) {
	srv, _ := authzFixture(t)
	const replicated = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	if err := srv.ingestionSvc.Create(ingestion.IngestionRecord{
		ID:        replicated,
		Filename:  "stego.png",
		Status:    "verified",
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{"stego_replicated": true},
	}); err != nil {
		t.Fatalf("seed replicated ingestion: %v", err)
	}

	_, err := srv.authorizer().Authorize(testStrangerKey, replicated, "submission s1", DenyOnMissingCreator)
	if err == nil {
		t.Fatal("expected a replicated wish to deny payout review, got nil")
	}
	if !strings.Contains(err.Error(), "replicated from another node") {
		t.Fatalf("error should explain the replica case, got %q", err)
	}
}

// A wallet that owns the wish must still be allowed under the strict policy;
// failing closed should not degrade into denying everyone.
func TestDenyOnMissingCreatorStillAllowsOwner(t *testing.T) {
	srv, _ := authzFixture(t)

	if _, err := srv.authorizer().Authorize(testCreatorKey, testWishHash, "submission s1", DenyOnMissingCreator); err != nil {
		t.Fatalf("expected the wish creator to be allowed under the strict policy, got %v", err)
	}
}

func keysWithoutWallet() auth.APIKeyValidator {
	return &mockAPIKeyStore{keys: map[string]auth.APIKey{"key-nowallet": {Key: "key-nowallet"}}}
}
