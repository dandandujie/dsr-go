// Package messages implements the Anthropic Messages protocol adapter.
//
// A MessagesRequest accepts text, images, client tools, and thinking blocks.
// Document content becomes the text "[Unsupported Document]" and hosted tool
// execution is outside the supported scope. Server tools are rejected by
// default; web-search handling is configurable through
// request.ConversionOptions.MessagesWebSearch.
//
// Input text must not spell out the image placeholder. Every placeholder in a
// prompt corresponds to one image source, and the adapter inserts them for
// image blocks only, so message text, tool references, thinking blocks, tool
// result text, the top-level system field, tool definitions, and historical
// tool calls are rejected when they carry the placeholder spelling.
//
// Use MessagesRequest.Convert to produce a shared request.ConversationRequest,
// and its NewChunkGenerator to build Messages streaming events.
// MessagesResponse accumulates those events into a complete response.
//
// The caller supplies model inference, tool execution, token usage, and HTTP
// transport.
package messages

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// MessagesRequest is an Anthropic Messages request. Unknown top-level fields
// are ignored.
//
// Model selection, execution, and usage accounting are the caller's
// responsibility. Images are supported. Document blocks become the text
// "[Unsupported Document]"; their contents are discarded. web_search server
// tools are rejected by default; see
// request.ConversionOptions.MessagesWebSearch.
type MessagesRequest struct {
	// Model is the requested model name.
	Model string `json:"model"`
	// Messages holds the conversation in order. At least one message is
	// required.
	Messages []MessagesMessage `json:"messages"`
	// System is the optional top-level system prompt.
	System *MessagesTextOrTextBlocks `json:"system"`
	// MaxTokens limits the number of generated tokens.
	MaxTokens *uint32 `json:"max_tokens"`
	// Temperature is the sampling temperature.
	Temperature *float32 `json:"temperature"`
	// TopP is the nucleus sampling probability.
	TopP *float32 `json:"top_p"`
	// StopSequences holds at most 16 non-empty stop sequences.
	StopSequences []string `json:"stop_sequences"`
	// Stream selects streaming transport.
	Stream *bool `json:"stream"`
	// Tools lists the available client tools.
	Tools []MessagesTool `json:"tools"`
	// ToolChoice selects the tool strategy.
	ToolChoice *MessagesToolChoice `json:"tool_choice"`
	// Thinking overrides reasoning effort and the conversion default.
	Thinking *MessagesThinking `json:"thinking"`
	// OutputConfig carries the requested reasoning effort.
	OutputConfig *MessagesOutputConfig `json:"output_config"`
	// Metadata is accepted for compatibility with Anthropic clients and ignored
	// during conversion.
	Metadata json.RawMessage `json:"metadata"`
}

// UnmarshalJSON reads a request and enforces the required fields.
func (r *MessagesRequest) UnmarshalJSON(data []byte) error {
	type plain MessagesRequest
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	model, ok := probe["model"]
	if !ok {
		return errors.New("missing field `model`")
	}
	if isJSONNull(model) {
		return errors.New("invalid type: null, expected a string")
	}
	messages, ok := probe["messages"]
	if !ok {
		return errors.New("missing field `messages`")
	}
	if isJSONNull(messages) {
		return errors.New("invalid type: null, expected a sequence")
	}
	return json.Unmarshal(data, (*plain)(r))
}

// Message role names.
const (
	// MessagesRoleUser is the user message role.
	MessagesRoleUser = "user"
	// MessagesRoleAssistant is the assistant message role.
	MessagesRoleAssistant = "assistant"
	// MessagesRoleSystem is the compatibility system message role, rendered as a
	// user system-reminder.
	MessagesRoleSystem = "system"
)

// MessagesMessage is one entry of the messages array. The role selects the
// variant: user and assistant messages carry plain text or content blocks, and
// the compatibility system role carries plain text or text-or-text-blocks
// content rendered as a user system-reminder.
type MessagesMessage struct {
	// Role is one of "user", "assistant", or "system".
	Role string
	// Content is the content of a user or assistant message.
	Content *MessagesContent
	// SystemContent is the content of a compatibility system message. It carries
	// text, tool reference, image, and document blocks.
	SystemContent *MessagesTextOrTextBlocks
}

