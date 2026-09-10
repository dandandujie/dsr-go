package messages

import (
	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// MessagesResponseType is the object type of a response.
type MessagesResponseType string

// MessagesResponseTypeMessage is the message object type.
const MessagesResponseTypeMessage MessagesResponseType = "message"

// MessagesResponseRole is the role of a response message.
type MessagesResponseRole string

// MessagesResponseRoleAssistant is the assistant role.
const MessagesResponseRoleAssistant MessagesResponseRole = "assistant"

// MessagesStopReason is the reason a response stopped.
type MessagesStopReason string

// Stop reasons reported to clients.
const (
	// MessagesStopReasonEndTurn means the model finished its turn.
	MessagesStopReasonEndTurn MessagesStopReason = "end_turn"
	// MessagesStopReasonMaxTokens means the output length limit was reached.
	MessagesStopReasonMaxTokens MessagesStopReason = "max_tokens"
	// MessagesStopReasonStopSequence means a stop sequence matched.
	MessagesStopReasonStopSequence MessagesStopReason = "stop_sequence"
	// MessagesStopReasonToolUse means the model produced tool calls.
	MessagesStopReasonToolUse MessagesStopReason = "tool_use"
	// MessagesStopReasonRefusal means output was withheld by a content filter.
	MessagesStopReasonRefusal MessagesStopReason = "refusal"
)

// MessagesStopReasonFrom maps a shared finish reason to the protocol value.
func MessagesStopReasonFrom(reason stream.FinishReason) MessagesStopReason {
	switch reason {
	case stream.FinishLength:
		return MessagesStopReasonMaxTokens
	case stream.FinishStopSequence:
		return MessagesStopReasonStopSequence
	case stream.FinishToolCalls:
		return MessagesStopReasonToolUse
	case stream.FinishContentFilter:
		return MessagesStopReasonRefusal
	default:
		return MessagesStopReasonEndTurn
	}
}

// MessagesServiceTier is the reported service tier.
type MessagesServiceTier string

// MessagesServiceTierStandard is the standard service tier.
const MessagesServiceTierStandard MessagesServiceTier = "standard"

// MessagesUsage reports caller-supplied token accounting.
type MessagesUsage struct {
	// InputTokens counts uncached input tokens.
	InputTokens int `json:"input_tokens"`
	// CacheCreationInputTokens counts tokens written to the prompt cache.
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	// CacheReadInputTokens counts cached input tokens.
	CacheReadInputTokens int `json:"cache_read_input_tokens"`
	// OutputTokens counts generated tokens.
	OutputTokens int `json:"output_tokens"`
	// ServiceTier is the reported service tier.
	ServiceTier MessagesServiceTier `json:"service_tier"`
}

// NewMessagesUsage builds usage from prompt and completion counts.
func NewMessagesUsage(prompt stream.PromptUsage, completion stream.CompletionUsage) MessagesUsage {
	inputTokens := prompt.PromptTokens - prompt.PromptCacheHitTokens
	if inputTokens < 0 {
		inputTokens = 0
	}
	return MessagesUsage{
		InputTokens:              inputTokens,
		CacheCreationInputTokens: 0,
		CacheReadInputTokens:     prompt.PromptCacheHitTokens,
		OutputTokens:             completion.CompletionTokens,
		ServiceTier:              MessagesServiceTierStandard,
	}
}

// MessagesStopInfo is the stop reason and matched stop sequence of a response.
type MessagesStopInfo struct {
	// StopReason is the resolved stop reason, when the response finished.
	StopReason *MessagesStopReason `json:"stop_reason"`
	// StopSequence is the matched stop sequence, when one matched.
	StopSequence *string `json:"stop_sequence"`
}

// MessagesResponseContent is one complete content block of a response,
// distinct from the delta applied to it. Blocks are values, so an event never
// aliases accumulated state.
type MessagesResponseContent interface {
	isMessagesResponseContent()
}

// MessagesTextContent is a complete text block.
type MessagesTextContent struct {
	// Text is the answer text.
	Text string
}

func (MessagesTextContent) isMessagesResponseContent() {}

// MarshalJSON encodes the tagged text block.
func (c MessagesTextContent) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{messagesBlockTypeText, c.Text})
}

