package stream

// PromptUsage holds the prompt-side token counts supplied by the inference
// backend. Unspecified counts default to zero.
type PromptUsage struct {
	// PromptTokens is the total input tokens, including cache hits.
	PromptTokens int
	// PromptCacheHitTokens is the input tokens read from the prompt cache.
	PromptCacheHitTokens int
}

// CompletionUsage holds the completion-side token counts accumulated by the
// stream processor from text chunk token counts and token IDs that decode into
// text.
type CompletionUsage struct {
	// CompletionTokens counts tokens accounted for by processed chunks,
	// including discarded markup and the entire chunk containing a local stop
	// sequence. Undecoded trailing token IDs and chunks after the stop are
	// excluded.
	CompletionTokens int
}

// InferenceChunkKind identifies one variant of InferenceChunk.
type InferenceChunkKind uint8

const (
	// InferenceReady is initial metadata reported before the model produces
	// output.
	InferenceReady InferenceChunkKind = iota
	// InferenceText is text produced by the model, with the tokens that text
	// accounts for.
	InferenceText
	// InferenceToken is one generated token ID, decoded by the processor's
	// tokenizer.
	InferenceToken
	// InferenceFinish is terminal metadata reported after the model stops.
	InferenceFinish
)

// InferenceChunk is text and metadata received from an inference backend.
type InferenceChunk struct {
	Kind InferenceChunkKind
	// SystemFingerprint is set on an InferenceReady chunk.
	SystemFingerprint *string
	// PromptUsage is set on an InferenceReady chunk.
	PromptUsage PromptUsage
	// Content and ContentTokens are set on an InferenceText chunk.
	Content       string
	ContentTokens int
	// TokenID is set on an InferenceToken chunk.
	TokenID uint32
	// FinishReason is set on an InferenceFinish chunk.
	FinishReason InferenceFinishReason
}

// NewReadyChunk builds an inference ready chunk.
func NewReadyChunk(systemFingerprint *string, usage PromptUsage) InferenceChunk {
	return InferenceChunk{Kind: InferenceReady, SystemFingerprint: systemFingerprint, PromptUsage: usage}
}

// NewTextChunk builds an inference text chunk.
func NewTextChunk(content string, contentTokens int) InferenceChunk {
	return InferenceChunk{Kind: InferenceText, Content: content, ContentTokens: contentTokens}
}

// NewTokenChunk builds a single token ID inference chunk.
func NewTokenChunk(tokenID uint32) InferenceChunk {
	return InferenceChunk{Kind: InferenceToken, TokenID: tokenID}
}

// NewFinishChunk builds an inference finish chunk.
func NewFinishChunk(reason InferenceFinishReason) InferenceChunk {
	return InferenceChunk{Kind: InferenceFinish, FinishReason: reason}
}

// InferenceFinishReason is the finish reason reported by the inference backend.
type InferenceFinishReason uint8

const (
	// InferenceFinishStop means the model stopped on its own.
	InferenceFinishStop InferenceFinishReason = iota
	// InferenceFinishLength means the model hit the output length limit.
	InferenceFinishLength
	// InferenceFinishContentFilter means output was withheld by a content
	// filter.
	InferenceFinishContentFilter
)

// FinishReason is the final result after applying the stream parser's tool and
// stop-sequence rules.
type FinishReason uint8

const (
	// FinishStop is a normal stop.
	FinishStop FinishReason = iota
	// FinishLength is an output length stop.
	FinishLength
	// FinishToolCalls is a stop after the model produced tool calls.
	FinishToolCalls
	// FinishStopSequence is a stop on a caller-supplied stop sequence.
	FinishStopSequence
	// FinishContentFilter is a content filter stop.
	FinishContentFilter
	// FinishEndOfStream means the source ended without an explicit finish
	// reason.
	FinishEndOfStream
)

// String returns the protocol-independent finish reason name.
func (r FinishReason) String() string {
	switch r {
	case FinishLength:
		return "length"
	case FinishToolCalls:
		return "tool_calls"
	case FinishStopSequence:
		return "stop_sequence"
	case FinishContentFilter:
		return "content_filter"
	case FinishEndOfStream:
		return "end_of_stream"
	default:
		return "stop"
	}
}
