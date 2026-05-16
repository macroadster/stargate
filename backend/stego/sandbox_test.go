package stego

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashSandboxDir(t *testing.T) {
	dir := t.TempDir()
	// Create files in non-sorted order to test determinism.
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("hello b"), 0644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello a"), 0644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "sub", "c.txt"), []byte("hello c"), 0644)

	hash1, err := HashSandboxDir(dir)
	if err != nil {
		t.Fatalf("HashSandboxDir: %v", err)
	}
	if len(hash1) != 64 {
		t.Fatalf("expected 64-char hex hash, got %d chars: %s", len(hash1), hash1)
	}

	// Re-hash should be deterministic.
	hash2, err := HashSandboxDir(dir)
	if err != nil {
		t.Fatalf("second HashSandboxDir: %v", err)
	}
	if hash1 != hash2 {
		t.Fatalf("non-deterministic: %s != %s", hash1, hash2)
	}

	// Verify should pass.
	if err := VerifySandboxHash(dir, hash1); err != nil {
		t.Fatalf("VerifySandboxHash: %v", err)
	}

	// Verify should fail with wrong hash.
	if err := VerifySandboxHash(dir, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("VerifySandboxHash should fail with wrong hash")
	}
}

func TestHashSandboxDirEmpty(t *testing.T) {
	dir := t.TempDir()
	_, err := HashSandboxDir(dir)
	if err == nil {
		t.Fatal("expected error for empty dir")
	}
}

func TestWriteSandboxTarball(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content"), 0644)

	outPath := filepath.Join(t.TempDir(), "sandbox.tar")
	hash, err := WriteSandboxTarball(dir, outPath)
	if err != nil {
		t.Fatalf("WriteSandboxTarball: %v", err)
	}

	// Hash from WriteSandboxTarball should match HashSandboxDir.
	expected, _ := HashSandboxDir(dir)
	if hash != expected {
		t.Fatalf("hash mismatch: WriteSandboxTarball=%s, HashSandboxDir=%s", hash, expected)
	}

	// File should exist and be non-empty.
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("stat tarball: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("tarball is empty")
	}
}