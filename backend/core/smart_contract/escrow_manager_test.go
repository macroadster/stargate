package smart_contract

import (
	"context"
	"testing"
	"time"
)

func TestNewEscrowManager(t *testing.T) {
	scriptInterpreter := NewScriptInterpreter()
	verifier := NewMerkleProofVerifier("mainnet")

	manager := NewEscrowManager(scriptInterpreter, verifier, "mainnet")
	if manager == nil {
		t.Fatal("NewEscrowManager() returned nil")
	}

	if manager.scriptInterpreter == nil {
		t.Error("Expected scriptInterpreter to be set")
	}
	if manager.verifier == nil {
		t.Error("Expected verifier to be set")
	}
	if manager.httpClient == nil {
		t.Error("Expected httpClient to be set")
	}
	if manager.bitcoinRPC != "mainnet" {
		t.Errorf("Expected bitcoinRPC to be 'mainnet' but got '%s'", manager.bitcoinRPC)
	}
}

func TestCreateEscrow(t *testing.T) {
	scriptInterpreter := NewScriptInterpreter()
	verifier := NewMerkleProofVerifier("mainnet")
	manager := NewEscrowManager(scriptInterpreter, verifier, "mainnet")

	ctx := context.Background()

	t.Run("Valid multisig escrow", func(t *testing.T) {
		config := EscrowConfig{
			ContractID:      "test-escrow-1",
			TotalBudgetSats: 100000,
			Participants: []EscrowParticipant{
				{Name: "Alice", PublicKey: "03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2", Role: "creator", SharePercent: 50},
				{Name: "Bob", PublicKey: "03b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3", Role: "worker", SharePercent: 40},
				{Name: "Charlie", PublicKey: "03c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4", Role: "arbitrator", SharePercent: 10},
			},
			RequiredSigs: 2,
			LockTime:     0,
			ContractType: "multisig",
		}

		contract, err := manager.CreateEscrow(ctx, config)

		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		if contract == nil {
			t.Fatal("Expected contract but got nil")
		}

		if contract.ContractID != config.ContractID {
			t.Errorf("Expected contract ID '%s' but got '%s'", config.ContractID, contract.ContractID)
		}

		if contract.TotalBudgetSats != config.TotalBudgetSats {
			t.Errorf("Expected budget %d but got %d", config.TotalBudgetSats, contract.TotalBudgetSats)
		}

		if contract.ContractType != config.ContractType {
			t.Errorf("Expected contract type '%s' but got '%s'", config.ContractType, contract.ContractType)
		}

		if contract.Status != "created" {
			t.Errorf("Expected status 'created' but got '%s'", contract.Status)
		}

		if contract.ScriptHex == "" {
			t.Error("Expected script hex to be set")
		}

		if contract.Address == "" {
			t.Error("Expected address to be set")
		}

		if len(contract.Participants) != 3 {
			t.Errorf("Expected 3 participants but got %d", len(contract.Participants))
		}
	})

	t.Run("Valid timelock escrow", func(t *testing.T) {
		config := EscrowConfig{
			ContractID:      "test-escrow-2",
			TotalBudgetSats: 50000,
			Participants: []EscrowParticipant{
				{Name: "Alice", PublicKey: "03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2", Role: "creator", SharePercent: 100},
			},
			RequiredSigs: 1,
			LockTime:     850000,
			ContractType: "timelock",
		}

		contract, err := manager.CreateEscrow(ctx, config)

		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		if contract == nil {
			t.Fatal("Expected contract but got nil")
		}

		if contract.ContractType != "timelock" {
			t.Errorf("Expected contract type 'timelock' but got '%s'", contract.ContractType)
		}

		if contract.LockTime != 850000 {
			t.Errorf("Expected lock time 850000 but got %d", contract.LockTime)
		}
	})

	t.Run("Valid taproot escrow", func(t *testing.T) {
		config := EscrowConfig{
			ContractID:      "test-escrow-3",
			TotalBudgetSats: 75000,
			Participants: []EscrowParticipant{
				{Name: "Alice", PublicKey: "03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2", Role: "creator", SharePercent: 100},
			},
			RequiredSigs: 1,
			LockTime:     0,
			ContractType: "taproot",
		}

		contract, err := manager.CreateEscrow(ctx, config)

		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		if contract == nil {
			t.Fatal("Expected contract but got nil")
		}

		if contract.ContractType != "taproot" {
			t.Errorf("Expected contract type 'taproot' but got '%s'", contract.ContractType)
		}
	})

	t.Run("Invalid contract type", func(t *testing.T) {
		config := EscrowConfig{
			ContractID:      "test-escrow-4",
			TotalBudgetSats: 100000,
			Participants: []EscrowParticipant{
				{Name: "Alice", PublicKey: "03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2", Role: "creator", SharePercent: 100},
			},
			RequiredSigs: 1,
			LockTime:     0,
			ContractType: "invalid",
		}

		contract, err := manager.CreateEscrow(ctx, config)

		if err == nil {
			t.Error("Expected error for invalid contract type")
		}

		if contract != nil {
			t.Error("Expected nil contract for invalid contract type")
		}
	})

	t.Run("Multisig with wrong participant count", func(t *testing.T) {
		config := EscrowConfig{
			ContractID:      "test-escrow-5",
			TotalBudgetSats: 100000,
			Participants: []EscrowParticipant{
				{Name: "Alice", PublicKey: "03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2", Role: "creator", SharePercent: 100},
			},
			RequiredSigs: 1,
			LockTime:     0,
			ContractType: "multisig",
		}

		contract, err := manager.CreateEscrow(ctx, config)

		if err == nil {
			t.Error("Expected error for multisig with wrong participant count")
		}

		if contract != nil {
			t.Error("Expected nil contract for multisig with wrong participant count")
		}
	})
}

