package core

import (
	"strings"
)

// ImageSpecialToken is the placeholder written into the prompt for every image.
const ImageSpecialToken = "<｜image｜>"

// ImageSpecialTokenID is the token ID that the V4.1 tokenizer assigns to
// ImageSpecialToken. A caller that encodes the prompt into token IDs can
// compare against this ID.
const ImageSpecialTokenID uint32 = 129264

// MaxImageURLLen is the maximum UTF-8 byte length of an external image URL
// during conversion.
const MaxImageURLLen = 8192

// ImageMediaType is a media type accepted for image input.
type ImageMediaType uint8

const (
	// ImageJPEG is the image/jpeg media type.
	ImageJPEG ImageMediaType = iota
	// ImagePNG is the image/png media type.
	ImagePNG
	// ImageGIF is the image/gif media type.
	ImageGIF
	// ImageWebP is the image/webp media type.
	ImageWebP
)

// MIME returns the canonical media type string.
func (m ImageMediaType) MIME() string {
	switch m {
	case ImagePNG:
		return "image/png"
	case ImageGIF:
		return "image/gif"
	case ImageWebP:
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

// ImageMediaTypeFromMIME resolves a media type string.
//
// Parameters after ";" are ignored, matching is case-insensitive, and
// "image/jpg" is accepted as an alias of "image/jpeg". ok is false for an
// unsupported media type.
func ImageMediaTypeFromMIME(mime string) (mediaType ImageMediaType, ok bool) {
	if index := strings.IndexByte(mime, ';'); index >= 0 {
		mime = mime[:index]
	}
	mime = strings.TrimSpace(mime)
	kind, subtype, found := strings.Cut(mime, "/")
	if !found || !strings.EqualFold(kind, "image") {
		return 0, false
	}
	switch {
	case strings.EqualFold(subtype, "jpeg"), strings.EqualFold(subtype, "jpg"):
		return ImageJPEG, true
	case strings.EqualFold(subtype, "png"):
		return ImagePNG, true
	case strings.EqualFold(subtype, "gif"):
		return ImageGIF, true
	case strings.EqualFold(subtype, "webp"):
		return ImageWebP, true
	}
	return 0, false
}

// ImageDetail is the requested image detail level.
//
// The preprocessor applies an additional long-side limit for ImageDetailLow
// before fitting the token budget. All levels still undergo token-budget
// fitting and WebP encoding, which can resize the image.
type ImageDetail uint8

const (
	// ImageDetailLow requests the low detail level.
	ImageDetailLow ImageDetail = iota
	// ImageDetailHigh is the default detail level.
	ImageDetailHigh
	// ImageDetailOriginal requests the original detail level.
	ImageDetailOriginal
	// ImageDetailAuto requests the automatic detail level.
	ImageDetailAuto
)

// IsLow reports whether the additional low-detail long-side limit applies.
func (d ImageDetail) IsLow() bool { return d == ImageDetailLow }

// String returns the lowercase name used by the protocol schemas.
func (d ImageDetail) String() string {
	switch d {
	case ImageDetailLow:
		return "low"
	case ImageDetailOriginal:
		return "original"
	case ImageDetailAuto:
		return "auto"
	default:
		return "high"
	}
}

// ParseImageDetail resolves a lowercase detail name.
func ParseImageDetail(value string) (ImageDetail, bool) {
	switch value {
	case "low":
		return ImageDetailLow, true
	case "high":
		return ImageDetailHigh, true
	case "original":
		return ImageDetailOriginal, true
	case "auto":
		return ImageDetailAuto, true
	}
	return ImageDetailHigh, false
}

// ImageSourceKind identifies one variant of ImageSource.
type ImageSourceKind uint8

const (
	// ImageSourceDataURL is a data URL with a base64 or percent-encoded body.
	ImageSourceDataURL ImageSourceKind = iota
	// ImageSourceURL is an external image URL.
	ImageSourceURL
	// ImageSourceBytes is encoded image data supplied by the caller.
	ImageSourceBytes
)

// ImageSource is one image supplied with a conversation, before resolution.
type ImageSource struct {
	Kind ImageSourceKind
	// DataURL is the data URL of a ImageSourceDataURL source.
	DataURL string
	// URL is the external URL of a ImageSourceURL source.
	URL string
	// Data holds the encoded bytes of a ImageSourceBytes source.
	Data []byte
	// Detail is the requested detail level.
	Detail ImageDetail
}

// DataURLImageSource builds a data URL image source.
func DataURLImageSource(dataURL string, detail ImageDetail) ImageSource {
	return ImageSource{Kind: ImageSourceDataURL, DataURL: dataURL, Detail: detail}
}

// URLImageSource builds an external URL image source.
func URLImageSource(url string, detail ImageDetail) ImageSource {
	return ImageSource{Kind: ImageSourceURL, URL: url, Detail: detail}
}

// BytesImageSource builds an image source from encoded bytes.
func BytesImageSource(data []byte, detail ImageDetail) ImageSource {
	return ImageSource{Kind: ImageSourceBytes, Data: data, Detail: detail}
}

// IsHTTPURL reports whether a value is an external image URL.
//
// It checks whether the first four bytes spell "http", ignoring ASCII case.
// This classifies external image references during conversion and resolution;
// it does not validate URL syntax, the complete scheme, or the destination.
func IsHTTPURL(url string) bool {
	if len(url) < 4 {
		return false
	}
	return strings.EqualFold(url[:4], "http")
}

// ImageInfo is a preprocessed image sent to the inference backend.
type ImageInfo struct {
	// Data holds the encoded image bytes.
	Data []byte
	// Width is the width in pixels after preprocessing.
	Width uint32
	// Height is the height in pixels after preprocessing.
	Height uint32
}

// MultiModalData holds the images attached to one conversation request.
type MultiModalData struct {
	// Images are in the order their placeholders appear in the prompt.
	Images []ImageInfo
}

// IsEmpty reports whether no image is attached.
func (d MultiModalData) IsEmpty() bool { return len(d.Images) == 0 }

// ImageTokenAdjustment returns the number of prompt tokens beyond one per
// placeholder.
//
// Every image contributes one placeholder token to the prompt and
// ImageTokenSpec.CalcTokenLen tokens to the inference input. The adjustment is
// the difference over all images.
func (d MultiModalData) ImageTokenAdjustment() (int, error) {
	spec := V41ImageTokenSpec()
	adjustment := 0
	for _, image := range d.Images {
		tokens, err := spec.CalcTokenLen(int(image.Width), int(image.Height))
		if err != nil {
			return 0, err
		}
		adjustment += tokens - 1
	}
	return adjustment, nil
}
