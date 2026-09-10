package image

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/HugoSmits86/nativewebp"
	"golang.org/x/image/webp"

	"github.com/dandandujie/dsr-go/core"
)

// encodePNG encodes an image as PNG, failing the test on error.
func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buffer.Bytes()
}

// solidNRGBA returns a width x height image filled with one straight-alpha
// color.
func solidNRGBA(width, height int, fill color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	return img
}

// decodeWebP decodes a WebP image, failing the test on error.
func decodeWebP(t *testing.T, data []byte) image.Image {
	t.Helper()
	decoded, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("webp.Decode: %v", err)
	}
	return decoded
}

// highDetailOptions returns high-detail options with the default limits.
func highDetailOptions() PreprocessOptions {
	return DefaultImageLimits().PreprocessOptions(core.ImageDetailHigh, 1)
}

// requirePixelColor fails the test when the pixel differs from want.
func requirePixelColor(t *testing.T, img image.Image, x, y int, want color.NRGBA) {
	t.Helper()
	got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	if got != want {
		t.Fatalf("pixel (%d, %d) = %+v, want %+v", x, y, got, want)
	}
}

func TestFitWebPTargetSizeMatchesTheTokenSpec(t *testing.T) {
	spec := core.V41ImageTokenSpec()
	// The expected sizes are the values of the Rust algorithm: the V4.1 fit,
	// scaled into the WebP dimension limit when a side would exceed it.
	for _, size := range []struct {
		width      int
		height     int
		wantWidth  int
		wantHeight int
	}{
		{width: 1, height: 1, wantWidth: 546, wantHeight: 546},
		{width: 14, height: 14, wantWidth: 546, wantHeight: 546},
		{width: 100, height: 100, wantWidth: 546, wantHeight: 546},
		{width: 544, height: 544, wantWidth: 546, wantHeight: 546},
		{width: 800, height: 600, wantWidth: 812, wantHeight: 602},
		{width: 4096, height: 4096, wantWidth: 1302, wantHeight: 1302},
		{width: 8192, height: 100, wantWidth: 8204, wantHeight: 112},
		{width: 100, height: 20000, wantWidth: 42, wantHeight: 8400},
		{width: 20000, height: 10, wantWidth: 16380, wantHeight: 28},
		{width: 16383, height: 1, wantWidth: 16380, wantHeight: 28},
	} {
		width, height, err := fitWebPTargetSize(size.width, size.height)
		requireNoError(t, err)
		if width <= 0 || height <= 0 || width > webpMaxDimension || height > webpMaxDimension {
			t.Fatalf("fitWebPTargetSize(%d, %d) = %dx%d, want a positive size within %d",
				size.width, size.height, width, height, webpMaxDimension)
		}
		if !spec.IsImageValid(width, height) {
			t.Fatalf("fitWebPTargetSize(%d, %d) = %dx%d, which the token specification would resize again",
				size.width, size.height, width, height)
		}
		if width != size.wantWidth || height != size.wantHeight {
			t.Fatalf("fitWebPTargetSize(%d, %d) = %dx%d, want %dx%d",
				size.width, size.height, width, height, size.wantWidth, size.wantHeight)
		}
	}
}

func TestPreprocessProducesTheTokenBudgetSize(t *testing.T) {
	source := solidNRGBA(800, 600, color.NRGBA{R: 20, G: 120, B: 220, A: 255})
	info, err := V41ImagePreprocessor{}.Preprocess(context.Background(), encodePNG(t, source), highDetailOptions())
	requireNoError(t, err)

	expectedWidth, expectedHeight, err := fitWebPTargetSize(800, 600)
	requireNoError(t, err)
	requireEqual(t, info.Width, uint32(expectedWidth), "Width")
	requireEqual(t, info.Height, uint32(expectedHeight), "Height")

	decoded := decodeWebP(t, info.Data)
	requireEqual(t, decoded.Bounds().Dx(), expectedWidth, "decoded width")
	requireEqual(t, decoded.Bounds().Dy(), expectedHeight, "decoded height")
	requirePixelColor(t, decoded, expectedWidth/2, expectedHeight/2, color.NRGBA{R: 20, G: 120, B: 220, A: 255})
}

func TestPreprocessAppliesTheLowDetailLimit(t *testing.T) {
	source := solidNRGBA(1000, 500, color.NRGBA{R: 200, G: 30, B: 40, A: 255})
	options := DefaultImageLimits().PreprocessOptions(core.ImageDetailLow, 1)
	info, err := V41ImagePreprocessor{}.Preprocess(context.Background(), encodePNG(t, source), options)
	requireNoError(t, err)

	// The long side is limited to 512 before token-budget fitting, so the
	// target is the V4.1 fit of 512 x 256, not of 1000 x 500.
	expectedWidth, expectedHeight, err := fitWebPTargetSize(512, 256)
	requireNoError(t, err)
	requireEqual(t, info.Width, uint32(expectedWidth), "Width")
	requireEqual(t, info.Height, uint32(expectedHeight), "Height")

	decoded := decodeWebP(t, info.Data)
	requirePixelColor(t, decoded, expectedWidth/2, expectedHeight/2, color.NRGBA{R: 200, G: 30, B: 40, A: 255})
}

