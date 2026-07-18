package starlight

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

const (
	// unifiedSize is the spatial size expected by the Starlight detector.
	unifiedSize = 256
	// metaSize is the fixed metadata vector length (EXIF + post-image tail).
	metaSize = 2048
	// paletteSize is 256 RGB entries.
	paletteSize = 768
)

// UnifiedInput is the multi-stream tensor bundle consumed by the Starlight model.
// Layout matches Python scripts/starlight_utils.py load_unified_input.
// Floating-point tensors are stored CHW-ready (or flat feature vectors) for inference.
type UnifiedInput struct {
	Pixel           []float32 // 3*256*256 CHW, values in [0,1]
	Meta            []float32 // 2048, values in [0,1]
	Alpha           []float32 // 2*256*256: full_alpha/255 + alpha_lsb
	LSB             []float32 // 3*256*256 CHW, values 0/1
	Palette         []float32 // 768, values in [0,1]
	PaletteLSB      []float32 // 1*256*256
	FormatFeatures  []float32 // 6
	ContentFeatures []float32 // 6 = lsb_content(3) + alpha_content(3)
	Width, Height   int
	Mode            string // "RGBA","RGB","P", etc.
	Format          string // "png","jpeg",...
}

// LoadUnifiedInput decodes image bytes and builds the unified multi-stream input
// used by the Trin/GGUF Starlight detector. Port of Python load_unified_input.
//
// Center-crop policy (documented divergence for undersized images):
//   - If both W and H >= 256: standard center crop to 256×256.
//   - If either dim < 256: zero-pad (black) so the out-of-bounds region is zero, then
//     center the available content (equivalent to torchvision CenterCrop pad behavior).
func LoadUnifiedInput(imageData []byte) (*UnifiedInput, error) {
	if len(imageData) == 0 {
		return nil, fmt.Errorf("empty image data")
	}

	img, format, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	formatLower := normalizeFormat(format)

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	mode := detectMode(img, imageData, formatLower)

	// --- RGB center crop for pixel + LSB ---
	rgbCrop := centerCropRGB(img, unifiedSize)

	pixel := make([]float32, 3*unifiedSize*unifiedSize)
	lsb := make([]float32, 3*unifiedSize*unifiedSize)
	lsbR := make([]byte, unifiedSize*unifiedSize)
	lsbG := make([]byte, unifiedSize*unifiedSize)
	lsbB := make([]byte, unifiedSize*unifiedSize)

	plane := unifiedSize * unifiedSize
	for y := 0; y < unifiedSize; y++ {
		for x := 0; x < unifiedSize; x++ {
			r, g, b, _ := rgbCrop.At(x, y).RGBA()
			r8 := uint8(r >> 8)
			g8 := uint8(g >> 8)
			b8 := uint8(b >> 8)
			idx := y*unifiedSize + x
			pixel[0*plane+idx] = float32(r8) / 255.0
			pixel[1*plane+idx] = float32(g8) / 255.0
			pixel[2*plane+idx] = float32(b8) / 255.0
			lsb[0*plane+idx] = float32(r8 & 1)
			lsb[1*plane+idx] = float32(g8 & 1)
			lsb[2*plane+idx] = float32(b8 & 1)
			lsbR[idx] = r8 & 1
			lsbG[idx] = g8 & 1
			lsbB[idx] = b8 & 1
		}
	}

	// packbits: stack R,G,B channel-major then flatten (np.stack([r,g,b],0).flatten())
	lsbBits := make([]byte, 0, 3*plane)
	lsbBits = append(lsbBits, lsbR...)
	lsbBits = append(lsbBits, lsbG...)
	lsbBits = append(lsbBits, lsbB...)
	lsbContent := calculateContentFeatures(packBitsMSB(lsbBits))

	// --- Metadata: EXIF (best-effort JPEG APP1) + post-image tail ---
	// Gap vs Python: Pillow may expose EXIF from more containers; we only parse JPEG APP1.
	exif := extractJPEGEXIF(imageData)
	tail := extractPostTail(imageData, formatLower)
	metaRaw := append(exif, tail...)
	if len(metaRaw) > metaSize {
		metaRaw = metaRaw[:metaSize]
	}
	meta := make([]float32, metaSize)
	for i := 0; i < len(metaRaw); i++ {
		meta[i] = float32(metaRaw[i]) / 255.0
	}

	// --- Alpha path ---
	alpha := make([]float32, 2*plane)
	alphaContent := [3]float32{0, 0, 0}
	hasAlphaF := float32(0)
	alphaStdDev := float32(0)

	if mode == "RGBA" {
		hasAlphaF = 1
		alphaPlane := extractAlphaGeneric(img, width, height)
		alphaStdDev = float32(stdDevUint8(alphaPlane) / 255.0)
		fullCrop, _ := centerCropPlane(alphaPlane, width, height, unifiedSize)
		lsbCrop := make([]byte, plane)
		for i := 0; i < plane; i++ {
			alpha[i] = float32(fullCrop[i]) / 255.0
			alpha[plane+i] = float32(fullCrop[i] & 1)
			lsbCrop[i] = fullCrop[i] & 1
		}
		alphaContent = calculateContentFeatures(packBitsMSB(lsbCrop))
	}

	// --- Palette path ---
	palette := make([]float32, paletteSize)
	paletteLSB := make([]float32, plane)
	isPalette := float32(0)

	if paletted, ok := img.(*image.Paletted); ok {
		isPalette = 1
		mode = "P"
		palBytes := make([]byte, 0, paletteSize)
		for _, c := range paletted.Palette {
			r, g, b, _ := c.RGBA()
			palBytes = append(palBytes, uint8(r>>8), uint8(g>>8), uint8(b>>8))
			if len(palBytes) >= paletteSize {
				break
			}
		}
		if len(palBytes) > paletteSize {
			palBytes = palBytes[:paletteSize]
		}
		for i := 0; i < len(palBytes); i++ {
			palette[i] = float32(palBytes[i]) / 255.0
		}
		idxPlane := make([]uint8, width*height)
		minX, minY := paletted.Bounds().Min.X, paletted.Bounds().Min.Y
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				idxPlane[y*width+x] = paletted.ColorIndexAt(minX+x, minY+y)
			}
		}
		idxCrop, _ := centerCropPlane(idxPlane, width, height, unifiedSize)
		for i := 0; i < plane; i++ {
			paletteLSB[i] = float32(idxCrop[i] & 1)
		}
	}

	// is_rgb: Python only when mode == "RGB" exactly (not RGBA/P)
	isRGB := float32(0)
	if mode == "RGB" {
		isRGB = 1
	}

	formatFeatures := []float32{
		hasAlphaF,
		alphaStdDev,
		isPalette,
		isRGB,
		float32(width) / 256.0,
		float32(height) / 256.0,
	}

	contentFeatures := []float32{
		lsbContent[0], lsbContent[1], lsbContent[2],
		alphaContent[0], alphaContent[1], alphaContent[2],
	}

	return &UnifiedInput{
		Pixel:           pixel,
		Meta:            meta,
		Alpha:           alpha,
		LSB:             lsb,
		Palette:         palette,
		PaletteLSB:      paletteLSB,
		FormatFeatures:  formatFeatures,
		ContentFeatures: contentFeatures,
		Width:           width,
		Height:          height,
		Mode:            mode,
		Format:          formatLower,
	}, nil
}

