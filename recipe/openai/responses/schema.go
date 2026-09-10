// Package responses implements the OpenAI Responses protocol adapter.
//
// A ResponsesRequest supports text, images, plaintext reasoning history,
// instructions, sampling parameters, reasoning effort, and JSON object output.
// Client tools include functions, namespaces, and the "apply_patch" custom
// tool.
//
// Document content, encrypted reasoning, conversation storage, and hosted tool
// execution are outside the supported scope. Web-search declarations and
// choices are ignored by default; request.WebSearchReject rejects them
// instead.
//
// Input text must not spell out the image placeholder. Every placeholder in a
// prompt corresponds to one image source, and the adapter inserts them for
// image blocks only, so text input, instructions, message text, tool output
// text, reasoning text, tool definitions, and historical tool calls that carry
// the placeholder spelling are rejected.
//
// JSON Schema output, logprobs, and top_logprobs are accepted and ignored.
// Function-tool strict settings are passed through for the caller to enforce.
// The caller supplies model inference, tool execution, and HTTP transport.
package responses

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dandandujie/dsr-go/core"
)

// ResponsesRequest is a Responses request with text and image input and client
// tools.
//
// Unknown top-level fields, including logprobs and top_logprobs, are ignored.
// Model resolution, server tools, and encrypted reasoning recovery belong to
// the caller.
type ResponsesRequest struct {
	// Model is the requested model name. It is required.
	Model string `json:"model"`
	// Input is a plain string or a list of input items.
	Input *ResponsesInput `json:"input"`
	// Instructions is the system instruction prepended to the conversation.
	Instructions *string `json:"instructions"`
	// MaxOutputTokens limits the number of generated tokens.
	MaxOutputTokens *uint32 `json:"max_output_tokens"`
	// Reasoning configures the reasoning effort and summary settings.
	Reasoning *ResponsesReasoningConfig `json:"reasoning"`
	// Stream selects streaming transport.
	Stream *bool `json:"stream"`
	// Temperature is the sampling temperature.
	Temperature *float32 `json:"temperature"`
	// Text configures the answer format and verbosity.
	Text *ResponsesTextConfig `json:"text"`
	// ToolChoice selects the tool strategy.
	ToolChoice *ResponsesToolChoice `json:"tool_choice"`
	// Tools lists the declared client tools.
	Tools []ResponsesTool `json:"tools"`
	// TopP is the nucleus sampling probability.
	TopP *float32 `json:"top_p"`
	// User is caller metadata. Extract this before consuming the request.
	User *string `json:"user"`
}

// UnmarshalJSON reads a request and enforces the required model field.
func (r *ResponsesRequest) UnmarshalJSON(data []byte) error {
	type plain ResponsesRequest
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if _, ok := probe["model"]; !ok {
		return errors.New("missing field `model`")
	}
	return json.Unmarshal(data, (*plain)(r))
}

// ResponsesInput is the input field: a plain string or a list of input items.
type ResponsesInput struct {
	// IsString reports whether the JSON value was a string.
	IsString bool
	// Text holds the string form.
	Text string
	// Items holds the list form.
	Items []ResponsesInputItem
}

// UnmarshalJSON decodes the untagged string or list form.
func (i *ResponsesInput) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		i.IsString = true
		i.Text = text
		i.Items = nil
		return nil
	}
	var items []ResponsesInputItem
	if err := json.Unmarshal(data, &items); err != nil {
		return errors.New("data did not match any variant of untagged enum ResponsesInput")
	}
	i.IsString = false
	i.Text = ""
	i.Items = items
	return nil
}

// ResponsesInputItem is an explicitly typed item or a message without a type
// field. Exactly one of Typed and Message is set.
type ResponsesInputItem struct {
	// Typed holds an item carrying a type field.
	Typed *ResponsesTypedInputItem
	// Message holds an item without a type field.
	Message *ResponsesInputMessage
}

