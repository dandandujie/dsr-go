package responses

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/internal/protocol"
	"github.com/dandandujie/dsr-go/recipe/request"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// Compile-time check that the request implements the protocol contract.
var _ request.ProtocolRequest = (*ResponsesRequest)(nil)

// CustomToolNames returns the custom tool names declared in the request tools.
//
// Read this before Convert and pass it to
// ResponsesChunkGenerator.WithCustomToolNames, so custom tool input strings are
// decoded during streaming.
func (r *ResponsesRequest) CustomToolNames() []string {
	names := []string{}
	seen := make(map[string]struct{})
	for _, tool := range r.Tools {
		if tool.Type != ResponsesToolTypeCustom {
			continue
		}
		if _, ok := seen[tool.Name]; ok {
			continue
		}
		seen[tool.Name] = struct{}{}
		names = append(names, tool.Name)
	}
	return names
}

// Convert validates and converts the request into a conversation request.
func (r *ResponsesRequest) Convert(options request.ConversionOptions) (*request.ConversationRequest, error) {
	if containsImageSpecialToken(r.Input, r.Instructions) {
		return nil, protocol.ImageSpecialTokenNotAllowed()
	}
	thinkingMode, reasoningEffort := r.resolveThinking(options)
	inferenceOptions, err := r.inferenceOptions()
	if err != nil {
		return nil, err
	}
	convertedTools, err := convertResponsesTools(r.Tools, options)
	if err != nil {
		return nil, err
	}
	messages := []core.InputMessage{}
	if r.Instructions != nil && *r.Instructions != "" {
		messages = append(messages, core.SystemMessage(*r.Instructions))
	}
	switch {
	case r.Input == nil:
		if len(messages) == 0 {
			return nil, request.BadRequest("Either input or instructions must be provided")
		}
	case r.Input.IsString:
		messages = append(messages, core.UserMessage(r.Input.Text, nil))
	default:
		if err := transformInputItems(r.Input.Items, &messages, convertedTools); err != nil {
			return nil, err
		}
	}
	tools, toolChoice, err := convertedTools.selectTools(r.ToolChoice, thinkingMode, options)
	if err != nil {
		return nil, err
	}
	if err := protocol.ValidateToolText(tools, messages); err != nil {
		return nil, err
	}
	responseFormat, err := convertTextFormat(r.Text, messages)
	if err != nil {
		return nil, err
	}
	parsingOptions := stream.ParsingOptions{
		ParseToolCalls:       len(tools) > 0,
		ToolCallInitialStage: toolChoice == core.ToolChoiceRequired,
		ParseJSONOutput:      responseFormat == core.ResponseFormatJSONObject,
	}
	if thinkingMode {
		stage := stream.ReasoningStageStart
		parsingOptions.ReasoningInitialStage = &stage
	}
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
		return NewChunkGenerator(id, model)
	}
	return converted, nil
}

// resolveThinking returns the resolved thinking mode and reasoning effort.
func (r *ResponsesRequest) resolveThinking(options request.ConversionOptions) (bool, *core.ReasoningEffort) {
	var effort *ResponsesReasoningEffort
	if r.Reasoning != nil {
		effort = r.Reasoning.Effort
	}
	value := func(level core.ReasoningEffort) *core.ReasoningEffort { return &level }
	if effort != nil {
		switch *effort {
		case ResponsesReasoningEffortNone:
			return false, nil
		case ResponsesReasoningEffortMinimal, ResponsesReasoningEffortLow:
			return true, value(core.ReasoningEffortLow)
		case ResponsesReasoningEffortMedium, ResponsesReasoningEffortHigh:
			return true, value(core.ReasoningEffortHigh)
		case ResponsesReasoningEffortXhigh:
			return true, value(core.ReasoningEffortXhigh)
		case ResponsesReasoningEffortMax:
			return true, value(core.ReasoningEffortMax)
		}
	}
	if !options.DefaultThinkingMode {
		return false, nil
	}
	return true, value(core.ReasoningEffortHigh)
}

