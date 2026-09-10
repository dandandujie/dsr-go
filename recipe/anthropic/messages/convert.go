package messages

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/internal/protocol"
	"github.com/dandandujie/dsr-go/recipe/request"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// maxStopSequences is the maximum number of stop sequences accepted by the
// adapter.
const maxStopSequences = 16

// Convert validates and converts the request into a conversation request.
//
// Explicit thinking overrides the reasoning effort, which overrides the
// supplied default. Missing historical thinking is accepted, and supplied
// thinking is preserved.
func (r *MessagesRequest) Convert(options request.ConversionOptions) (*request.ConversationRequest, error) {
	thinkingMode, reasoningEffort := r.resolveThinking(options)
	inferenceOptions, err := r.inferenceOptions()
	if err != nil {
		return nil, err
	}
	stopSequences := r.StopSequences
	if stopSequences == nil {
		stopSequences = []string{}
	}
	tools, toolChoice, err := convertTools(r.Tools, r.ToolChoice, thinkingMode, &options)
	if err != nil {
		return nil, err
	}
	parsingOptions := parsingOptionsFor(thinkingMode, tools, toolChoice, stopSequences)
	messages, err := transformMessages(r.Messages, r.System, &options)
	if err != nil {
		return nil, err
	}
	if err := protocol.ValidateToolText(tools, messages); err != nil {
		return nil, err
	}

	conversation := &core.Conversation{
		Messages:        messages,
		ThinkingMode:    thinkingMode,
		Tools:           tools,
		ToolChoice:      toolChoice,
		ReasoningEffort: reasoningEffort,
		ResponseFormat:  core.ResponseFormatText,
	}
	converted := &request.ConversationRequest{
		Conversation:     conversation,
		InferenceOptions: inferenceOptions,
		ParsingOptions:   parsingOptions,
		Model:            &r.Model,
		Stream:           r.Stream != nil && *r.Stream,
	}
	converted.NewChunkGenerator = func(id, model string) stream.Generator {
		return NewChunkGenerator(id, model, thinkingMode)
	}
	return converted, nil
}

// resolveThinking returns the resolved thinking mode and reasoning effort.
func (r *MessagesRequest) resolveThinking(options request.ConversionOptions) (bool, *core.ReasoningEffort) {
	var effort *core.ReasoningEffort
	if r.OutputConfig != nil && r.OutputConfig.Effort != nil {
		effort = reasoningEffortFrom(*r.OutputConfig.Effort)
	}
	thinking := options.DefaultThinkingMode
	switch {
	case r.Thinking != nil && r.Thinking.Type == MessagesThinkingEnabled:
		thinking = true
	case r.Thinking != nil && r.Thinking.Type == MessagesThinkingDisabled:
		thinking = false
	case r.Thinking == nil && effort != nil:
		thinking = true
	}
	if !thinking {
		return false, nil
	}
	if effort == nil {
		high := core.ReasoningEffortHigh
		return true, &high
	}
	return true, effort
}

// reasoningEffortFrom maps a protocol effort to the shared reasoning level.
func reasoningEffortFrom(effort MessagesReasoningEffort) *core.ReasoningEffort {
	value := func(level core.ReasoningEffort) *core.ReasoningEffort { return &level }
	switch effort {
	case MessagesReasoningEffortLow:
		return value(core.ReasoningEffortLow)
	case MessagesReasoningEffortMedium, MessagesReasoningEffortHigh:
		return value(core.ReasoningEffortHigh)
	case MessagesReasoningEffortXhigh:
		return value(core.ReasoningEffortXhigh)
	default:
		return value(core.ReasoningEffortMax)
	}
}

