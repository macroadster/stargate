package security

import (
	"testing"
)

func TestPathTraversalProtection(t *testing.T) {
	attackVectors := []string{
		"../../../etc/passwd",
		"....//....//....//etc/passwd",
		"/etc/passwd",
	}

	for _, payload := range attackVectors {
		safe := SanitizeFilename(payload)
		if safe == "" || safe == "file" {
			continue
		}

		if containsPathTraversal(safe) {
			t.Errorf("SanitizeFilename(%q) still allows path traversal: %s", payload, safe)
		}

		if hasPathSeparators(safe) {
			t.Errorf("SanitizeFilename(%q) still has path separators: %s", payload, safe)
		}
	}
}

func TestFilePathValidation(t *testing.T) {
	baseDir := "/safe/base"

	tests := []struct {
		path      string
		shouldErr bool
	}{
		{"normal.txt", false},
		{"subdir/file.txt", false},
		{"deep/nested/path/file.txt", false},
		{"../../../etc/passwd", true},
		{"..\\windows\\system32", true},
		{".../.../.../etc/passwd", true},
	}

	for _, tt := range tests {
		_, err := SanitizePath(baseDir, tt.path)
		if (err != nil) != tt.shouldErr {
			t.Errorf("SanitizePath(%q) error = %v, shouldErr %v", tt.path, err, tt.shouldErr)
		}
	}
}

func TestExtensionWhitelistValidation(t *testing.T) {
	testFiles := []struct {
		filename string
		allowed  []string
		expected bool
	}{
		{"image.png", AllowedImageExtensions, true},
		{"image.jpg", AllowedImageExtensions, true},
		{"image.jpeg", AllowedImageExtensions, true},
		{"image.gif", AllowedImageExtensions, true},
		{"image.webp", AllowedImageExtensions, true},
		{"image.avif", AllowedImageExtensions, true},
		{"image.bmp", AllowedImageExtensions, true},
		{"image.svg", AllowedImageExtensions, true},
		{"malicious.exe", AllowedImageExtensions, false},
		{"malicious.sh", AllowedImageExtensions, false},
		{"malicious.bat", AllowedImageExtensions, false},
		{"malicious.php", AllowedImageExtensions, false},
		{"document.txt", AllowedTextExtensions, true},
		{"document.json", AllowedTextExtensions, true},
		{"document.html", AllowedTextExtensions, true},
		{"document.md", AllowedTextExtensions, true},
		{"malicious.exe", AllowedTextExtensions, false},
		{"data.hex", AllowedDataExtensions, true},
		{"data.bin", AllowedDataExtensions, true},
		{"malicious.txt", AllowedDataExtensions, false},
	}

	for _, tt := range testFiles {
		result := ValidateExtension(tt.filename, tt.allowed)
		if result != tt.expected {
			t.Errorf("ValidateExtension(%q, %v) = %v, want %v", tt.filename, tt.allowed, result, tt.expected)
		}
	}
}

func TestControlCharacterHandling(t *testing.T) {
	attackVectors := []string{
		"\x00file.txt",
		"file\x00.txt",
		"\x01\x02\x03file.txt",
		"file\x01.txt",
		"\x7ffile.txt",
		"file\x7f.txt",
	}

	for _, payload := range attackVectors {
		safe := SanitizeFilename(payload)
		if safe == "" || safe == "file" {
			continue
		}

		if hasControlCharacters(safe) {
			t.Errorf("SanitizeFilename(%q) still contains control characters: %q", payload, safe)
		}
	}
}

func TestLongFilenameHandling(t *testing.T) {
	longFilename := make([]byte, 500)
	for i := range longFilename {
		longFilename[i] = 'a'
	}
	filename := string(longFilename) + ".txt"

	safe := SanitizeFilename(filename)
	if len(safe) > 255 {
		t.Errorf("SanitizeFilename() returned filename longer than 255 chars: %d", len(safe))
	}

	if hasPathSeparators(safe) {
		t.Errorf("SanitizeFilename() returned filename with path separators: %s", safe)
	}
}

