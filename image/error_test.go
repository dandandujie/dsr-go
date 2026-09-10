package image

import (
	"errors"
	"testing"

	"github.com/dandandujie/dsr-go/core"
)

func TestImageErrorMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"unsupported", NewUnsupportedError("no fetcher"), "unsupported image source: no fetcher"},
		{"url too long", NewURLTooLongError(10, 8), "external link length 10 too long, max link length 8"},
		{"invalid url", NewInvalidURLError("ftp://example.com"), "invalid url: \"ftp://example.com\""},
		{"too many images", NewTooManyImagesError(600, 601), "too many images: max 600 images per request, got 601"},
		{"image too large", NewImageTooLargeError(5, 4), "image size 5 bytes exceeds the limit of 4 bytes"},
		{"total too large", NewTotalSizeTooLargeError(12, 10), "total image size 12 bytes exceeds the limit of 10 bytes"},
		{"invalid data url", NewInvalidDataURLError("not a valid data url"), "invalid data url: not a valid data url"},
		{"empty image", NewEmptyImageError(), "input image data is empty"},
		{"unsupported media type", NewUnsupportedMediaTypeError("image/bmp"), "unsupported image media type: image/bmp"},
		{"dimensions", NewImageDimensionsTooLargeError(), "image dimensions exceed the maximum allowed"},
		{"decode", NewDecodeError(errors.New("boom")), "failed to decode image: boom"},
		{"resize", NewResizeError(errors.New("boom")), "failed to resize image: boom"},
		{"encode", NewEncodeError(errors.New("boom")), "failed to encode image: boom"},
		{"encode failed", NewEncodeFailedError(), "failed to encode image"},
		{"fetch", NewFetchError("https://example.com", errors.New("boom")), "failed to fetch image from https://example.com: boom"},
		{"fetch status", NewFetchStatusError("https://example.com", 503), "failed to fetch image from https://example.com: unexpected status 503"},
		{"client", NewClientError(errors.New("boom")), "failed to create the HTTP client: boom"},
		{
			"token budget",
			NewTokenBudgetError(&core.CalcResizeError{MaxIter: 10}),
			(&core.CalcResizeError{MaxIter: 10}).Error(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requireEqual(t, test.err.Error(), test.want, "Error()")
		})
	}
}

func TestImageErrorKinds(t *testing.T) {
	requireEqual(t, NewUnsupportedError("x").Kind.String(), "Unsupported", "Kind.String()")
	requireEqual(t, NewURLTooLongError(1, 1).Kind.String(), "UrlTooLong", "Kind.String()")
	requireEqual(t, NewInvalidURLError("x").Kind.String(), "InvalidUrl", "Kind.String()")
	requireEqual(t, NewTooManyImagesError(1, 1).Kind.String(), "TooManyImages", "Kind.String()")
	requireEqual(t, NewImageTooLargeError(1, 1).Kind.String(), "ImageTooLarge", "Kind.String()")
	requireEqual(t, NewTotalSizeTooLargeError(1, 1).Kind.String(), "TotalSizeTooLarge", "Kind.String()")
	requireEqual(t, NewInvalidDataURLError("x").Kind.String(), "InvalidDataUrl", "Kind.String()")
	requireEqual(t, NewEmptyImageError().Kind.String(), "EmptyImage", "Kind.String()")
	requireEqual(t, NewUnsupportedMediaTypeError("x").Kind.String(), "UnsupportedMediaType", "Kind.String()")
	requireEqual(t, NewImageDimensionsTooLargeError().Kind.String(), "ImageDimensionsTooLarge", "Kind.String()")
	requireEqual(t, NewDecodeError(errors.New("x")).Kind.String(), "Decode", "Kind.String()")
	requireEqual(t, NewResizeError(errors.New("x")).Kind.String(), "Resize", "Kind.String()")
	requireEqual(t, NewEncodeError(errors.New("x")).Kind.String(), "Encode", "Kind.String()")
	requireEqual(t, NewEncodeFailedError().Kind.String(), "EncodeFailed", "Kind.String()")
	requireEqual(t, NewTokenBudgetError(errors.New("x")).Kind.String(), "TokenBudget", "Kind.String()")
	requireEqual(t, NewFetchError("x", errors.New("y")).Kind.String(), "Fetch", "Kind.String()")
	requireEqual(t, NewFetchStatusError("x", 500).Kind.String(), "FetchStatus", "Kind.String()")
	requireEqual(t, NewClientError(errors.New("x")).Kind.String(), "Client", "Kind.String()")
}

func TestImageErrorRetryability(t *testing.T) {
	requireEqual(t, NewFetchError("u", errors.New("x")).IsRetryable(), true, "Fetch")
	requireEqual(t, NewFetchStatusError("u", 500).IsRetryable(), true, "FetchStatus 500")
	requireEqual(t, NewFetchStatusError("u", 499).IsRetryable(), false, "FetchStatus 499")
	requireEqual(t, NewImageTooLargeError(1, 1).IsRetryable(), false, "ImageTooLarge")
}

func TestImageErrorServerClassification(t *testing.T) {
	requireEqual(t, NewClientError(errors.New("x")).IsServerError(), true, "Client")
	requireEqual(t, NewTokenBudgetError(errors.New("x")).IsServerError(), true, "TokenBudget")
	requireEqual(t, NewEmptyImageError().IsServerError(), false, "EmptyImage")
	requireEqual(t, NewDecodeError(errors.New("x")).IsServerError(), false, "Decode")
}

func TestImageErrorUnwrapsItsCause(t *testing.T) {
	cause := errors.New("boom")
	if !errors.Is(NewDecodeError(cause), cause) {
		t.Fatal("errors.Is(NewDecodeError(cause), cause) = false")
	}
	if !errors.Is(NewTokenBudgetError(cause), cause) {
		t.Fatal("errors.Is(NewTokenBudgetError(cause), cause) = false")
	}
}
