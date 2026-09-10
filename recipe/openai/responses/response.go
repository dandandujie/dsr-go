package responses

import (
	"fmt"
	"strconv"

	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// responsesObject is the object name of a complete response.
const responsesObject = "response"

// ResponsesResponse is a complete Responses response with event accumulation.
//
// Deltas append, done events replace values, and full responses replace state.
// Incremental events with mismatched indices, IDs, or types are ignored.
type ResponsesResponse struct {
	// ID is the response identifier.
	ID string `json:"id"`
	// Object is always "response".
	Object string `json:"object"`
	// CreatedAt is the response creation time in Unix seconds.
	CreatedAt uint64 `json:"created_at"`
	// Model is the public model name.
	Model string `json:"model"`
	// Status is the response state.
	Status ResponsesStatus `json:"status"`
	// CompletedAt is set by the finishing event.
	CompletedAt *uint64 `json:"completed_at"`
	// Error carries a complete-response error. Generated responses leave it
	// null because parsed output chunks carry no error information.
	Error *ResponsesError `json:"error"`
	// IncompleteDetails explains an incomplete response.
	IncompleteDetails *ResponsesIncompleteDetails `json:"incomplete_details"`
	// Output holds the output items in order.
	Output []ResponsesOutputItem `json:"output"`
	// Usage is null until the finishing event. Constructor token counts are
	// ignored.
	Usage *ResponsesUsage `json:"usage"`
}

// MarshalJSON encodes a response, keeping the output list an array.
func (r ResponsesResponse) MarshalJSON() ([]byte, error) {
	type plain ResponsesResponse
	value := plain(r)
	if value.Output == nil {
		value.Output = []ResponsesOutputItem{}
	}
	return jsonx.MarshalCompact(value)
}

// ResponsesStatus is a response or output item state.
type ResponsesStatus string

// Response states.
const (
	ResponsesStatusInProgress ResponsesStatus = "in_progress"
	ResponsesStatusCompleted  ResponsesStatus = "completed"
	ResponsesStatusIncomplete ResponsesStatus = "incomplete"
)

// ResponsesError is an error of a complete response.
type ResponsesError struct {
	// Code is the machine-readable error code.
	Code string `json:"code"`
	// Message is the human-readable error message.
	Message string `json:"message"`
}

// ResponsesIncompleteDetails explains why a response is incomplete.
type ResponsesIncompleteDetails struct {
	// Reason is the incomplete reason.
	Reason ResponsesIncompleteReason `json:"reason"`
}

// ResponsesIncompleteReason is an incomplete response reason.
type ResponsesIncompleteReason string

// Incomplete response reasons.
const (
	ResponsesIncompleteReasonMaxOutputTokens ResponsesIncompleteReason = "max_output_tokens"
	ResponsesIncompleteReasonContentFilter   ResponsesIncompleteReason = "content_filter"
)

// ResponsesOutputItem is one output item of a response. It is implemented by
// ResponsesMessageOutputItem, ResponsesReasoningOutputItem,
// ResponsesFunctionCallOutputItem, and ResponsesCustomToolCallOutputItem.
type ResponsesOutputItem interface {
	// MarshalJSON encodes the internally tagged item.
	MarshalJSON() ([]byte, error)
	// responsesItemID returns the item ID used to match streaming events.
	responsesItemID() string
	// isResponsesOutputItem marks the interface.
	isResponsesOutputItem()
}

// ResponsesMessageOutputItem is an assistant message output item.
//
// Item IDs identify output items in events. The phase is commentary when the
// answer content immediately precedes a tool call and final_answer otherwise.
type ResponsesMessageOutputItem struct {
	// ID is the item identifier.
	ID string
	// Status is the item state.
	Status ResponsesStatus
	// Role is always "assistant".
	Role ResponsesRole
	// Phase is the answer phase.
	Phase ResponsesPhase
	// Content holds the output text parts.
	Content []ResponsesContentPart
}

// responsesItemID returns the item ID.
func (i *ResponsesMessageOutputItem) responsesItemID() string { return i.ID }

// isResponsesOutputItem marks the item variant.
func (i *ResponsesMessageOutputItem) isResponsesOutputItem() {}

// MarshalJSON encodes the item with its type tag.
func (i *ResponsesMessageOutputItem) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("message", struct {
		ID      string                 `json:"id"`
		Status  ResponsesStatus        `json:"status"`
		Role    ResponsesRole          `json:"role"`
		Phase   ResponsesPhase         `json:"phase"`
		Content []ResponsesContentPart `json:"content"`
	}{i.ID, i.Status, i.Role, i.Phase, nonNilParts(i.Content)})
}

