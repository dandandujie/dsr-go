package stream

// Event is one protocol response event.
type Event interface {
	// EventName returns the SSE event name, or "" for an unnamed data event.
	EventName() string
}

// Generator converts parsed output into a protocol's response events.
type Generator interface {
	// Generate produces zero or more events for one parsed output chunk.
	Generate(chunk OutputChunk) []Event
}

// OutputChunk is one parsed piece of model output, before protocol-specific
// event conversion.
type OutputChunk interface {
	isOutputChunk()
}

// StartChunk reports the beginning of model output.
type StartChunk struct {
	// SystemFingerprint is the backend fingerprint, when reported.
	SystemFingerprint *string
	// Usage is the prompt usage reported before output starts.
	Usage PromptUsage
}

// RawChunk is ordinary answer content.
type RawChunk struct {
	// Content is the answer text.
	Content string
}

// ReasoningChunk is reasoning content.
type ReasoningChunk struct {
	// Content is the reasoning text.
	Content string
}

// ToolCallBeginChunk marks the beginning of a tool call.
type ToolCallBeginChunk struct{}

// ToolCallChunk starts one tool call with its name.
type ToolCallChunk struct {
	// ToolName is the called function name.
	ToolName string
	// Arguments is the initial arguments JSON, usually empty.
	Arguments string
}

// ToolArgumentsDeltaChunk appends to the arguments of the current tool call.
type ToolArgumentsDeltaChunk struct {
	// Content is the JSON fragment.
	Content string
}

// FinishChunk reports the end of model output.
type FinishChunk struct {
	// Reason is the resolved finish reason.
	Reason FinishReason
	// StopSequence is the matched stop sequence, when one matched.
	StopSequence *string
	// Usage is the accumulated completion usage.
	Usage CompletionUsage
}

func (StartChunk) isOutputChunk()              {}
func (RawChunk) isOutputChunk()                {}
func (ReasoningChunk) isOutputChunk()          {}
func (ToolCallBeginChunk) isOutputChunk()      {}
func (ToolCallChunk) isOutputChunk()           {}
func (ToolArgumentsDeltaChunk) isOutputChunk() {}
func (FinishChunk) isOutputChunk()             {}

// StreamError is an error that ends stream processing before normal completion.
type StreamError struct {
	// MissingTokenizer is true when a token-id chunk arrived without an
	// attached tokenizer.
	MissingTokenizer bool
	// DecodeDetail carries the tokenizer's failure message.
	DecodeDetail string
}

func (e *StreamError) Error() string {
	if e.MissingTokenizer {
		return "token chunk received without a tokenizer"
	}
	return "failed to decode token ids: " + e.DecodeDetail
}

// ErrMissingTokenizer is returned for a token chunk without a tokenizer.
var ErrMissingTokenizer = &StreamError{MissingTokenizer: true}
