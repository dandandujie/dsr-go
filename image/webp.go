package image

import (
	"bytes"
	stdimage "image"

	"github.com/HugoSmits86/nativewebp"
)

// webpCompressionLevel is the compression level of the lossless WebP encoder.
const webpCompressionLevel = nativewebp.BestCompression

// encodeWebP encodes one preprocessed image as WebP.
//
// The Rust reference encodes lossy VP8 at quality 90 with libwebp through
// OpenCV. No lossy WebP encoder exists in pure Go, so this port writes a
// lossless VP8L image with github.com/HugoSmits86/nativewebp at its maximum
// compression level. The result is pixel-exact for the resized image, but it is
// larger than a quality-90 lossy WebP and the quality parameter has no
// equivalent. The dimensions, which determine the token cost, are unaffected.
func encodeWebP(image stdimage.Image) ([]byte, error) {
	var buffer bytes.Buffer
	options := nativewebp.Options{CompressionLevel: webpCompressionLevel}
	if err := nativewebp.Encode(&buffer, image, &options); err != nil {
		return nil, NewEncodeError(err)
	}
	if buffer.Len() == 0 {
		return nil, NewEncodeFailedError()
	}
	return buffer.Bytes(), nil
}