// ResponsesReasoningOutputItem is a reasoning output item.
type ResponsesReasoningOutputItem struct {
	// ID is the item identifier.
	ID string
	// Status is the item state.
	Status ResponsesStatus
	// Content holds the reasoning text parts.
	Content []ResponsesContentPart
	// Summary is always an empty array because no summary is produced.
	Summary []jsonx.Value
}

// responsesItemID returns the item ID.
func (i *ResponsesReasoningOutputItem) responsesItemID() string { return i.ID }

// isResponsesOutputItem marks the item variant.
func (i *ResponsesReasoningOutputItem) isResponsesOutputItem() {}

// MarshalJSON encodes the item with its type tag.
func (i *ResponsesReasoningOutputItem) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("reasoning", struct {
		ID      string                 `json:"id"`
		Status  ResponsesStatus        `json:"status"`
		Content []ResponsesContentPart `json:"content"`
		Summary []jsonx.Value          `json:"summary"`
	}{i.ID, i.Status, nonNilParts(i.Content), nonNilValues(i.Summary)})
}

// ResponsesFunctionCallOutputItem is a function call output item.
type ResponsesFunctionCallOutputItem struct {
	// ID is the item identifier.
	ID string
	// Status is the item state.
	Status ResponsesStatus
	// CallID associates later tool results with this call.
	CallID string
	// Name is the function name.
	Name string
	// Namespace is the namespace of the function, when the flattened model name
	// contains one.
	Namespace *string
	// Arguments is the serialized call arguments.
	Arguments string
}

// responsesItemID returns the item ID.
func (i *ResponsesFunctionCallOutputItem) responsesItemID() string { return i.ID }

// isResponsesOutputItem marks the item variant.
func (i *ResponsesFunctionCallOutputItem) isResponsesOutputItem() {}

// MarshalJSON encodes the item with its type tag.
func (i *ResponsesFunctionCallOutputItem) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("function_call", struct {
		ID        string          `json:"id"`
		Status    ResponsesStatus `json:"status"`
		CallID    string          `json:"call_id"`
		Name      string          `json:"name"`
		Namespace *string         `json:"namespace,omitempty"`
		Arguments string          `json:"arguments"`
	}{i.ID, i.Status, i.CallID, i.Name, i.Namespace, i.Arguments})
}

// ResponsesCustomToolCallOutputItem is a custom tool call output item.
type ResponsesCustomToolCallOutputItem struct {
	// ID is the item identifier.
	ID string
	// Status is the item state.
	Status ResponsesStatus
	// CallID associates later tool results with this call.
	CallID string
	// Name is the custom tool name.
	Name string
	// Input is the decoded custom tool input.
	Input string
}

// responsesItemID returns the item ID.
func (i *ResponsesCustomToolCallOutputItem) responsesItemID() string { return i.ID }

// isResponsesOutputItem marks the item variant.
func (i *ResponsesCustomToolCallOutputItem) isResponsesOutputItem() {}

// MarshalJSON encodes the item with its type tag.
func (i *ResponsesCustomToolCallOutputItem) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("custom_tool_call", struct {
		ID     string          `json:"id"`
		Status ResponsesStatus `json:"status"`
		CallID string          `json:"call_id"`
		Name   string          `json:"name"`
		Input  string          `json:"input"`
	}{i.ID, i.Status, i.CallID, i.Name, i.Input})
}

// ResponsesContentPart is one content part produced from parsed model output.
// It is implemented by ResponsesOutputTextPart and ResponsesReasoningTextPart.
type ResponsesContentPart interface {
	// MarshalJSON encodes the internally tagged part.
	MarshalJSON() ([]byte, error)
	// isResponsesContentPart marks the interface.
	isResponsesContentPart()
}

