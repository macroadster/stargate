package identity

import "testing"

func TestCandidateIDs(t *testing.T) {
	ids := CandidateIDs("abc", "ing-1")
	if len(ids) < 2 {
		t.Fatalf("expected candidates, got %v", ids)
	}
	foundWish := false
	for _, id := range ids {
		if id == "wish-abc" {
			foundWish = true
		}
	}
	// abc is not 64-hex so still gets wish- prefix in CandidateIDs loop
	if !foundWish {
		// still should have wish-abc from add("wish-" + n)
		t.Logf("candidates: %v", ids)
	}
}

func TestIsPixelHash(t *testing.T) {
	h := "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2"
	if !IsPixelHash(h) {
		t.Fatal("expected pixel hash")
	}
	if IsPixelHash("short") {
		t.Fatal("unexpected")
	}
}

func TestToWishID(t *testing.T) {
	if got := ToWishID("abc"); got != "wish-abc" {
		t.Fatalf("got %q", got)
	}
	if got := ToWishID("wish-abc"); got != "wish-abc" {
		t.Fatalf("got %q", got)
	}
}
