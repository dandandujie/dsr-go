package image

import (
	"bytes"
	"context"
	"fmt"
	stdimage "image"
	"math"

	xdraw "golang.org/x/image/draw"

	"github.com/dandandujie/dsr-go/core"
)

// Preprocessing constants of the Rust crate.
const (
	// padColor is the padding color of a resized image, the gray value 127 that
	// matches the model image transform mean of 0.5.
	padColor = 127
	// webpMaxDimension is the maximum size of one dimension of a WebP image.
	webpMaxDimension = 16383
	// maxFitRounds is the number of token-budget fits of one preprocess call.
	// The specification scales a size below its minimum area back up, so the
	// WebP limit is reached after a few rounds that each raise the short side.
	maxFitRounds = 6
)

// V41ImagePreprocessor preprocesses images for DeepSeek V4.1 model input.
//
// It replaces OpenCvImagePreprocessor, which the Rust crate builds on OpenCV.
// Preprocessing detects the media type from the file header, applies the
// low-detail limit, fits the image into the V4.1 token budget of
// core.V41ImageTokenSpec, and encodes it as WebP.
//
// The preprocessor is stateless and safe for concurrent use.
type V41ImagePreprocessor struct{}

// Preprocess preprocesses one encoded image.
//
// It returns a KindUnsupportedMediaType error for a media type other than JPEG,
// PNG, GIF, and WebP, a KindImageDimensionsTooLarge error for an image above
// options.MaxDimensionPx, and a KindTokenBudget error when the token-budget fit
// does not converge.
func (V41ImagePreprocessor) Preprocess(_ context.Context, data []byte, options PreprocessOptions) (core.ImageInfo, error) {
	if len(data) == 0 {
		return core.ImageInfo{}, NewEmptyImageError()
	}
	mediaType, err := detectMediaType(data)
	if err != nil {
		return core.ImageInfo{}, err
	}
	if err := checkDimensions(data, mediaType, options.MaxDimensionPx); err != nil {
		return core.ImageInfo{}, err
	}
	decoded, err := decodeImage(data, mediaType)
	if err != nil {
		return core.ImageInfo{}, err
	}
	if options.Detail.IsLow() {
		decoded = limitImage(decoded, options.LowDetailMaxDimensionPx)
	}
	return preprocessImage(decoded, options.MaxDimensionPx)
}

// detectMediaType returns the media type of the image bytes, read from the file
// header.
//
// It replaces the infer crate the Rust preprocessor uses. The sniffing
// recognizes the four supported media types and the most common unsupported
// image formats; it reports "unknown media type" for everything else.
func detectMediaType(data []byte) (core.ImageMediaType, error) {
	mime, known := sniffMediaType(data)
	if !known {
		return 0, NewUnsupportedMediaTypeError("unknown media type")
	}
	mediaType, supported := core.ImageMediaTypeFromMIME(mime)
	if !supported {
		return 0, NewUnsupportedMediaTypeError(mime)
	}
	return mediaType, nil
}

// sniffMediaType returns the media type of the image bytes and whether the
// header is recognized.
func sniffMediaType(data []byte) (string, bool) {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", true
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg", true
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif", true
	case isWebPHeader(data):
		return "image/webp", true
	case bytes.HasPrefix(data, []byte("BM")):
		return "image/bmp", true
	case bytes.HasPrefix(data, []byte("II*\x00")), bytes.HasPrefix(data, []byte("MM\x00*")):
		return "image/tiff", true
	case bytes.HasPrefix(data, []byte("\x00\x00\x01\x00")), bytes.HasPrefix(data, []byte("\x00\x00\x02\x00")):
		return "image/vnd.microsoft.icon", true
	case bytes.HasPrefix(data, []byte("qoif")):
		return "image/qoi", true
	case bytes.HasPrefix(data, []byte("DDS ")):
		return "image/vnd-ms.dds", true
	case bytes.HasPrefix(data, []byte("8BPS")):
		return "image/vnd.adobe.photoshop", true
	case bytes.HasPrefix(data, []byte("farbfeld")):
		return "image/x-farbfeld", true
	case bytes.HasPrefix(data, []byte("\x76\x2f\x31\x01")):
		return "image/x-exr", true
	case bytes.HasPrefix(data, []byte("#?RADIANCE")), bytes.HasPrefix(data, []byte("#?RGBE")):
		return "image/vnd.radiance", true
	case bytes.HasPrefix(data, []byte("\xff\x0a")):
		return "image/jxl", true
	}
	if mime, known := sniffISOBaseMediaType(data); known {
		return mime, true
	}
	if hasTGATrailer(data) {
		return "image/x-tga", true
	}
	return "", false
}

