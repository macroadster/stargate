package ipfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestClientEmbeddedNode(t *testing.T) {
	// Set up temporary repo
	tmpRepo, err := os.MkdirTemp("", "ipfs-repo-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpRepo)

	os.Setenv("IPFS_ENABLED", "true")
	os.Setenv("IPFS_EMBEDDED_ENABLED", "true")
	os.Setenv("IPFS_EMBEDDED_REPO", tmpRepo)
	os.Setenv("IPFS_EMBEDDED_LISTEN", "/ip4/127.0.0.1/tcp/0") // Random port
	os.Setenv("IPFS_API_URL", "http://127.0.0.1:9999") // Avoid local node

	client := NewClientFromEnv()
	if client == nil {
		t.Fatal("expected client to be enabled")
	}
	if client.embedded == nil {
		t.Fatal("expected embedded node to be initialized")
	}
	defer client.Close()

	ctx := context.Background()
	testData := []byte("hello embedded ipfs")
	
	// Test AddBytes using embedded node
	cid, err := client.AddBytes(ctx, "test.txt", testData)
	if err != nil {
		t.Fatalf("AddBytes failed: %v", err)
	}
	
	if cid == "" {
		t.Fatal("expected non-empty CID")
	}
	t.Logf("Added CID: %s", cid)
	
	// Test Cat using embedded node
	data, err := client.Cat(ctx, cid)
	if err != nil {
		t.Fatalf("Cat failed: %v", err)
	}
	
	if string(data) != string(testData) {
		t.Errorf("expected %s, got %s", testData, data)
	}
}

func TestClientNativeFallback(t *testing.T) {
	// Set up temporary storage
	tmpDir, err := os.MkdirTemp("", "ipfs-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	os.Setenv("IPFS_ENABLED", "true")
	os.Setenv("IPFS_EMBEDDED_ENABLED", "false") // Disable embedded for this test
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