// ResponsesOutputTextPart is an output text content part.
//
// Annotations and log probabilities are emitted as empty arrays.
type ResponsesOutputTextPart struct {
	// Annotations is always an empty array.
	Annotations []jsonx.Value
	// Logprobs is always an empty array; no log probabilities are supplied.
	Logprobs []jsonx.Value
	// Text is the output text.
	Text string
}

// isResponsesContentPart marks the part variant.
func (ResponsesOutputTextPart) isResponsesContentPart() {}

// MarshalJSON encodes the part with its type tag.
func (p ResponsesOutputTextPart) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("output_text", struct {
		Annotations []jsonx.Value `json:"annotations"`
		Logprobs    []jsonx.Value `json:"logprobs"`
		Text        string        `json:"text"`
	}{nonNilValues(p.Annotations), nonNilValues(p.Logprobs), p.Text})
}

// ResponsesReasoningTextPart is a reasoning text content part.
type ResponsesReasoningTextPart struct {
	// Text is the reasoning text.
	Text string
}

// isResponsesContentPart marks the part variant.
func (ResponsesReasoningTextPart) isResponsesContentPart() {}

// MarshalJSON encodes the part with its type tag.
func (p ResponsesReasoningTextPart) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("reasoning_text", struct {
		Text string `json:"text"`
	}{p.Text})
}

// ResponsesRole is the role of a message output item.
type ResponsesRole string

// ResponsesRoleAssistant is the assistant role.
const ResponsesRoleAssistant ResponsesRole = "assistant"

// ResponsesPhase is the phase of a message output item.
type ResponsesPhase string

// Message phases.
const (
	// ResponsesPhaseCommentary is answer content immediately before a tool call.
	ResponsesPhaseCommentary ResponsesPhase = "commentary"
	// ResponsesPhaseFinalAnswer is ordinary answer content.
	ResponsesPhaseFinalAnswer ResponsesPhase = "final_answer"
)

// ResponsesUsage reports backend prompt counts combined with completion counts
// accumulated by the stream processor.
type ResponsesUsage struct {
	// InputTokens is the total number of input tokens.
	InputTokens int `json:"input_tokens"`
	// InputTokensDetails reports cached input tokens.
	InputTokensDetails ResponsesInputTokensDetails `json:"input_tokens_details"`
	// OutputTokens is the total number of output tokens.
	OutputTokens int `json:"output_tokens"`
	// OutputTokensDetails reports reasoning output tokens.
	OutputTokensDetails ResponsesOutputTokensDetails `json:"output_tokens_details"`
	// TotalTokens is the sum of input and output tokens.
	TotalTokens int `json:"total_tokens"`
}

// ResponsesInputTokensDetails reports cached input tokens.
type ResponsesInputTokensDetails struct {
	// CachedTokens is the number of input tokens read from the prompt cache.
	CachedTokens int `json:"cached_tokens"`
}

// ResponsesOutputTokensDetails reports reasoning output tokens.
type ResponsesOutputTokensDetails struct {
	// ReasoningTokens is always zero because the backend reports no reasoning
	// token count.
	ReasoningTokens int `json:"reasoning_tokens"`
}

// NewResponsesUsage builds usage from prompt and completion counts.
func NewResponsesUsage(prompt stream.PromptUsage, completion stream.CompletionUsage) ResponsesUsage {
	return ResponsesUsage{
		InputTokens:        prompt.PromptTokens,
		InputTokensDetails: ResponsesInputTokensDetails{CachedTokens: prompt.PromptCacheHitTokens},
		OutputTokens:       completion.CompletionTokens,
		TotalTokens:        prompt.PromptTokens + completion.CompletionTokens,
	}
}

// NewResponsesResponse constructs a response with caller-supplied identity.
//
// The response starts in progress with no output and no usage. The constructor
// token counts of the reference implementation are ignored.
func NewResponsesResponse(id, model string, created uint64) *ResponsesResponse {
	return &ResponsesResponse{
		ID:        id,
		Object:    responsesObject,
		CreatedAt: created,
		Model:     model,
		Status:    ResponsesStatusInProgress,
		Output:    []ResponsesOutputItem{},
	}
}

