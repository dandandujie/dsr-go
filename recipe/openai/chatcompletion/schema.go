// Package chatcompletion implements the OpenAI Chat Completions protocol
// adapter.
//
// A ChatCompletionRequest accepts messages, sampling parameters, client tools,
// and JSON object output. An explicit thinking setting overrides
// reasoning_effort, which overrides the caller's thinking default. Sampling
// values are validated and passed through to the caller.
//
// Text, images, and historical tool calls are supported. A tool result must
// follow the assistant message that requested it, and tool names must be
// unique. Named or required tool choices require thinking to be disabled when
// tools are present. JSON Schema output, logprobs, and top_logprobs are
// accepted and ignored. Function-tool strict flags are passed through for the
// caller to enforce.
package chatcompletion

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dandandujie/dsr-go/core"
)

// ChatCompletionRequest is a Chat Completions request. Unknown fields are
// ignored.
//
// Model resolution and inference execution belong to the caller. Client tools,
// historical tool calls, and image blocks are supported. Server tools are
// rejected by this adapter.
type ChatCompletionRequest struct {
	Messages []ChatCompletionRequestMessage `json:"messages"`
	Model    string                         `json:"model"`

	// FrequencyPenalty is validated in [-2, 2], then omitted from the converted
	// request. Extract before conversion if the backend supports it.
	FrequencyPenalty *float32 `json:"frequency_penalty"`
	// MaxTokens limits the generated tokens.
	MaxTokens *uint32 `json:"max_tokens"`
	// N must be 1; only one completion per request is supported.
	N *uint8 `json:"n"`
	// PresencePenalty is validated in [-2, 2], then omitted from the converted
	// request.
	PresencePenalty *float32 `json:"presence_penalty"`
	// Seed is validated in [0, 2^63), then omitted from the converted request.
	Seed *uint64 `json:"seed"`
	// Stop holds one stop sequence or a list of stop sequences.
	Stop *ChatCompletionStopSequences `json:"stop"`
	// Stream selects streaming transport.
	Stream *bool `json:"stream"`
	// StreamOptions configures streaming transport.
	StreamOptions *ChatCompletionStreamOptions `json:"stream_options"`
	// Temperature is the sampling temperature.
	Temperature *float32 `json:"temperature"`
	// TopP is the nucleus sampling probability.
	TopP *float32 `json:"top_p"`
	// ResponseFormat selects text or JSON object output.
	ResponseFormat *ChatCompletionResponseFormat `json:"response_format"`
	// Tools lists the available client tools.
	Tools []ChatCompletionTool `json:"tools"`
	// ToolChoice selects the tool strategy. With tools present, required and
	// named choices require thinking off.
	ToolChoice *ChatCompletionToolChoiceOption `json:"tool_choice"`
	// ReasoningEffort maps to the shared reasoning levels.
	ReasoningEffort *ChatCompletionReasoningEffort `json:"reasoning_effort"`
	// Thinking overrides reasoning effort and the conversion default.
	Thinking *ChatCompletionThinking `json:"thinking"`
	// ParallelToolCalls is deserialized for the caller; conversion leaves this
	// setting unused.
	ParallelToolCalls *bool `json:"parallel_tool_calls"`
}

// UnmarshalJSON reads a request and enforces the required fields.
func (r *ChatCompletionRequest) UnmarshalJSON(data []byte) error {
	type plain ChatCompletionRequest
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if _, ok := probe["messages"]; !ok {
		return errors.New("missing field `messages`")
	}
	if _, ok := probe["model"]; !ok {
		return errors.New("missing field `model`")
	}
	return json.Unmarshal(data, (*plain)(r))
}

// ChatCompletionRequestMessage is one input message. Role selects the variant.
type ChatCompletionRequestMessage struct {
	// Role is one of "system", "user", "assistant", "tool", or
	// "latest_reminder".
	Role string
	// Content is the message content. It is required for system, user, and tool
	// messages and optional for assistant messages.
	Content *ChatCompletionRequestContent
	// ReasoningContent is the reasoning history of an assistant message.
	ReasoningContent *string
	// ToolCalls lists the tool calls of an assistant message.
	ToolCalls []ChatCompletionRequestToolCall
	// ToolCallID identifies the call a tool message answers.
	ToolCallID string
}

