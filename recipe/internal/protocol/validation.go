package protocol

import (
	"bytes"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/request"
)

// ImageSpecialTokenNotAllowed returns the error for input text carrying
// core.ImageSpecialToken.
//
// An adapter inserts one placeholder per image source. A placeholder in input
// text would reach the prompt without an image source, so the adapters reject
// input text that spells it out.
func ImageSpecialTokenNotAllowed() error {
	return request.BadRequestf(
		"The sub-string %q in your input is not allowed for this API. Please remove %q from your input and try again.",
		core.ImageSpecialToken, core.ImageSpecialToken)
}

// ValidateToolText returns the error for converted tool text carrying
// core.ImageSpecialToken.
//
// A rendered tool prompt contains the name, description, and parameter schema
// of every definition, and the tool calls of a historical assistant turn
// contribute their names and arguments. Declared tool names are already
// restricted by ValidateToolName; historical call names and arguments are not.
func ValidateToolText(tools []core.ToolDefinition, messages []core.InputMessage) error {
	for _, tool := range tools {
		if toolDefinitionContainsImageSpecialToken(tool) {
			return ImageSpecialTokenNotAllowed()
		}
	}
	for _, message := range messages {
		if toolCallsContainImageSpecialToken(message) {
			return ImageSpecialTokenNotAllowed()
		}
	}
	return nil
}

func toolDefinitionContainsImageSpecialToken(tool core.ToolDefinition) bool {
	if strings.Contains(tool.Name, core.ImageSpecialToken) {
		return true
	}
	if tool.Description != nil && strings.Contains(*tool.Description, core.ImageSpecialToken) {
		return true
	}
	return strings.Contains(jsonx.MustMarshalPython(tool.Parameters), core.ImageSpecialToken)
}

func toolCallsContainImageSpecialToken(message core.InputMessage) bool {
	if message.Kind != core.MessageAssistant {
		return false
	}
	for _, call := range message.ToolCalls {
		if strings.Contains(call.Name, core.ImageSpecialToken) ||
			strings.Contains(call.Arguments, core.ImageSpecialToken) {
			return true
		}
	}
	return false
}

// ValidateMaxTokens rejects a zero token limit.
func ValidateMaxTokens(value *uint32, field string) error {
	if value != nil && *value == 0 {
		return request.BadRequestf("%s must be greater than zero", field)
	}
	return nil
}

// ValidateSampling checks temperature and top_p ranges.
func ValidateSampling(temperature, topP *float32) error {
	if temperature != nil && (*temperature < 0 || *temperature > 2) {
		return request.BadRequest("temperature must be in [0, 2]")
	}
	if topP != nil {
		value := float64(*topP)
		if value != value /* NaN */ || value <= 0 || value > 1 {
			return request.BadRequest("top_p must be in (0, 1]")
		}
	}
	return nil
}

// ValidateToolName checks a declared tool name.
func ValidateToolName(name, path string) error {
	valid := len(name) >= 1 && len(name) <= 128
	if valid {
		for i := 0; i < len(name); i++ {
			c := name[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
				valid = false
				break
			}
		}
	}
	if !valid {
		return request.BadRequestf("%s: expected 1-128 ASCII letters, digits, underscores or hyphens", path)
	}
	return nil
}

// ValidateToolParameters checks that a parameter schema is a compilable JSON
// Schema of type object.
//
// Schema validation uses local data with HTTP and file resolution disabled.
func ValidateToolParameters(schema jsonx.Value, path string) error {
	obj, ok := schema.(*jsonx.Object)
	if !ok {
		return request.BadRequestf("%s must be a JSON Schema of type object", path)
	}
	if typeValue, ok := obj.Get("type"); !ok || typeValue != "object" {
		return request.BadRequestf("%s must be a JSON Schema of type object", path)
	}
	if err := compileSchema(schema); err != nil {
		return request.BadRequestf("%s: %s", path, err)
	}
	return nil
}

// ContainsJSONInstruction reports whether any message content spells "json",
// ignoring ASCII case.
func ContainsJSONInstruction(messages []core.InputMessage) bool {
	for _, message := range messages {
		if containsFoldASCII(message.Content, "json") {
			return true
		}
	}
	return false
}

func containsFoldASCII(content, needle string) bool {
	if len(needle) == 0 || len(content) < len(needle) {
		return false
	}
	for start := 0; start+len(needle) <= len(content); start++ {
		matched := true
		for offset := 0; offset < len(needle); offset++ {
			c := content[start+offset]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != needle[offset] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// compileSchema compiles a JSON Schema with local resolution only. Remote and
// file references are not fetched.
func compileSchema(schema jsonx.Value) error {
	raw, err := jsonx.MarshalCompact(schema)
	if err != nil {
		return err
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("mem://schema.json", document); err != nil {
		return err
	}
	_, err = compiler.Compile("mem://schema.json")
	return err
}
