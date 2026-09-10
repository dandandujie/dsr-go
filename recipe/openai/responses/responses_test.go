package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/request"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// mustRequest decodes a request body.
func mustRequest(t *testing.T, body string) *ResponsesRequest {
	t.Helper()
	var value ResponsesRequest
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		t.Fatalf("unmarshal request %s: %v", body, err)
	}
	return &value
}

// mustConvert decodes and converts a request body.
func mustConvert(t *testing.T, body string, options request.ConversionOptions) *request.ConversationRequest {
	t.Helper()
	converted, err := mustRequest(t, body).Convert(options)
	if err != nil {
		t.Fatalf("convert request %s: %v", body, err)
	}
	return converted
}

// conversionError returns the detail of a failed conversion.
func conversionError(t *testing.T, body string, options request.ConversionOptions) string {
	t.Helper()
	_, err := mustRequest(t, body).Convert(options)
	if err == nil {
		t.Fatalf("expected conversion error for %s", body)
	}
	return err.Error()
}

// marshalJSON encodes a value in the compact form used by the protocol.
func marshalJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return string(encoded)
}

// thinkingOff returns conversion options that disable thinking by default.
func thinkingOff() request.ConversionOptions {
	return request.NewConversionOptions().WithDefaultThinkingMode(false)
}

func TestSimpleTextRequestConversion(t *testing.T) {
	converted := mustConvert(t, `{"model":"deepseek-flash","input":"Hello"}`, request.NewConversionOptions())
	conversation := converted.Conversation
	if len(conversation.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(conversation.Messages))
	}
	if message := conversation.Messages[0]; message.Kind != core.MessageUser || message.Content != "Hello" {
		t.Errorf("message = %+v, want user Hello", message)
	}
	if !conversation.ThinkingMode {
		t.Error("thinking mode = false, want true by default")
	}
	if conversation.ReasoningEffort == nil || *conversation.ReasoningEffort != core.ReasoningEffortHigh {
		t.Errorf("reasoning effort = %v, want high", conversation.ReasoningEffort)
	}
	if conversation.ResponseFormat != core.ResponseFormatText {
		t.Errorf("response format = %v, want text", conversation.ResponseFormat)
	}
	if conversation.ToolChoice != core.ToolChoiceAuto {
		t.Errorf("tool choice = %v, want auto", conversation.ToolChoice)
	}
	if len(conversation.Tools) != 0 {
		t.Errorf("tools = %d, want 0", len(conversation.Tools))
	}
	if converted.ParsingOptions.ParseToolCalls ||
		converted.ParsingOptions.ToolCallInitialStage ||
		converted.ParsingOptions.ParseJSONOutput {
		t.Errorf("parsing options = %+v, want all false", converted.ParsingOptions)
	}
	if converted.ParsingOptions.ReasoningInitialStage == nil ||
		*converted.ParsingOptions.ReasoningInitialStage != stream.ReasoningStageStart {
		t.Errorf("reasoning stage = %v, want start", converted.ParsingOptions.ReasoningInitialStage)
	}
	if converted.Model == nil || *converted.Model != "deepseek-flash" {
		t.Errorf("model = %v, want deepseek-flash", converted.Model)
	}
	if converted.Stream {
		t.Error("stream = true, want false")
	}
	generator, ok := converted.NewChunkGenerator("id", "model").(*ResponsesChunkGenerator)
	if !ok || generator == nil {
		t.Fatalf("chunk generator = %T, want *ResponsesChunkGenerator", converted.NewChunkGenerator("id", "model"))
	}
	if len(generator.customToolNames) != 0 {
		t.Errorf("custom tool names = %v, want empty", generator.customToolNames)
	}
}

func TestThinkingModeResolution(t *testing.T) {
	value := func(level core.ReasoningEffort) *core.ReasoningEffort { return &level }
	cases := []struct {
		name     string
		body     string
		options  request.ConversionOptions
		thinking bool
		effort   *core.ReasoningEffort
	}{
		{"default enabled", `{"model":"m","input":"x"}`, request.NewConversionOptions(), true, value(core.ReasoningEffortHigh)},
		{"default disabled", `{"model":"m","input":"x"}`, thinkingOff(), false, nil},
		{"none", `{"model":"m","input":"x","reasoning":{"effort":"none"}}`, request.NewConversionOptions(), false, nil},
		{"minimal", `{"model":"m","input":"x","reasoning":{"effort":"minimal"}}`, request.NewConversionOptions(), true, value(core.ReasoningEffortLow)},
		{"low", `{"model":"m","input":"x","reasoning":{"effort":"low"}}`, thinkingOff(), true, value(core.ReasoningEffortLow)},
		{"medium", `{"model":"m","input":"x","reasoning":{"effort":"medium"}}`, request.NewConversionOptions(), true, value(core.ReasoningEffortHigh)},
		{"high", `{"model":"m","input":"x","reasoning":{"effort":"high"}}`, request.NewConversionOptions(), true, value(core.ReasoningEffortHigh)},
		{"xhigh", `{"model":"m","input":"x","reasoning":{"effort":"xhigh"}}`, request.NewConversionOptions(), true, value(core.ReasoningEffortXhigh)},
		{"max", `{"model":"m","input":"x","reasoning":{"effort":"max"}}`, request.NewConversionOptions(), true, value(core.ReasoningEffortMax)},
		{"explicit none overrides default", `{"model":"m","input":"x","reasoning":{"effort":"none"}}`, thinkingOff(), false, nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			converted := mustConvert(t, testCase.body, testCase.options)
			conversation := converted.Conversation
			if conversation.ThinkingMode != testCase.thinking {
				t.Fatalf("thinking mode = %v, want %v", conversation.ThinkingMode, testCase.thinking)
			}
			switch {
			case testCase.effort == nil && conversation.ReasoningEffort != nil:
				t.Fatalf("reasoning effort = %v, want nil", *conversation.ReasoningEffort)
			case testCase.effort != nil && conversation.ReasoningEffort == nil:
				t.Fatalf("reasoning effort = nil, want %v", *testCase.effort)
			case testCase.effort != nil && *conversation.ReasoningEffort != *testCase.effort:
				t.Fatalf("reasoning effort = %v, want %v", *conversation.ReasoningEffort, *testCase.effort)
			}
			if testCase.thinking {
				if converted.ParsingOptions.ReasoningInitialStage == nil ||
					*converted.ParsingOptions.ReasoningInitialStage != stream.ReasoningStageStart {
					t.Errorf("reasoning stage = %v, want start", converted.ParsingOptions.ReasoningInitialStage)
				}
			} else if converted.ParsingOptions.ReasoningInitialStage != nil {
				t.Errorf("reasoning stage = %v, want nil", *converted.ParsingOptions.ReasoningInitialStage)
			}
		})
	}
}

