package image

import "fmt"

// ImageErrorKind identifies one variant of ImageError. The names match the Rust
// enum variants of deepseek-recipe-image.
type ImageErrorKind uint8

// The variants of ImageError. The zero value is KindUnsupported.
const (
	// KindUnsupported is an image source that needs a capability this
	// implementation does not provide.
	KindUnsupported ImageErrorKind = iota
	// KindURLTooLong is an external image URL above ImageLimits.MaxURLLen.
	KindURLTooLong
	// KindInvalidURL is an external image reference without the required
	// "http" prefix.
	KindInvalidURL
	// KindTooManyImages is a request with more images than ImageLimits.MaxImages.
	KindTooManyImages
	// KindImageTooLarge is one image above the encoded size limit.
	KindImageTooLarge
	// KindTotalSizeTooLarge is a request whose images exceed the total encoded
	// size limit.
	KindTotalSizeTooLarge
	// KindInvalidDataURL is a malformed data URL or encoded body.
	KindInvalidDataURL
	// KindEmptyImage is empty image data.
	KindEmptyImage
	// KindUnsupportedMediaType is an unsupported image media type.
	KindUnsupportedMediaType
	// KindImageDimensionsTooLarge is a width or height above the dimension limit.
	KindImageDimensionsTooLarge
	// KindDecode is a decoding failure.
	KindDecode
	// KindResize is a resizing failure.
	KindResize
	// KindEncode is an encoding failure.
	KindEncode
	// KindEncodeFailed is an encoder that reported failure without a message.
	KindEncodeFailed
	// KindTokenBudget is a token-budget fitting failure.
	KindTokenBudget
	// KindFetch is a failed image download.
	KindFetch
	// KindFetchStatus is an image server that returned a non-success status.
	KindFetchStatus
	// KindClient is an HTTP client that could not be constructed.
	KindClient
)

// String returns the Rust variant name of the kind, such as "ImageTooLarge".
func (k ImageErrorKind) String() string {
	switch k {
	case KindUnsupported:
		return "Unsupported"
	case KindURLTooLong:
		return "UrlTooLong"
	case KindInvalidURL:
		return "InvalidUrl"
	case KindTooManyImages:
		return "TooManyImages"
	case KindImageTooLarge:
		return "ImageTooLarge"
	case KindTotalSizeTooLarge:
		return "TotalSizeTooLarge"
	case KindInvalidDataURL:
		return "InvalidDataUrl"
	case KindEmptyImage:
		return "EmptyImage"
	case KindUnsupportedMediaType:
		return "UnsupportedMediaType"
	case KindImageDimensionsTooLarge:
		return "ImageDimensionsTooLarge"
	case KindDecode:
		return "Decode"
	case KindResize:
		return "Resize"
	case KindEncode:
		return "Encode"
	case KindEncodeFailed:
		return "EncodeFailed"
	case KindTokenBudget:
		return "TokenBudget"
	case KindFetch:
		return "Fetch"
	case KindFetchStatus:
		return "FetchStatus"
	case KindClient:
		return "Client"
	default:
		return fmt.Sprintf("ImageErrorKind(%d)", uint8(k))
	}
}

