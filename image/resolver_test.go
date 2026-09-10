package image

import (
	"context"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dandandujie/dsr-go/core"
)

// fixedFetcher returns a body of a fixed size and ignores the budget.
type fixedFetcher struct {
	bodyLen int
}

// Fetch returns a zeroed body of the configured size.
func (f fixedFetcher) Fetch(context.Context, string, *ImageByteBudget) ([]byte, error) {
	return make([]byte, f.bodyLen), nil
}

// countingFetcher records how many of its calls run at the same time.
type countingFetcher struct {
	active    atomic.Int64
	maxActive atomic.Int64
}

// Fetch records the concurrency of its calls.
func (f *countingFetcher) Fetch(_ context.Context, _ string, budget *ImageByteBudget) ([]byte, error) {
	active := f.active.Add(1)
	for {
		maximum := f.maxActive.Load()
		if active <= maximum || f.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}
	runtime.Gosched()
	f.active.Add(-1)
	if err := budget.Reserve(1); err != nil {
		return nil, err
	}
	return []byte{0}, nil
}

// noPreprocessor rejects every image, like the default ImagePreprocessor method
// of the Rust crate.
type noPreprocessor struct{}

// Preprocess rejects every image.
func (noPreprocessor) Preprocess(context.Context, []byte, PreprocessOptions) (core.ImageInfo, error) {
	return core.ImageInfo{}, NewUnsupportedError("image preprocessing requires an ImagePreprocessor implementation")
}

// acceptingPreprocessor accepts every image without inspecting its bytes.
type acceptingPreprocessor struct{}

// Preprocess returns the data unchanged with placeholder dimensions.
func (acceptingPreprocessor) Preprocess(_ context.Context, data []byte, _ PreprocessOptions) (core.ImageInfo, error) {
	return core.ImageInfo{Data: data, Width: 1, Height: 1}, nil
}

// limits returns the default limits with the two size limits replaced.
func limits(maxImageBytes, maxTotalBytes int) ImageLimits {
	limits := DefaultImageLimits()
	limits.MaxImageBytes = maxImageBytes
	limits.MaxTotalBytes = maxTotalBytes
	return limits
}

// bytesSource returns an in-memory source of the given encoded size.
func bytesSource(length int) core.ImageSource {
	return core.BytesImageSource(make([]byte, length), core.ImageDetailHigh)
}

// urlSource returns the external URL source used by the ported tests.
func urlSource() core.ImageSource {
	return core.URLImageSource("https://example.com/image.webp", core.ImageDetailHigh)
}

func TestBytesOverTheImageLimitAreRejected(t *testing.T) {
	resolver := NewImageResolver(fixedFetcher{bodyLen: 0}, noPreprocessor{}).WithLimits(limits(4, 64))
	_, err := resolver.Resolve([]core.ImageSource{bytesSource(5)}, NewImageQuota())

	imageErr := requireImageError(t, err, KindImageTooLarge)
	requireEqual(t, imageErr.Size, 5, "Size")
	requireEqual(t, imageErr.Max, 4, "Max")
}

func TestBytesOverTheTotalLimitAreRejected(t *testing.T) {
	resolver := NewImageResolver(fixedFetcher{bodyLen: 0}, noPreprocessor{}).WithLimits(limits(8, 10))
	_, err := resolver.Resolve([]core.ImageSource{bytesSource(6), bytesSource(6)}, NewImageQuota())

	imageErr := requireImageError(t, err, KindTotalSizeTooLarge)
	requireEqual(t, imageErr.Size, 12, "Size")
	requireEqual(t, imageErr.Max, 10, "Max")
}

func TestAFetcherThatIgnoresTheBudgetIsRejected(t *testing.T) {
	resolver := NewImageResolver(fixedFetcher{bodyLen: 5}, noPreprocessor{}).WithLimits(limits(4, 64))
	_, err := resolver.Resolve([]core.ImageSource{urlSource()}, NewImageQuota())

	imageErr := requireImageError(t, err, KindImageTooLarge)
	requireEqual(t, imageErr.Size, 5, "Size")
	requireEqual(t, imageErr.Max, 4, "Max")
}

func TestDataURLsOverTheImageLimitAreRejectedWithoutDecoding(t *testing.T) {
	resolver := NewImageResolver(fixedFetcher{bodyLen: 0}, noPreprocessor{}).WithLimits(limits(4, 64))
	source := core.DataURLImageSource("data:image/webp;base64,"+strings.Repeat("A", 8), core.ImageDetailHigh)
	_, err := resolver.Resolve([]core.ImageSource{source}, NewImageQuota())

	imageErr := requireImageError(t, err, KindImageTooLarge)
	requireEqual(t, imageErr.Size, 6, "Size")
	requireEqual(t, imageErr.Max, 4, "Max")
}