// UnmarshalJSON decodes an item. The presence of a type field selects typed
// decoding.
func (i *ResponsesInputItem) UnmarshalJSON(data []byte) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return errors.New("invalid type: expected a Responses input item object")
	}
	if _, ok := probe["type"]; ok {
		var typed ResponsesTypedInputItem
		if err := json.Unmarshal(data, &typed); err != nil {
			return err
		}
		i.Typed = &typed
		i.Message = nil
		return nil
	}
	var message ResponsesInputMessage
	if err := json.Unmarshal(data, &message); err != nil {
		return err
	}
	i.Message = &message
	i.Typed = nil
	return nil
}

// ResponsesTypedInputItemKind identifies the variant of a typed input item.
type ResponsesTypedInputItemKind uint8

// Typed input item kinds.
const (
	// ResponsesTypedInputItemUnsupported is an item whose type is not listed
	// below. It is ignored during conversion.
	ResponsesTypedInputItemUnsupported ResponsesTypedInputItemKind = iota
	// ResponsesTypedInputItemMessage is a message item.
	ResponsesTypedInputItemMessage
	// ResponsesTypedInputItemFunctionCall is a historical function call.
	ResponsesTypedInputItemFunctionCall
	// ResponsesTypedInputItemFunctionCallOutput is a function call result.
	ResponsesTypedInputItemFunctionCallOutput
	// ResponsesTypedInputItemReasoning is a reasoning history item.
	ResponsesTypedInputItemReasoning
	// ResponsesTypedInputItemCustomToolCall is a historical custom tool call.
	ResponsesTypedInputItemCustomToolCall
	// ResponsesTypedInputItemCustomToolCallOutput is a custom tool call result.
	ResponsesTypedInputItemCustomToolCallOutput
)

// ResponsesTypedInputItem is one input item carrying a type field. Kind selects
// the payload field that is set.
type ResponsesTypedInputItem struct {
	// Kind selects the variant.
	Kind ResponsesTypedInputItemKind
	// Message is the payload of a message item.
	Message *ResponsesInputMessage
	// FunctionCall is the payload of a function_call item.
	FunctionCall *ResponsesFunctionCall
	// FunctionCallOutput is the payload of a function_call_output item.
	FunctionCallOutput *ResponsesFunctionCallOutput
	// Reasoning is the payload of a reasoning item.
	Reasoning *ResponsesReasoningItem
	// CustomToolCall is the payload of a custom_tool_call item.
	CustomToolCall *ResponsesCustomToolCall
	// CustomToolCallOutput is the payload of a custom_tool_call_output item.
	CustomToolCallOutput *ResponsesCustomToolCallOutput
}

// Typed input item type names.
const (
	ResponsesTypedInputMessage              = "message"
	ResponsesTypedInputFunctionCall         = "function_call"
	ResponsesTypedInputFunctionCallOutput   = "function_call_output"
	ResponsesTypedInputReasoning            = "reasoning"
	ResponsesTypedInputCustomToolCall       = "custom_tool_call"
	ResponsesTypedInputCustomToolCallOutput = "custom_tool_call_output"
)

