package smart_contract

import (
	"testing"

	coresc "stargate-backend/core/smart_contract"
)

func TestPrepareProposalForCreateDefaults(t *testing.T) {
	p := coresc.Proposal{
		ID:               "p1",
		Title:            "Hello",
		VisiblePixelHash: "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2",
	}
	vh, meta, wish, err := PrepareProposalForCreate(&p)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "pending" {
		t.Fatalf("status %q", p.Status)
	}
	if vh != p.VisiblePixelHash {
		t.Fatalf("visible %q", vh)
	}
	if len(meta) == 0 {
		t.Fatal("expected metadata json")
	}
	if wish != "" {
		t.Fatalf("pending should not supersede wish, got %q", wish)
	}
	if p.Metadata["visible_pixel_hash"] != p.VisiblePixelHash {
		t.Fatal("metadata should backfill vph")
	}
}

func TestPrepareProposalForCreateSupersedeWish(t *testing.T) {
	p := coresc.Proposal{
		ID:               "p2",
		Title:            "Approved",
		Status:           "approved",
		VisiblePixelHash: "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2",
	}
	_, _, wish, err := PrepareProposalForCreate(&p)
	if err != nil {
		t.Fatal(err)
	}
	if wish != "wish-"+p.VisiblePixelHash {
		t.Fatalf("wish %q", wish)
	}
}
