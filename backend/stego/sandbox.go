package stego

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HashSandboxDir creates a deterministic tarball of dir's contents and returns
// its SHA256 hex digest. The archive uses sorted paths, zero timestamps, and
// fixed ownership so the hash is reproducible across machines and runs.
func HashSandboxDir(dir string) (string, error) {
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("sandbox dir stat: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("sandbox path is not a directory: %s", dir)
	}

	// Collect all regular files, sorted by relative path.
	var files []string
	err = filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk sandbox dir: %w", err)
	}
	sort.Strings(files)

	if len(files) == 0 {
		return "", fmt.Errorf("sandbox dir is empty: %s", dir)
	}

	h := sha256.New()
	tw := tar.NewWriter(h)

	for _, rel := range files {
		full := filepath.Join(dir, rel)
		data, err := os.ReadFile(full)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", rel, err)
		}
		// Use forward-slash paths for cross-platform determinism.
		name := strings.ReplaceAll(rel, string(filepath.Separator), "/")
		hdr := &tar.Header{
			Name: name,
			Size: int64(len(data)),
			Mode: 0644,
			// Zero timestamps and fixed UID/GID for reproducibility.
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return "", fmt.Errorf("tar header %s: %w", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			return "", fmt.Errorf("tar write %s: %w", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return "", fmt.Errorf("tar close: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifySandboxHash checks that the sandbox directory at dir matches the
// expected SHA256 hex hash. Returns nil on match or an error describing the
// mismatch.
func VerifySandboxHash(dir, expectedHash string) error {
	actual, err := HashSandboxDir(dir)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expectedHash) {
		return fmt.Errorf("sandbox hash mismatch: expected %s got %s", expectedHash, actual)
	}
	return nil
}

// WriteSandboxTarball creates the deterministic tarball on disk and returns its
// SHA256 hex digest. Useful when the caller also needs the archive file (e.g.,
// for uploading to IPFS).
func WriteSandboxTarball(dir, outPath string) (string, error) {
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("sandbox dir stat: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("sandbox path is not a directory: %s", dir)
	}

	var files []string
	err = filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk sandbox dir: %w", err)
	}
	sort.Strings(files)

	if len(files) == 0 {
		return "", fmt.Errorf("sandbox dir is empty: %s", dir)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("create tarball: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	mw := io.MultiWriter(f, h)
	tw := tar.NewWriter(mw)

	for _, rel := range files {
		full := filepath.Join(dir, rel)
		data, err := os.ReadFile(full)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", rel, err)
		}
		name := strings.ReplaceAll(rel, string(filepath.Separator), "/")
		hdr := &tar.Header{
			Name: name,
			Size: int64(len(data)),
			Mode: 0644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return "", fmt.Errorf("tar header %s: %w", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			return "", fmt.Errorf("tar write %s: %w", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return "", fmt.Errorf("tar close: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}