// UnmarshalJSON decodes a message tagged by its role.
func (m *MessagesMessage) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role    json.RawMessage `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Role == nil {
		return errors.New("missing field `role`")
	}
	if isJSONNull(raw.Role) {
		return errors.New("invalid type: null, expected string or map")
	}
	if err := json.Unmarshal(raw.Role, &m.Role); err != nil {
		return err
	}
	if raw.Content == nil {
		return errors.New("missing field `content`")
	}
	switch m.Role {
	case MessagesRoleUser, MessagesRoleAssistant:
		var content MessagesContent
		if err := content.UnmarshalJSON(raw.Content); err != nil {
			return err
		}
		m.Content = &content
	case MessagesRoleSystem:
		var content MessagesTextOrTextBlocks
		if err := content.UnmarshalJSON(raw.Content); err != nil {
			return err
		}
		m.SystemContent = &content
	default:
		return fmt.Errorf("unknown variant `%s`, expected one of `user`, `assistant`, `system`", m.Role)
	}
	return nil
}

// MessagesContent is message content: either plain text or a list of content
// blocks.
type MessagesContent struct {
	// IsString reports whether the JSON value was a string.
	IsString bool
	// Text holds the string form.
	Text string
	// Blocks holds the list form.
	Blocks []MessagesContentBlock
}

// UnmarshalJSON decodes the untagged string or block-list form.
func (c *MessagesContent) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return err
		}
		c.IsString = true
		c.Text = text
		c.Blocks = nil
		return nil
	}
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var blocks []MessagesContentBlock
		if err := json.Unmarshal(trimmed, &blocks); err != nil {
			// Untagged enums report a single generic failure.
			return errors.New("data did not match any variant of untagged enum MessagesContent")
		}
		c.IsString = false
		c.Text = ""
		c.Blocks = blocks
		return nil
	}
	return errors.New("data did not match any variant of untagged enum MessagesContent")
}

// MessagesTextOrTextBlocks is plain text or a list of text blocks.
type MessagesTextOrTextBlocks struct {
	// IsString reports whether the JSON value was a string.
	IsString bool
	// Text holds the string form.
	Text string
	// Blocks holds the list form.
	Blocks []MessagesTextBlock
}

// UnmarshalJSON decodes the untagged string or block-list form.
func (c *MessagesTextOrTextBlocks) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return err
		}
		c.IsString = true
		c.Text = text
		c.Blocks = nil
		return nil
	}
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var blocks []MessagesTextBlock
		if err := json.Unmarshal(trimmed, &blocks); err != nil {
			// Untagged enums report a single generic failure.
			return errors.New("data did not match any variant of untagged enum MessagesTextOrTextBlocks")
		}
		c.IsString = false
		c.Text = ""
		c.Blocks = blocks
		return nil
	}
	return errors.New("data did not match any variant of untagged enum MessagesTextOrTextBlocks")
}

// Text block type names.
const (
	// MessagesTextBlockText is a text block.
	MessagesTextBlockText = "text"
	// MessagesTextBlockToolReference is a tool reference block.
	MessagesTextBlockToolReference = "tool_reference"
	// MessagesTextBlockImage is an image block.
	MessagesTextBlockImage = "image"
	// MessagesTextBlockDocument is a document block, rendered as an
	// unsupported-document placeholder.
	MessagesTextBlockDocument = "document"
)

// MessagesTextBlock is one entry of a text-or-text-blocks list.
type MessagesTextBlock struct {
	// Type is one of "text", "tool_reference", "image", or "document". Any other
	// value is preserved as the unsupported variant and rejected during
	// conversion.
	Type string
	// Text is the text of a text block.
	Text string
	// ToolName names the referenced tool of a tool_reference block.
	ToolName string
	// Source is the image source of an image block.
	Source *MessagesImageSource
}

// UnmarshalJSON decodes a text block tagged by its type.
func (b *MessagesTextBlock) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type *string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.Type == nil {
		return errors.New("missing field `type`")
	}
	b.Type = *probe.Type
	switch b.Type {
	case MessagesTextBlockText, MessagesTextBlockToolReference, MessagesTextBlockImage, MessagesTextBlockDocument:
	default:
		// Unknown block types are preserved and rejected during conversion.
		return nil
	}
	var raw struct {
		Text     *string         `json:"text"`
		ToolName *string         `json:"tool_name"`
		Source   json.RawMessage `json:"source"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch b.Type {
	case MessagesTextBlockText:
		if raw.Text == nil {
			return errors.New("missing field `text`")
		}
		b.Text = *raw.Text
	case MessagesTextBlockToolReference:
		if raw.ToolName == nil {
			return errors.New("missing field `tool_name`")
		}
		b.ToolName = *raw.ToolName
	case MessagesTextBlockImage:
		if raw.Source == nil {
			return errors.New("missing field `source`")
		}
		var source MessagesImageSource
		if err := source.UnmarshalJSON(raw.Source); err != nil {
			return err
		}
		b.Source = &source
	case MessagesTextBlockDocument:
	}
	return nil
}

