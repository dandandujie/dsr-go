// Package request holds shared request data and the conversion contract for
// protocol adapters.
package request

import (
	"fmt"

	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// InferenceOptions holds the inference parameters extracted from a protocol
// request.
//
// Unspecified parameters remain nil; model-specific defaults belong to the
// caller.
type InferenceOptions struct {
	// MaxTokens limits the number of generated tokens.
	MaxTokens *uint32
	// Temperature is the sampling temperature.
	Temperature *float32
	// TopP is the nucleus sampling probability.
	TopP *float32
	// ThinkingBudgetTokens is passed through for applications that support a
	// thinking budget.
	ThinkingBudgetTokens *uint64
	// DisableParallelToolUse lets the caller control parallel tool execution.
	DisableParallelToolUse *bool
}

// ConversationRequest is a conversation with inference parameters and output
// parsing options.
//
// Protocol adapters produce this representation. The caller renders the
// conversation, tokenizes the prompt, and constructs the inference request.
// Protocol-specific metadata is available on the protocol request before
// conversion.
type ConversationRequest struct {
	// Conversation is the converted conversation.
	Conversation *core.Conversation
	// InferenceOptions holds the extracted sampling parameters.
	InferenceOptions InferenceOptions
	// ParsingOptions configures output parsing for this request.
	ParsingOptions stream.ParsingOptions
	// Model is the original model name, when exposed by the adapter. The caller
	// resolves it.
	Model *string
	// Stream reports whether the protocol request selected streaming transport.
	Stream bool
	// NewChunkGenerator builds the protocol event generator for this request.
	// Protocol adapters set it during conversion.
	NewChunkGenerator func(id, model string) stream.Generator
}

// NewConversationRequest constructs a request with default inference, parsing,
// and transport options.
func NewConversationRequest(conversation *core.Conversation) *ConversationRequest {
	return &ConversationRequest{
		Conversation:   conversation,
		ParsingOptions: stream.DefaultParsingOptions(),
		Stream:         false,
	}
}

// NewChunkGeneratorOrNil returns the request's generator factory, or nil when
// the request was not produced by a protocol adapter.
func (r *ConversationRequest) NewChunkGeneratorOrNil() func(id, model string) stream.Generator {
	return r.NewChunkGenerator
}

// ProtocolRequest converts protocol input into a shared conversation request.
type ProtocolRequest interface {
	// Convert validates and converts the protocol's supported request fields.
	//
	// Extract any protocol-specific metadata needed by the application first.
	// It returns a bad-request error for invalid or unsupported input, or an
	// internal error when a conversion step fails.
	Convert(options ConversionOptions) (*ConversationRequest, error)
}

// ConversionErrorKind identifies the class of a conversion failure.
type ConversionErrorKind uint8

const (
	// ConversionBadRequest means the input is invalid or uses an unsupported
	// feature.
	ConversionBadRequest ConversionErrorKind = iota
	// ConversionInternal means an internal conversion operation failed.
	ConversionInternal
)

// ConversionError is a failure while converting protocol input into a
// conversation request.
type ConversionError struct {
	// Kind is the error class.
	Kind ConversionErrorKind
	// Detail is the human-readable message.
	Detail string
}

// BadRequest builds a bad-request conversion error.
func BadRequest(detail string) *ConversionError {
	return &ConversionError{Kind: ConversionBadRequest, Detail: detail}
}

// BadRequestf builds a formatted bad-request conversion error.
func BadRequestf(format string, args ...any) *ConversionError {
	return BadRequest(fmt.Sprintf(format, args...))
}

// Internal builds an internal conversion error.
func Internal(detail string) *ConversionError {
	return &ConversionError{Kind: ConversionInternal, Detail: detail}
}

// Internalf builds a formatted internal conversion error.
func Internalf(format string, args ...any) *ConversionError {
	return Internal(fmt.Sprintf(format, args...))
}

func (e *ConversionError) Error() string { return e.Detail }

// StatusCode returns the HTTP status code for this error.
func (e *ConversionError) StatusCode() int {
	if e.Kind == ConversionInternal {
		return 500
	}
	return 400
}
