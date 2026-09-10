package chatcompletion

import (
	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

const chatCompletionObject = "chat.completion"

// chatCompletionChunkObject is the object name of a streamed chunk.
const chatCompletionChunkObject = "chat.completion.chunk"

// ChatCompletionResponse is a complete response, also usable as an accumulator
// for generated chunks. Chunks are accumulated in inference order. Tool
// argument deltas extend the tool call identified by their index.
type ChatCompletionResponse struct {
	ID                string                 `json:"id"`
	Object            string                 `json:"object"`
	Created           uint64                 `json:"created"`
	Model             string                 `json:"model"`
	SystemFingerprint *string                `json:"system_fingerprint"`
	Choices           []ChatCompletionChoice `json:"choices"`
	Usage             *ChatCompletionUsage   `json:"usage,omitempty"`
}

// ChatCompletionChoice is one complete response choice.
type ChatCompletionChoice struct {
	Index        uint32                        `json:"index"`
	Message      ChatCompletionResponseMessage `json:"message"`
	Logprobs     *ChatCompletionLogprobs       `json:"logprobs"`
	FinishReason *ChatCompletionFinishReason   `json:"finish_reason"`
}

// ChatCompletionChunk is one streamed response chunk.
type ChatCompletionChunk struct {
	ID                string                               `json:"id"`
	Object            string                               `json:"object"`
	Created           uint64                               `json:"created"`
	Model             string                               `json:"model"`
	SystemFingerprint *string                              `json:"system_fingerprint"`
	Choices           []ChatCompletionChunkChoice          `json:"choices"`
	Usage             *jsonx.Optional[ChatCompletionUsage] `json:"usage,omitempty"`
}

// EventName returns the SSE event name. Chat Completions chunks are unnamed
// data events.
func (c ChatCompletionChunk) EventName() string { return "" }

// ChatCompletionChunkChoice is one streamed choice.
type ChatCompletionChunkChoice struct {
	Index        uint32                      `json:"index"`
	Delta        ChatCompletionMessageDelta  `json:"delta"`
	Logprobs     *ChatCompletionLogprobs     `json:"logprobs"`
	FinishReason *ChatCompletionFinishReason `json:"finish_reason"`
}

// ChatCompletionResponseMessage is a complete assistant message accumulated
// from response deltas.
type ChatCompletionResponseMessage struct {
	Role             ChatCompletionRole               `json:"role"`
	Content          *string                          `json:"content"`
	ReasoningContent *string                          `json:"reasoning_content,omitempty"`
	ToolCalls        []ChatCompletionResponseToolCall `json:"tool_calls,omitempty"`
}

// ChatCompletionMessageDelta is an incremental update to an assistant message.
type ChatCompletionMessageDelta struct {
	Role *ChatCompletionRole `json:"role,omitempty"`
	// Content is nil when the field is omitted, an Optional without a value for
	// an explicit null, and an Optional with a value for a string.
	Content          *jsonx.Optional[string]       `json:"content,omitempty"`
	ReasoningContent *jsonx.Optional[string]       `json:"reasoning_content,omitempty"`
	ToolCalls        []ChatCompletionToolCallDelta `json:"tool_calls,omitempty"`
}

// ChatCompletionRole is the assistant role name.
type ChatCompletionRole string

// ChatCompletionRoleAssistant is the assistant role.
const ChatCompletionRoleAssistant ChatCompletionRole = "assistant"

// ChatCompletionResponseToolCall is a complete tool call in an assistant
// response message.
type ChatCompletionResponseToolCall struct {
	ID       string                             `json:"id"`
	Type     ChatCompletionToolCallType         `json:"type"`
	Function ChatCompletionResponseFunctionCall `json:"function"`
}

// ChatCompletionResponseFunctionCall is the called function.
type ChatCompletionResponseFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionToolCallDelta is an initial tool call or an update to its
// arguments.
type ChatCompletionToolCallDelta interface {
	isChatCompletionToolCallDelta()
}

// ChatCompletionToolCallDeltaStart is the initial tool call.
type ChatCompletionToolCallDeltaStart struct {
	// Index identifies the tool call within the response.
	Index uint32
	// Call is the initial tool call.
	Call ChatCompletionResponseToolCall
}

// ChatCompletionToolCallDeltaArguments extends the arguments of a tool call.
type ChatCompletionToolCallDeltaArguments struct {
	// Index identifies the tool call within the response.
	Index uint32
	// Function carries the arguments fragment.
	Function ChatCompletionFunctionArgumentsDelta
}

func (ChatCompletionToolCallDeltaStart) isChatCompletionToolCallDelta()     {}
func (ChatCompletionToolCallDeltaArguments) isChatCompletionToolCallDelta() {}

// MarshalJSON encodes the start delta with the flattened tool call fields.
func (d ChatCompletionToolCallDeltaStart) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Index    uint32                             `json:"index"`
		ID       string                             `json:"id"`
		Type     ChatCompletionToolCallType         `json:"type"`
		Function ChatCompletionResponseFunctionCall `json:"function"`
	}{d.Index, d.Call.ID, d.Call.Type, d.Call.Function})
}

