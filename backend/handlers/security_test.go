package handlers

import (
	"stargate-backend/security"
	"strings"
	"testing"
)

func TestHandleCreateInscription_PathTraversalProtection(t *testing.T) {
	attackVectors := []string{
		"../../../etc/passwd",
		"../../uploads/../../../etc/passwd",
		"....//....//....//etc/passwd",
	}

	for _, filename := range attackVectors {
		safe := security.SafeFilePath("/uploads", filename)
		if safe == "" || safe == "file" {
			continue
		}

		if !strings.HasPrefix(safe, "/uploads/") && safe != "/uploads" {
			t.Errorf("SafeFilePath doesn't start with base: %s", safe)
		}

		if containsTraversal(safe) {
			t.Errorf("SafeFilePath still allows path traversal: %s", safe)
		}
	}
}

func TestUploadSecurity_MaliciousFilenamesRejected(t *testing.T) {
	maliciousFilenames := []string{
		"malicious.exe",
		"malicious.sh",
		"malicious.bat",
		"malicious.php",
		"malicious.asp",
		"malicious.jsp",
	}

	for _, filename := range maliciousFilenames {
		if security.ValidateExtension(filename, security.AllowedImageExtensions) {
			t.Errorf("ValidateExtension incorrectly allows malicious file: %s", filename)
		}
	}
}

func TestUploadSecurity_AllowedFilenamesAccepted(t *testing.T) {
	allowedFilenames := []string{
		"image.png",
		"image.jpg",
		"image.jpeg",
		"image.gif",
		"image.webp",
		"image.avif",
		"image.bmp",
		"image.svg",
	}

	for _, filename := range allowedFilenames {
		if !security.ValidateExtension(filename, security.AllowedImageExtensions) {
			t.Errorf("ValidateExtension incorrectly rejects allowed file: %s", filename)
		}
	}
}

func containsTraversal(path string) bool {
	traversalSequences := []string{"../", "..\\"}
	for _, seq := range traversalSequences {
		for i := 0; i <= len(path)-len(seq); i++ {
			if path[i:i+len(seq)] == seq {
				return true
			}
		}
	}
	return false
}

func hasSeparators(path string) bool {
	for i := 0; i < len(path); i++ {
		if path[i] == '/' || path[i] == '\\' {
			return true
		}
	}
	return false
}