// inferenceOptions validates and extracts the inference parameters.
func (r *ResponsesRequest) inferenceOptions() (request.InferenceOptions, error) {
	if err := protocol.ValidateMaxTokens(r.MaxOutputTokens, "max_output_tokens"); err != nil {
		return request.InferenceOptions{}, err
	}
	if err := protocol.ValidateSampling(r.Temperature, r.TopP); err != nil {
		return request.InferenceOptions{}, err
	}
	return request.InferenceOptions{
		MaxTokens:   r.MaxOutputTokens,
		Temperature: r.Temperature,
		TopP:        r.TopP,
	}, nil
}

// convertTextFormat resolves the requested answer format.
func convertTextFormat(text *ResponsesTextConfig, messages []core.InputMessage) (core.ResponseFormat, error) {
	format := ResponsesTextFormatText
	if text != nil && text.Format.Type != "" {
		format = text.Format.Type
	}
	if format != ResponsesTextFormatJSONObject {
		return core.ResponseFormatText, nil
	}
	if !protocol.ContainsJSONInstruction(messages) {
		return core.ResponseFormatText, request.BadRequest(
			"Response input messages must contain the word 'json' to use text.format of type json_object")
	}
	return core.ResponseFormatJSONObject, nil
}

// unsupportedContent is the error for a content block type that is not listed
// in the schema.
func unsupportedContent() error {
	return request.BadRequest("Unsupported content block")
}

// containsImageSpecialToken reports whether the input or instructions carry the
// image placeholder.
//
// Message text, tool output text, and reasoning text are checked. Tool call
// names and arguments are checked after conversion by ValidateToolText.
func containsImageSpecialToken(input *ResponsesInput, instructions *string) bool {
	if instructions != nil && strings.Contains(*instructions, core.ImageSpecialToken) {
		return true
	}
	if input == nil {
		return false
	}
	if input.IsString {
		return strings.Contains(input.Text, core.ImageSpecialToken)
	}
	for _, item := range input.Items {
		if itemContainsImageSpecialToken(item) {
			return true
		}
	}
	return false
}

// itemContainsImageSpecialToken reports whether one input item carries the
// image placeholder.
func itemContainsImageSpecialToken(item ResponsesInputItem) bool {
	if item.Typed != nil {
		switch item.Typed.Kind {
		case ResponsesTypedInputItemMessage:
			return item.Typed.Message != nil &&
				messageContentContainsImageSpecialToken(item.Typed.Message.Content)
		case ResponsesTypedInputItemFunctionCallOutput:
			return item.Typed.FunctionCallOutput != nil &&
				outputContentContainsImageSpecialToken(item.Typed.FunctionCallOutput.Output)
		case ResponsesTypedInputItemCustomToolCallOutput:
			return item.Typed.CustomToolCallOutput != nil && item.Typed.CustomToolCallOutput.Output != nil &&
				outputContentContainsImageSpecialToken(*item.Typed.CustomToolCallOutput.Output)
		case ResponsesTypedInputItemReasoning:
			if item.Typed.Reasoning == nil {
				return false
			}
			for _, block := range item.Typed.Reasoning.Content {
				if strings.Contains(block.Text, core.ImageSpecialToken) {
					return true
				}
			}
		}
		return false
	}
	return item.Message != nil && messageContentContainsImageSpecialToken(item.Message.Content)
}

// messageContentContainsImageSpecialToken reports whether one message content
// carries the image placeholder.
func messageContentContainsImageSpecialToken(content ResponsesMessageContent) bool {
	if content.IsString {
		return strings.Contains(content.Text, core.ImageSpecialToken)
	}
	for _, block := range content.Blocks {
		switch block.Type {
		case ResponsesContentBlockInputText, ResponsesContentBlockOutputText:
			if strings.Contains(block.Text, core.ImageSpecialToken) {
				return true
			}
		}
	}
	return false
}

