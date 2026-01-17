package storage

import (
	"stargate-backend/security"
	"testing"
)

func TestReadTextContent_PathTraversalProtection(t *testing.T) {
	attackVectors := []string{
		"../../../etc/passwd",
		"../../uploads/../../../etc/passwd",
		"....//....//....//etc/passwd",
		"/etc/passwd",
	}

	for _, filePath := range attackVectors {
		_, err := security.SanitizePath("/safe/base", filePath)
		if err == nil {
			t.Errorf("SanitizePath should reject path traversal: %s", filePath)
		}
	}
}

func TestReadTextContent_AbsolutePathsRejected(t *testing.T) {
	absolutePaths := []string{
		"/etc/passwd",
		"/tmp/file.txt",
		"/var/log/system.log",
	}

	for _, filePath := range absolutePaths {
		_, err := security.SanitizePath("/safe/base", filePath)
		if err == nil {
			t.Errorf("SanitizePath should reject absolute path: %s", filePath)
		}
	}
}

func TestReadTextContent_SafePathsAccepted(t *testing.T) {
	safePaths := []string{
		"normal.txt",
		"subdir/file.txt",
		"deep/nested/path/file.txt",
	}

	for _, filePath := range safePaths {
		result, err := security.SanitizePath("/safe/base", filePath)
		if err != nil {
			t.Errorf("SanitizePath should accept safe path: %s (error: %v)", filePath, err)
		}
		if result == "" {
			t.Errorf("SanitizePath returned empty path for: %s", filePath)
		}
	}
}