// Message role names.
const (
	RoleSystem         = "system"
	RoleUser           = "user"
	RoleAssistant      = "assistant"
	RoleTool           = "tool"
	RoleLatestReminder = "latest_reminder"
)

// UnmarshalJSON decodes a message tagged by its role.
func (m *ChatCompletionRequestMessage) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role             string                          `json:"role"`
		Content          json.RawMessage                 `json:"content"`
		ReasoningContent *string                         `json:"reasoning_content"`
		ToolCalls        []ChatCompletionRequestToolCall `json:"tool_calls"`
		ToolCallID       *string                         `json:"tool_call_id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role = raw.Role
	m.ReasoningContent = raw.ReasoningContent
	m.ToolCalls = raw.ToolCalls
	switch raw.Role {
	case RoleSystem, RoleUser, RoleLatestReminder:
		if raw.Content == nil {
			return errors.New("missing field `content`")
		}
	case RoleTool:
		if raw.Content == nil {
			return errors.New("missing field `content`")
		}
		if raw.ToolCallID == nil {
			return errors.New("missing field `tool_call_id`")
		}
		m.ToolCallID = *raw.ToolCallID
	case RoleAssistant:
	default:
		return fmt.Errorf("unknown variant `%s`, expected one of `system`, `user`, `assistant`, `tool`, `latest_reminder`", raw.Role)
	}
	if raw.Content != nil {
		var content ChatCompletionRequestContent
		if err := content.UnmarshalJSON(raw.Content); err != nil {
			return fmt.Errorf("invalid content: %w", err)
		}
		m.Content = &content
	}
	if raw.Role == RoleLatestReminder {
		if m.Content == nil || !m.Content.IsString {
			return errors.New("invalid content: expected a string")
		}
	}
	return nil
}

// ChatCompletionRequestToolCall is one historical tool call.
type ChatCompletionRequestToolCall struct {
	ID       string                            `json:"id"`
	Function ChatCompletionRequestFunctionCall `json:"function"`
}

// ChatCompletionRequestFunctionCall is the function of a historical tool call.
type ChatCompletionRequestFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionRequestContent is either plain text or a list of content
// blocks.
type ChatCompletionRequestContent struct {
	// IsString reports whether the JSON value was a string.
	IsString bool
	// Text holds the string form.
	Text string
	// Blocks holds the list form.
	Blocks []ChatCompletionRequestContentBlock
}

// UnmarshalJSON decodes the untagged string or list form.
func (c *ChatCompletionRequestContent) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		c.IsString = true
		c.Text = text
		c.Blocks = nil
		return nil
	}
	var blocks []ChatCompletionRequestContentBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		return err
	}
	c.IsString = false
	c.Text = ""
	c.Blocks = blocks
	return nil
}

// ChatCompletionImageURL is an image reference.
type ChatCompletionImageURL struct {
	// URL is an HTTP URL or a base64 data URL. A value that does not start with
	// "http" must carry a "base64" body.
	URL string `json:"url"`
	// Detail is the requested detail level.
	Detail *core.ImageDetail
}

// UnmarshalJSON decodes an image reference, including its detail level.
func (i *ChatCompletionImageURL) UnmarshalJSON(data []byte) error {
	var raw struct {
		URL    string  `json:"url"`
		Detail *string `json:"detail"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	i.URL = raw.URL
	i.Detail = nil
	if raw.Detail != nil {
		detail, ok := core.ParseImageDetail(*raw.Detail)
		if !ok {
			return fmt.Errorf("unknown variant `%s`, expected one of `low`, `high`, `original`, `auto`", *raw.Detail)
		}
		i.Detail = &detail
	}
	return nil
}

