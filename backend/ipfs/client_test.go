package ipfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestClientNativeFallback(t *testing.T) {
	// Set up temporary storage
	tmpDir, err := os.MkdirTemp("", "ipfs-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	os.Setenv("IPFS_ENABLED", "true")
	os.Setenv("IPFS_STORAGE_DIR", tmpDir)
	// Point to non-existent node to force fallback
	os.Setenv("IPFS_API_URL", "http://127.0.0.1:9999")

	client := NewClientFromEnv()
	if client == nil {
		t.Fatal("expected client to be enabled")
	}

	ctx := context.Background()
	testData := []byte("hello native ipfs")
	
	// Test AddBytes fallback to local storage
	cid, err := client.AddBytes(ctx, "test.txt", testData)
	if err != nil {
		t.Fatalf("AddBytes failed: %v", err)
	}
	
	if cid == "" {
		t.Fatal("expected non-empty CID")
	}
	
	// Verify it's in the local storage dir
	localPath := filepath.Join(tmpDir, cid)
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		t.Errorf("expected local file %s to exist", localPath)
	}

	// Test Cat fallback to local storage
	data, err := client.Cat(ctx, cid)
	if err != nil {
		t.Fatalf("Cat failed: %v", err)
	}
	
	if string(data) != string(testData) {
		t.Errorf("expected %s, got %s", testData, data)
	}
}

func TestClientGatewayFallback(t *testing.T) {
	// Set up temporary storage for caching
	tmpDir, err := os.MkdirTemp("", "ipfs-test-gw-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	os.Setenv("IPFS_ENABLED", "true")
	os.Setenv("IPFS_STORAGE_DIR", tmpDir)
	os.Setenv("IPFS_API_URL", "http://127.0.0.1:9999")

	client := NewClientFromEnv()
	
	// Use a CID that should be on public gateways (IPFS whitepaper)
	cid := "QmYwAPJ9mkWgpbS65rnMDN9XoSREvS9uYshYxndk387/about"
	
	ctx := context.Background()
	data, err := client.Cat(ctx, cid)
	if err != nil {
		// Gateways might be flaky in CI, so we'll just log instead of failing
		t.Logf("Gateway Cat failed (expected if offline/flaky): %v", err)
		return
	}
	
	if len(data) == 0 {
		t.Error("expected non-empty data from gateway")
	}
	
	// Verify it was cached locally
	localPath := filepath.Join(tmpDir, cid)
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		t.Errorf("expected local cache file %s to exist", localPath)
	}
}
