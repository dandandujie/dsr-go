package image

import (
	"context"
	"errors"
	"sync"

	"github.com/dandandujie/dsr-go/core"
)

// ImageResolver resolves image sources into preprocessed images.
//
// It supports external URLs, data URLs, and encoded bytes. It applies request
// limits and preserves the source order. The caller supplies the fetcher and
// preprocessor. At most ImageLimits.MaxConcurrentSources sources are fetched and
// preprocessed concurrently.
type ImageResolver struct {
	// fetcher downloads the bytes of an external URL source.
	fetcher ImageFetcher
	// preprocessor encodes one fetched image for the inference backend.
	preprocessor ImagePreprocessor
	// limits holds the request limits.
	limits ImageLimits
	// retry holds the retry policy of image downloads.
	retry RetryPolicy
}

// NewImageResolver combines an image fetcher with an image preprocessor.
//
// A nil fetcher or preprocessor rejects every URL or image, matching the default
// methods of the Rust traits. All other settings default to DefaultImageLimits
// and DownloadRetryPolicy.
func NewImageResolver(fetcher ImageFetcher, preprocessor ImagePreprocessor) *ImageResolver {
	if fetcher == nil {
		fetcher = unsupportedFetcher{}
	}
	if preprocessor == nil {
		preprocessor = unsupportedPreprocessor{}
	}
	return &ImageResolver{
		fetcher:      fetcher,
		preprocessor: preprocessor,
		limits:       DefaultImageLimits(),
		retry:        DownloadRetryPolicy(),
	}
}

// WithLimits replaces the default limits and returns the resolver.
func (r *ImageResolver) WithLimits(limits ImageLimits) *ImageResolver {
	r.limits = limits
	return r
}

// WithRetry replaces the default retry policy of image downloads and returns the
// resolver.
func (r *ImageResolver) WithRetry(retry RetryPolicy) *ImageResolver {
	r.retry = retry
	return r
}

// Resolve resolves sources concurrently and preserves their order in the
// result. It is ResolveContext with a background context.
//
// Pass one ImageQuota per request and reuse it across calls, so the count and
// total byte limits apply across calls. Dimension limits use the accumulated
// count including this call; earlier images are not rechecked. The call reserves
// the bytes it keeps from one ImageByteBudget it shares with every fetch, so the
// sources of the call are limited together.
//
// Resolve returns an ImageError when a source cannot be fetched, decoded, or
// preprocessed, or when the request exceeds a limit. A failed call leaves quota
// unchanged: the sources of the call are recorded once every one of them is
// preprocessed.
func (r *ImageResolver) Resolve(sources []core.ImageSource, quota *ImageQuota) (*core.MultiModalData, error) {
	return r.ResolveContext(context.Background(), sources, quota)
}

// ResolveContext resolves sources like Resolve and cancels the work when ctx is
// done.
//
// Unlike the Rust buffered stream, which stops polling the remaining sources
// after the first failure, the Go port attempts every source of a call; the
// error it returns is still the failure of the first source in source order.
func (r *ImageResolver) ResolveContext(ctx context.Context, sources []core.ImageSource, quota *ImageQuota) (*core.MultiModalData, error) {
	requestImageCount := quota.ImageCount() + len(sources)
	if requestImageCount > r.limits.MaxImages {
		return nil, NewTooManyImagesError(r.limits.MaxImages, requestImageCount)
	}

	// The bytes of earlier calls of this request are recorded in quota rather
	// than reserved again in this budget.
	budget := NewImageByteBudget(r.limits.MaxImageBytes, r.limits.MaxTotalBytes, quota.ByteSize())
	concurrency := max(r.limits.MaxConcurrentSources, 1)
	encoded, err := concurrentMapIndexed(ctx, concurrency, sources,
		func(ctx context.Context, _ int, source core.ImageSource) ([]byte, error) {
			return r.encodedBytes(ctx, source, budget)
		})
	if err != nil {
		return nil, err
	}

	// A fetch reserves the bytes it keeps while it reads a body. This check
	// rejects an implementation that returns more than it reserved.
	requestBytes := quota.ByteSize()
	for _, data := range encoded {
		if len(data) > r.limits.MaxImageBytes {
			return nil, NewImageTooLargeError(len(data), r.limits.MaxImageBytes)
		}
		requestBytes += len(data)
		if requestBytes > r.limits.MaxTotalBytes {
			return nil, NewTotalSizeTooLargeError(requestBytes, r.limits.MaxTotalBytes)
		}
	}
	admittedBytes := requestBytes - quota.ByteSize()

	images, err := concurrentMapIndexed(ctx, concurrency, sources,
		func(ctx context.Context, index int, source core.ImageSource) (core.ImageInfo, error) {
			options := r.limits.PreprocessOptions(source.Detail, requestImageCount)
			return r.preprocessor.Preprocess(ctx, encoded[index], options)
		})
	if err != nil {
		return nil, err
	}

	// The call records its sources only once all of them are preprocessed, so a
	// failure or a cancellation leaves the quota unchanged.
	quota.add(len(sources), admittedBytes)
	return &core.MultiModalData{Images: images}, nil
}

