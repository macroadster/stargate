package smart_contract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// ScriptInterpreter validates Bitcoin scripts for smart contracts
type ScriptInterpreter struct {
	// Simplified implementation without external Bitcoin libraries
}

// NewScriptInterpreter creates a new Bitcoin script interpreter
func NewScriptInterpreter() *ScriptInterpreter {
	return &ScriptInterpreter{}
}

// ValidationResult contains result of script validation
type ValidationResult struct {
	Valid              bool                   `json:"valid"`
	Error              string                 `json:"error,omitempty"`
	Details            map[string]interface{} `json:"details,omitempty"`
	ScriptType         string                 `json:"script_type,omitempty"`
	RequiredSignatures int                    `json:"required_signatures,omitempty"`
	Timelock           int64                  `json:"timelock,omitempty"`
}

// ValidateMultisigEscrow validates a 2-of-3 multisig escrow script
func (si *ScriptInterpreter) ValidateMultisigEscrow(scriptHex string, signatures []string, pubKeys []string) (*ValidationResult, error) {
	result := &ValidationResult{
		Details: make(map[string]interface{}),
	}

	// Basic validation
	if len(signatures) < 2 {
		result.Error = "Multisig requires at least 2 signatures"
		return result, nil
	}

	if len(pubKeys) != 3 {
		result.Error = "Multisig requires exactly 3 public keys"
		return result, nil
	}

	// Validate hex format
	if _, err := hex.DecodeString(scriptHex); err != nil {
		result.Error = fmt.Sprintf("Invalid script hex: %v", err)
		return result, nil
	}

	// Validate signatures format
	for i, sig := range signatures {
		if _, err := hex.DecodeString(sig); err != nil {
			result.Error = fmt.Sprintf("Invalid signature %d: %v", i, err)
			return result, nil
		}
	}

	// Validate pubKeys format
	for i, pubKey := range pubKeys {
		if _, err := hex.DecodeString(pubKey); err != nil {
			result.Error = fmt.Sprintf("Invalid pubKey %d: %v", i, err)
			return result, nil
		}
	}

	// Check script pattern for 2-of-3 multisig
	// Expected pattern: OP_2 <pub1> <pub2> <pub3> OP_3 OP_CHECKMULTISIG
	if !strings.Contains(scriptHex, "52") || // OP_2
		!strings.Contains(scriptHex, "53") || // OP_3
		!strings.Contains(scriptHex, "ae") { // OP_CHECKMULTISIG
		result.Error = "Invalid multisig script pattern"
		return result, nil
	}

	result.Valid = true
	result.ScriptType = "multisig"
	result.RequiredSignatures = 2
	return result, nil
}

// ValidateP2PKH validates a Pay-to-Public-Key-Hash script
func (si *ScriptInterpreter) ValidateP2PKH(scriptHex, signature, pubKey string) (*ValidationResult, error) {
	result := &ValidationResult{
		Details: make(map[string]interface{}),
	}

	// Validate hex formats
	if _, err := hex.DecodeString(scriptHex); err != nil {
		result.Error = fmt.Sprintf("Invalid script hex: %v", err)
		return result, nil
	}

	if _, err := hex.DecodeString(signature); err != nil {
		result.Error = fmt.Sprintf("Invalid signature: %v", err)
		return result, nil
	}

	if _, err := hex.DecodeString(pubKey); err != nil {
		result.Error = fmt.Sprintf("Invalid pubKey: %v", err)
		return result, nil
	}

	// Check P2PKH pattern: OP_DUP OP_HASH160 <hash> OP_EQUALVERIFY OP_CHECKSIG
	// Expected pattern: 76 a9 14 <20-byte-hash> 88 ac
	if !strings.Contains(scriptHex, "76") || // OP_DUP
		!strings.Contains(scriptHex, "a9") || // OP_HASH160
		!strings.Contains(scriptHex, "14") || // 20 bytes
		!strings.Contains(scriptHex, "88") || // OP_EQUALVERIFY
		!strings.Contains(scriptHex, "ac") { // OP_CHECKSIG
		result.Error = "Script does not match P2PKH pattern"
		return result, nil
	}

	result.Valid = true
	result.ScriptType = "p2pkh"

	return result, nil
}

// ValidateTimeLock validates CLTV/CSV time-lock scripts
func (si *ScriptInterpreter) ValidateTimeLock(scriptHex string, currentBlockHeight int64) (*ValidationResult, error) {
	result := &ValidationResult{
		Details: make(map[string]interface{}),
	}

	if _, err := hex.DecodeString(scriptHex); err != nil {
		result.Error = fmt.Sprintf("Invalid script hex: %v", err)
		return result, nil
	}

	// Extract lock time (simplified - just look for 4-byte sequence after opcode)
	parts := strings.Split(scriptHex, "b1") // CLTV
	var lockTime int64
	if len(parts) > 1 {
		lockTimeHex := strings.TrimSpace(parts[1][:8]) // First 4 bytes after opcode
		if lt, err := strconv.ParseInt(lockTimeHex, 16, 64); err == nil {
			lockTime = lt
			if currentBlockHeight < lockTime {
				result.Error = fmt.Sprintf("Time-lock not expired: current %d < lock %d", currentBlockHeight, lockTime)
				return result, nil
			}
			result.Details["lock_time"] = lockTime
		}
	}

	result.Valid = true
	result.ScriptType = "timelock"
	result.Timelock = lockTime

	return result, nil
}

