// Package protocol holds validation and image helpers shared by the protocol
// adapters.
package protocol

import (
	"encoding/base64"
	"strings"

	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/recipe/request"
)

// UnsupportedDocumentPlaceholder is the text substituted for document blocks by
// the Messages and Responses adapters. Document contents are not read or
// forwarded to the backend.
const UnsupportedDocumentPlaceholder = "[Unsupported Document]"

// UnsupportedImageErr is the error message for an image whose bytes are missing
// or unsupported.
const UnsupportedImageErr = "You have uploaded an unsupported image. Please make sure your image is valid and has one of the following formats: webp, png, jpeg, and gif."

// ExternalImageSource converts an external image URL into an image source.
//
// It checks the case-insensitive "http" prefix and a UTF-8 byte length no
// greater than core.MaxImageURLLen. Full URL validation belongs to the fetcher.
func ExternalImageSource(url string, detail *core.ImageDetail, path string) (core.ImageSource, error) {
	if !core.IsHTTPURL(url) {
		return core.ImageSource{}, request.BadRequestf("%s: invalid url: %q", path, url)
	}
	if len(url) > core.MaxImageURLLen {
		return core.ImageSource{}, request.BadRequestf(
			"%s: external link length %d too long, max link length %d. Consider passing file data directly with base64 data url instead.",
			path, len(url), core.MaxImageURLLen)
	}
	return core.URLImageSource(url, detailOrHigh(detail)), nil
}

// DataURLImageSource converts a data URL into an image source.
//
// The body is not inspected. The resolver accepts both a base64 body and a
// percent-encoded body.
func DataURLImageSource(dataURL string, detail *core.ImageDetail) core.ImageSource {
	return core.DataURLImageSource(dataURL, detailOrHigh(detail))
}

// ImageURLSource converts an image_url value into an image source.
//
// Values with a case-insensitive "http" prefix are external. Other values must
// contain the literal "base64," marker. Data URL syntax, media type, and body
// decoding are checked later by the image resolver.
func ImageURLSource(url string, detail *core.ImageDetail, path string) (core.ImageSource, error) {
	if core.IsHTTPURL(url) {
		return ExternalImageSource(url, detail, path)
	}
	if strings.Contains(url, "base64,") {
		return DataURLImageSource(url, detail), nil
	}
	return core.ImageSource{}, request.BadRequestf("%s: Unsupported image_url format", path)
}

// Base64ImageSource builds a base64 data URL from a declared media type and
// base64 payload.
//
// It normalizes trailing padding and validates the standard base64 encoding.
// Empty payloads and invalid image contents are checked by the resolver and
// preprocessor, respectively.
func Base64ImageSource(mediaType, data, path string) (core.ImageSource, error) {
	resolved, ok := core.ImageMediaTypeFromMIME(mediaType)
	if !ok {
		return core.ImageSource{}, UnsupportedImage(path)
	}
	data = normalizeBase64Padding(data)
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		return core.ImageSource{}, request.BadRequestf("%s: base64 decode error", path)
	}
	return core.DataURLImageSource("data:"+resolved.MIME()+";base64,"+data, core.ImageDetailHigh), nil
}

// UnsupportedImage returns the error for an unsupported image.
func UnsupportedImage(path string) error {
	return request.BadRequestf("%s: %s", path, UnsupportedImageErr)
}

// ImageNotAllowed returns the error for an image in a message that does not
// accept images.
func ImageNotAllowed(role string) error {
	return request.BadRequestf("Image in %s message is unsupported", role)
}

// FileIDUnsupported returns the error for a file identifier, which requires a
// file service this library does not provide.
func FileIDUnsupported(path string) error {
	return request.BadRequestf(
		"%s: file_id is not supported. Pass the image as a base64 data url instead.", path)
}

func detailOrHigh(detail *core.ImageDetail) core.ImageDetail {
	if detail == nil {
		return core.ImageDetailHigh
	}
	return *detail
}

// normalizeBase64Padding normalizes the padding of a base64 payload.
//
// A caller may supply the wrong number of trailing "=" characters. A payload
// carrying at least one trailing "=" has its padding dropped and replaced with
// the canonical amount. A payload with no trailing "=" is returned unchanged,
// because a truncated payload and an unpadded payload are indistinguishable. A
// payload whose unpadded length leaves a single symbol is also returned
// unchanged, so the decoder reports the invalid payload.
func normalizeBase64Padding(data string) string {
	if !strings.HasSuffix(data, "=") {
		return data
	}
	unpadded := strings.TrimRight(data, "=")
	remainder := len(unpadded) % 4
	if remainder == 1 {
		return data
	}
	return unpadded + strings.Repeat("=", (4-remainder)%4)
}
