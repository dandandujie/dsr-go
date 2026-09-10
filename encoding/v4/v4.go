// Package v4 renders DeepSeek V4 and V4.1 prompts and encodes conversations
// into token IDs.
//
// Model-version differences are supplied through the Encoding interface, which
// the dsv4 and dsv41 subpackages implement.
package v4

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/encoding"
)

// Prompt tokens. The fullwidth vertical line (U+FF5C) and the lower one eighth
// block (U+2581) are part of the token spelling.
const (
	// BOSToken marks the start of the prompt.
	BOSToken = "<｜begin▁of▁sentence｜>"
	// ThinkingStartToken starts the reasoning content of an assistant turn.
	ThinkingStartToken = "<think>"
	// ThinkingEndToken ends the reasoning content of an assistant turn.
	ThinkingEndToken = "</think>"
	// SystemSPToken starts a system message in a V4.1 prompt.
	SystemSPToken = "<｜System｜>"
	// UserSPToken starts a user message.
	UserSPToken = "<｜User｜>"
	// AssistantSPToken starts an assistant message.
	AssistantSPToken = "<｜Assistant｜>"
	// LatestReminderSPToken starts the latest reminder message.
	LatestReminderSPToken = "<｜latest_reminder｜>"
	// EOSToken terminates a message.
	EOSToken = "<｜end▁of▁sentence｜>"
	// DSMLSPToken is the tag name prefix of the markup that structures tool
	// calls. The angle brackets come from the surrounding template, as in
	// "<｜DSML｜tool_calls>" for V4 and "<｜DSML｜ calls>" for V4.1.
	DSMLSPToken = "｜DSML｜"
)

// Encoding describes the model-version-specific parts of V4 prompt rendering.
type Encoding interface {
	// Tokenizer returns the attached tokenizer, or nil when none is attached.
	Tokenizer() encoding.TokenizerEncoder
	// SupportsMidConversationSystem reports whether a system message that
	// follows another message starts with the system token.
	SupportsMidConversationSystem() bool
	// SystemToken returns the token that starts a system message; it is empty
	// for V4.
	SystemToken() string
	// ToolCallsBlockName returns the tag name of the block that groups tool
	// calls.
	ToolCallsBlockName() string
	// ToolCallTagName returns the tag name of one tool call.
	ToolCallTagName() string
	// ToolParameterTagName returns the tag name of one tool parameter.
	ToolParameterTagName() string
	// RenderReasoningEffort returns the reasoning-effort prefix of the message
	// at index, or an empty string when the message has no prefix.
	RenderReasoningEffort(index int, thinkingMode bool, effort *core.ReasoningEffort) string
}

// Encode encodes a conversation into model token IDs using the attached
// tokenizer.
//
// The tokenizer must be attached to the encoding before this call, through the
// encoding's WithTokenizer method. Encode returns an error whose kind is
// encoding.EncodingMissingTokenizer when no tokenizer is attached.
func Encode(enc Encoding, c *core.Conversation) ([]uint32, error) {
	tokenizer := enc.Tokenizer()
	if tokenizer == nil {
		return nil, &encoding.EncodingError{Kind: encoding.EncodingMissingTokenizer}
	}
	rendered := RenderConversation(enc, c)
	ids, err := tokenizer.EncodeIDs(rendered.Prompt)
	if err != nil {
		return nil, &encoding.EncodingError{Kind: encoding.EncodingEncode, Detail: err.Error()}
	}
	return ids, nil
}

