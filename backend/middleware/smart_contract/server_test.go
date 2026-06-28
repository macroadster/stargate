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

	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
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
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if payload.ChangeAddress != payerWallet {
			t.Fatalf("expected change address %s, got %s", payerWallet, payload.ChangeAddress)
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