// outputContentContainsImageSpecialToken reports whether one tool call output
// carries the image placeholder.
func outputContentContainsImageSpecialToken(content ResponsesToolCallOutputContent) bool {
	if content.IsString {
		return strings.Contains(content.Text, core.ImageSpecialToken)
	}
	for _, item := range content.Items {
		if item.Type == ResponsesContentBlockInputText &&
			strings.Contains(item.Text, core.ImageSpecialToken) {
			return true
		}
	}
	return false
}

// intoContent converts text, substitutes document placeholders, and collects
// images with one image placeholder per image.
func (c ResponsesMessageContent) intoContent(path string) (string, []core.ImageSource, error) {
	if c.IsString {
		return c.Text, nil, nil
	}
	texts := make([]string, 0, len(c.Blocks))
	var imageSources []core.ImageSource
	for blockIndex, block := range c.Blocks {
		blockPath := fmt.Sprintf("%s.content[%d]", path, blockIndex)
		switch block.Type {
		case ResponsesContentBlockInputText, ResponsesContentBlockOutputText:
			texts = append(texts, block.Text)
		case ResponsesContentBlockInputImage:
			image := ResponsesInputImage{}
			if block.Image != nil {
				image = *block.Image
			}
			source, err := inputImageSource(image, blockPath)
			if err != nil {
				return "", nil, err
			}
			imageSources = append(imageSources, source)
			texts = append(texts, core.ImageSpecialToken)
		case ResponsesContentBlockInputFile:
			texts = append(texts, protocol.UnsupportedDocumentPlaceholder)
		default:
			return "", nil, unsupportedContent()
		}
	}
	return strings.Join(texts, "\n\n"), imageSources, nil
}

// intoText converts text and document placeholders, rejecting images.
func (c ResponsesMessageContent) intoText(path, role string) (string, error) {
	content, imageSources, err := c.intoContent(path)
	if err != nil {
		return "", err
	}
	if len(imageSources) > 0 {
		return "", protocol.ImageNotAllowed(role)
	}
	return content, nil
}

// intoContent converts text and document placeholders, collecting image
// sources.
func (c ResponsesToolCallOutputContent) intoContent(path string) (string, []core.ImageSource, error) {
	if c.IsString {
		return c.Text, nil, nil
	}
	texts := make([]string, 0, len(c.Items))
	var imageSources []core.ImageSource
	for itemIndex, item := range c.Items {
		itemPath := fmt.Sprintf("%s[%d]", path, itemIndex)
		switch item.Type {
		case ResponsesContentBlockInputText:
			texts = append(texts, item.Text)
		case ResponsesContentBlockInputImage:
			image := ResponsesInputImage{}
			if item.Image != nil {
				image = *item.Image
			}
			source, err := inputImageSource(image, itemPath)
			if err != nil {
				return "", nil, err
			}
			imageSources = append(imageSources, source)
			texts = append(texts, core.ImageSpecialToken)
		case ResponsesContentBlockInputFile:
			texts = append(texts, protocol.UnsupportedDocumentPlaceholder)
		default:
			return "", nil, unsupportedContent()
		}
	}
	return strings.Join(texts, "\n\n"), imageSources, nil
}

// inputImageSource converts one image reference.
func inputImageSource(image ResponsesInputImage, path string) (core.ImageSource, error) {
	imageURL := ""
	hasURL := false
	if image.ImageURL != nil && *image.ImageURL != "" {
		imageURL = *image.ImageURL
		hasURL = true
	}
	switch {
	case !hasURL && image.FileID == nil:
		return core.ImageSource{}, request.BadRequestf(
			"%s: input_image must have image_url or file_id", path)
	case hasURL && image.FileID != nil:
		return core.ImageSource{}, request.BadRequestf(
			"%s: input_image cannot have both image_url and file_id", path)
	case !hasURL:
		return core.ImageSource{}, protocol.FileIDUnsupported(path)
	default:
		return protocol.ImageURLSource(imageURL, image.Detail, path+".image_url")
	}
}

