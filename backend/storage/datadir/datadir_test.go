package datadir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A realistic 64-char hex hash for testing.
const testHash = "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

func TestPartPrefix(t *testing.T) {
	tests := []struct {
		key    string
		want   string
		wantOK bool
	}{
		{testHash, filepath.Join("ab", "cd", "ef"), true},
		{"ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890", filepath.Join("AB", "CD", "EF"), true},
		{"abc", "", false},
		{"", "", false},
		{"123456", filepath.Join("12", "34", "56"), true},
	}
	for _, tt := range tests {
		got, ok := PartPrefix(tt.key)
		if ok != tt.wantOK || got != tt.want {
			t.Errorf("PartPrefix(%q) = (%q, %v), want (%q, %v)", tt.key, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestPartPath(t *testing.T) {
	got := PartPath("/data/uploads", testHash)
	want := filepath.Join("/data/uploads", "ab", "cd", "ef", testHash)
	if got != want {
		t.Fatalf("PartPath = %q, want %q", got, want)
	}

	// Short key should fall back to flat.
	got = PartPath("/data/uploads", "short")
	want = filepath.Join("/data/uploads", "short")
	if got != want {
		t.Fatalf("PartPath(short) = %q, want %q", got, want)
	}
}

func TestPartResolve_PartitionedFirst(t *testing.T) {
	base := t.TempDir()

	// Create a file in the partitioned location.
	partDir := filepath.Join(base, "ab", "cd", "ef")
	os.MkdirAll(partDir, 0755)
	os.WriteFile(filepath.Join(partDir, testHash), []byte("new"), 0644)

	// Also create a file in the flat location.
	os.WriteFile(filepath.Join(base, testHash), []byte("old"), 0644)

	// Should prefer partitioned.
	got := PartResolve(base, testHash)
	if !strings.Contains(got, filepath.Join("ab", "cd", "ef")) {
		t.Fatalf("expected partitioned path, got %q", got)
	}
}

func TestPartResolve_FlatFallback(t *testing.T) {
	base := t.TempDir()

	// Only flat file exists.
	os.WriteFile(filepath.Join(base, testHash), []byte("old"), 0644)

	got := PartResolve(base, testHash)
	if got != filepath.Join(base, testHash) {
		t.Fatalf("expected flat fallback, got %q", got)
	}
}

func TestPartResolve_NeitherExists(t *testing.T) {
	base := t.TempDir()

	// Nothing exists — should return partitioned path for new creation.
	got := PartResolve(base, testHash)
	want := PartPath(base, testHash)
	if got != want {
		t.Fatalf("expected new partitioned path %q, got %q", want, got)
	}
}

func TestPartMkdirAll(t *testing.T) {
	base := t.TempDir()
	dir, err := PartMkdirAll(base, testHash, 0755)
	if err != nil {
		t.Fatal(err)
	}
	want := PartPath(base, testHash)
	if dir != want {
		t.Fatalf("PartMkdirAll = %q, want %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatal("directory not created")
	}
}

func TestResolveUploadRelPath_FlatHash(t *testing.T) {
	base := t.TempDir()

	// Put file in partitioned location.
	partDir := filepath.Join(base, "ab", "cd", "ef")
	os.MkdirAll(partDir, 0755)
	os.WriteFile(filepath.Join(partDir, testHash), []byte("x"), 0644)

	got := ResolveUploadRelPath(base, testHash)
	if !strings.Contains(got, filepath.Join("ab", "cd", "ef", testHash)) {
		t.Fatalf("expected partitioned resolution, got %q", got)
	}
}

func TestResolveUploadRelPath_Results(t *testing.T) {
	base := t.TempDir()

	// Put results dir in partitioned location.
	partDir := filepath.Join(base, "results", "ab", "cd", "ef", testHash)
	os.MkdirAll(partDir, 0755)
	os.WriteFile(filepath.Join(partDir, "index.html"), []byte("<html>"), 0644)

	got := ResolveUploadRelPath(base, "results/"+testHash+"/index.html")
	if !strings.Contains(got, filepath.Join("ab", "cd", "ef", testHash, "index.html")) {
		t.Fatalf("expected partitioned result path, got %q", got)
	}
}

func TestResolveUploadRelPath_NonHash(t *testing.T) {
	base := t.TempDir()

	// Non-hash filename should use flat path.
	got := ResolveUploadRelPath(base, "12345_photo.png")
	want := filepath.Join(base, "12345_photo.png")
	if got != want {
		t.Fatalf("non-hash: got %q, want %q", got, want)
	}
}

func TestMigrateUploads(t *testing.T) {
	base := t.TempDir()

	// Seed flat hash files.
	hash1 := "aaaaaaaaaa1111111111bbbbbbbbbb2222222222cccccccccc3333333333dd44"
	hash2 := "eeeeeeeeee5555555555ffffffffff6666666666000000000077777777778888"
	os.WriteFile(filepath.Join(base, hash1), []byte("file1"), 0644)
	os.WriteFile(filepath.Join(base, hash2), []byte("file2"), 0644)

	// Seed a non-hash file that should NOT be moved.
	os.WriteFile(filepath.Join(base, "readme.txt"), []byte("keep"), 0644)

	// Seed a flat result directory.
	hash3 := "1111111111222222222233333333334444444444555555555566666666667777"
	resultsDir := filepath.Join(base, "results", hash3)
	os.MkdirAll(resultsDir, 0755)
	os.WriteFile(filepath.Join(resultsDir, "index.html"), []byte("<html>"), 0644)

	// Run migration.
	if err := MigrateUploads(base); err != nil {
		t.Fatalf("MigrateUploads: %v", err)
	}

	// Verify flat files moved.
	for _, h := range []string{hash1, hash2} {
		partitioned := PartPath(base, h)
		if _, err := os.Stat(partitioned); err != nil {
			t.Errorf("expected partitioned file at %s", partitioned)
		}
		if _, err := os.Stat(filepath.Join(base, h)); err == nil {
			t.Errorf("flat file %s should have been moved", h)
		}
	}

	// Verify non-hash file stayed.
	if _, err := os.Stat(filepath.Join(base, "readme.txt")); err != nil {
		t.Error("non-hash file should not be moved")
	}

	// Verify results directory moved.
	partRes := PartPath(filepath.Join(base, "results"), hash3)
	if _, err := os.Stat(filepath.Join(partRes, "index.html")); err != nil {
		t.Errorf("expected partitioned results at %s: %v", partRes, err)
	}
	if _, err := os.Stat(filepath.Join(base, "results", hash3)); err == nil {
		t.Error("flat results dir should have been moved")
	}

	// Verify marker file.
	if _, err := os.Stat(filepath.Join(base, migrateMarker)); err != nil {
		t.Error("marker file not created")
	}

	// Second call should be a no-op.
	if err := MigrateUploads(base); err != nil {
		t.Fatalf("second MigrateUploads: %v", err)
	}
}

func TestMigrateUploads_EmptyDir(t *testing.T) {
	base := t.TempDir()
	if err := MigrateUploads(base); err != nil {
		t.Fatalf("MigrateUploads on empty dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, migrateMarker)); err != nil {
		t.Error("marker file should be written even for empty dir")
	}
}

func TestMigrateUploads_NonexistentDir(t *testing.T) {
	if err := MigrateUploads("/nonexistent/path/that/does/not/exist"); err != nil {
		t.Fatalf("should silently return nil for missing dir: %v", err)
	}
}

func TestIsHexHash(t *testing.T) {
	if !IsHexHash(testHash) {
		t.Error("should accept valid 64-char hex")
	}
	if IsHexHash("not-a-hash") {
		t.Error("should reject non-hex")
	}
	if IsHexHash("abcdef") {
		t.Error("should reject short hex")
	}
	if IsHexHash(testHash + "ff") {
		t.Error("should reject >64 chars")
	}
}
