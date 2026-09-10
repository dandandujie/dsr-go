package image

import (
	"bytes"
	"encoding/binary"
	stdimage "image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/webp"

	"github.com/dandandujie/dsr-go/core"
)

// inferenceBackground is the background applied where an image has
// transparency: the RGB color #FDFDFD.
const inferenceBackground = 0xfd

// decodeImage decodes one encoded image into an opaque 8-bit RGBA image.
//
// GIF is decoded with the standard library, which returns the first frame, like
// the image crate the Rust preprocessor uses for GIF. The other media types are
// decoded with their standard library or golang.org/x/image decoder.
func decodeImage(data []byte, mediaType core.ImageMediaType) (*stdimage.RGBA, error) {
	reader := bytes.NewReader(data)
	var decoded stdimage.Image
	var err error
	switch mediaType {
	case core.ImageGIF:
		decoded, err = gif.Decode(reader)
	case core.ImageJPEG:
		decoded, err = jpeg.Decode(reader)
	case core.ImagePNG:
		decoded, err = png.Decode(reader)
	case core.ImageWebP:
		decoded, err = webp.Decode(reader)
	default:
		return nil, NewUnsupportedMediaTypeError(mediaType.MIME())
	}
	if err != nil {
		return nil, NewDecodeError(err)
	}
	if decoded.Bounds().Dx() == 0 || decoded.Bounds().Dy() == 0 {
		return nil, NewEmptyImageError()
	}
	return toOpaqueRGBA(decoded), nil
}

// checkDimensions rejects an image whose declared dimensions exceed
// maxDimension, reading the file header only.
//
// It is the Go equivalent of the image crate's Limits check: the header
// decoders of the standard library do not allocate pixel buffers, so the
// allocation limit of the Rust code has no equivalent and only the dimension
// limit is applied.
func checkDimensions(data []byte, mediaType core.ImageMediaType, maxDimension uint32) error {
	reader := bytes.NewReader(data)
	var config stdimage.Config
	var err error
	switch mediaType {
	case core.ImageGIF:
		config, err = gif.DecodeConfig(reader)
	case core.ImageJPEG:
		config, err = jpeg.DecodeConfig(reader)
	case core.ImagePNG:
		config, err = png.DecodeConfig(reader)
	case core.ImageWebP:
		config, err = webp.DecodeConfig(reader)
	default:
		return NewUnsupportedMediaTypeError(mediaType.MIME())
	}
	if err != nil {
		return NewDecodeError(err)
	}
	if uint32(config.Width) > maxDimension || uint32(config.Height) > maxDimension {
		return NewImageDimensionsTooLargeError()
	}
	return nil
}