// DoneMessage returns the final transport sentinel of the protocol. The
// Responses protocol has none.
func (r *ResponsesResponse) DoneMessage() (string, bool) { return "", false }

// ResponsesCreatedEvent reports that a response was created.
type ResponsesCreatedEvent struct {
	// Response is the response snapshot.
	Response ResponsesResponse
	// SequenceNumber is the event position within the response.
	SequenceNumber int
}

// EventName returns the SSE event name.
func (e ResponsesCreatedEvent) EventName() string { return "response.created" }

// MarshalJSON encodes the event with its type tag.
func (e ResponsesCreatedEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.created", struct {
		Response       ResponsesResponse `json:"response"`
		SequenceNumber int               `json:"sequence_number"`
	}{e.Response, e.SequenceNumber})
}

// ResponsesInProgressEvent reports that a response is in progress.
type ResponsesInProgressEvent struct {
	// Response is the response snapshot.
	Response ResponsesResponse
	// SequenceNumber is the event position within the response.
	SequenceNumber int
}

// EventName returns the SSE event name.
func (e ResponsesInProgressEvent) EventName() string { return "response.in_progress" }

// MarshalJSON encodes the event with its type tag.
func (e ResponsesInProgressEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.in_progress", struct {
		Response       ResponsesResponse `json:"response"`
		SequenceNumber int               `json:"sequence_number"`
	}{e.Response, e.SequenceNumber})
}

// ResponsesOutputItemAddedEvent reports a new output item.
type ResponsesOutputItemAddedEvent struct {
	// Item is the output item without its initial content part.
	Item ResponsesOutputItem
	// OutputIndex is the item position in the response output.
	OutputIndex int
	// SequenceNumber is the event position within the response.
	SequenceNumber int
}

// EventName returns the SSE event name.
func (e ResponsesOutputItemAddedEvent) EventName() string { return "response.output_item.added" }

// MarshalJSON encodes the event with its type tag.
func (e ResponsesOutputItemAddedEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.output_item.added", struct {
		Item           ResponsesOutputItem `json:"item"`
		OutputIndex    int                 `json:"output_index"`
		SequenceNumber int                 `json:"sequence_number"`
	}{e.Item, e.OutputIndex, e.SequenceNumber})
}

// ResponsesContentPartAddedEvent reports a new content part.
type ResponsesContentPartAddedEvent struct {
	// ContentIndex is the part position in the item content.
	ContentIndex int
	// ItemID identifies the output item.
	ItemID string
	// OutputIndex is the item position in the response output.
	OutputIndex int
	// Part is the new content part.
	Part ResponsesContentPart
	// SequenceNumber is the event position within the response.
	SequenceNumber int
}

// EventName returns the SSE event name.
func (e ResponsesContentPartAddedEvent) EventName() string { return "response.content_part.added" }

// MarshalJSON encodes the event with its type tag.
func (e ResponsesContentPartAddedEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.content_part.added", struct {
		ContentIndex   int                  `json:"content_index"`
		ItemID         string               `json:"item_id"`
		OutputIndex    int                  `json:"output_index"`
		Part           ResponsesContentPart `json:"part"`
		SequenceNumber int                  `json:"sequence_number"`
	}{e.ContentIndex, e.ItemID, e.OutputIndex, e.Part, e.SequenceNumber})
}

// ResponsesOutputTextDeltaEvent appends output text.
type ResponsesOutputTextDeltaEvent struct {
	// ContentIndex is the part position in the item content.
	ContentIndex int
	// Delta is the appended text.
	Delta string
	// ItemID identifies the output item.
	ItemID string
	// Logprobs carries no values; the array is always empty.
	Logprobs []jsonx.Value
	// OutputIndex is the item position in the response output.
	OutputIndex int
	// SequenceNumber is the event position within the response.
	SequenceNumber int
}

// EventName returns the SSE event name.
func (e ResponsesOutputTextDeltaEvent) EventName() string { return "response.output_text.delta" }

