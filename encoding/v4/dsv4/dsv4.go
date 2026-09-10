// Package dsv4 renders and encodes DeepSeek V4 prompts.
package dsv4

import (
	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/encoding"
	"github.com/dandandujie/dsr-go/encoding/v4"
)

// reasoningEffortHigh is the default reasoning-effort prefix of a V4 prompt.
const reasoningEffortHigh = "Reasoning Effort: Absolute maximum with no shortcuts permitted.\n" +
	"You MUST be very thorough in your thinking and comprehensively decompose the problem to resolve the root cause, rigorously stress-testing your logic against all potential paths, edge cases, and adversarial scenarios.\n" +
	"Explicitly write out your entire deliberation process, documenting every intermediate step, considered alternative, and rejected hypothesis to ensure absolutely no assumption is left unchecked.\n\n"

// reasoningEffortMax is the reasoning-effort prefix of a V4 prompt with maximum
// effort.
const reasoningEffortMax = "Reasoning Effort: Beyond maximum — exhaustive, relentless, and uncompromising.\n" +
	"You MUST reason with the utmost depth and rigor, leaving absolutely nothing to chance: exhaustively decompose the problem into its most fundamental components, trace every causal chain to its root, and resolve the underlying cause rather than any surface symptom.\n" +
	"Do not stop reasoning until you have independently verified the solution from multiple angles and are certain that no assumption remains unchecked and no error remains undiscovered.\n\n"

// DeepseekV4Encoding renders and encodes DeepSeek V4 prompts.
type DeepseekV4Encoding struct {
	tokenizer encoding.TokenizerEncoder
}

var (
	_ v4.Encoding             = (*DeepseekV4Encoding)(nil)
	_ encoding.PromptEncoding = (*DeepseekV4Encoding)(nil)
)

// New creates an encoding without an attached tokenizer.
//
// Token encoding requires a tokenizer, attached with WithTokenizer.
func New() *DeepseekV4Encoding {
	return &DeepseekV4Encoding{}
}

// WithTokenizer attaches the tokenizer used by token encoding and returns the
// encoding.
func (e *DeepseekV4Encoding) WithTokenizer(tokenizer encoding.TokenizerEncoder) *DeepseekV4Encoding {
	e.tokenizer = tokenizer
	return e
}

// Encode encodes a conversation into model token IDs using the attached
// tokenizer.
func (e *DeepseekV4Encoding) Encode(c *core.Conversation) ([]uint32, error) {
	return v4.Encode(e, c)
}

// RenderConversation renders the conversation and the prefix for the next
// assistant turn.
func (e *DeepseekV4Encoding) RenderConversation(c *core.Conversation) encoding.RenderedPrompt {
	return v4.RenderConversation(e, c)
}

// Tokenizer returns the attached tokenizer, or nil when none is attached.
func (e *DeepseekV4Encoding) Tokenizer() encoding.TokenizerEncoder {
	return e.tokenizer
}

// SupportsMidConversationSystem reports whether a system message that follows
// another message starts with the system token. V4 merges such messages into
// the leading system message or a user message instead.
func (e *DeepseekV4Encoding) SupportsMidConversationSystem() bool {
	return false
}

// SystemToken returns the token that starts a system message; V4 has none.
func (e *DeepseekV4Encoding) SystemToken() string {
	return ""
}

// ToolCallsBlockName returns the tag name of the block that groups tool calls.
func (e *DeepseekV4Encoding) ToolCallsBlockName() string {
	return "tool_calls"
}

// ToolCallTagName returns the tag name of one tool call.
func (e *DeepseekV4Encoding) ToolCallTagName() string {
	return "invoke"
}

// ToolParameterTagName returns the tag name of one tool parameter.
func (e *DeepseekV4Encoding) ToolParameterTagName() string {
	return "parameter"
}

// RenderReasoningEffort returns the reasoning-effort prefix of the message at
// index, or an empty string when the message has no prefix.
func (e *DeepseekV4Encoding) RenderReasoningEffort(index int, thinkingMode bool, effort *core.ReasoningEffort) string {
	if index != 0 || !thinkingMode {
		return ""
	}
	if effort != nil {
		switch *effort {
		case core.ReasoningEffortLow:
			return ""
		case core.ReasoningEffortMax:
			return reasoningEffortMax
		}
	}
	return reasoningEffortHigh
}