// inferenceOptions validates and extracts the inference parameters.
func (r *MessagesRequest) inferenceOptions() (request.InferenceOptions, error) {
	if err := protocol.ValidateMaxTokens(r.MaxTokens, "max_tokens"); err != nil {
		return request.InferenceOptions{}, err
	}
	if err := protocol.ValidateSampling(r.Temperature, r.TopP); err != nil {
		return request.InferenceOptions{}, err
	}
	if len(r.StopSequences) > maxStopSequences {
		return request.InferenceOptions{}, request.BadRequest(
			"stop_sequences must contain at most 16 non-empty strings")
	}
	for _, sequence := range r.StopSequences {
		if sequence == "" {
			return request.InferenceOptions{}, request.BadRequest(
				"stop_sequences must contain at most 16 non-empty strings")
		}
	}
	var disableParallelToolUse *bool
	if r.ToolChoice != nil {
		switch r.ToolChoice.Type {
		case MessagesToolChoiceAuto, MessagesToolChoiceAny, MessagesToolChoiceTool:
			disableParallelToolUse = r.ToolChoice.DisableParallelToolUse
		}
	}
	var thinkingBudgetTokens *uint64
	if r.Thinking != nil {
		thinkingBudgetTokens = r.Thinking.BudgetTokens
	}
	return request.InferenceOptions{
		MaxTokens:              r.MaxTokens,
		Temperature:            r.Temperature,
		TopP:                   r.TopP,
		ThinkingBudgetTokens:   thinkingBudgetTokens,
		DisableParallelToolUse: disableParallelToolUse,
	}, nil
}

// parsingOptionsFor builds the output parsing options of this request.
func parsingOptionsFor(
	thinking bool,
	tools []core.ToolDefinition,
	toolChoice core.ToolChoice,
	stopSequences []string,
) stream.ParsingOptions {
	options := stream.DefaultParsingOptions()
	options.ParseToolCalls = len(tools) > 0
	if thinking {
		stage := stream.ReasoningStageStart
		options.ReasoningInitialStage = &stage
	}
	options.ToolCallInitialStage = toolChoice == core.ToolChoiceRequired
	options.StopSequences = append([]string{}, stopSequences...)
	return options
}

// convertTools converts client tool declarations and resolves the tool choice.
func convertTools(
	tools []MessagesTool,
	choice *MessagesToolChoice,
	thinking bool,
	options *request.ConversionOptions,
) ([]core.ToolDefinition, core.ToolChoice, error) {
	names := make(map[string]struct{}, len(tools))
	definitions := make([]core.ToolDefinition, 0, len(tools))
	ignoredServerToolNames := make(map[string]struct{})
	for index := range tools {
		tool := &tools[index]
		if tool.Type != nil {
			if strings.HasPrefix(*tool.Type, "web_search") &&
				options.MessagesWebSearch == request.WebSearchIgnore {
				ignoredServerToolNames[tool.Name] = struct{}{}
				continue
			}
			return nil, core.ToolChoiceAuto, request.BadRequestf(
				"tools.%d: server tools are not supported", index)
		}
		if err := protocol.ValidateToolName(tool.Name, fmt.Sprintf("tools.%d.name", index)); err != nil {
			return nil, core.ToolChoiceAuto, err
		}
		if _, exists := names[tool.Name]; exists {
			return nil, core.ToolChoiceAuto, request.BadRequest("Tool names must be unique")
		}
		names[tool.Name] = struct{}{}
		// An explicit null is an absent optional value.
		if isJSONNull(tool.InputSchema) {
			return nil, core.ToolChoiceAuto, request.BadRequestf("tools.%d: missing input_schema", index)
		}
		parameters, err := jsonx.Parse(tool.InputSchema)
		if err != nil {
			return nil, core.ToolChoiceAuto, request.BadRequestf(
				"tools.%d.input_schema: invalid JSON Schema: %v", index, err)
		}
		if err := protocol.ValidateToolParameters(parameters, fmt.Sprintf("tools.%d.input_schema", index)); err != nil {
			return nil, core.ToolChoiceAuto, err
		}
		definitions = append(definitions, core.ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  parameters,
		})
	}
	switch {
	case choice != nil && choice.Type == MessagesToolChoiceNone:
		return []core.ToolDefinition{}, core.ToolChoiceNone, nil
	case choice != nil && choice.Type == MessagesToolChoiceTool:
		name := choice.Name
		if _, ignored := ignoredServerToolNames[name]; ignored {
			// The named server tool was dropped; drop the choice too.
			return definitions, core.ToolChoiceAuto, nil
		}
		if _, ok := names[name]; !ok {
			return nil, core.ToolChoiceAuto, request.BadRequestf(
				"tool_choice: no tool named '%s' was specified", name)
		}
		if thinking {
			return nil, core.ToolChoiceAuto, request.BadRequest(
				"Thinking mode does not support this tool_choice")
		}
		kept := make([]core.ToolDefinition, 0, 1)
		for _, tool := range definitions {
			if tool.Name == name {
				kept = append(kept, tool)
			}
		}
		return kept, core.ToolChoiceRequired, nil
	default:
		return definitions, core.ToolChoiceAuto, nil
	}
}