// UnmarshalJSON decodes a typed item by its type tag. Unsupported item types,
// including web_search_call, are ignored.
func (i *ResponsesTypedInputItem) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type *string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.Type == nil {
		return errors.New("invalid type: expected a string for the type field")
	}
	switch *probe.Type {
	case ResponsesTypedInputMessage:
		var message ResponsesInputMessage
		if err := json.Unmarshal(data, &message); err != nil {
			return err
		}
		i.Kind = ResponsesTypedInputItemMessage
		i.Message = &message
	case ResponsesTypedInputFunctionCall:
		var call ResponsesFunctionCall
		if err := json.Unmarshal(data, &call); err != nil {
			return err
		}
		i.Kind = ResponsesTypedInputItemFunctionCall
		i.FunctionCall = &call
	case ResponsesTypedInputFunctionCallOutput:
		var output ResponsesFunctionCallOutput
		if err := json.Unmarshal(data, &output); err != nil {
			return err
		}
		i.Kind = ResponsesTypedInputItemFunctionCallOutput
		i.FunctionCallOutput = &output
	case ResponsesTypedInputReasoning:
		var reasoning ResponsesReasoningItem
		if err := json.Unmarshal(data, &reasoning); err != nil {
			return err
		}
		i.Kind = ResponsesTypedInputItemReasoning
		i.Reasoning = &reasoning
	case ResponsesTypedInputCustomToolCall:
		var call ResponsesCustomToolCall
		if err := json.Unmarshal(data, &call); err != nil {
			return err
		}
		i.Kind = ResponsesTypedInputItemCustomToolCall
		i.CustomToolCall = &call
	case ResponsesTypedInputCustomToolCallOutput:
		var output ResponsesCustomToolCallOutput
		if err := json.Unmarshal(data, &output); err != nil {
			return err
		}
		i.Kind = ResponsesTypedInputItemCustomToolCallOutput
		i.CustomToolCallOutput = &output
	default:
		i.Kind = ResponsesTypedInputItemUnsupported
	}
	return nil
}

// ResponsesInputMessage is one input message.
type ResponsesInputMessage struct {
	// Role is the message role.
	Role ResponsesMessageRole
	// Content is the message content.
	Content ResponsesMessageContent
}

// UnmarshalJSON reads a message and enforces the required role and content
// fields.
func (m *ResponsesInputMessage) UnmarshalJSON(data []byte) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	raw, ok := probe["role"]
	if !ok {
		return errors.New("missing field `role`")
	}
	if err := json.Unmarshal(raw, &m.Role); err != nil {
		return err
	}
	raw, ok = probe["content"]
	if !ok {
		return errors.New("missing field `content`")
	}
	return m.Content.UnmarshalJSON(raw)
}

// ResponsesMessageRole is a message role.
type ResponsesMessageRole string

// Message role names.
const (
	ResponsesMessageRoleUser      ResponsesMessageRole = "user"
	ResponsesMessageRoleAssistant ResponsesMessageRole = "assistant"
	ResponsesMessageRoleSystem    ResponsesMessageRole = "system"
	ResponsesMessageRoleDeveloper ResponsesMessageRole = "developer"
)

// UnmarshalJSON decodes a message role.
func (r *ResponsesMessageRole) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch ResponsesMessageRole(value) {
	case ResponsesMessageRoleUser, ResponsesMessageRoleAssistant, ResponsesMessageRoleSystem, ResponsesMessageRoleDeveloper:
		*r = ResponsesMessageRole(value)
		return nil
	}
	return fmt.Errorf("unknown variant `%s`, expected one of `user`, `assistant`, `system`, `developer`", value)
}

// ResponsesMessageContent is either plain text or a list of content blocks.
type ResponsesMessageContent struct {
	// IsString reports whether the JSON value was a string.
	IsString bool
	// Text holds the string form.
	Text string
	// Blocks holds the list form.
	Blocks []ResponsesContentBlock
}

// UnmarshalJSON decodes the untagged string or list form.
func (c *ResponsesMessageContent) UnmarshalJSON(data []byte) error {
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
	var blocks []ResponsesContentBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		return errors.New("data did not match any variant of untagged enum ResponsesMessageContent")
	}
	c.IsString = false
	c.Text = ""
	c.Blocks = blocks
	return nil
}

// ResponsesContentBlock is one block of a message content list.
type ResponsesContentBlock struct {
	// Type is one of "input_text", "output_text", "input_image", or
	// "input_file". Content block types not listed are unsupported and are
	// rejected during conversion.
	Type string
	// Text is the text of an input_text or output_text block.
	Text string
	// Image is the image of an input_image block.
	Image *ResponsesInputImage
}