// RenderConversation renders the conversation and the prefix for the next
// assistant turn.
func RenderConversation(enc Encoding, c *core.Conversation) encoding.RenderedPrompt {
	messages := normalizeMessages(enc, c.Messages)
	hasTools := c.ToolChoice != core.ToolChoiceNone && len(c.Tools) > 0
	hasFormatSchema := c.ResponseFormat == core.ResponseFormatJSONObject
	if hasTools || hasFormatSchema {
		if len(messages) == 0 || messages[0].Kind != core.MessageSystem {
			messages = append([]core.InputMessage{core.SystemMessage("")}, messages...)
		}
		if hasTools {
			messages[0].Content += "\n\n"
			messages[0].Content += renderToolPrompt(enc, c.Tools)
		}
		if hasFormatSchema {
			messages[0].Content += "\n\n## Response Format:\n\nYou MUST strictly adhere to the following schema to reply:\n"
			messages[0].Content += jsonx.MustMarshalPython(jsonObjectSchema())
		}
	}
	var prompt strings.Builder
	prompt.WriteString(BOSToken)
	for index := range messages {
		prompt.WriteString(renderMessage(enc, messages, index, c.ThinkingMode, c.ReasoningEffort))
	}
	prompt.WriteString(AssistantSPToken)
	if c.ThinkingMode {
		prompt.WriteString(ThinkingStartToken)
	} else {
		prompt.WriteString(ThinkingEndToken)
	}
	if c.ToolChoice == core.ToolChoiceRequired && len(c.Tools) > 0 {
		prompt.WriteString("\n\n<")
		prompt.WriteString(DSMLSPToken)
		prompt.WriteString(enc.ToolCallsBlockName())
		prompt.WriteString(">\n")
	}
	return encoding.RenderedPrompt{
		Prompt:       prompt.String(),
		ImageSources: collectImageSources(messages),
	}
}

// jsonObjectSchema returns the schema appended to the prompt for a JSON-object
// response format.
func jsonObjectSchema() *jsonx.Object {
	schema := jsonx.NewObject()
	schema.Set("type", "json_object")
	return schema
}

// collectImageSources returns the image sources of the user and tool messages,
// in prompt order.
func collectImageSources(messages []core.InputMessage) []core.ImageSource {
	var sources []core.ImageSource
	for _, message := range messages {
		switch message.Kind {
		case core.MessageUser, core.MessageTool:
			sources = append(sources, message.ImageSources...)
		}
	}
	return sources
}

// parameterTemplate renders one structured tool parameter.
func parameterTemplate(dsmlToken, toolParameterTagName, key, isStr, value string) string {
	return fmt.Sprintf(
		"<%s%s name=\"%s\" string=\"%s\">%s</%s%s>",
		dsmlToken, toolParameterTagName, key, isStr, value, dsmlToken, toolParameterTagName,
	)
}

// renderToolArguments renders a tool call's arguments as structured parameters.
//
// Arguments that do not parse as a JSON object are rendered as the single
// string parameter "arguments".
func renderToolArguments(toolCall core.ToolCall, toolParameterTagName string) string {
	arguments := jsonx.NewObject()
	parsed, err := jsonx.ParseString(toolCall.Arguments)
	if err == nil {
		if object, ok := parsed.(*jsonx.Object); ok {
			arguments = object
		} else {
			arguments.Set("arguments", toolCall.Arguments)
		}
	} else {
		arguments.Set("arguments", toolCall.Arguments)
	}
	rendered := make([]string, 0, arguments.Len())
	for _, key := range arguments.Keys() {
		value, _ := arguments.Get(key)
		isStr := "false"
		text := ""
		if stringValue, ok := value.(string); ok {
			isStr = "true"
			text = stringValue
		} else {
			text = jsonx.MustMarshalPython(value)
		}
		rendered = append(rendered, parameterTemplate(DSMLSPToken, toolParameterTagName, key, isStr, text))
	}
	return strings.Join(rendered, "\n")
}

// toolCallTemplate renders one tool call.
func toolCallTemplate(enc Encoding, name, arguments string) string {
	toolCallTagName := enc.ToolCallTagName()
	return fmt.Sprintf(
		"<%s%s name=\"%s\">\n%s\n</%s%s>",
		DSMLSPToken, toolCallTagName, name, arguments, DSMLSPToken, toolCallTagName,
	)
}

// toolCallsTemplate renders the block that groups tool calls.
func toolCallsTemplate(enc Encoding, toolCalls string) string {
	toolCallsBlockName := enc.ToolCallsBlockName()
	return fmt.Sprintf(
		"<%s%s>\n%s\n</%s%s>",
		DSMLSPToken, toolCallsBlockName, toolCalls, DSMLSPToken, toolCallsBlockName,
	)
}

// renderToolCalls renders the historical tool calls of an assistant message.
func renderToolCalls(enc Encoding, toolCalls []core.ToolCall) string {
	rendered := make([]string, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		rendered = append(rendered, toolCallTemplate(
			enc,
			toolCall.Name,
			renderToolArguments(toolCall, enc.ToolParameterTagName()),
		))
	}
	return strings.Join(rendered, "\n")
}

