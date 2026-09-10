package responses

import (
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// Append accumulates one event, applying events in generation order.
//
// Full responses replace the accumulated state. Incremental events with
// mismatched indices, IDs, or types are ignored.
func (r *ResponsesResponse) Append(event stream.Event) {
	switch value := event.(type) {
	case ResponsesCreatedEvent:
		*r = value.Response
	case ResponsesInProgressEvent:
		*r = value.Response
	case ResponsesCompletedEvent:
		*r = value.Response
	case ResponsesIncompleteEvent:
		*r = value.Response
	case ResponsesOutputItemAddedEvent:
		if value.OutputIndex == len(r.Output) && validItemContent(value.Item) {
			r.Output = append(r.Output, value.Item)
		}
	case ResponsesOutputItemDoneEvent:
		if value.OutputIndex < 0 || value.OutputIndex >= len(r.Output) {
			return
		}
		current := r.Output[value.OutputIndex]
		if responsesItemID(current) == responsesItemID(value.Item) &&
			sameItemKind(current, value.Item) && validItemContent(value.Item) {
			r.Output[value.OutputIndex] = value.Item
		}
	case ResponsesContentPartAddedEvent:
		content := r.matchingContent(value.OutputIndex, value.ItemID, value.Part)
		if content == nil {
			return
		}
		if value.ContentIndex == len(*content) {
			*content = append(*content, value.Part)
		}
	case ResponsesContentPartDoneEvent:
		content := r.matchingContent(value.OutputIndex, value.ItemID, value.Part)
		if content == nil || value.ContentIndex < 0 || value.ContentIndex >= len(*content) {
			return
		}
		current := (*content)[value.ContentIndex]
		if samePartKind(current, value.Part) {
			(*content)[value.ContentIndex] = value.Part
		}
	case ResponsesOutputTextDeltaEvent:
		content := r.matchingContent(value.OutputIndex, value.ItemID, ResponsesOutputTextPart{})
		if content == nil || value.ContentIndex < 0 || value.ContentIndex >= len(*content) {
			return
		}
		switch current := (*content)[value.ContentIndex].(type) {
		case ResponsesOutputTextPart:
			current.Text += value.Delta
			current.Logprobs = append(current.Logprobs, value.Logprobs...)
			(*content)[value.ContentIndex] = current
		case *ResponsesOutputTextPart:
			current.Text += value.Delta
			current.Logprobs = append(current.Logprobs, value.Logprobs...)
		}
	case ResponsesOutputTextDoneEvent:
		content := r.matchingContent(value.OutputIndex, value.ItemID, ResponsesOutputTextPart{})
		if content == nil || value.ContentIndex < 0 || value.ContentIndex >= len(*content) {
			return
		}
		switch current := (*content)[value.ContentIndex].(type) {
		case ResponsesOutputTextPart:
			current.Text = value.Text
			current.Logprobs = value.Logprobs
			(*content)[value.ContentIndex] = current
		case *ResponsesOutputTextPart:
			current.Text = value.Text
			current.Logprobs = value.Logprobs
		}
	case ResponsesReasoningTextDeltaEvent:
		content := r.matchingContent(value.OutputIndex, value.ItemID, ResponsesReasoningTextPart{})
		if content == nil || value.ContentIndex < 0 || value.ContentIndex >= len(*content) {
			return
		}
		switch current := (*content)[value.ContentIndex].(type) {
		case ResponsesReasoningTextPart:
			current.Text += value.Delta
			(*content)[value.ContentIndex] = current
		case *ResponsesReasoningTextPart:
			current.Text += value.Delta
		}
	case ResponsesReasoningTextDoneEvent:
		content := r.matchingContent(value.OutputIndex, value.ItemID, ResponsesReasoningTextPart{})
		if content == nil || value.ContentIndex < 0 || value.ContentIndex >= len(*content) {
			return
		}
		switch current := (*content)[value.ContentIndex].(type) {
		case ResponsesReasoningTextPart:
			current.Text = value.Text
			(*content)[value.ContentIndex] = current
		case *ResponsesReasoningTextPart:
			current.Text = value.Text
		}
	case ResponsesFunctionCallArgumentsDeltaEvent:
		if call, ok := r.matchingItem(value.OutputIndex, value.ItemID).(*ResponsesFunctionCallOutputItem); ok {
			call.Arguments += value.Delta
		}
	case ResponsesFunctionCallArgumentsDoneEvent:
		if call, ok := r.matchingItem(value.OutputIndex, value.ItemID).(*ResponsesFunctionCallOutputItem); ok {
			call.Arguments = value.Arguments
		}
	case ResponsesCustomToolCallInputDeltaEvent:
		if call, ok := r.matchingItem(value.OutputIndex, value.ItemID).(*ResponsesCustomToolCallOutputItem); ok {
			call.Input += value.Delta
		}
	case ResponsesCustomToolCallInputDoneEvent:
		if call, ok := r.matchingItem(value.OutputIndex, value.ItemID).(*ResponsesCustomToolCallOutputItem); ok {
			call.Input = value.Input
		}
	}
}

// matchingItem returns the output item at output_index when its ID matches.
func (r *ResponsesResponse) matchingItem(outputIndex int, expectedID string) ResponsesOutputItem {
	if outputIndex < 0 || outputIndex >= len(r.Output) {
		return nil
	}
	item := r.Output[outputIndex]
	if responsesItemID(item) != expectedID {
		return nil
	}
	return item
}

// matchingContent returns the content list of the matching item when the part
// kind belongs to that item.
func (r *ResponsesResponse) matchingContent(
	outputIndex int,
	expectedID string,
	part ResponsesContentPart,
) *[]ResponsesContentPart {
	item := r.matchingItem(outputIndex, expectedID)
	switch typed := item.(type) {
	case *ResponsesMessageOutputItem:
		if isOutputTextPart(part) {
			return &typed.Content
		}
	case *ResponsesReasoningOutputItem:
		if isReasoningTextPart(part) {
			return &typed.Content
		}
	}
	return nil
}

// responsesItemID returns the ID of an output item, or an empty string when the
// item is absent.
func responsesItemID(item ResponsesOutputItem) string {
	if isNilItem(item) {
		return ""
	}
	return item.responsesItemID()
}

// isNilItem reports whether an output item holds no value.
func isNilItem(item ResponsesOutputItem) bool {
	switch typed := item.(type) {
	case nil:
		return true
	case *ResponsesMessageOutputItem:
		return typed == nil
	case *ResponsesReasoningOutputItem:
		return typed == nil
	case *ResponsesFunctionCallOutputItem:
		return typed == nil
	case *ResponsesCustomToolCallOutputItem:
		return typed == nil
	}
	return true
}

// sameItemKind reports whether two output items have the same variant.
func sameItemKind(a, b ResponsesOutputItem) bool {
	switch a.(type) {
	case *ResponsesMessageOutputItem:
		_, ok := b.(*ResponsesMessageOutputItem)
		return ok
	case *ResponsesReasoningOutputItem:
		_, ok := b.(*ResponsesReasoningOutputItem)
		return ok
	case *ResponsesFunctionCallOutputItem:
		_, ok := b.(*ResponsesFunctionCallOutputItem)
		return ok
	case *ResponsesCustomToolCallOutputItem:
		_, ok := b.(*ResponsesCustomToolCallOutputItem)
		return ok
	}
	return false
}

// samePartKind reports whether two content parts have the same variant.
func samePartKind(a, b ResponsesContentPart) bool {
	return (isOutputTextPart(a) && isOutputTextPart(b)) ||
		(isReasoningTextPart(a) && isReasoningTextPart(b))
}

// isOutputTextPart reports whether a content part is an output text part.
func isOutputTextPart(part ResponsesContentPart) bool {
	switch part.(type) {
	case ResponsesOutputTextPart, *ResponsesOutputTextPart:
		return true
	}
	return false
}

// isReasoningTextPart reports whether a content part is a reasoning text part.
func isReasoningTextPart(part ResponsesContentPart) bool {
	switch part.(type) {
	case ResponsesReasoningTextPart, *ResponsesReasoningTextPart:
		return true
	}
	return false
}

// validItemContent reports whether an item carries only content parts of its
// own kind.
func validItemContent(item ResponsesOutputItem) bool {
	switch typed := item.(type) {
	case *ResponsesMessageOutputItem:
		if typed == nil {
			return false
		}
		for _, part := range typed.Content {
			if !isOutputTextPart(part) {
				return false
			}
		}
		return true
	case *ResponsesReasoningOutputItem:
		if typed == nil {
			return false
		}
		for _, part := range typed.Content {
			if !isReasoningTextPart(part) {
				return false
			}
		}
		return true
	case *ResponsesFunctionCallOutputItem, *ResponsesCustomToolCallOutputItem:
		return !isNilItem(item)
	}
	return false
}
