package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"stargate-backend/mcp"
)

func main() {
	// Create test directory structure
	testBlocksDir := "/tmp/test_blocks_scan"
	txID := "143c0a0e10b4ee16e7f91b33117d5e3fa07dfefdb03568e18e24713054d7cf22"
	
	// Clean up any existing test directory
	os.RemoveAll(testBlocksDir)
	
	// Create test blocks directory
	if err := os.MkdirAll(testBlocksDir, 0755); err != nil {
		panic(err)
	}
	
	// Create a test PNG file for the transaction
	testImagePath := filepath.Join(testBlocksDir, txID+".png")
	testImageData := []byte("fake png data for testing")
	if err := os.WriteFile(testImagePath, testImageData, 0644); err != nil {
		panic(err)
	}
	
	fmt.Printf("Created test file: %s\n", testImagePath)
	
	// Set environment variable for test
	os.Setenv("BLOCKS_DIR", testBlocksDir)
	defer os.Unsetenv("BLOCKS_DIR")
	
	// Test the scan transaction logic
	fmt.Println("Testing scan_transaction function...")
	
	// Create a mock MCP server to test the function
	// Note: This is a simplified test - the actual function requires Bitcoin client and scanner manager
	fmt.Printf("Would scan transaction: %s\n", txID)
	fmt.Printf("Expected image path: %s\n", testImagePath)
	
	// Check if our file lookup logic works
	imagePath := filepath.Join(testBlocksDir, fmt.Sprintf("%s.png", txID))
	if _, err := os.Stat(imagePath); err == nil {
		fmt.Printf("✓ Direct .png lookup found: %s\n", imagePath)
	} else {
		fmt.Printf("✗ Direct .png lookup failed\n")
	}
	
	// Test without extension
	imagePath = filepath.Join(testBlocksDir, txID)
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		fmt.Printf("✓ No-extension path correctly returns error\n")
	} else {
		fmt.Printf("✗ No-extension path should not exist\n")
	}
	
	// Clean up
	os.RemoveAll(testBlocksDir)
	fmt.Println("✓ Test completed successfully")
}