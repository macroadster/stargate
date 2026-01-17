package security

import (
	"strings"
	"testing"
)

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
		{"test\x00file.txt", "testfile.txt"},
		{"\x01\x02test.txt", "test.txt"},
		{"a.txt", "a.txt"},
		{strings.Repeat("a", 300) + ".txt", strings.Repeat("a", 255)},
		{"image.png", "image.png"},
		{"/etc/passwd", "passwd"},
		{"./file.txt", "file.txt"},
		{".hidden", ".hidden"},
		{"...test...", "...test..."},
	}

	for _, tt := range tests {
		result := SanitizeFilename(tt.input)
		if result != tt.expected {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
		}

		if strings.Contains(result, "../") || strings.Contains(result, "..\\") {
			t.Errorf("SanitizeFilename(%q) still contains path traversal: %s", tt.input, result)
		}

		if strings.Contains(result, "/") || strings.Contains(result, "\\") {
			t.Errorf("SanitizeFilename(%q) still contains path separators: %s", tt.input, result)
		}
	}
}

func TestSanitizePath(t *testing.T) {
	baseDir := "/safe/base"

	tests := []struct {
		path      string
		shouldErr bool
	}{
		{"file.txt", false},
		{"subdir/file.txt", false},
		{"deep/nested/path/file.txt", false},
		{"../../../etc/passwd", true},
		{"../outside.txt", true},
		{"/etc/passwd", true},
		{"./../escape.txt", true},
		{"..\\escape.txt", true},
		{"normal_path/file.txt", false},
	}

	for _, tt := range tests {
		result, err := SanitizePath(baseDir, tt.path)
		if (err != nil) != tt.shouldErr {
			t.Errorf("SanitizePath(%q) error = %v, shouldErr %v", tt.path, err, tt.shouldErr)
		}

		if !tt.shouldErr {
			if !strings.HasPrefix(result, baseDir+string("/")) {
				t.Errorf("SanitizePath(%q) = %q, should start with %s", tt.path, result, baseDir)
			}

			if strings.Contains(result, "../") || strings.Contains(result, "..\\") {
				t.Errorf("SanitizePath(%q) = %q, still contains path traversal", tt.path, result)
			}
		}
	}
}

func TestValidateExtension(t *testing.T) {
	tests := []struct {
		filename string
		allowed  []string
		expected bool
	}{
		{"image.png", []string{".png", ".jpg"}, true},
		{"image.JPG", []string{".png", ".jpg"}, true},
		{"image.PnG", []string{".png", ".jpg"}, true},
		{"image.gif", []string{".png", ".jpg"}, false},
		{"image.jpeg", []string{".jpg", ".jpeg"}, true},
		{"document.txt", AllowedTextExtensions, true},
		{"document.md", AllowedTextExtensions, true},
		{"script.exe", AllowedImageExtensions, false},
		{"archive.zip", AllowedImageExtensions, false},
		{"noextension", []string{".txt"}, false},
		{"", []string{".txt"}, false},
		{"file.txt", []string{}, false},
		{"file.txt", nil, false},
		{".hiddenfile", []string{".txt"}, false},
	}

	for _, tt := range tests {
		result := ValidateExtension(tt.filename, tt.allowed)
		if result != tt.expected {
			t.Errorf("ValidateExtension(%q, %v) = %v, want %v", tt.filename, tt.allowed, result, tt.expected)
		}
	}
}

func TestSafeFilePath(t *testing.T) {
	tests := []struct {
		baseDir  string
		filename string
		wantBase string
	}{
		{"/uploads", "file.txt", "/uploads"},
		{"/uploads", "dir/file.txt", "/uploads"},
		{"/uploads", "../../../etc/passwd", "/uploads"},
		{"/uploads", "", "/uploads"},
		{"/uploads", "..", "/uploads"},
		{"/uploads", ".", "/uploads"},
	}

	for _, tt := range tests {
		result := SafeFilePath(tt.baseDir, tt.filename)
		if !strings.HasPrefix(result, tt.wantBase+string("/")) && result != tt.wantBase {
			t.Errorf("SafeFilePath(%q, %q) = %q, should start with %s", tt.baseDir, tt.filename, result, tt.wantBase)
		}

		if strings.Contains(result, "../") || strings.Contains(result, "..\\") {
			t.Errorf("SafeFilePath(%q, %q) = %q, contains path traversal", tt.baseDir, tt.filename, result)
		}
	}
}

func TestValidateExtensionWithAllowedLists(t *testing.T) {
	if !ValidateExtension("image.png", AllowedImageExtensions) {
		t.Error("Expected PNG to be in allowed image extensions")
	}

	if !ValidateExtension("document.txt", AllowedTextExtensions) {
		t.Error("Expected TXT to be in allowed text extensions")
	}

	if !ValidateExtension("data.hex", AllowedDataExtensions) {
		t.Error("Expected HEX to be in allowed data extensions")
	}

	if ValidateExtension("malicious.exe", AllowedImageExtensions) {
		t.Error("Expected EXE to NOT be in allowed image extensions")
	}

	if ValidateExtension("malicious.sh", AllowedTextExtensions) {
		t.Error("Expected SH to NOT be in allowed text extensions")
	}
}