// ValidateTaproot validates a Taproot output script (simplified)
func (si *ScriptInterpreter) ValidateTaproot(scriptHex, signature, controlBlock string) (*ValidationResult, error) {
	result := &ValidationResult{
		Details: make(map[string]interface{}),
	}

	// Validate hex formats
	if _, err := hex.DecodeString(scriptHex); err != nil {
		result.Error = fmt.Sprintf("Invalid script hex: %v", err)
		return result, nil
	}

	if _, err := hex.DecodeString(signature); err != nil {
		result.Error = fmt.Sprintf("Invalid signature: %v", err)
		return result, nil
	}

	if _, err := hex.DecodeString(controlBlock); err != nil {
		result.Error = fmt.Sprintf("Invalid control block: %v", err)
		return result, nil
	}

	// Check Taproot pattern: OP_1 <32-byte-key>
	// Expected: 51 <20-byte-key>
	if !strings.HasPrefix(scriptHex, "51") || len(scriptHex) < 66 {
		result.Error = "Invalid Taproot script format"
		return result, nil
	}

	result.Valid = true
	result.ScriptType = "taproot"

	return result, nil
}

// ExtractScriptDetails extracts information from a script
func (si *ScriptInterpreter) ExtractScriptDetails(scriptHex string) (map[string]interface{}, error) {
	details := make(map[string]interface{})

	if _, err := hex.DecodeString(scriptHex); err != nil {
		return nil, fmt.Errorf("invalid script hex: %v", err)
	}

	details["script_size"] = len(scriptHex) / 2 // hex chars to bytes

	// Detect script type
	scriptType := si.detectScriptType(scriptHex)
	details["script_type"] = scriptType

	// Extract opcodes (simplified)
	opcodes := si.extractOpcodes(scriptHex)
	details["opcodes"] = opcodes

	return details, nil
}

func (si *ScriptInterpreter) detectScriptType(scriptHex string) string {
	if strings.Contains(scriptHex, "76a914") && strings.Contains(scriptHex, "88ac") {
		return "p2pkh"
	}

	if strings.Contains(scriptHex, "a914") && strings.Contains(scriptHex, "87") {
		return "p2sh"
	}

	if strings.HasPrefix(scriptHex, "6a") {
		return "op_return"
	}

	if strings.Contains(scriptHex, "52") && strings.Contains(scriptHex, "53") && strings.Contains(scriptHex, "ae") {
		return "multisig"
	}

	if strings.HasPrefix(scriptHex, "51") && len(scriptHex) >= 66 {
		return "taproot"
	}

	return "unknown"
}

func (si *ScriptInterpreter) extractOpcodes(scriptHex string) []string {
	var opcodes []string
	i := 0
	for i < len(scriptHex) {
		if i+2 <= len(scriptHex) {
			opcode := scriptHex[i : i+2]
			opcodes = append(opcodes, si.getOpcodeName(opcode))
			i += 2
		} else {
			break
		}
	}
	return opcodes
}

func (si *ScriptInterpreter) getOpcodeName(hexCode string) string {
	opcodeMap := map[string]string{
		"00": "OP_0",
		"51": "OP_1",
		"52": "OP_2",
		"53": "OP_3",
		"54": "OP_4",
		"55": "OP_5",
		"56": "OP_6",
		"57": "OP_7",
		"58": "OP_8",
		"59": "OP_9",
		"5a": "OP_10",
		"5b": "OP_11",
		"5c": "OP_12",
		"5d": "OP_13",
		"5e": "OP_14",
		"5f": "OP_15",
		"60": "OP_16",
		"6a": "OP_RETURN",
		"76": "OP_DUP",
		"87": "OP_EQUAL",
		"88": "OP_EQUALVERIFY",
		"89": "OP_EQUAL",
		"8a": "OP_EQUALVERIFY",
		"8b": "OP_EQUAL",
		"8c": "OP_EQUALVERIFY",
		"8d": "OP_EQUAL",
		"8e": "OP_EQUALVERIFY",
		"8f": "OP_EQUAL",
		"90": "OP_EQUALVERIFY",
		"91": "OP_EQUAL",
		"92": "OP_EQUALVERIFY",
		"93": "OP_EQUAL",
		"94": "OP_EQUALVERIFY",
		"95": "OP_EQUAL",
		"96": "OP_EQUALVERIFY",
		"97": "OP_EQUAL",
		"98": "OP_EQUALVERIFY",
		"99": "OP_EQUAL",
		"9a": "OP_EQUALVERIFY",
		"9b": "OP_EQUAL",
		"9c": "OP_EQUALVERIFY",
		"9d": "OP_EQUAL",
		"9e": "OP_EQUALVERIFY",
		"9f": "OP_EQUAL",
		"a0": "OP_EQUALVERIFY",
		"a1": "OP_EQUAL",
		"a2": "OP_EQUALVERIFY",
		"a3": "OP_EQUAL",
		"a4": "OP_EQUALVERIFY",
		"a5": "OP_EQUAL",
		"a6": "OP_EQUALVERIFY",
		"a7": "OP_EQUAL",
		"a8": "OP_HASH160",
		"a9": "OP_HASH160",
		"aa": "OP_HASH160",
		"ab": "OP_HASH160",
		"ac": "OP_CHECKSIG",
		"ad": "OP_CHECKSIG",
		"ae": "OP_CHECKMULTISIG",
		"af": "OP_CHECKMULTISIG",
		"b0": "OP_CHECKMULTISIG",
		"b1": "OP_CHECKLOCKTIMEVERIFY",
		"b2": "OP_CHECKSEQUENCEVERIFY",
	}

	if name, exists := opcodeMap[strings.ToLower(hexCode)]; exists {
		return name
	}
	return "OP_UNKNOWN_" + strings.ToUpper(hexCode)
}

