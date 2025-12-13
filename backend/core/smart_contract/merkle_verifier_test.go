package smart_contract

import (
	"testing"
)

func TestNewMerkleProofVerifier(t *testing.T) {
	// Test basic creation
	verifier := NewMerkleProofVerifier("mainnet")
	if verifier == nil {
		t.Fatal("NewMerkleProofVerifier() returned nil")
	}
}

func TestVerifyProof(t *testing.T) {
	verifier := NewMerkleProofVerifier("mainnet")

	// Test valid proof
	validProof := &MerkleProof{
		TxID:                  "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
		BlockHeight:           170000,
		BlockHeaderMerkleRoot: "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b",
		ProofPath: []ProofNode{
			{Hash: "8b8a2e6d3e9c1b2a3f4e5d6c7b8a9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6", Direction: "left"},
			{Hash: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6", Direction: "right"},
		},
		VisiblePixelHash:   "abc123def456",
		FundedAmountSats:   100000,
		FundingAddress:     "test-address",
		ConfirmationStatus: "confirmed",
	}

	result, err := verifier.VerifyProof(validProof)
	if err != nil {
		t.Errorf("VerifyProof() unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("VerifyProof() expected valid result but got invalid")
	}

	// Test nil proof
	result, err = verifier.VerifyProof(nil)
	if err != nil {
		t.Errorf("VerifyProof() unexpected error for nil proof: %v", err)
	}
	if result.Valid {
		t.Error("Expected invalid result for nil proof")
	}

	// Test invalid proof - empty TX ID
	invalidProof := &MerkleProof{
		TxID:        "",
		BlockHeight: 170000,
	}

	result, err = verifier.VerifyProof(invalidProof)
	if err != nil {
		t.Errorf("VerifyProof() unexpected error for empty TX ID: %v", err)
	}
	if result.Valid {
		t.Error("Expected invalid result for empty TX ID")
	}

	// Test invalid proof - negative block height
	invalidProof2 := &MerkleProof{
		TxID:        "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
		BlockHeight: -1,
	}

	result, err = verifier.VerifyProof(invalidProof2)
	if err != nil {
		t.Errorf("VerifyProof() unexpected error for negative block height: %v", err)
	}
	if result.Valid {
		t.Error("Expected invalid result for negative block height")
	}

	// Test invalid proof - empty proof path
	invalidProof3 := &MerkleProof{
		TxID:        "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
		BlockHeight: 170000,
		ProofPath:   []ProofNode{},
	}

	result, err = verifier.VerifyProof(invalidProof3)
	if err != nil {
		t.Errorf("VerifyProof() unexpected error for empty proof path: %v", err)
	}
	if result.Valid {
		t.Error("Expected invalid result for empty proof path")
	}
}

func TestBatchVerifyProofs(t *testing.T) {
	verifier := NewMerkleProofVerifier("mainnet")

	// Test empty batch
	results, err := verifier.VerifyBatchProofs([]*MerkleProof{})
	if err != nil {
		t.Errorf("VerifyBatchProofs() unexpected error for empty batch: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("VerifyBatchProofs() expected empty results for empty batch but got %d", len(results))
	}

	// Test single proof batch
	singleProof := &MerkleProof{
		TxID:                  "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
		BlockHeight:           170000,
		BlockHeaderMerkleRoot: "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b",
		ProofPath: []ProofNode{
			{Hash: "8b8a2e6d3e9c1b2a3f4e5d6c7b8a9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6", Direction: "left"},
			{Hash: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6", Direction: "right"},
		},
		VisiblePixelHash:   "abc123def456",
		FundedAmountSats:   100000,
		FundingAddress:     "test-address",
		ConfirmationStatus: "confirmed",
	}

	batchResults, err := verifier.VerifyBatchProofs([]*MerkleProof{singleProof})
	if err != nil {
		t.Errorf("VerifyBatchProofs() unexpected error: %v", err)
	}
	if len(batchResults) != 1 {
		t.Errorf("VerifyBatchProofs() expected 1 result but got %d", len(batchResults))
	}

	// Test mixed valid/invalid batch
	invalidProof := &MerkleProof{
		TxID: "", // Invalid
	}

	batchResults, err = verifier.VerifyBatchProofs([]*MerkleProof{singleProof, invalidProof})
	if err != nil {
		t.Errorf("VerifyBatchProofs() unexpected error: %v", err)
	}
	if len(batchResults) != 2 {
		t.Errorf("VerifyBatchProofs() expected 2 results but got %d", len(batchResults))
	}
}

func TestGetProofStatus(t *testing.T) {
	verifier := NewMerkleProofVerifier("mainnet")

	proof := &MerkleProof{
		TxID:                  "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
		BlockHeight:           170000,
		BlockHeaderMerkleRoot: "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b",
		ProofPath: []ProofNode{
			{Hash: "8b8a2e6d3e9c1b2a3f4e5d6c7b8a9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6", Direction: "left"},
			{Hash: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6", Direction: "right"},
		},
		VisiblePixelHash:   "abc123def456",
		FundedAmountSats:   100000,
		FundingAddress:     "test-address",
		ConfirmationStatus: "confirmed",
	}

	status, err := verifier.GetProofStatus(proof)
	if err != nil {
		t.Errorf("GetProofStatus() unexpected error: %v", err)
	}
	if status == nil {
		t.Error("GetProofStatus() returned nil status")
	}

	// Check required fields
	if status["tx_id"] != proof.TxID {
		t.Errorf("GetProofStatus() expected tx_id %s but got %s", proof.TxID, status["tx_id"])
	}
	if status["block_height"] != float64(proof.BlockHeight) {
		t.Errorf("GetProofStatus() expected block_height %d but got %v", proof.BlockHeight, status["block_height"])
	}
	if status["confirmation_status"] != proof.ConfirmationStatus {
		t.Errorf("GetProofStatus() expected confirmation_status %s but got %s", proof.ConfirmationStatus, status["confirmation_status"])
	}
}

func TestValidateProofChain(t *testing.T) {
	verifier := NewMerkleProofVerifier("mainnet")

	// Test empty chain
	result, err := verifier.ValidateProofChain([]*MerkleProof{})
	if err != nil {
		t.Errorf("ValidateProofChain() unexpected error for empty chain: %v", err)
	}
	if result.Valid {
		t.Errorf("ValidateProofChain() expected invalid result for empty chain but got valid")
	}

	// Test single proof chain
	singleProof := &MerkleProof{
		TxID:                  "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
		BlockHeight:           170000,
		BlockHeaderMerkleRoot: "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b",
		ProofPath: []ProofNode{
			{Hash: "8b8a2e6d3e9c1b2a3f4e5d6c7b8a9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6", Direction: "left"},
			{Hash: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6", Direction: "right"},
		},
		VisiblePixelHash:   "abc123def456",
		FundedAmountSats:   100000,
		FundingAddress:     "test-address",
		ConfirmationStatus: "confirmed",
	}

	chainResult, err := verifier.ValidateProofChain([]*MerkleProof{singleProof})
	if err != nil {
		t.Errorf("ValidateProofChain() unexpected error: %v", err)
	}
	if !chainResult.Valid {
		t.Errorf("ValidateProofChain() expected valid result but got invalid: %s", chainResult.Error)
	}
}

// Edge case tests
func TestMerkleProofVerifierEdgeCases(t *testing.T) {
	verifier := NewMerkleProofVerifier("mainnet")

	t.Run("Nil proof verification", func(t *testing.T) {
		result, err := verifier.VerifyProof(nil)
		if err != nil {
			t.Errorf("VerifyProof() unexpected error for nil proof: %v", err)
		}
		if result.Valid {
			t.Error("Expected invalid result for nil proof")
		}
	})

	t.Run("Empty proof path", func(t *testing.T) {
		proof := &MerkleProof{
			TxID:      "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
			ProofPath: []ProofNode{}, // Empty path
		}

		result, err := verifier.VerifyProof(proof)
		if err != nil {
			t.Logf("Expected error for empty proof path: %v", err)
		}
		if result.Valid {
			t.Error("Expected invalid result for empty proof path")
		}
	})

	t.Run("Extremely large proof", func(t *testing.T) {
		// Test with extremely large proof path
		largePath := make([]ProofNode, 1000)
		for i := range largePath {
			largePath[i] = ProofNode{
				Hash:      string(make([]byte, 64)), // 64-byte hash
				Direction: "left",
			}
		}

		proof := &MerkleProof{
			TxID:        "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
			BlockHeight: 170000,
			ProofPath:   largePath,
		}

		// Should handle gracefully without panicking
		result, err := verifier.VerifyProof(proof)
		if err != nil {
			t.Logf("Expected error for extremely large proof: %v", err)
		}
		_ = result // Result may be valid or invalid depending on implementation
	})

	t.Run("Invalid characters in hash", func(t *testing.T) {
		proof := &MerkleProof{
			TxID: "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
			ProofPath: []ProofNode{
				{Hash: "invalid-hash-with-special-chars-!@#$%", Direction: "left"},
			},
		}

		result, err := verifier.VerifyProof(proof)
		if err != nil {
			t.Logf("Expected error for invalid hash characters: %v", err)
		}
		if result.Valid {
			t.Error("Expected invalid result for invalid hash characters")
		}
	})
}