// MarshalJSON encodes the arguments delta.
func (d ChatCompletionToolCallDeltaArguments) MarshalJSON() ([]byte, error) {
	return jsonx.MarshalCompact(struct {
		Index    uint32                               `json:"index"`
		Function ChatCompletionFunctionArgumentsDelta `json:"function"`
	}{d.Index, d.Function})
}

// ChatCompletionToolCallType is the tool call type.
type ChatCompletionToolCallType string

// ChatCompletionToolCallTypeFunction is the function tool call type.
const ChatCompletionToolCallTypeFunction ChatCompletionToolCallType = "function"

// ChatCompletionFunctionArgumentsDelta carries an arguments fragment.
type ChatCompletionFunctionArgumentsDelta struct {
	Arguments string `json:"arguments"`
}

// ChatCompletionFinishReason is the finish reason reported to clients.
type ChatCompletionFinishReason string

// Finish reasons reported to clients.
const (
	ChatCompletionFinishStop                       ChatCompletionFinishReason = "stop"
	ChatCompletionFinishLength                     ChatCompletionFinishReason = "length"
	ChatCompletionFinishContentFilter              ChatCompletionFinishReason = "content_filter"
	ChatCompletionFinishInsufficientSystemResource ChatCompletionFinishReason = "insufficient_system_resource"
	ChatCompletionFinishToolCalls                  ChatCompletionFinishReason = "tool_calls"
	ChatCompletionFinishAborted                    ChatCompletionFinishReason = "aborted"
)

// FinishReasonFrom maps a shared finish reason to the protocol value.
func FinishReasonFrom(reason stream.FinishReason) ChatCompletionFinishReason {
	switch reason {
	case stream.FinishToolCalls:
		return ChatCompletionFinishToolCalls
	case stream.FinishContentFilter:
		return ChatCompletionFinishContentFilter
	case stream.FinishLength:
		return ChatCompletionFinishLength
	default:
		return ChatCompletionFinishStop
	}
}

// ChatCompletionLogprobs holds log probabilities. They are not supplied by the
// current inference contract.
type ChatCompletionLogprobs struct {
	Content          []ChatCompletionTokenLogprob `json:"content,omitempty"`
	ReasoningContent []ChatCompletionTokenLogprob `json:"reasoning_content,omitempty"`
}

// ChatCompletionTokenLogprob is one token's log probability.
type ChatCompletionTokenLogprob struct {
	Token       string                     `json:"token"`
	Logprob     float32                    `json:"logprob"`
	Bytes       []byte                     `json:"bytes"`
	TopLogprobs []ChatCompletionTopLogprob `json:"top_logprobs"`
}

// ChatCompletionTopLogprob is one alternative token's log probability.
type ChatCompletionTopLogprob struct {
	Token   string  `json:"token"`
	Logprob float32 `json:"logprob"`
	Bytes   []byte  `json:"bytes"`
}

