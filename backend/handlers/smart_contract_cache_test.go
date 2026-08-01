package handlers

import (
	"testing"
	"time"

	"stargate-backend/models"
	storageSC "stargate-backend/storage/smart_contract"
)

func TestEnrichedCacheTTLAndInvalidate(t *testing.T) {
	raw := storageSC.NewContractCache(time.Minute, 100)
	h := NewSmartContractHandler(nil, nil, raw)
	h.enrichedTTL = 50 * time.Millisecond

	key := "contracts:status:confirmed:limit:12"
	items := []models.InscriptionRequest{{ID: "old-contract", Status: "confirmed", Timestamp: 1}}
	h.setEnrichedCache(key, items, 12)

	got, fetched, ok := h.getEnrichedCache(key)
	if !ok || len(got) != 1 || got[0].ID != "old-contract" || fetched != 12 {
		t.Fatalf("expected cache hit, got ok=%v fetched=%d items=%+v", ok, fetched, got)
	}

	// Expire
	time.Sleep(60 * time.Millisecond)
	if _, _, ok := h.getEnrichedCache(key); ok {
		t.Fatal("expected TTL miss after expiry")
	}

	// Repopulate then invalidate
	h.setEnrichedCache(key, items, 12)
	// Also put something in raw cache
	raw.Set(key, nil) // empty contracts still sets entry; use via InvalidateAll path
	raw.Set("contracts:status:confirmed:limit:12", nil)
	h.InvalidateContractCache()
	if _, _, ok := h.getEnrichedCache(key); ok {
		t.Fatal("expected miss after InvalidateContractCache")
	}
	if _, ok := raw.Get(key); ok {
		t.Fatal("expected raw cache cleared by InvalidateAll")
	}
}

func TestContractsPageHasMoreIgnoresDedupeShrink(t *testing.T) {
	// SQL returned a full page of 12; twin dedupe left 11 visible.
	if !contractsPageHasMore(12, 11, 12) {
		t.Fatal("full pre-dedupe page must report has_more")
	}
	if contractsPageHasMore(11, 11, 12) {
		t.Fatal("short pre-dedupe page must not report has_more")
	}
	if !contractsPageHasMore(0, 12, 12) {
		t.Fatal("pageLen alone still signals full page when fetch count unknown")
	}
	if contractsPageHasMore(0, 0, 12) {
		t.Fatal("empty page has no more")
	}
}
