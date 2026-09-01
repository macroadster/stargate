package smart_contract

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"

	"path/filepath"

	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
	"stargate-backend/services"
	auth "stargate-backend/storage/auth"
	scstore "stargate-backend/storage/smart_contract"
)

// mockAPIKeyStore is a simple mock for testing
type mockAPIKeyStore struct {
	keys map[string]auth.APIKey
}

func (m *mockAPIKeyStore) Validate(key string) bool {
	_, ok := m.keys[key]
	return ok
}

func (m *mockAPIKeyStore) Get(key string) (auth.APIKey, bool) {
	k, ok := m.keys[key]
	return k, ok
}

func TestApproveProposalRequiresWishContract(t *testing.T) {
	store := scstore.NewMemoryStore(72 * 60 * 60)

	// Set up mock API key store with wallet binding
	apiKey := "approve-rest-key"
	creatorWallet := "bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh"
	mockStore := &mockAPIKeyStore{
		keys: map[string]auth.APIKey{
			apiKey: {Key: apiKey, Wallet: creatorWallet},
		},
	}

	server := NewServer(store, mockStore, nil)

	visibleHash := strings.Repeat("b", 64)
	proposal := smart_contract.Proposal{
		ID:               "proposal-approve-rest",
		Title:            "Approve proposal",
		DescriptionMD:    "Approve proposal details",
		VisiblePixelHash: visibleHash,
		BudgetSats:       1000,
		Status:           "pending",
		Tasks: []smart_contract.Task{
			{
				TaskID:     "proposal-approve-rest-task-1",
				ContractID: "proposal-approve-rest",
				Title:      "Do work",
				BudgetSats: 1000,
				Status:     "available",
			},
		},
		Metadata: map[string]interface{}{
			"creator_wallet":     creatorWallet,
			"visible_pixel_hash": visibleHash,
		},
	}
	if err := store.CreateProposal(context.Background(), proposal); err != nil {
		t.Fatalf("failed to seed proposal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/proposals/"+proposal.ID+"/approve", nil)
	req.Header.Set("X-API-Key", apiKey)
	rec := httptest.NewRecorder()
	server.handleProposals(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "wish not found") {
		t.Fatalf("expected wish not found error, got: %s", rec.Body.String())
	}

	wishID := "wish-" + visibleHash
	contract := smart_contract.Contract{
		ContractID: wishID,
		Title:      "Wish",
		Status:     "pending",
	}
	if err := store.UpsertContractWithTasks(context.Background(), contract, nil); err != nil {
		t.Fatalf("failed to seed wish contract: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/smart_contract/proposals/"+proposal.ID+"/approve", nil)
	req.Header.Set("X-API-Key", apiKey)
	rec = httptest.NewRecorder()
	server.handleProposals(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestContractPSBTRejectsInvalidChangeAddress(t *testing.T) {
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	store := scstore.NewMemoryStore(72 * 60 * 60)
	payerWallet := mustTestnetAddress(t, 1)
	apiKey := "psbt-rest-key"
	server := NewServer(store, &mockAPIKeyStore{
		keys: map[string]auth.APIKey{
			apiKey: {Key: apiKey, Wallet: payerWallet},
		},
	}, nil)
	server.mempool = &bitcoin.MempoolClient{}

	contract := smart_contract.Contract{
		ContractID:      "contract-invalid-change",
		Title:           "Test contract",
		Status:          "open",
		TotalBudgetSats: 1000,
	}
	if err := store.UpsertContractWithTasks(context.Background(), contract, nil); err != nil {
		t.Fatalf("failed to seed contract: %v", err)
	}

	body := `{"contractor_wallet":"` + mustTestnetAddress(t, 2) + `","pixel_hash":"` + strings.Repeat("a", 64) + `","change_address":"not-an-address"}`
	req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/contracts/"+contract.ContractID+"/psbt", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	rec := httptest.NewRecorder()

	server.handleContracts(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid change address") {
		t.Fatalf("expected invalid change address error, got: %s", rec.Body.String())
	}
}

func TestContractPSBTResponseIncludesEffectiveChangeAddress(t *testing.T) {
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	store := scstore.NewMemoryStore(72 * 60 * 60)
	payerWallet := mustTestnetAddress(t, 1)
	contractorWallet := mustTestnetAddress(t, 2)
	customChangeWallet := mustTestnetAddress(t, 3)
	apiKey := "psbt-rest-key"

	rawTxHex, txID := mustFundingTx(t, payerWallet, 5000)
	mux := http.NewServeMux()
	mux.HandleFunc("/address/"+payerWallet+"/utxo", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"txid":  txID,
				"vout":  0,
				"value": 5000,
				"status": map[string]interface{}{
					"confirmed": true,
				},
			},
		})
	})
	mux.HandleFunc("/tx/"+txID+"/raw", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(rawTxHex))
	})
	mempoolServer := httptest.NewServer(mux)
	defer mempoolServer.Close()
	t.Setenv("MEMPOOL_API_BASE", mempoolServer.URL)

	server := NewServer(store, &mockAPIKeyStore{
		keys: map[string]auth.APIKey{
			apiKey: {Key: apiKey, Wallet: payerWallet},
		},
	}, nil)

	contract := smart_contract.Contract{
		ContractID:      "contract-change-defaults",
		Title:           "Test contract",
		Status:          "open",
		TotalBudgetSats: 1000,
	}
	if err := store.UpsertContractWithTasks(context.Background(), contract, nil); err != nil {
		t.Fatalf("failed to seed contract: %v", err)
	}

	t.Run("defaults to payer wallet", func(t *testing.T) {
		body := `{"contractor_wallet":"` + contractorWallet + `","pixel_hash":"` + strings.Repeat("a", 64) + `","commitment_target":"funding"}`
		req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/contracts/"+contract.ContractID+"/psbt", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", apiKey)
		rec := httptest.NewRecorder()

		server.handleContracts(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var payload struct {
			ChangeAddress string `json:"change_address"`
			Network       string `json:"network"`
			NetworkParams string `json:"network_params"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if payload.ChangeAddress != payerWallet {
			t.Fatalf("expected change address %s, got %s", payerWallet, payload.ChangeAddress)
		}
		if payload.Network != bitcoin.GetCurrentNetwork() {
			t.Fatalf("psbt network=%q want %q", payload.Network, bitcoin.GetCurrentNetwork())
		}
		if payload.NetworkParams != bitcoin.CurrentNetworkParams().Name {
			t.Fatalf("psbt network_params=%q want %q", payload.NetworkParams, bitcoin.CurrentNetworkParams().Name)
		}
	})

	t.Run("uses custom change wallet", func(t *testing.T) {
		body := `{"contractor_wallet":"` + contractorWallet + `","pixel_hash":"` + strings.Repeat("b", 64) + `","commitment_target":"funding","change_address":"` + customChangeWallet + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/contracts/"+contract.ContractID+"/psbt", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", apiKey)
		rec := httptest.NewRecorder()

		server.handleContracts(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var payload struct {
			ChangeAddress string `json:"change_address"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if payload.ChangeAddress != customChangeWallet {
			t.Fatalf("expected change address %s, got %s", customChangeWallet, payload.ChangeAddress)
		}
	})
}

// TestContractPSBTProductTargetDefersCommitment verifies that commitment_target="product"
// produces a valid PSBT with no commitment output (commitmentSats forced to 0),
// deferring the commitment to delivery time when the product image is available.
func TestContractPSBTProductTargetDefersCommitment(t *testing.T) {
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	store := scstore.NewMemoryStore(72 * 60 * 60)
	payerWallet := mustTestnetAddress(t, 1)
	contractorWallet := mustTestnetAddress(t, 2)
	apiKey := "psbt-product-key"

	rawTxHex, txID := mustFundingTx(t, payerWallet, 5000)
	mux := http.NewServeMux()
	mux.HandleFunc("/address/"+payerWallet+"/utxo", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"txid":  txID,
				"vout":  0,
				"value": 5000,
				"status": map[string]interface{}{
					"confirmed": true,
				},
			},
		})
	})
	mux.HandleFunc("/tx/"+txID+"/raw", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(rawTxHex))
	})
	mempoolServer := httptest.NewServer(mux)
	defer mempoolServer.Close()
	t.Setenv("MEMPOOL_API_BASE", mempoolServer.URL)

	server := NewServer(store, &mockAPIKeyStore{
		keys: map[string]auth.APIKey{
			apiKey: {Key: apiKey, Wallet: payerWallet},
		},
	}, nil)

	contract := smart_contract.Contract{
		ContractID:      "contract-product-target",
		Title:           "Product commitment test",
		Status:          "open",
		TotalBudgetSats: 1000,
	}
	if err := store.UpsertContractWithTasks(context.Background(), contract, nil); err != nil {
		t.Fatalf("failed to seed contract: %v", err)
	}

	// Request with commitment_target=product — should succeed and produce no commitment output
	body := `{
		"contractor_wallet":"` + contractorWallet + `",
		"pixel_hash":"` + strings.Repeat("a", 64) + `",
		"commitment_target":"product"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/contracts/"+contract.ContractID+"/psbt", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	rec := httptest.NewRecorder()

	server.handleContracts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// commitment_sats should be 0 — no commitment output in the PSBT
	commitSats, _ := resp["commitment_sats"].(float64)
	if commitSats != 0 {
		t.Errorf("commitment_sats = %v, want 0 for product target", commitSats)
	}

	// commitment_script should be empty — no hashlock output created
	commitScript, _ := resp["commitment_script"].(string)
	if commitScript != "" {
		t.Errorf("commitment_script should be empty for product target, got %q", commitScript)
	}
}

// TestContractPSBTProductTargetStoresSourceOnTask verifies that when a taskID is
// provided with commitment_target="product", the task's MerkleProof.CommitmentSource
// is set to "product".
func TestContractPSBTProductTargetStoresSourceOnTask(t *testing.T) {
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	store := scstore.NewMemoryStore(72 * 60 * 60)
	payerWallet := mustTestnetAddress(t, 1)
	contractorWallet := mustTestnetAddress(t, 2)
	apiKey := "psbt-product-task-key"

	rawTxHex, txID := mustFundingTx(t, payerWallet, 5000)
	mux := http.NewServeMux()
	mux.HandleFunc("/address/"+payerWallet+"/utxo", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"txid":  txID,
				"vout":  0,
				"value": 5000,
				"status": map[string]interface{}{
					"confirmed": true,
				},
			},
		})
	})
	mux.HandleFunc("/tx/"+txID+"/raw", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(rawTxHex))
	})
	mempoolServer := httptest.NewServer(mux)
	defer mempoolServer.Close()
	t.Setenv("MEMPOOL_API_BASE", mempoolServer.URL)

	server := NewServer(store, &mockAPIKeyStore{
		keys: map[string]auth.APIKey{
			apiKey: {Key: apiKey, Wallet: payerWallet},
		},
	}, nil)

	contractID := "contract-product-task"
	taskID := "task-product-source"
	store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID:      contractID,
		Title:           "Product task test",
		Status:          "open",
		TotalBudgetSats: 1000,
	}, []smart_contract.Task{
		{
			TaskID:     taskID,
			ContractID: contractID,
			GoalID:     contractID,
			Title:      "Test task",
			Status:     "available",
			BudgetSats: 1000,
		},
	})

	body := `{
		"contractor_wallet":"` + contractorWallet + `",
		"pixel_hash":"` + strings.Repeat("a", 64) + `",
		"commitment_target":"product",
		"task_id":"` + taskID + `"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/contracts/"+contractID+"/psbt", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	rec := httptest.NewRecorder()

	server.handleContracts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	task, err := store.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.MerkleProof == nil {
		t.Fatal("expected MerkleProof to be set on task")
	}
	if task.MerkleProof.CommitmentSource != "product" {
		t.Errorf("CommitmentSource = %q, want \"product\"", task.MerkleProof.CommitmentSource)
	}
}

// TestContractPSBTRaiseFundPassesDonationAndProductHash verifies handleContractPSBT
// in raise_fund mode forwards DonationAddress + ProductPixelHash into the
// combined builder (64-byte OP_RETURN) and persists funding_txid when SegWit.
func TestContractPSBTRaiseFundPassesDonationAndProductHash(t *testing.T) {
	t.Setenv("BITCOIN_NETWORK", "testnet4")
	store := scstore.NewMemoryStore(72 * 60 * 60)
	apiKey := "psbt-raise-key"
	payerWallet := mustTestnetAddress(t, 1)
	fundraiser := mustTestnetAddress(t, 2)
	contractorA := mustTestnetAddress(t, 3)
	contractorB := mustTestnetAddress(t, 4)
	wishHash := strings.Repeat("ab", 32)
	stegoHash := strings.Repeat("cd", 32)

	rawA, txA := mustFundingTx(t, contractorA, 80_000)
	rawB, txB := mustFundingTx(t, contractorB, 90_000)
	mux := http.NewServeMux()
	mux.HandleFunc("/address/"+contractorA+"/utxo", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"txid": txA, "vout": 0, "value": 80_000, "status": map[string]interface{}{"confirmed": true}},
		})
	})
	mux.HandleFunc("/address/"+contractorB+"/utxo", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"txid": txB, "vout": 0, "value": 90_000, "status": map[string]interface{}{"confirmed": true}},
		})
	})
	mux.HandleFunc("/tx/"+txA+"/raw", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(rawA)) })
	mux.HandleFunc("/tx/"+txB+"/raw", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(rawB)) })
	mempoolServer := httptest.NewServer(mux)
	defer mempoolServer.Close()
	t.Setenv("MEMPOOL_API_BASE", mempoolServer.URL)

	ingest, err := services.NewIngestionService(filepath.Join(t.TempDir(), "raise-psbt.db"))
	if err != nil {
		t.Fatalf("ingestion: %v", err)
	}
	contractID := "contract-raise-opreturn"
	if err := ingest.Create(services.IngestionRecord{
		ID:          contractID,
		Filename:    wishHash + ".png",
		Method:      "alpha",
		ImageBase64: "ZmFrZQ==",
		Metadata:    map[string]interface{}{"visible_pixel_hash": wishHash},
		Status:      "pending",
	}); err != nil {
		t.Fatalf("seed ingestion: %v", err)
	}

	server := NewServer(store, &mockAPIKeyStore{
		keys: map[string]auth.APIKey{apiKey: {Key: apiKey, Wallet: payerWallet}},
	}, ingest)

	if err := store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID:      contractID,
		Title:           "raise fund for community art",
		Status:          "open",
		TotalBudgetSats: 30_000,
	}, []smart_contract.Task{
		{TaskID: "rf-a", ContractID: contractID, Title: "A", BudgetSats: 10_000, Status: "available", ContractorWallet: contractorA},
		{TaskID: "rf-b", ContractID: contractID, Title: "B", BudgetSats: 20_000, Status: "available", ContractorWallet: contractorB},
	}); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if err := store.CreateProposal(context.Background(), smart_contract.Proposal{
		ID:               contractID,
		Title:            "Please raise fund",
		DescriptionMD:    "raise fund",
		VisiblePixelHash: wishHash,
		BudgetSats:       30_000,
		Status:           "approved",
		Metadata: map[string]interface{}{
			"funding_mode":       "raise_fund",
			"funding_address":    fundraiser,
			"visible_pixel_hash": wishHash,
		},
	}); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}

	body := `{
		"pixel_hash":"` + wishHash + `",
		"product_pixel_hash":"` + stegoHash + `",
		"commitment_target":"funding",
		"fee_rate_sats_vb":1
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/contracts/"+contractID+"/psbt", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	rec := httptest.NewRecorder()
	server.handleContracts(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		FundingMode    string `json:"funding_mode"`
		SplitPSBT      bool   `json:"split_psbt"`
		OPReturnScript string `json:"op_return_script"`
		DonationAddr   string `json:"donation_address"`
		FundingTxID    string `json:"funding_txid"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.FundingMode != "raise_fund" {
		t.Fatalf("funding_mode=%q", payload.FundingMode)
	}
	if payload.SplitPSBT {
		t.Fatal("combined raise must not take the split path")
	}
	script, err := hex.DecodeString(payload.OPReturnScript)
	if err != nil || len(script) == 0 {
		t.Fatalf("missing op_return_script: %v %q", err, payload.OPReturnScript)
	}
	if script[0] != txscript.OP_RETURN {
		t.Fatalf("expected OP_RETURN, got 0x%02x", script[0])
	}
	if !bytes.Contains(script, mustDecodeHex(t, wishHash)) || !bytes.Contains(script, mustDecodeHex(t, stegoHash)) {
		t.Fatal("handler did not pass wish+stego hashes into the raise-fund builder")
	}
	if payload.DonationAddr != fundraiser {
		t.Fatalf("donation_address=%q want fundraiser %s", payload.DonationAddr, fundraiser)
	}
	if payload.FundingTxID == "" {
		t.Fatal("all-SegWit raise-fund should persist a precomputed funding_txid")
	}
	stored, err := ingest.Get(contractID)
	if err != nil {
		t.Fatalf("get ingestion: %v", err)
	}
	if stored.Metadata["funding_txid"] != payload.FundingTxID {
		t.Fatalf("ingestion funding_txid=%v want %s", stored.Metadata["funding_txid"], payload.FundingTxID)
	}
}

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	return b
}

func TestPaymentDetailsAndPSBTUseConfiguredNetwork(t *testing.T) {
	store := scstore.NewMemoryStore(72 * 60 * 60)
	payerWallet := mustTestnetAddress(t, 1)
	contractorWallet := mustTestnetAddress(t, 2)
	apiKey := "payment-net-key"
	server := NewServer(store, &mockAPIKeyStore{
		keys: map[string]auth.APIKey{
			apiKey: {Key: apiKey, Wallet: payerWallet},
		},
	}, nil)

	contractID := "contract-payment-network"
	if err := store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID:      contractID,
		Title:           "Network alignment",
		Status:          "approved",
		TotalBudgetSats: 1500,
	}, []smart_contract.Task{{
		TaskID:           "task-payment-network",
		ContractID:       contractID,
		Title:            "Pay",
		BudgetSats:       1500,
		Status:           "approved",
		ContractorWallet: contractorWallet,
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, network := range []string{"testnet4", "signet", "mainnet", "testnet"} {
		t.Run("payment-details/"+network, func(t *testing.T) {
			t.Setenv("BITCOIN_NETWORK", network)
			req := httptest.NewRequest(http.MethodGet, "/api/smart_contract/contracts/"+contractID+"/payment-details", nil)
			req.Header.Set("X-API-Key", apiKey)
			rec := httptest.NewRecorder()
			server.handleContracts(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
			}
			var payload struct {
				Network       string `json:"network"`
				NetworkParams string `json:"network_params"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if payload.Network != network {
				t.Fatalf("payment JSON network=%q want %q (hardcoded testnet is a fund-loss class bug)", payload.Network, network)
			}
			wantParams := bitcoin.NetworkParams(network).Name
			if payload.NetworkParams != wantParams {
				t.Fatalf("network_params=%q want %q", payload.NetworkParams, wantParams)
			}
		})
	}

	t.Run("default is testnet4 not testnet", func(t *testing.T) {
		t.Setenv("BITCOIN_NETWORK", "")
		req := httptest.NewRequest(http.MethodGet, "/api/smart_contract/contracts/"+contractID+"/payment-details", nil)
		req.Header.Set("X-API-Key", apiKey)
		rec := httptest.NewRecorder()
		server.handleContracts(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var payload struct {
			Network string `json:"network"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if payload.Network != "testnet4" {
			t.Fatalf("default network=%q want testnet4", payload.Network)
		}
	})

	t.Run("psbt builder rejects testnet wallet on mainnet", func(t *testing.T) {
		t.Setenv("BITCOIN_NETWORK", "mainnet")
		body := `{"contractor_wallet":"` + contractorWallet + `","pixel_hash":"` + strings.Repeat("a", 64) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/smart_contract/contracts/"+contractID+"/psbt", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", apiKey)
		rec := httptest.NewRecorder()
		server.handleContracts(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on mainnet+testnet wallet, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "invalid payer wallet") {
			t.Fatalf("expected invalid payer wallet, got: %s", rec.Body.String())
		}
	})

	t.Run("chain backend network wins over env", func(t *testing.T) {
		t.Setenv("BITCOIN_NETWORK", "testnet4")
		server.mempool = &networkStubChain{network: "signet"}
		t.Cleanup(func() { server.mempool = &bitcoin.MempoolClient{} })
		req := httptest.NewRequest(http.MethodGet, "/api/smart_contract/contracts/"+contractID+"/payment-details", nil)
		req.Header.Set("X-API-Key", apiKey)
		rec := httptest.NewRecorder()
		server.handleContracts(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var payload struct {
			Network       string `json:"network"`
			NetworkParams string `json:"network_params"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if payload.Network != "signet" || payload.NetworkParams != chaincfg.SigNetParams.Name {
			t.Fatalf("backend network not used: %+v", payload)
		}
	})
}

type networkStubChain struct {
	network string
}

func (s *networkStubChain) Network() string                      { return s.network }
func (s *networkStubChain) Ready(context.Context) error          { return nil }
func (s *networkStubChain) Synced(context.Context) (bool, error) { return true, nil }
func (s *networkStubChain) GetTipHeight(context.Context) (int64, error) {
	return 0, nil
}
func (s *networkStubChain) GetBlockHash(context.Context, int64) (string, error) {
	return "", nil
}
func (s *networkStubChain) GetRawBlockHex(context.Context, int64) (string, error) {
	return "", nil
}
func (s *networkStubChain) GetRawTx(context.Context, string) (*wire.MsgTx, error) {
	return nil, nil
}
func (s *networkStubChain) GetTxStatus(context.Context, string) (int64, bool, error) {
	return 0, false, nil
}
func (s *networkStubChain) NodeStatus(context.Context) (map[string]any, error) {
	return map[string]any{"backend": "stub"}, nil
}
func (s *networkStubChain) Close() error { return nil }
func (s *networkStubChain) ListConfirmedUTXOs(string) ([]bitcoin.AddressUTXO, error) {
	return nil, nil
}
func (s *networkStubChain) FetchTx(string) (*wire.MsgTx, error) { return nil, nil }
func (s *networkStubChain) FetchTxOutput(string, uint32) (*wire.MsgTx, *wire.TxOut, error) {
	return nil, nil, nil
}
func (s *networkStubChain) BroadcastTx(string) (string, error) { return "", nil }

func mustTestnetAddress(t *testing.T, fill byte) string {
	t.Helper()
	hash := bytes.Repeat([]byte{fill}, 20)
	addr, err := btcutil.NewAddressWitnessPubKeyHash(hash, &chaincfg.TestNet4Params)
	if err != nil {
		t.Fatalf("failed to build address: %v", err)
	}
	return addr.EncodeAddress()
}

func mustFundingTx(t *testing.T, payoutAddress string, value int64) (string, string) {
	t.Helper()
	addr, err := btcutil.DecodeAddress(payoutAddress, &chaincfg.TestNet4Params)
	if err != nil {
		t.Fatalf("failed to decode payout address: %v", err)
	}
	script, err := txscript.PayToAddrScript(addr)
	if err != nil {
		t.Fatalf("failed to build payout script: %v", err)
	}
	tx := wire.NewMsgTx(2)
	prevHash := chainhash.Hash{}
	tx.AddTxIn(wire.NewTxIn(wire.NewOutPoint(&prevHash, 0xffffffff), nil, nil))
	tx.AddTxOut(wire.NewTxOut(value, script))
	var buf bytes.Buffer
	if err := tx.Serialize(&buf); err != nil {
		t.Fatalf("failed to serialize tx: %v", err)
	}
	return hex.EncodeToString(buf.Bytes()), tx.TxHash().String()
}