// MarshalJSON encodes the event with its type tag.
func (e ResponsesOutputTextDeltaEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.output_text.delta", struct {
		ContentIndex   int           `json:"content_index"`
		Delta          string        `json:"delta"`
		ItemID         string        `json:"item_id"`
		Logprobs       []jsonx.Value `json:"logprobs"`
		OutputIndex    int           `json:"output_index"`
		SequenceNumber int           `json:"sequence_number"`
	}{e.ContentIndex, e.Delta, e.ItemID, nonNilValues(e.Logprobs), e.OutputIndex, e.SequenceNumber})
}

// ResponsesOutputTextDoneEvent replaces the accumulated output text.
type ResponsesOutputTextDoneEvent struct {
	// ContentIndex is the part position in the item content.
	ContentIndex int
	// ItemID identifies the output item.
	ItemID string
	// Logprobs carries no values; the array is always empty.
	Logprobs []jsonx.Value
	// OutputIndex is the item position in the response output.
	OutputIndex int
	// SequenceNumber is the event position within the response.
	SequenceNumber int
	// Text is the final output text.
	Text string
}

// EventName returns the SSE event name.
func (e ResponsesOutputTextDoneEvent) EventName() string { return "response.output_text.done" }

// MarshalJSON encodes the event with its type tag.
func (e ResponsesOutputTextDoneEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.output_text.done", struct {
		ContentIndex   int           `json:"content_index"`
		ItemID         string        `json:"item_id"`
		Logprobs       []jsonx.Value `json:"logprobs"`
		OutputIndex    int           `json:"output_index"`
		SequenceNumber int           `json:"sequence_number"`
		Text           string        `json:"text"`
	}{e.ContentIndex, e.ItemID, nonNilValues(e.Logprobs), e.OutputIndex, e.SequenceNumber, e.Text})
}

// ResponsesReasoningTextDeltaEvent appends reasoning text.
type ResponsesReasoningTextDeltaEvent struct {
	// ContentIndex is the part position in the item content.
	ContentIndex int
	// Delta is the appended text.
	Delta string
	// ItemID identifies the output item.
	ItemID string
	// OutputIndex is the item position in the response output.
	OutputIndex int
	// SequenceNumber is the event position within the response.
	SequenceNumber int
}

// EventName returns the SSE event name.
func (e ResponsesReasoningTextDeltaEvent) EventName() string {
	return "response.reasoning_text.delta"
}

// MarshalJSON encodes the event with its type tag.
func (e ResponsesReasoningTextDeltaEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.reasoning_text.delta", struct {
		ContentIndex   int    `json:"content_index"`
		Delta          string `json:"delta"`
		ItemID         string `json:"item_id"`
		OutputIndex    int    `json:"output_index"`
		SequenceNumber int    `json:"sequence_number"`
	}{e.ContentIndex, e.Delta, e.ItemID, e.OutputIndex, e.SequenceNumber})
}

// ResponsesReasoningTextDoneEvent replaces the accumulated reasoning text.
type ResponsesReasoningTextDoneEvent struct {
	// ContentIndex is the part position in the item content.
	ContentIndex int
	// ItemID identifies the output item.
	ItemID string
	// OutputIndex is the item position in the response output.
	OutputIndex int
	// SequenceNumber is the event position within the response.
	SequenceNumber int
	// Text is the final reasoning text.
	Text string
}

// EventName returns the SSE event name.
func (e ResponsesReasoningTextDoneEvent) EventName() string {
	return "response.reasoning_text.done"
}

// MarshalJSON encodes the event with its type tag.
func (e ResponsesReasoningTextDoneEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.reasoning_text.done", struct {
		ContentIndex   int    `json:"content_index"`
		ItemID         string `json:"item_id"`
		OutputIndex    int    `json:"output_index"`
		SequenceNumber int    `json:"sequence_number"`
		Text           string `json:"text"`
	}{e.ContentIndex, e.ItemID, e.OutputIndex, e.SequenceNumber, e.Text})
}