// Image source type names.
const (
	// MessagesImageSourceBase64 is an inline base64 image source.
	MessagesImageSourceBase64 = "base64"
	// MessagesImageSourceURL is an external URL image source.
	MessagesImageSourceURL = "url"
	// MessagesImageSourceFile is a file identifier image source. A file
	// identifier requires a file service and is rejected during conversion.
	MessagesImageSourceFile = "file"
)

// MessagesImageSource is one image reference: inline data, a URL, or a file
// identifier.
type MessagesImageSource struct {
	// Type is "base64", "url", or "file".
	Type string
	// MediaType is the declared media type of a base64 source.
	MediaType string
	// Data is the base64 payload of a base64 source.
	Data string
	// URL is the external image URL of a url source.
	URL string
	// FileID identifies the file of a file source.
	FileID string
}

// UnmarshalJSON decodes an image source tagged by its type.
func (s *MessagesImageSource) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type *string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.Type == nil {
		return errors.New("missing field `type`")
	}
	s.Type = *probe.Type
	var raw struct {
		MediaType *string `json:"media_type"`
		Data      *string `json:"data"`
		URL       *string `json:"url"`
		FileID    *string `json:"file_id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch s.Type {
	case MessagesImageSourceBase64:
		if raw.MediaType == nil {
			return errors.New("missing field `media_type`")
		}
		if raw.Data == nil {
			return errors.New("missing field `data`")
		}
		s.MediaType = *raw.MediaType
		s.Data = *raw.Data
	case MessagesImageSourceURL:
		if raw.URL == nil {
			return errors.New("missing field `url`")
		}
		s.URL = *raw.URL
	case MessagesImageSourceFile:
		if raw.FileID == nil {
			return errors.New("missing field `file_id`")
		}
		s.FileID = *raw.FileID
	default:
		return fmt.Errorf("unknown variant `%s`, expected one of `base64`, `url`, `file`", s.Type)
	}
	return nil
}

// Content block type names.
const (
	// MessagesContentBlockText is a text block.
	MessagesContentBlockText = "text"
	// MessagesContentBlockToolReference is a tool reference block.
	MessagesContentBlockToolReference = "tool_reference"
	// MessagesContentBlockToolUse is a client tool call block.
	MessagesContentBlockToolUse = "tool_use"
	// MessagesContentBlockToolResult is a tool result block.
	MessagesContentBlockToolResult = "tool_result"
	// MessagesContentBlockThinking is a thinking block.
	MessagesContentBlockThinking = "thinking"
	// MessagesContentBlockImage is an image block.
	MessagesContentBlockImage = "image"
	// MessagesContentBlockServerToolUse is a server tool invocation block.
	MessagesContentBlockServerToolUse = "server_tool_use"
	// MessagesContentBlockWebSearchToolResult is a server tool result block.
	MessagesContentBlockWebSearchToolResult = "web_search_tool_result"
	// MessagesContentBlockDocument is a document block, rendered as an
	// unsupported-document placeholder.
	MessagesContentBlockDocument = "document"
)

// MessagesContentBlock is one content block of a user or assistant message.
type MessagesContentBlock struct {
	// Type is the block discriminator. A value this adapter does not recognize is
	// preserved and rejected during conversion.
	Type string
	// Text is the text of a text block.
	Text string
	// ToolName names the referenced tool of a tool_reference block.
	ToolName string
	// ID is the tool call identifier of a tool_use or server_tool_use block.
	ID string
	// Name is the called tool name of a tool_use or server_tool_use block.
	Name string
	// Input holds the JSON input of a tool_use or server_tool_use block, kept as
	// raw JSON so that object key order survives.
	Input json.RawMessage
	// ToolUseID identifies the call a tool_result or web_search_tool_result block
	// answers.
	ToolUseID string
	// Content is the optional content of a tool_result block.
	Content *MessagesTextOrTextBlocks
	// IsError is accepted for compatibility; the result text is preserved either
	// way.
	IsError *bool
	// Signature is the signature data of a thinking block.
	Signature *string
	// Thinking is the reasoning text of a thinking block.
	Thinking string
	// Source is the image source of an image block.
	Source *MessagesImageSource
	// WebSearchContent holds the content of a web_search_tool_result block. It is
	// dropped or rejected per request.ConversionOptions.MessagesWebSearch.
	WebSearchContent json.RawMessage
}

// UnmarshalJSON decodes a content block tagged by its type.
func (b *MessagesContentBlock) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type *string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.Type == nil {
		return errors.New("missing field `type`")
	}
	b.Type = *probe.Type
	switch b.Type {
	case MessagesContentBlockText, MessagesContentBlockToolReference, MessagesContentBlockToolUse,
		MessagesContentBlockToolResult, MessagesContentBlockThinking, MessagesContentBlockImage,
		MessagesContentBlockServerToolUse, MessagesContentBlockWebSearchToolResult,
		MessagesContentBlockDocument:
	default:
		// Unknown block types are preserved and rejected during conversion.
		return nil
	}
	var raw struct {
		Text      *string         `json:"text"`
		ToolName  *string         `json:"tool_name"`
		ID        *string         `json:"id"`
		Name      *string         `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID *string         `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
		IsError   *bool           `json:"is_error"`
		Signature *string         `json:"signature"`
		Thinking  *string         `json:"thinking"`
		Source    json.RawMessage `json:"source"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch b.Type {
	case MessagesContentBlockText:
		if raw.Text == nil {
			return errors.New("missing field `text`")
		}
		b.Text = *raw.Text
	case MessagesContentBlockToolReference:
		if raw.ToolName == nil {
			return errors.New("missing field `tool_name`")
		}
		b.ToolName = *raw.ToolName
	case MessagesContentBlockToolUse, MessagesContentBlockServerToolUse:
		if raw.ID == nil {
			return errors.New("missing field `id`")
		}
		if raw.Name == nil {
			return errors.New("missing field `name`")
		}
		if raw.Input == nil {
			return errors.New("missing field `input`")
		}
		b.ID = *raw.ID
		b.Name = *raw.Name
		b.Input = raw.Input
	case MessagesContentBlockToolResult:
		if raw.ToolUseID == nil {
			return errors.New("missing field `tool_use_id`")
		}
		b.ToolUseID = *raw.ToolUseID
		b.IsError = raw.IsError
		if !isJSONNull(raw.Content) {
			var content MessagesTextOrTextBlocks
			if err := content.UnmarshalJSON(raw.Content); err != nil {
				return err
			}
			b.Content = &content
		}
	case MessagesContentBlockThinking:
		if raw.Thinking == nil {
			return errors.New("missing field `thinking`")
		}
		b.Thinking = *raw.Thinking
		b.Signature = raw.Signature
	case MessagesContentBlockImage:
		if raw.Source == nil {
			return errors.New("missing field `source`")
		}
		var source MessagesImageSource
		if err := source.UnmarshalJSON(raw.Source); err != nil {
			return err
		}
		b.Source = &source
	case MessagesContentBlockWebSearchToolResult:
		if raw.ToolUseID == nil {
			return errors.New("missing field `tool_use_id`")
		}
		if raw.Content == nil {
			return errors.New("missing field `content`")
		}
		b.ToolUseID = *raw.ToolUseID
		b.WebSearchContent = raw.Content
	case MessagesContentBlockDocument:
	}
	return nil
}

