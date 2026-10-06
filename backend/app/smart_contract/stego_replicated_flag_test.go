package smart_contract

import (
	"context"
	"testing"

	"stargate-backend/stego"
)

// ensureStegoIngestion hands one metadata map to both its create and update
// paths, and UpdateFromIngest merges with incoming precedence, so anything in
// that map overwrites what a locally created wish already recorded. These pin
// stego_replicated to the create path only.

func stegoFixtureManifest() stego.Manifest {
	return stego.Manifest{
		SchemaVersion:    2,
		ProposalID:       "proposal-stego",
		VisiblePixelHash: testWishHash,
		Issuer:           "oracle-1",
		CreatedAt:        1,
	}
}

// A wish created locally must not be relabelled as replicated when its own
// stego image is later reconciled, and its creator must survive.
func TestEnsureStegoIngestionDoesNotStampExistingRecord(t *testing.T) {
	srv, _ := authzFixture(t)
	ctx := context.Background()

	srv.ensureStegoIngestion(ctx, testWishHash, "cid-1", "hash-1", []byte("not-a-real-png"), stegoFixtureManifest())

	rec, err := srv.ingestionSvc.Get(testWishHash)
	if err != nil {
		t.Fatalf("get ingestion: %v", err)
	}
	if _, stamped := rec.Metadata["stego_replicated"]; stamped {
		t.Fatal("a locally created wish was marked replicated by reconciling its own stego image")
	}
	if got, _ := rec.Metadata["creator_wallet"].(string); got != testCreatorWlt {
		t.Fatalf("creator_wallet = %q, want %q: the update path overwrote it", got, testCreatorWlt)
	}

	// The flag drives an authorization message, so confirm the owner still passes.
	if _, err := srv.authorizer().Authorize(testCreatorKey, testWishHash, "submission s1"); err != nil {
		t.Fatalf("wish creator should still be authorized after reconcile, got %v", err)
	}
}

// A record that genuinely originates from a peer still gets the flag, otherwise
// the denial cannot explain itself.
func TestEnsureStegoIngestionStampsNewRecord(t *testing.T) {
	srv, _ := authzFixture(t)
	ctx := context.Background()

	const fromPeer = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	manifest := stegoFixtureManifest()
	manifest.VisiblePixelHash = fromPeer

	srv.ensureStegoIngestion(ctx, fromPeer, "cid-2", "hash-2", []byte("not-a-real-png"), manifest)

	rec, err := srv.ingestionSvc.Get(fromPeer)
	if err != nil {
		t.Fatalf("get ingestion: %v", err)
	}
	if replicated, _ := rec.Metadata["stego_replicated"].(bool); !replicated {
		t.Fatalf("a peer-created record should be marked replicated, metadata: %v", rec.Metadata)
	}
	if _, hasCreator := rec.Metadata["creator_wallet"]; hasCreator {
		t.Fatal("reconcile must not invent a creator_wallet from the wire")
	}
}