// ImageError is a failure to fetch or preprocess one image. It mirrors the Rust
// enum ImageError: Kind selects the variant and the remaining fields carry the
// values of that variant, so a caller can switch on Kind and inspect only the
// fields the kind documents.
//
// The error message of a variant matches the Rust message, except that the
// message of KindInvalidDataURL and KindUnsupportedMediaType reproduces the
// description of the Go parser rather than the Rust data-url crate.
type ImageError struct {
	// Kind selects the error variant.
	Kind ImageErrorKind
	// Message is the detail of KindUnsupported, KindInvalidDataURL,
	// KindUnsupportedMediaType, KindDecode, KindResize, KindEncode, KindClient,
	// and KindTokenBudget.
	Message string
	// Len is the rejected URL length of KindURLTooLong.
	Len int
	// Max is the maximum of KindURLTooLong, KindTooManyImages,
	// KindImageTooLarge, and KindTotalSizeTooLarge.
	Max int
	// URL is the rejected URL of KindInvalidURL, KindFetch, and KindFetchStatus.
	URL string
	// Size is the rejected encoded size of KindImageTooLarge and
	// KindTotalSizeTooLarge.
	Size int
	// Count is the number of images of KindTooManyImages.
	Count int
	// Status is the HTTP status code of KindFetchStatus.
	Status int
	// Err is the underlying cause of KindDecode, KindResize, KindEncode,
	// KindClient, KindTokenBudget, and KindFetch. It is nil for the variants
	// that carry no cause.
	Err error
}

// Error returns the message of the error variant.
func (e *ImageError) Error() string {
	switch e.Kind {
	case KindUnsupported:
		return "unsupported image source: " + e.Message
	case KindURLTooLong:
		return fmt.Sprintf("external link length %d too long, max link length %d", e.Len, e.Max)
	case KindInvalidURL:
		return fmt.Sprintf("invalid url: %q", e.URL)
	case KindTooManyImages:
		return fmt.Sprintf("too many images: max %d images per request, got %d", e.Max, e.Count)
	case KindImageTooLarge:
		return fmt.Sprintf("image size %d bytes exceeds the limit of %d bytes", e.Size, e.Max)
	case KindTotalSizeTooLarge:
		return fmt.Sprintf("total image size %d bytes exceeds the limit of %d bytes", e.Size, e.Max)
	case KindInvalidDataURL:
		return "invalid data url: " + e.Message
	case KindEmptyImage:
		return "input image data is empty"
	case KindUnsupportedMediaType:
		return "unsupported image media type: " + e.Message
	case KindImageDimensionsTooLarge:
		return "image dimensions exceed the maximum allowed"
	case KindDecode:
		return "failed to decode image: " + e.Message
	case KindResize:
		return "failed to resize image: " + e.Message
	case KindEncode:
		return "failed to encode image: " + e.Message
	case KindEncodeFailed:
		return "failed to encode image"
	case KindTokenBudget:
		return e.Message
	case KindFetch:
		return fmt.Sprintf("failed to fetch image from %s: %s", e.URL, e.Message)
	case KindFetchStatus:
		return fmt.Sprintf("failed to fetch image from %s: unexpected status %d", e.URL, e.Status)
	case KindClient:
		return "failed to create the HTTP client: " + e.Message
	default:
		return "image error"
	}
}

// Unwrap returns the underlying cause of the error, or nil when the variant
// carries none.
func (e *ImageError) Unwrap() error { return e.Err }

// IsRetryable reports whether another attempt of the same operation may
// succeed.
//
// KindFetch is retryable regardless of its cause; KindFetchStatus is retryable
// for status codes of at least 500. Every other variant is not retryable. The
// resolver evaluates the fetcher's error before wrapping it in KindFetch, so a
// returned wrapper may have a different classification.
func (e *ImageError) IsRetryable() bool {
	switch e.Kind {
	case KindFetch:
		return true
	case KindFetchStatus:
		return e.Status >= 500
	default:
		return false
	}
}

// IsServerError reports whether the failure is an internal failure of the
// service rather than invalid input.
//
// It matches the mapping of the Rust example server, which turns KindClient and
// KindTokenBudget into an internal (HTTP 500) error and every other variant into
// a bad request (HTTP 400).
func (e *ImageError) IsServerError() bool {
	return e.Kind == KindClient || e.Kind == KindTokenBudget
}

// NewUnsupportedError returns a KindUnsupported error with the given detail.
func NewUnsupportedError(message string) *ImageError {
	return &ImageError{Kind: KindUnsupported, Message: message}
}

