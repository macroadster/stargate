package starlight

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"stargate-backend/storage/datadir"
)

// HF defaults for auto-download of starlight.gguf.
const (
	defaultHFRepo     = "macroadster/starlight-prod"
	defaultHFFile     = "starlight.gguf"
	defaultHFRevision = "main"
	// Large GGUF downloads can take a long time on slow links.
	hfDownloadTimeout = 30 * time.Minute
)

// hfBaseURL is the Hugging Face host used to build resolve URLs.
// Tests override this with an httptest server URL.
var hfBaseURL = "https://huggingface.co"

// httpClient is used for GGUF downloads. Tests may replace it.
var httpClient = http.DefaultClient

// DefaultTrinModelPath returns the well-known local path for starlight.gguf
// under STARGATE_DATA_DIR (or data/models/starlight.gguf).
func DefaultTrinModelPath() string {
	return datadir.Path("models/starlight.gguf")
}

// fileExistsRegular reports whether path exists and is a regular file.
func fileExistsRegular(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

// ResolveTrinModelPath returns an existing GGUF path using existence-checked order:
//  1. STARLIGHT_GGUF if set and file exists
//  2. STARLIGHT_TRIN_MODEL if set and file exists
//  3. DefaultTrinModelPath() if it exists
//
// Pure: no network. Returns "" when none of the candidates exist.
// Env values that point at missing paths are skipped (do not fail hard).
func ResolveTrinModelPath() string {
	if p := os.Getenv("STARLIGHT_GGUF"); p != "" && fileExistsRegular(p) {
		return p
	}
	if p := os.Getenv("STARLIGHT_TRIN_MODEL"); p != "" && fileExistsRegular(p) {
		return p
	}
	if def := DefaultTrinModelPath(); fileExistsRegular(def) {
		return def
	}
	return ""
}

// envTruthy reports whether v is a common true-ish string.
func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// EnsureTrinModel resolves a local GGUF path, downloading from Hugging Face into
// DefaultTrinModelPath when missing (or when STARLIGHT_GGUF_FORCE_DOWNLOAD is set).
// Returns "" on failure so callers can fall through to Alpha without crashing.
func EnsureTrinModel() string {
	force := envTruthy(os.Getenv("STARLIGHT_GGUF_FORCE_DOWNLOAD"))
	if !force {
		if p := ResolveTrinModelPath(); p != "" {
			return p
		}
	}

	dest := DefaultTrinModelPath()
	if err := downloadTrinModel(dest); err != nil {
		log.Printf("starlight GGUF auto-download failed (dest=%s): %v; Trin unavailable, Alpha fallthrough", dest, err)
		return ""
	}
	return dest
}

// hfDownloadURL builds the Hugging Face resolve URL for the configured repo/file.
func hfDownloadURL() string {
	repo := os.Getenv("STARLIGHT_HF_REPO")
	if repo == "" {
		repo = defaultHFRepo
	}
	file := os.Getenv("STARLIGHT_HF_FILE")
	if file == "" {
		file = defaultHFFile
	}
	revision := os.Getenv("STARLIGHT_HF_REVISION")
	if revision == "" {
		revision = defaultHFRevision
	}
	// https://huggingface.co/{repo}/resolve/{revision}/{file}
	return strings.TrimRight(hfBaseURL, "/") + "/" + repo + "/resolve/" + revision + "/" + file
}

// downloadTrinModel downloads the configured HF GGUF into destPath.
func downloadTrinModel(destPath string) error {
	return downloadFile(httpClient, hfDownloadURL(), destPath)
}

// downloadFile streams url into destPath via a same-dir partial file, then renames atomically.
// On non-2xx responses or I/O errors the partial file is removed and dest is left unchanged.
func downloadFile(client *http.Client, url, destPath string) error {
	if client == nil {
		client = http.DefaultClient
	}
	if destPath == "" {
		return fmt.Errorf("download dest path empty")
	}
	if url == "" {
		return fmt.Errorf("download url empty")
	}

	log.Printf("GGUF missing locally, downloading from HF… url=%s dest=%s", url, destPath)

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir model dir %s: %w", dir, err)
	}

	partial := destPath + ".partial"
	// Clean any leftover partial from a prior crash.
	_ = os.Remove(partial)

	ctx, cancel := context.WithTimeout(context.Background(), hfDownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download HTTP %s for %s", resp.Status, url)
	}

	f, err := os.OpenFile(partial, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create partial file %s: %w", partial, err)
	}

	written, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("stream download to %s: %w", partial, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("close partial file %s: %w", partial, closeErr)
	}

	if err := os.Rename(partial, destPath); err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("atomic rename %s -> %s: %w", partial, destPath, err)
	}

	log.Printf("GGUF download complete: path=%s size=%d", destPath, written)
	return nil
}