// toOpaqueRGBA converts a decoded image to an opaque 8-bit RGBA image.
//
// It mirrors normalize_decoded_mat of the Rust crate: a grayscale channel is
// replicated into the three color channels, a 16-bit channel is scaled to eight
// bits by division by 257, and an alpha channel is blended onto the inference
// background with (channel*alpha + background*(255-alpha) + 127) / 255. The Rust
// code works on OpenCV BGR matrices, while this port works on RGB; the channel
// order does not affect the result, because the channels are resized and encoded
// consistently.
func toOpaqueRGBA(src stdimage.Image) *stdimage.RGBA {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	dst := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))
	switch img := src.(type) {
	case *stdimage.Gray:
		for y := 0; y < height; y++ {
			row := img.Pix[img.PixOffset(bounds.Min.X, bounds.Min.Y+y):]
			target := dst.Pix[y*dst.Stride:]
			for x := 0; x < width; x++ {
				value := row[x]
				offset := x * 4
				target[offset], target[offset+1], target[offset+2], target[offset+3] = value, value, value, 0xff
			}
		}
	case *stdimage.Gray16:
		for y := 0; y < height; y++ {
			row := img.Pix[img.PixOffset(bounds.Min.X, bounds.Min.Y+y):]
			target := dst.Pix[y*dst.Stride:]
			for x := 0; x < width; x++ {
				value := eightBit(binary.BigEndian.Uint16(row[x*2:]))
				offset := x * 4
				target[offset], target[offset+1], target[offset+2], target[offset+3] = value, value, value, 0xff
			}
		}
	case *stdimage.NRGBA:
		for y := 0; y < height; y++ {
			row := img.Pix[img.PixOffset(bounds.Min.X, bounds.Min.Y+y):]
			target := dst.Pix[y*dst.Stride:]
			for x := 0; x < width; x++ {
				blendPixel(target[x*4:], row[x*4], row[x*4+1], row[x*4+2], row[x*4+3])
			}
		}
	case *stdimage.RGBA:
		for y := 0; y < height; y++ {
			row := img.Pix[img.PixOffset(bounds.Min.X, bounds.Min.Y+y):]
			target := dst.Pix[y*dst.Stride:]
			for x := 0; x < width; x++ {
				blendPremultipliedPixel(target[x*4:], row[x*4], row[x*4+1], row[x*4+2], row[x*4+3])
			}
		}
	case *stdimage.NRGBA64:
		for y := 0; y < height; y++ {
			row := img.Pix[img.PixOffset(bounds.Min.X, bounds.Min.Y+y):]
			target := dst.Pix[y*dst.Stride:]
			for x := 0; x < width; x++ {
				offset := x * 8
				blendPixel(target[x*4:],
					eightBit(binary.BigEndian.Uint16(row[offset:])),
					eightBit(binary.BigEndian.Uint16(row[offset+2:])),
					eightBit(binary.BigEndian.Uint16(row[offset+4:])),
					eightBit(binary.BigEndian.Uint16(row[offset+6:])))
			}
		}
	case *stdimage.RGBA64:
		for y := 0; y < height; y++ {
			row := img.Pix[img.PixOffset(bounds.Min.X, bounds.Min.Y+y):]
			target := dst.Pix[y*dst.Stride:]
			for x := 0; x < width; x++ {
				offset := x * 8
				blendPremultipliedPixel(target[x*4:],
					eightBit(binary.BigEndian.Uint16(row[offset:])),
					eightBit(binary.BigEndian.Uint16(row[offset+2:])),
					eightBit(binary.BigEndian.Uint16(row[offset+4:])),
					eightBit(binary.BigEndian.Uint16(row[offset+6:])))
			}
		}
	default:
		// A palette, YCbCr, CMYK, or any other image. The color model
		// conversion yields straight (non-premultiplied) alpha, like the OpenCV
		// decoder the Rust crate uses.
		for y := 0; y < height; y++ {
			target := dst.Pix[y*dst.Stride:]
			for x := 0; x < width; x++ {
				converted := color.NRGBAModel.Convert(src.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
				blendPixel(target[x*4:], converted.R, converted.G, converted.B, converted.A)
			}
		}
	}
	return dst
}

// eightBit scales a 16-bit channel to eight bits like OpenCV convert_to with a
// scale of 1/257: the value is divided by 257 and rounded to the nearest integer.
func eightBit(value uint16) uint8 {
	return uint8((uint32(value) + 128) / 257)
}

// blendPixel writes one straight-alpha pixel blended onto the inference
// background.
func blendPixel(target []byte, red, green, blue, alpha uint8) {
	if alpha == 0xff {
		target[0], target[1], target[2], target[3] = red, green, blue, 0xff
		return
	}
	target[0] = blendChannel(red, alpha)
	target[1] = blendChannel(green, alpha)
	target[2] = blendChannel(blue, alpha)
	target[3] = 0xff
}

// blendChannel blends one straight-alpha channel onto the inference background.
func blendChannel(channel, alpha uint8) uint8 {
	value := uint32(channel)*uint32(alpha) + inferenceBackground*uint32(255-alpha) + 127
	return uint8(value / 255)
}

// blendPremultipliedPixel writes one premultiplied-alpha pixel blended onto the
// inference background.
func blendPremultipliedPixel(target []byte, red, green, blue, alpha uint8) {
	if alpha == 0xff {
		target[0], target[1], target[2], target[3] = red, green, blue, 0xff
		return
	}
	background := inferenceBackground * uint32(255-alpha)
	target[0] = uint8((uint32(red)*255 + background + 127) / 255)
	target[1] = uint8((uint32(green)*255 + background + 127) / 255)
	target[2] = uint8((uint32(blue)*255 + background + 127) / 255)
	target[3] = 0xff
}