// Content block type names.
const (
	ResponsesContentBlockInputText  = "input_text"
	ResponsesContentBlockOutputText = "output_text"
	ResponsesContentBlockInputImage = "input_image"
	ResponsesContentBlockInputFile  = "input_file"
)

// UnmarshalJSON decodes a content block by its type tag.
func (b *ResponsesContentBlock) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type *string `json:"type"`
		Text *string `json:"text"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Type == nil {
		return errors.New("missing field `type`")
	}
	b.Type = *raw.Type
	switch b.Type {
	case ResponsesContentBlockInputText, ResponsesContentBlockOutputText:
		if raw.Text == nil {
			return errors.New("missing field `text`")
		}
		b.Text = *raw.Text
	case ResponsesContentBlockInputImage:
		var image ResponsesInputImage
		if err := json.Unmarshal(data, &image); err != nil {
			return err
		}
		b.Image = &image
	case ResponsesContentBlockInputFile:
	default:
		// Content block types not listed are rejected during conversion.
	}
	return nil
}

// ResponsesInputImage is one image referenced by URL or by a file identifier. A
// file identifier requires a file service and is rejected during conversion.
type ResponsesInputImage struct {
	// ImageURL is an HTTP URL or base64 data URL. An empty string is treated as
	// absent, and a value that does not start with "http" must carry a "base64"
	// body.
	ImageURL *string `json:"image_url"`
	// Detail is the requested detail level.
	Detail *core.ImageDetail `json:"detail"`
	// FileID identifies a stored file. It is rejected during conversion.
	FileID *string `json:"file_id"`
}

// UnmarshalJSON decodes an image reference, including its detail level.
func (i *ResponsesInputImage) UnmarshalJSON(data []byte) error {
	var raw struct {
		ImageURL *string `json:"image_url"`
		Detail   *string `json:"detail"`
		FileID   *string `json:"file_id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	i.ImageURL = raw.ImageURL
	i.FileID = raw.FileID
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

// ResponsesFunctionCall is one historical function call.
type ResponsesFunctionCall struct {
	// Arguments is the serialized call arguments.
	Arguments string `json:"arguments"`
	// CallID identifies the call in later function call results.
	CallID string `json:"call_id"`
	// Name is the called function name.
	Name string `json:"name"`
	// Namespace is the namespace of the called function, when declared.
	Namespace *string `json:"namespace"`
}

// UnmarshalJSON reads a function call and enforces its required fields.
func (c *ResponsesFunctionCall) UnmarshalJSON(data []byte) error {
	type plain ResponsesFunctionCall
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	for _, field := range []string{"arguments", "call_id", "name"} {
		if _, ok := probe[field]; !ok {
			return fmt.Errorf("missing field `%s`", field)
		}
	}
	return json.Unmarshal(data, (*plain)(c))
}

// ResponsesFunctionCallOutput is the result of one historical function call.
type ResponsesFunctionCallOutput struct {
	// CallID identifies the call this output answers.
	CallID string
	// Output is the call output content.
	Output ResponsesToolCallOutputContent
}

// UnmarshalJSON reads a function call output and enforces its required fields.
func (o *ResponsesFunctionCallOutput) UnmarshalJSON(data []byte) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	rawCallID, ok := probe["call_id"]
	if !ok {
		return errors.New("missing field `call_id`")
	}
	if err := json.Unmarshal(rawCallID, &o.CallID); err != nil {
		return err
	}
	raw, ok := probe["output"]
	if !ok {
		return errors.New("missing field `output`")
	}
	return o.Output.UnmarshalJSON(raw)
}

// ResponsesToolCallOutputContent is either plain text or a list of output
// items.
type ResponsesToolCallOutputContent struct {
	// IsString reports whether the JSON value was a string.
	IsString bool
	// Text holds the string form.
	Text string
	// Items holds the list form.
	Items []ResponsesToolCallOutputItem
}