func normalizeFormat(f string) string {
	switch f {
	case "jpeg", "jpg", "JPEG", "JPG":
		return "jpeg"
	case "png", "PNG":
		return "png"
	case "gif", "GIF":
		return "gif"
	case "webp", "WEBP":
		return "webp"
	case "bmp", "BMP":
		return "bmp"
	default:
		return f
	}
}

// detectMode approximates Pillow's img.mode using Go image types + container headers.
func detectMode(img image.Image, raw []byte, format string) string {
	if _, ok := img.(*image.Paletted); ok {
		return "P"
	}
	if _, ok := img.(*image.Gray); ok {
		return "L"
	}
	if _, ok := img.(*image.Gray16); ok {
		return "L"
	}
	if _, ok := img.(*image.YCbCr); ok {
		return "RGB"
	}
	if _, ok := img.(*image.CMYK); ok {
		return "CMYK"
	}

	// PNG: use IHDR color type for accurate RGB vs RGBA (Go often uses NRGBA for both).
	if format == "png" {
		if ct, ok := pngColorType(raw); ok {
			switch ct {
			case 0, 2: // gray, truecolor
				return "RGB"
			case 3:
				return "P"
			case 4, 6: // gray+alpha, truecolor+alpha
				return "RGBA"
			}
		}
	}

	switch img.(type) {
	case *image.NRGBA, *image.NRGBA64, *image.RGBA, *image.RGBA64:
		// Without header hint, treat as RGBA if any non-opaque pixel exists.
		if hasNonOpaquePixel(img) {
			return "RGBA"
		}
		return "RGB"
	default:
		return "RGB"
	}
}

