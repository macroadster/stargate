package stego

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

// Decode limits protect against decompression bombs and pathological images
// from untrusted sources (Bitcoin inscriptions, user uploads).
const (
	// MaxDecodePixels is the maximum width*height accepted for full decode.
	// 64 megapixels ≈ 256MB for RGBA; enough for legitimate covers, small enough
	// to bound memory from crafted PNG/GIF headers.
	MaxDecodePixels = 64 * 1024 * 1024

	// MaxDecodeDimension is the max width or height of a single side.
	MaxDecodeDimension = 16384

	// MaxDecodeBytes is a soft byte-size ceiling for input buffers (10 MiB).
	// Callers may apply their own limits; this is enforced inside DecodeImage.
	MaxDecodeBytes = 10 * 1024 * 1024
)

// ErrUnsupportedFormat indicates the bytes are not a format this process can
// (or will) decode. Callers that scan untrusted images should treat this as
// non-stego / not-found rather than a hard failure.
var ErrUnsupportedFormat = errors.New("unsupported image format")

// ErrImageTooLarge indicates the image exceeds dimension or pixel-count limits.
var ErrImageTooLarge = errors.New("image exceeds safe decode limits")

// ErrImageTooLargeBytes indicates the raw buffer is too large.
var ErrImageTooLargeBytes = errors.New("image payload exceeds size limit")

// IsUnsupportedFormat reports whether err is (or wraps) ErrUnsupportedFormat.
func IsUnsupportedFormat(err error) bool {
	return err != nil && errors.Is(err, ErrUnsupportedFormat)
}

// IsSoftDecodeFailure reports decode failures that should not be treated as
// HTTP 500 or circuit-breaker failures: unsupported format or oversize image.
func IsSoftDecodeFailure(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrUnsupportedFormat) ||
		errors.Is(err, ErrImageTooLarge) ||
		errors.Is(err, ErrImageTooLargeBytes)
}

// DecodeImage safely decodes image bytes for steganography paths.
//
// Security notes:
//   - No AVIF/HEIC/libavif: native AV1 stacks have had remote memory-corruption
//     bugs; we intentionally leave those formats unsupported.
//   - DecodeConfig is used first so dimension limits apply before full rasterization.
//   - Only stdlib + golang.org/x/image formats are registered (png/jpeg/gif/webp/bmp).
func DecodeImage(data []byte) (image.Image, string, error) {
	if len(data) == 0 {
		return nil, "", fmt.Errorf("%w: empty payload", ErrUnsupportedFormat)
	}
	if len(data) > MaxDecodeBytes {
		return nil, "", fmt.Errorf("%w: %d bytes (max %d)", ErrImageTooLargeBytes, len(data), MaxDecodeBytes)
	}

	// Sniff known-unsupported containers early so we never hit opaque codec paths.
	if hint := SniffUnsupportedFormat(data); hint != "" {
		return nil, "", fmt.Errorf("%w: %s", ErrUnsupportedFormat, hint)
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrUnsupportedFormat, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, format, fmt.Errorf("%w: invalid dimensions %dx%d", ErrUnsupportedFormat, cfg.Width, cfg.Height)
	}
	if cfg.Width > MaxDecodeDimension || cfg.Height > MaxDecodeDimension {
		return nil, format, fmt.Errorf("%w: %dx%d (max side %d)", ErrImageTooLarge, cfg.Width, cfg.Height, MaxDecodeDimension)
	}
	pixels := int64(cfg.Width) * int64(cfg.Height)
	if pixels > MaxDecodePixels {
		return nil, format, fmt.Errorf("%w: %dx%d (%d pixels, max %d)", ErrImageTooLarge, cfg.Width, cfg.Height, pixels, MaxDecodePixels)
	}

	img, format2, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// Corrupt payload of a known container: still soft-fail for scanners.
		if strings.Contains(err.Error(), "unknown format") {
			return nil, "", fmt.Errorf("%w: %v", ErrUnsupportedFormat, err)
		}
		return nil, format, fmt.Errorf("%w: decode failed: %v", ErrUnsupportedFormat, err)
	}
	if format2 != "" {
		format = format2
	}
	return img, format, nil
}

// SniffUnsupportedFormat returns a short name if data looks like a container we
// deliberately refuse to decode (native codecs with RCE history), else "".
func SniffUnsupportedFormat(data []byte) string {
	if len(data) < 12 {
		return ""
	}
	// ISO BMFF: ftyp.... brands (AVIF/HEIC/HEIF)
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		brand := string(data[8:12])
		switch brand {
		case "avif", "avis", "heic", "heif", "mif1", "msf1", "hevx", "heim", "heis":
			return brand
		}
		// Also scan early brand list for ftypavif embedded after major brand.
		window := data
		if len(window) > 64 {
			window = window[:64]
		}
		if bytes.Contains(window, []byte("avif")) || bytes.Contains(window, []byte("avis")) {
			return "avif"
		}
		if bytes.Contains(window, []byte("heic")) || bytes.Contains(window, []byte("heif")) {
			return "heif"
		}
	}
	// TIFF (sometimes used as stego carrier; not registered here)
	if bytes.HasPrefix(data, []byte{0x49, 0x49, 0x2a, 0x00}) || bytes.HasPrefix(data, []byte{0x4d, 0x4d, 0x00, 0x2a}) {
		return "tiff"
	}
	return ""
}