// UnmarshalJSON decodes the untagged string or list form.
func (c *ResponsesToolCallOutputContent) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		c.IsString = true
		c.Text = text
		c.Items = nil
		return nil
	}
	var items []ResponsesToolCallOutputItem
	if err := json.Unmarshal(data, &items); err != nil {
		return errors.New("data did not match any variant of untagged enum ResponsesToolCallOutputContent")
	}
	c.IsString = false
	c.Text = ""
	c.Items = items
	return nil
}

// ResponsesToolCallOutputItem is one item of a tool call output list.
type ResponsesToolCallOutputItem struct {
	// Type is one of "input_text", "input_image", or "input_file". Content
	// block types not listed are rejected during conversion.
	Type string
	// Text is the text of an input_text item.
	Text string
	// Image is the image of an input_image item.
	Image *ResponsesInputImage
}

// UnmarshalJSON decodes a tool call output item by its type tag.
func (i *ResponsesToolCallOutputItem) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type *string `json:"type"`
		Text *string `json:"text"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Type == nil {
		return errors.New("missing field `type`")
	}
	i.Type = *raw.Type
	switch i.Type {
	case ResponsesContentBlockInputText:
		if raw.Text == nil {
			return errors.New("missing field `text`")
		}
		i.Text = *raw.Text
	case ResponsesContentBlockInputImage:
		var image ResponsesInputImage
		if err := json.Unmarshal(data, &image); err != nil {
			return err
		}
		i.Image = &image
	case ResponsesContentBlockInputFile:
	default:
		// Content block types not listed are rejected during conversion.
	}
	return nil
}

// ResponsesReasoningItem is one reasoning history item.
type ResponsesReasoningItem struct {
	// Content holds the plaintext reasoning blocks.
	Content []ResponsesReasoningContent `json:"content"`
	// EncryptedContent is ignored; plaintext reasoning is preserved.
	EncryptedContent *string `json:"encrypted_content"`
}

// ResponsesReasoningContent is one plaintext reasoning block.
type ResponsesReasoningContent struct {
	// Text is the reasoning text.
	Text string `json:"text"`
}

// UnmarshalJSON reads a reasoning block and enforces its required text field.
func (c *ResponsesReasoningContent) UnmarshalJSON(data []byte) error {
	type plain ResponsesReasoningContent
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if _, ok := probe["text"]; !ok {
		return errors.New("missing field `text`")
	}
	return json.Unmarshal(data, (*plain)(c))
}

// ResponsesCustomToolCall is one historical custom tool call.
type ResponsesCustomToolCall struct {
	// CallID identifies the call in later custom tool call results.
	CallID string `json:"call_id"`
	// Name is the custom tool name.
	Name string `json:"name"`
	// Input is the custom tool input.
	Input string `json:"input"`
}

// UnmarshalJSON reads a custom tool call and enforces its required fields.
func (c *ResponsesCustomToolCall) UnmarshalJSON(data []byte) error {
	type plain ResponsesCustomToolCall
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	for _, field := range []string{"call_id", "name", "input"} {
		if _, ok := probe[field]; !ok {
			return fmt.Errorf("missing field `%s`", field)
		}
	}
	return json.Unmarshal(data, (*plain)(c))
}

// ResponsesCustomToolCallOutput is the result of one historical custom tool
// call.
type ResponsesCustomToolCallOutput struct {
	// CallID identifies the call this output answers.
	CallID string `json:"call_id"`
	// Output is the call output content.
	Output *ResponsesToolCallOutputContent `json:"output"`
}

// UnmarshalJSON reads a custom tool call output and enforces its call_id.
func (o *ResponsesCustomToolCallOutput) UnmarshalJSON(data []byte) error {
	type plain ResponsesCustomToolCallOutput
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if _, ok := probe["call_id"]; !ok {
		return errors.New("missing field `call_id`")
	}
	return json.Unmarshal(data, (*plain)(o))
}

// ResponsesReasoningConfig configures reasoning for a request.
type ResponsesReasoningConfig struct {
	// Effort selects the reasoning effort.
	Effort *ResponsesReasoningEffort `json:"effort"`
	// Summary is a summary setting available to the caller before conversion.
	Summary *ResponsesReasoningSummary `json:"summary"`
	// GenerateSummary is an alternate summary setting available to the caller
	// before conversion.
	GenerateSummary *ResponsesReasoningSummary `json:"generate_summary"`
}

// ResponsesReasoningEffort is the reasoning effort accepted by this adapter.
type ResponsesReasoningEffort string

// Reasoning effort values.
const (
	ResponsesReasoningEffortNone    ResponsesReasoningEffort = "none"
	ResponsesReasoningEffortMinimal ResponsesReasoningEffort = "minimal"
	ResponsesReasoningEffortLow     ResponsesReasoningEffort = "low"
	ResponsesReasoningEffortMedium  ResponsesReasoningEffort = "medium"
	ResponsesReasoningEffortHigh    ResponsesReasoningEffort = "high"
	ResponsesReasoningEffortXhigh   ResponsesReasoningEffort = "xhigh"
	ResponsesReasoningEffortMax     ResponsesReasoningEffort = "max"
)

// UnmarshalJSON decodes a reasoning effort.
func (e *ResponsesReasoningEffort) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch ResponsesReasoningEffort(value) {
	case ResponsesReasoningEffortNone, ResponsesReasoningEffortMinimal,
		ResponsesReasoningEffortLow, ResponsesReasoningEffortMedium,
		ResponsesReasoningEffortHigh, ResponsesReasoningEffortXhigh,
		ResponsesReasoningEffortMax:
		*e = ResponsesReasoningEffort(value)
		return nil
	}
	return fmt.Errorf("unknown variant `%s`, expected one of `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`", value)
}

// ResponsesReasoningSummary is a reasoning summary setting.
type ResponsesReasoningSummary string

// Reasoning summary values.
const (
	ResponsesReasoningSummaryAuto     ResponsesReasoningSummary = "auto"
	ResponsesReasoningSummaryConcise  ResponsesReasoningSummary = "concise"
	ResponsesReasoningSummaryDetailed ResponsesReasoningSummary = "detailed"
)

// UnmarshalJSON decodes a reasoning summary setting.
func (s *ResponsesReasoningSummary) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch ResponsesReasoningSummary(value) {
	case ResponsesReasoningSummaryAuto, ResponsesReasoningSummaryConcise, ResponsesReasoningSummaryDetailed:
		*s = ResponsesReasoningSummary(value)
		return nil
	}
	return fmt.Errorf("unknown variant `%s`, expected one of `auto`, `concise`, `detailed`", value)
}

// ResponsesTextConfig configures the answer format of a request.
type ResponsesTextConfig struct {
	// Format selects text, JSON object, or JSON Schema output. An absent format
	// means text output.
	Format ResponsesTextFormat `json:"format"`
	// Verbosity is a setting available to the caller before conversion.
	Verbosity *ResponsesVerbosity `json:"verbosity"`
}

// ResponsesTextFormat selects the answer format.
type ResponsesTextFormat struct {
	// Type is one of "text", "json_object", or "json_schema". JSON Schema
	// output is accepted and ignored, and uses plain text output. The zero
	// value means text output.
	Type string
}

// Text format type names.
const (
	ResponsesTextFormatText       = "text"
	ResponsesTextFormatJSONObject = "json_object"
	ResponsesTextFormatJSONSchema = "json_schema"
)

// UnmarshalJSON decodes a text format by its type tag.
func (f *ResponsesTextFormat) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type *string `json:"type"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Type == nil {
		return errors.New("missing field `type`")
	}
	switch *raw.Type {
	case ResponsesTextFormatText, ResponsesTextFormatJSONObject, ResponsesTextFormatJSONSchema:
		f.Type = *raw.Type
		return nil
	}
	return fmt.Errorf("unknown variant `%s`, expected one of `text`, `json_object`, `json_schema`", *raw.Type)
}

