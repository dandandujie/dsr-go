package request

// WebSearchBehavior selects how an adapter handles supported web_search
// declarations and tool choices. Messages also applies this setting to
// assistant server-tool content blocks.
type WebSearchBehavior uint8

const (
	// WebSearchIgnore drops matching declarations and choices, plus Messages
	// server-tool blocks.
	WebSearchIgnore WebSearchBehavior = iota
	// WebSearchReject rejects matching declarations and choices, plus Messages
	// server-tool blocks.
	WebSearchReject
)

// ConversionOptions holds the defaults for conversion from protocol requests.
type ConversionOptions struct {
	// DefaultThinkingMode is the thinking mode for requests that do not specify
	// one. It defaults to true.
	DefaultThinkingMode bool
	// ResponsesWebSearch selects the behavior for Responses web_search tool
	// declarations and tool choices. Historical web_search_call input items are
	// ignored under either setting. It defaults to WebSearchIgnore.
	ResponsesWebSearch WebSearchBehavior
	// MessagesWebSearch selects the behavior for the Messages web_search server
	// tool, its named tool choice, and its server_tool_use and
	// web_search_tool_result blocks. It defaults to WebSearchReject.
	MessagesWebSearch WebSearchBehavior
}

// NewConversionOptions returns options that enable thinking for requests that
// do not specify a thinking mode.
func NewConversionOptions() ConversionOptions {
	return ConversionOptions{
		DefaultThinkingMode: true,
		ResponsesWebSearch:  WebSearchIgnore,
		MessagesWebSearch:   WebSearchReject,
	}
}

// WithDefaultThinkingMode sets the default thinking mode.
func (o ConversionOptions) WithDefaultThinkingMode(defaultThinkingMode bool) ConversionOptions {
	o.DefaultThinkingMode = defaultThinkingMode
	return o
}

// WithResponsesWebSearch sets the Responses web_search behavior.
func (o ConversionOptions) WithResponsesWebSearch(behavior WebSearchBehavior) ConversionOptions {
	o.ResponsesWebSearch = behavior
	return o
}

// WithMessagesWebSearch sets the Messages web_search behavior.
func (o ConversionOptions) WithMessagesWebSearch(behavior WebSearchBehavior) ConversionOptions {
	o.MessagesWebSearch = behavior
	return o
}