func TestFundEscrow(t *testing.T) {
	scriptInterpreter := NewScriptInterpreter()
	verifier := NewMerkleProofVerifier("mainnet")
	manager := NewEscrowManager(scriptInterpreter, verifier, "mainnet")

	ctx := context.Background()

	// Create a test contract first
	config := EscrowConfig{
		ContractID:      "test-escrow-fund",
		TotalBudgetSats: 100000,
		Participants: []EscrowParticipant{
			{Name: "Alice", PublicKey: "03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2", Role: "creator", SharePercent: 100},
		},
		RequiredSigs: 1,
		LockTime:     0,
		ContractType: "timelock",
	}

	contract, err := manager.CreateEscrow(ctx, config)
	if err != nil {
		t.Fatalf("Failed to create test contract: %v", err)
	}

	t.Run("Valid funding transaction", func(t *testing.T) {
		fundingTxHex := "0100000001a15d5709aa7ff7987d5a5ea2cbbf06b2f1f1d421a1c7a6c6c6c6c6c6c6c6c6c0100000000ffffffff0180969800000000001976a91462e907b15cbf27d5425399ebf6f0fb50ebb88f1888ac00000000"

		tx, err := manager.FundEscrow(ctx, contract, fundingTxHex)

		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		if tx == nil {
			t.Fatal("Expected transaction but got nil")
		}

		if tx.Type != "funding" {
			t.Errorf("Expected transaction type 'funding' but got '%s'", tx.Type)
		}

		if tx.AmountSats != contract.TotalBudgetSats {
			t.Errorf("Expected amount %d but got %d", contract.TotalBudgetSats, tx.AmountSats)
		}

		if tx.ToAddress != contract.Address {
			t.Errorf("Expected to address '%s' but got '%s'", contract.Address, tx.ToAddress)
		}

		if tx.Status != "pending" {
			t.Errorf("Expected status 'pending' but got '%s'", tx.Status)
		}

		if tx.TxID == "" {
			t.Error("Expected transaction ID to be set")
		}
	})

	t.Run("Empty funding transaction", func(t *testing.T) {
		tx, err := manager.FundEscrow(ctx, contract, "")

		if err == nil {
			t.Error("Expected error for empty funding transaction")
		}

		if tx != nil {
			t.Error("Expected nil transaction for empty funding transaction")
		}
	})
}