func TestInstructionsHandling(t *testing.T) {
	converted := mustConvert(t, `{"model":"m","instructions":"Be concise.","input":"Hi"}`, request.NewConversionOptions())
	messages := converted.Conversation.Messages
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	if messages[0].Kind != core.MessageSystem || messages[0].Content != "Be concise." {
		t.Errorf("first message = %+v, want system Be concise.", messages[0])
	}
	if messages[1].Kind != core.MessageUser || messages[1].Content != "Hi" {
		t.Errorf("second message = %+v, want user Hi", messages[1])
	}

	converted = mustConvert(t, `{"model":"m","instructions":"","input":"Hi"}`, request.NewConversionOptions())
	if len(converted.Conversation.Messages) != 1 || converted.Conversation.Messages[0].Kind != core.MessageUser {
		t.Errorf("empty instructions added a message: %+v", converted.Conversation.Messages)
	}

	converted = mustConvert(t, `{"model":"m","instructions":"Only instructions."}`, request.NewConversionOptions())
	messages = converted.Conversation.Messages
	if len(messages) != 1 || messages[0].Kind != core.MessageSystem || messages[0].Content != "Only instructions." {
		t.Errorf("instructions-only messages = %+v", messages)
	}

	if detail := conversionError(t, `{"model":"m"}`, request.NewConversionOptions()); detail != "Either input or instructions must be provided" {
		t.Errorf("error = %q, want Either input or instructions must be provided", detail)
	}

	converted = mustConvert(t, `{"model":"m","input":""}`, request.NewConversionOptions())
	messages = converted.Conversation.Messages
	if len(messages) != 1 || messages[0].Kind != core.MessageUser || messages[0].Content != "" {
		t.Errorf("empty input messages = %+v, want one empty user message", messages)
	}
}

func TestInstructionsRejectImageSpecialToken(t *testing.T) {
	body := `{"model":"m","instructions":"see ` + core.ImageSpecialToken + `","input":"x"}`
	detail := conversionError(t, body, request.NewConversionOptions())
	if !strings.Contains(detail, "is not allowed for this API") {
		t.Errorf("error = %q, want image token rejection", detail)
	}
}

func TestFunctionToolsAndToolChoice(t *testing.T) {
	body := `{"model":"m","input":"Weather?","reasoning":{"effort":"none"},"tools":[{"type":"function","name":"get_weather","description":"w","parameters":{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]},"strict":true}],"tool_choice":"required"}`
	converted := mustConvert(t, body, request.NewConversionOptions())
	conversation := converted.Conversation
	if len(conversation.Tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(conversation.Tools))
	}
	tool := conversation.Tools[0]
	if tool.Name != "get_weather" || tool.DescriptionOrEmpty() != "w" || tool.Strict == nil || !*tool.Strict {
		t.Errorf("tool = %+v", tool)
	}
	parameters := marshalJSON(t, tool.Parameters)
	want := `{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`
	if parameters != want {
		t.Errorf("parameters = %s, want %s", parameters, want)
	}
	if conversation.ToolChoice != core.ToolChoiceRequired {
		t.Errorf("tool choice = %v, want required", conversation.ToolChoice)
	}
	if !converted.ParsingOptions.ParseToolCalls || !converted.ParsingOptions.ToolCallInitialStage {
		t.Errorf("parsing options = %+v, want tool parsing in the initial stage", converted.ParsingOptions)
	}

	// A required choice is rejected while thinking is enabled.
	requiredThinking := `{"model":"m","input":"x","tools":[{"type":"function","name":"f"}],"tool_choice":"required"}`
	if detail := conversionError(t, requiredThinking, request.NewConversionOptions()); detail != "Thinking mode does not support this tool_choice" {
		t.Errorf("error = %q", detail)
	}

	// A named choice keeps only the named tool.
	named := `{"model":"m","input":"x","reasoning":{"effort":"none"},"tools":[{"type":"function","name":"a"},{"type":"function","name":"b"}],"tool_choice":{"type":"function","name":"b"}}`
	converted = mustConvert(t, named, request.NewConversionOptions())
	if len(converted.Conversation.Tools) != 1 || converted.Conversation.Tools[0].Name != "b" {
		t.Errorf("named tools = %+v, want only b", converted.Conversation.Tools)
	}
	if converted.Conversation.ToolChoice != core.ToolChoiceRequired {
		t.Errorf("named tool choice = %v, want required", converted.Conversation.ToolChoice)
	}
	if detail := conversionError(t, `{"model":"m","input":"x","tools":[{"type":"function","name":"a"}],"tool_choice":{"type":"function","name":"b"}}`, request.NewConversionOptions()); detail != "tool_choice: no tool named 'b' was specified" {
		t.Errorf("error = %q", detail)
	}

	// Tool choice none drops the definitions without validating parameters.
	none := `{"model":"m","input":"x","tools":[{"type":"function","name":"a","parameters":{"type":"string"}}],"tool_choice":"none"}`
	converted = mustConvert(t, none, request.NewConversionOptions())
	if len(converted.Conversation.Tools) != 0 || converted.Conversation.ToolChoice != core.ToolChoiceNone {
		t.Errorf("none conversion = %+v / %v", converted.Conversation.Tools, converted.Conversation.ToolChoice)
	}

	// A function tool without parameters uses an empty object schema.
	converted = mustConvert(t, `{"model":"m","input":"x","tools":[{"type":"function","name":"a"}]}`, request.NewConversionOptions())
	if got := marshalJSON(t, converted.Conversation.Tools[0].Parameters); got != "{}" {
		t.Errorf("default parameters = %s, want {}", got)
	}

	// An explicitly empty schema is rejected.
	empty := `{"model":"m","input":"x","tools":[{"type":"function","name":"a","parameters":{}}]}`
	if detail := conversionError(t, empty, request.NewConversionOptions()); detail != "Tool 'a' parameters must be a JSON Schema of type object" {
		t.Errorf("error = %q", detail)
	}

	// Duplicate names are rejected.
	duplicate := `{"model":"m","input":"x","tools":[{"type":"function","name":"a"},{"type":"function","name":"a"}]}`
	if detail := conversionError(t, duplicate, request.NewConversionOptions()); detail != "Tool names must be unique" {
		t.Errorf("error = %q", detail)
	}

	// Invalid names are rejected with the declaration path.
	badName := `{"model":"m","input":"x","tools":[{"type":"function","name":"bad name"}]}`
	if detail := conversionError(t, badName, request.NewConversionOptions()); detail != "tools[0].name: expected 1-128 ASCII letters, digits, underscores or hyphens" {
		t.Errorf("error = %q", detail)
	}
}

