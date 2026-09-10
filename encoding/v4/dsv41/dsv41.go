// Package dsv41 renders and encodes DeepSeek V4.1 prompts.
package dsv41

import (
	"fmt"

	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/encoding"
	"github.com/dandandujie/dsr-go/encoding/v4"
)

// reasoningEffortTemplate is the reasoning-effort prefix of a V4.1 prompt. The
// score describes the requested effort on a scale from 1 to 100.
const reasoningEffortTemplate = "Reasoning Effort: %d (range 1-100, the higher the value, the more thorough the reasoning)\n\n"

// DeepseekV41Encoding renders and encodes DeepSeek V4.1 prompts.
type DeepseekV41Encoding struct {
	tokenizer encoding.TokenizerEncoder
}

var (
	_ v4.Encoding             = (*DeepseekV41Encoding)(nil)
	_ encoding.PromptEncoding = (*DeepseekV41Encoding)(nil)
)

// New creates an encoding without an attached tokenizer.
//
// Token encoding requires a tokenizer, attached with WithTokenizer.
func New() *DeepseekV41Encoding {
	return &DeepseekV41Encoding{}
}

// WithTokenizer attaches the tokenizer used by token encoding and returns the
// encoding.
func (e *DeepseekV41Encoding) WithTokenizer(tokenizer encoding.TokenizerEncoder) *DeepseekV41Encoding {
	e.tokenizer = tokenizer
	return e
}

// Encode encodes a conversation into model token IDs using the attached
// tokenizer.
func (e *DeepseekV41Encoding) Encode(c *core.Conversation) ([]uint32, error) {
	return v4.Encode(e, c)
}

// RenderConversation renders the conversation and the prefix for the next
// assistant turn.
func (e *DeepseekV41Encoding) RenderConversation(c *core.Conversation) encoding.RenderedPrompt {
	return v4.RenderConversation(e, c)
}

// Tokenizer returns the attached tokenizer, or nil when none is attached.
func (e *DeepseekV41Encoding) Tokenizer() encoding.TokenizerEncoder {
	return e.tokenizer
}

// SupportsMidConversationSystem reports whether a system message that follows
// another message starts with the system token.
func (e *DeepseekV41Encoding) SupportsMidConversationSystem() bool {
	return true
}

// SystemToken returns the token that starts a system message.
func (e *DeepseekV41Encoding) SystemToken() string {
	return v4.SystemSPToken
}

// ToolCallsBlockName returns the tag name of the block that groups tool calls.
func (e *DeepseekV41Encoding) ToolCallsBlockName() string {
	return " calls"
}

// ToolCallTagName returns the tag name of one tool call.
func (e *DeepseekV41Encoding) ToolCallTagName() string {
	return " invoke"
}

// ToolParameterTagName returns the tag name of one tool parameter.
func (e *DeepseekV41Encoding) ToolParameterTagName() string {
	return " parameter"
}

// RenderReasoningEffort returns the reasoning-effort prefix of the message at
// index, or an empty string when the message has no prefix.
func (e *DeepseekV41Encoding) RenderReasoningEffort(index int, thinkingMode bool, effort *core.ReasoningEffort) string {
	if index != 0 || !thinkingMode {
		return ""
	}
	effortScore := 75
	if effort != nil {
		switch *effort {
		case core.ReasoningEffortLow:
			effortScore = 50
		case core.ReasoningEffortMax:
			effortScore = 100
		}
	}
	return fmt.Sprintf(reasoningEffortTemplate, effortScore)
}