func TestNullByteHandling(t *testing.T) {
	attackVectors := []string{
		"\x00image.png",
		"image\x00.png",
		"ima\x00ge.png",
		"/tmp/\x00/../../etc/passwd",
	}

	for _, payload := range attackVectors {
		safe := SanitizeFilename(payload)
		if safe == "" || safe == "file" {
			continue
		}

		if hasNullBytes(safe) {
			t.Errorf("SanitizeFilename(%q) still contains null bytes: %q", payload, safe)
		}
	}
}

func TestUnicodeHandling(t *testing.T) {
	tests := []struct {
		input string
		safe  bool
	}{
		{"normal.png", true},
		{"image-中文字符.png", true},
		{"image-日本語.png", true},
		{"image-한글.png", true},
		{"image-العربية.png", true},
		{"image-😀.png", true},
		{"image\u202e.png", false},
		{"image\u202a.png", false},
		{"image\u202d.png", false},
	}

	for _, tt := range tests {
		safe := SanitizeFilename(tt.input)
		if !tt.safe && (hasPathSeparators(safe) || containsPathTraversal(safe)) {
			t.Errorf("SanitizeFilename(%q) returned unsafe filename: %s", tt.input, safe)
		}
	}
}

func TestSafeFilePathFunction(t *testing.T) {
	tests := []struct {
		baseDir  string
		filename string
		wantSafe bool
	}{
		{"/uploads", "normal.png", true},
		{"/uploads", "image.jpg", true},
		{"/uploads", "../../../etc/passwd", true},
		{"/uploads", "/etc/passwd", true},
		{"/uploads", "\x00file.txt", true},
	}

	for _, tt := range tests {
		result := SafeFilePath(tt.baseDir, tt.filename)
		if !tt.wantSafe {
			continue
		}

		if containsPathTraversal(result) {
			t.Errorf("SafeFilePath(%q, %q) returned unsafe path: %s", tt.baseDir, tt.filename, result)
		}

		if !hasValidPrefix(result, tt.baseDir) {
			t.Errorf("SafeFilePath(%q, %q) doesn't start with base: %s", tt.baseDir, tt.filename, result)
		}
	}
}

func TestSanitizePathAbsolutePaths(t *testing.T) {
	baseDir := "/safe/base"

	absolutePaths := []string{
		"/etc/passwd",
		"/tmp/file.txt",
		"/var/log/system.log",
	}

	for _, path := range absolutePaths {
		_, err := SanitizePath(baseDir, path)
		if err == nil {
			t.Errorf("SanitizePath(%q) should reject absolute path", path)
		}
	}
}

func TestCombinedAttacks(t *testing.T) {
	combinedAttacks := []string{
		"\x00../../../etc/passwd",
		"....//....//....//etc/passwd",
		"file\x00../../../etc/passwd",
		"image.png\x00../../../etc/passwd",
		"\x7f../../../etc/passwd",
	}

	for _, attack := range combinedAttacks {
		safe := SanitizeFilename(attack)
		if safe == "" || safe == "file" {
			continue
		}

		if containsPathTraversal(safe) || hasPathSeparators(safe) {
			t.Errorf("SanitizeFilename(%q) vulnerable to combined attack: %s", attack, safe)
		}
	}
}

func containsPathTraversal(path string) bool {
	return containsAny(path, []string{"../", "..\\", "%2e%2e", "%252e", "%5c", "%5f"})
}

func hasPathSeparators(path string) bool {
	for i := 0; i < len(path); i++ {
		if path[i] == '/' || path[i] == '\\' {
			return true
		}
	}
	return false
}

func hasControlCharacters(s string) bool {
	for _, r := range s {
		if r < 32 || r == 127 {
			return true
		}
	}
	return false
}

func hasNullBytes(s string) bool {
	for _, r := range s {
		if r == 0 {
			return true
		}
	}
	return false
}

func hasValidPrefix(path, baseDir string) bool {
	return containsPrefix(path, baseDir) || containsPrefix(path, ".")
}

func containsAny(s string, substrings []string) bool {
	for _, substr := range substrings {
		if containsPrefix(s, substr) {
			return true
		}
	}
	return false
}

func containsPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}