// encodedBytes returns the encoded bytes of one image source.
func (r *ImageResolver) encodedBytes(ctx context.Context, source core.ImageSource, budget *ImageByteBudget) ([]byte, error) {
	var data []byte
	switch source.Kind {
	case core.ImageSourceDataURL:
		decoded, err := decodeDataURL(source.DataURL, budget)
		if err != nil {
			return nil, err
		}
		data = decoded
	case core.ImageSourceURL:
		// Direct sources receive the same HTTP prefix check as protocol input.
		// The fetcher validates the full URL.
		if !core.IsHTTPURL(source.URL) {
			return nil, NewInvalidURLError(source.URL)
		}
		if len(source.URL) > r.limits.MaxURLLen {
			return nil, NewURLTooLongError(len(source.URL), r.limits.MaxURLLen)
		}
		fetched, err := ExecuteRetry(ctx, r.retry,
			func(int) ([]byte, error) { return r.fetcher.Fetch(ctx, source.URL, budget) },
			retryableImageError)
		if err != nil {
			var imageErr *ImageError
			// The fetcher already reports the rejected URL.
			if errors.As(err, &imageErr) && (imageErr.Kind == KindFetch || imageErr.Kind == KindFetchStatus) {
				return nil, err
			}
			return nil, NewFetchError(source.URL, err)
		}
		data = fetched
	case core.ImageSourceBytes:
		if err := reserveImageBytes(budget, len(source.Data)); err != nil {
			return nil, err
		}
		data = source.Data
	default:
		return nil, NewUnsupportedError("unknown image source kind")
	}
	if len(data) == 0 {
		return nil, NewEmptyImageError()
	}
	return data, nil
}

// reserveImageBytes reserves the bytes of one image that is already in memory.
func reserveImageBytes(budget *ImageByteBudget, bytes int) error {
	maxBytes := budget.MaxImageBytes()
	if bytes > maxBytes {
		return NewImageTooLargeError(bytes, maxBytes)
	}
	return budget.Reserve(bytes)
}

// decodeDataURL decodes a base64 or percent-encoded data URL with a supported
// image media type.
func decodeDataURL(dataURL string, budget *ImageByteBudget) ([]byte, error) {
	parsed, err := processDataURL(dataURL)
	if err != nil {
		return nil, NewInvalidDataURLError(err.Error())
	}
	if _, supported := core.ImageMediaTypeFromMIME(parsed.mediaType); !supported {
		return nil, NewUnsupportedMediaTypeError(parsed.mediaType)
	}
	// A data URL carries the image in the request itself, so the decoded size is
	// checked before the body is decoded and again once it is known.
	estimated := estimatedDecodedLen(dataURL)
	maxImageBytes := budget.MaxImageBytes()
	if estimated > maxImageBytes {
		return nil, NewImageTooLargeError(estimated, maxImageBytes)
	}
	data, err := parsed.decode()
	if err != nil {
		return nil, NewInvalidDataURLError(err.Error())
	}
	if err := reserveImageBytes(budget, len(data)); err != nil {
		return nil, err
	}
	return data, nil
}

// retryableImageError reports whether err is an ImageError whose variant may
// succeed on another attempt. An error that is not an ImageError is not
// retryable.
func retryableImageError(err error) bool {
	var imageErr *ImageError
	if errors.As(err, &imageErr) {
		return imageErr.IsRetryable()
	}
	return false
}

// concurrentMapIndexed applies transform to every item with at most concurrency
// goroutines and returns the results in item order.
//
// Every item is attempted, even after an earlier item fails. The returned error
// is the failure of the first item in order, like the Rust buffered stream.
func concurrentMapIndexed[T, U any](
	ctx context.Context,
	concurrency int,
	items []T,
	transform func(ctx context.Context, index int, item T) (U, error),
) ([]U, error) {
	if len(items) == 0 {
		return nil, nil
	}
	semaphore := make(chan struct{}, concurrency)
	results := make([]U, len(items))
	errs := make([]error, len(items))
	var group sync.WaitGroup
	for index, item := range items {
		group.Add(1)
		go func() {
			defer group.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			results[index], errs[index] = transform(ctx, index, item)
		}()
	}
	group.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}
