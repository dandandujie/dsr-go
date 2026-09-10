package core

// Conversation is a conversation and its prompt configuration.
//
// Sampling parameters and transport options belong to the caller. Thinking is
// enabled by default.
type Conversation struct {
	// Messages holds the messages in conversation order.
	Messages []InputMessage
	// ThinkingMode enables thinking mode for the next assistant turn.
	ThinkingMode bool
	// Tools holds the available client tools in declaration order.
	Tools []ToolDefinition
	// ToolChoice is the tool selection after protocol-specific normalization.
	ToolChoice ToolChoice
	// ReasoningEffort is used when thinking mode is enabled.
	ReasoningEffort *ReasoningEffort
	// ResponseFormat is the requested answer format used during rendering.
	ResponseFormat ResponseFormat
}

// NewConversation returns a conversation with the default prompt settings:
// thinking enabled, automatic tool choice, text output, and no messages.
func NewConversation() *Conversation {
	return &Conversation{
		ThinkingMode:   true,
		ToolChoice:     ToolChoiceAuto,
		ResponseFormat: ResponseFormatText,
	}
}

// ResponseFormat is the output format described in the inference prompt.
type ResponseFormat uint8

const (
	// ResponseFormatText requests ordinary text output.
	ResponseFormatText ResponseFormat = iota
	// ResponseFormatJSONObject requests a JSON object.
	ResponseFormatJSONObject
)

// ReasoningEffort is the protocol-independent reasoning effort.
type ReasoningEffort uint8

const (
	// ReasoningEffortLow is the lowest effort level.
	ReasoningEffortLow ReasoningEffort = iota
	// ReasoningEffortHigh is the default effort level.
	ReasoningEffortHigh
	// ReasoningEffortXhigh is a higher-than-default effort level.
	ReasoningEffortXhigh
	// ReasoningEffortMax is the highest effort level.
	ReasoningEffortMax
)

// String returns the snake_case name used by the protocol schemas.
func (e ReasoningEffort) String() string {
	switch e {
	case ReasoningEffortLow:
		return "low"
	case ReasoningEffortHigh:
		return "high"
	case ReasoningEffortXhigh:
		return "xhigh"
	default:
		return "max"
	}
}

// ParseReasoningEffort resolves a snake_case effort name.
func ParseReasoningEffort(value string) (ReasoningEffort, bool) {
	switch value {
	case "low":
		return ReasoningEffortLow, true
	case "high":
		return ReasoningEffortHigh, true
	case "xhigh":
		return ReasoningEffortXhigh, true
	case "max":
		return ReasoningEffortMax, true
	}
	return ReasoningEffortHigh, false
}