// ResponsesVerbosity is a verbosity setting.
type ResponsesVerbosity string

// Verbosity values.
const (
	ResponsesVerbosityLow    ResponsesVerbosity = "low"
	ResponsesVerbosityMedium ResponsesVerbosity = "medium"
	ResponsesVerbosityHigh   ResponsesVerbosity = "high"
)

// UnmarshalJSON decodes a verbosity setting.
func (v *ResponsesVerbosity) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch ResponsesVerbosity(value) {
	case ResponsesVerbosityLow, ResponsesVerbosityMedium, ResponsesVerbosityHigh:
		*v = ResponsesVerbosity(value)
		return nil
	}
	return fmt.Errorf("unknown variant `%s`, expected one of `low`, `medium`, `high`", value)
}

// ResponsesTool is one declared tool. Type selects the payload fields that are
// set.
type ResponsesTool struct {
	// Type is one of "function", "namespace", "custom", or "web_search".
	// Unsupported tool types are ignored during conversion.
	Type string
	// Function holds a function tool or one namespace member.
	Function *ResponsesFunctionTool
	// Name is the namespace name or the custom tool name.
	Name string
	// Description is the namespace description.
	Description *string
	// Tools holds the members of a namespace.
	Tools []ResponsesNamespaceTool
}

// Tool type names.
const (
	ResponsesToolTypeFunction       = "function"
	ResponsesToolTypeNamespace      = "namespace"
	ResponsesToolTypeCustom         = "custom"
	ResponsesToolTypeWebSearch      = "web_search"
	ResponsesToolTypeWebSearchDated = "web_search_2025_08_26"
)

