package messages

import (
	"fmt"

	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// MessagesChunkGenerator converts parsed inference output into Messages
// streaming events.
//
// The caller supplies an ordered stream for one inference. Content blocks are
// opened and closed as the active block kind changes: a thinking block is
// opened when thinking mode is enabled, text and tool call blocks follow the
// output, and every block emits its stop event before the next one starts.
type MessagesChunkGenerator struct {
	id            string
	model         string
	thinkingMode  bool
	signature     string
	contentIndex  int
	toolCallIndex int
	active        *messagesContentBlockKind
	promptUsage   stream.PromptUsage
	started       bool
	finished      bool
}

// messagesContentBlockKind is the kind of the content block being generated.
type messagesContentBlockKind uint8

const (
	messagesBlockText messagesContentBlockKind = iota
	messagesBlockThinking
	messagesBlockToolUse
)

// NewChunkGenerator builds a generator. The caller supplies a unique response
// ID and the public response model name. Tool IDs are derived from the response
// ID and the per-response tool index. The default opaque thinking signature is
// the response ID.
func NewChunkGenerator(id, model string, thinkingMode bool) *MessagesChunkGenerator {
	return &MessagesChunkGenerator{
		id:           id,
		model:        model,
		thinkingMode: thinkingMode,
		signature:    id,
	}
}

// WithSignature sets the signature emitted at the end of a thinking block.
func (g *MessagesChunkGenerator) WithSignature(signature string) *MessagesChunkGenerator {
	g.signature = signature
	return g
}

// closeBlock stops the active content block, emitting the thinking signature
// first.
func (g *MessagesChunkGenerator) closeBlock(events *[]stream.Event) {
	if g.active == nil {
		return
	}
	kind := *g.active
	g.active = nil
	if kind == messagesBlockThinking {
		*events = append(*events, MessagesContentBlockDeltaEvent{
			Index: g.contentIndex,
			Delta: MessagesSignatureDelta{Signature: g.signature},
		})
	}
	*events = append(*events, MessagesContentBlockStopEvent{Index: g.contentIndex})
	g.contentIndex++
}

// openBlock stops the active block and starts a new one. The first content
// block of a response is followed by a ping.
func (g *MessagesChunkGenerator) openBlock(
	kind messagesContentBlockKind,
	contentBlock MessagesResponseContent,
	events *[]stream.Event,
) {
	g.closeBlock(events)
	*events = append(*events, MessagesContentBlockStartEvent{
		Index:        g.contentIndex,
		ContentBlock: contentBlock,
	})
	if g.contentIndex == 0 {
		*events = append(*events, MessagesPingEvent{})
	}
	active := kind
	g.active = &active
}

// delta builds a content block delta for the active block.
func (g *MessagesChunkGenerator) delta(delta MessagesDelta) stream.Event {
	return MessagesContentBlockDeltaEvent{Index: g.contentIndex, Delta: delta}
}

// isActive reports whether the active content block has the given kind.
func (g *MessagesChunkGenerator) isActive(kind messagesContentBlockKind) bool {
	return g.active != nil && *g.active == kind
}

// Generate produces the events of one parsed output chunk.
func (g *MessagesChunkGenerator) Generate(chunk stream.OutputChunk) []stream.Event {
	if g.finished {
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
		events = append(events, MessagesMessageStartEvent{
			Message: NewMessagesResponseWithUsage(g.id, g.model, 0, value.Usage),
		})
		if g.thinkingMode {
			g.openBlock(messagesBlockThinking, MessagesThinkingContent{}, &events)
		}
	case stream.RawChunk:
		if !g.isActive(messagesBlockText) {
			g.openBlock(messagesBlockText, MessagesTextContent{}, &events)
		}
		events = append(events, g.delta(MessagesTextDelta{Text: value.Content}))
	case stream.ReasoningChunk:
		if !g.isActive(messagesBlockThinking) {
			g.openBlock(messagesBlockThinking, MessagesThinkingContent{}, &events)
		}
		events = append(events, g.delta(MessagesThinkingDelta{Thinking: value.Content}))
	case stream.ToolCallChunk:
		id := fmt.Sprintf("toolu_%s_%d", g.id, g.toolCallIndex)
		g.toolCallIndex++
		input := jsonx.Value(jsonx.NewObject())
		if value.Arguments != "" {
			parsed, err := jsonx.ParseString(value.Arguments)
			if err == nil {
				input = parsed
			}
		}
		g.openBlock(messagesBlockToolUse, MessagesToolUseContent{
			ID:    id,
			Name:  value.ToolName,
			Input: input,
		}, &events)
	case stream.ToolArgumentsDeltaChunk:
		if g.isActive(messagesBlockToolUse) {
			events = append(events, g.delta(MessagesInputJSONDelta{PartialJSON: value.Content}))
		}
	case stream.FinishChunk:
		g.closeBlock(&events)
		stopReason := MessagesStopReasonFrom(value.Reason)
		events = append(events, MessagesMessageDeltaEvent{
			Delta: MessagesStopInfo{
				StopReason:   &stopReason,
				StopSequence: value.StopSequence,
			},
			Usage: NewMessagesUsage(g.promptUsage, value.Usage),
		})
		events = append(events, MessagesMessageStopEvent{})
		g.finished = true
	case stream.ToolCallBeginChunk:
	}
	return events
}
