package core

import "github.com/dandandujie/dsr-go/core/jsonx"

// ToolDefinition is a function the caller may execute after receiving a tool
// call.
type ToolDefinition struct {
	// Name is the function name.
	Name string
	// Description is the human-readable purpose of the function.
	Description *string
	// Parameters is the JSON Schema describing the input object.
	Parameters jsonx.Value
	// Strict requests strict argument validation. Enforcement belongs to the
	// caller.
	Strict *bool
}

// DescriptionOrEmpty returns the description or an empty string.
func (t ToolDefinition) DescriptionOrEmpty() string {
	if t.Description == nil {
		return ""
	}
	return *t.Description
}

// ToolChoice is the normalized tool selection for a rendered conversation.
type ToolChoice uint8

const (
	// ToolChoiceAuto lets the model answer or call a tool.
	ToolChoiceAuto ToolChoice = iota
	// ToolChoiceNone renders the prompt with tool definitions omitted.
	ToolChoiceNone
	// ToolChoiceRequired starts the assistant output inside a tool-call block.
	ToolChoiceRequired
)

func (c ToolChoice) String() string {
	switch c {
	case ToolChoiceNone:
		return "none"
	case ToolChoiceRequired:
		return "required"
	default:
		return "auto"
	}
}
