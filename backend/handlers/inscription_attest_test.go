package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"stargate-backend/bitcoin"
	"stargate-backend/services"
	"stargate-backend/stego"
	auth "stargate-backend/storage/auth"
	"stargate-backend/storage/ingestion"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
)

type attestKeyStore struct {
	keys map[string]auth.APIKey
}

func (s *attestKeyStore) Validate(key string) bool {
	_, ok := s.keys[key]
	return ok
}

func (s *attestKeyStore) Get(key string) (auth.APIKey, bool) {
	k, ok := s.keys[key]
	return k, ok
}

func attestWallet(t *testing.T) (*btcec.PrivateKey, string) {
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

func TestHandleCreatorAttestStoresVerifiedSignature(t *testing.T) {
	priv, wallet := attestWallet(t)
	const hash = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	ingest, err := services.NewIngestionService(filepath.Join(t.TempDir(), "ingest.db"))
	if err != nil {
		t.Fatalf("ingestion: %v", err)
	}
	if err := ingest.Create(ingestion.IngestionRecord{
		ID:        hash,
		Filename:  "wish.png",
		Status:    "pending",
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{"creator_wallet": wallet},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	keys := &attestKeyStore{keys: map[string]auth.APIKey{
		"creator-key": {Key: "creator-key", Wallet: wallet},
	}}
	h := NewInscriptionHandler(nil, ingest, nil, keys)

	body, _ := json.Marshal(map[string]string{"signature": bitcoin.SignLegacyMessage(priv, stego.WishCreatorMessage(hash))})
	req := httptest.NewRequest(http.MethodPost, "/api/inscriptions/"+hash+"/attest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "creator-key")
	rec := httptest.NewRecorder()
	h.HandleInscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	got, err := ingest.Get(hash)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if sig, _ := got.Metadata["creator_sig"].(string); sig == "" {
		t.Fatal("expected creator_sig to be stored")
	}
}

func TestHandleCreatorAttestRejectsStranger(t *testing.T) {
	priv, wallet := attestWallet(t)
	const hash = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	ingest, err := services.NewIngestionService(filepath.Join(t.TempDir(), "ingest.db"))
	if err != nil {
		t.Fatalf("ingestion: %v", err)
	}
	if err := ingest.Create(ingestion.IngestionRecord{
		ID:        hash,
		Filename:  "wish.png",
		Status:    "pending",
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{"creator_wallet": wallet},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	keys := &attestKeyStore{keys: map[string]auth.APIKey{
		"stranger-key": {Key: "stranger-key", Wallet: "tb1qstrangerxxxxxxxxxxxxxxxxxxxxxxxxx"},
	}}
	h := NewInscriptionHandler(nil, ingest, nil, keys)

	body, _ := json.Marshal(map[string]string{"signature": bitcoin.SignLegacyMessage(priv, stego.WishCreatorMessage(hash))})
	req := httptest.NewRequest(http.MethodPost, "/api/inscriptions/"+hash+"/attest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "stranger-key")
	rec := httptest.NewRecorder()
	h.HandleInscription(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 body=%s", rec.Code, rec.Body.String())
	}
}