// NewURLTooLongError returns a KindURLTooLong error for a URL of len bytes with
// a maximum of max bytes.
func NewURLTooLongError(length, max int) *ImageError {
	return &ImageError{Kind: KindURLTooLong, Len: length, Max: max}
}

// NewInvalidURLError returns a KindInvalidURL error for the rejected URL.
func NewInvalidURLError(url string) *ImageError {
	return &ImageError{Kind: KindInvalidURL, URL: url}
}

// NewTooManyImagesError returns a KindTooManyImages error for a request that
// carries count images where max are allowed.
func NewTooManyImagesError(max, count int) *ImageError {
	return &ImageError{Kind: KindTooManyImages, Max: max, Count: count}
}

// NewImageTooLargeError returns a KindImageTooLarge error for an image of size
// bytes where max are allowed.
func NewImageTooLargeError(size, max int) *ImageError {
	return &ImageError{Kind: KindImageTooLarge, Size: size, Max: max}
}

// NewTotalSizeTooLargeError returns a KindTotalSizeTooLarge error for images of
// size bytes where max are allowed.
func NewTotalSizeTooLargeError(size, max int) *ImageError {
	return &ImageError{Kind: KindTotalSizeTooLarge, Size: size, Max: max}
}

// NewInvalidDataURLError returns a KindInvalidDataURL error with the given
// detail.
func NewInvalidDataURLError(message string) *ImageError {
	return &ImageError{Kind: KindInvalidDataURL, Message: message}
}

// NewEmptyImageError returns a KindEmptyImage error.
func NewEmptyImageError() *ImageError {
	return &ImageError{Kind: KindEmptyImage}
}

// NewUnsupportedMediaTypeError returns a KindUnsupportedMediaType error for the
// given media type.
func NewUnsupportedMediaTypeError(mediaType string) *ImageError {
	return &ImageError{Kind: KindUnsupportedMediaType, Message: mediaType}
}

// NewImageDimensionsTooLargeError returns a KindImageDimensionsTooLarge error.
func NewImageDimensionsTooLargeError() *ImageError {
	return &ImageError{Kind: KindImageDimensionsTooLarge}
}

// NewDecodeError returns a KindDecode error caused by err.
func NewDecodeError(err error) *ImageError {
	return &ImageError{Kind: KindDecode, Message: causeMessage(err), Err: err}
}

// NewResizeError returns a KindResize error caused by err.
func NewResizeError(err error) *ImageError {
	return &ImageError{Kind: KindResize, Message: causeMessage(err), Err: err}
}

// NewEncodeError returns a KindEncode error caused by err.
func NewEncodeError(err error) *ImageError {
	return &ImageError{Kind: KindEncode, Message: causeMessage(err), Err: err}
}

// NewEncodeFailedError returns a KindEncodeFailed error, which reports an
// encoder that failed without a message.
func NewEncodeFailedError() *ImageError {
	return &ImageError{Kind: KindEncodeFailed}
}

// NewTokenBudgetError returns a KindTokenBudget error caused by a failed
// token-budget fit.
func NewTokenBudgetError(err error) *ImageError {
	return &ImageError{Kind: KindTokenBudget, Message: causeMessage(err), Err: err}
}

// NewFetchError returns a KindFetch error for the rejected URL caused by err.
func NewFetchError(url string, err error) *ImageError {
	return &ImageError{Kind: KindFetch, URL: url, Message: causeMessage(err), Err: err}
}

// NewFetchStatusError returns a KindFetchStatus error for a non-success HTTP
// status.
func NewFetchStatusError(url string, status int) *ImageError {
	return &ImageError{Kind: KindFetchStatus, URL: url, Status: status}
}

// NewClientError returns a KindClient error caused by err.
func NewClientError(err error) *ImageError {
	return &ImageError{Kind: KindClient, Message: causeMessage(err), Err: err}
}

// causeMessage returns the message of err, or a placeholder when err is nil.
func causeMessage(err error) string {
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}