// ValidateContractScript validates a smart contract script based on contract type
func (si *ScriptInterpreter) ValidateContractScript(contractType string, scriptHex string, params map[string]interface{}) (*ValidationResult, error) {
	switch contractType {
	case "multisig_escrow":
		return si.validateMultisigEscrowContract(scriptHex, params)
	case "timelock_refund":
		return si.validateTimelockContract(scriptHex, params)
	case "taproot_contract":
		return si.validateTaprootContract(scriptHex, params)
	default:
		return &ValidationResult{
			Valid: false,
			Error: fmt.Sprintf("Unknown contract type: %s", contractType),
		}, nil
	}
}

func (si *ScriptInterpreter) validateMultisigEscrowContract(scriptHex string, params map[string]interface{}) (*ValidationResult, error) {
	signatures, _ := params["signatures"].([]string)
	pubKeys, _ := params["pubkeys"].([]string)

	if len(signatures) < 2 || len(pubKeys) != 3 {
		return &ValidationResult{
			Valid: false,
			Error: "Multisig escrow requires 2 signatures and 3 pubkeys",
		}, nil
	}

	return si.ValidateMultisigEscrow(scriptHex, signatures, pubKeys)
}

func (si *ScriptInterpreter) validateTimelockContract(scriptHex string, params map[string]interface{}) (*ValidationResult, error) {
	currentHeight, _ := params["current_height"].(int64)
	if currentHeight == 0 {
		currentHeight = 850000 // Default to current height
	}

	return si.ValidateTimeLock(scriptHex, currentHeight)
}

func (si *ScriptInterpreter) validateTaprootContract(scriptHex string, params map[string]interface{}) (*ValidationResult, error) {
	signature, _ := params["signature"].(string)
	controlBlock, _ := params["control_block"].(string)

	if signature == "" || controlBlock == "" {
		return &ValidationResult{
			Valid: false,
			Error: "Taproot contract requires signature and control block",
		}, nil
	}

	return si.ValidateTaproot(scriptHex, signature, controlBlock)
}

// VerifySignature verifies a signature against a public key (simplified)
func (si *ScriptInterpreter) VerifySignature(message, signature, pubKey string) bool {
	// This is a placeholder for signature verification
	// In a real implementation, this would use ECDSA verification
	// For now, just validate hex format
	_, err1 := hex.DecodeString(signature)
	_, err2 := hex.DecodeString(pubKey)

	return err1 == nil && err2 == nil && len(signature) > 0 && len(pubKey) > 0
}

// ComputeHash160 computes RIPEMD160(SHA256) hash (simplified)
func (si *ScriptInterpreter) ComputeHash160(data string) string {
	shaHash := sha256.Sum256([]byte(data))
	// In real implementation, this would apply RIPEMD160
	return hex.EncodeToString(shaHash[:])
}

// ComputeMerkleRoot computes Merkle root from a list of hashes
func (si *ScriptInterpreter) ComputeMerkleRoot(hashes []string) string {
	if len(hashes) == 0 {
		return ""
	}

	// Simple Merkle tree computation
	current := make([]string, len(hashes))
	copy(current, hashes)

	for len(current) > 1 {
		var next []string
		for i := 0; i < len(current); i += 2 {
			if i+1 < len(current) {
				combined := current[i] + current[i+1]
				hash := sha256.Sum256([]byte(combined))
				next = append(next, hex.EncodeToString(hash[:]))
			} else {
				// Odd number, hash with itself
				combined := current[i] + current[i]
				hash := sha256.Sum256([]byte(combined))
				next = append(next, hex.EncodeToString(hash[:]))
			}
		}
		current = next
	}

	return current[0]
}