// MessagesThinkingContent is a complete thinking block.
type MessagesThinkingContent struct {
	// Thinking is the reasoning text.
	Thinking string
	// Signature is the opaque signature reported when the block stops.
	Signature string
}

func (MessagesThinkingContent) isMessagesResponseContent() {}

// MarshalJSON encodes the tagged thinking block.
func (c MessagesThinkingContent) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Type      string `json:"type"`
		Thinking  string `json:"thinking"`
		Signature string `json:"signature"`
	}{messagesBlockTypeThinking, c.Thinking, c.Signature})
}

// MessagesToolUseContent is a complete tool call block.
type MessagesToolUseContent struct {
	// ID identifies the tool call.
	ID string
	// Name is the called tool name.
	Name string
	// Input is the parsed tool input.
	Input jsonx.Value
}

func (MessagesToolUseContent) isMessagesResponseContent() {}

// MarshalJSON encodes the tagged tool call block.
func (c MessagesToolUseContent) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Type  string      `json:"type"`
		ID    string      `json:"id"`
		Name  string      `json:"name"`
		Input jsonx.Value `json:"input"`
	}{messagesBlockTypeToolUse, c.ID, c.Name, c.Input})
}

// MessagesDelta is one incremental content update.
type MessagesDelta interface {
	isMessagesDelta()
}

// MessagesTextDelta extends a text block.
type MessagesTextDelta struct {
	// Text is the appended answer text.
	Text string
}

func (MessagesTextDelta) isMessagesDelta() {}

// MarshalJSON encodes the tagged text delta.
func (d MessagesTextDelta) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{messagesDeltaTypeText, d.Text})
}

// MessagesThinkingDelta extends a thinking block.
type MessagesThinkingDelta struct {
	// Thinking is the appended reasoning text.
	Thinking string
}

func (MessagesThinkingDelta) isMessagesDelta() {}

// MarshalJSON encodes the tagged thinking delta.
func (d MessagesThinkingDelta) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Type     string `json:"type"`
		Thinking string `json:"thinking"`
	}{messagesDeltaTypeThinking, d.Thinking})
}

// MessagesSignatureDelta sets the signature of a thinking block.
type MessagesSignatureDelta struct {
	// Signature is the appended signature data.
	Signature string
}

func (MessagesSignatureDelta) isMessagesDelta() {}

// MarshalJSON encodes the tagged signature delta.
func (d MessagesSignatureDelta) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Type      string `json:"type"`
		Signature string `json:"signature"`
	}{messagesDeltaTypeSignature, d.Signature})
}

// MessagesInputJSONDelta extends the input of a tool call block.
type MessagesInputJSONDelta struct {
	// PartialJSON is the JSON fragment appended to the tool input.
	PartialJSON string
}

func (MessagesInputJSONDelta) isMessagesDelta() {}

// MarshalJSON encodes the tagged input-json delta.
func (d MessagesInputJSONDelta) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Type        string `json:"type"`
		PartialJSON string `json:"partial_json"`
	}{messagesDeltaTypeInputJSON, d.PartialJSON})
}

// Content block and delta type names on the wire.
const (
	messagesBlockTypeText     = "text"
	messagesBlockTypeThinking = "thinking"
	messagesBlockTypeToolUse  = "tool_use"

	messagesDeltaTypeText      = "text_delta"
	messagesDeltaTypeThinking  = "thinking_delta"
	messagesDeltaTypeSignature = "signature_delta"
	messagesDeltaTypeInputJSON = "input_json_delta"
)

// MessagesResponse is a complete Messages response, also usable as an
// accumulator for generated events.
//
// Tool argument fragments are parsed when their content block stops. Invalid or
// truncated JSON produces an empty object. Events with invalid indices or
// incompatible delta types leave the response unchanged.
type MessagesResponse struct {
	// ID is the unique response identifier.
	ID string `json:"id"`
	// Type is always "message".
	Type MessagesResponseType `json:"type"`
	// Role is always "assistant".
	Role MessagesResponseRole `json:"role"`
	// Model is the public response model name.
	Model string `json:"model"`
	// Content holds the complete content blocks.
	Content []MessagesResponseContent `json:"content"`
	// StopReason is the resolved stop reason, when the response finished.
	StopReason *MessagesStopReason `json:"stop_reason"`
	// StopSequence is the matched stop sequence, when one matched.
	StopSequence *string `json:"stop_sequence"`
	// Usage reports token accounting.
	Usage MessagesUsage `json:"usage"`
	// partialToolInputs collects tool argument fragments until their block
	// stops.
	partialToolInputs map[int]string
}