func TestNamespacesAndHistoricalCalls(t *testing.T) {
	body := `{"model":"m","input":"x","reasoning":{"effort":"none"},"tools":[` +
		`{"type":"function","name":"search","parameters":{"type":"object"}},` +
		`{"type":"namespace","name":"files","description":"File tools.","tools":[` +
		`{"type":"function","name":"search","description":"search files","parameters":{"type":"object"}},` +
		`{"type":"function","name":"read"}]}]}`
	converted := mustConvert(t, body, request.NewConversionOptions())
	tools := converted.Conversation.Tools
	if len(tools) != 3 {
		t.Fatalf("tools = %d, want 3", len(tools))
	}
	if tools[0].Name != "search" || tools[0].Description != nil {
		t.Errorf("tools[0] = %+v, want search without description", tools[0])
	}
	if tools[1].Name != "files::search" || tools[1].DescriptionOrEmpty() != "File tools.\nsearch files" {
		t.Errorf("tools[1] = %+v", tools[1])
	}
	if tools[2].Name != "files::read" || tools[2].DescriptionOrEmpty() != "File tools.\n" {
		t.Errorf("tools[2] = %+v", tools[2])
	}

	// A namespace member is resolved from the declared mapping, and an
	// undeclared namespace is flattened on the fly.
	history := `{"model":"m","input":[` +
		`{"type":"message","role":"user","content":"hi"},` +
		`{"type":"function_call","call_id":"call_b","name":"read","namespace":"other","arguments":"{}"},` +
		`{"type":"function_call","call_id":"call_a","name":"search","namespace":"files","arguments":"{}"},` +
		`{"type":"function_call_output","call_id":"call_a","output":"ok"},` +
		`{"type":"function_call_output","call_id":"call_b","output":"ok"}],` +
		`"reasoning":{"effort":"none"},"tools":[` +
		`{"type":"namespace","name":"files","tools":[{"type":"function","name":"search"}]}]}`
	converted = mustConvert(t, history, request.NewConversionOptions())
	messages := converted.Conversation.Messages
	if len(messages) != 4 {
		t.Fatalf("messages = %d, want 4: %+v", len(messages), messages)
	}
	toolCalls := messages[1].ToolCalls
	if len(toolCalls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(toolCalls))
	}
	if toolCalls[0].ID != "call_a" || toolCalls[0].Name != "files::search" {
		t.Errorf("tool call 0 = %+v, want call_a files::search", toolCalls[0])
	}
	if toolCalls[1].ID != "call_b" || toolCalls[1].Name != "other::read" {
		t.Errorf("tool call 1 = %+v, want call_b other::read", toolCalls[1])
	}
	if messages[2].Kind != core.MessageTool || messages[2].ToolCallID != "call_a" {
		t.Errorf("message 2 = %+v, want tool result for call_a", messages[2])
	}
	if messages[3].Kind != core.MessageTool || messages[3].ToolCallID != "call_b" {
		t.Errorf("message 3 = %+v, want tool result for call_b", messages[3])
	}

	// A custom tool inside a namespace is rejected.
	customInNamespace := `{"model":"m","input":"x","tools":[{"type":"namespace","name":"ns","tools":[{"type":"custom","name":"apply_patch"}]}]}`
	if detail := conversionError(t, customInNamespace, request.NewConversionOptions()); detail != "tools[0].tools[0]: custom tools are not supported inside a namespace" {
		t.Errorf("error = %q", detail)
	}

	// Duplicate namespace names are rejected.
	duplicateNamespace := `{"model":"m","input":"x","tools":[{"type":"namespace","name":"ns","tools":[]},{"type":"namespace","name":"ns","tools":[]}]}`
	if detail := conversionError(t, duplicateNamespace, request.NewConversionOptions()); detail != "tools[1]: duplicate namespace name 'ns'" {
		t.Errorf("error = %q", detail)
	}

	// Duplicate member names inside one namespace are rejected.
	duplicateMember := `{"model":"m","input":"x","tools":[{"type":"namespace","name":"ns","tools":[{"type":"function","name":"a"},{"type":"function","name":"a"}]}]}`
	if detail := conversionError(t, duplicateMember, request.NewConversionOptions()); detail != "tools[0].tools[1]: tool names within a namespace must be unique" {
		t.Errorf("error = %q", detail)
	}
}

func TestApplyPatchCustomTool(t *testing.T) {
	body := `{"model":"m","input":"x","reasoning":{"effort":"none"},"tools":[{"type":"custom","name":"apply_patch"}]}`
	parsed := mustRequest(t, body)
	names := parsed.CustomToolNames()
	if len(names) != 1 || names[0] != "apply_patch" {
		t.Fatalf("custom tool names = %v, want [apply_patch]", names)
	}
	converted, err := parsed.Convert(request.NewConversionOptions())
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	tools := converted.Conversation.Tools
	if len(tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(tools))
	}
	tool := tools[0]
	if tool.Name != "apply_patch" {
		t.Errorf("name = %q, want apply_patch", tool.Name)
	}
	if tool.DescriptionOrEmpty() != applyPatchDescription {
		t.Errorf("description does not match the reference description")
	}
	if tool.Strict != nil {
		t.Errorf("strict = %v, want nil", *tool.Strict)
	}
	if got := marshalJSON(t, tool.Parameters); got != applyPatchParameters {
		t.Errorf("parameters = %s, want %s", got, applyPatchParameters)
	}

	// A custom named choice selects the generated tool.
	named := `{"model":"m","input":"x","reasoning":{"effort":"none"},"tools":[{"type":"custom","name":"apply_patch"}],"tool_choice":{"type":"custom","name":"apply_patch"}}`
	converted = mustConvert(t, named, request.NewConversionOptions())
	if converted.Conversation.ToolChoice != core.ToolChoiceRequired || len(converted.Conversation.Tools) != 1 {
		t.Errorf("named custom choice = %v / %d tools", converted.Conversation.ToolChoice, len(converted.Conversation.Tools))
	}

	// Other custom tools are rejected.
	unsupported := `{"model":"m","input":"x","tools":[{"type":"custom","name":"other"}]}`
	if detail := conversionError(t, unsupported, request.NewConversionOptions()); detail != "Unsupported custom tool: 'other'. Only 'apply_patch' is supported" {
		t.Errorf("error = %q", detail)
	}
}

