package responses

import (
	"encoding/json"
	"fmt"

	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/internal/protocol"
	"github.com/dandandujie/dsr-go/recipe/request"
)

// applyPatchParameters is the JSON Schema of the generated apply_patch tool.
// The key order matches the reference serde_json object.
const applyPatchParameters = `{"type":"object","additionalProperties":false,"properties":{"input":{"type":"string","description":"The entire contents of the apply_patch command (the *** Begin Patch ... *** End Patch envelope)."}},"required":["input"]}`

// applyPatchToolName is the only supported custom tool name.
const applyPatchToolName = "apply_patch"

// responsesNamespaceKey identifies one function inside a namespace.
type responsesNamespaceKey struct {
	namespace string
	name      string
}

// responsesToolSet holds the converted tool declarations of a request.
//
// Definitions are the converted function tools in declaration order.
// Namespace members are renamed with the "namespace::name" form, keeping the
// mapping so historical calls can be resolved. Top-level function names are
// kept so generated names never collide with them.
type responsesToolSet struct {
	definitions    []ResponsesFunctionTool
	namespaceNames map[responsesNamespaceKey]string
	topLevelNames  map[string]struct{}
}

// convertResponsesTools validates and converts the declared tools of a request.
//
// Web-search declarations are dropped under request.WebSearchIgnore and
// rejected under request.WebSearchReject. The apply_patch custom tool becomes a
// function tool, and namespaces are flattened with unique names.
func convertResponsesTools(tools []ResponsesTool, options request.ConversionOptions) (responsesToolSet, error) {
	converted := responsesToolSet{
		namespaceNames: make(map[responsesNamespaceKey]string),
		topLevelNames:  make(map[string]struct{}),
	}
	for _, tool := range tools {
		if tool.Type == ResponsesToolTypeFunction && tool.Function != nil {
			converted.topLevelNames[tool.Function.Name] = struct{}{}
		}
	}
	namespaces := make(map[string]struct{})
	for index, tool := range tools {
		path := fmt.Sprintf("tools[%d]", index)
		switch tool.Type {
		case ResponsesToolTypeFunction:
			if tool.Function == nil {
				return responsesToolSet{}, request.BadRequestf("%s: missing field `name`", path)
			}
			if err := protocol.ValidateToolName(tool.Function.Name, path+".name"); err != nil {
				return responsesToolSet{}, err
			}
			converted.definitions = append(converted.definitions, *tool.Function)
		case ResponsesToolTypeNamespace:
			if err := protocol.ValidateToolName(tool.Name, path+".name"); err != nil {
				return responsesToolSet{}, err
			}
			if _, duplicate := namespaces[tool.Name]; duplicate {
				return responsesToolSet{}, request.BadRequestf("%s: duplicate namespace name '%s'", path, tool.Name)
			}
			namespaces[tool.Name] = struct{}{}
			innerNames := make(map[string]struct{})
			for innerIndex, inner := range tool.Tools {
				innerPath := fmt.Sprintf("%s.tools[%d]", path, innerIndex)
				if inner.Type != ResponsesToolTypeFunction || inner.Function == nil {
					return responsesToolSet{}, request.BadRequestf(
						"%s: custom tools are not supported inside a namespace", innerPath)
				}
				function := *inner.Function
				if err := protocol.ValidateToolName(function.Name, innerPath+".name"); err != nil {
					return responsesToolSet{}, err
				}
				if _, duplicate := innerNames[function.Name]; duplicate {
					return responsesToolSet{}, request.BadRequestf(
						"%s: tool names within a namespace must be unique", innerPath)
				}
				innerNames[function.Name] = struct{}{}
				if tool.Description != nil && *tool.Description != "" {
					description := ""
					if function.Description != nil {
						description = *function.Description
					}
					joined := *tool.Description + "\n" + description
					function.Description = &joined
				}
				functionName := uniqueToolName(namespaceJoin(tool.Name, function.Name), converted.isNameTaken)
				converted.namespaceNames[responsesNamespaceKey{namespace: tool.Name, name: function.Name}] = functionName
				function.Name = functionName
				converted.definitions = append(converted.definitions, function)
			}
		case ResponsesToolTypeCustom:
			if tool.Name != applyPatchToolName {
				return responsesToolSet{}, request.BadRequestf(
					"Unsupported custom tool: '%s'. Only '%s' is supported", tool.Name, applyPatchToolName)
			}
			converted.definitions = append(converted.definitions, applyPatchTool())
		case ResponsesToolTypeWebSearch, ResponsesToolTypeWebSearchDated:
			switch options.ResponsesWebSearch {
			case request.WebSearchIgnore:
			case request.WebSearchReject:
				return responsesToolSet{}, request.BadRequest("Server tools are not supported")
			}
		default:
			// Unsupported tool types are ignored.
		}
	}
	return converted, nil
}