// NewMessagesResponse constructs a response with caller-supplied identity and
// zero usage. The created timestamp is unused by the Messages protocol.
func NewMessagesResponse(id, model string, created uint64) *MessagesResponse {
	return NewMessagesResponseWithUsage(id, model, created, stream.PromptUsage{})
}

// NewMessagesResponseWithUsage constructs a response with caller-supplied
// identity and prompt usage. The created timestamp is unused by the Messages
// protocol.
func NewMessagesResponseWithUsage(id, model string, created uint64, promptUsage stream.PromptUsage) *MessagesResponse {
	return &MessagesResponse{
		ID:                id,
		Type:              MessagesResponseTypeMessage,
		Role:              MessagesResponseRoleAssistant,
		Model:             model,
		Content:           []MessagesResponseContent{},
		Usage:             NewMessagesUsage(promptUsage, stream.CompletionUsage{}),
		partialToolInputs: map[int]string{},
	}
}

// DoneMessage reports that the Messages protocol has no final transport
// sentinel.
func (r *MessagesResponse) DoneMessage() (string, bool) { return "", false }

// Append accumulates one event produced by MessagesChunkGenerator.
func (r *MessagesResponse) Append(event stream.Event) {
	switch value := event.(type) {
	case MessagesMessageStartEvent:
		if value.Message == nil {
			return
		}
		message := *value.Message
		message.partialToolInputs = clonePartialToolInputs(value.Message.partialToolInputs)
		*r = message
	case MessagesContentBlockStartEvent:
		if value.Index == len(r.Content) {
			r.Content = append(r.Content, normalizeContent(value.ContentBlock))
		}
	case MessagesContentBlockDeltaEvent:
		if value.Index < 0 || value.Index >= len(r.Content) {
			return
		}
		r.appendDelta(value.Index, value.Delta)
	case MessagesContentBlockStopEvent:
		fragment, ok := r.partialToolInputs[value.Index]
		delete(r.partialToolInputs, value.Index)
		if !ok || value.Index < 0 || value.Index >= len(r.Content) {
			return
		}
		toolUse, ok := r.Content[value.Index].(MessagesToolUseContent)
		if !ok {
			return
		}
		input, err := jsonx.ParseString(fragment)
		if err != nil {
			input = jsonx.NewObject()
		}
		toolUse.Input = input
		r.Content[value.Index] = toolUse
	case MessagesMessageDeltaEvent:
		r.StopReason = value.Delta.StopReason
		r.StopSequence = value.Delta.StopSequence
		r.Usage = value.Usage
	case MessagesMessageStopEvent, MessagesPingEvent:
	}
}

// appendDelta applies one incremental update to its content block.
func (r *MessagesResponse) appendDelta(index int, delta MessagesDelta) {
	switch value := delta.(type) {
	case MessagesTextDelta:
		if text, ok := r.Content[index].(MessagesTextContent); ok {
			text.Text += value.Text
			r.Content[index] = text
		}
	case MessagesThinkingDelta:
		if thinking, ok := r.Content[index].(MessagesThinkingContent); ok {
			thinking.Thinking += value.Thinking
			r.Content[index] = thinking
		}
	case MessagesSignatureDelta:
		if thinking, ok := r.Content[index].(MessagesThinkingContent); ok {
			thinking.Signature += value.Signature
			r.Content[index] = thinking
		}
	case MessagesInputJSONDelta:
		if _, ok := r.Content[index].(MessagesToolUseContent); ok {
			if r.partialToolInputs == nil {
				r.partialToolInputs = map[int]string{}
			}
			r.partialToolInputs[index] += value.PartialJSON
		}
	}
}

// clonePartialToolInputs copies the in-progress tool input fragments of a
// message start event.
func clonePartialToolInputs(inputs map[int]string) map[int]string {
	cloned := make(map[int]string, len(inputs))
	for index, fragment := range inputs {
		cloned[index] = fragment
	}
	return cloned
}

// normalizeContent copies a content block so the accumulated response never
// shares mutable state with the event that started it.
func normalizeContent(content MessagesResponseContent) MessagesResponseContent {
	switch value := content.(type) {
	case *MessagesTextContent:
		return *value
	case *MessagesThinkingContent:
		return *value
	case *MessagesToolUseContent:
		return *value
	}
	return content
}