// TestPreviousResponseIDIsIgnored verifies the reference behavior: the request
// schema has no previous_response_id field and unknown top-level fields are
// ignored, so no stored conversation context is retrieved or retained.
func TestPreviousResponseIDIsIgnored(t *testing.T) {
	converted := mustConvert(t, `{"model":"m","input":"x","previous_response_id":"resp_1"}`, request.NewConversionOptions())
	messages := converted.Conversation.Messages
	if len(messages) != 1 || messages[0].Content != "x" {
		t.Errorf("messages = %+v, want only the input", messages)
	}
	if !converted.Conversation.ThinkingMode {
		t.Error("thinking mode = false, want the default")
	}
}

func TestWebSearchBehavior(t *testing.T) {
	withSearch := `{"model":"m","input":"x","reasoning":{"effort":"none"},"tools":[{"type":"function","name":"f"},{"type":"web_search"}]}`
	converted := mustConvert(t, withSearch, request.NewConversionOptions())
	if len(converted.Conversation.Tools) != 1 || converted.Conversation.Tools[0].Name != "f" {
		t.Errorf("ignored web_search tools = %+v, want only f", converted.Conversation.Tools)
	}
	reject := request.NewConversionOptions().WithResponsesWebSearch(request.WebSearchReject)
	if detail := conversionError(t, withSearch, reject); detail != "Server tools are not supported" {
		t.Errorf("error = %q", detail)
	}

	// The dated alias is handled the same way.
	dated := `{"model":"m","input":"x","tools":[{"type":"web_search_2025_08_26"}]}`
	if detail := conversionError(t, dated, reject); detail != "Server tools are not supported" {
		t.Errorf("alias error = %q", detail)
	}
	converted = mustConvert(t, dated, request.NewConversionOptions())
	if len(converted.Conversation.Tools) != 0 {
		t.Errorf("alias tools = %+v, want none", converted.Conversation.Tools)
	}

	// Dropping the declaration also drops a choice naming it.
	namedChoice := `{"model":"m","input":"x","reasoning":{"effort":"none"},"tools":[{"type":"function","name":"f"}],"tool_choice":{"type":"web_search"}}`
	converted = mustConvert(t, namedChoice, request.NewConversionOptions())
	if converted.Conversation.ToolChoice != core.ToolChoiceAuto || len(converted.Conversation.Tools) != 1 {
		t.Errorf("ignored web_search choice = %v / %d tools", converted.Conversation.ToolChoice, len(converted.Conversation.Tools))
	}
	if detail := conversionError(t, namedChoice, reject); detail != "Server tools are not supported" {
		t.Errorf("rejected choice error = %q", detail)
	}

	// Unsupported tool declarations are ignored.
	unsupported := `{"model":"m","input":"x","tools":[{"type":"computer_use_preview"}]}`
	converted = mustConvert(t, unsupported, request.NewConversionOptions())
	if len(converted.Conversation.Tools) != 0 {
		t.Errorf("unsupported tools = %+v, want none", converted.Conversation.Tools)
	}
}

func TestImageInput(t *testing.T) {
	body := `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"what?"},{"type":"input_image","image_url":"https://example.com/a.png","detail":"auto"}]}]}`
	converted := mustConvert(t, body, request.NewConversionOptions())
	message := converted.Conversation.Messages[0]
	if message.Content != "what?\n\n"+core.ImageSpecialToken {
		t.Errorf("content = %q", message.Content)
	}
	if len(message.ImageSources) != 1 {
		t.Fatalf("images = %d, want 1", len(message.ImageSources))
	}
	image := message.ImageSources[0]
	if image.Kind != core.ImageSourceURL || image.URL != "https://example.com/a.png" || image.Detail != core.ImageDetailAuto {
		t.Errorf("image = %+v", image)
	}

	// A base64 data URL is accepted.
	dataURL := `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]}]}`
	converted = mustConvert(t, dataURL, request.NewConversionOptions())
	image = converted.Conversation.Messages[0].ImageSources[0]
	if image.Kind != core.ImageSourceDataURL || image.DataURL != "data:image/png;base64,AAAA" {
		t.Errorf("data url image = %+v", image)
	}

	// Documents become the unsupported-document placeholder.
	document := `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_file","file_id":"file-1"}]}]}`
	converted = mustConvert(t, document, request.NewConversionOptions())
	if converted.Conversation.Messages[0].Content == "" {
		t.Error("document content is empty, want the unsupported placeholder")
	}

	// Tool output images are collected as well.
	toolImage := `{"model":"m","input":[{"type":"function_call","call_id":"c1","name":"f","arguments":"{}"},{"type":"function_call_output","call_id":"c1","output":[{"type":"input_text","text":"see"},{"type":"input_image","image_url":"https://example.com/b.png"}]}]}`
	converted = mustConvert(t, toolImage, request.NewConversionOptions())
	messages := converted.Conversation.Messages
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	toolMessage := messages[1]
	if toolMessage.Kind != core.MessageTool || toolMessage.Content != "see\n\n"+core.ImageSpecialToken {
		t.Errorf("tool message = %+v", toolMessage)
	}
	if len(toolMessage.ImageSources) != 1 || toolMessage.ImageSources[0].Detail != core.ImageDetailHigh {
		t.Errorf("tool images = %+v", toolMessage.ImageSources)
	}

	// Invalid image references are rejected.
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"file_id",
			`{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_image","file_id":"file-1"}]}]}`,
			"input[0].content[0]: file_id is not supported. Pass the image as a base64 data url instead.",
		},
		{
			"both",
			`{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.com/a.png","file_id":"file-1"}]}]}`,
			"input[0].content[0]: input_image cannot have both image_url and file_id",
		},
		{
			"neither",
			`{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_image"}]}]}`,
			"input[0].content[0]: input_image must have image_url or file_id",
		},
		{
			"empty url",
			`{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":""}]}]}`,
			"input[0].content[0]: input_image must have image_url or file_id",
		},
		{
			"system image",
			`{"model":"m","input":[{"type":"message","role":"system","content":[{"type":"input_image","image_url":"https://example.com/a.png"}]}]}`,
			"Image in system message is unsupported",
		},
		{
			"unsupported url",
			`{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"ftp://example.com/a.png"}]}]}`,
			"input[0].content[0].image_url: Unsupported image_url format",
		},
		{
			"unsupported content block",
			`{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"audio","data":"x"}]}]}`,
			"Unsupported content block",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if detail := conversionError(t, testCase.body, request.NewConversionOptions()); detail != testCase.want {
				t.Errorf("error = %q, want %q", detail, testCase.want)
			}
		})
	}
}

func TestInputValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"empty items",
			`{"model":"m","input":[]}`,
			"Input items array must not be empty",
		},
		{
			"pending call",
			`{"model":"m","input":[{"type":"function_call","call_id":"c1","name":"f","arguments":"{}"}]}`,
			"No tool output found for tool call c1.",
		},
		{
			"duplicate call id",
			`{"model":"m","input":[{"type":"function_call","call_id":"c1","name":"f","arguments":"{}"},{"type":"function_call","call_id":"c1","name":"f","arguments":"{}"}]}`,
			"Duplicate 'call_id': c1.",
		},
		{
			"unknown output",
			`{"model":"m","input":[{"type":"function_call_output","call_id":"c1","output":"x"}]}`,
			"No tool call found for tool output with call_id c1.",
		},
		{
			"duplicate output",
			`{"model":"m","input":[{"type":"function_call","call_id":"c1","name":"f","arguments":"{}"},{"type":"function_call_output","call_id":"c1","output":"x"},{"type":"function_call_output","call_id":"c1","output":"y"}]}`,
			"Duplicate tool output for call_id: c1.",
		},
		{
			"empty call id",
			`{"model":"m","input":[{"type":"function_call","call_id":"","name":"f","arguments":"{}"}]}`,
			"Invalid 'input[0].call_id': empty string. Expected a string with minimum length 1, but got an empty string instead.",
		},
		{
			"resolved call id reused",
			`{"model":"m","input":[{"type":"function_call","call_id":"c1","name":"f","arguments":"{}"},{"type":"function_call_output","call_id":"c1","output":"x"},{"type":"function_call","call_id":"c1","name":"f","arguments":"{}"}]}`,
			"Duplicate 'call_id': c1.",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if detail := conversionError(t, testCase.body, request.NewConversionOptions()); detail != testCase.want {
				t.Errorf("error = %q, want %q", detail, testCase.want)
			}
		})
	}
}

func TestHistoryCustomToolCall(t *testing.T) {
	body := `{"model":"m","input":[{"type":"custom_tool_call","call_id":"c1","name":"apply_patch","input":"*** Begin Patch"},{"type":"custom_tool_call_output","call_id":"c1","output":"done"}]}`
	converted := mustConvert(t, body, request.NewConversionOptions())
	messages := converted.Conversation.Messages
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	call := messages[0].ToolCalls[0]
	if call.ID != "c1" || call.Name != "apply_patch" {
		t.Errorf("call = %+v", call)
	}
	if call.Arguments != `{"input":"*** Begin Patch"}` {
		t.Errorf("arguments = %q", call.Arguments)
	}
	if messages[1].Kind != core.MessageTool || messages[1].Content != "done" {
		t.Errorf("tool result = %+v", messages[1])
	}

	// An output field that is absent carries empty content.
	converted = mustConvert(t, `{"model":"m","input":[{"type":"custom_tool_call","call_id":"c1","name":"apply_patch","input":"x"},{"type":"custom_tool_call_output","call_id":"c1"}]}`, request.NewConversionOptions())
	messages = converted.Conversation.Messages
	if len(messages) != 2 || messages[1].Kind != core.MessageTool || messages[1].Content != "" {
		t.Errorf("messages = %+v, want an empty tool result", messages)
	}

	// An unresolved custom tool call is rejected.
	pending := `{"model":"m","input":[{"type":"custom_tool_call","call_id":"c1","name":"apply_patch","input":"x"}]}`
	if detail := conversionError(t, pending, request.NewConversionOptions()); detail != "No tool output found for tool call c1." {
		t.Errorf("error = %q", detail)
	}
}
func TestCustomToolInputParser(t *testing.T) {
	newline := string(rune(10))
	tab := string(rune(9))
	cases := []struct {
		name   string
		chunks []string
		want   string
	}{
		{"plain", []string{`{"input":"abc"}`}, "abc"},
		{"split prefix", []string{`{"in`, `put":"x"}`}, "x"},
		{"split value", []string{`{"input":"a`, `b"}`}, "ab"},
		{"trailing content", []string{`{"input":"a"}trailing`}, "a"},
		{"escapes", []string{`{"input":"a\nb\t\"c"}`}, "a" + newline + "b" + tab + "\"" + `c`},
		{"unicode escape", []string{`{"input":"\u0041"}`}, "A"},
		{"split surrogate pair", []string{`{"input":"\ud83d`, `\ude00"}`}, "😀"},
		{"passthrough", []string{`{"other":1}`}, `{"other":1}`},
		{"leading whitespace passthrough", []string{"  " + `{"other":1}`}, "  " + `{"other":1}`},
		{"invalid escape kept", []string{`{"input":"\q"}`}, `\q`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parser := &customToolInputParser{}
			var output strings.Builder
			for _, chunk := range testCase.chunks {
				output.WriteString(parser.feed(chunk))
			}
			if got := output.String(); got != testCase.want {
				t.Errorf("output = %q, want %q", got, testCase.want)
			}
			if got := parser.finish(); got != "" {
				t.Errorf("finish = %q, want empty", got)
			}
			if got := parser.feed("more"); got != "" {
				t.Errorf("feed after finish = %q, want empty", got)
			}
			if got := parser.finish(); got != "" {
				t.Errorf("second finish = %q, want empty", got)
			}
		})
	}
}