// transformMessages converts the Messages conversation into input messages.
func transformMessages(
	messages []MessagesMessage,
	system *MessagesTextOrTextBlocks,
	options *request.ConversionOptions,
) ([]core.InputMessage, error) {
	if len(messages) == 0 {
		return nil, request.BadRequest("messages: at least one message is required")
	}
	if messagesContainImageSpecialToken(messages, system) {
		return nil, protocol.ImageSpecialTokenNotAllowed()
	}

	transformed := make([]core.InputMessage, 0, len(messages)+1)
	if system != nil {
		content, err := system.intoText("system", "system")
		if err != nil {
			return nil, err
		}
		transformed = append(transformed, core.SystemMessage(content))
	}

	toolUseIDs := make(map[string]int)
	for messageIndex := range messages {
		message := &messages[messageIndex]
		switch message.Role {
		case MessagesRoleUser:
			if message.Content == nil {
				return nil, request.BadRequestf(
					"messages.%d: all messages must have non-empty content", messageIndex)
			}
			if message.Content.IsString {
				if err := checkToolUseResolved(messageIndex, toolUseIDs); err != nil {
					return nil, err
				}
				transformed = append(transformed,
					core.UserMessage(message.Content.Text, []core.ImageSource{}))
				continue
			}
			blocks := message.Content.Blocks
			if len(blocks) == 0 {
				return nil, request.BadRequestf(
					"messages.%d: all messages must have non-empty content", messageIndex)
			}
			matchedToolUseIDs := make(map[string]struct{})
			for blockIndex := range blocks {
				block := &blocks[blockIndex]
				path := fmt.Sprintf("messages.%d.content[%d]", messageIndex, blockIndex)
				switch block.Type {
				case MessagesContentBlockText, MessagesContentBlockToolReference:
					if err := checkToolUseResolved(messageIndex, toolUseIDs); err != nil {
						return nil, err
					}
					content, err := block.intoText(path, "user")
					if err != nil {
						return nil, err
					}
					transformed = append(transformed,
						core.UserMessage(content, []core.ImageSource{}))
				case MessagesContentBlockImage:
					if err := checkToolUseResolved(messageIndex, toolUseIDs); err != nil {
						return nil, err
					}
					source, err := imageSource(*block.Source, path)
					if err != nil {
						return nil, err
					}
					transformed = append(transformed, core.UserMessage(
						core.ImageSpecialToken, []core.ImageSource{source}))
				case MessagesContentBlockDocument:
					if err := checkToolUseResolved(messageIndex, toolUseIDs); err != nil {
						return nil, err
					}
					transformed = append(transformed, core.UserMessage(
						protocol.UnsupportedDocumentPlaceholder, []core.ImageSource{}))
				case MessagesContentBlockToolUse:
					return nil, request.BadRequestf(
						"messages.%d: `tool_use` blocks can only be in `assistant` messages",
						messageIndex)
				case MessagesContentBlockToolResult:
					toolUseID := block.ToolUseID
					if _, duplicate := matchedToolUseIDs[toolUseID]; duplicate {
						return nil, request.BadRequestf(
							"messages.%d.content.%d: each tool_use must have a single result. Found multiple `tool_result` blocks with id: %s",
							messageIndex, blockIndex, toolUseID)
					} else if _, known := toolUseIDs[toolUseID]; !known {
						return nil, request.BadRequestf(
							"unexpected `messages.%d.content.%d: tool_use_id` found in `tool_result` blocks: %s. Each `tool_result` block must have a corresponding `tool_use` block in the previous message.",
							messageIndex, blockIndex, toolUseID)
					} else {
						delete(toolUseIDs, toolUseID)
						matchedToolUseIDs[toolUseID] = struct{}{}
					}
					var content string
					var imageSources []core.ImageSource
					if block.Content != nil {
						value, images, err := block.Content.intoContent(path)
						if err != nil {
							return nil, err
						}
						content, imageSources = value, images
					}
					transformed = append(transformed,
						core.ToolMessage(content, imageSources, toolUseID))
				case MessagesContentBlockThinking:
					return nil, request.BadRequestf(
						"messages.%d.content: thinking blocks may only be in `assistant` messages",
						messageIndex)
				case MessagesContentBlockServerToolUse:
					return nil, request.BadRequestf(
						"messages.%d: `server_tool_use` blocks can only be in `assistant` messages",
						messageIndex)
				case MessagesContentBlockWebSearchToolResult:
					return nil, request.BadRequestf(
						"messages.%d: `web_search_tool_result` blocks can only be in `assistant` messages",
						messageIndex)
				default:
					return nil, unsupportedBlock()
				}
			}
			if len(toolUseIDs) > 0 {
				return nil, request.BadRequestf(
					"messages.%d:`tool_use` ids were found without `tool_result` blocks immediately after: %s. Each `tool_use` block must have a corresponding `tool_result` block in the next message.",
					messageIndex-1, joinToolUseIDs(toolUseIDs))
			}
		case MessagesRoleAssistant:
			if message.Content == nil {
				return nil, request.BadRequestf(
					"messages.%d: all messages must have non-empty content", messageIndex)
			}
			if message.Content.IsString {
				if err := checkToolUseResolved(messageIndex, toolUseIDs); err != nil {
					return nil, err
				}
				transformed = append(transformed,
					core.AssistantMessage(message.Content.Text, "", nil))
				continue
			}
			if err := checkToolUseResolved(messageIndex, toolUseIDs); err != nil {
				return nil, err
			}
			blocks := message.Content.Blocks
			if len(blocks) == 0 {
				return nil, request.BadRequestf(
					"messages.%d: all messages must have non-empty content", messageIndex)
			}
			var content strings.Builder
			var reasoningContent *string
			var toolCalls []core.ToolCall
			for blockIndex := range blocks {
				block := &blocks[blockIndex]
				path := fmt.Sprintf("messages.%d.content[%d]", messageIndex, blockIndex)
				switch block.Type {
				case MessagesContentBlockText, MessagesContentBlockToolReference,
					MessagesContentBlockImage, MessagesContentBlockDocument:
					if toolCalls != nil {
						return nil, request.BadRequestf(
							"messages.%d.%d: `tool_use` ids were found without `tool_result` blocks immediately after: %s. Each `tool_use` block must have a corresponding `tool_result` block in the next message.",
							messageIndex, blockIndex, joinToolUseIDs(toolUseIDs))
					}
					value, err := block.intoText(path, "assistant")
					if err != nil {
						return nil, err
					}
					if content.Len() > 0 {
						content.WriteString("\n\n")
					}
					content.WriteString(value)
				case MessagesContentBlockToolUse:
					arguments, err := stringifyToolInput(block.Input)
					if err != nil {
						return nil, err
					}
					toolCalls = append(toolCalls, core.ToolCall{
						ID:        block.ID,
						Name:      block.Name,
						Arguments: arguments,
					})
					if _, duplicate := toolUseIDs[block.ID]; duplicate {
						return nil, request.BadRequestf(
							"messages.%d.content.%d: `tool_use` ids must be unique",
							messageIndex, blockIndex)
					}
					toolUseIDs[block.ID] = blockIndex
				case MessagesContentBlockToolResult:
					return nil, request.BadRequestf(
						"messages.%d: `tool_result` blocks can only be in `user` messages",
						messageIndex)
				case MessagesContentBlockThinking:
					if reasoningContent != nil {
						*reasoningContent += "\n\n" + block.Thinking
					} else {
						value := block.Thinking
						reasoningContent = &value
					}
				case MessagesContentBlockServerToolUse, MessagesContentBlockWebSearchToolResult:
					if options.MessagesWebSearch == request.WebSearchReject {
						return nil, request.BadRequest(
							"Unsupported content block: server tools are not supported")
					}
				default:
					return nil, unsupportedBlock()
				}
			}
			transformed = append(transformed, core.AssistantMessage(
				content.String(), derefString(reasoningContent), toolCalls))
		case MessagesRoleSystem:
			if err := checkToolUseResolved(messageIndex, toolUseIDs); err != nil {
				return nil, err
			}
			content, imageSources, err := message.SystemContent.intoContent(
				fmt.Sprintf("messages.%d", messageIndex))
			if err != nil {
				return nil, err
			}
			transformed = append(transformed, core.UserMessage(
				"<system-reminder>\n"+content+"\n</system-reminder>", imageSources))
		default:
			return nil, request.BadRequestf("unsupported message role: %s", message.Role)
		}
	}

	if len(toolUseIDs) > 0 {
		return nil, request.BadRequestf(
			"messages.%d:`tool_use` ids were found without `tool_result` blocks immediately after: %s. Each `tool_use` block must have a corresponding `tool_result` block in the next message.",
			len(messages)-1, joinToolUseIDs(toolUseIDs))
	}
	return transformed, nil
}