// MessagesMessageStartEvent starts a stream with the initial response snapshot.
type MessagesMessageStartEvent struct {
	// Message is the initial response snapshot.
	Message *MessagesResponse
}

// EventName returns the SSE event name.
func (MessagesMessageStartEvent) EventName() string { return "message_start" }

// MarshalJSON encodes the event with its type tag.
func (e MessagesMessageStartEvent) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Type    string            `json:"type"`
		Message *MessagesResponse `json:"message"`
	}{messagesEventMessageStart, e.Message})
}

// MessagesContentBlockStartEvent starts one content block.
type MessagesContentBlockStartEvent struct {
	// Index identifies the content block within the response.
	Index int
	// ContentBlock is the empty block to extend.
	ContentBlock MessagesResponseContent
}

// EventName returns the SSE event name.
func (MessagesContentBlockStartEvent) EventName() string { return "content_block_start" }

// MarshalJSON encodes the event with its type tag.
func (e MessagesContentBlockStartEvent) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Type         string                  `json:"type"`
		Index        int                     `json:"index"`
		ContentBlock MessagesResponseContent `json:"content_block"`
	}{messagesEventContentBlockStart, e.Index, e.ContentBlock})
}

// MessagesContentBlockDeltaEvent extends one content block.
type MessagesContentBlockDeltaEvent struct {
	// Index identifies the content block within the response.
	Index int
	// Delta is the incremental update.
	Delta MessagesDelta
}

// EventName returns the SSE event name.
func (MessagesContentBlockDeltaEvent) EventName() string { return "content_block_delta" }

// MarshalJSON encodes the event with its type tag.
func (e MessagesContentBlockDeltaEvent) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Type  string        `json:"type"`
		Index int           `json:"index"`
		Delta MessagesDelta `json:"delta"`
	}{messagesEventContentBlockDelta, e.Index, e.Delta})
}

// MessagesContentBlockStopEvent stops one content block.
type MessagesContentBlockStopEvent struct {
	// Index identifies the content block within the response.
	Index int
}

// EventName returns the SSE event name.
func (MessagesContentBlockStopEvent) EventName() string { return "content_block_stop" }

// MarshalJSON encodes the event with its type tag.
func (e MessagesContentBlockStopEvent) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Type  string `json:"type"`
		Index int    `json:"index"`
	}{messagesEventContentBlockStop, e.Index})
}

// MessagesMessageDeltaEvent reports the stop reason and final usage.
type MessagesMessageDeltaEvent struct {
	// Delta carries the stop reason and matched stop sequence.
	Delta MessagesStopInfo
	// Usage is the final token accounting.
	Usage MessagesUsage
}

// EventName returns the SSE event name.
func (MessagesMessageDeltaEvent) EventName() string { return "message_delta" }

// MarshalJSON encodes the event with its type tag.
func (e MessagesMessageDeltaEvent) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Type  string           `json:"type"`
		Delta MessagesStopInfo `json:"delta"`
		Usage MessagesUsage    `json:"usage"`
	}{messagesEventMessageDelta, e.Delta, e.Usage})
}

// MessagesMessageStopEvent ends a stream.
type MessagesMessageStopEvent struct{}

// EventName returns the SSE event name.
func (MessagesMessageStopEvent) EventName() string { return "message_stop" }

// MarshalJSON encodes the event with its type tag.
func (MessagesMessageStopEvent) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Type string `json:"type"`
	}{messagesEventMessageStop})
}

// MessagesPingEvent keeps a stream alive.
type MessagesPingEvent struct{}

// EventName returns the SSE event name.
func (MessagesPingEvent) EventName() string { return "ping" }

// MarshalJSON encodes the event with its type tag.
func (MessagesPingEvent) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Type string `json:"type"`
	}{messagesEventPing})
}

// Stream event type names on the wire.
const (
	messagesEventMessageStart      = "message_start"
	messagesEventContentBlockStart = "content_block_start"
	messagesEventContentBlockDelta = "content_block_delta"
	messagesEventContentBlockStop  = "content_block_stop"
	messagesEventMessageDelta      = "message_delta"
	messagesEventMessageStop       = "message_stop"
	messagesEventPing              = "ping"
)