// responsesCallIDs is an insertion-ordered set of tool call IDs.
//
// Pending calls are reported in insertion order, which keeps the error message
// deterministic when more than one call is unresolved.
type responsesCallIDs struct {
	order []string
	index map[string]struct{}
}

// newResponsesCallIDs returns an empty call ID set.
func newResponsesCallIDs() *responsesCallIDs {
	return &responsesCallIDs{index: make(map[string]struct{})}
}

// contains reports whether id is in the set.
func (s *responsesCallIDs) contains(id string) bool {
	_, ok := s.index[id]
	return ok
}

// add inserts id when it is not already present.
func (s *responsesCallIDs) add(id string) {
	if s.contains(id) {
		return
	}
	s.index[id] = struct{}{}
	s.order = append(s.order, id)
}

// remove deletes id from the set.
func (s *responsesCallIDs) remove(id string) {
	delete(s.index, id)
}

// first returns the earliest inserted member.
func (s *responsesCallIDs) first() (string, bool) {
	for _, id := range s.order {
		if s.contains(id) {
			return id, true
		}
	}
	return "", false
}

// checkToolCallID validates the call ID of a new tool call.
func checkToolCallID(itemIndex int, callID string, pending, resolved *responsesCallIDs) error {
	if callID == "" {
		return request.BadRequestf(
			"Invalid 'input[%d].call_id': empty string. Expected a string with minimum length 1, but got an empty string instead.",
			itemIndex)
	}
	if pending.contains(callID) || resolved.contains(callID) {
		return request.BadRequestf("Duplicate 'call_id': %s.", callID)
	}
	return nil
}

// checkToolOutputID validates the call ID of a tool output and marks the call
// resolved.
func checkToolOutputID(itemIndex int, callID string, pending, resolved *responsesCallIDs) error {
	if callID == "" {
		return request.BadRequestf(
			"Invalid 'input[%d].call_id': empty string. Expected a string with minimum length 1, but got an empty string instead.",
			itemIndex)
	}
	if !pending.contains(callID) {
		if resolved.contains(callID) {
			return request.BadRequestf("Duplicate tool output for call_id: %s.", callID)
		}
		return request.BadRequestf("No tool call found for tool output with call_id %s.", callID)
	}
	pending.remove(callID)
	resolved.add(callID)
	return nil
}

// checkPendingCalls rejects unresolved tool calls.
func checkPendingCalls(pending *responsesCallIDs) error {
	if callID, ok := pending.first(); ok {
		return request.BadRequestf("No tool output found for tool call %s.", callID)
	}
	return nil
}

// responsesToolResult is the converted output of one tool call.
type responsesToolResult struct {
	content      string
	imageSources []core.ImageSource
}

// responsesToolCallGroup collects the calls and outputs of one assistant turn.
type responsesToolCallGroup struct {
	calls   []core.ToolCall
	outputs map[string]responsesToolResult
}

// newResponsesToolCallGroup returns an empty call group.
func newResponsesToolCallGroup() *responsesToolCallGroup {
	return &responsesToolCallGroup{outputs: make(map[string]responsesToolResult)}
}

// isComplete reports whether every call has an output.
func (g *responsesToolCallGroup) isComplete() bool {
	return len(g.outputs) >= len(g.calls)
}

// appendMessages appends the assistant tool call message and its tool results.
func (g *responsesToolCallGroup) appendMessages(messages *[]core.InputMessage) {
	if len(g.calls) == 0 {
		return
	}
	calls := g.calls
	sort.Slice(calls, func(i, j int) bool { return calls[i].ID < calls[j].ID })
	toolResults := make([]core.InputMessage, 0, len(calls))
	for _, call := range calls {
		output, ok := g.outputs[call.ID]
		if !ok {
			continue
		}
		delete(g.outputs, call.ID)
		toolResults = append(toolResults, core.ToolMessage(output.content, output.imageSources, call.ID))
	}
	if len(*messages) == 0 || (*messages)[len(*messages)-1].Kind != core.MessageAssistant {
		*messages = append(*messages, core.AssistantMessage("", "", nil))
	}
	last := &(*messages)[len(*messages)-1]
	if last.ToolCalls == nil {
		last.ToolCalls = calls
	} else {
		last.ToolCalls = append(last.ToolCalls, calls...)
	}
	*messages = append(*messages, toolResults...)
}