func TestCustomToolInputParserFinish(t *testing.T) {
	// An unfinished escape is returned unchanged by finish.
	parser := &customToolInputParser{}
	if got := parser.feed(`{"input":"a\`); got != "a" {
		t.Errorf("feed = %q, want a", got)
	}
	if got := parser.finish(); got != `\` {
		t.Errorf("finish = %q, want a backslash", got)
	}
	if got := parser.feed("more"); got != "" {
		t.Errorf("feed after finish = %q, want empty", got)
	}

	// An unfinished prefix is returned unchanged by finish.
	parser = &customToolInputParser{}
	if got := parser.feed(`{"input":`); got != "" {
		t.Errorf("feed = %q, want empty", got)
	}
	if got := parser.finish(); got != `{"input":` {
		t.Errorf("finish = %q, want the pending prefix", got)
	}
}

// normalizeTimestamps replaces response timestamps with zero so tests do not
// depend on the clock.
func normalizeTimestamps(t *testing.T, text string) string {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatalf("unmarshal %s: %v", text, err)
	}
	normalizeTimestampsValue(value)
	encoded, err := jsonx.MarshalCompact(value)
	if err != nil {
		t.Fatalf("marshal %v: %v", value, err)
	}
	return string(encoded)
}

// normalizeTimestampsValue zeroes created_at and completed_at members.
func normalizeTimestampsValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if key == "created_at" || key == "completed_at" {
				typed[key] = 0
				continue
			}
			normalizeTimestampsValue(item)
		}
	case []any:
		for _, item := range typed {
			normalizeTimestampsValue(item)
		}
	}
}

// stringPointer returns a pointer to value.
func stringPointer(value string) *string { return &value }

func TestStreamAndAccumulateText(t *testing.T) {
	converted := mustConvert(t, `{"model":"m","input":"q","reasoning":{"effort":"none"}}`, request.NewConversionOptions())
	generator := converted.NewChunkGenerator("mock-id", "deepseek-flash")
	var events []stream.Event
	events = append(events, generator.Generate(stream.StartChunk{
		Usage: stream.PromptUsage{PromptTokens: 4},
	})...)
	events = append(events, generator.Generate(stream.RawChunk{Content: "Hello"})...)
	events = append(events, generator.Generate(stream.RawChunk{Content: " there!"})...)
	events = append(events, generator.Generate(stream.FinishChunk{
		Reason: stream.FinishStop,
		Usage:  stream.CompletionUsage{CompletionTokens: 3},
	})...)

	wantNames := []string{
		"response.created",
		"response.in_progress",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
		"response.output_text.delta",
		"response.output_text.done",
		"response.content_part.done",
		"response.output_item.done",
		"response.completed",
	}
	if len(events) != len(wantNames) {
		t.Fatalf("events = %d, want %d: %v", len(events), len(wantNames), eventNames(events))
	}
	for index, event := range events {
		if got := event.EventName(); got != wantNames[index] {
			t.Errorf("event %d = %q, want %q", index, got, wantNames[index])
		}
	}

	// The added item carries no content part yet.
	added, ok := events[2].(ResponsesOutputItemAddedEvent)
	if !ok {
		t.Fatalf("event 2 = %T, want ResponsesOutputItemAddedEvent", events[2])
	}
	addedMessage, ok := added.Item.(*ResponsesMessageOutputItem)
	if !ok || len(addedMessage.Content) != 0 {
		t.Errorf("added item = %+v, want a message without content", added.Item)
	}

	response := NewResponsesResponse("mock-id", "deepseek-flash", 0)
	for _, event := range events {
		response.Append(event)
	}
	got := normalizeTimestamps(t, marshalJSON(t, response))
	want := `{"id":"mock-id","object":"response","created_at":0,"model":"deepseek-flash","status":"completed","completed_at":0,"error":null,"incomplete_details":null,"output":[{"type":"message","id":"msg_mock-id_0","status":"completed","role":"assistant","phase":"final_answer","content":[{"type":"output_text","annotations":[],"logprobs":[],"text":"Hello there!"}]}],"usage":{"input_tokens":4,"input_tokens_details":{"cached_tokens":0},"output_tokens":3,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":7}}`
	if got != normalizeTimestamps(t, want) {
		t.Errorf("accumulated response = %s, want %s", got, want)
	}

	// Without the final event the deltas still reconstruct the text.
	partial := NewResponsesResponse("mock-id", "deepseek-flash", 0)
	for _, event := range events[:len(events)-1] {
		partial.Append(event)
	}
	item, ok := partial.Output[0].(*ResponsesMessageOutputItem)
	if !ok {
		t.Fatalf("output item = %T", partial.Output[0])
	}
	part, ok := item.Content[0].(ResponsesOutputTextPart)
	if !ok || part.Text != "Hello there!" {
		t.Errorf("accumulated part = %+v, want Hello there!", item.Content[0])
	}
	if partial.Status != ResponsesStatusInProgress || partial.Usage != nil {
		t.Errorf("partial response = %+v, want in progress without usage", partial)
	}
}

// TestStreamReasoningSequenceMatchesReference replays the reference golden case
// stream_resp_text: the request has no supported thinking field, so the
// default thinking mode keeps reasoning enabled and the parsed text is
// reasoning content.
func TestStreamReasoningSequenceMatchesReference(t *testing.T) {
	converted := mustConvert(t, `{"model":"m","input":"q","thinking":{"type":"disabled"}}`, request.NewConversionOptions())
	generator := converted.NewChunkGenerator("mock-id", "deepseek-flash")
	processor := stream.NewProcessor(generator, converted.ParsingOptions)
	var events []stream.Event
	chunks := []stream.InferenceChunk{
		stream.NewReadyChunk(stringPointer("fp-1"), stream.PromptUsage{PromptTokens: 4, PromptCacheHitTokens: 0}),
		stream.NewTextChunk("Hello there!", 3),
		stream.NewFinishChunk(stream.InferenceFinishStop),
	}
	for _, chunk := range chunks {
		pushed, err := processor.Push(chunk)
		if err != nil {
			t.Fatalf("push %+v: %v", chunk, err)
		}
		events = append(events, pushed...)
	}
	events = append(events, processor.Finish()...)

	response := `{"id":"mock-id","object":"response","created_at":0,"model":"deepseek-flash","status":"in_progress","completed_at":null,"error":null,"incomplete_details":null,"output":[],"usage":null}`
	finalResponse := `{"id":"mock-id","object":"response","created_at":0,"model":"deepseek-flash","status":"completed","completed_at":0,"error":null,"incomplete_details":null,"output":[{"type":"reasoning","id":"rs_mock-id_0","status":"completed","content":[{"type":"reasoning_text","text":"Hello there!"}],"summary":[]}],"usage":{"input_tokens":4,"input_tokens_details":{"cached_tokens":0},"output_tokens":3,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":7}}`
	want := []string{
		`{"type":"response.created","response":` + response + `,"sequence_number":0}`,
		`{"type":"response.in_progress","response":` + response + `,"sequence_number":1}`,
		`{"type":"response.output_item.added","item":{"type":"reasoning","id":"rs_mock-id_0","status":"in_progress","content":[],"summary":[]},"output_index":0,"sequence_number":2}`,
		`{"type":"response.content_part.added","content_index":0,"item_id":"rs_mock-id_0","output_index":0,"part":{"type":"reasoning_text","text":""},"sequence_number":3}`,
		`{"type":"response.reasoning_text.delta","content_index":0,"delta":"Hello there!","item_id":"rs_mock-id_0","output_index":0,"sequence_number":4}`,
		`{"type":"response.reasoning_text.done","content_index":0,"item_id":"rs_mock-id_0","output_index":0,"sequence_number":5,"text":"Hello there!"}`,
		`{"type":"response.content_part.done","content_index":0,"item_id":"rs_mock-id_0","output_index":0,"part":{"type":"reasoning_text","text":"Hello there!"},"sequence_number":6}`,
		`{"type":"response.output_item.done","item":{"type":"reasoning","id":"rs_mock-id_0","status":"completed","content":[{"type":"reasoning_text","text":"Hello there!"}],"summary":[]},"output_index":0,"sequence_number":7}`,
		`{"type":"response.completed","response":` + finalResponse + `,"sequence_number":8}`,
	}
	if len(events) != len(want) {
		t.Fatalf("events = %d, want %d: %v", len(events), len(want), eventNames(events))
	}
	for index, event := range events {
		got := normalizeTimestamps(t, marshalJSON(t, event))
		expected := normalizeTimestamps(t, want[index])
		if got != expected {
			t.Errorf("event %d = %s, want %s", index, got, expected)
		}
	}

	accumulated := NewResponsesResponse("mock-id", "deepseek-flash", 0)
	for _, event := range events {
		accumulated.Append(event)
	}
	got := normalizeTimestamps(t, marshalJSON(t, accumulated))
	if expected := normalizeTimestamps(t, finalResponse); got != expected {
		t.Errorf("accumulated response = %s, want %s", got, expected)
	}
}

// eventNames returns the event names of a collected event list.
func eventNames(events []stream.Event) []string {
	names := make([]string, 0, len(events))
	for _, event := range events {
		names = append(names, event.EventName())
	}
	return names
}

func TestCustomToolCallStreaming(t *testing.T) {
	body := `{"model":"m","input":"q","reasoning":{"effort":"none"},"tools":[{"type":"custom","name":"apply_patch"}]}`
	parsed := mustRequest(t, body)
	if _, err := parsed.Convert(request.NewConversionOptions()); err != nil {
		t.Fatalf("convert: %v", err)
	}
	generator := NewChunkGenerator("mock-id", "deepseek-flash").WithCustomToolNames(parsed.CustomToolNames())
	var events []stream.Event
	events = append(events, generator.Generate(stream.StartChunk{
		Usage: stream.PromptUsage{PromptTokens: 9, PromptCacheHitTokens: 2},
	})...)
	events = append(events, generator.Generate(stream.ToolCallChunk{ToolName: "apply_patch"})...)
	events = append(events, generator.Generate(stream.ToolArgumentsDeltaChunk{Content: `{"input":"line1`})...)
	events = append(events, generator.Generate(stream.ToolArgumentsDeltaChunk{Content: `\nline2"}`})...)
	events = append(events, generator.Generate(stream.FinishChunk{
		Reason: stream.FinishToolCalls,
		Usage:  stream.CompletionUsage{CompletionTokens: 4},
	})...)
	wantNames := []string{
		"response.created",
		"response.in_progress",
		"response.output_item.added",
		"response.custom_tool_call_input.delta",
		"response.custom_tool_call_input.delta",
		"response.custom_tool_call_input.done",
		"response.output_item.done",
		"response.completed",
	}
	if len(events) != len(wantNames) {
		t.Fatalf("events = %d, want %d: %v", len(events), len(wantNames), eventNames(events))
	}
	for index, event := range events {
		if got := event.EventName(); got != wantNames[index] {
			t.Errorf("event %d = %q, want %q", index, got, wantNames[index])
		}
	}
	added, ok := events[2].(ResponsesOutputItemAddedEvent)
	if !ok {
		t.Fatalf("event 2 = %T", events[2])
	}
	addedCall, ok := added.Item.(*ResponsesCustomToolCallOutputItem)
	if !ok {
		t.Fatalf("added item = %T, want *ResponsesCustomToolCallOutputItem", added.Item)
	}
	if addedCall.Name != "apply_patch" || addedCall.CallID != "call_mock-id_0" || addedCall.Input != "" {
		t.Errorf("added call = %+v", addedCall)
	}
	delta, ok := events[3].(ResponsesCustomToolCallInputDeltaEvent)
	if !ok || delta.Delta != "line1" {
		t.Errorf("first delta = %+v, want line1", events[3])
	}
	delta, ok = events[4].(ResponsesCustomToolCallInputDeltaEvent)
	if !ok || delta.Delta != "\nline2" {
		t.Errorf("second delta = %+v, want a newline and line2", events[4])
	}

	response := NewResponsesResponse("mock-id", "deepseek-flash", 0)
	for _, event := range events {
		response.Append(event)
	}
	if len(response.Output) != 1 {
		t.Fatalf("output = %d, want 1", len(response.Output))
	}
	call, ok := response.Output[0].(*ResponsesCustomToolCallOutputItem)
	if !ok {
		t.Fatalf("output item = %T", response.Output[0])
	}
	if call.Input != "line1\nline2" || call.Status != ResponsesStatusCompleted {
		t.Errorf("call = %+v, want the decoded input", call)
	}
	if response.Usage == nil || response.Usage.InputTokens != 9 ||
		response.Usage.InputTokensDetails.CachedTokens != 2 ||
		response.Usage.OutputTokens != 4 || response.Usage.TotalTokens != 13 {
		t.Errorf("usage = %+v", response.Usage)
	}

	// Without custom tool names the same output is a function call.
	plain := NewChunkGenerator("mock-id", "deepseek-flash")
	plain.Generate(stream.StartChunk{})
	plainEvents := plain.Generate(stream.ToolCallChunk{ToolName: "apply_patch", Arguments: `{"input":"x"}`})
	if len(plainEvents) != 1 {
		t.Fatalf("plain events = %v", eventNames(plainEvents))
	}
	plainAdded, ok := plainEvents[0].(ResponsesOutputItemAddedEvent)
	if !ok {
		t.Fatalf("plain event = %T", plainEvents[0])
	}
	if _, ok := plainAdded.Item.(*ResponsesFunctionCallOutputItem); !ok {
		t.Errorf("plain item = %T, want *ResponsesFunctionCallOutputItem", plainAdded.Item)
	}
}

func TestFunctionCallStreaming(t *testing.T) {
	generator := NewChunkGenerator("mock-id", "deepseek-flash")
	generator.Generate(stream.StartChunk{})
	events := generator.Generate(stream.ToolCallChunk{ToolName: "files::read", Arguments: `{}`})
	if len(events) != 1 {
		t.Fatalf("events = %v", eventNames(events))
	}
	added, ok := events[0].(ResponsesOutputItemAddedEvent)
	if !ok {
		t.Fatalf("event = %T", events[0])
	}
	call, ok := added.Item.(*ResponsesFunctionCallOutputItem)
	if !ok {
		t.Fatalf("item = %T", added.Item)
	}
	if call.ID != "fc_mock-id_0" || call.CallID != "call_mock-id_0" || call.Name != "read" {
		t.Errorf("call = %+v", call)
	}
	if call.Namespace == nil || *call.Namespace != "files" {
		t.Errorf("namespace = %v, want files", call.Namespace)
	}
	if call.Arguments != "{}" {
		t.Errorf("arguments = %q, want {}", call.Arguments)
	}

	deltas := generator.Generate(stream.ToolArgumentsDeltaChunk{Content: `"x"`})
	if len(deltas) != 1 {
		t.Fatalf("delta events = %v", eventNames(deltas))
	}
	delta, ok := deltas[0].(ResponsesFunctionCallArgumentsDeltaEvent)
	if !ok || delta.Delta != `"x"` || delta.ItemID != "fc_mock-id_0" {
		t.Errorf("delta = %+v", deltas[0])
	}

	// Answer text before a tool call marks the message as commentary.
	generator = NewChunkGenerator("mock-id", "deepseek-flash")
	var all []stream.Event
	all = append(all, generator.Generate(stream.StartChunk{})...)
	all = append(all, generator.Generate(stream.RawChunk{Content: "thinking out loud"})...)
	all = append(all, generator.Generate(stream.ToolCallChunk{ToolName: "f"})...)
	all = append(all, generator.Generate(stream.FinishChunk{Reason: stream.FinishToolCalls})...)
	response := NewResponsesResponse("mock-id", "deepseek-flash", 0)
	for _, event := range all {
		response.Append(event)
	}
	if len(response.Output) != 2 {
		t.Fatalf("output = %d, want 2", len(response.Output))
	}
	message, ok := response.Output[0].(*ResponsesMessageOutputItem)
	if !ok {
		t.Fatalf("first item = %T", response.Output[0])
	}
	if message.Phase != ResponsesPhaseCommentary || message.Status != ResponsesStatusCompleted {
		t.Errorf("message = %+v, want commentary and completed", message)
	}
}

func TestIncompleteFinishReason(t *testing.T) {
	generator := NewChunkGenerator("mock-id", "deepseek-flash")
	generator.Generate(stream.StartChunk{})
	generator.Generate(stream.RawChunk{Content: "partial"})
	events := generator.Generate(stream.FinishChunk{
		Reason: stream.FinishLength,
		Usage:  stream.CompletionUsage{CompletionTokens: 2},
	})
	if len(events) == 0 {
		t.Fatal("no events")
	}
	incomplete, ok := events[len(events)-1].(ResponsesIncompleteEvent)
	if !ok {
		t.Fatalf("last event = %T, want ResponsesIncompleteEvent", events[len(events)-1])
	}
	if incomplete.Response.Status != ResponsesStatusIncomplete {
		t.Errorf("status = %q, want incomplete", incomplete.Response.Status)
	}
	if incomplete.Response.IncompleteDetails == nil ||
		incomplete.Response.IncompleteDetails.Reason != ResponsesIncompleteReasonMaxOutputTokens {
		t.Errorf("incomplete details = %+v", incomplete.Response.IncompleteDetails)
	}

	// Content filter stops are incomplete as well.
	filtered := NewChunkGenerator("mock-id", "deepseek-flash")
	filtered.Generate(stream.StartChunk{})
	filteredEvents := filtered.Generate(stream.FinishChunk{Reason: stream.FinishContentFilter})
	last, ok := filteredEvents[len(filteredEvents)-1].(ResponsesIncompleteEvent)
	if !ok || last.Response.IncompleteDetails.Reason != ResponsesIncompleteReasonContentFilter {
		t.Errorf("content filter event = %+v", filteredEvents[len(filteredEvents)-1])
	}
}

func TestGeneratorIgnoresChunksOutsideTheSequence(t *testing.T) {
	generator := NewChunkGenerator("mock-id", "deepseek-flash")
	if events := generator.Generate(stream.RawChunk{Content: "before start"}); len(events) != 0 {
		t.Errorf("pre-start events = %v", eventNames(events))
	}
	generator.Generate(stream.StartChunk{})
	if events := generator.Generate(stream.StartChunk{}); len(events) != 0 {
		t.Errorf("repeated start events = %v", eventNames(events))
	}
	generator.Generate(stream.FinishChunk{Reason: stream.FinishStop})
	if events := generator.Generate(stream.RawChunk{Content: "after finish"}); len(events) != 0 {
		t.Errorf("post-finish events = %v", eventNames(events))
	}
	if events := generator.Generate(stream.ToolCallBeginChunk{}); len(events) != 0 {
		t.Errorf("begin events = %v", eventNames(events))
	}
}
