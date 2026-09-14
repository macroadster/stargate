package smart_contract

import (
	"context"
	"testing"
	"time"

	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
	"stargate-backend/stego"
	auth "stargate-backend/storage/auth"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
)

func attestationKey(t *testing.T) (*btcec.PrivateKey, string) {
	t.Helper()
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("new key: %v", err)
	}
	pubHash := btcutil.Hash160(priv.PubKey().SerializeCompressed())
	addr, err := btcutil.NewAddressWitnessPubKeyHash(pubHash, &chaincfg.TestNet4Params)
	if err != nil {
		t.Fatalf("address: %v", err)
	}
	return priv, addr.EncodeAddress()
}

func TestEnsureStegoIngestionSetsVerifiedCreatorOnNewRecord(t *testing.T) {
	srv, _ := authzFixture(t)
	ctx := context.Background()

	priv, wallet := attestationKey(t)
	const fromPeer = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	manifest := stegoFixtureManifest()
	manifest.VisiblePixelHash = fromPeer
	manifest.CreatorWallet = wallet
	manifest.CreatorSig = bitcoin.SignLegacyMessage(priv, stego.WishCreatorMessage(fromPeer))

	srv.ensureStegoIngestion(ctx, fromPeer, "cid-3", "hash-3", []byte("not-a-real-png"), manifest)

	rec, err := srv.ingestionSvc.Get(fromPeer)
	if err != nil {
		t.Fatalf("get ingestion: %v", err)
	}
	if got, _ := rec.Metadata["creator_wallet"].(string); got != wallet {
		t.Fatalf("creator_wallet = %q, want verified %q", got, wallet)
	}
}

func TestEnsureStegoIngestionIgnoresUnverifiedCreatorClaim(t *testing.T) {
	srv, _ := authzFixture(t)
	ctx := context.Background()

	const fromPeer = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	manifest := stegoFixtureManifest()
	manifest.VisiblePixelHash = fromPeer
	manifest.CreatorWallet = "tb1qattacker"
	manifest.CreatorSig = "not-a-signature"

	srv.ensureStegoIngestion(ctx, fromPeer, "cid-4", "hash-4", []byte("not-a-real-png"), manifest)

	rec, err := srv.ingestionSvc.Get(fromPeer)
	if err != nil {
		t.Fatalf("get ingestion: %v", err)
	}
	if _, has := rec.Metadata["creator_wallet"]; has {
		t.Fatal("unverified creator_wallet must not be recorded")
	}
}

func TestEnsureStegoIngestionDoesNotOverwriteExistingCreator(t *testing.T) {
	srv, _ := authzFixture(t)
	ctx := context.Background()

	priv, wallet := attestationKey(t)
	manifest := stegoFixtureManifest()
	manifest.CreatorWallet = wallet
	manifest.CreatorSig = bitcoin.SignLegacyMessage(priv, stego.WishCreatorMessage(testWishHash))

	srv.ensureStegoIngestion(ctx, testWishHash, "cid-5", "hash-5", []byte("not-a-real-png"), manifest)

	rec, err := srv.ingestionSvc.Get(testWishHash)
	if err != nil {
		t.Fatalf("get ingestion: %v", err)
	}
	if got, _ := rec.Metadata["creator_wallet"].(string); got != testCreatorWlt {
		t.Fatalf("creator_wallet = %q, want origin %q", got, testCreatorWlt)
	}
}

func TestExtractStegoMetadataSkipsCreatorFields(t *testing.T) {
	entries := extractStegoMetadata(map[string]interface{}{
		"wish_text":      "hello",
		"creator_wallet": "tb1qattacker",
		"creator_sig":    "c2ln",
	})
	for _, e := range entries {
		if stego.IsCreatorAttestationMetaKey(e.Key) {
			t.Fatalf("creator field %q leaked into payload metadata", e.Key)
		}
	}
}

func TestReplicaWithVerifiedCreatorCanApprove(t *testing.T) {
	srv, store := authzFixture(t)
	ctx := context.Background()

	priv, wallet := attestationKey(t)
	const fromPeer = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	manifest := stegoFixtureManifest()
	manifest.VisiblePixelHash = fromPeer
	manifest.CreatorWallet = wallet
	manifest.CreatorSig = bitcoin.SignLegacyMessage(priv, stego.WishCreatorMessage(fromPeer))
	srv.ensureStegoIngestion(ctx, fromPeer, "cid-6", "hash-6", []byte("not-a-real-png"), manifest)

	keys := srv.apiKeys.(*mockAPIKeyStore)
	keys.keys["key-replica-creator"] = auth.APIKey{Key: "key-replica-creator", Wallet: wallet}

	contractID := "wish-" + fromPeer
	if err := store.UpsertContractWithTasks(ctx,
		smart_contract.Contract{ContractID: contractID, Title: "replica", Status: "active"},
		[]smart_contract.Task{{TaskID: "rep-ok", ContractID: contractID, Title: "task", Status: "submitted"}},
	); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if err := store.SyncSubmission(ctx, smart_contract.Submission{
		SubmissionID: "sub-replica-ok",
		TaskID:       "rep-ok",
		Status:       "pending_review",
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("seed submission: %v", err)
	}

	if err := srv.authorizeReview(ctx, "key-replica-creator", "sub-replica-ok"); err != nil {
		t.Fatalf("verified replica creator should be able to review, got %v", err)
	}
}

func TestSetCreatorWalletIfAbsentDoesNotOverwrite(t *testing.T) {
	srv, _ := authzFixture(t)
	if err := srv.ingestionSvc.SetCreatorWalletIfAbsent(testWishHash, "tb1qother"); err != nil {
		t.Fatalf("SetCreatorWalletIfAbsent: %v", err)
	}
	rec, err := srv.ingestionSvc.Get(testWishHash)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got, _ := rec.Metadata["creator_wallet"].(string); got != testCreatorWlt {
		t.Fatalf("creator_wallet = %q, want %q", got, testCreatorWlt)
	}
}

func TestVerifyCreatorAttestationAcceptsLegacySignMessage(t *testing.T) {
	priv, wallet := attestationKey(t)
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sig := bitcoin.SignLegacyMessage(priv, stego.WishCreatorMessage(hash))
	if err := VerifyCreatorAttestation(wallet, sig, hash); err != nil {
		t.Fatalf("valid attestation rejected: %v", err)
	}
}

func TestVerifyCreatorAttestationRejectsWrongHash(t *testing.T) {
	priv, wallet := attestationKey(t)
	sig := bitcoin.SignLegacyMessage(priv, stego.WishCreatorMessage("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	if err := VerifyCreatorAttestation(wallet, sig, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err == nil {
		t.Fatal("expected signature over a different hash to fail")
	}
}

func TestVerifyCreatorAttestationRejectsIncomplete(t *testing.T) {
	if err := VerifyCreatorAttestation("", "sig", "hash"); err == nil {
		t.Fatal("expected empty wallet to fail")
	}
}