// pushCallToGroup appends one call to the pending group.
func pushCallToGroup(pendingGroup **responsesToolCallGroup, toolCall core.ToolCall) {
	if *pendingGroup == nil {
		*pendingGroup = newResponsesToolCallGroup()
	}
	(*pendingGroup).calls = append((*pendingGroup).calls, toolCall)
}

// pushOutputToGroup records one output and appends the group when complete.
func pushOutputToGroup(
	pendingGroup **responsesToolCallGroup,
	callID string,
	output responsesToolResult,
	messages *[]core.InputMessage,
) {
	if *pendingGroup == nil {
		*messages = append(*messages, core.ToolMessage(output.content, output.imageSources, callID))
		return
	}
	(*pendingGroup).outputs[callID] = output
	if (*pendingGroup).isComplete() {
		appendToolCallGroup(pendingGroup, messages)
	}
}

// appendToolCallGroup appends the pending group and clears it.
func appendToolCallGroup(pendingGroup **responsesToolCallGroup, messages *[]core.InputMessage) {
	if *pendingGroup != nil {
		(*pendingGroup).appendMessages(messages)
		*pendingGroup = nil
	}
}

// pushMessage converts one input message.
func pushMessage(message ResponsesInputMessage, itemIndex int, messages *[]core.InputMessage) error {
	path := fmt.Sprintf("input[%d]", itemIndex)
	switch message.Role {
	case ResponsesMessageRoleSystem:
		content, err := message.Content.intoText(path, "system")
		if err != nil {
			return err
		}
		*messages = append(*messages, core.SystemMessage(content))
	case ResponsesMessageRoleUser, ResponsesMessageRoleDeveloper:
		content, imageSources, err := message.Content.intoContent(path)
		if err != nil {
			return err
		}
		*messages = append(*messages, core.UserMessage(content, imageSources))
	case ResponsesMessageRoleAssistant:
		content, err := message.Content.intoText(path, "assistant")
		if err != nil {
			return err
		}
		if len(*messages) > 0 {
			last := &(*messages)[len(*messages)-1]
			if last.Kind == core.MessageAssistant && last.ToolCalls == nil {
				if content != "" {
					if last.Content != "" {
						last.Content += "\n\n"
					}
					last.Content += content
				}
				return nil
			}
		}
		*messages = append(*messages, core.AssistantMessage(content, "", nil))
	}
	return nil
}

// pushReasoning appends one reasoning history item to the trailing assistant
// message.
func pushReasoning(reasoning ResponsesReasoningItem, messages *[]core.InputMessage) {
	texts := make([]string, 0, len(reasoning.Content))
	for _, content := range reasoning.Content {
		texts = append(texts, content.Text)
	}
	text := strings.Join(texts, "\n")
	if text == "" {
		return
	}
	if len(*messages) == 0 {
		*messages = append(*messages, core.AssistantMessage("", "", nil))
	} else if last := (*messages)[len(*messages)-1]; last.Kind != core.MessageAssistant || last.Content != "" {
		*messages = append(*messages, core.AssistantMessage("", "", nil))
	}
	last := &(*messages)[len(*messages)-1]
	if last.ReasoningContent != "" {
		last.ReasoningContent += "\n" + text
	} else {
		last.ReasoningContent = text
	}
}