func TestPreprocessUsesTheManyImagesDimensionLimit(t *testing.T) {
	source := solidNRGBA(5000, 10, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	options := DefaultImageLimits().PreprocessOptions(core.ImageDetailHigh, 15)
	requireEqual(t, options.MaxDimensionPx, uint32(4096), "MaxDimensionPx")
	_, err := V41ImagePreprocessor{}.Preprocess(context.Background(), encodePNG(t, source), options)
	requireImageError(t, err, KindImageDimensionsTooLarge)
}

func TestPreprocessRejectsDimensionsAboveTheLimit(t *testing.T) {
	source := solidNRGBA(8193, 1, color.NRGBA{A: 255})
	_, err := V41ImagePreprocessor{}.Preprocess(context.Background(), encodePNG(t, source), highDetailOptions())
	requireImageError(t, err, KindImageDimensionsTooLarge)
}

func TestPreprocessRejectsUnsupportedMediaTypes(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "bmp", data: []byte("BM\x00\x00\x00\x00"), want: "image/bmp"},
		{name: "unknown", data: []byte("not an image"), want: "unknown media type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := V41ImagePreprocessor{}.Preprocess(context.Background(), test.data, highDetailOptions())
			imageErr := requireImageError(t, err, KindUnsupportedMediaType)
			requireEqual(t, imageErr.Message, test.want, "Message")
		})
	}
}

func TestPreprocessRejectsEmptyData(t *testing.T) {
	_, err := V41ImagePreprocessor{}.Preprocess(context.Background(), nil, highDetailOptions())
	requireImageError(t, err, KindEmptyImage)
}

func TestPreprocessBlendsAlphaOntoTheInferenceBackground(t *testing.T) {
	source := solidNRGBA(8, 8, color.NRGBA{R: 255, A: 128})
	info, err := V41ImagePreprocessor{}.Preprocess(context.Background(), encodePNG(t, source), highDetailOptions())
	requireNoError(t, err)

	decoded := decodeWebP(t, info.Data)
	// (255*128 + 253*127 + 127) / 255 = 254 and (0*128 + 253*127 + 127) / 255 = 126.
	requirePixelColor(t, decoded, int(info.Width)/2, int(info.Height)/2, color.NRGBA{R: 254, G: 126, B: 126, A: 255})
}

func TestPreprocessReplicatesGrayscale(t *testing.T) {
	source := image.NewGray(image.Rect(0, 0, 8, 8))
	for index := range source.Pix {
		source.Pix[index] = 200
	}
	info, err := V41ImagePreprocessor{}.Preprocess(context.Background(), encodePNG(t, source), highDetailOptions())
	requireNoError(t, err)

	decoded := decodeWebP(t, info.Data)
	requirePixelColor(t, decoded, int(info.Width)/2, int(info.Height)/2, color.NRGBA{R: 200, G: 200, B: 200, A: 255})
}

func TestPreprocessScales16BitChannelsBy257(t *testing.T) {
	source := image.NewGray16(image.Rect(0, 0, 8, 8))
	for index := 0; index < len(source.Pix); index += 2 {
		source.Pix[index], source.Pix[index+1] = 0xff, 0xff
	}
	info, err := V41ImagePreprocessor{}.Preprocess(context.Background(), encodePNG(t, source), highDetailOptions())
	requireNoError(t, err)

	decoded := decodeWebP(t, info.Data)
	requirePixelColor(t, decoded, int(info.Width)/2, int(info.Height)/2, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
}

func TestPreprocessDecodesGIF(t *testing.T) {
	palette := color.Palette{color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}}
	source := image.NewPaletted(image.Rect(0, 0, 4, 4), palette)
	for index := range source.Pix {
		source.Pix[index] = uint8(index % 2)
	}
	var buffer bytes.Buffer
	requireNoError(t, gif.Encode(&buffer, source, nil))

	info, err := V41ImagePreprocessor{}.Preprocess(context.Background(), buffer.Bytes(), highDetailOptions())
	requireNoError(t, err)

	expectedWidth, expectedHeight, err := fitWebPTargetSize(4, 4)
	requireNoError(t, err)
	decoded := decodeWebP(t, info.Data)
	requireEqual(t, decoded.Bounds().Dx(), expectedWidth, "decoded width")
	requireEqual(t, decoded.Bounds().Dy(), expectedHeight, "decoded height")
}

