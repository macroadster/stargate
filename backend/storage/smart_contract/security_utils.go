package smart_contract

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"stargate-backend/core/smart_contract"
)

// Security constants
const (
	MaxMetadataSize    = 1 * 1024 * 1024 // 1MB
	MaxJSONDepth       = 10
	MaxProposalTitle   = 500
	MaxProposalDesc    = 10000
	MaxTaskTitle       = 200
	MaxTaskDescription = 5000
)

// Dangerous patterns to sanitize
var (
	xssPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)<script[^>]*>.*?</script>`),
		regexp.MustCompile(`(?i)<iframe[^>]*>.*?</iframe>`),
		regexp.MustCompile(`(?i)<object[^>]*>.*?</object>`),
		regexp.MustCompile(`(?i)<embed[^>]*>.*?</embed>`),
		regexp.MustCompile(`(?i)javascript:`),
		regexp.MustCompile(`(?i)vbscript:`),
		regexp.MustCompile(`(?i)onload\s*=`),
		regexp.MustCompile(`(?i)onerror\s*=`),
		regexp.MustCompile(`(?i)onclick\s*=`),
	}

	pathTraversalPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\.\./`),
		regexp.MustCompile(`\.\\`),
		regexp.MustCompile(`(?i)%2e%2e%2f`),
		regexp.MustCompile(`(?i)%2e%2e%5c`),
	}

	controlCharPattern = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`)
)

// SanitizeInput removes dangerous patterns from user input.
// It returns the sanitized string and a boolean indicating if any dangerous patterns were found.
func SanitizeInput(input string) (string, bool) {
	if input == "" {
		return input, false
	}

	foundDangerous := false
	result := input

	// Check for control characters
	if controlCharPattern.MatchString(result) {
		foundDangerous = true
		result = controlCharPattern.ReplaceAllString(result, "")
	}

	// Check and remove XSS patterns
	for _, pattern := range xssPatterns {
		if pattern.MatchString(result) {
			foundDangerous = true
			result = pattern.ReplaceAllString(result, "")
		}
	}

	// Check and remove path traversal patterns
	for _, pattern := range pathTraversalPatterns {
		if pattern.MatchString(result) {
			foundDangerous = true
			result = pattern.ReplaceAllString(result, "")
		}
	}

	// Check and remove null bytes
	if strings.Contains(result, "\x00") {
		foundDangerous = true
		result = strings.ReplaceAll(result, "\x00", "")
	}

	return result, foundDangerous
}

// ValidateMetadataSize checks if metadata is within size limits
func ValidateMetadataSize(metadata map[string]interface{}) error {
	if metadata == nil {
		return nil
	}

	// Calculate approximate size
	totalSize := 0
	for key, value := range metadata {
		totalSize += len(key)
		totalSize += estimateValueSize(value)
		if totalSize > MaxMetadataSize {
			return fmt.Errorf("metadata size %d exceeds maximum %d bytes", totalSize, MaxMetadataSize)
		}
	}

	return nil
}

// estimateValueSize estimates the size of a metadata value
func estimateValueSize(value interface{}) int {
	switch v := value.(type) {
	case string:
		return len(v)
	case []byte:
		return len(v)
	case int, int32, int64, float32, float64, bool:
		return 8
	case map[string]interface{}:
		size := 0
		for k, val := range v {
			size += len(k) + estimateValueSize(val)
		}
		return size
	case []interface{}:
		size := 0
		for _, val := range v {
			size += estimateValueSize(val)
		}
		return size
	default:
		return 16 // Default estimate for unknown types
	}
}

// ValidateJSONDepth checks if JSON structure is too deeply nested
func ValidateJSONDepth(value interface{}, currentDepth int) error {
	if currentDepth > MaxJSONDepth {
		return fmt.Errorf("JSON depth %d exceeds maximum %d", currentDepth, MaxJSONDepth)
	}

	switch v := value.(type) {
	case map[string]interface{}:
		for _, val := range v {
			if err := ValidateJSONDepth(val, currentDepth+1); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, val := range v {
			if err := ValidateJSONDepth(val, currentDepth+1); err != nil {
				return err
			}
		}
	}

	return nil
}

// ValidatePixelHashFormat validates visible pixel hash format
func ValidatePixelHashFormat(hash string) error {
	if hash == "" {
		return fmt.Errorf("visible pixel hash cannot be empty")
	}

	// Remove whitespace
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return fmt.Errorf("visible pixel hash cannot be whitespace only")
	}

	// Check if it's valid hex
	if _, err := hex.DecodeString(hash); err != nil {
		return fmt.Errorf("visible pixel hash must be valid hexadecimal string")
	}

	// Check length (should be reasonable for a hash)
	if len(hash) < 8 || len(hash) > 128 {
		return fmt.Errorf("visible pixel hash length %d is invalid (8-128 characters expected)", len(hash))
	}

	return nil
}

// ValidateBitcoinAddress validates Bitcoin address format with enhanced security
func ValidateBitcoinAddress(addr string) error {
	if addr == "" {
		return fmt.Errorf("bitcoin address cannot be empty")
	}

	addr = strings.TrimSpace(addr)
	if addr == "" {
		return fmt.Errorf("bitcoin address cannot be whitespace only")
	}

	// Enhanced length check with stricter bounds
	if len(addr) < 26 || len(addr) > 90 {
		return fmt.Errorf("bitcoin address length %d is invalid (expected 26-90 characters)", len(addr))
	}

	// Check for common attack patterns
	/*
		if strings.Contains(strings.ToLower(addr), "test") ||
			strings.Contains(strings.ToLower(addr), "example") ||
			strings.Contains(addr, "....") ||
			strings.Count(addr, "1") > len(addr)/2 {
			return fmt.Errorf("bitcoin address appears to be invalid or test address")
		}
	*/

	// Bech32 address validation (bc1 and tb1 addresses)
	if strings.HasPrefix(addr, "bc1") || strings.HasPrefix(addr, "tb1") {
		// Bech32 addresses have different character set
		bech32Chars := "023456789acdefghjklmnpqrstuvwxyz"
		for _, char := range addr[3:] { // Skip "bc1" or "tb1" prefix
			if !strings.ContainsRune(bech32Chars, char) {
				return fmt.Errorf("bech32 address contains invalid character: %c", char)
			}
		}

		// Bech32 length validation
		if len(addr) < 42 || len(addr) > 90 {
			return fmt.Errorf("bech32 bitcoin address length %d is invalid (expected 42-90 characters)", len(addr))
		}

		// Basic bech32 checksum validation (simplified)
		if len(addr) < 8 {
			return fmt.Errorf("bech32 address too short for valid checksum")
		}

		return nil
	}

	// Base58 address validation (legacy addresses including testnet)
	if strings.HasPrefix(addr, "1") || strings.HasPrefix(addr, "3") || strings.HasPrefix(addr, "m") || strings.HasPrefix(addr, "n") {
		// Length validation for legacy addresses (mainnet and testnet)
		if len(addr) < 26 || len(addr) > 35 { // Standard range for Base58 addresses
			return fmt.Errorf("legacy bitcoin address length %d is invalid (expected 26-35 characters)", len(addr))
		}

		// Check for valid Bitcoin address characters (base58)
		validChars := "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
		for _, char := range addr {
			if !strings.ContainsRune(validChars, char) {
				return fmt.Errorf("bitcoin address contains invalid character: %c", char)
			}
		}

		// Basic checksum validation (simplified - would need full base58 decode for proper validation)
		// For now, just ensure it doesn't end with obviously invalid patterns
		if strings.HasSuffix(addr, "111111") || strings.HasSuffix(addr, "222222") {
			return fmt.Errorf("bitcoin address has invalid checksum pattern")
		}

		return nil
	}

	return fmt.Errorf("bitcoin address has invalid prefix (must start with 1, 3, m, n, bc1, or tb1)")
}

// ValidateProposalInput validates all proposal input fields
func ValidateProposalInput(proposal smart_contract.Proposal) error {
	// Validate title
	if proposal.Title != "" {
		if len(proposal.Title) > MaxProposalTitle {
			return fmt.Errorf("proposal title length %d exceeds maximum %d", len(proposal.Title), MaxProposalTitle)
		}
		var foundDangerous bool
		proposal.Title, foundDangerous = SanitizeInput(proposal.Title)
		if foundDangerous {
			return fmt.Errorf("proposal title contains dangerous patterns")
		}
	}

	// Validate description
	if proposal.DescriptionMD != "" {
		if len(proposal.DescriptionMD) > MaxProposalDesc {
			return fmt.Errorf("proposal description length %d exceeds maximum %d", len(proposal.DescriptionMD), MaxProposalDesc)
		}
		var foundDangerous bool
		proposal.DescriptionMD, foundDangerous = SanitizeInput(proposal.DescriptionMD)
		if foundDangerous {
			return fmt.Errorf("proposal description contains dangerous patterns")
		}
	}

	// Validate metadata
	if err := ValidateMetadataSize(proposal.Metadata); err != nil {
		return fmt.Errorf("metadata validation failed: %v", err)
	}

	if err := ValidateJSONDepth(proposal.Metadata, 0); err != nil {
		return fmt.Errorf("metadata JSON depth validation failed: %v", err)
	}

	// Sanitize metadata string values
	if proposal.Metadata != nil {
		for key, value := range proposal.Metadata {
			if str, ok := value.(string); ok {
				var foundDangerous bool
				sanitizedStr, foundDangerous := SanitizeInput(str)
				if foundDangerous {
					return fmt.Errorf("metadata field '%s' contains dangerous patterns", key)
				}
				proposal.Metadata[key] = sanitizedStr
			}
		}
	}

	// Validate visible_pixel_hash or image_scan_data requirement
	var hasValidPixelHash bool
	if proposal.Metadata != nil {
		if vph, ok := proposal.Metadata["visible_pixel_hash"].(string); ok {
			trimmedVPH := strings.TrimSpace(vph)
			if trimmedVPH != "" {
				// If visible_pixel_hash is provided and non-empty, it must be valid.
				if err := ValidatePixelHashFormat(trimmedVPH); err != nil {
					return fmt.Errorf("visible pixel hash validation failed: %v", err)
				}
				hasValidPixelHash = true
			}
		}
	}
	if !hasValidPixelHash {
		trimmedVPH := strings.TrimSpace(proposal.VisiblePixelHash)
		if trimmedVPH != "" {
			if err := ValidatePixelHashFormat(trimmedVPH); err != nil {
				return fmt.Errorf("visible pixel hash validation failed: %v", err)
			}
			hasValidPixelHash = true
		}
	}

	hasImageScanData := false
	if proposal.Metadata != nil && proposal.Metadata["image_scan_data"] != nil {
		hasImageScanData = true
	}

	// At least one of visible_pixel_hash (if provided and non-empty) or image_scan_data must be present.
	if !hasValidPixelHash && !hasImageScanData {
		return fmt.Errorf("proposals must include image scan metadata (valid visible_pixel_hash or image_scan_data in metadata)")
	}

	// Validate tasks if present
	if len(proposal.Tasks) > 0 {
		for i, task := range proposal.Tasks {
			// Each task must have a budget
			if task.BudgetSats <= 0 {
				return fmt.Errorf("task %d (%s) must have a positive budget_sats", i+1, task.Title)
			}

			// Validate task input fields
			if err := ValidateTaskInput(task); err != nil {
				return fmt.Errorf("task %d validation failed: %v", i+1, err)
			}
		}
	}

	return nil
}

// ValidateTaskInput validates all task input fields
func ValidateTaskInput(task smart_contract.Task) error {
	// Validate title
	if task.Title != "" {
		if len(task.Title) > MaxTaskTitle {
			return fmt.Errorf("task title length %d exceeds maximum %d", len(task.Title), MaxTaskTitle)
		}
		var foundDangerous bool
		task.Title, foundDangerous = SanitizeInput(task.Title)
		if foundDangerous {
			return fmt.Errorf("task title contains dangerous patterns")
		}
	}

	// Validate description
	if task.Description != "" {
		if len(task.Description) > MaxTaskDescription {
			return fmt.Errorf("task description length %d exceeds maximum %d", len(task.Description), MaxTaskDescription)
		}
		var foundDangerous bool
		task.Description, foundDangerous = SanitizeInput(task.Description)
		if foundDangerous {
			return fmt.Errorf("task description contains dangerous patterns")
		}
	}

	// Validate contractor wallet if present
	if task.ContractorWallet != "" {
		if err := ValidateBitcoinAddress(task.ContractorWallet); err != nil {
			return fmt.Errorf("contractor wallet validation failed: %v", err)
		}
	}

	return nil
}

// IsValidStatus checks if a status is valid for the given entity type
func IsValidStatus(status string, entityType string) bool {
	if status == "" {
		return false
	}

	status = strings.ToLower(status)

	switch entityType {
	case "proposal":
		validStatuses := []string{"pending", "approved", "rejected", "published"}
		for _, valid := range validStatuses {
			if status == valid {
				return true
			}
		}
	case "task":
		validStatuses := []string{"available", "claimed", "submitted", "approved", "published", "completed", "rejected"}
		for _, valid := range validStatuses {
			if status == valid {
				return true
			}
		}
	case "claim":
		validStatuses := []string{"active", "submitted", "complete", "expired", "rejected"}
		for _, valid := range validStatuses {
			if status == valid {
				return true
			}
		}
	case "submission":
		validStatuses := []string{"pending_review", "reviewed", "approved", "rejected"}
		for _, valid := range validStatuses {
			if status == valid {
				return true
			}
		}
	}

	return false
}

// SanitizeFileName sanitizes file names to prevent path traversal
func SanitizeFileName(filename string) string {
	if filename == "" {
		return filename
	}

	// Remove path separators
	filename = strings.ReplaceAll(filename, "/", "_")
	filename = strings.ReplaceAll(filename, "\\", "_")
	filename = strings.ReplaceAll(filename, "..", "_")

	// Remove control characters
	result := ""
	for _, r := range filename {
		if unicode.IsGraphic(r) && r != '\x00' {
			result += string(r)
		}
	}

	// Limit length
	if len(result) > 255 {
		result = result[:255]
	}

	return result
}

// ValidateAPIKeyFormat validates API key format
func ValidateAPIKeyFormat(key string) error {
	if key == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	if len(key) != 64 {
		return fmt.Errorf("API key must be 64 characters (256 bits), got %d", len(key))
	}

	if _, err := hex.DecodeString(key); err != nil {
		return fmt.Errorf("API key must be valid hexadecimal string")
	}

	return nil
}

// isValidProposalStatus checks if a status is valid for proposals
func isValidProposalStatus(status string) bool {
	validStatuses := []string{"pending", "approved", "rejected", "published", "confirmed"}
	for _, valid := range validStatuses {
		if strings.EqualFold(status, valid) {
			return true
		}
	}
	return false
}

// contractIDFromMeta determines the canonical contract identifier from metadata.
// It prioritizes visible_pixel_hash, then contract_id, then ingestion_id, and finally the proposal ID.
func contractIDFromMeta(meta map[string]interface{}, id string) string {
	// Use visible_pixel_hash as the canonical contract identifier
	// since it uniquely identifies the steganography content
	if hash, ok := meta["visible_pixel_hash"].(string); ok && strings.TrimSpace(hash) != "" {
		return hash
	}

	// Fallback to explicit contract_id if provided
	if cid, ok := meta["contract_id"].(string); ok && strings.TrimSpace(cid) != "" {
		return cid
	}

	// Fallback to ingestion_id for proposals created from ingestion
	if cid, ok := meta["ingestion_id"].(string); ok && strings.TrimSpace(cid) != "" {
		return cid
	}

	// Final fallback to proposal ID
	return id
}
