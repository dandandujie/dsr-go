package image

import (
	"sync/atomic"

	"github.com/dandandujie/dsr-go/core"
)

// Default limits of ImageLimits. They match the Rust crate.
const (
	// DefaultMaxImages is the maximum number of images in one request.
	DefaultMaxImages = 600
	// DefaultMaxImageBytes is the maximum encoded size of one image: 32 MiB.
	DefaultMaxImageBytes = 32 * 1024 * 1024
	// DefaultMaxTotalBytes is the maximum total encoded size of the images of
	// one request: 64 MiB.
	DefaultMaxTotalBytes = 64 * 1024 * 1024
	// DefaultMaxConcurrentSources is the maximum number of sources processed
	// concurrently by one resolve call.
	DefaultMaxConcurrentSources = 8
	// DefaultMaxDimensionPx is the maximum width or height in pixels when a
	// request has few images.
	DefaultMaxDimensionPx = 8192
	// DefaultMaxDimensionOnManyImagesPx is the maximum width or height in
	// pixels when a request has many images.
	DefaultMaxDimensionOnManyImagesPx = 4096
	// DefaultManyImagesThreshold is the image count at which
	// DefaultMaxDimensionOnManyImagesPx applies.
	DefaultManyImagesThreshold = 15
	// DefaultLowDetailMaxDimensionPx is the maximum long side before token
	// budget fitting at low detail.
	DefaultLowDetailMaxDimensionPx = 512
)

// ImageLimits holds the limits applied to the images of one request.
type ImageLimits struct {
	// MaxImages is the maximum number of images in one request.
	MaxImages int
	// MaxURLLen is the maximum UTF-8 byte length of an external image URL.
	MaxURLLen int
	// MaxImageBytes is the maximum encoded size of one image.
	MaxImageBytes int
	// MaxTotalBytes is the maximum total encoded size of the images of one
	// request.
	MaxTotalBytes int
	// MaxConcurrentSources is the maximum number of sources processed
	// concurrently by one resolve call. A value of zero is treated as one.
	MaxConcurrentSources int
	// MaxDimensionPx is the maximum width or height in pixels when a request
	// has few images.
	MaxDimensionPx uint32
	// MaxDimensionOnManyImagesPx is the maximum width or height in pixels when
	// a request has many images.
	MaxDimensionOnManyImagesPx uint32
	// ManyImagesThreshold is the image count at which
	// MaxDimensionOnManyImagesPx applies.
	ManyImagesThreshold int
	// LowDetailMaxDimensionPx is the maximum long side before token-budget
	// fitting at low detail. The token specification may subsequently upscale
	// the image.
	LowDetailMaxDimensionPx uint32
}

// DefaultImageLimits returns the limits of the Rust crate's Default
// implementation.
func DefaultImageLimits() ImageLimits {
	return ImageLimits{
		MaxImages:                  DefaultMaxImages,
		MaxURLLen:                  core.MaxImageURLLen,
		MaxImageBytes:              DefaultMaxImageBytes,
		MaxTotalBytes:              DefaultMaxTotalBytes,
		MaxConcurrentSources:       DefaultMaxConcurrentSources,
		MaxDimensionPx:             DefaultMaxDimensionPx,
		MaxDimensionOnManyImagesPx: DefaultMaxDimensionOnManyImagesPx,
		ManyImagesThreshold:        DefaultManyImagesThreshold,
		LowDetailMaxDimensionPx:    DefaultLowDetailMaxDimensionPx,
	}
}

// MaxDimension returns the maximum accepted width or height for a request
// carrying imageCount images.
func (l ImageLimits) MaxDimension(imageCount int) uint32 {
	if imageCount < l.ManyImagesThreshold {
		return l.MaxDimensionPx
	}
	return l.MaxDimensionOnManyImagesPx
}

// PreprocessOptions returns the preprocessing options for a request carrying
// imageCount images at the given detail level.
func (l ImageLimits) PreprocessOptions(detail core.ImageDetail, imageCount int) PreprocessOptions {
	return PreprocessOptions{
		Detail:                  detail,
		MaxDimensionPx:          l.MaxDimension(imageCount),
		LowDetailMaxDimensionPx: l.LowDetailMaxDimensionPx,
	}
}