// ChatCompletionUsage reports token usage.
type ChatCompletionUsage struct {
	PromptTokens            int                             `json:"prompt_tokens"`
	CompletionTokens        int                             `json:"completion_tokens"`
	TotalTokens             int                             `json:"total_tokens"`
	PromptTokensDetails     ChatCompletionInputTokenUsage   `json:"prompt_tokens_details"`
	CompletionTokensDetails *ChatCompletionOutputTokenUsage `json:"completion_tokens_details,omitempty"`
	PromptCacheHitTokens    int                             `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens   int                             `json:"prompt_cache_miss_tokens"`
}

// ChatCompletionInputTokenUsage reports cached prompt tokens.
type ChatCompletionInputTokenUsage struct {
	CachedTokens int `json:"cached_tokens"`
}

// ChatCompletionOutputTokenUsage reports reasoning tokens.
type ChatCompletionOutputTokenUsage struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// NewChatCompletionUsage builds usage from prompt and completion counts.
func NewChatCompletionUsage(prompt stream.PromptUsage, completion stream.CompletionUsage) ChatCompletionUsage {
	miss := prompt.PromptTokens - prompt.PromptCacheHitTokens
	if miss < 0 {
		miss = 0
	}
	return ChatCompletionUsage{
		PromptTokens:          prompt.PromptTokens,
		CompletionTokens:      completion.CompletionTokens,
		TotalTokens:           prompt.PromptTokens + completion.CompletionTokens,
		PromptTokensDetails:   ChatCompletionInputTokenUsage{CachedTokens: prompt.PromptCacheHitTokens},
		PromptCacheHitTokens:  prompt.PromptCacheHitTokens,
		PromptCacheMissTokens: miss,
	}
}

// NewChatCompletionResponse constructs a response with caller-supplied
// identity.
func NewChatCompletionResponse(id, model string, created uint64) *ChatCompletionResponse {
	return &ChatCompletionResponse{
		ID:      id,
		Object:  chatCompletionObject,
		Created: created,
		Model:   model,
		Choices: []ChatCompletionChoice{},
	}
}

// DoneMessage returns the final transport sentinel of the protocol.
func (r *ChatCompletionResponse) DoneMessage() (string, bool) { return "[DONE]", true }

// Append accumulates one streamed chunk.
func (r *ChatCompletionResponse) Append(event stream.Event) {
	chunk, ok := event.(ChatCompletionChunk)
	if !ok {
		return
	}
	if chunk.Usage != nil && chunk.Usage.Set {
		usage := chunk.Usage.Value
		r.Usage = &usage
	}
	if r.SystemFingerprint == nil {
		r.SystemFingerprint = chunk.SystemFingerprint
	}
	for _, chunkChoice := range chunk.Choices {
		var choice *ChatCompletionChoice
		for i := range r.Choices {
			if r.Choices[i].Index == chunkChoice.Index {
				choice = &r.Choices[i]
				break
			}
		}
		if choice == nil {
			r.Choices = append(r.Choices, ChatCompletionChoice{
				Index: chunkChoice.Index,
				Message: ChatCompletionResponseMessage{
					Role: ChatCompletionRoleAssistant,
				},
			})
			choice = &r.Choices[len(r.Choices)-1]
		}
		choice.Message.append(chunkChoice.Delta)
		if chunkChoice.Logprobs != nil {
			if choice.Logprobs == nil {
				choice.Logprobs = &ChatCompletionLogprobs{}
			}
			choice.Logprobs.Content = append(choice.Logprobs.Content, chunkChoice.Logprobs.Content...)
			choice.Logprobs.ReasoningContent = append(choice.Logprobs.ReasoningContent, chunkChoice.Logprobs.ReasoningContent...)
		}
		if chunkChoice.FinishReason != nil {
			choice.FinishReason = chunkChoice.FinishReason
		}
	}
}

// append accumulates a message delta.
func (m *ChatCompletionResponseMessage) append(delta ChatCompletionMessageDelta) {
	if delta.Role != nil {
		m.Role = *delta.Role
	}
	if delta.Content != nil && delta.Content.Set {
		value := delta.Content.Value
		if m.Content == nil {
			m.Content = &value
		} else {
			*m.Content += value
		}
	}
	if delta.ReasoningContent != nil && delta.ReasoningContent.Set {
		value := delta.ReasoningContent.Value
		if m.ReasoningContent == nil {
			m.ReasoningContent = &value
		} else {
			*m.ReasoningContent += value
		}
	}
	for _, toolDelta := range delta.ToolCalls {
		switch value := toolDelta.(type) {
		case ChatCompletionToolCallDeltaStart:
			m.ToolCalls = append(m.ToolCalls, value.Call)
		case ChatCompletionToolCallDeltaArguments:
			if int(value.Index) < len(m.ToolCalls) {
				m.ToolCalls[value.Index].Function.Arguments += value.Function.Arguments
			}
		}
	}
}
