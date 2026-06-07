package stego

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

const (
	AlphaPrefix = "AI42"
)

// InscribeResult contains the result of an inscription operation
type InscribeResult struct {
	ID          string
	ImageSHA256 string
	ImageBase64 string
	ImageBytes  []byte
}

// Inscribe embeds a message into an image using the specified method.
// Currently only "alpha" method is implemented natively.
func Inscribe(cover []byte, message string, method string) (*InscribeResult, error) {
	// Detection supports all 5 methods (alpha, palette, lsb.rgb, exif, raw) for scanning images
	// inscribed by other tools or future methods. However, inscription (writing) only supports
	// "alpha" method. Callers requesting non-alpha for inscription get a clear error instead
	// of silent downgrade (per Cat 6.1 reconciliation).
	if method == "" || method == "auto" {
		method = "alpha"
	}
	if method != "alpha" {
		return nil, fmt.Errorf("only alpha method is supported for inscription (detection supports: alpha, palette, lsb.rgb, exif, raw)")
	}

	img, _, err := image.Decode(bytes.NewReader(cover))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	stegoRGBA, err := EmbedAlpha(img, []byte(message))
	if err != nil {
		return nil, fmt.Errorf("failed to embed message: %w", err)
	}

	// Encode back to PNG for lossless stego
	var buf bytes.Buffer
	if err := png.Encode(&buf, stegoRGBA); err != nil {
		return nil, fmt.Errorf("failed to encode stego image: %w", err)
	}
	stegoBytes := buf.Bytes()

	// Calculate SHA256 hash of the stego image
	hasher := sha256.New()
	hasher.Write(stegoBytes)
	hash := hex.EncodeToString(hasher.Sum(nil))

	return &InscribeResult{
		ID:          hash,
		ImageSHA256: hash,
		ImageBase64: base64.StdEncoding.EncodeToString(stegoBytes),
		ImageBytes:  stegoBytes,
	}, nil
}

// EmbedAlpha embeds a payload into the alpha channel of an image using LSB.
// It follows the AI42 alpha algorithm: prefix + payload + null terminator.
// Bit order: LSB-first (bit 0 to 7 of each byte).
func EmbedAlpha(img image.Image, payload []byte) (*image.RGBA, error) {
	fullPayload := append([]byte(AlphaPrefix), payload...)
	fullPayload = append(fullPayload, 0x00)

	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	numPixels := width * height

	if len(fullPayload)*8 > numPixels {
		return nil, image.ErrFormat // Or a custom error for capacity
	}

	// Convert to RGBA if not already
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, img, bounds.Min, draw.Src)

	pixelIdx := 0
	for _, b := range fullPayload {
		for bitIdx := 0; bitIdx < 8; bitIdx++ {
			bit := (b >> bitIdx) & 1
			
			// Get current pixel
			offset := rgba.PixOffset(bounds.Min.X+(pixelIdx%width), bounds.Min.Y+(pixelIdx/width))
			// Alpha is at offset + 3
			rgba.Pix[offset+3] = (rgba.Pix[offset+3] & 0xFE) | bit
			
			pixelIdx++
		}
	}

	return rgba, nil
}

// ExtractAlpha extracts a payload from the alpha channel of an image.
// It looks for the AI42 prefix and reads until a null terminator on a byte boundary.
func ExtractAlpha(img image.Image) ([]byte, error) {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	numPixels := width * height

	// We need at least (len(AlphaPrefix) + 1) * 8 pixels
	if numPixels < (len(AlphaPrefix)+1)*8 {
		return nil, nil
	}

	// For efficiency, we can use a type switch if it's already RGBA
	var getAlpha func(x, y int) uint8
	if rgba, ok := img.(*image.RGBA); ok {
		getAlpha = func(x, y int) uint8 {
			return rgba.Pix[rgba.PixOffset(x, y)+3]
		}
	} else {
		getAlpha = func(x, y int) uint8 {
			_, _, _, a := img.At(x, y).RGBA()
			return uint8(a >> 8) // color.RGBA uses 16-bit values
		}
	}

	var bits []uint8
	// We only need as many bits as possible, but let's cap it to a reasonable size
	// stego_tool.py uses max_length=24576 bytes -> ~200k bits
	maxBits := numPixels
	if maxBits > 1000000 {
		maxBits = 1000000
	}

	for i := 0; i < maxBits; i++ {
		x := bounds.Min.X + (i % width)
		y := bounds.Min.Y + (i / width)
		alpha := getAlpha(x, y)
		bits = append(bits, alpha&1)
	}

	// Check for prefix
	prefixBits := make([]uint8, 0, len(AlphaPrefix)*8)
	for _, b := range []byte(AlphaPrefix) {
		for bitIdx := 0; bitIdx < 8; bitIdx++ {
			prefixBits = append(prefixBits, (uint8(b)>>bitIdx)&1)
		}
	}

	if len(bits) < len(prefixBits) {
		return nil, nil
	}

	for i := 0; i < len(prefixBits); i++ {
		if bits[i] != prefixBits[i] {
			return nil, nil // Prefix not found
		}
	}

	// Prefix found, extract until null terminator on byte boundary
	var payload []byte
	for i := len(prefixBits); i+8 <= len(bits); i += 8 {
		var b uint8
		for bitIdx := 0; bitIdx < 8; bitIdx++ {
			b |= bits[i+bitIdx] << bitIdx
		}
		if b == 0 {
			return payload, nil
		}
		payload = append(payload, b)
	}

	return nil, nil // Null terminator not found
}

// GetAlphaBits extracts all alpha LSB bits from the image.
func GetAlphaBits(img image.Image, maxBits int) []uint8 {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	numPixels := width * height

	if maxBits <= 0 || maxBits > numPixels {
		maxBits = numPixels
	}

	bits := make([]uint8, maxBits)
	
	if rgba, ok := img.(*image.RGBA); ok {
		for i := 0; i < maxBits; i++ {
			x := bounds.Min.X + (i % width)
			y := bounds.Min.Y + (i / width)
			bits[i] = rgba.Pix[rgba.PixOffset(x, y)+3] & 1
		}
	} else {
		for i := 0; i < maxBits; i++ {
			x := bounds.Min.X + (i % width)
			y := bounds.Min.Y + (i / width)
			_, _, _, a := img.At(x, y).RGBA()
			bits[i] = uint8(a>>8) & 1
		}
	}
	return bits
}
