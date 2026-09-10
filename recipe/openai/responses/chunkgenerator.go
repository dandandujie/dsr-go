package responses

import (
	"fmt"
	"strings"
	"time"

	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// ResponsesChunkGenerator converts parsed output into Responses streaming
// events for one Start to Finish sequence.
//
// Repeated starts and chunks outside this sequence are ignored. Argument deltas
// extend only the active tool call. Custom tool output requires
// WithCustomToolNames. Other tools retain their model names, including
// flattened namespaces.
type ResponsesChunkGenerator struct {
	response        ResponsesResponse
	active          *int
	sequenceNumber  int
	toolCallIndex   int
	customToolNames map[string]struct{}
	customToolInput *customToolInputParser
	promptUsage     stream.PromptUsage
	started         bool
	finished        bool
}

// NewChunkGenerator builds a generator from a unique response ID and public
// model name.
//
// Item and call IDs derive from the response ID and their output indices. The
// caller applies custom tool names with WithCustomToolNames.
func NewChunkGenerator(id, model string) *ResponsesChunkGenerator {
	return &ResponsesChunkGenerator{
		response:        *NewResponsesResponse(id, model, timestamp()),
		customToolNames: make(map[string]struct{}),
	}
}

// WithCustomToolNames sets the declared custom tool names whose JSON input
// strings are decoded.
//
// It defaults to an empty set; other tools retain their function arguments.
func (g *ResponsesChunkGenerator) WithCustomToolNames(names []string) *ResponsesChunkGenerator {
	g.customToolNames = make(map[string]struct{}, len(names))
	for _, name := range names {
		g.customToolNames[name] = struct{}{}
	}
	return g
}

// Generate produces the events of one parsed output chunk.
func (g *ResponsesChunkGenerator) Generate(chunk stream.OutputChunk) []stream.Event {
	if g.finished {
		return nil
	}
	if _, ok := chunk.(stream.StartChunk); !ok && !g.started {
		return nil
	}
	var events []stream.Event
	switch value := chunk.(type) {
	case stream.StartChunk:
		if g.started {
			return events
		}
		g.started = true
		g.promptUsage = value.Usage
		events = append(events, ResponsesCreatedEvent{
			Response:       cloneResponse(g.response),
			SequenceNumber: g.nextSequenceNumber(),
		})
		events = append(events, ResponsesInProgressEvent{
			Response:       cloneResponse(g.response),
			SequenceNumber: g.nextSequenceNumber(),
		})
	case stream.RawChunk:
		g.appendText(value.Content, false, &events)
	case stream.ReasoningChunk:
		g.appendText(value.Content, true, &events)
	case stream.ToolCallChunk:
		g.startToolCall(value, &events)
	case stream.ToolArgumentsDeltaChunk:
		g.appendToolArguments(value.Content, &events)
	case stream.FinishChunk:
		g.finish(value, &events)
	case stream.ToolCallBeginChunk:
		// The begin chunk produces no Responses event.
	}
	return events
}

// nextSequenceNumber returns the next event position.
func (g *ResponsesChunkGenerator) nextSequenceNumber() int {
	sequenceNumber := g.sequenceNumber
	g.sequenceNumber++
	return sequenceNumber
}

// openItem stores an output item and emits its added events.
//
// The added event carries the item without its trailing content part, which the
// content part added event reports separately.
func (g *ResponsesChunkGenerator) openItem(item ResponsesOutputItem, events *[]stream.Event) {
	outputIndex := len(g.response.Output)
	g.response.Output = append(g.response.Output, item)
	var itemID string
	var part ResponsesContentPart
	switch typed := item.(type) {
	case *ResponsesMessageOutputItem:
		if len(typed.Content) > 0 {
			itemID = typed.ID
			part = typed.Content[len(typed.Content)-1]
		}
	case *ResponsesReasoningOutputItem:
		if len(typed.Content) > 0 {
			itemID = typed.ID
			part = typed.Content[len(typed.Content)-1]
		}
	}
	active := outputIndex
	g.active = &active
	// The event carries a snapshot of the item: later argument deltas mutate
	// the stored item only.
	eventItem := cloneItem(item)
	if part != nil {
		truncateItemTail(eventItem)
	}
	*events = append(*events, ResponsesOutputItemAddedEvent{
		Item:           eventItem,
		OutputIndex:    outputIndex,
		SequenceNumber: g.nextSequenceNumber(),
	})
	if part != nil {
		*events = append(*events, ResponsesContentPartAddedEvent{
			ContentIndex:   0,
			ItemID:         itemID,
			OutputIndex:    outputIndex,
			Part:           part,
			SequenceNumber: g.nextSequenceNumber(),
		})
	}
}

// closeItem finishes the active item and emits its done events.
func (g *ResponsesChunkGenerator) closeItem(status ResponsesStatus, events *[]stream.Event) {
	if g.active == nil {
		return
	}
	outputIndex := *g.active
	g.active = nil
	if parser := g.customToolInput; parser != nil {
		g.customToolInput = nil
		g.appendCustomInput(outputIndex, parser.finish(), events)
	}
	item := g.response.Output[outputIndex]
	setItemStatus(item, status)
	item = cloneItem(item)
	switch typed := item.(type) {
	case *ResponsesMessageOutputItem:
		for contentIndex, part := range typed.Content {
			g.appendPartDone(contentIndex, typed.ID, outputIndex, part, events)
		}
	case *ResponsesReasoningOutputItem:
		for contentIndex, part := range typed.Content {
			g.appendPartDone(contentIndex, typed.ID, outputIndex, part, events)
		}
	case *ResponsesFunctionCallOutputItem:
		*events = append(*events, ResponsesFunctionCallArgumentsDoneEvent{
			Arguments:      typed.Arguments,
			ItemID:         typed.ID,
			OutputIndex:    outputIndex,
			SequenceNumber: g.nextSequenceNumber(),
		})
	case *ResponsesCustomToolCallOutputItem:
		*events = append(*events, ResponsesCustomToolCallInputDoneEvent{
			Input:          typed.Input,
			ItemID:         typed.ID,
			OutputIndex:    outputIndex,
			SequenceNumber: g.nextSequenceNumber(),
		})
	}
	*events = append(*events, ResponsesOutputItemDoneEvent{
		Item:           item,
		OutputIndex:    outputIndex,
		SequenceNumber: g.nextSequenceNumber(),
	})
}

// appendPartDone emits the done events of one content part.
func (g *ResponsesChunkGenerator) appendPartDone(
	contentIndex int,
	itemID string,
	outputIndex int,
	part ResponsesContentPart,
	events *[]stream.Event,
) {
	sequenceNumber := g.nextSequenceNumber()
	switch current := part.(type) {
	case ResponsesOutputTextPart:
		*events = append(*events, ResponsesOutputTextDoneEvent{
			ContentIndex:   contentIndex,
			ItemID:         itemID,
			Logprobs:       current.Logprobs,
			OutputIndex:    outputIndex,
			SequenceNumber: sequenceNumber,
			Text:           current.Text,
		})
	case *ResponsesOutputTextPart:
		*events = append(*events, ResponsesOutputTextDoneEvent{
			ContentIndex:   contentIndex,
			ItemID:         itemID,
			Logprobs:       current.Logprobs,
			OutputIndex:    outputIndex,
			SequenceNumber: sequenceNumber,
			Text:           current.Text,
		})
	case ResponsesReasoningTextPart:
		*events = append(*events, ResponsesReasoningTextDoneEvent{
			ContentIndex:   contentIndex,
			ItemID:         itemID,
			OutputIndex:    outputIndex,
			SequenceNumber: sequenceNumber,
			Text:           current.Text,
		})
	case *ResponsesReasoningTextPart:
		*events = append(*events, ResponsesReasoningTextDoneEvent{
			ContentIndex:   contentIndex,
			ItemID:         itemID,
			OutputIndex:    outputIndex,
			SequenceNumber: sequenceNumber,
			Text:           current.Text,
		})
	}
	*events = append(*events, ResponsesContentPartDoneEvent{
		ContentIndex:   contentIndex,
		ItemID:         itemID,
		OutputIndex:    outputIndex,
		Part:           part,
		SequenceNumber: g.nextSequenceNumber(),
	})
}

// appendText appends answer or reasoning text, opening an item when needed.
func (g *ResponsesChunkGenerator) appendText(delta string, reasoning bool, events *[]stream.Event) {
	continues := false
	if g.active != nil {
		item := g.response.Output[*g.active]
		if reasoning {
			_, continues = item.(*ResponsesReasoningOutputItem)
		} else {
			_, continues = item.(*ResponsesMessageOutputItem)
		}
	}
	if !continues {
		g.closeItem(ResponsesStatusCompleted, events)
		outputIndex := len(g.response.Output)
		var item ResponsesOutputItem
		if reasoning {
			item = &ResponsesReasoningOutputItem{
				ID:      fmt.Sprintf("rs_%s_%d", g.response.ID, outputIndex),
				Status:  ResponsesStatusInProgress,
				Content: []ResponsesContentPart{ResponsesReasoningTextPart{}},
			}
		} else {
			item = &ResponsesMessageOutputItem{
				ID:     fmt.Sprintf("msg_%s_%d", g.response.ID, outputIndex),
				Status: ResponsesStatusInProgress,
				Role:   ResponsesRoleAssistant,
				Phase:  ResponsesPhaseFinalAnswer,
				Content: []ResponsesContentPart{ResponsesOutputTextPart{
					Annotations: []jsonx.Value{},
					Logprobs:    []jsonx.Value{},
				}},
			}
		}
		g.openItem(item, events)
	}
	if g.active == nil {
		return
	}
	outputIndex := *g.active
	var itemID string
	var content []ResponsesContentPart
	switch typed := g.response.Output[outputIndex].(type) {
	case *ResponsesMessageOutputItem:
		itemID, content = typed.ID, typed.Content
	case *ResponsesReasoningOutputItem:
		itemID, content = typed.ID, typed.Content
	default:
		return
	}
	if len(content) == 0 {
		return
	}
	switch current := content[0].(type) {
	case ResponsesOutputTextPart:
		current.Text += delta
		content[0] = current
	case ResponsesReasoningTextPart:
		current.Text += delta
		content[0] = current
	}
	sequenceNumber := g.nextSequenceNumber()
	if reasoning {
		*events = append(*events, ResponsesReasoningTextDeltaEvent{
			ContentIndex:   0,
			Delta:          delta,
			ItemID:         itemID,
			OutputIndex:    outputIndex,
			SequenceNumber: sequenceNumber,
		})
	} else {
		*events = append(*events, ResponsesOutputTextDeltaEvent{
			ContentIndex:   0,
			Delta:          delta,
			ItemID:         itemID,
			Logprobs:       []jsonx.Value{},
			OutputIndex:    outputIndex,
			SequenceNumber: sequenceNumber,
		})
	}
}

// startToolCall starts a function or custom tool call item.
func (g *ResponsesChunkGenerator) startToolCall(value stream.ToolCallChunk, events *[]stream.Event) {
	if g.active != nil {
		if message, ok := g.response.Output[*g.active].(*ResponsesMessageOutputItem); ok {
			message.Phase = ResponsesPhaseCommentary
		}
	}
	g.closeItem(ResponsesStatusCompleted, events)
	outputIndex := len(g.response.Output)
	callID := fmt.Sprintf("call_%s_%d", g.response.ID, g.toolCallIndex)
	var item ResponsesOutputItem
	if _, custom := g.customToolNames[value.ToolName]; custom {
		parser := &customToolInputParser{}
		input := parser.feed(value.Arguments)
		g.customToolInput = parser
		item = &ResponsesCustomToolCallOutputItem{
			ID:     fmt.Sprintf("ctc_%s_%d", g.response.ID, outputIndex),
			Status: ResponsesStatusInProgress,
			CallID: callID,
			Name:   value.ToolName,
			Input:  input,
		}
	} else {
		name := value.ToolName
		var namespace *string
		if prefix, remainder, ok := strings.Cut(value.ToolName, "::"); ok {
			name = remainder
			namespace = &prefix
		}
		item = &ResponsesFunctionCallOutputItem{
			ID:        fmt.Sprintf("fc_%s_%d", g.response.ID, outputIndex),
			Status:    ResponsesStatusInProgress,
			CallID:    callID,
			Name:      name,
			Namespace: namespace,
			Arguments: value.Arguments,
		}
	}
	g.toolCallIndex++
	g.openItem(item, events)
}

// appendToolArguments extends the active function or custom tool call.
func (g *ResponsesChunkGenerator) appendToolArguments(content string, events *[]stream.Event) {
	if g.active == nil {
		return
	}
	if call, ok := g.response.Output[*g.active].(*ResponsesFunctionCallOutputItem); ok {
		call.Arguments += content
		*events = append(*events, ResponsesFunctionCallArgumentsDeltaEvent{
			Delta:          content,
			ItemID:         call.ID,
			OutputIndex:    *g.active,
			SequenceNumber: g.nextSequenceNumber(),
		})
		return
	}
	if parser := g.customToolInput; parser != nil {
		g.appendCustomInput(*g.active, parser.feed(content), events)
	}
}

// appendCustomInput extends the active custom tool call input.
func (g *ResponsesChunkGenerator) appendCustomInput(outputIndex int, delta string, events *[]stream.Event) {
	if delta == "" {
		return
	}
	if call, ok := g.response.Output[outputIndex].(*ResponsesCustomToolCallOutputItem); ok {
		call.Input += delta
		*events = append(*events, ResponsesCustomToolCallInputDeltaEvent{
			Delta:          delta,
			ItemID:         call.ID,
			OutputIndex:    outputIndex,
			SequenceNumber: g.nextSequenceNumber(),
		})
	}
}

// finish completes the response and emits its final event.
func (g *ResponsesChunkGenerator) finish(value stream.FinishChunk, events *[]stream.Event) {
	var incompleteReason *ResponsesIncompleteReason
	switch value.Reason {
	case stream.FinishLength:
		reason := ResponsesIncompleteReasonMaxOutputTokens
		incompleteReason = &reason
	case stream.FinishContentFilter:
		reason := ResponsesIncompleteReasonContentFilter
		incompleteReason = &reason
	}
	if incompleteReason != nil {
		g.response.IncompleteDetails = &ResponsesIncompleteDetails{Reason: *incompleteReason}
		g.response.Status = ResponsesStatusIncomplete
	} else {
		g.response.Status = ResponsesStatusCompleted
	}
	usage := NewResponsesUsage(g.promptUsage, value.Usage)
	g.response.Usage = &usage
	completedAt := timestamp()
	g.response.CompletedAt = &completedAt
	g.closeItem(g.response.Status, events)
	response := cloneResponse(g.response)
	sequenceNumber := g.nextSequenceNumber()
	if g.response.Status == ResponsesStatusIncomplete {
		*events = append(*events, ResponsesIncompleteEvent{
			Response:       response,
			SequenceNumber: sequenceNumber,
		})
	} else {
		*events = append(*events, ResponsesCompletedEvent{
			Response:       response,
			SequenceNumber: sequenceNumber,
		})
	}
	g.finished = true
}

// truncateItemTail removes the trailing content part of a copied item.
func truncateItemTail(item ResponsesOutputItem) {
	switch typed := item.(type) {
	case *ResponsesMessageOutputItem:
		typed.Content = typed.Content[:len(typed.Content)-1]
	case *ResponsesReasoningOutputItem:
		typed.Content = typed.Content[:len(typed.Content)-1]
	}
}

// cloneItem returns a deep copy of an item.
func cloneItem(item ResponsesOutputItem) ResponsesOutputItem {
	switch typed := item.(type) {
	case *ResponsesMessageOutputItem:
		copy := *typed
		copy.Content = append([]ResponsesContentPart{}, typed.Content...)
		return &copy
	case *ResponsesReasoningOutputItem:
		copy := *typed
		copy.Content = append([]ResponsesContentPart{}, typed.Content...)
		copy.Summary = append([]jsonx.Value{}, typed.Summary...)
		return &copy
	case *ResponsesFunctionCallOutputItem:
		copy := *typed
		return &copy
	case *ResponsesCustomToolCallOutputItem:
		copy := *typed
		return &copy
	}
	return item
}

// cloneResponse returns a snapshot of a response whose own mutable fields can
// no longer change the emitted event.
func cloneResponse(response ResponsesResponse) ResponsesResponse {
	copy := response
	if response.Output != nil {
		copy.Output = append([]ResponsesOutputItem{}, response.Output...)
	}
	if response.Usage != nil {
		usage := *response.Usage
		copy.Usage = &usage
	}
	if response.CompletedAt != nil {
		completedAt := *response.CompletedAt
		copy.CompletedAt = &completedAt
	}
	if response.Error != nil {
		value := *response.Error
		copy.Error = &value
	}
	if response.IncompleteDetails != nil {
		value := *response.IncompleteDetails
		copy.IncompleteDetails = &value
	}
	return copy
}

// setItemStatus updates the status of an output item.
func setItemStatus(item ResponsesOutputItem, status ResponsesStatus) {
	switch typed := item.(type) {
	case *ResponsesMessageOutputItem:
		typed.Status = status
	case *ResponsesReasoningOutputItem:
		typed.Status = status
	case *ResponsesFunctionCallOutputItem:
		typed.Status = status
	case *ResponsesCustomToolCallOutputItem:
		typed.Status = status
	}
}

// timestamp returns the current Unix time in seconds.
func timestamp() uint64 {
	return uint64(time.Now().Unix())
}
