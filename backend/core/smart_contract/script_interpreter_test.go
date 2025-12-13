package smart_contract

import (
	"testing"
)

func TestNewScriptInterpreter(t *testing.T) {
	interpreter := NewScriptInterpreter()
	if interpreter == nil {
		t.Fatal("NewScriptInterpreter() returned nil")
	}
}

func TestValidateP2PKH(t *testing.T) {
	interpreter := NewScriptInterpreter()

	// Test valid P2PKH script
	result, err := interpreter.ValidateP2PKH(
		"76a914abc123def456789012345678901234567890123488ac",
		"304402207fa7a6d1e0ee81132a269ad84e68d695483745cde8b541e3bf630749894e342a022030c55193580c486495d3536a4122e742b062da727f1185654d03bdc656bfc822",
		"03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2",
	)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("Expected valid result but got invalid: %s", result.Error)
	}
	if result.ScriptType != "p2pkh" {
		t.Errorf("Expected script type 'p2pkh' but got '%s'", result.ScriptType)
	}

	// Test invalid script (missing opcodes)
	result, err = interpreter.ValidateP2PKH(
		"0014abc123def4567890123456789012345678901234",
		"304402207fa7a6d1e0ee81132a269ad84e68d695483745cde8b541e3bf630749894e342a022030c55193580c486495d3536a4122e742b062da727f1185654d03bdc656bfc822",
		"03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2",
	)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("Expected invalid result but got valid")
	}
}

func TestValidateMultisigEscrow(t *testing.T) {
	interpreter := NewScriptInterpreter()

	// Test valid 2-of-3 multisig
	result, err := interpreter.ValidateMultisigEscrow(
		"522103a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b22103b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c32103c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d453ae",
		[]string{
			"304402207fa7a6d1e0ee81132a269ad84e68d695483745cde8b541e3bf630749894e342a022030c55193580c486495d3536a4122e742b062da727f1185654d03bdc656bfc822",
			"304402207fa7a6d1e0ee81132a269ad84e68d695483745cde8b541e3bf630749894e342a022030c55193580c486495d3536a4122e742b062da727f1185654d03bdc656bfc823",
		},
		[]string{
			"03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2",
			"03b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3",
			"03c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4",
		},
	)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("Expected valid result but got invalid: %s", result.Error)
	}
	if result.ScriptType != "multisig" {
		t.Errorf("Expected script type 'multisig' but got '%s'", result.ScriptType)
	}
	if result.RequiredSignatures != 2 {
		t.Errorf("Expected required signatures 2 but got %d", result.RequiredSignatures)
	}

	// Test insufficient signatures
	result, err = interpreter.ValidateMultisigEscrow(
		"522103a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b22103b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c32103c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d453ae",
		[]string{
			"304402207fa7a6d1e0ee81132a269ad84e68d695483745cde8b541e3bf630749894e342a022030c55193580c486495d3536a4122e742b062da727f1185654d03bdc656bfc822",
		},
		[]string{
			"03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2",
			"03b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3",
			"03c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4",
		},
	)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("Expected invalid result due to insufficient signatures")
	}
}

func TestValidateTimeLock(t *testing.T) {
	interpreter := NewScriptInterpreter()

	// Test valid timelock (expired) - use simple hex
	result, err := interpreter.ValidateTimeLock("b100000090", 150) // b1 = CLTV, 00000090 = lock time 144

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("Expected valid result but got invalid: %s", result.Error)
	}
	if result.ScriptType != "timelock" {
		t.Errorf("Expected script type 'timelock' but got '%s'", result.ScriptType)
	}
	if result.Timelock != 144 {
		t.Errorf("Expected timelock 144 but got %d", result.Timelock)
	}

	// Test timelock not yet reached
	result, err = interpreter.ValidateTimeLock("b100000090", 100) // Current height 100 < lock 144

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("Expected invalid result due to timelock not reached")
	}
}

func TestValidateTaproot(t *testing.T) {
	interpreter := NewScriptInterpreter()

	// Test valid taproot script
	result, err := interpreter.ValidateTaproot(
		"51abc123def45678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678",
		"304402207fa7a6d1e0ee81132a269ad84e68d695483745cde8b541e3bf630749894e342a022030c55193580c486495d3536a4122e742b062da727f1185654d03bdc656bfc822",
		"21abc123def4567890123456789012345678901234567890123456789012345678901234", // Even length hex (72 chars)
	)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("Expected valid result but got invalid: %s", result.Error)
	}
	if result.ScriptType != "taproot" {
		t.Errorf("Expected script type 'taproot' but got '%s'", result.ScriptType)
	}

	// Test invalid taproot (too short)
	result, err = interpreter.ValidateTaproot(
		"51abc123",
		"304402207fa7a6d1e0ee81132a269ad84e68d695483745cde8b541e3bf630749894e342a022030c55193580c486495d3536a4122e742b062da727f1185654d03bdc656bfc822",
		"21abc123def4567890123456789012345678901234567890123456789012345678901234", // Even length hex (72 chars)
	)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("Expected invalid result due to short script")
	}
}

func TestValidateContractScript(t *testing.T) {
	interpreter := NewScriptInterpreter()

	// Test valid multisig contract
	params := map[string]interface{}{
		"signatures": []string{
			"304402207fa7a6d1e0ee81132a269ad84e68d695483745cde8b541e3bf630749894e342a022030c55193580c486495d3536a4122e742b062da727f1185654d03bdc656bfc822",
			"304402207fa7a6d1e0ee81132a269ad84e68d695483745cde8b541e3bf630749894e342a022030c55193580c486495d3536a4122e742b062da727f1185654d03bdc656bfc823",
		},
		"pubkeys": []string{
			"03a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2",
			"03b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3",
			"03c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4",
		},
	}

	result, err := interpreter.ValidateContractScript(
		"multisig_escrow",
		"522103a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b22103b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c32103c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d453ae",
		params,
	)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("Expected valid result but got invalid: %s", result.Error)
	}

	// Test invalid contract type
	result, err = interpreter.ValidateContractScript(
		"invalid_contract",
		"76a914abc123def456789012345678901234567890123488ac",
		map[string]interface{}{},
	)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("Expected invalid result for unknown contract type")
	}
}

func TestScriptInterpreterEdgeCases(t *testing.T) {
	interpreter := NewScriptInterpreter()

	t.Run("Nil inputs", func(t *testing.T) {
		// Test with empty strings
		result, err := interpreter.ValidateP2PKH("", "", "")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if result.Valid {
			t.Error("Expected invalid result for empty inputs")
		}

		result, err = interpreter.ValidateMultisigEscrow("", nil, nil)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if result.Valid {
			t.Error("Expected invalid result for empty inputs")
		}
	})

	t.Run("Malformed hex strings", func(t *testing.T) {
		result, err := interpreter.ValidateP2PKH("xyz", "3044", "03a1")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if result.Valid {
			t.Error("Expected invalid result for malformed hex")
		}
	})

	t.Run("Extremely long inputs", func(t *testing.T) {
		longScript := "51" + "abc123def45678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678"
		result, err := interpreter.ValidateTaproot(longScript, "3044", "21a1")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		// Should handle gracefully without panicking
		_ = result
	})
}
