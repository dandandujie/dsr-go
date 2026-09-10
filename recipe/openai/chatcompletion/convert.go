package chatcompletion

import (
	"fmt"
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
func (r *ChatCompletionRequest) Convert(options request.ConversionOptions) (*request.ConversationRequest, error) {
	thinkingMode, reasoningEffort := r.resolveThinking(options)
	parsingOptions := r.parsingOptions(thinkingMode)
	inferenceOptions, err := r.inferenceOptions()
	if err != nil {
		return nil, err
	}
	if err := validateAPIParams(r.Stop, r.Stream, r.StreamOptions); err != nil {
		return nil, err
	}
	tools, toolChoice, err := convertTools(r.Tools, r.ToolChoice, thinkingMode)
	if err != nil {
		return nil, err
	}
	parsingOptions.ParseToolCalls = len(tools) > 0 && toolChoice != core.ToolChoiceNone
	parsingOptions.ToolCallInitialStage = toolChoice == core.ToolChoiceRequired
	messages, err := transformMessages(r.Messages)
	if err != nil {
		return nil, err
	}
	if err := protocol.ValidateToolText(tools, messages); err != nil {
		return nil, err
	}
	responseFormat, err := convertResponseFormat(messages, r.ResponseFormat)
	if err != nil {
		return nil, err
	}
	parsingOptions.ParseJSONOutput = responseFormat == core.ResponseFormatJSONObject

	conversation := &core.Conversation{
		Messages:        messages,
		ThinkingMode:    thinkingMode,
		Tools:           tools,
		ToolChoice:      toolChoice,
		ReasoningEffort: reasoningEffort,
		ResponseFormat:  responseFormat,
	}
	streaming := r.Stream != nil && *r.Stream
	converted := &request.ConversationRequest{
		Conversation:     conversation,
		InferenceOptions: inferenceOptions,
		ParsingOptions:   parsingOptions,
		Model:            &r.Model,
		Stream:           streaming,
	}
	converted.NewChunkGenerator = func(id, model string) stream.Generator {
		return NewChunkGenerator(id, model, !streaming, thinkingMode)
	}
	return converted, nil
}

// resolveThinking returns the resolved thinking mode and reasoning effort.
func (r *ChatCompletionRequest) resolveThinking(options request.ConversionOptions) (bool, *core.ReasoningEffort) {
	effort := toReasoningEffort(r.ReasoningEffort)
	var thinking bool
	switch {
	case r.Thinking != nil && r.Thinking.Type == ThinkingEnabled:
		thinking = true
	case r.Thinking != nil && r.Thinking.Type == ThinkingDisabled:
		thinking = false
	case r.ReasoningEffort != nil && *r.ReasoningEffort == ReasoningEffortNone:
		thinking = false
	case r.ReasoningEffort != nil:
		thinking = true
	default:
		thinking = options.DefaultThinkingMode
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

// inferenceOptions validates and extracts the inference parameters.
func (r *ChatCompletionRequest) inferenceOptions() (request.InferenceOptions, error) {
	if r.N != nil && *r.N != 1 {
		return request.InferenceOptions{}, request.BadRequest(
			"Invalid n value (currently only n = 1 is supported)")
	}
	for _, penalty := range []struct {
		name  string
		value *float32
	}{
		{"frequency_penalty", r.FrequencyPenalty},
		{"presence_penalty", r.PresencePenalty},
	} {
		if penalty.value != nil && (*penalty.value < -2 || *penalty.value > 2) {
			return request.InferenceOptions{}, request.BadRequestf("%s must be in [-2, 2]", penalty.name)
		}
	}
	if err := protocol.ValidateMaxTokens(r.MaxTokens, "max_tokens"); err != nil {
		return request.InferenceOptions{}, err
	}
	if err := protocol.ValidateSampling(r.Temperature, r.TopP); err != nil {
		return request.InferenceOptions{}, err
	}
	if r.Seed != nil && *r.Seed >= 1<<63 {
		return request.InferenceOptions{}, request.BadRequest("seed must be in [0, 2^63)")
	}
	return request.InferenceOptions{
		MaxTokens:   r.MaxTokens,
		Temperature: r.Temperature,
		TopP:        r.TopP,
	}, nil
}

// parsingOptions builds the output parsing options of this request.
func (r *ChatCompletionRequest) parsingOptions(thinking bool) stream.ParsingOptions {
	options := stream.DefaultParsingOptions()
	if thinking {
		stage := stream.ReasoningStageStart
		options.ReasoningInitialStage = &stage
	}
	if r.Stop != nil {
		options.StopSequences = append([]string{}, r.Stop.Values...)
	}
	return options
}

// IncludeUsage returns the setting for usage inclusion in response chunks.
//
// Read this before consuming the request with Convert, then pass the value to
// ChatCompletionChunkGenerator.WithIncludeUsage.
func (r *ChatCompletionRequest) IncludeUsage() bool {
	if r.Stream == nil || !*r.Stream {
		return true
	}
	return r.StreamOptions != nil && r.StreamOptions.IncludeUsage != nil && *r.StreamOptions.IncludeUsage
}

func convertTools(tools []ChatCompletionTool, choice *ChatCompletionToolChoiceOption, thinking bool) ([]core.ToolDefinition, core.ToolChoice, error) {
	for index, tool := range tools {
		if err := protocol.ValidateToolName(tool.Function.Name, fmt.Sprintf("tools.%d.function.name", index)); err != nil {
			return nil, core.ToolChoiceAuto, err
		}
	}
	if choice != nil && choice.Mode != nil && *choice.Mode == ToolChoiceModeNone {
		return []core.ToolDefinition{}, core.ToolChoiceNone, nil
	}
	if len(tools) == 0 {
		return []core.ToolDefinition{}, core.ToolChoiceAuto, nil
	}

	names := make(map[string]struct{}, len(tools))
	definitions := make([]core.ToolDefinition, 0, len(tools))
	for index, tool := range tools {
		function := tool.Function
		if _, exists := names[function.Name]; exists {
			return nil, core.ToolChoiceAuto, request.BadRequest("Tool names must be unique")
		}
		names[function.Name] = struct{}{}
		parameters := jsonx.Value(jsonx.NewObject())
		if function.Parameters != nil {
			parsed, err := jsonx.Parse(function.Parameters)
			if err != nil {
				return nil, core.ToolChoiceAuto, request.BadRequestf(
					"tools.%d.function.parameters: invalid JSON Schema: %v", index, err)
			}
			parameters = parsed
			if err := protocol.ValidateToolParameters(parameters, fmt.Sprintf("tools.%d.function.parameters", index)); err != nil {
				return nil, core.ToolChoiceAuto, err
			}
		}
		definitions = append(definitions, core.ToolDefinition{
			Name:        function.Name,
			Description: function.Description,
			Parameters:  parameters,
			Strict:      function.Strict,
		})
	}
	resolved := core.ToolChoiceAuto
	switch {
	case choice != nil && choice.Named != nil:
		name := choice.Named.Function.Name
		if _, ok := names[name]; !ok {
			return nil, core.ToolChoiceAuto, request.BadRequestf("tool_choice: no tool named '%s' was specified", name)
		}
		kept := definitions[:0]
		for _, tool := range definitions {
			if tool.Name == name {
				kept = append(kept, tool)
			}
		}
		definitions = kept
		resolved = core.ToolChoiceRequired
	case choice != nil && choice.Mode != nil && *choice.Mode == ToolChoiceModeRequired:
		resolved = core.ToolChoiceRequired
	}
	if resolved == core.ToolChoiceRequired && thinking {
		return nil, core.ToolChoiceAuto, request.BadRequest("Thinking mode does not support this tool_choice")
	}
	return definitions, resolved, nil
}

func toReasoningEffort(effort *ChatCompletionReasoningEffort) *core.ReasoningEffort {
	if effort == nil {
		return nil
	}
	value := func(level core.ReasoningEffort) *core.ReasoningEffort { return &level }
	switch *effort {
	case ReasoningEffortNone:
		return nil
	case ReasoningEffortMinimal, ReasoningEffortLow:
		return value(core.ReasoningEffortLow)
	case ReasoningEffortMedium, ReasoningEffortHigh:
		return value(core.ReasoningEffortHigh)
	case ReasoningEffortXhigh:
		return value(core.ReasoningEffortXhigh)
	case ReasoningEffortMax:
		return value(core.ReasoningEffortMax)
	}
	return nil
}

// containsImageSpecialToken reports whether text carries the image placeholder.
func (c *ChatCompletionRequestContent) containsImageSpecialToken() bool {
	if c == nil {
		return false
	}
	if c.IsString {
		return strings.Contains(c.Text, core.ImageSpecialToken)
	}
	for _, block := range c.Blocks {
		if block.Type == ContentBlockText && strings.Contains(block.Text, core.ImageSpecialToken) {
			return true
		}
	}
	return false
}

// intoContent converts text and image blocks. Each image block contributes one
// image placeholder in the returned text.
func (c *ChatCompletionRequestContent) intoContent(path string) (string, []core.ImageSource, error) {
	if c == nil {
		return "", nil, nil
	}
	if c.IsString {
		return c.Text, nil, nil
	}
	texts := make([]string, 0, len(c.Blocks))
	var imageSources []core.ImageSource
	for blockIndex, block := range c.Blocks {
		switch block.Type {
		case ContentBlockText:
			texts = append(texts, block.Text)
		case ContentBlockImageURL:
			blockPath := fmt.Sprintf("%s.content[%d].image_url.url", path, blockIndex)
			source, err := protocol.ImageURLSource(block.ImageURL.URL, block.ImageURL.Detail, blockPath)
			if err != nil {
				return "", nil, err
			}
			imageSources = append(imageSources, source)
			texts = append(texts, core.ImageSpecialToken)
		case ContentBlockFile:
			if block.FileID != nil {
				return "", nil, protocol.FileIDUnsupported(fmt.Sprintf("%s.content[%d]", path, blockIndex))
			}
			if block.FileData == nil {
				return "", nil, request.BadRequestf("%s.content[%d]: file must have file_data", path, blockIndex)
			}
			imageSources = append(imageSources, protocol.DataURLImageSource(*block.FileData, nil))
			texts = append(texts, core.ImageSpecialToken)
		}
	}
	return strings.Join(texts, "\n\n"), imageSources, nil
}

// intoText converts text blocks and rejects image blocks.
func (c *ChatCompletionRequestContent) intoText(path, role string) (string, error) {
	content, imageSources, err := c.intoContent(path)
	if err != nil {
		return "", err
	}
	if len(imageSources) > 0 {
		return "", protocol.ImageNotAllowed(role)
	}
	return content, nil
}

// messagesContainImageSpecialToken reports whether any message carries the
// image placeholder in text that reaches the prompt.
func messagesContainImageSpecialToken(messages []ChatCompletionRequestMessage) bool {
	for _, message := range messages {
		switch message.Role {
		case RoleSystem, RoleUser, RoleTool:
			if message.Content.containsImageSpecialToken() {
				return true
			}
		case RoleAssistant:
			if message.Content.containsImageSpecialToken() {
				return true
			}
			if message.ReasoningContent != nil && strings.Contains(*message.ReasoningContent, core.ImageSpecialToken) {
				return true
			}
		case RoleLatestReminder:
			if message.Content != nil && strings.Contains(message.Content.Text, core.ImageSpecialToken) {
				return true
			}
		}
	}
	return false
}

func transformMessages(messages []ChatCompletionRequestMessage) ([]core.InputMessage, error) {
	if len(messages) == 0 {
		return nil, request.BadRequest("Empty input messages")
	}
	if messagesContainImageSpecialToken(messages) {
		return nil, protocol.ImageSpecialTokenNotAllowed()
	}

	result := make([]core.InputMessage, 0, len(messages))
	for index := 0; index < len(messages); index++ {
		message := messages[index]
		path := fmt.Sprintf("messages[%d]", index)
		switch message.Role {
		case RoleSystem:
			content, err := message.Content.intoText(path, "system")
			if err != nil {
				return nil, err
			}
			if content != "" {
				result = append(result, core.SystemMessage(content))
			}
		case RoleUser:
			content, imageSources, err := message.Content.intoContent(path)
			if err != nil {
				return nil, err
			}
			result = append(result, core.UserMessage(content, imageSources))
		case RoleAssistant:
			if message.Content == nil && message.ToolCalls == nil {
				return nil, request.BadRequest("Invalid assistant message: content or tool_calls must be set")
			}
			content := ""
			if message.Content != nil {
				var err error
				content, err = message.Content.intoText(path, "assistant")
				if err != nil {
					return nil, err
				}
			}
			var toolResults []core.InputMessage
			var inputToolCalls []core.ToolCall
			if message.ToolCalls != nil {
				count := len(message.ToolCalls)
				if count == 0 {
					return nil, request.BadRequestf(
						"Invalid '%s.tool_calls': empty array. Expected an array with minimum length 1, but got an empty array instead.",
						path)
				}
				toolIDs := make(map[string]struct{}, count)
				for _, toolCall := range message.ToolCalls {
					if _, exists := toolIDs[toolCall.ID]; exists {
						return nil, request.BadRequestf(
							"Duplicate value for 'tool_call_id' of %s in message[%d]", toolCall.ID, index)
					}
					toolIDs[toolCall.ID] = struct{}{}
				}

				// The assistant message is followed by one tool message per call.
				checkedToolCallIDs := make(map[string]struct{}, count)
				for i := 0; i < count; i++ {
					index++
					if index >= len(messages) || messages[index].Role != RoleTool {
						return nil, request.BadRequest(
							"An assistant message with 'tool_calls' must be followed by tool messages responding to each 'tool_call_id'. (insufficient tool messages following tool_calls message)")
					}
					toolResult := messages[index]
					toolCallID := toolResult.ToolCallID
					if _, ok := toolIDs[toolCallID]; !ok {
						return nil, request.BadRequestf(
							"An assistant message with 'tool_calls' must be followed by tool messages responding to each 'tool_call_id'. Unexpected tool_call_id: %s", toolCallID)
					}
					if _, exists := checkedToolCallIDs[toolCallID]; exists {
						return nil, request.BadRequestf(
							"Duplicate value for 'tool_call_id' of %s in message[%d]", toolCallID, index)
					}
					checkedToolCallIDs[toolCallID] = struct{}{}
					toolContent, imageSources, err := toolResult.Content.intoContent(fmt.Sprintf("messages[%d].content", index))
					if err != nil {
						return nil, err
					}
					toolResults = append(toolResults, core.ToolMessage(toolContent, imageSources, toolCallID))
				}
				inputToolCalls = make([]core.ToolCall, 0, count)
				for _, toolCall := range message.ToolCalls {
					inputToolCalls = append(inputToolCalls, core.ToolCall{
						ID:        toolCall.ID,
						Name:      toolCall.Function.Name,
						Arguments: toolCall.Function.Arguments,
					})
				}
			}
			result = append(result, core.AssistantMessage(content, derefString(message.ReasoningContent), inputToolCalls))
			result = append(result, toolResults...)
		case RoleTool:
			// Tool results must follow an assistant tool call.
			return nil, request.BadRequest(
				"Messages with role 'tool' must be a response to a preceding message with 'tool_calls'")
		case RoleLatestReminder:
			result = append(result, core.LatestReminderMessage(message.Content.Text))
		default:
			return nil, request.BadRequestf("unsupported message role: %s", message.Role)
		}
	}
	return result, nil
}

func convertResponseFormat(messages []core.InputMessage, responseFormat *ChatCompletionResponseFormat) (core.ResponseFormat, error) {
	if responseFormat == nil {
		return core.ResponseFormatText, nil
	}
	switch responseFormat.Type {
	case ResponseFormatJSONObject:
		if !protocol.ContainsJSONInstruction(messages) {
			return core.ResponseFormatText, request.BadRequest(
				"Prompt must contain the word 'json' in some form to use 'response_format' of type 'json_object'.")
		}
		return core.ResponseFormatJSONObject, nil
	case ResponseFormatRegex:
		return core.ResponseFormatText, request.BadRequest(
			"response_format: only text and json_object are supported")
	default:
		return core.ResponseFormatText, nil
	}
}

// validateAPIParams validates stop sequences and streaming options.
func validateAPIParams(stop *ChatCompletionStopSequences, stream *bool, streamOptions *ChatCompletionStreamOptions) error {
	if stop != nil && stop.IsArray && len(stop.Values) > maxStopSequences {
		return request.BadRequestf("ChatCompletionStopSequences string array too long: %d", len(stop.Values))
	}
	if (stream == nil || !*stream) && streamOptions != nil {
		return request.BadRequest("stream_options should be set along with stream = true")
	}
	return nil
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
