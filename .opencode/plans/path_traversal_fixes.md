# Path Traversal Vulnerability Remediation Plan

## Executive Summary

**Issue ID:** starlight-1aio
**Priority:** 0 (Critical)
**Type:** Security Bug

Critical path traversal vulnerabilities identified across multiple file operations allowing arbitrary file read/write on the system. User-controlled filenames and file paths from inscriptions are used directly with `filepath.Join()` without sanitization.

**Potential Impacts:**
- Remote Code Execution (RCE) via file overwrite
- Sensitive data exfiltration (credentials, API keys, configuration)
- System compromise
- Data integrity violations

---

## Vulnerability Inventory

### 1. File Upload Path Traversal (CRITICAL)
**Files:** `backend/handlers/handlers.go`
**Lines:** 249, 566, 830

**Vulnerable Code:**
```go
targetPath := filepath.Join(uploadsDir, filename)        // Line 249
target := filepath.Join(uploadsDir, rec.Filename)        // Line 566
imagePath := filepath.Join(uploadsDir, imageFilename)    // Line 830
```

**Attack Vector:** User-controlled `filename` from JSON API requests can contain `../` sequences.

---

### 2. Inscription File Read Path Traversal (CRITICAL)
**Files:** `backend/api/data_api.go`
**Lines:** 859, 1368, 1523

**Vulnerable Code:**
```go
textPath := filepath.Join(blockDir, ins.FilePath)                              // Line 859
fsPath := filepath.Join(fmt.Sprintf("%s/%d_00000000", base, height), filePath) // Line 1368
fsPath := filepath.Join(blockDir, ins.FilePath)                               // Line 1523
```

**Attack Vector:** `ins.FilePath` and `filePath` from inscription metadata (stored on blockchain) can contain path traversal sequences.

---

### 3. ReadTextContent Path Traversal (CRITICAL)
**File:** `backend/storage/data_storage.go`
**Line:** 457

**Vulnerable Code:**
```go
fullPath := filepath.Join("blocks", fmt.Sprintf("%d_00000000", height), filePath)
content, err := os.ReadFile(fullPath)
```

**Attack Vector:** `filePath` parameter directly from inscription data without validation.

---

### 4. Block Monitor File Write Vulnerabilities (HIGH)
**File:** `backend/bitcoin/block_monitor.go`
**Lines:** 854, 892, 914, 987, 1488, 2459, 2522

**Vulnerable Code:**
```go
hexFile := filepath.Join(blockDir, "block.hex")
imageFile := filepath.Join(imagesDir, cleaned.FileName)
// ... and more
```

**Attack Vector:** `cleaned.FileName` and other user-controlled values may not be sanitized.

---

### 5. Directory Entry Name Vulnerability (MEDIUM)
**File:** `backend/storage/data_storage.go`
**Line:** 330

**Vulnerable Code:**
```go
inscriptionsJsonPath := filepath.Join(ds.dataDir, blockEntry.Name(), "inscriptions.json")
```

---

### 6. Content-Type Sniffing Path Traversal (MEDIUM)
**File:** `backend/api/data_api.go`
**Line:** 890

**Vulnerable Code:**
```go
fsPath := filepath.Join(fmt.Sprintf("%s/%d_00000000", base, height), filePath)
file, err := os.Open(fsPath)
```

---

## Remediation Strategy

### Phase 1: Create Security Utility Functions

**Create new file:** `backend/security/path_sanitizer.go`

```go
package security

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SanitizeFilename removes dangerous path sequences and normalizes filename
func SanitizeFilename(filename string) string {
	// Remove path separators
	filename = filepath.Base(filename)

	// Remove null bytes and control characters
	filename = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, filename)

	// Limit filename length (255 is typical max for most filesystems)
	if len(filename) > 255 {
		filename = filename[:255]
	}

	// If filename becomes empty, use a safe default
	if filename == "" || filename == "." || filename == ".." {
		filename = "file"
	}

	return filename
}

// SanitizePath validates a path is safe and doesn't escape base directory
func SanitizePath(baseDir, userPath string) (string, error) {
	// Join and clean the path
	fullPath := filepath.Join(baseDir, userPath)
	cleanPath := filepath.Clean(fullPath)

	// Ensure the resulting path is within baseDir
	baseDir = filepath.Clean(baseDir)
	if !strings.HasPrefix(cleanPath, baseDir+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal detected: %s", userPath)
	}

	return cleanPath, nil
}

// ValidateExtension checks if file extension is in whitelist
func ValidateExtension(filename string, allowed []string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	for _, allowedExt := range allowed {
		if strings.ToLower(allowedExt) == ext {
			return true
		}
	}
	return false
}

// SafeFilePath constructs a safe file path preventing traversal
func SafeFilePath(baseDir, filename string) string {
	sanitized := SanitizeFilename(filename)
	return filepath.Join(baseDir, sanitized)
}
```