func TestSourcesWithinTheLimitsReachPreprocessing(t *testing.T) {
	resolver := NewImageResolver(fixedFetcher{bodyLen: 4}, noPreprocessor{}).WithLimits(limits(4, 64))
	_, err := resolver.Resolve([]core.ImageSource{urlSource()}, NewImageQuota())

	requireImageError(t, err, KindUnsupported)
}

func TestACompletedCallRecordsItsSources(t *testing.T) {
	resolver := NewImageResolver(fixedFetcher{bodyLen: 0}, acceptingPreprocessor{}).WithLimits(limits(8, 64))
	quota := NewImageQuota()
	data, err := resolver.Resolve([]core.ImageSource{bytesSource(6), bytesSource(6)}, quota)
	requireNoError(t, err)

	requireEqual(t, len(data.Images), 2, "len(Images)")
	requireEqual(t, quota.ImageCount(), 2, "ImageCount()")
	requireEqual(t, quota.ByteSize(), 12, "ByteSize()")
}

func TestAPreprocessingFailureLeavesTheQuotaUnchanged(t *testing.T) {
	resolver := NewImageResolver(fixedFetcher{bodyLen: 0}, noPreprocessor{}).WithLimits(limits(8, 64))
	quota := NewImageQuota()
	_, err := resolver.Resolve([]core.ImageSource{bytesSource(6), bytesSource(6)}, quota)
	requireImageError(t, err, KindUnsupported)

	requireEqual(t, *quota, ImageQuota{}, "quota")
}

func TestResolutionsAreLimitedToMaxConcurrentSources(t *testing.T) {
	const sourceCount = 6
	fetcher := &countingFetcher{}
	requestLimits := limits(8, 64)
	requestLimits.MaxConcurrentSources = 3
	resolver := NewImageResolver(fetcher, noPreprocessor{}).WithLimits(requestLimits)
	sources := make([]core.ImageSource, sourceCount)
	for index := range sources {
		sources[index] = urlSource()
	}
	_, err := resolver.Resolve(sources, NewImageQuota())
	requireImageError(t, err, KindUnsupported)

	requireEqual(t, fetcher.maxActive.Load(), int64(3),
		"the resolver starts at most MaxConcurrentSources fetches at a time")
}

func TestTooManyImagesAreRejectedAcrossCalls(t *testing.T) {
	requestLimits := limits(64, 256)
	requestLimits.MaxImages = 3
	resolver := NewImageResolver(fixedFetcher{bodyLen: 0}, acceptingPreprocessor{}).WithLimits(requestLimits)
	quota := NewImageQuota()
	_, err := resolver.Resolve([]core.ImageSource{bytesSource(1), bytesSource(1)}, quota)
	requireNoError(t, err)

	_, err = resolver.Resolve([]core.ImageSource{bytesSource(1), bytesSource(1)}, quota)
	imageErr := requireImageError(t, err, KindTooManyImages)
	requireEqual(t, imageErr.Max, 3, "Max")
	requireEqual(t, imageErr.Count, 4, "Count")
}

func TestAnEmptySourceIsRejected(t *testing.T) {
	resolver := NewImageResolver(fixedFetcher{bodyLen: 0}, acceptingPreprocessor{}).WithLimits(limits(8, 64))
	_, err := resolver.Resolve([]core.ImageSource{bytesSource(0)}, NewImageQuota())
	requireImageError(t, err, KindEmptyImage)
}

func TestAnInvalidURLIsRejected(t *testing.T) {
	resolver := NewImageResolver(fixedFetcher{bodyLen: 0}, acceptingPreprocessor{}).WithLimits(limits(8, 64))
	source := core.URLImageSource("ftp://example.com/image.png", core.ImageDetailHigh)
	_, err := resolver.Resolve([]core.ImageSource{source}, NewImageQuota())
	requireImageError(t, err, KindInvalidURL)
}

func TestAnOverlongURLIsRejected(t *testing.T) {
	requestLimits := limits(8, 64)
	requestLimits.MaxURLLen = 16
	resolver := NewImageResolver(fixedFetcher{bodyLen: 0}, acceptingPreprocessor{}).WithLimits(requestLimits)
	source := core.URLImageSource("https://example.com/image.png", core.ImageDetailHigh)
	_, err := resolver.Resolve([]core.ImageSource{source}, NewImageQuota())

	imageErr := requireImageError(t, err, KindURLTooLong)
	requireEqual(t, imageErr.Len, len("https://example.com/image.png"), "Len")
	requireEqual(t, imageErr.Max, 16, "Max")
}

func TestABackgroundContextResolveMatchesResolveContext(t *testing.T) {
	resolver := NewImageResolver(fixedFetcher{bodyLen: 4}, acceptingPreprocessor{}).WithLimits(limits(8, 64))
	quota := NewImageQuota()
	data, err := resolver.ResolveContext(context.Background(), []core.ImageSource{urlSource()}, quota)
	requireNoError(t, err)
	requireEqual(t, len(data.Images), 1, "len(Images)")
	requireEqual(t, quota.ByteSize(), 4, "ByteSize()")
}