func TestPreprocessDecodesWebPInput(t *testing.T) {
	source := solidNRGBA(20, 12, color.NRGBA{R: 10, G: 200, B: 30, A: 255})
	var buffer bytes.Buffer
	requireNoError(t, nativewebp.Encode(&buffer, source, nil))

	info, err := V41ImagePreprocessor{}.Preprocess(context.Background(), buffer.Bytes(), highDetailOptions())
	requireNoError(t, err)

	decoded := decodeWebP(t, info.Data)
	requirePixelColor(t, decoded, int(info.Width)/2, int(info.Height)/2, color.NRGBA{R: 10, G: 200, B: 30, A: 255})
}

func TestPreprocessDecodesJPEG(t *testing.T) {
	var buffer bytes.Buffer
	requireNoError(t, jpeg.Encode(&buffer, solidNRGBA(16, 16, color.NRGBA{R: 200, G: 100, B: 50, A: 255}), nil))

	info, err := V41ImagePreprocessor{}.Preprocess(context.Background(), buffer.Bytes(), highDetailOptions())
	requireNoError(t, err)

	decoded := decodeWebP(t, info.Data)
	got := color.NRGBAModel.Convert(decoded.At(int(info.Width)/2, int(info.Height)/2)).(color.NRGBA)
	if difference(got.R, 200) > 8 || difference(got.G, 100) > 8 || difference(got.B, 50) > 8 {
		t.Fatalf("JPEG center pixel = %+v, want about (200, 100, 50)", got)
	}
}

func TestPreprocessReportsATruncatedImageAsADecodeError(t *testing.T) {
	// The Rust preprocessor decodes with OpenCV, whose imdecode returns an
	// empty matrix for truncated input and therefore reports EmptyImage. The
	// Go decoders report the failure, which this port surfaces as Decode.
	data := encodePNG(t, solidNRGBA(8, 8, color.NRGBA{A: 255}))
	_, err := V41ImagePreprocessor{}.Preprocess(context.Background(), data[:len(data)-8], highDetailOptions())
	requireImageError(t, err, KindDecode)
}

func TestEncodeWebPRoundTripsEveryPixel(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 17, 9))
	for y := 0; y < 9; y++ {
		for x := 0; x < 17; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x * 15), G: uint8(y * 28), B: uint8(x * y), A: 255})
		}
	}
	encoded, err := encodeWebP(source)
	requireNoError(t, err)

	decoded := decodeWebP(t, encoded)
	requireEqual(t, decoded.Bounds(), source.Bounds(), "decoded bounds")
	for y := 0; y < 9; y++ {
		for x := 0; x < 17; x++ {
			want := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
			requirePixelColor(t, decoded, x, y, want)
		}
	}
}

func TestResizeCatmullRomKeepsAUniformImageUniform(t *testing.T) {
	opaque := toOpaqueRGBA(solidNRGBA(3, 5, color.NRGBA{R: 40, G: 80, B: 120, A: 255}))
	scaled := resizeCatmullRom(opaque, 11, 7)
	for y := 0; y < 7; y++ {
		for x := 0; x < 11; x++ {
			requirePixelColor(t, scaled, x, y, color.NRGBA{R: 40, G: 80, B: 120, A: 255})
		}
	}
}

func TestRoundHalfEvenRoundsTiesToEven(t *testing.T) {
	requireEqual(t, roundHalfEven(0.5), 0, "roundHalfEven(0.5)")
	requireEqual(t, roundHalfEven(1.5), 2, "roundHalfEven(1.5)")
	requireEqual(t, roundHalfEven(2.5), 2, "roundHalfEven(2.5)")
	requireEqual(t, roundHalfEven(0.4999), 0, "roundHalfEven(0.4999)")
	requireEqual(t, roundHalfEven(-1.5), -2, "roundHalfEven(-1.5)")
}

func TestScaleIntoWebPLimitPlacesSidesOnThePatchGrid(t *testing.T) {
	width, height := scaleIntoWebPLimit(14, 20000, 10)
	requireEqual(t, width, webpMaxDimension/14*14, "width")
	requireEqual(t, height, 14, "height")
	requireEqual(t, width%14, 0, "width on the patch grid")
	requireEqual(t, height%14, 0, "height on the patch grid")

	// A size within the limit is returned unchanged.
	withinWidth, withinHeight := scaleIntoWebPLimit(14, 100, 50)
	requireEqual(t, withinWidth, 100, "width within the limit")
	requireEqual(t, withinHeight, 50, "height within the limit")
}

// difference returns the absolute difference of two channel values.
func difference(a, b uint8) int {
	if a > b {
		return int(a) - int(b)
	}
	return int(b) - int(a)
}