---

### Phase 2: Fix File Upload Vulnerabilities

#### 2.1 Fix `handlers/handlers.go:249`

**Before:**
```go
targetPath := filepath.Join(uploadsDir, filename)
```

**After:**
```go
import "stargate-backend/security"

targetPath := security.SafeFilePath(uploadsDir, filename)
```

#### 2.2 Fix `handlers/handlers.go:566`

**Before:**
```go
target := filepath.Join(uploadsDir, rec.Filename)
```

**After:**
```go
target := security.SafeFilePath(uploadsDir, rec.Filename)
```

#### 2.3 Fix `handlers/handlers.go:830`

**Before:**
```go
imagePath := filepath.Join(uploadsDir, imageFilename)
```

**After:**
```go
imagePath := security.SafeFilePath(uploadsDir, imageFilename)
```

---

### Phase 3: Fix File Read Vulnerabilities

#### 3.1 Fix `api/data_api.go:859`

**Before:**
```go
textPath := filepath.Join(blockDir, ins.FilePath)
if data, err := os.ReadFile(textPath); err == nil {
```

**After:**
```go
import "stargate-backend/security"

safePath, err := security.SanitizePath(blockDir, ins.FilePath)
if err == nil {
	if data, err := os.ReadFile(safePath); err == nil {
```

#### 3.2 Fix `api/data_api.go:1368`

**Before:**
```go
fsPath := filepath.Join(fmt.Sprintf("%s/%d_00000000", base, height), filePath)
file, err := os.Open(fsPath)
```

**After:**
```go
baseDir := fmt.Sprintf("%s/%d_00000000", base, height)
safePath, err := security.SanitizePath(baseDir, filePath)
if err != nil {
	http.Error(w, "invalid file path", http.StatusBadRequest)
	return
}
file, err := os.Open(safePath)
```

#### 3.3 Fix `api/data_api.go:1523`

**Before:**
```go
fsPath := filepath.Join(blockDir, ins.FilePath)
if data, err := os.ReadFile(fsPath); err == nil {
```

**After:**
```go
safePath, err := security.SanitizePath(blockDir, ins.FilePath)
if err != nil {
	// Skip invalid paths
	continue
}
if data, err := os.ReadFile(safePath); err == nil {
```

---

### Phase 4: Fix ReadTextContent

**File:** `backend/storage/data_storage.go:454-466`

**Before:**
```go
func (ds *DataStorage) ReadTextContent(height int64, filePath string) (string, error) {
	fullPath := filepath.Join("blocks", fmt.Sprintf("%d_00000000", height), filePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read text file %s: %w", fullPath, err)
	}
	return string(content), nil
}
```

**After:**
```go
import "stargate-backend/security"

func (ds *DataStorage) ReadTextContent(height int64, filePath string) (string, error) {
	blockDir := filepath.Join(ds.dataDir, fmt.Sprintf("%d_00000000", height))
	safePath, err := security.SanitizePath(blockDir, filePath)
	if err != nil {
		return "", fmt.Errorf("invalid file path: %w", err)
	}
	content, err := os.ReadFile(safePath)
	if err != nil {
		return "", fmt.Errorf("failed to read text file %s: %w", safePath, err)
	}
	return string(content), nil
}
```

---

### Phase 5: Fix Block Monitor File Writes

**File:** `backend/bitcoin/block_monitor.go`

For each location using `filepath.Join()` with user-controlled filenames, apply sanitization.

Example fix for line 914:
```go
import "stargate-backend/security"

// Before:
imageFile := filepath.Join(imagesDir, cleaned.FileName)

// After:
imageFile := security.SafeFilePath(imagesDir, cleaned.FileName)
```

**Locations to fix:** 854, 892, 914, 987, 1488, 2459, 2522

---

### Phase 6: Additional Security Measures

#### 6.1 Add File Extension Whitelist

Create configuration file: `backend/security/allowed_extensions.go`