// UnmarshalJSON decodes a tool by its type tag.
func (t *ResponsesTool) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type        *string         `json:"type"`
		Name        *string         `json:"name"`
		Description *string         `json:"description"`
		Tools       json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Type == nil {
		return errors.New("missing field `type`")
	}
	t.Type = *raw.Type
	switch t.Type {
	case ResponsesToolTypeFunction:
		var function ResponsesFunctionTool
		if err := json.Unmarshal(data, &function); err != nil {
			return err
		}
		t.Function = &function
	case ResponsesToolTypeNamespace:
		if raw.Name == nil {
			return errors.New("missing field `name`")
		}
		if raw.Tools == nil {
			return errors.New("missing field `tools`")
		}
		t.Name = *raw.Name
		t.Description = raw.Description
		if err := json.Unmarshal(raw.Tools, &t.Tools); err != nil {
			return err
		}
	case ResponsesToolTypeCustom:
		if raw.Name == nil {
			return errors.New("missing field `name`")
		}
		t.Name = *raw.Name
	case ResponsesToolTypeWebSearch, ResponsesToolTypeWebSearchDated:
	default:
		// Unsupported tool types are ignored during conversion.
	}
	return nil
}

// ResponsesFunctionTool describes one function tool.
type ResponsesFunctionTool struct {
	// Name is the function name.
	Name string `json:"name"`
	// Description is the human-readable purpose of the function.
	Description *string `json:"description"`
	// Parameters is a JSON Schema of type object, kept as raw JSON so object
	// key order is preserved. A nil value means the field is absent.
	Parameters json.RawMessage `json:"parameters"`
	// Strict is passed through; backend enforcement belongs to the caller.
	Strict *bool `json:"strict"`
}

// UnmarshalJSON reads a function tool and enforces its required name field.
func (f *ResponsesFunctionTool) UnmarshalJSON(data []byte) error {
	type plain ResponsesFunctionTool
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if _, ok := probe["name"]; !ok {
		return errors.New("missing field `name`")
	}
	return json.Unmarshal(data, (*plain)(f))
}