// ResponsesFunctionCallArgumentsDeltaEvent appends function call arguments.
type ResponsesFunctionCallArgumentsDeltaEvent struct {
	// Delta is the appended arguments fragment.
	Delta string
	// ItemID identifies the output item.
	ItemID string
	// OutputIndex is the item position in the response output.
	OutputIndex int
	// SequenceNumber is the event position within the response.
	SequenceNumber int
}

// EventName returns the SSE event name.
func (e ResponsesFunctionCallArgumentsDeltaEvent) EventName() string {
	return "response.function_call_arguments.delta"
}

// MarshalJSON encodes the event with its type tag.
func (e ResponsesFunctionCallArgumentsDeltaEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.function_call_arguments.delta", struct {
		Delta          string `json:"delta"`
		ItemID         string `json:"item_id"`
		OutputIndex    int    `json:"output_index"`
		SequenceNumber int    `json:"sequence_number"`
	}{e.Delta, e.ItemID, e.OutputIndex, e.SequenceNumber})
}

// ResponsesFunctionCallArgumentsDoneEvent replaces the accumulated arguments.
type ResponsesFunctionCallArgumentsDoneEvent struct {
	// Arguments is the final serialized arguments.
	Arguments string
	// ItemID identifies the output item.
	ItemID string
	// OutputIndex is the item position in the response output.
	OutputIndex int
	// SequenceNumber is the event position within the response.
	SequenceNumber int
}

// EventName returns the SSE event name.
func (e ResponsesFunctionCallArgumentsDoneEvent) EventName() string {
	return "response.function_call_arguments.done"
}

// MarshalJSON encodes the event with its type tag.
func (e ResponsesFunctionCallArgumentsDoneEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.function_call_arguments.done", struct {
		Arguments      string `json:"arguments"`
		ItemID         string `json:"item_id"`
		OutputIndex    int    `json:"output_index"`
		SequenceNumber int    `json:"sequence_number"`
	}{e.Arguments, e.ItemID, e.OutputIndex, e.SequenceNumber})
}

// ResponsesCustomToolCallInputDeltaEvent appends custom tool input.
type ResponsesCustomToolCallInputDeltaEvent struct {
	// Delta is the appended input fragment.
	Delta string
	// ItemID identifies the output item.
	ItemID string
	// OutputIndex is the item position in the response output.
	OutputIndex int
	// SequenceNumber is the event position within the response.
	SequenceNumber int
}

// EventName returns the SSE event name.
func (e ResponsesCustomToolCallInputDeltaEvent) EventName() string {
	return "response.custom_tool_call_input.delta"
}

// MarshalJSON encodes the event with its type tag.
func (e ResponsesCustomToolCallInputDeltaEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.custom_tool_call_input.delta", struct {
		Delta          string `json:"delta"`
		ItemID         string `json:"item_id"`
		OutputIndex    int    `json:"output_index"`
		SequenceNumber int    `json:"sequence_number"`
	}{e.Delta, e.ItemID, e.OutputIndex, e.SequenceNumber})
}

// ResponsesCustomToolCallInputDoneEvent replaces the accumulated custom tool
// input.
type ResponsesCustomToolCallInputDoneEvent struct {
	// Input is the final decoded input.
	Input string
	// ItemID identifies the output item.
	ItemID string
	// OutputIndex is the item position in the response output.
	OutputIndex int
	// SequenceNumber is the event position within the response.
	SequenceNumber int
}

// EventName returns the SSE event name.
func (e ResponsesCustomToolCallInputDoneEvent) EventName() string {
	return "response.custom_tool_call_input.done"
}

// MarshalJSON encodes the event with its type tag.
func (e ResponsesCustomToolCallInputDoneEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.custom_tool_call_input.done", struct {
		Input          string `json:"input"`
		ItemID         string `json:"item_id"`
		OutputIndex    int    `json:"output_index"`
		SequenceNumber int    `json:"sequence_number"`
	}{e.Input, e.ItemID, e.OutputIndex, e.SequenceNumber})
}

// ResponsesContentPartDoneEvent replaces a content part.
type ResponsesContentPartDoneEvent struct {
	// ContentIndex is the part position in the item content.
	ContentIndex int
	// ItemID identifies the output item.
	ItemID string
	// OutputIndex is the item position in the response output.
	OutputIndex int
	// Part is the final content part.
	Part ResponsesContentPart
	// SequenceNumber is the event position within the response.
	SequenceNumber int
}

