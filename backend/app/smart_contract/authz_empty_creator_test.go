package smart_contract

import (
	"strings"
	"testing"

	auth "stargate-backend/storage/auth"
	"stargate-backend/storage/ingestion"
)

// seedCreatorMetadata writes an ingestion row for hash carrying meta, and returns
// an authorizer over it. The row id is the bare hash, which is what both the
// local wish path (inscription_handler.go) and ensureStegoIngestion use.
func seedCreatorMetadata(t *testing.T, hash string, meta map[string]interface{}) WishCreatorAuthorizer {
	t.Helper()
	ing, err := ingestion.NewIngestionService(t.TempDir() + "/ing.db")
	if err != nil {
		t.Fatalf("ingestion service: %v", err)
	}
	if err := ing.Create(ingestion.IngestionRecord{
		ID:       hash,
		Filename: "wish.png",
		Metadata: meta,
		Status:   "verified",
	}); err != nil {
		t.Fatalf("seed ingestion row: %v", err)
	}
	keys := &mockAPIKeyStore{keys: map[string]auth.APIKey{
		testStrangerKey: {Key: testStrangerKey, Wallet: testStrangerWlt},
	}}
	return WishCreatorAuthorizer{Keys: keys, Ingestion: ing}
}

// A wish whose creator_wallet is empty has no established creator.
//
// inscription_handler.go writes the key unconditionally, so "" arrives whenever
// no creator key resolved. It type-asserts as a string, so the authorizer used to
// call that a known creator of "" (stargate-b11).
func TestEmptyCreatorWalletIsNotAKnownCreator(t *testing.T) {
	a := seedCreatorMetadata(t, testWishHash, map[string]interface{}{"creator_wallet": ""})

	if own := a.wishOwnership(testWishHash); own.known {
		t.Errorf("wishOwnership reports a known creator of %q; an empty wallet establishes nobody", own.creator)
	}
}

// Whitespace is the same case: TrimSpace already ran before the comparison, so a
// blank value could never match a bound wallet either.
func TestBlankCreatorWalletIsNotAKnownCreator(t *testing.T) {
	a := seedCreatorMetadata(t, testWishHash, map[string]interface{}{"creator_wallet": "   "})

	if own := a.wishOwnership(testWishHash); own.known {
		t.Errorf("wishOwnership reports a known creator of %q; a blank wallet establishes nobody", own.creator)
	}
}

// The point of the change: the denial has to say the wish records no creator,
// not that the caller's wallet is the wrong one.
//
// Both spellings denied before and still deny, so the decision is not what this
// pins -- the explanation is. An operator told "does not match wish creator"
// goes looking for the creator's key; there isn't one to find.
func TestEmptyCreatorWalletDeniesWithTheAccurateReason(t *testing.T) {
	a := seedCreatorMetadata(t, testWishHash, map[string]interface{}{"creator_wallet": ""})

	wallet, err := a.Authorize(testStrangerKey, testWishHash, "the submission")
	if err == nil {
		t.Fatalf("Authorize allowed %q against a wish with no recorded creator", wallet)
	}
	if strings.Contains(err.Error(), "does not match wish creator") {
		t.Errorf("denial blames the caller's key: %v", err)
	}
	if !strings.Contains(err.Error(), "no creator wallet recorded") {
		t.Errorf("denial does not say the wish records no creator: %v", err)
	}
}

// A real creator still authorizes, and a stranger is still refused against one.
// Without this, narrowing wishOwnership until it never reports known would pass
// every test above.
func TestRecordedCreatorStillAuthorizes(t *testing.T) {
	a := seedCreatorMetadata(t, testWishHash, map[string]interface{}{"creator_wallet": testStrangerWlt})

	own := a.wishOwnership(testWishHash)
	if !own.known || own.creator != testStrangerWlt {
		t.Fatalf("wishOwnership lost a real creator: known=%v creator=%q", own.known, own.creator)
	}
	if _, err := a.Authorize(testStrangerKey, testWishHash, "the submission"); err != nil {
		t.Errorf("the recorded creator was refused: %v", err)
	}
}

// A replicated wish with an empty creator_wallet now reaches the replica message
// rather than the mismatch one, which is the case the wording was written for.
func TestReplicatedWishWithEmptyCreatorReportsReplication(t *testing.T) {
	a := seedCreatorMetadata(t, testWishHash, map[string]interface{}{
		"creator_wallet":   "",
		"stego_replicated": true,
	})

	_, err := a.Authorize(testStrangerKey, testWishHash, "the submission")
	if err == nil {
		t.Fatal("Authorize allowed a replicated wish with no creator")
	}
	if !strings.Contains(err.Error(), "replicated from another node") {
		t.Errorf("denial does not mention replication: %v", err)
	}
}