// isWebPHeader reports whether data starts with a WebP container header.
func isWebPHeader(data []byte) bool {
	return len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
}

// sniffISOBaseMediaType returns the media type of the ISO base media brands the
// infer crate recognizes, or false when data is not such a file.
func sniffISOBaseMediaType(data []byte) (string, bool) {
	if len(data) < 12 || !bytes.Equal(data[4:8], []byte("ftyp")) {
		return "", false
	}
	switch string(data[8:12]) {
	case "avif", "avis":
		return "image/avif", true
	case "heic", "heix", "hevc", "hevx", "heim", "heis", "hevm", "hevs", "mif1", "msf1":
		return "image/heif", true
	}
	return "", false
}

// hasTGATrailer reports whether data ends with the TGA 2.0 footer.
func hasTGATrailer(data []byte) bool {
	const footer = "TRUEVISION-XFILE."
	if len(data) < 26 {
		return false
	}
	return bytes.Contains(data[len(data)-26:], []byte(footer))
}

// preprocessImage fits a decoded image into the V4.1 token budget and encodes
// it as WebP.
func preprocessImage(image *stdimage.RGBA, maxDimensionPx uint32) (core.ImageInfo, error) {
	width, height := image.Bounds().Dx(), image.Bounds().Dy()
	// checkDimensions reads the size the header declares. The decoded image is
	// the image the request carries, so it is checked against the limit too.
	if uint32(width) > maxDimensionPx || uint32(height) > maxDimensionPx {
		return core.ImageInfo{}, NewImageDimensionsTooLargeError()
	}
	bestWidth, bestHeight, err := fitWebPTargetSize(width, height)
	if err != nil {
		return core.ImageInfo{}, err
	}
	if bestWidth <= 0 || bestHeight <= 0 {
		return core.ImageInfo{}, NewResizeError(fmt.Errorf("invalid target size: %dx%d", bestWidth, bestHeight))
	}

	resized := image
	if width != bestWidth || height != bestHeight {
		resized = resizeToBest(image, width, height, bestWidth, bestHeight)
	}
	encoded, err := encodeWebP(resized)
	if err != nil {
		return core.ImageInfo{}, err
	}
	return core.ImageInfo{
		Data:   encoded,
		Width:  uint32(bestWidth),
		Height: uint32(bestHeight),
	}, nil
}

// fitWebPTargetSize returns the V4.1 target size of an image of width x height
// pixels that WebP can encode.
//
// The token specification fits the token budget without a dimension limit, and
// an elongated image can require a side above webpMaxDimension. A size outside
// the limit is scaled into it and placed on the patch grid, and fitting is
// repeated for that size, so the returned size is both a size the specification
// leaves unchanged and a size WebP encodes.
func fitWebPTargetSize(width, height int) (int, int, error) {
	spec := core.V41ImageTokenSpec()
	sourceWidth, sourceHeight := width, height
	for round := 0; round < maxFitRounds; round++ {
		fitted, err := spec.CalcResize(sourceWidth, sourceHeight)
		if err != nil {
			return 0, 0, NewTokenBudgetError(err)
		}
		if fitted.BestWidth <= webpMaxDimension && fitted.BestHeight <= webpMaxDimension {
			return fitted.BestWidth, fitted.BestHeight, nil
		}
		sourceWidth, sourceHeight = scaleIntoWebPLimit(spec.PatchSize(), fitted.BestWidth, fitted.BestHeight)
	}
	return 0, 0, NewResizeError(fmt.Errorf(
		"no V4.1 target size within %d pixels for an image of %dx%d pixels",
		webpMaxDimension, width, height))
}

