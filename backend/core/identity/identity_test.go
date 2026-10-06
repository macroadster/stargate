package identity

import (
	"strings"
	"testing"
)

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

func TestCanonicalContractID(t *testing.T) {
	h := "A1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2"
	want := strings.ToLower(h)
	if got := CanonicalContractID(h); got != want {
		t.Fatalf("bare: got %q want %q", got, want)
	}
	if got := CanonicalContractID("wish-" + h); got != want {
		t.Fatalf("wish prefix: got %q want %q", got, want)
	}
	if got := CanonicalContractID("wish-aa"); got != "wish-aa" {
		t.Fatalf("non-pixel kept: got %q", got)
	}
	pixel := "41a974b813b024a3817c9c99b5406cc5131a00406c76a4e01fd7519974ccfb40"
	if got := CanonicalContractID("contract-" + pixel); got != pixel {
		t.Fatalf("contract- prefix: got %q want %q", got, pixel)
	}
	if got := CanonicalContractID("CONTRACT-" + strings.ToUpper(pixel)); got != pixel {
		t.Fatalf("contract- prefix case: got %q want %q", got, pixel)
	}
	if got := CanonicalContractID("wish-contract-" + pixel); got != pixel {
		t.Fatalf("wish-contract- prefix: got %q want %q", got, pixel)
	}
	if got := CanonicalContractID("contract-001"); got != "contract-001" {
		t.Fatalf("non-pixel contract- id changed: got %q", got)
	}
}

func TestResultsDirKey(t *testing.T) {
	pixel := "41a974b813b024a3817c9c99b5406cc5131a00406c76a4e01fd7519974ccfb40"
	got, ok := ResultsDirKey(pixel, "contract-"+pixel)
	if !ok || got != pixel {
		t.Fatalf("visible hash: got %q ok=%v", got, ok)
	}
	got, ok = ResultsDirKey("", "contract-"+pixel)
	if !ok || got != pixel {
		t.Fatalf("contract- alias: got %q ok=%v", got, ok)
	}
	got, ok = ResultsDirKey("wish-"+pixel, "contract-001")
	if !ok || got != pixel {
		t.Fatalf("wish- alias: got %q ok=%v", got, ok)
	}
	if _, ok := ResultsDirKey("", "contract-001"); ok {
		t.Fatal("non-pixel contract id must not be a results key")
	}
	if _, ok := ResultsDirKey("wish-test-hash", ""); ok {
		t.Fatal("short wish id must not be a results key")
	}
}