// Content block type names.
const (
	ContentBlockText     = "text"
	ContentBlockImageURL = "image_url"
	ContentBlockFile     = "file"
)

// ChatCompletionRequestContentBlock is one item of a content list.
type ChatCompletionRequestContentBlock struct {
	// Type is one of "text", "image_url", or "file".
	Type string
	// Text is the text of a text block.
	Text string
	// ImageURL is the image of an image_url block.
	ImageURL *ChatCompletionImageURL
	// FileID, FileData, and Filename belong to a file block. file_id is
	// rejected during conversion; the filename is accepted and ignored.
	FileID   *string
	FileData *string
	Filename *string
}

// UnmarshalJSON decodes a content block tagged by its type.
func (b *ChatCompletionRequestContentBlock) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type     string                  `json:"type"`
		Text     *string                 `json:"text"`
		ImageURL *ChatCompletionImageURL `json:"image_url"`
		FileID   *string                 `json:"file_id"`
		FileData *string                 `json:"file_data"`
		Filename *string                 `json:"filename"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	b.Type = raw.Type
	b.FileID = raw.FileID
	b.FileData = raw.FileData
	b.Filename = raw.Filename
	switch raw.Type {
	case ContentBlockText:
		if raw.Text == nil {
			return errors.New("missing field `text`")
		}
		b.Text = *raw.Text
	case ContentBlockImageURL:
		if raw.ImageURL == nil {
			return errors.New("missing field `image_url`")
		}
		b.ImageURL = raw.ImageURL
	case ContentBlockFile:
	default:
		return fmt.Errorf("unknown variant `%s`, expected one of `text`, `image_url`, `file`", raw.Type)
	}
	return nil
}

// ChatCompletionStopSequences is one stop sequence or a list of stop sequences.
type ChatCompletionStopSequences struct {
	// IsArray reports whether the JSON value was an array.
	IsArray bool
	// Values holds the stop sequences.
	Values []string
}

// UnmarshalJSON decodes the untagged string or array form.
func (s *ChatCompletionStopSequences) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		s.IsArray = false
		s.Values = []string{value}
		return nil
	}
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	s.IsArray = true
	s.Values = values
	if s.Values == nil {
		s.Values = []string{}
	}
	return nil
}

// ChatCompletionStreamOptions configures streaming transport.
type ChatCompletionStreamOptions struct {
	// IncludeUsage includes a null usage field before the final chunk. The final
	// chunk carries usage regardless of this setting.
	IncludeUsage *bool `json:"include_usage"`
}

// ChatCompletionResponseFormat selects the answer format.
type ChatCompletionResponseFormat struct {
	// Type is one of "json_object", "json_schema", "regex", or "text".
	Type string
	// Regex is the pattern of a regex format.
	Regex string
}

// Response format type names.
const (
	ResponseFormatJSONObject = "json_object"
	ResponseFormatJSONSchema = "json_schema"
	ResponseFormatRegex      = "regex"
	ResponseFormatText       = "text"
)

// UnmarshalJSON decodes a response format tagged by its type.
func (f *ChatCompletionResponseFormat) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type  string `json:"type"`
		Regex string `json:"regex"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch raw.Type {
	case ResponseFormatJSONObject, ResponseFormatJSONSchema, ResponseFormatText:
	case ResponseFormatRegex:
		f.Regex = raw.Regex
	default:
		return fmt.Errorf("unknown variant `%s`, expected one of `json_object`, `json_schema`, `regex`, `text`", raw.Type)
	}
	f.Type = raw.Type
	return nil
}

// ChatCompletionTool declares one client tool.
type ChatCompletionTool struct {
	// Type is always "function".
	Type ChatCompletionToolType `json:"type"`
	// Function is the function definition.
	Function ChatCompletionFunctionDefinition `json:"function"`
}

// ChatCompletionToolType is the supported tool type.
type ChatCompletionToolType string

// ChatCompletionToolTypeFunction is the function tool type.
const ChatCompletionToolTypeFunction ChatCompletionToolType = "function"