// scaleIntoWebPLimit scales a size into the WebP dimension limit.
//
// The long side is placed on the patch grid at or below the limit, and the short
// side is placed on the patch grid at or above its scaled value. Raising the
// short side keeps the scaled size at or above the minimum area of the
// specification, which otherwise scales the long side past the limit again.
func scaleIntoWebPLimit(patchSize, width, height int) (int, int) {
	longSide := max(width, height)
	if longSide <= webpMaxDimension {
		return width, height
	}
	scaledLong := webpMaxDimension / patchSize * patchSize
	scaledShort := math.Round(float64(min(width, height)) * float64(scaledLong) / float64(longSide))
	shortSide := divCeilInt(max(int(scaledShort), 1), patchSize)
	if width >= height {
		return scaledLong, shortSide
	}
	return shortSide, scaledLong
}

// divCeilInt returns the smallest multiple of divisor that is at least value.
func divCeilInt(value, divisor int) int {
	return (value + divisor - 1) / divisor * divisor
}

// limitImage scales an image down so that its long side is at most
// maxDimension.
func limitImage(image *stdimage.RGBA, maxDimension uint32) *stdimage.RGBA {
	width, height := image.Bounds().Dx(), image.Bounds().Dy()
	longSide := max(width, height)
	if longSide <= int(maxDimension) {
		return image
	}
	ratio := float64(maxDimension) / float64(longSide)
	targetWidth := max(int(math.Round(float64(width)*ratio)), 1)
	targetHeight := max(int(math.Round(float64(height)*ratio)), 1)
	return resizeCatmullRom(image, targetWidth, targetHeight)
}

// resizeToBest scales an image to fit bestWidth x bestHeight while preserving
// its aspect ratio, and centers it on a background of padColor.
func resizeToBest(image *stdimage.RGBA, sourceWidth, sourceHeight, bestWidth, bestHeight int) *stdimage.RGBA {
	imageRatio := float64(sourceWidth) / float64(sourceHeight)
	targetRatio := float64(bestWidth) / float64(bestHeight)
	width, height := bestWidth, bestHeight
	switch {
	case imageRatio > targetRatio:
		height = max(roundHalfEven(float64(sourceHeight)/float64(sourceWidth)*float64(bestWidth)), 1)
	case imageRatio < targetRatio:
		width = max(roundHalfEven(float64(sourceWidth)/float64(sourceHeight)*float64(bestHeight)), 1)
	}

	scaled := resizeCatmullRom(image, width, height)
	padded := stdimage.NewRGBA(stdimage.Rect(0, 0, bestWidth, bestHeight))
	fillOpaque(padded, padColor)
	left := roundHalfEven(float64(bestWidth-width) * 0.5)
	top := roundHalfEven(float64(bestHeight-height) * 0.5)
	xdraw.Draw(padded,
		stdimage.Rect(left, top, left+width, top+height),
		scaled,
		stdimage.Point{},
		xdraw.Src)
	return padded
}

// resizeCatmullRom scales an image to width x height.
//
// The Rust code calls OpenCV imgproc::resize with INTER_CUBIC; the CatmullRom
// interpolator of golang.org/x/image/draw is the closest pure-Go equivalent.
// OpenCV uses the cubic kernel with a = -0.75 and fixed-point coefficients while
// CatmullRom uses a = -0.5, so resized pixels can differ by a few units on
// high-contrast edges.
func resizeCatmullRom(image *stdimage.RGBA, width, height int) *stdimage.RGBA {
	scaled := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), image, image.Bounds(), xdraw.Src, nil)
	return scaled
}

// fillOpaque fills an image with one gray value and full opacity.
func fillOpaque(image *stdimage.RGBA, value uint8) {
	for index := 0; index < len(image.Pix); index += 4 {
		image.Pix[index], image.Pix[index+1], image.Pix[index+2], image.Pix[index+3] = value, value, value, 0xff
	}
}

// roundHalfEven rounds to the nearest integer, with a value exactly between two
// integers rounding to the even one.
func roundHalfEven(value float64) int {
	floor := math.Floor(value)
	fraction := value - floor
	if fraction < 0.5 || (fraction == 0.5 && int64(floor)%2 == 0) {
		return int(floor)
	}
	return int(floor + 1)
}