```go
package security

var AllowedImageExtensions = []string{
	".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".bmp", ".svg",
}

var AllowedTextExtensions = []string{
	".txt", ".json", ".html", ".md",
}

var AllowedDataExtensions = []string{
	".hex", ".bin",
}
```

#### 6.2 Add Input Validation Middleware

**Create:** `backend/middleware/security.go`

```go
package middleware

import (
	"net/http"
	"strings"
)

// ValidateFilename is middleware to check request parameters for path traversal
func ValidateFilename(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for key, values := range r.URL.Query() {
			for _, value := range values {
				if strings.Contains(value, "../") || strings.Contains(value, "..\\") {
					http.Error(w, "invalid input", http.StatusBadRequest)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
```

---

## Testing Strategy

### 1. Unit Tests for Security Utilities

**File:** `backend/security/path_sanitizer_test.go`

```go
package security

import "testing"

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal.txt", "normal.txt"},
		{"../../../etc/passwd", "passwd"},
		{"foo/bar/baz.txt", "baz.txt"},
		{"", "file"},
		{"..", "file"},
		{".", "file"},
		{"\x00test.txt", "test.txt"},
	}

	for _, tt := range tests {
		result := SanitizeFilename(tt.input)
		if result != tt.expected {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestSanitizePath(t *testing.T) {
	baseDir := "/safe/base"

	tests := []struct {
		path      string
		shouldErr bool
	}{
		{"/safe/base/file.txt", false},
		{"/safe/base/subdir/file.txt", false},
		{"../../../etc/passwd", true},
		{"../outside.txt", true},
	}

	for _, tt := range tests {
		_, err := SanitizePath(baseDir, tt.path)
		if (err != nil) != tt.shouldErr {
			t.Errorf("SanitizePath(%q) error = %v, shouldErr %v", tt.path, err, tt.shouldErr)
		}
	}
}
```

### 2. Integration Tests

**File:** `backend/security/integration_test.go`

Test actual API endpoints with malicious payloads:
- Path traversal in filename upload
- Malicious inscription FilePath values
- Unicode and control character attacks

### 3. Security Regression Tests

Add tests to existing test suites to prevent regressions:
- `handlers_test.go` - test upload sanitization
- `data_api_test.go` - test file read sanitization
- `data_storage_test.go` - test ReadTextContent sanitization

---

## Implementation Order

### Priority 1 (Critical - Week 1)
1. Create `backend/security/path_sanitizer.go`
2. Fix file upload vulnerabilities (handlers/handlers.go)
3. Write unit tests for sanitization functions
4. Add security middleware

### Priority 2 (High - Week 2)
5. Fix file read vulnerabilities (api/data_api.go)
6. Fix ReadTextContent (storage/data_storage.go)
7. Add integration tests
8. Manual security testing

### Priority 3 (Medium - Week 3)
9. Fix block monitor file writes
10. Add file extension validation
11. Add security regression tests
12. Documentation update

---

## Deployment Plan

### Pre-Deployment Checklist
- [ ] All unit tests passing
- [ ] All integration tests passing
- [ ] Manual security testing completed
- [ ] Code review approved
- [ ] Documentation updated

### Deployment Steps
1. **Staging Environment**
   - Deploy changes to staging
   - Run full test suite
   - Perform manual security testing
   - Monitor logs for errors

2. **Production Rollout**
   - Deploy during low-traffic window
   - Monitor error rates closely
   - Have rollback plan ready
   - Verify uploads and file serving work correctly

3. **Post-Deployment**
   - Monitor for 24-48 hours
   - Check for any failed file operations
   - Review security logs
   - Document any issues found

### Rollback Plan
If issues detected, revert to previous version immediately. Rollback should take < 5 minutes.

---

## Success Criteria

- All path traversal vulnerabilities eliminated
- Security tests pass 100%
- No regressions in file operations
- Upload and file serving functionality verified
- Zero critical security findings from follow-up scan
- Documentation complete and reviewed

---

## Related Issues

None currently. Consider creating follow-up issues for:
- Add rate limiting to file upload endpoints
- Implement file size quotas per user/IP
- Add virus scanning for uploaded files
- Implement audit logging for file operations

---

## References

- CWE-22: Improper Limitation of a Pathname to a Restricted Directory ('Path Traversal')
- CWE-23: Relative Path Traversal
- OWASP Path Traversal: https://owasp.org/www-community/attacks/Path_Traversal
- Go secure filepath patterns: https://github.com/golang/go/wiki/CodeReviewComments#security

---

**Status:** Planning
**Next Action:** Review and approve plan before implementation