// checkToolUseResolved reports unresolved tool calls of a previous assistant
// message.
func checkToolUseResolved(messageIndex int, toolUseIDs map[string]int) error {
	if len(toolUseIDs) == 0 {
		return nil
	}
	return request.BadRequestf(
		"messages.%d: `tool_use` ids were found without `tool_result` blocks immediately after: %s. Each `tool_use` block must have a corresponding `tool_result` block in the next message.",
		messageIndex, joinToolUseIDs(toolUseIDs))
}

// joinToolUseIDs joins unresolved tool call identifiers in key order.
func joinToolUseIDs(toolUseIDs map[string]int) string {
	keys := make([]string, 0, len(toolUseIDs))
	for key := range toolUseIDs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// unsupportedBlock returns the error for an unrecognized content block.
func unsupportedBlock() error {
	return request.BadRequest("Unsupported content block")
}

// derefString returns the value or an empty string.
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// stringifyToolInput renders a tool input the way the prompt expects.
func stringifyToolInput(input json.RawMessage) (string, error) {
	value, err := jsonx.Parse(input)
	if err != nil {
		return "", request.Internalf("invalid tool input: %v", err)
	}
	return jsonx.MarshalPython(value)
}

// imageSource converts one Messages image reference.
func imageSource(source MessagesImageSource, path string) (core.ImageSource, error) {
	switch source.Type {
	case MessagesImageSourceBase64:
		return protocol.Base64ImageSource(source.MediaType, source.Data, path)
	case MessagesImageSourceURL:
		return protocol.ExternalImageSource(source.URL, nil, path)
	case MessagesImageSourceFile:
		return core.ImageSource{}, protocol.FileIDUnsupported(path)
	}
	return core.ImageSource{}, request.BadRequestf("%s: unsupported image source", path)
}

// intoContent converts text and tool references, substitutes document
// placeholders, and collects images with one image placeholder per image.
func (b *MessagesContentBlock) intoContent(path string) (string, []core.ImageSource, error) {
	switch b.Type {
	case MessagesContentBlockText:
		return b.Text, nil, nil
	case MessagesContentBlockToolReference:
		return b.ToolName, nil, nil
	case MessagesContentBlockImage:
		source, err := imageSource(*b.Source, path)
		if err != nil {
			return "", nil, err
		}
		return core.ImageSpecialToken, []core.ImageSource{source}, nil
	case MessagesContentBlockDocument:
		return protocol.UnsupportedDocumentPlaceholder, nil, nil
	default:
		return "", nil, request.Internal("unexpected content block type")
	}
}

// intoText converts textual content and document placeholders, rejecting
// images.
func (b *MessagesContentBlock) intoText(path, role string) (string, error) {
	content, imageSources, err := b.intoContent(path)
	if err != nil {
		return "", err
	}
	if len(imageSources) > 0 {
		return "", protocol.ImageNotAllowed(role)
	}
	return content, nil
}

// intoContent converts text and tool references, substitutes document
// placeholders, and collects images with one image placeholder per image.
func (b *MessagesTextBlock) intoContent(path string) (string, []core.ImageSource, error) {
	switch b.Type {
	case MessagesTextBlockText:
		return b.Text, nil, nil
	case MessagesTextBlockToolReference:
		return b.ToolName, nil, nil
	case MessagesTextBlockImage:
		source, err := imageSource(*b.Source, path)
		if err != nil {
			return "", nil, err
		}
		return core.ImageSpecialToken, []core.ImageSource{source}, nil
	case MessagesTextBlockDocument:
		return protocol.UnsupportedDocumentPlaceholder, nil, nil
	default:
		return "", nil, unsupportedBlock()
	}
}

// intoContent joins textual content and document placeholders, collecting
// image sources.
func (c *MessagesTextOrTextBlocks) intoContent(path string) (string, []core.ImageSource, error) {
	if c == nil {
		return "", nil, nil
	}
	if c.IsString {
		return c.Text, nil, nil
	}
	texts := make([]string, 0, len(c.Blocks))
	var imageSources []core.ImageSource
	for blockIndex := range c.Blocks {
		block := &c.Blocks[blockIndex]
		text, images, err := block.intoContent(fmt.Sprintf("%s[%d]", path, blockIndex))
		if err != nil {
			return "", nil, err
		}
		texts = append(texts, text)
		imageSources = append(imageSources, images...)
	}
	return strings.Join(texts, "\n\n"), imageSources, nil
}

// intoText joins textual content and document placeholders, rejecting images.
func (c *MessagesTextOrTextBlocks) intoText(path, role string) (string, error) {
	content, imageSources, err := c.intoContent(path)
	if err != nil {
		return "", err
	}
	if len(imageSources) > 0 {
		return "", protocol.ImageNotAllowed(role)
	}
	return content, nil
}

// messagesContainImageSpecialToken reports whether any message text or
// top-level system text carries the image placeholder.
//
// Message text, tool references, thinking blocks, and tool result text are
// checked. Tool call names and arguments are checked after conversion by
// protocol.ValidateToolText.
func messagesContainImageSpecialToken(messages []MessagesMessage, system *MessagesTextOrTextBlocks) bool {
	for messageIndex := range messages {
		message := &messages[messageIndex]
		switch message.Role {
		case MessagesRoleUser, MessagesRoleAssistant:
			if message.Content.containsImageSpecialToken() {
				return true
			}
		case MessagesRoleSystem:
			if message.SystemContent.containsImageSpecialToken() {
				return true
			}
		}
	}
	return system.containsImageSpecialToken()
}

// containsImageSpecialToken reports whether one message content carries the
// image placeholder.
func (c *MessagesContent) containsImageSpecialToken() bool {
	if c == nil {
		return false
	}
	if c.IsString {
		return strings.Contains(c.Text, core.ImageSpecialToken)
	}
	for blockIndex := range c.Blocks {
		if c.Blocks[blockIndex].containsImageSpecialToken() {
			return true
		}
	}
	return false
}

// containsImageSpecialToken reports whether one content block carries the image
// placeholder.
func (b *MessagesContentBlock) containsImageSpecialToken() bool {
	switch b.Type {
	case MessagesContentBlockText:
		return strings.Contains(b.Text, core.ImageSpecialToken)
	case MessagesContentBlockToolReference:
		return strings.Contains(b.ToolName, core.ImageSpecialToken)
	case MessagesContentBlockThinking:
		return strings.Contains(b.Thinking, core.ImageSpecialToken)
	case MessagesContentBlockToolResult:
		return b.Content.containsImageSpecialToken()
	}
	return false
}

// containsImageSpecialToken reports whether one text-or-text-blocks value
// carries the image placeholder.
func (c *MessagesTextOrTextBlocks) containsImageSpecialToken() bool {
	if c == nil {
		return false
	}
	if c.IsString {
		return strings.Contains(c.Text, core.ImageSpecialToken)
	}
	for blockIndex := range c.Blocks {
		block := &c.Blocks[blockIndex]
		switch block.Type {
		case MessagesTextBlockText:
			if strings.Contains(block.Text, core.ImageSpecialToken) {
				return true
			}
		case MessagesTextBlockToolReference:
			if strings.Contains(block.ToolName, core.ImageSpecialToken) {
				return true
			}
		}
	}
	return false
}
