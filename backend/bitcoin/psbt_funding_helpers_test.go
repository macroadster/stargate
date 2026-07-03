package bitcoin

import (
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
)

func TestResolveFundingCommitmentRequiresSats(t *testing.T) {
	params := &chaincfg.TestNet4Params
	req := PSBTRequest{PixelHash: make([]byte, 32), CommitmentSats: 0}
	_, sats, _, _, _, don, err := resolveFundingCommitment(params, req)
	if err != nil {
		t.Fatal(err)
	}
	if sats != 0 || don != nil {
		t.Fatalf("expected no commitment without sats, sats=%d don=%v", sats, don != nil)
	}
}

func TestSelectFundingUTXOsInsufficient(t *testing.T) {
	_, _, err := selectFundingUTXOs(nil, 0, nil, nil, 1000, 1, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestFundingModeProducesOPReturn verifies that when DonationAddress,
// PixelHash, and CommitmentSats are all provided (as the server_psbt handler
// now does for commitment_target=funding), resolveFundingCommitment produces
// a donationOutputs with an OP_RETURN script.
func TestFundingModeProducesOPReturn(t *testing.T) {
	params := &chaincfg.TestNet4Params
	// Use a valid testnet4 P2WPKH address as the "donation" (funding mode
	// sets this to the payer's own address).
	addr, err := btcutil.DecodeAddress("tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx", params)
	if err != nil {
		t.Fatalf("decode address: %v", err)
	}
	pixelHash := make([]byte, 32)
	pixelHash[0] = 0xab // non-zero to be realistic

	req := PSBTRequest{
		DonationAddress: addr,
		PixelHash:       pixelHash,
		CommitmentSats:  1000,
	}
	commitmentScript, sats, _, _, _, donation, err := resolveFundingCommitment(params, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if donation == nil {
		t.Fatal("expected donation outputs (OP_RETURN path) but got nil")
	}
	if len(donation.opReturnScript) == 0 {
		t.Fatal("expected non-empty OP_RETURN script")
	}
	if donation.opReturnScript[0] != 0x6a { // OP_RETURN opcode
		t.Fatalf("expected OP_RETURN opcode (0x6a), got 0x%02x", donation.opReturnScript[0])
	}
	if len(donation.donationScript) == 0 {
		t.Fatal("expected non-empty donation script")
	}
	if sats != 1000 {
		t.Fatalf("expected commitment sats=1000, got %d", sats)
	}
	// commitmentScript should be nil when donation path is used (donation
	// struct carries it instead).
	if commitmentScript != nil {
		t.Fatal("expected nil commitmentScript when donation path is used")
	}
}

// TestFundingModeWithoutDonationAddrFallsToHashlock confirms that when
// DonationAddress is nil but PixelHash and CommitmentSats are set, the
// legacy hashlock path is used (no OP_RETURN).  This was the old behaviour
// for funding mode and is now only the fallback.
func TestFundingModeWithoutDonationAddrFallsToHashlock(t *testing.T) {
	params := &chaincfg.TestNet4Params
	addr, err := btcutil.DecodeAddress("tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx", params)
	if err != nil {
		t.Fatalf("decode address: %v", err)
	}
	pixelHash := make([]byte, 32)
	pixelHash[0] = 0xcd

	req := PSBTRequest{
		PixelHash:         pixelHash,
		CommitmentSats:    1000,
		CommitmentAddress: addr,
		// DonationAddress intentionally nil
	}
	commitmentScript, sats, _, _, commitmentAddr, donation, err := resolveFundingCommitment(params, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if donation != nil {
		t.Fatal("expected nil donation (hashlock path)")
	}
	if commitmentScript == nil {
		t.Fatal("expected non-nil commitment script (hashlock)")
	}
	if sats != 1000 {
		t.Fatalf("expected sats=1000, got %d", sats)
	}
	if commitmentAddr == "" {
		t.Fatal("expected non-empty commitment address")
	}
}