// isJSONNull reports whether raw is an explicit JSON null.
func isJSONNull(raw json.RawMessage) bool {
	return raw == nil || string(bytes.TrimSpace(raw)) == "null"
}

// MessagesTool declares one client tool. A type starting with "web_search" is
// dropped or rejected per request.ConversionOptions.MessagesWebSearch; other
// type discriminators are rejected as unsupported.
type MessagesTool struct {
	// Name is the tool name.
	Name string `json:"name"`
	// Description is the human-readable purpose of the tool.
	Description *string `json:"description"`
	// InputSchema is the JSON Schema of the tool input object, kept as raw JSON so
	// that object key order survives.
	InputSchema json.RawMessage `json:"input_schema"`
	// Type is an optional server-tool discriminator.
	Type *string `json:"type"`
}

// UnmarshalJSON reads a tool and enforces the required name.
func (t *MessagesTool) UnmarshalJSON(data []byte) error {
	type plain MessagesTool
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	name, ok := probe["name"]
	if !ok {
		return errors.New("missing field `name`")
	}
	if isJSONNull(name) {
		return errors.New("invalid type: null, expected a string")
	}
	return json.Unmarshal(data, (*plain)(t))
}

// Tool choice type names.
const (
	// MessagesToolChoiceAuto lets the model answer or call a tool.
	MessagesToolChoiceAuto = "auto"
	// MessagesToolChoiceAny permits both text replies and tool calls.
	MessagesToolChoiceAny = "any"
	// MessagesToolChoiceTool selects one named tool.
	MessagesToolChoiceTool = "tool"
	// MessagesToolChoiceNone disables tool use.
	MessagesToolChoiceNone = "none"
)

