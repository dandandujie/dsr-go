// Package encoding renders model prompts and encodes conversations into token
// IDs.
//
// V4 and V4.1 provide prompt rendering in the encoding/v4 subpackages. Attach a
// tokenizer to an encoding to encode conversations into token IDs.
package encoding

import "github.com/dandandujie/dsr-go/core"

// TokenizerEncoder encodes text into model token IDs.
//
// A tokenizer library implements this interface to back a prompt encoding.
type TokenizerEncoder interface {
	// EncodeIDs encodes text into token IDs without added special tokens. The
	// prompt already contains the special token text.
	EncodeIDs(text string) ([]uint32, error)
}

// RenderedPrompt is a rendered prompt and the images its placeholders refer to.
type RenderedPrompt struct {
	// Prompt is the model input string.
	Prompt string
	// ImageSources holds the image sources in the order their placeholders
	// appear in the prompt.
	ImageSources []core.ImageSource
}

// EncodingErrorKind identifies one kind of EncodingError.
type EncodingErrorKind uint8

const (
	// EncodingEncode reports that the rendered prompt could not be encoded.
	EncodingEncode EncodingErrorKind = iota
	// EncodingMissingTokenizer reports that the encoding has no attached
	// tokenizer.
	EncodingMissingTokenizer
)

// EncodingError is a failure to encode a conversation.
type EncodingError struct {
	// Kind identifies the failure.
	Kind EncodingErrorKind
	// Detail describes an EncodingEncode failure.
	Detail string
}

// Error implements the error interface.
func (e EncodingError) Error() string {
	switch e.Kind {
	case EncodingMissingTokenizer:
		return "no tokenizer is attached to the encoding"
	default:
		return "failed to encode conversation: " + e.Detail
	}
}

// PromptEncoding renders model prompts and encodes conversations into token
// IDs.
type PromptEncoding interface {
	// Encode encodes a conversation into model token IDs using the attached
	// tokenizer.
	//
	// The tokenizer must be attached to the encoding before this call, through
	// the encoding's WithTokenizer method. Encode returns an error whose kind
	// is EncodingMissingTokenizer when no tokenizer is attached.
	Encode(c *core.Conversation) ([]uint32, error)

	// RenderConversation renders the conversation and the prefix for the next
	// assistant turn.
	RenderConversation(c *core.Conversation) RenderedPrompt
}
