package chatcompletion

import (
	"fmt"
	"time"

	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// ChatCompletionChunkGenerator converts parsed output into Chat Completions
// streaming chunks.
//
// The caller supplies an ordered stream for one inference. Tool argument
// deltas follow the tool call whose arguments they extend.
type ChatCompletionChunkGenerator struct {
	id                string
	model             string
	created           uint64
	thinkingMode      bool
	includeUsage      bool
	toolCallIndex     uint32
	promptUsage       stream.PromptUsage
	systemFingerprint *string
}

// NewChunkGenerator builds a generator. The caller supplies a unique response
// ID and public model name. Tool call IDs combine the response ID and
// per-response tool index.
func NewChunkGenerator(id, model string, includeUsage, thinkingMode bool) *ChatCompletionChunkGenerator {
	return &ChatCompletionChunkGenerator{
		id:           id,
		model:        model,
		created:      uint64(time.Now().Unix()),
		thinkingMode: thinkingMode,
		includeUsage: includeUsage,
	}
}

// WithIncludeUsage applies usage settings extracted from the original Chat
// Completions request.
func (g *ChatCompletionChunkGenerator) WithIncludeUsage(includeUsage bool) *ChatCompletionChunkGenerator {
	g.includeUsage = includeUsage
	return g
}

// newChunk builds one chunk carrying a single choice.
func (g *ChatCompletionChunkGenerator) newChunk(
	delta ChatCompletionMessageDelta,
	finishReason *ChatCompletionFinishReason,
	usage *ChatCompletionUsage,
) []stream.Event {
	// Include null usage before the final snapshot when requested. Otherwise the
	// final chunk includes usage.
	var serialized *jsonx.Optional[ChatCompletionUsage]
	if g.includeUsage {
		if usage != nil {
			serialized = jsonx.Some(*usage)
		} else {
			serialized = jsonx.Null[ChatCompletionUsage]()
		}
	} else if usage != nil {
		serialized = jsonx.Some(*usage)
	}
	systemFingerprint := g.systemFingerprint
	return []stream.Event{ChatCompletionChunk{
		ID:                g.id,
		Object:            chatCompletionChunkObject,
		Created:           g.created,
		Model:             g.model,
		SystemFingerprint: systemFingerprint,
		Choices: []ChatCompletionChunkChoice{{
			Index:        0,
			Delta:        delta,
			FinishReason: finishReason,
		}},
		Usage: serialized,
	}}
}

// Generate produces the chunks of one parsed output chunk.
func (g *ChatCompletionChunkGenerator) Generate(chunk stream.OutputChunk) []stream.Event {
	switch value := chunk.(type) {
	case stream.StartChunk:
		g.promptUsage = value.Usage
		g.systemFingerprint = value.SystemFingerprint
		delta := ChatCompletionMessageDelta{Role: rolePointer(ChatCompletionRoleAssistant)}
		if g.thinkingMode {
			delta.Content = jsonx.Null[string]()
			delta.ReasoningContent = jsonx.Some("")
		} else {
			delta.Content = jsonx.Some("")
		}
		return g.newChunk(delta, nil, nil)
	case stream.RawChunk:
		delta := ChatCompletionMessageDelta{Content: jsonx.Some(value.Content)}
		if g.thinkingMode {
			delta.ReasoningContent = jsonx.Null[string]()
		}
		return g.newChunk(delta, nil, nil)
	case stream.ReasoningChunk:
		delta := ChatCompletionMessageDelta{
			Content:          jsonx.Null[string](),
			ReasoningContent: jsonx.Some(value.Content),
		}
		return g.newChunk(delta, nil, nil)
	case stream.ToolCallBeginChunk:
		return nil
	case stream.ToolCallChunk:
		index := g.toolCallIndex
		g.toolCallIndex++
		delta := ChatCompletionMessageDelta{
			ToolCalls: []ChatCompletionToolCallDelta{ChatCompletionToolCallDeltaStart{
				Index: index,
				Call: ChatCompletionResponseToolCall{
					ID:   fmt.Sprintf("call_%s_%d", g.id, index),
					Type: ChatCompletionToolCallTypeFunction,
					Function: ChatCompletionResponseFunctionCall{
						Name:      value.ToolName,
						Arguments: value.Arguments,
					},
				},
			}},
		}
		return g.newChunk(delta, nil, nil)
	case stream.ToolArgumentsDeltaChunk:
		index := uint32(0)
		if g.toolCallIndex > 0 {
			index = g.toolCallIndex - 1
		}
		delta := ChatCompletionMessageDelta{
			ToolCalls: []ChatCompletionToolCallDelta{ChatCompletionToolCallDeltaArguments{
				Index:    index,
				Function: ChatCompletionFunctionArgumentsDelta{Arguments: value.Content},
			}},
		}
		return g.newChunk(delta, nil, nil)
	case stream.FinishChunk:
		delta := ChatCompletionMessageDelta{Content: jsonx.Some("")}
		if g.thinkingMode {
			delta.ReasoningContent = jsonx.Null[string]()
		}
		reason := FinishReasonFrom(value.Reason)
		usage := NewChatCompletionUsage(g.promptUsage, value.Usage)
		return g.newChunk(delta, &reason, &usage)
	default:
		return nil
	}
}

func rolePointer(role ChatCompletionRole) *ChatCompletionRole { return &role }