// ChatCompletionFunctionDefinition describes one function tool.
type ChatCompletionFunctionDefinition struct {
	// Name is the function name.
	Name string `json:"name"`
	// Description is the human-readable purpose of the function.
	Description *string `json:"description"`
	// Parameters is a JSON Schema of type object, kept as raw JSON so that
	// object key order survives. Omission renders an empty schema.
	Parameters json.RawMessage `json:"parameters"`
	// Strict is passed through for the caller to enforce during inference.
	Strict *bool `json:"strict"`
}

// ChatCompletionToolChoiceOption is a tool choice mode or a named tool choice.
type ChatCompletionToolChoiceOption struct {
	// Mode is the string form: "none", "auto", or "required".
	Mode *string
	// Named is the object form.
	Named *ChatCompletionNamedToolChoice
}

// Tool choice mode names.
const (
	ToolChoiceModeNone     = "none"
	ToolChoiceModeAuto     = "auto"
	ToolChoiceModeRequired = "required"
)

// UnmarshalJSON decodes the untagged mode or named form.
func (c *ChatCompletionToolChoiceOption) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var mode string
		if err := json.Unmarshal(data, &mode); err != nil {
			return err
		}
		switch mode {
		case ToolChoiceModeNone, ToolChoiceModeAuto, ToolChoiceModeRequired:
		default:
			return fmt.Errorf("unknown variant `%s`, expected one of `none`, `auto`, `required`", mode)
		}
		c.Mode = &mode
		return nil
	}
	var named ChatCompletionNamedToolChoice
	if err := json.Unmarshal(data, &named); err != nil {
		return err
	}
	c.Named = &named
	return nil
}

// ChatCompletionNamedToolChoice selects one named tool.
type ChatCompletionNamedToolChoice struct {
	// Type is always "function".
	Type ChatCompletionToolType `json:"type"`
	// Function names the selected tool.
	Function ChatCompletionToolChoiceFunction `json:"function"`
}

// ChatCompletionToolChoiceFunction names the selected tool.
type ChatCompletionToolChoiceFunction struct {
	// Name is the tool name.
	Name string `json:"name"`
}

// ChatCompletionThinking is the optional thinking control for DeepSeek
// compatible requests.
type ChatCompletionThinking struct {
	// Type is "enabled" (or its alias "adaptive") or "disabled".
	Type ChatCompletionThinkingType `json:"type"`
	// BudgetTokens is deserialized for the caller; conversion leaves this
	// setting unused.
	BudgetTokens *uint64 `json:"budget_tokens"`
}

// ChatCompletionThinkingType is the thinking control value.
type ChatCompletionThinkingType string

// Thinking control values.
const (
	ThinkingEnabled  ChatCompletionThinkingType = "enabled"
	ThinkingDisabled ChatCompletionThinkingType = "disabled"
)

// UnmarshalJSON decodes a thinking control, accepting the "adaptive" alias.
func (t *ChatCompletionThinkingType) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch value {
	case "enabled", "adaptive":
		*t = ThinkingEnabled
	case "disabled":
		*t = ThinkingDisabled
	default:
		return fmt.Errorf("unknown variant `%s`, expected `enabled` or `disabled`", value)
	}
	return nil
}

// ChatCompletionReasoningEffort is the reasoning effort accepted by this
// adapter. Unless overridden by thinking, "none" disables thinking and other
// values enable it.
type ChatCompletionReasoningEffort string

// Reasoning effort values.
const (
	ReasoningEffortNone    ChatCompletionReasoningEffort = "none"
	ReasoningEffortMinimal ChatCompletionReasoningEffort = "minimal"
	ReasoningEffortLow     ChatCompletionReasoningEffort = "low"
	ReasoningEffortMedium  ChatCompletionReasoningEffort = "medium"
	ReasoningEffortHigh    ChatCompletionReasoningEffort = "high"
	ReasoningEffortXhigh   ChatCompletionReasoningEffort = "xhigh"
	ReasoningEffortMax     ChatCompletionReasoningEffort = "max"
)
