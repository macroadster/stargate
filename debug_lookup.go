package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func testBlockLookup(baseDir string, blockHeight int64, txID string) {
	fmt.Printf("Testing block directory lookup for tx %s, height %d\n", txID, blockHeight)
	
	// Test the pattern matching
	blockDirPattern := filepath.Join(baseDir, fmt.Sprintf("%d_*", blockHeight))
	fmt.Printf("Pattern: %s\n", blockDirPattern)
	
	matches, err := filepath.Glob(blockDirPattern)
	if err != nil {
		fmt.Printf("Error with glob: %v\n", err)
		return
	}
	
	fmt.Printf("Found %d matches: %v\n", len(matches), matches)
	
	if len(matches) == 0 {
		legacyDir := filepath.Join(baseDir, fmt.Sprintf("%d_00000000", blockHeight))
		fmt.Printf("Trying legacy dir: %s\n", legacyDir)
		if _, err := os.Stat(legacyDir); err == nil {
			matches = []string{legacyDir}
			fmt.Printf("Legacy dir exists\n")
		}
	}
	
	if len(matches) > 0 {
		blockDir := matches[0]
		fmt.Printf("Using block dir: %s\n", blockDir)
		
		inscriptionsPath := filepath.Join(blockDir, "inscriptions.json")
		fmt.Printf("Inscriptions path: %s\n", inscriptionsPath)
		
		if _, err := os.Stat(inscriptionsPath); err == nil {
			fmt.Printf("✓ inscriptions.json exists\n")
			
			// List files in images directory
			imagesDir := filepath.Join(blockDir, "images")
			if files, err := os.ReadDir(imagesDir); err == nil {
				fmt.Printf("Files in images directory:\n")
				for _, file := range files {
					fileName := file.Name()
					if strings.Contains(fileName, txID[:8]) || strings.Contains(fileName, txID[len(txID)-8:]) {
						fmt.Printf("  - %s (matches txid pattern)\n", fileName)
					} else {
						fmt.Printf("  - %s\n", fileName)
					}
				}
			} else {
				fmt.Printf("✗ Cannot read images directory: %v\n", err)
			}
		} else {
			fmt.Printf("✗ inscriptions.json not found: %v\n", err)
		}
	}
}

func main() {
	baseDir := "/data/blocks"
	blockHeight := int64(121662)
	txID := "143c0a0e10b4ee16e7f91b33117d5e3fa07dfefdb03568e18e24713054d7cf22"
	
	testBlockLookup(baseDir, blockHeight, txID)
}