// renderMessage renders one normalized message, including its token prefix.
func renderMessage(enc Encoding, messages []core.InputMessage, index int, thinkingMode bool, reasoningEffort *core.ReasoningEffort) string {
	message := messages[index]
	var previous *core.InputMessage
	if index > 0 {
		previous = &messages[index-1]
	}
	reasoningEffortPrompt := enc.RenderReasoningEffort(index, thinkingMode, reasoningEffort)
	prompt := ""
	if index == 0 && (reasoningEffortPrompt != "" || message.Kind == core.MessageSystem) {
		prompt = enc.SystemToken()
	}
	prompt += reasoningEffortPrompt
	switch message.Kind {
	case core.MessageSystem:
		if index > 0 && enc.SupportsMidConversationSystem() {
			prompt += enc.SystemToken()
		}
		prompt += message.Content
	case core.MessageUser:
		if previous != nil && (previous.Kind == core.MessageUser || previous.Kind == core.MessageTool) {
			prompt += "\n\n"
		} else {
			prompt += UserSPToken
		}
		prompt += message.Content
	case core.MessageLatestReminder:
		prompt += LatestReminderSPToken
		prompt += message.Content
	case core.MessageTool:
		if previous != nil && (previous.Kind == core.MessageUser || previous.Kind == core.MessageTool) {
			prompt += "\n\n"
		} else {
			prompt += UserSPToken
		}
		prompt += "<tool_result>" + message.Content + "</tool_result>"
	case core.MessageAssistant:
		toolCallsContent := ""
		if len(message.ToolCalls) > 0 {
			toolCallsContent = "\n\n" + toolCallsTemplate(enc, renderToolCalls(enc, message.ToolCalls))
		}
		thinkingPart := ""
		if thinkingMode && index > 0 {
			thinkingPart += message.ReasoningContent
			thinkingPart += ThinkingEndToken
		}
		prompt += AssistantSPToken
		if thinkingPart != "" {
			prompt += ThinkingStartToken
		} else {
			prompt += ThinkingEndToken
		}
		prompt += thinkingPart
		prompt += message.Content
		prompt += toolCallsContent
		prompt += EOSToken
	}
	return prompt
}

// renderToolPrompt renders the tool definitions appended to the first system
// message.
func renderToolPrompt(enc Encoding, tools []core.ToolDefinition) string {
	toolSchemas := make([]string, 0, len(tools))
	for _, tool := range tools {
		schema := jsonx.NewObject()
		schema.Set("name", tool.Name)
		schema.Set("description", tool.DescriptionOrEmpty())
		schema.Set("parameters", tool.Parameters)
		toolSchemas = append(toolSchemas, jsonx.MustMarshalPython(schema))
	}
	toolCallsBlockName := enc.ToolCallsBlockName()
	toolCallTagName := enc.ToolCallTagName()
	toolParameterTagName := enc.ToolParameterTagName()
	return fmt.Sprintf(
		toolPromptTemplate,
		toolCallsBlockName, toolCallsBlockName,
		toolCallTagName,
		toolParameterTagName, toolParameterTagName,
		toolCallTagName,
		toolCallTagName,
		toolCallTagName,
		toolCallsBlockName,
		strings.Join(toolSchemas, "\n"),
	)
}

// toolPromptTemplate is the tool-definition prompt. Its placeholders are the
// tool-calls block name, the tool-call tag name, the tool-parameter tag name,
// and the joined tool schemas.
const toolPromptTemplate = `## Tools

You have access to a set of tools to help answer the user's question. You can invoke tools by writing a "<｜DSML｜%s>" block like the following:

<｜DSML｜%s>
<｜DSML｜%s name="$TOOL_NAME">
<｜DSML｜%s name="$PARAMETER_NAME" string="true|false">$PARAMETER_VALUE</｜DSML｜%s>
...
</｜DSML｜%s>
<｜DSML｜%s name="$TOOL_NAME2">
...
</｜DSML｜%s>
</｜DSML｜%s>

String parameters should be specified as is and set ` + "`string=\"true\"`" + `. For all other types (numbers, booleans, arrays, objects), pass the value in JSON format and set ` + "`string=\"false\"`" + `.

If thinking_mode is enabled (triggered by ` + ThinkingStartToken + `), you MUST output your complete reasoning inside ` + ThinkingStartToken + `...` + ThinkingEndToken + ` BEFORE any tool calls or final response.

Otherwise, output directly after ` + ThinkingEndToken + ` with tool calls or final response.

### Available Tool Schemas

%s

You MUST strictly follow the above defined tool name and parameter schemas to invoke tool calls.
`

