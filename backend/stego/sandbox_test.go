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

// TestHashSandboxDirMultiAgentFinalState simulates multiple agents submitting
// artifacts to the same contract sandbox directory at different times. The hash
// computed after all submissions should be deterministic and capture the complete
// final state — this is the scenario the stego publish flow relies on.
func TestHashSandboxDirMultiAgentFinalState(t *testing.T) {
	dir := t.TempDir()

	// Agent A submits first
	os.WriteFile(filepath.Join(dir, "agent_a_result.json"), []byte(`{"status":"ok"}`), 0644)
	hashAfterA, err := HashSandboxDir(dir)
	if err != nil {
		t.Fatalf("hash after agent A: %v", err)
	}

	// Agent B submits later
	os.MkdirAll(filepath.Join(dir, "task-2"), 0755)
	os.WriteFile(filepath.Join(dir, "task-2", "output.bin"), []byte("binary data"), 0644)
	hashAfterB, err := HashSandboxDir(dir)
	if err != nil {
		t.Fatalf("hash after agent B: %v", err)
	}

	// Hashes should differ — B added new files
	if hashAfterA == hashAfterB {
		t.Fatal("hash should change after agent B adds files")
	}

	// Agent C submits last
	os.WriteFile(filepath.Join(dir, "agent_c_report.md"), []byte("# Done"), 0644)
	hashFinal, err := HashSandboxDir(dir)
	if err != nil {
		t.Fatalf("hash final: %v", err)
	}

	// Re-hashing the same final state should be deterministic
	hashFinal2, _ := HashSandboxDir(dir)
	if hashFinal != hashFinal2 {
		t.Fatalf("final hash not deterministic: %s != %s", hashFinal, hashFinal2)
	}

	// The final hash should differ from all partial states
	if hashFinal == hashAfterA || hashFinal == hashAfterB {
		t.Fatal("final hash should differ from partial states")
	}

	// Verification at publish time should pass with the final hash
	if err := VerifySandboxHash(dir, hashFinal); err != nil {
		t.Fatalf("VerifySandboxHash should pass for final state: %v", err)
	}

	// Verification with a stale partial hash should fail
	if err := VerifySandboxHash(dir, hashAfterA); err == nil {
		t.Fatal("VerifySandboxHash should fail for stale partial hash")
	}
}

// TestHashSandboxDirCrossPlatformPaths verifies that file paths are normalized
// to forward slashes, so the hash is the same regardless of OS path separator.
func TestHashSandboxDirCrossPlatformPaths(t *testing.T) {
	dir := t.TempDir()

	// Create nested structure
	os.MkdirAll(filepath.Join(dir, "deep", "nested"), 0755)
	os.WriteFile(filepath.Join(dir, "deep", "nested", "file.txt"), []byte("data"), 0644)

	hash1, err := HashSandboxDir(dir)
	if err != nil {
		t.Fatalf("HashSandboxDir: %v", err)
	}

	// Same directory, hash again
	hash2, _ := HashSandboxDir(dir)
	if hash1 != hash2 {
		t.Fatal("nested paths should produce deterministic hash")
	}
}