// transformInputItems converts the input item list into conversation messages.
func transformInputItems(items []ResponsesInputItem, messages *[]core.InputMessage, tools responsesToolSet) error {
	if len(items) == 0 {
		return request.BadRequest("Input items array must not be empty")
	}
	pending := newResponsesCallIDs()
	resolved := newResponsesCallIDs()
	var pendingGroup *responsesToolCallGroup
	historicalNames := make(map[responsesNamespaceKey]string)
	for itemIndex, item := range items {
		if item.Typed != nil {
			if err := transformTypedInputItem(item.Typed, itemIndex, messages, tools, pending, resolved, &pendingGroup, historicalNames); err != nil {
				return err
			}
			continue
		}
		if item.Message == nil {
			continue
		}
		if err := checkPendingCalls(pending); err != nil {
			return err
		}
		appendToolCallGroup(&pendingGroup, messages)
		if err := pushMessage(*item.Message, itemIndex, messages); err != nil {
			return err
		}
	}
	appendToolCallGroup(&pendingGroup, messages)
	return checkPendingCalls(pending)
}

// transformTypedInputItem converts one typed input item.
func transformTypedInputItem(
	item *ResponsesTypedInputItem,
	itemIndex int,
	messages *[]core.InputMessage,
	tools responsesToolSet,
	pending, resolved *responsesCallIDs,
	pendingGroup **responsesToolCallGroup,
	historicalNames map[responsesNamespaceKey]string,
) error {
	switch item.Kind {
	case ResponsesTypedInputItemMessage:
		if err := checkPendingCalls(pending); err != nil {
			return err
		}
		appendToolCallGroup(pendingGroup, messages)
		if item.Message != nil {
			return pushMessage(*item.Message, itemIndex, messages)
		}
	case ResponsesTypedInputItemReasoning:
		if err := checkPendingCalls(pending); err != nil {
			return err
		}
		appendToolCallGroup(pendingGroup, messages)
		if item.Reasoning != nil {
			pushReasoning(*item.Reasoning, messages)
		}
	case ResponsesTypedInputItemFunctionCall:
		if item.FunctionCall == nil {
			return nil
		}
		call := *item.FunctionCall
		if err := checkToolCallID(itemIndex, call.CallID, pending, resolved); err != nil {
			return err
		}
		pending.add(call.CallID)
		pushCallToGroup(pendingGroup, tools.functionCall(call, historicalNames))
	case ResponsesTypedInputItemFunctionCallOutput:
		if item.FunctionCallOutput == nil {
			return nil
		}
		output := *item.FunctionCallOutput
		if err := checkToolOutputID(itemIndex, output.CallID, pending, resolved); err != nil {
			return err
		}
		content, imageSources, err := output.Output.intoContent(fmt.Sprintf("input[%d].output", itemIndex))
		if err != nil {
			return err
		}
		pushOutputToGroup(pendingGroup, output.CallID, responsesToolResult{
			content:      content,
			imageSources: imageSources,
		}, messages)
	case ResponsesTypedInputItemCustomToolCall:
		if item.CustomToolCall == nil {
			return nil
		}
		call := *item.CustomToolCall
		if err := checkToolCallID(itemIndex, call.CallID, pending, resolved); err != nil {
			return err
		}
		pending.add(call.CallID)
		arguments := jsonx.NewObject()
		arguments.Set("input", call.Input)
		serialized, err := jsonx.MarshalString(arguments)
		if err != nil {
			return request.Internalf("custom tool call input: %s", err)
		}
		pushCallToGroup(pendingGroup, core.ToolCall{
			ID:        call.CallID,
			Name:      call.Name,
			Arguments: serialized,
		})
	case ResponsesTypedInputItemCustomToolCallOutput:
		if item.CustomToolCallOutput == nil {
			return nil
		}
		output := *item.CustomToolCallOutput
		if err := checkToolOutputID(itemIndex, output.CallID, pending, resolved); err != nil {
			return err
		}
		result := responsesToolResult{}
		if output.Output != nil {
			content, imageSources, err := output.Output.intoContent(fmt.Sprintf("input[%d].output", itemIndex))
			if err != nil {
				return err
			}
			result = responsesToolResult{content: content, imageSources: imageSources}
		}
		pushOutputToGroup(pendingGroup, output.CallID, result, messages)
	}
	return nil
}