// EventName returns the SSE event name.
func (e ResponsesContentPartDoneEvent) EventName() string { return "response.content_part.done" }

// MarshalJSON encodes the event with its type tag.
func (e ResponsesContentPartDoneEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.content_part.done", struct {
		ContentIndex   int                  `json:"content_index"`
		ItemID         string               `json:"item_id"`
		OutputIndex    int                  `json:"output_index"`
		Part           ResponsesContentPart `json:"part"`
		SequenceNumber int                  `json:"sequence_number"`
	}{e.ContentIndex, e.ItemID, e.OutputIndex, e.Part, e.SequenceNumber})
}

// ResponsesOutputItemDoneEvent replaces an output item.
type ResponsesOutputItemDoneEvent struct {
	// Item is the final output item.
	Item ResponsesOutputItem
	// OutputIndex is the item position in the response output.
	OutputIndex int
	// SequenceNumber is the event position within the response.
	SequenceNumber int
}

// EventName returns the SSE event name.
func (e ResponsesOutputItemDoneEvent) EventName() string { return "response.output_item.done" }

// MarshalJSON encodes the event with its type tag.
func (e ResponsesOutputItemDoneEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.output_item.done", struct {
		Item           ResponsesOutputItem `json:"item"`
		OutputIndex    int                 `json:"output_index"`
		SequenceNumber int                 `json:"sequence_number"`
	}{e.Item, e.OutputIndex, e.SequenceNumber})
}

// ResponsesCompletedEvent reports a completed response.
type ResponsesCompletedEvent struct {
	// Response is the complete response.
	Response ResponsesResponse
	// SequenceNumber is the event position within the response.
	SequenceNumber int
}

// EventName returns the SSE event name.
func (e ResponsesCompletedEvent) EventName() string { return "response.completed" }

// MarshalJSON encodes the event with its type tag.
func (e ResponsesCompletedEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.completed", struct {
		Response       ResponsesResponse `json:"response"`
		SequenceNumber int               `json:"sequence_number"`
	}{e.Response, e.SequenceNumber})
}

// ResponsesIncompleteEvent reports an incomplete response.
type ResponsesIncompleteEvent struct {
	// Response is the complete response with its incomplete details.
	Response ResponsesResponse
	// SequenceNumber is the event position within the response.
	SequenceNumber int
}

// EventName returns the SSE event name.
func (e ResponsesIncompleteEvent) EventName() string { return "response.incomplete" }

// MarshalJSON encodes the event with its type tag.
func (e ResponsesIncompleteEvent) MarshalJSON() ([]byte, error) {
	return marshalTaggedValue("response.incomplete", struct {
		Response       ResponsesResponse `json:"response"`
		SequenceNumber int               `json:"sequence_number"`
	}{e.Response, e.SequenceNumber})
}

// marshalTaggedValue encodes an internally tagged value whose type tag comes
// first, matching serde's representation of internally tagged enums.
func marshalTaggedValue(typeName string, body any) ([]byte, error) {
	encoded, err := jsonx.MarshalCompact(body)
	if err != nil {
		return nil, err
	}
	if len(encoded) < 2 || encoded[0] != '{' || encoded[len(encoded)-1] != '}' {
		return nil, fmt.Errorf("responses: tagged body of %q is not an object", typeName)
	}
	prefix := []byte(`{"type":` + strconv.Quote(typeName) + `,`)
	out := make([]byte, 0, len(prefix)+len(encoded)-1)
	out = append(out, prefix...)
	out = append(out, encoded[1:]...)
	return out, nil
}

// nonNilParts returns an empty part array when the value is nil.
func nonNilParts(parts []ResponsesContentPart) []ResponsesContentPart {
	if parts == nil {
		return []ResponsesContentPart{}
	}
	return parts
}

// nonNilValues returns an empty value array when the value is nil.
func nonNilValues(values []jsonx.Value) []jsonx.Value {
	if values == nil {
		return []jsonx.Value{}
	}
	return values
}