func pngColorType(raw []byte) (byte, bool) {
	// signature(8) + len(4) + "IHDR"(4) + width(4)+height(4)+bit_depth(1)+color_type(1)
	if len(raw) < 26 {
		return 0, false
	}
	if !bytes.HasPrefix(raw, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}) {
		return 0, false
	}
	if string(raw[12:16]) != "IHDR" {
		return 0, false
	}
	return raw[25], true
}

func hasNonOpaquePixel(img image.Image) bool {
	b := img.Bounds()
	// Sample a grid for speed on large images; full scan for small.
	step := 1
	if b.Dx()*b.Dy() > 256*256 {
		step = 4
	}
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 0xffff {
				return true
			}
		}
	}
	return false
}

// centerCropRGB returns a size×size RGBA image (RGB content) via center crop or zero-pad.
func centerCropRGB(img image.Image, size int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, size, size))

	srcX0 := b.Min.X + (w-size)/2
	srcY0 := b.Min.Y + (h-size)/2

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			sx := srcX0 + x
			sy := srcY0 + y
			if sx >= b.Min.X && sx < b.Max.X && sy >= b.Min.Y && sy < b.Max.Y {
				r, g, bb, _ := img.At(sx, sy).RGBA()
				out.Set(x, y, color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bb >> 8), A: 255})
			}
		}
	}
	return out
}

// centerCropPlane center-crops (or zero-pads) a single-channel plane to size×size.
func centerCropPlane(plane []uint8, w, h, size int) (crop []uint8, _ []uint8) {
	crop = make([]uint8, size*size)
	srcX0 := (w - size) / 2
	srcY0 := (h - size) / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			sx := srcX0 + x
			sy := srcY0 + y
			if sx >= 0 && sx < w && sy >= 0 && sy < h {
				crop[y*size+x] = plane[sy*w+sx]
			}
		}
	}
	return crop, crop
}

func extractAlphaGeneric(img image.Image, w, h int) []uint8 {
	b := img.Bounds()
	plane := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			_, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			plane[y*w+x] = uint8(a >> 8)
		}
	}
	return plane
}

func stdDevUint8(data []uint8) float64 {
	if len(data) == 0 {
		return 0
	}
	var sum float64
	for _, v := range data {
		sum += float64(v)
	}
	mean := sum / float64(len(data))
	var variance float64
	for _, v := range data {
		d := float64(v) - mean
		variance += d * d
	}
	// Population std (numpy ndarray.std default ddof=0)
	return math.Sqrt(variance / float64(len(data)))
}

// packBitsMSB packs 0/1 bits into bytes, MSB-first (numpy.packbits default).
func packBitsMSB(bits []byte) []byte {
	n := (len(bits) + 7) / 8
	out := make([]byte, n)
	for i, b := range bits {
		if b != 0 {
			out[i/8] |= 1 << uint(7-(i%8))
		}
	}
	return out
}

