package bitcoin

import (
	"testing"

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