// ImageQuota is the usage accumulated over the resolve calls of one request.
//
// The image count and total source byte limits are enforced across
// ImageResolver.Resolve calls. Create one quota per request and pass the same
// quota to every call. A call records its sources after preprocessing every one
// of them, so a failed call adds nothing.
//
// The zero value is an empty quota.
type ImageQuota struct {
	images int
	bytes  int
}

// NewImageQuota returns an empty quota.
func NewImageQuota() *ImageQuota { return &ImageQuota{} }

// ImageCount returns the number of sources recorded by the completed calls.
func (q *ImageQuota) ImageCount() int { return q.images }

// ByteSize returns the encoded source bytes recorded by the completed calls.
func (q *ImageQuota) ByteSize() int { return q.bytes }

// add records images preprocessed sources of bytes encoded size.
func (q *ImageQuota) add(images, bytes int) {
	q.images += images
	q.bytes += bytes
}

// ImageByteBudget holds the encoded bytes still available to the image sources
// of one resolve call.
//
// ImageResolver.Resolve creates one budget for each call and passes it to every
// fetch of that call. A fetch reserves the bytes it keeps as its body arrives,
// so the fetches running concurrently cannot together exceed
// ImageLimits.MaxTotalBytes. A budget is not shared between calls: the images of
// earlier calls are accounted for through the ImageQuota the call was given.
//
// A budget is safe for concurrent use. It must not be copied after first use;
// construct it with NewImageByteBudget.
type ImageByteBudget struct {
	// remaining is the number of bytes still available to the sources of one
	// resolve call.
	remaining atomic.Int64
	// maxImageBytes is the maximum encoded size of one image.
	maxImageBytes int
	// maxTotalBytes is the maximum total encoded size of the images of one
	// request.
	maxTotalBytes int
}

// NewImageByteBudget returns the budget of one request that has already
// accumulated usedBytes.
//
// maxImageBytes limits one image, and maxTotalBytes limits every image of the
// request.
func NewImageByteBudget(maxImageBytes, maxTotalBytes, usedBytes int) *ImageByteBudget {
	available := int64(maxTotalBytes) - int64(usedBytes)
	if available < 0 {
		available = 0
	}
	budget := &ImageByteBudget{
		maxImageBytes: maxImageBytes,
		maxTotalBytes: maxTotalBytes,
	}
	budget.remaining.Store(available)
	return budget
}

// MaxImageBytes returns the maximum encoded size of one image.
func (b *ImageByteBudget) MaxImageBytes() int { return b.maxImageBytes }

// Reserve reserves bytes of one image.
//
// It returns a KindTotalSizeTooLarge error when the request would exceed its
// total encoded size. A failed reservation reserves nothing.
func (b *ImageByteBudget) Reserve(bytes int) error {
	for {
		available := b.remaining.Load()
		if available < int64(bytes) {
			used := int64(b.maxTotalBytes) - available
			if used < 0 {
				used = 0
			}
			return NewTotalSizeTooLargeError(int(used)+bytes, b.maxTotalBytes)
		}
		if b.remaining.CompareAndSwap(available, available-int64(bytes)) {
			return nil
		}
	}
}

// Release returns bytes reserved by an attempt that failed.
func (b *ImageByteBudget) Release(bytes int) {
	b.remaining.Add(int64(bytes))
}

// PreprocessOptions holds the options of one image preprocessing call.
type PreprocessOptions struct {
	// Detail is the requested detail level.
	Detail core.ImageDetail
	// MaxDimensionPx is the maximum accepted source width or height in pixels,
	// before resizing.
	MaxDimensionPx uint32
	// LowDetailMaxDimensionPx is the maximum long side before token-budget
	// fitting at low detail. The token specification may subsequently upscale
	// the image.
	LowDetailMaxDimensionPx uint32
}
