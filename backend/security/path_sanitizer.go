package security

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SanitizeFilename removes dangerous path sequences and normalizes filename
func SanitizeFilename(filename string) string {
	if filename == "" {
		return "file"
	}

	filename = filepath.Base(filename)

	filename = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, filename)

	if len(filename) > 255 {
		filename = filename[:255]
	}

	if filename == "" || filename == "." || filename == ".." {
		filename = "file"
	}

	return filename
}

// SanitizePath validates a path is safe and doesn't escape base directory
func SanitizePath(baseDir, userPath string) (string, error) {
	if baseDir == "" || userPath == "" {
		return "", fmt.Errorf("invalid path parameters")
	}

	if strings.Contains(userPath, "../") || strings.Contains(userPath, "..\\") {
		return "", fmt.Errorf("path traversal detected: %s", userPath)
	}

	if filepath.IsAbs(userPath) {
		return "", fmt.Errorf("absolute path not allowed: %s", userPath)
	}

	fullPath := filepath.Join(baseDir, userPath)
	cleanPath := filepath.Clean(fullPath)

	baseDir = filepath.Clean(baseDir)
	if !strings.HasPrefix(cleanPath, baseDir+string(filepath.Separator)) && cleanPath != baseDir {
		return "", fmt.Errorf("path traversal detected: %s", userPath)
	}

	return cleanPath, nil
}

// ValidateExtension checks if file extension is in whitelist
func ValidateExtension(filename string, allowed []string) bool {
	if filename == "" || len(allowed) == 0 {
		return false
	}

	ext := strings.ToLower(filepath.Ext(filename))
	ext = strings.TrimPrefix(ext, ".")

	for _, allowedExt := range allowed {
		allowedLower := strings.ToLower(allowedExt)
		allowedLower = strings.TrimPrefix(allowedLower, ".")
		if allowedLower == ext {
			return true
		}
	}
	return false
}

// SafeFilePath constructs a safe file path preventing traversal
func SafeFilePath(baseDir, filename string) string {
	if baseDir == "" {
		baseDir = "."
	}

	sanitized := SanitizeFilename(filename)
	return filepath.Join(baseDir, sanitized)
}
