package core

// MessageKind identifies one variant of InputMessage.
type MessageKind uint8

const (
	// MessageSystem is a system message.
	MessageSystem MessageKind = iota
	// MessageUser is a user message, optionally carrying images.
	MessageUser
	// MessageAssistant is a historical assistant message.
	MessageAssistant
	// MessageTool is a tool result, optionally carrying images.
	MessageTool
	// MessageLatestReminder is a reminder appended to the conversation.
	MessageLatestReminder
)

// InputMessage is a message in the conversation supplied for prompt rendering.
type InputMessage struct {
	Kind MessageKind
	// Content is the message text.
	Content string
	// ImageSources holds the images of a user or tool message, in order.
	ImageSources []ImageSource
	// ReasoningContent is the reasoning text of an assistant message; an empty
	// string means the message has no reasoning content.
	ReasoningContent string
	// ToolCalls lists the historical tool calls of an assistant message.
	ToolCalls []ToolCall
	// ToolCallID identifies the call a tool message answers.
	ToolCallID string
}

// SystemMessage builds a system message.
func SystemMessage(content string) InputMessage {
	return InputMessage{Kind: MessageSystem, Content: content}
}

// UserMessage builds a user message.
func UserMessage(content string, imageSources []ImageSource) InputMessage {
	return InputMessage{Kind: MessageUser, Content: content, ImageSources: imageSources}
}

// AssistantMessage builds an assistant message. An empty reasoning string means
// the message carries no reasoning content.
func AssistantMessage(content, reasoningContent string, toolCalls []ToolCall) InputMessage {
	return InputMessage{
		Kind:             MessageAssistant,
		Content:          content,
		ReasoningContent: reasoningContent,
		ToolCalls:        toolCalls,
	}
}

// ToolMessage builds a tool result message.
func ToolMessage(content string, imageSources []ImageSource, toolCallID string) InputMessage {
	return InputMessage{
		Kind:         MessageTool,
		Content:      content,
		ImageSources: imageSources,
		ToolCallID:   toolCallID,
	}
}

// LatestReminderMessage builds a latest-reminder message.
func LatestReminderMessage(content string) InputMessage {
	return InputMessage{Kind: MessageLatestReminder, Content: content}
}

// ToolCall is a historical tool call whose arguments were serialized as JSON.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}
