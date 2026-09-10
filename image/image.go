// Package image fetches and preprocesses images for DeepSeek V4.1 model input.
//
// ImageResolver turns the core.ImageSource values produced by protocol
// conversion into core.MultiModalData. Fetching external URLs and preprocessing
// are supplied by ImageFetcher and ImagePreprocessor implementations. The
// package provides HTTPImageFetcher, which downloads images with net/http, and
// V41ImagePreprocessor, which decodes, resizes, and encodes images for the
// inference backend.
//
// This package is the Go port of the Rust deepseek-recipe-image crate. The Rust
// crate preprocesses with OpenCV; the Go port uses the standard library
// decoders plus golang.org/x/image/webp and golang.org/x/image/draw instead, so
// two behaviours differ from the Rust reference:
//
//   - Resizing uses the CatmullRom interpolator of golang.org/x/image/draw
//     where the Rust code calls OpenCV imgproc::resize with INTER_CUBIC. Both
//     are bicubic filters, but OpenCV uses the cubic kernel with a = -0.75 and
//     11-bit fixed-point coefficients, while CatmullRom uses a = -0.5, so
//     resized pixels can differ by a few units on high-contrast edges.
//
//   - The Rust code encodes lossy VP8 WebP at quality 90 through libwebp. No
//     lossy WebP encoder exists in pure Go, so the port encodes lossless VP8L
//     with github.com/HugoSmits86/nativewebp at its maximum compression level.
//     Encoded images are pixel-exact but larger, and the quality parameter has
//     no equivalent.
//
// Decoding differs at the edges as well: OpenCV returns a partially decoded
// image for truncated input where the Go decoders report an error, and animated
// WebP is not supported by golang.org/x/image/webp.
package image

import (
	"context"

	"github.com/dandandujie/dsr-go/core"
)

// ImageFetcher fetches the bytes of an image identified by an external URL.
//
// The resolver checks a case-insensitive "http" prefix before calling Fetch.
// The fetcher must validate the complete URL and the allowed destinations.
//
// The fetches of one resolve call run concurrently, so an implementation keeps
// their total size within the limit by reserving every chunk it keeps from
// budget before it keeps the chunk, and it rejects a body larger than
// ImageByteBudget.MaxImageBytes. An implementation releases the bytes it has
// reserved before it returns a retryable error, so the retry of that attempt
// reserves them once. The resolver checks the size of the returned bytes
// against ImageLimits as well.
//
// The ctx parameter adapts the Rust trait's asynchronous method to Go; the Rust
// method has no cancellation.
type ImageFetcher interface {
	// Fetch returns the encoded bytes of url.
	Fetch(ctx context.Context, url string, budget *ImageByteBudget) ([]byte, error)
}

// ImagePreprocessor decodes, resizes, and encodes one image for the inference
// backend.
//
// The ctx parameter adapts the Rust trait's asynchronous method to Go; the Rust
// implementation has no cancellation.
type ImagePreprocessor interface {
	// Preprocess returns one encoded image and its dimensions after fitting it
	// into the V4.1 token budget.
	Preprocess(ctx context.Context, data []byte, options PreprocessOptions) (core.ImageInfo, error)
}

// unsupportedFetcher is the default ImageFetcher of the Rust crate: it rejects
// every URL. A resolver whose fetcher is nil uses it.
type unsupportedFetcher struct{}

// Fetch rejects every URL, like the default trait method of the Rust crate.
func (unsupportedFetcher) Fetch(context.Context, string, *ImageByteBudget) ([]byte, error) {
	return nil, NewUnsupportedError("image fetching requires an ImageFetcher implementation")
}

// unsupportedPreprocessor is the default ImagePreprocessor of the Rust crate:
// it rejects every image. A resolver whose preprocessor is nil uses it.
type unsupportedPreprocessor struct{}

// Preprocess rejects every image, like the default trait method of the Rust
// crate.
func (unsupportedPreprocessor) Preprocess(context.Context, []byte, PreprocessOptions) (core.ImageInfo, error) {
	return core.ImageInfo{}, NewUnsupportedError("image preprocessing requires an ImagePreprocessor implementation")
}