// normalizeMessages resolves the messages that prompt rendering consumes.
//
// A system message after the first message is merged into the leading system
// message (V4) or kept as its own message (V4.1). Consecutive user messages are
// joined with a blank line, and tool results are reordered to match their calls.
func normalizeMessages(enc Encoding, messages []core.InputMessage) []core.InputMessage {
	normalized := make([]core.InputMessage, 0, len(messages))
	hasNonSystem := false
	for _, message := range messages {
		if message.Kind == core.MessageSystem && !enc.SupportsMidConversationSystem() {
			if !hasNonSystem {
				if last := len(normalized) - 1; last >= 0 && normalized[last].Kind == core.MessageSystem {
					head := &normalized[last]
					if head.Content != "" && message.Content != "" {
						head.Content += "\n\n"
					}
					head.Content += message.Content
				} else {
					normalized = append(normalized, core.SystemMessage(message.Content))
				}
			} else if message.Content != "" {
				normalized = append(normalized, core.UserMessage(message.Content, nil))
			}
			continue
		}
		if message.Kind == core.MessageUser {
			hasNonSystem = true
			if last := len(normalized) - 1; last >= 0 && normalized[last].Kind == core.MessageUser {
				previous := &normalized[last]
				previous.Content += "\n\n"
				previous.Content += message.Content
				previous.ImageSources = append(previous.ImageSources, message.ImageSources...)
			} else {
				normalized = append(normalized, core.UserMessage(
					message.Content,
					cloneImageSources(message.ImageSources),
				))
			}
			continue
		}
		if message.Kind != core.MessageSystem {
			hasNonSystem = true
		}
		if message.Kind == core.MessageTool {
			message.ImageSources = cloneImageSources(message.ImageSources)
		}
		normalized = append(normalized, message)
	}
	sortToolResultsByCallOrder(normalized)
	return normalized
}

// sortToolResultsByCallOrder reorders each run of consecutive tool results to
// match the order of the tool calls of the preceding assistant message.
//
// Tool results whose call id is unknown keep the front of the run, and results
// keep their relative order when their calls are equally ranked.
func sortToolResultsByCallOrder(messages []core.InputMessage) {
	var order []string
	index := 0
	for index < len(messages) {
		switch {
		case messages[index].Kind == core.MessageAssistant && len(messages[index].ToolCalls) > 0:
			toolCalls := messages[index].ToolCalls
			order = make([]string, 0, len(toolCalls))
			for _, toolCall := range toolCalls {
				order = append(order, toolCall.ID)
			}
			index++
		case messages[index].Kind == core.MessageUser || messages[index].Kind == core.MessageTool:
			start := index
			for index < len(messages) &&
				(messages[index].Kind == core.MessageUser || messages[index].Kind == core.MessageTool) {
				index++
			}
			toolIndexes := make([]int, 0, index-start)
			for i := start; i < index; i++ {
				if messages[i].Kind == core.MessageTool {
					toolIndexes = append(toolIndexes, i)
				}
			}
			if len(toolIndexes) > 1 && len(order) > 0 {
				tools := make([]core.InputMessage, len(toolIndexes))
				for i, messageIndex := range toolIndexes {
					tools[i] = messages[messageIndex]
					messages[messageIndex] = core.LatestReminderMessage("")
				}
				sort.SliceStable(tools, func(a, b int) bool {
					return toolCallRank(tools[a], order) < toolCallRank(tools[b], order)
				})
				for i, messageIndex := range toolIndexes {
					messages[messageIndex] = tools[i]
				}
			}
		default:
			index++
		}
	}
}

// toolCallRank returns the position of a tool result's call in order, or 0 when
// the call is unknown.
func toolCallRank(message core.InputMessage, order []string) int {
	if message.Kind != core.MessageTool {
		return 0
	}
	for rank, id := range order {
		if id == message.ToolCallID {
			return rank
		}
	}
	return 0
}

// cloneImageSources copies an image-source slice so normalized messages do not
// alias the caller's conversation.
func cloneImageSources(sources []core.ImageSource) []core.ImageSource {
	if len(sources) == 0 {
		return nil
	}
	cloned := make([]core.ImageSource, len(sources))
	copy(cloned, sources)
	return cloned
}