func TestClaimEscrow(t *testing.T) {
	scriptInterpreter := NewScriptInterpreter()
	verifier := NewMerkleProofVerifier("mainnet")
	manager := NewEscrowManager(scriptInterpreter, verifier, "mainnet")

	ctx := context.Background()

	// Create and fund a test contract
	config := EscrowConfig{
		ContractID:      "test-escrow-claim",
		TotalBudgetSats: 100000,
		Participants: []EscrowParticipant{
			{Name: "Alice", PublicKey: "03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2", Role: "creator", SharePercent: 100},
		},
		RequiredSigs: 1,
		LockTime:     0,
		ContractType: "timelock",
	}

	contract, err := manager.CreateEscrow(ctx, config)
	if err != nil {
		t.Fatalf("Failed to create test contract: %v", err)
	}

	// Simulate funded contract
	contract.Status = "funded"
	now := time.Now()
	contract.FundedAt = &now

	t.Run("Valid claim", func(t *testing.T) {
		// Use multisig contract type which is supported by script interpreter
		multisigConfig := EscrowConfig{
			ContractID:      "test-escrow-multisig",
			TotalBudgetSats: 100000,
			Participants: []EscrowParticipant{
				{Name: "Alice", PublicKey: "03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2", Role: "creator", SharePercent: 34},
				{Name: "Bob", PublicKey: "03b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3", Role: "worker", SharePercent: 33},
				{Name: "Charlie", PublicKey: "03c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4", Role: "arbitrator", SharePercent: 33},
			},
			RequiredSigs: 2,
			LockTime:     0,
			ContractType: "multisig",
		}

		multisigContract, err := manager.CreateEscrow(ctx, multisigConfig)
		if err != nil {
			t.Fatalf("Failed to create multisig contract: %v", err)
		}

		// Simulate funded contract
		multisigContract.Status = "funded"
		now := time.Now()
		multisigContract.FundedAt = &now

		signatures := []string{
			"304402207fa7a6d1e0ee81132a269ad84e68d695483745cde8b541e3bf630749894e342a022030c55193580c486495d3536a4122e742b062da727f1185654d03bdc656bfc822",
			"304402207fa7a6d1e0ee81132a269ad84e68d695483745cde8b541e3bf630749894e342a022030c55193580c486495d3536a4122e742b062da727f1185654d03bdc656bfc823",
		}

		tx, err := manager.ClaimEscrow(ctx, multisigContract, multisigConfig.Participants[0].PublicKey, signatures)

		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		if tx == nil {
			t.Fatal("Expected transaction but got nil")
		}

		if tx.Type != "claim" {
			t.Errorf("Expected transaction type 'claim' but got '%s'", tx.Type)
		}
	})

	t.Run("Claim on unfunded contract", func(t *testing.T) {
		unfundedContract := *contract
		unfundedContract.Status = "created"

		signatures := []string{
			"304402207fa7a6d1e0ee81132a269ad84e68d695483745cde8b541e3bf630749894e342a022030c55193580c486495d3536a4122e742b062da727f1185654d03bdc656bfc822",
		}

		tx, err := manager.ClaimEscrow(ctx, &unfundedContract, config.Participants[0].PublicKey, signatures)

		if err == nil {
			t.Error("Expected error for claim on unfunded contract")
		}

		if tx != nil {
			t.Error("Expected nil transaction for claim on unfunded contract")
		}
	})
}

func TestEscrowManagerEdgeCases(t *testing.T) {
	scriptInterpreter := NewScriptInterpreter()
	verifier := NewMerkleProofVerifier("mainnet")
	manager := NewEscrowManager(scriptInterpreter, verifier, "mainnet")

	ctx := context.Background()

	t.Run("Nil context", func(t *testing.T) {
		config := EscrowConfig{
			ContractID:      "test-escrow-nil-ctx",
			TotalBudgetSats: 100000,
			Participants: []EscrowParticipant{
				{Name: "Alice", PublicKey: "03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2", Role: "creator", SharePercent: 100},
			},
			RequiredSigs: 1,
			LockTime:     0,
			ContractType: "timelock",
		}

		// Should handle nil context gracefully
		contract, err := manager.CreateEscrow(nil, config)

		if err != nil {
			t.Errorf("Unexpected error with nil context: %v", err)
		}

		if contract == nil {
			t.Error("Expected contract even with nil context")
		}
	})

	t.Run("Invalid public key", func(t *testing.T) {
		config := EscrowConfig{
			ContractID:      "test-escrow-invalid-pubkey",
			TotalBudgetSats: 100000,
			Participants: []EscrowParticipant{
				{Name: "Alice", PublicKey: "invalid-pubkey", Role: "creator", SharePercent: 100},
			},
			RequiredSigs: 1,
			LockTime:     0,
			ContractType: "timelock",
		}

		contract, err := manager.CreateEscrow(ctx, config)

		if err == nil {
			t.Error("Expected error for invalid public key")
		}

		if contract != nil {
			t.Error("Expected nil contract for invalid public key")
		}
	})

	t.Run("Zero budget", func(t *testing.T) {
		config := EscrowConfig{
			ContractID:      "test-escrow-zero-budget",
			TotalBudgetSats: 0,
			Participants: []EscrowParticipant{
				{Name: "Alice", PublicKey: "03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2", Role: "creator", SharePercent: 100},
			},
			RequiredSigs: 1,
			LockTime:     0,
			ContractType: "timelock",
		}

		contract, err := manager.CreateEscrow(ctx, config)

		// Contract creation should succeed, but funding should fail
		if err != nil {
			t.Errorf("Unexpected error for zero budget contract creation: %v", err)
		}

		if contract == nil {
			t.Fatal("Expected contract for zero budget")
		}

		// Funding should fail
		fundingTxHex := "0100000001a15d5709aa7ff7987d5a5ea2cbbf06b2f1f1d421a1c7a6c6c6c6c6c6c6c6c0100000000ffffffff0180969800000000001976a91462e907b15cbf27d5425399ebf6f0fb50ebb88f1888ac00000000"
		tx, err := manager.FundEscrow(ctx, contract, fundingTxHex)

		if err == nil {
			t.Error("Expected error for funding zero budget contract")
		}

		if tx != nil {
			t.Error("Expected nil transaction for funding zero budget contract")
		}
	})
}
