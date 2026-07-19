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
	h.setEnrichedCache(key, items)

	got, ok := h.getEnrichedCache(key)
	if !ok || len(got) != 1 || got[0].ID != "old-contract" {
		t.Fatalf("expected cache hit, got ok=%v items=%+v", ok, got)
	}

	// Expire
	time.Sleep(60 * time.Millisecond)
	if _, ok := h.getEnrichedCache(key); ok {
		t.Fatal("expected TTL miss after expiry")
	}

	// Repopulate then invalidate
	h.setEnrichedCache(key, items)
	// Also put something in raw cache
	raw.Set(key, nil) // empty contracts still sets entry; use via InvalidateAll path
	raw.Set("contracts:status:confirmed:limit:12", nil)
	h.InvalidateContractCache()
	if _, ok := h.getEnrichedCache(key); ok {
		t.Fatal("expected miss after InvalidateContractCache")
	}
	if _, ok := raw.Get(key); ok {
		t.Fatal("expected raw cache cleared by InvalidateAll")
	}
}