// ResponsesNamespaceTool is one member of a namespace.
type ResponsesNamespaceTool struct {
	// Type is "function" or "custom". Custom tools inside a namespace are
	// rejected during conversion.
	Type string
	// Function holds a function namespace member.
	Function *ResponsesFunctionTool
	// Name is the name of a custom namespace member.
	Name string
}

// UnmarshalJSON decodes a namespace member by its type tag.
func (t *ResponsesNamespaceTool) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type *string `json:"type"`
		Name *string `json:"name"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Type == nil {
		return errors.New("missing field `type`")
	}
	t.Type = *raw.Type
	switch t.Type {
	case ResponsesToolTypeFunction:
		var function ResponsesFunctionTool
		if err := json.Unmarshal(data, &function); err != nil {
			return err
		}
		t.Function = &function
	case ResponsesToolTypeCustom:
		if raw.Name == nil {
			return errors.New("missing field `name`")
		}
		t.Name = *raw.Name
	default:
		return fmt.Errorf("unknown variant `%s`, expected `function` or `custom`", t.Type)
	}
	return nil
}

// ResponsesToolChoice is a tool choice mode or a named tool choice.
type ResponsesToolChoice struct {
	// Mode is the string form.
	Mode *ResponsesToolChoiceMode
	// Named is the object form.
	Named *ResponsesNamedToolChoice
}

// UnmarshalJSON decodes the untagged mode or named form.
func (c *ResponsesToolChoice) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var mode ResponsesToolChoiceMode
		if err := mode.UnmarshalJSON(data); err != nil {
			return err
		}
		c.Mode = &mode
		c.Named = nil
		return nil
	}
	var named ResponsesNamedToolChoice
	if err := json.Unmarshal(data, &named); err != nil {
		if _, ok := err.(*json.UnmarshalTypeError); ok {
			return errors.New("data did not match any variant of untagged enum ResponsesToolChoice")
		}
		return err
	}
	c.Named = &named
	c.Mode = nil
	return nil
}

// ResponsesToolChoiceMode is a tool choice mode.
type ResponsesToolChoiceMode string

// Tool choice mode names.
const (
	ResponsesToolChoiceModeNone     ResponsesToolChoiceMode = "none"
	ResponsesToolChoiceModeAuto     ResponsesToolChoiceMode = "auto"
	ResponsesToolChoiceModeRequired ResponsesToolChoiceMode = "required"
)

// UnmarshalJSON decodes a tool choice mode.
func (m *ResponsesToolChoiceMode) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch ResponsesToolChoiceMode(value) {
	case ResponsesToolChoiceModeNone, ResponsesToolChoiceModeAuto, ResponsesToolChoiceModeRequired:
		*m = ResponsesToolChoiceMode(value)
		return nil
	}
	return fmt.Errorf("unknown variant `%s`, expected one of `none`, `auto`, `required`", value)
}

// ResponsesNamedToolChoice selects one named tool.
type ResponsesNamedToolChoice struct {
	// Type is one of "function", "custom", or "web_search".
	Type string
	// Name is the selected tool name for function and custom choices.
	Name string
}

// UnmarshalJSON decodes a named tool choice by its type tag.
func (c *ResponsesNamedToolChoice) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type *string `json:"type"`
		Name *string `json:"name"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Type == nil {
		return errors.New("missing field `type`")
	}
	c.Type = *raw.Type
	switch c.Type {
	case ResponsesToolTypeFunction, ResponsesToolTypeCustom:
		if raw.Name == nil {
			return errors.New("missing field `name`")
		}
		c.Name = *raw.Name
	case ResponsesToolTypeWebSearch, ResponsesToolTypeWebSearchDated:
	default:
		return fmt.Errorf("unknown variant `%s`, expected one of `function`, `custom`, `web_search`", c.Type)
	}
	return nil
}
