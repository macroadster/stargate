# Security Documentation

## Path Traversal Vulnerability Fixes

### Summary

Critical path traversal vulnerabilities were identified and fixed across multiple file operations. User-controlled filenames and file paths from inscriptions are now properly validated using `security.SafeFilePath()` and `security.SanitizePath()` functions.

### Vulnerability Types

#### 1. File Upload Path Traversal (CRITICAL)
- **File:** `backend/handlers/handlers.go`
- **Lines:** 249, 566, 830
- **Fix:** All user-controlled filenames now use `security.SafeFilePath()` to prevent `../` sequences from writing files outside the uploads directory.

#### 2. Inscription File Read Path Traversal (CRITICAL)
- **File:** `backend/api/data_api.go`
- **Lines:** 859, 1368, 1523
- **Fix:** All file read operations now validate paths with `security.SanitizePath()` to prevent reading files outside intended block directories.

#### 3. ReadTextContent Path Traversal (CRITICAL)
- **File:** `backend/storage/data_storage.go`
- **Line:** 457
- **Fix:** `filePath` parameter is validated to prevent directory traversal attacks.

#### 4. Block Monitor File Write Vulnerabilities (HIGH)
- **File:** `backend/bitcoin/block_monitor.go`
- **Lines:** 2408, 912
- **Fix:** File write operations use `security.SafeFilePath()` to prevent malicious filenames from writing outside block directories.

### Security Utilities

#### `security.SanitizeFilename(filename string) string`
Removes dangerous path sequences and normalizes filename:
- Strips path separators using `filepath.Base()`
- Removes null bytes and control characters
- Limits filename length to 255 characters
- Returns safe default filename if empty

#### `security.SanitizePath(baseDir, userPath string) (string, error)`
Validates a path is safe and doesn't escape base directory:
- Joins baseDir and userPath with `filepath.Join()`
- Cleans path with `filepath.Clean()`
- Rejects paths containing `../` or `..\\`
- Rejects absolute paths
- Returns error if path traversal detected

#### `security.ValidateExtension(filename string, allowed []string) bool`
Checks if file extension is in whitelist:
- Case-insensitive comparison
- Supports both `.ext` and `ext` formats in whitelist

#### `security.SafeFilePath(baseDir, filename string) string`
Convenience function that combines sanitization:
- Calls `SanitizeFilename()` on filename
- Joins with baseDir using `filepath.Join()`

### Allowed Extensions

#### Images
`.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`, `.avif`, `.bmp`, `.svg`

#### Text Files
`.txt`, `.json`, `.html`, `.md`, `.htm`

#### Data Files
`.hex`, `.bin`

### Security Guidelines for File Operations

#### 1. ALWAYS Use Security Functions
```go
// DON'T:
path := filepath.Join(baseDir, userFilename)

// DO:
path := security.SafeFilePath(baseDir, userFilename)
```

#### 2. Validate User Input Before File Operations
```go
// For file writes:
safePath := security.SafeFilePath(uploadsDir, userFilename)
if err := os.WriteFile(safePath, data, 0644); err != nil {
    // Handle error
}

// For file reads:
safePath, err := security.SanitizePath(baseDir, userPath)
if err != nil {
    // Reject the request
    http.Error(w, "invalid file path", http.StatusBadRequest)
    return
}
data, err := os.ReadFile(safePath)
```

#### 3. Never Trust Filenames from Untrusted Sources
- User uploads
- Inscription metadata from blockchain
- API request parameters
- Configuration files (if user-editable)

#### 4. Add Extension Validation for Uploads
```go
if !security.ValidateExtension(filename, security.AllowedImageExtensions) {
    http.Error(w, "Invalid file type", http.StatusBadRequest)
    return
}
```

#### 5. Use Path Whitelisting for Safe Directories
```go
// Only allow files within approved directories
approvedDirs := []string{
    "/data/uploads",
    "/data/blocks",
    "/tmp",
}

for _, dir := range approvedDirs {
    if strings.HasPrefix(fullPath, dir) {
        // Safe to proceed
    }
}
```

### Testing

#### Unit Tests
All security utilities have comprehensive unit tests:
- `backend/security/path_sanitizer_test.go` - Tests for sanitization functions
- `backend/security/integration_test.go` - Integration tests for attack vectors

#### Regression Tests
Security regression tests added to:
- `backend/handlers/security_test.go` - Handler upload security
- `backend/api/security_test.go` - API endpoint security
- `backend/storage/security_test.go` - Storage layer security

Run tests:
```bash
go test ./security/... -v
go test ./handlers/... -run Security -v
go test ./api/... -run Security -v
go test ./storage/... -run Security -v
```

### Attack Vectors Prevented

- Path traversal using `../` sequences
- Windows path traversal using `..\\`
- Absolute path bypass using `/etc/passwd`
- Control character injection
- Null byte injection
- Long filename attacks
- URL-encoded path traversal

### Related Files

- `backend/security/path_sanitizer.go` - Core security utilities
- `backend/security/allowed_extensions.go` - Extension whitelists
- `backend/middleware/security.go` - HTTP request validation middleware

### Deployment Considerations

1. **Staging Testing**
   - Deploy to staging environment first
   - Run full security test suite
   - Perform manual penetration testing
   - Monitor logs for errors

2. **Monitoring**
   - Watch for failed file operations
   - Monitor for rejected paths
   - Alert on suspicious patterns
   - Log all file access attempts

3. **Rollback Plan**
   - Have previous version ready
   - Rollback should take < 5 minutes
   - Test rollback procedure

### References

- CWE-22: Improper Limitation of a Pathname to a Restricted Directory ('Path Traversal')
- CWE-23: Relative Path Traversal
- OWASP Path Traversal: https://owasp.org/www-community/attacks/Path_Traversal
- Go secure filepath patterns: https://github.com/golang/go/wiki/CodeReviewComments#security

---

**Last Updated:** 2026-01-17
**Issue ID:** starlight-1aio