// MessagesToolChoice selects the tool strategy. The any variant is accepted for
// compatibility and, like auto, permits both text replies and tool calls.
type MessagesToolChoice struct {
	// Type is "auto", "any", "tool", or "none".
	Type string
	// Name names the selected tool of a tool choice.
	Name string
	// DisableParallelToolUse controls parallel tool execution. It is only
	// meaningful for the auto, any, and tool choices.
	DisableParallelToolUse *bool
}

// UnmarshalJSON decodes a tool choice tagged by its type.
func (c *MessagesToolChoice) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type json.RawMessage `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.Type == nil {
		return errors.New("missing field `type`")
	}
	if isJSONNull(probe.Type) {
		return errors.New("invalid type: null, expected variant identifier")
	}
	if err := json.Unmarshal(probe.Type, &c.Type); err != nil {
		return err
	}
	var raw struct {
		Name                   *string `json:"name"`
		DisableParallelToolUse *bool   `json:"disable_parallel_tool_use"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch c.Type {
	case MessagesToolChoiceAuto, MessagesToolChoiceAny:
		c.DisableParallelToolUse = raw.DisableParallelToolUse
	case MessagesToolChoiceTool:
		if raw.Name == nil {
			return errors.New("missing field `name`")
		}
		c.Name = *raw.Name
		c.DisableParallelToolUse = raw.DisableParallelToolUse
	case MessagesToolChoiceNone:
	default:
		return fmt.Errorf("unknown variant `%s`, expected one of `auto`, `any`, `tool`, `none`", c.Type)
	}
	return nil
}

// Thinking control values.
const (
	// MessagesThinkingEnabled enables thinking. The "adaptive" value is accepted
	// as an alias of "enabled".
	MessagesThinkingEnabled = "enabled"
	// MessagesThinkingDisabled disables thinking.
	MessagesThinkingDisabled = "disabled"
)

// MessagesThinking is the thinking control.
type MessagesThinking struct {
	// Type is "enabled" or "disabled".
	Type string
	// BudgetTokens is the thinking budget passed through for the caller to
	// enforce.
	BudgetTokens *uint64
}

// UnmarshalJSON decodes a thinking control, accepting the "adaptive" alias.
func (t *MessagesThinking) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type json.RawMessage `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.Type == nil {
		return errors.New("missing field `type`")
	}
	if isJSONNull(probe.Type) {
		return errors.New("invalid type: null, expected string or map")
	}
	var raw struct {
		Type         string  `json:"type"`
		BudgetTokens *uint64 `json:"budget_tokens"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch raw.Type {
	case MessagesThinkingEnabled, "adaptive":
		t.Type = MessagesThinkingEnabled
	case MessagesThinkingDisabled:
		t.Type = MessagesThinkingDisabled
	default:
		return fmt.Errorf("unknown variant `%s`, expected `enabled` or `disabled`", raw.Type)
	}
	t.BudgetTokens = raw.BudgetTokens
	return nil
}

// MessagesOutputConfig carries optional output settings.
type MessagesOutputConfig struct {
	// Effort is the requested reasoning effort.
	Effort *MessagesReasoningEffort `json:"effort"`
}

// MessagesReasoningEffort is the reasoning effort of an output config.
type MessagesReasoningEffort string

// Reasoning effort values.
const (
	// MessagesReasoningEffortLow is the lowest effort level.
	MessagesReasoningEffortLow MessagesReasoningEffort = "low"
	// MessagesReasoningEffortMedium maps to the high effort level.
	MessagesReasoningEffortMedium MessagesReasoningEffort = "medium"
	// MessagesReasoningEffortHigh is the default effort level.
	MessagesReasoningEffortHigh MessagesReasoningEffort = "high"
	// MessagesReasoningEffortXhigh is a higher-than-default effort level.
	MessagesReasoningEffortXhigh MessagesReasoningEffort = "xhigh"
	// MessagesReasoningEffortUltra maps to the maximum effort level.
	MessagesReasoningEffortUltra MessagesReasoningEffort = "ultra"
	// MessagesReasoningEffortMax is the highest effort level.
	MessagesReasoningEffortMax MessagesReasoningEffort = "max"
)

// UnmarshalJSON decodes a reasoning effort.
func (e *MessagesReasoningEffort) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch MessagesReasoningEffort(value) {
	case MessagesReasoningEffortLow, MessagesReasoningEffortMedium, MessagesReasoningEffortHigh,
		MessagesReasoningEffortXhigh, MessagesReasoningEffortUltra, MessagesReasoningEffortMax:
		*e = MessagesReasoningEffort(value)
		return nil
	}
	return fmt.Errorf("unknown variant `%s`, expected one of `low`, `medium`, `high`, `xhigh`, `ultra`, `max`", value)
}