// functionCall converts one historical function call, resolving a namespace
// member to its flattened unique name.
//
// historicalNames keeps the names assigned to namespace/name pairs that were
// not declared by this request, so repeated calls resolve consistently.
func (s responsesToolSet) functionCall(call ResponsesFunctionCall, historicalNames map[responsesNamespaceKey]string) core.ToolCall {
	name := call.Name
	if call.Namespace != nil {
		key := responsesNamespaceKey{namespace: *call.Namespace, name: call.Name}
		switch {
		case s.namespaceNames[key] != "":
			name = s.namespaceNames[key]
		case historicalNames[key] != "":
			name = historicalNames[key]
		default:
			// Each historical namespace/name pair uses one unique name.
			name = uniqueToolName(namespaceJoin(key.namespace, key.name), func(candidate string) bool {
				if s.isNameTaken(candidate) {
					return true
				}
				for _, existing := range historicalNames {
					if existing == candidate {
						return true
					}
				}
				return false
			})
			historicalNames[key] = name
		}
	}
	return core.ToolCall{ID: call.CallID, Name: name, Arguments: call.Arguments}
}

// selectTools resolves the tool choice, validates the declared schemas, and
// converts the kept definitions.
func (s responsesToolSet) selectTools(
	choice *ResponsesToolChoice,
	thinking bool,
	options request.ConversionOptions,
) ([]core.ToolDefinition, core.ToolChoice, error) {
	// Dropping the web_search tool also drops a choice naming it.
	if choice != nil && choice.Named != nil && isWebSearchToolType(choice.Named.Type) {
		switch options.ResponsesWebSearch {
		case request.WebSearchIgnore:
			choice = nil
		case request.WebSearchReject:
			return nil, core.ToolChoiceAuto, request.BadRequest("Server tools are not supported")
		}
	}
	// Disabling tools skips parameter-schema and final-name checks. Definition
	// conversion has already handled tool names and kinds.
	if choice != nil && choice.Mode != nil && *choice.Mode == ResponsesToolChoiceModeNone {
		return []core.ToolDefinition{}, core.ToolChoiceNone, nil
	}
	if len(s.definitions) == 0 {
		return []core.ToolDefinition{}, core.ToolChoiceAuto, nil
	}
	names := make(map[string]struct{}, len(s.definitions))
	definitions := make([]core.ToolDefinition, 0, len(s.definitions))
	for _, tool := range s.definitions {
		if _, duplicate := names[tool.Name]; duplicate {
			return nil, core.ToolChoiceAuto, request.BadRequest("Tool names must be unique")
		}
		names[tool.Name] = struct{}{}
		parameters := jsonx.Value(jsonx.NewObject())
		if tool.Parameters != nil {
			parsed, err := jsonx.Parse(tool.Parameters)
			if err != nil {
				return nil, core.ToolChoiceAuto, request.Internalf("Tool '%s' parameters: %s", tool.Name, err)
			}
			if err := protocol.ValidateToolParameters(parsed, fmt.Sprintf("Tool '%s' parameters", tool.Name)); err != nil {
				return nil, core.ToolChoiceAuto, err
			}
			parameters = parsed
		}
		definitions = append(definitions, core.ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  parameters,
			Strict:      tool.Strict,
		})
	}
	required := false
	switch {
	case choice != nil && choice.Named != nil &&
		(choice.Named.Type == ResponsesToolTypeFunction || choice.Named.Type == ResponsesToolTypeCustom):
		if _, ok := names[choice.Named.Name]; !ok {
			return nil, core.ToolChoiceAuto, request.BadRequestf(
				"tool_choice: no tool named '%s' was specified", choice.Named.Name)
		}
		kept := definitions[:0]
		for _, tool := range definitions {
			if tool.Name == choice.Named.Name {
				kept = append(kept, tool)
			}
		}
		definitions = kept
		required = true
	case choice != nil && choice.Mode != nil && *choice.Mode == ResponsesToolChoiceModeRequired:
		required = true
	}
	if required && thinking {
		return nil, core.ToolChoiceAuto, request.BadRequest("Thinking mode does not support this tool_choice")
	}
	if required {
		return definitions, core.ToolChoiceRequired, nil
	}
	return definitions, core.ToolChoiceAuto, nil
}

// isNameTaken reports whether a candidate flattened name is already used.
func (s responsesToolSet) isNameTaken(candidate string) bool {
	if _, ok := s.topLevelNames[candidate]; ok {
		return true
	}
	for _, name := range s.namespaceNames {
		if name == candidate {
			return true
		}
	}
	return false
}

// isWebSearchToolType reports whether a tool type names the web_search server
// tool.
func isWebSearchToolType(toolType string) bool {
	return toolType == ResponsesToolTypeWebSearch || toolType == ResponsesToolTypeWebSearchDated
}

// applyPatchTool builds the function tool that replaces the apply_patch custom
// tool.
func applyPatchTool() ResponsesFunctionTool {
	description := applyPatchDescription
	return ResponsesFunctionTool{
		Name:        applyPatchToolName,
		Description: &description,
		Parameters:  json.RawMessage(applyPatchParameters),
	}
}

// namespaceJoin builds the flattened name of a namespace member.
func namespaceJoin(namespace, name string) string {
	return namespace + "::" + name
}

// uniqueToolName returns base when it is free, or the first free "base_N" name.
func uniqueToolName(base string, isTaken func(string) bool) string {
	if !isTaken(base) {
		return base
	}
	for index := 1; ; index++ {
		candidate := fmt.Sprintf("%s_%d", base, index)
		if !isTaken(candidate) {
			return candidate
		}
	}
}