// calculateContentFeatures ports _calculate_content_features.
// Returns [uniqueness_ratio, printable_char_ratio, most_common_char_ratio].
func calculateContentFeatures(data []byte) [3]float32 {
	if len(data) == 0 {
		return [3]float32{0, 0, 0}
	}
	total := len(data)

	var uniqueness float64
	if total <= 256 {
		seen := make(map[byte]struct{}, total)
		for _, b := range data {
			seen[b] = struct{}{}
		}
		uniqueness = float64(len(seen)) / float64(total)
	} else {
		step := total / 1000
		if step < 1 {
			step = 1
		}
		seen := make(map[byte]struct{})
		for i := 0; i < total; i += step {
			seen[data[i]] = struct{}{}
		}
		uniqueness = math.Min(1.0, float64(len(seen))/256.0)
	}

	var printable int
	counts := make([]int, 256)
	for _, b := range data {
		counts[b]++
		if b >= 32 && b <= 126 {
			printable++
		}
	}
	printableRatio := float64(printable) / float64(total)
	maxCount := 0
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}
	mostCommon := float64(maxCount) / float64(total)

	return [3]float32{float32(uniqueness), float32(printableRatio), float32(mostCommon)}
}

// extractPostTail ports extract_post_tail: bytes after official end of image.
func extractPostTail(raw []byte, formatHint string) []byte {
	if len(raw) < 1000 {
		return nil
	}
	var tail []byte
	switch formatHint {
	case "jpeg":
		pos := bytes.LastIndex(raw, []byte{0xff, 0xd9})
		if pos != -1 && pos+2 < len(raw) {
			tail = raw[pos+2:]
		}
	case "png":
		pos := bytes.LastIndex(raw, []byte("IEND"))
		if pos != -1 && pos+12 < len(raw) {
			tail = raw[pos+12:]
		}
	case "gif":
		pos := bytes.LastIndexByte(raw, ';')
		if pos != -1 && pos+1 < len(raw) {
			tail = raw[pos+1:]
		}
	case "webp":
		if len(raw) >= 12 && bytes.HasPrefix(raw, []byte("RIFF")) && string(raw[8:12]) == "WEBP" {
			riffSize := uint32(raw[4]) | uint32(raw[5])<<8 | uint32(raw[6])<<16 | uint32(raw[7])<<24
			expected := 8 + int(riffSize)
			if len(raw) > expected+100 {
				tail = raw[expected:]
			}
		}
	}
	if len(tail) > 2048 {
		tail = tail[:2048]
	}
	return tail
}

// extractJPEGEXIF extracts APP1 EXIF payload from a JPEG (best-effort).
// Returns the APP1 segment data (including "Exif\x00\x00" when present), or nil.
//
// Gap vs Python: Pillow may surface EXIF from other containers; we only parse JPEG APP1.
func extractJPEGEXIF(raw []byte) []byte {
	if len(raw) < 4 || raw[0] != 0xff || raw[1] != 0xd8 {
		return nil
	}
	i := 2
	for i+1 < len(raw) {
		// Find 0xFF marker prefix
		if raw[i] != 0xff {
			return nil
		}
		for i < len(raw) && raw[i] == 0xff {
			i++
		}
		if i >= len(raw) {
			return nil
		}
		marker := raw[i]
		i++
		// Standalone markers without length
		if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd9) {
			if marker == 0xd9 {
				return nil
			}
			continue
		}
		if i+2 > len(raw) {
			return nil
		}
		segLen := int(raw[i])<<8 | int(raw[i+1])
		if segLen < 2 || i+segLen > len(raw) {
			return nil
		}
		segData := raw[i+2 : i+segLen]
		i += segLen
		if marker == 0xe1 {
			return segData
		}
		if marker == 0xda { // SOS
			return nil
		}
	}
	return nil
}
