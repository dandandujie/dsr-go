package messages

import (
	"encoding/json"
	"testing"

	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/request"
	"github.com/dandandujie/dsr-go/recipe/response"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// Compile-time checks for the interfaces this package implements.
var (
	_ request.ProtocolRequest   = (*MessagesRequest)(nil)
	_ response.ProtocolResponse = (*MessagesResponse)(nil)
	_ stream.Generator          = (*MessagesChunkGenerator)(nil)
)

// convertBody decodes and converts a request body.
func convertBody(t *testing.T, body string, options request.ConversionOptions) *request.ConversationRequest {
	t.Helper()
	var typed MessagesRequest
	if err := json.Unmarshal([]byte(body), &typed); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	converted, err := typed.Convert(options)
	if err != nil {
		t.Fatalf("convert request: %v", err)
	}
	return converted
}

// convertFailure decodes a body and returns its conversion error.
func convertFailure(t *testing.T, body string, options request.ConversionOptions) error {
	t.Helper()
	var typed MessagesRequest
	if err := json.Unmarshal([]byte(body), &typed); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	converted, err := typed.Convert(options)
	if err == nil {
		t.Fatalf("expected a conversion error, got %+v", converted)
	}
	return err
}

// requireDetail asserts a bad-request conversion error with the given detail.
func requireDetail(t *testing.T, err error, want string) {
	t.Helper()
	conversion, ok := err.(*request.ConversionError)
	if !ok {
		t.Fatalf("error type = %T, want *request.ConversionError", err)
	}
	if conversion.Kind != request.ConversionBadRequest {
		t.Errorf("error kind = %v, want bad request", conversion.Kind)
	}
	if conversion.Detail != want {
		t.Errorf("error detail = %q, want %q", conversion.Detail, want)
	}
}

// requireMessages asserts the converted message contents.
func requireMessages(t *testing.T, converted *request.ConversationRequest, want []string) {
	t.Helper()
	messages := converted.Conversation.Messages
	if len(messages) != len(want) {
		t.Fatalf("message count = %d, want %d (%+v)", len(messages), len(want), messages)
	}
	for index, message := range messages {
		if message.Content != want[index] {
			t.Errorf("message %d content = %q, want %q", index, message.Content, want[index])
		}
	}
}

func TestConvertSimpleText(t *testing.T) {
	converted := convertBody(t,
		`{"model":"deepseek-flash","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}`,
		request.NewConversionOptions())

	conversation := converted.Conversation
	requireMessages(t, converted, []string{"Hello"})
	if conversation.Messages[0].Kind != core.MessageUser {
		t.Errorf("message kind = %v, want user", conversation.Messages[0].Kind)
	}
	if len(conversation.Messages[0].ImageSources) != 0 {
		t.Errorf("image sources = %d, want 0", len(conversation.Messages[0].ImageSources))
	}
	if !conversation.ThinkingMode {
		t.Error("thinking mode = false, want true")
	}
	if conversation.ReasoningEffort == nil || *conversation.ReasoningEffort != core.ReasoningEffortHigh {
		t.Errorf("reasoning effort = %v, want high", conversation.ReasoningEffort)
	}
	if conversation.ToolChoice != core.ToolChoiceAuto {
		t.Errorf("tool choice = %v, want auto", conversation.ToolChoice)
	}
	if len(conversation.Tools) != 0 {
		t.Errorf("tools = %d, want 0", len(conversation.Tools))
	}
	if converted.Model == nil || *converted.Model != "deepseek-flash" {
		t.Errorf("model = %v, want deepseek-flash", converted.Model)
	}
	if converted.Stream {
		t.Error("stream = true, want false")
	}
	if converted.InferenceOptions.MaxTokens == nil || *converted.InferenceOptions.MaxTokens != 1024 {
		t.Errorf("max tokens = %v, want 1024", converted.InferenceOptions.MaxTokens)
	}
	if converted.ParsingOptions.ParseToolCalls {
		t.Error("parse tool calls = true, want false")
	}
	if converted.ParsingOptions.ToolCallInitialStage {
		t.Error("tool call initial stage = true, want false")
	}
	if converted.ParsingOptions.ReasoningInitialStage == nil ||
		*converted.ParsingOptions.ReasoningInitialStage != stream.ReasoningStageStart {
		t.Errorf("reasoning initial stage = %v, want start", converted.ParsingOptions.ReasoningInitialStage)
	}
	if converted.ParsingOptions.StopSequences == nil {
		t.Error("stop sequences = nil, want an empty slice")
	}
	if converted.NewChunkGenerator == nil {
		t.Error("new chunk generator = nil")
	}
}

func TestConvertSystemPrompt(t *testing.T) {
	base := `"model":"m","max_tokens":100,"messages":[{"role":"user","content":"Hi"}]`
	tests := []struct {
		name   string
		system string
		want   []string
	}{
		{
			name:   "string",
			system: `"system":"You are helpful.",`,
			want:   []string{"You are helpful.", "Hi"},
		},
		{
			name:   "blocks",
			system: `"system":[{"type":"text","text":"System A"},{"type":"text","text":"System B"}],`,
			want:   []string{"System A\n\nSystem B", "Hi"},
		},
		{
			name:   "tool reference and document",
			system: `"system":[{"type":"tool_reference","tool_name":"get_weather"},{"type":"document"}],`,
			want:   []string{"get_weather\n\n[Unsupported Document]", "Hi"},
		},
		{
			name:   "empty blocks",
			system: `"system":[],`,
			want:   []string{"", "Hi"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			converted := convertBody(t, "{"+test.system+base+"}", request.NewConversionOptions())
			requireMessages(t, converted, test.want)
			if converted.Conversation.Messages[0].Kind != core.MessageSystem {
				t.Errorf("first message kind = %v, want system", converted.Conversation.Messages[0].Kind)
			}
		})
	}

	t.Run("image rejected", func(t *testing.T) {
		body := `{"model":"m","max_tokens":100,"system":[{"type":"image","source":{"type":"url","url":"https://example.com/a.png"}}],"messages":[{"role":"user","content":"Hi"}]}`
		requireDetail(t, convertFailure(t, body, request.NewConversionOptions()),
			"Image in system message is unsupported")
	})
}

func TestConvertSystemMessageRole(t *testing.T) {
	converted := convertBody(t,
		`{"model":"m","max_tokens":100,"messages":[{"role":"system","content":"be nice"},{"role":"user","content":"Hi"}]}`,
		request.NewConversionOptions())
	requireMessages(t, converted, []string{"<system-reminder>\nbe nice\n</system-reminder>", "Hi"})
	if converted.Conversation.Messages[0].Kind != core.MessageUser {
		t.Errorf("system-reminder kind = %v, want user", converted.Conversation.Messages[0].Kind)
	}
}

func TestConvertContentBlocksWithImages(t *testing.T) {
	converted := convertBody(t,
		`{"model":"m","max_tokens":100,"messages":[{"role":"user","content":[
			{"type":"text","text":"What is this?"},
			{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}},
			{"type":"image","source":{"type":"url","url":"https://example.com/a.png"}},
			{"type":"document"}
		]}]}`, request.NewConversionOptions())
	requireMessages(t, converted, []string{
		"What is this?", core.ImageSpecialToken, core.ImageSpecialToken, "[Unsupported Document]",
	})
	messages := converted.Conversation.Messages
	if len(messages[1].ImageSources) != 1 || messages[1].ImageSources[0].Kind != core.ImageSourceDataURL {
		t.Fatalf("inline image sources = %+v", messages[1].ImageSources)
	}
	if messages[1].ImageSources[0].DataURL != "data:image/png;base64,iVBORw0KGgo=" {
		t.Errorf("inline image data url = %q", messages[1].ImageSources[0].DataURL)
	}
	if len(messages[2].ImageSources) != 1 || messages[2].ImageSources[0].Kind != core.ImageSourceURL {
		t.Fatalf("url image sources = %+v", messages[2].ImageSources)
	}
	if messages[2].ImageSources[0].URL != "https://example.com/a.png" {
		t.Errorf("url image source = %q", messages[2].ImageSources[0].URL)
	}
	if messages[2].ImageSources[0].Detail != core.ImageDetailHigh {
		t.Errorf("image detail = %v, want high", messages[2].ImageSources[0].Detail)
	}
}

func TestConvertContentBlockErrors(t *testing.T) {
	options := request.NewConversionOptions()
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "image in assistant message",
			body: `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"q"},{"role":"assistant","content":[{"type":"image","source":{"type":"url","url":"https://example.com/a.png"}}]}]}`,
			want: "Image in assistant message is unsupported",
		},
		{
			name: "tool use in user message",
			body: `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"tool_use","id":"t1","name":"n","input":{}}]}]}`,
			want: "messages.0: `tool_use` blocks can only be in `assistant` messages",
		},
		{
			name: "thinking in user message",
			body: `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"thinking","thinking":"t"}]}]}`,
			want: "messages.0.content: thinking blocks may only be in `assistant` messages",
		},
		{
			name: "server tool use in user message",
			body: `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"server_tool_use","id":"s1","name":"web_search","input":{}}]}]}`,
			want: "messages.0: `server_tool_use` blocks can only be in `assistant` messages",
		},
		{
			name: "web search result in user message",
			body: `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"web_search_tool_result","tool_use_id":"s1","content":[]}]}]}`,
			want: "messages.0: `web_search_tool_result` blocks can only be in `assistant` messages",
		},
		{
			name: "tool result in assistant message",
			body: `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"q"},{"role":"assistant","content":[{"type":"tool_result","tool_use_id":"t1","content":"x"}]}]}`,
			want: "messages.1: `tool_result` blocks can only be in `user` messages",
		},
		{
			name: "unknown block",
			body: `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"mystery"}]}]}`,
			want: "Unsupported content block",
		},
		{
			name: "empty content blocks",
			body: `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[]}]}`,
			want: "messages.0: all messages must have non-empty content",
		},
		{
			name: "empty messages",
			body: `{"model":"m","max_tokens":10,"messages":[]}`,
			want: "messages: at least one message is required",
		},
		{
			name: "file id image",
			body: `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"f1"}}]}]}`,
			want: "messages.0.content[0]: file_id is not supported. Pass the image as a base64 data url instead.",
		},
		{
			name: "unsupported media type",
			body: `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/svg+xml","data":"aGk="}}]}]}`,
			want: "messages.0.content[0]: You have uploaded an unsupported image. Please make sure your image is valid and has one of the following formats: webp, png, jpeg, and gif.",
		},
		{
			name: "invalid image url",
			body: `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"ftp://example.com/a.png"}}]}]}`,
			want: "messages.0.content[0]: invalid url: \"ftp://example.com/a.png\"",
		},
		{
			name: "image special token",
			body: `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"a<｜image｜>b"}]}`,
			want: "The sub-string \"<｜image｜>\" in your input is not allowed for this API. Please remove \"<｜image｜>\" from your input and try again.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requireDetail(t, convertFailure(t, test.body, options), test.want)
		})
	}
}

func TestConvertToolHistory(t *testing.T) {
	converted := convertBody(t,
		`{"model":"m","max_tokens":100,"messages":[
			{"role":"user","content":"Weather?"},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"Need tool."},
				{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"location":"Paris","days":2}}
			]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"20C"}]}
		],"tools":[{"name":"get_weather","input_schema":{"type":"object"}}]}`,
		request.NewConversionOptions())
	requireMessages(t, converted, []string{"Weather?", "", "20C"})
	messages := converted.Conversation.Messages
	assistant := messages[1]
	if assistant.Kind != core.MessageAssistant {
		t.Fatalf("assistant kind = %v", assistant.Kind)
	}
	if assistant.ReasoningContent != "Need tool." {
		t.Errorf("reasoning content = %q", assistant.ReasoningContent)
	}
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(assistant.ToolCalls))
	}
	if assistant.ToolCalls[0].Arguments != `{"location": "Paris", "days": 2}` {
		t.Errorf("tool arguments = %q", assistant.ToolCalls[0].Arguments)
	}
	tool := messages[2]
	if tool.Kind != core.MessageTool || tool.ToolCallID != "toolu_1" {
		t.Errorf("tool message = %+v", tool)
	}
}

func TestConvertUnresolvedToolUse(t *testing.T) {
	options := request.NewConversionOptions()
	body := `{"model":"m","max_tokens":10,"messages":[
		{"role":"user","content":"q"},
		{"role":"assistant","content":[
			{"type":"tool_use","id":"b","name":"n","input":{}},
			{"type":"tool_use","id":"a","name":"n","input":{}}
		]},
		{"role":"user","content":"done"}
	],"tools":[{"name":"n","input_schema":{"type":"object"}}]}`
	requireDetail(t, convertFailure(t, body, options),
		"messages.2: `tool_use` ids were found without `tool_result` blocks immediately after: a, b. Each `tool_use` block must have a corresponding `tool_result` block in the next message.")

	unexpected := `{"model":"m","max_tokens":10,"messages":[
		{"role":"user","content":"q"},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"x"}]}
	]}`
	requireDetail(t, convertFailure(t, unexpected, options),
		"unexpected `messages.1.content.0: tool_use_id` found in `tool_result` blocks: t1. Each `tool_result` block must have a corresponding `tool_use` block in the previous message.")
}

func TestConvertToolsAndToolChoice(t *testing.T) {
	options := request.NewConversionOptions()
	noThinking := `"thinking":{"type":"disabled"},`
	twoTools := `"tools":[
		{"name":"get_weather","description":"w","input_schema":{"type":"object","properties":{"location":{"type":"string"}}}},
		{"name":"get_time","input_schema":{"type":"object"}}
	],`

	t.Run("auto", func(t *testing.T) {
		converted := convertBody(t, `{"model":"m","max_tokens":10,`+noThinking+twoTools+
			`"messages":[{"role":"user","content":"q"}],"tool_choice":{"type":"auto"}}`, options)
		tools := converted.Conversation.Tools
		if len(tools) != 2 {
			t.Fatalf("tools = %d, want 2", len(tools))
		}
		if tools[0].Name != "get_weather" || tools[0].Description == nil || *tools[0].Description != "w" {
			t.Errorf("tool 0 = %+v", tools[0])
		}
		parameters, err := jsonx.Marshal(tools[0].Parameters)
		if err != nil {
			t.Fatal(err)
		}
		if string(parameters) != `{"type":"object","properties":{"location":{"type":"string"}}}` {
			t.Errorf("tool parameters = %s", parameters)
		}
		if converted.Conversation.ToolChoice != core.ToolChoiceAuto {
			t.Errorf("tool choice = %v, want auto", converted.Conversation.ToolChoice)
		}
		if !converted.ParsingOptions.ParseToolCalls {
			t.Error("parse tool calls = false, want true")
		}
	})

	t.Run("any", func(t *testing.T) {
		converted := convertBody(t, `{"model":"m","max_tokens":10,`+noThinking+twoTools+
			`"messages":[{"role":"user","content":"q"}],"tool_choice":{"type":"any","disable_parallel_tool_use":true}}`, options)
		if converted.Conversation.ToolChoice != core.ToolChoiceAuto {
			t.Errorf("tool choice = %v, want auto", converted.Conversation.ToolChoice)
		}
		disable := converted.InferenceOptions.DisableParallelToolUse
		if disable == nil || !*disable {
			t.Errorf("disable parallel tool use = %v, want true", disable)
		}
	})

	t.Run("named", func(t *testing.T) {
		converted := convertBody(t, `{"model":"m","max_tokens":10,`+noThinking+twoTools+
			`"messages":[{"role":"user","content":"q"}],"tool_choice":{"type":"tool","name":"get_time"}}`, options)
		tools := converted.Conversation.Tools
		if len(tools) != 1 || tools[0].Name != "get_time" {
			t.Fatalf("tools = %+v, want only get_time", tools)
		}
		if converted.Conversation.ToolChoice != core.ToolChoiceRequired {
			t.Errorf("tool choice = %v, want required", converted.Conversation.ToolChoice)
		}
		if !converted.ParsingOptions.ToolCallInitialStage {
			t.Error("tool call initial stage = false, want true")
		}
	})

	t.Run("none", func(t *testing.T) {
		converted := convertBody(t, `{"model":"m","max_tokens":10,`+noThinking+twoTools+
			`"messages":[{"role":"user","content":"q"}],"tool_choice":{"type":"none"}}`, options)
		if len(converted.Conversation.Tools) != 0 {
			t.Errorf("tools = %d, want 0", len(converted.Conversation.Tools))
		}
		if converted.Conversation.ToolChoice != core.ToolChoiceNone {
			t.Errorf("tool choice = %v, want none", converted.Conversation.ToolChoice)
		}
		if converted.ParsingOptions.ParseToolCalls {
			t.Error("parse tool calls = true, want false")
		}
	})

	t.Run("named with thinking", func(t *testing.T) {
		body := `{"model":"m","max_tokens":10,` + twoTools +
			`"messages":[{"role":"user","content":"q"}],"tool_choice":{"type":"tool","name":"get_time"}}`
		requireDetail(t, convertFailure(t, body, options), "Thinking mode does not support this tool_choice")
	})

	t.Run("named unknown tool", func(t *testing.T) {
		body := `{"model":"m","max_tokens":10,` + noThinking + twoTools +
			`"messages":[{"role":"user","content":"q"}],"tool_choice":{"type":"tool","name":"nope"}}`
		requireDetail(t, convertFailure(t, body, options), "tool_choice: no tool named 'nope' was specified")
	})

	t.Run("duplicate names", func(t *testing.T) {
		body := `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"q"}],
			"tools":[{"name":"a","input_schema":{"type":"object"}},{"name":"a","input_schema":{"type":"object"}}]}`
		requireDetail(t, convertFailure(t, body, options), "Tool names must be unique")
	})

	t.Run("invalid name", func(t *testing.T) {
		body := `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"q"}],
			"tools":[{"name":"bad name!","input_schema":{"type":"object"}}]}`
		requireDetail(t, convertFailure(t, body, options),
			"tools.0.name: expected 1-128 ASCII letters, digits, underscores or hyphens")
	})

	t.Run("missing schema", func(t *testing.T) {
		body := `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"q"}],"tools":[{"name":"a"}]}`
		requireDetail(t, convertFailure(t, body, options), "tools.0: missing input_schema")

		nullSchema := `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"q"}],
			"tools":[{"name":"a","input_schema":null}]}`
		requireDetail(t, convertFailure(t, nullSchema, options), "tools.0: missing input_schema")
	})

	t.Run("invalid schema", func(t *testing.T) {
		body := `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"q"}],
			"tools":[{"name":"a","input_schema":{"type":"string"}}]}`
		requireDetail(t, convertFailure(t, body, options),
			"tools.0.input_schema must be a JSON Schema of type object")
	})
}

func TestConvertThinking(t *testing.T) {
	options := request.NewConversionOptions()
	base := `"model":"m","max_tokens":100,"messages":[{"role":"user","content":"Hi"}]`
	tests := []struct {
		name         string
		prefix       string
		options      request.ConversionOptions
		wantThinking bool
		wantEffort   *core.ReasoningEffort
		wantBudget   *uint64
	}{
		{
			name:         "default enabled",
			prefix:       "",
			options:      options,
			wantThinking: true,
			wantEffort:   effortPointer(core.ReasoningEffortHigh),
		},
		{
			name:         "default disabled",
			prefix:       "",
			options:      options.WithDefaultThinkingMode(false),
			wantThinking: false,
		},
		{
			name:         "explicitly disabled",
			prefix:       `"thinking":{"type":"disabled"},`,
			options:      options,
			wantThinking: false,
		},
		{
			name:         "enabled with budget",
			prefix:       `"thinking":{"type":"enabled","budget_tokens":2048},`,
			options:      options,
			wantThinking: true,
			wantEffort:   effortPointer(core.ReasoningEffortHigh),
			wantBudget:   uint64Pointer(2048),
		},
		{
			name:         "adaptive alias",
			prefix:       `"thinking":{"type":"adaptive"},`,
			options:      options,
			wantThinking: true,
			wantEffort:   effortPointer(core.ReasoningEffortHigh),
		},
		{
			name:         "disabled passes budget through",
			prefix:       `"thinking":{"type":"disabled","budget_tokens":77},`,
			options:      options,
			wantThinking: false,
			wantBudget:   uint64Pointer(77),
		},
		{
			name:         "explicit disabled overrides effort",
			prefix:       `"output_config":{"effort":"max"},"thinking":{"type":"disabled"},`,
			options:      options,
			wantThinking: false,
		},
		{
			name:         "effort enables thinking",
			prefix:       `"output_config":{"effort":"max"},`,
			options:      options.WithDefaultThinkingMode(false),
			wantThinking: true,
			wantEffort:   effortPointer(core.ReasoningEffortMax),
		},
		{
			name:         "effort low",
			prefix:       `"output_config":{"effort":"low"},`,
			options:      options,
			wantThinking: true,
			wantEffort:   effortPointer(core.ReasoningEffortLow),
		},
		{
			name:         "effort medium maps to high",
			prefix:       `"output_config":{"effort":"medium"},`,
			options:      options,
			wantThinking: true,
			wantEffort:   effortPointer(core.ReasoningEffortHigh),
		},
		{
			name:         "effort xhigh",
			prefix:       `"output_config":{"effort":"xhigh"},`,
			options:      options,
			wantThinking: true,
			wantEffort:   effortPointer(core.ReasoningEffortXhigh),
		},
		{
			name:         "effort ultra maps to max",
			prefix:       `"output_config":{"effort":"ultra"},`,
			options:      options,
			wantThinking: true,
			wantEffort:   effortPointer(core.ReasoningEffortMax),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			converted := convertBody(t, "{"+test.prefix+base+"}", test.options)
			conversation := converted.Conversation
			if conversation.ThinkingMode != test.wantThinking {
				t.Errorf("thinking mode = %v, want %v", conversation.ThinkingMode, test.wantThinking)
			}
			requireEffort(t, conversation.ReasoningEffort, test.wantEffort)
			requireUint64(t, converted.InferenceOptions.ThinkingBudgetTokens, test.wantBudget)
			reasoningStage := converted.ParsingOptions.ReasoningInitialStage
			if test.wantThinking {
				if reasoningStage == nil || *reasoningStage != stream.ReasoningStageStart {
					t.Errorf("reasoning initial stage = %v, want start", reasoningStage)
				}
			} else if reasoningStage != nil {
				t.Errorf("reasoning initial stage = %v, want nil", reasoningStage)
			}
		})
	}
}

func TestConvertWebSearch(t *testing.T) {
	reject := request.NewConversionOptions()
	ignore := request.NewConversionOptions().WithMessagesWebSearch(request.WebSearchIgnore)
	serverTool := `"tools":[{"type":"web_search_20250305","name":"web_search","max_uses":3}],`
	user := `"messages":[{"role":"user","content":"Hi"}]`

	t.Run("rejected by default", func(t *testing.T) {
		body := `{"model":"m","max_tokens":10,` + serverTool + user + `}`
		requireDetail(t, convertFailure(t, body, reject), "tools.0: server tools are not supported")
	})

	t.Run("other server type rejected even when ignoring", func(t *testing.T) {
		body := `{"model":"m","max_tokens":10,"tools":[{"type":"computer_20241022","name":"computer"}],` + user + `}`
		requireDetail(t, convertFailure(t, body, ignore), "tools.0: server tools are not supported")
	})

	t.Run("dropped when ignoring", func(t *testing.T) {
		body := `{"model":"m","max_tokens":10,"tools":[
			{"type":"web_search_20250305","name":"web_search","max_uses":3},
			{"name":"get_weather","input_schema":{"type":"object"}}
		],` + user + `,"tool_choice":{"type":"auto"}}`
		converted := convertBody(t, body, ignore)
		tools := converted.Conversation.Tools
		if len(tools) != 1 || tools[0].Name != "get_weather" {
			t.Fatalf("tools = %+v, want only get_weather", tools)
		}
		if !converted.ParsingOptions.ParseToolCalls {
			t.Error("parse tool calls = false, want true")
		}
	})

	t.Run("dropped server tool choice falls back to auto", func(t *testing.T) {
		body := `{"model":"m","max_tokens":10,"thinking":{"type":"disabled"},` + serverTool + user +
			`,"tool_choice":{"type":"tool","name":"web_search"}}`
		converted := convertBody(t, body, ignore)
		if len(converted.Conversation.Tools) != 0 {
			t.Errorf("tools = %d, want 0", len(converted.Conversation.Tools))
		}
		if converted.Conversation.ToolChoice != core.ToolChoiceAuto {
			t.Errorf("tool choice = %v, want auto", converted.Conversation.ToolChoice)
		}
		if converted.ParsingOptions.ParseToolCalls {
			t.Error("parse tool calls = true, want false")
		}
	})

	t.Run("assistant server blocks rejected", func(t *testing.T) {
		body := `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"q"},
			{"role":"assistant","content":[{"type":"server_tool_use","id":"s1","name":"web_search","input":{"query":"x"}}]}]}`
		requireDetail(t, convertFailure(t, body, reject),
			"Unsupported content block: server tools are not supported")
	})

	t.Run("assistant server blocks dropped", func(t *testing.T) {
		body := `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"q"},
			{"role":"assistant","content":[
				{"type":"server_tool_use","id":"s1","name":"web_search","input":{"query":"x"}},
				{"type":"web_search_tool_result","tool_use_id":"s1","content":[]}
			]}]}`
		converted := convertBody(t, body, ignore)
		requireMessages(t, converted, []string{"q", ""})
		assistant := converted.Conversation.Messages[1]
		if assistant.ToolCalls != nil {
			t.Errorf("tool calls = %+v, want nil", assistant.ToolCalls)
		}
	})
}

func TestConvertValidation(t *testing.T) {
	options := request.NewConversionOptions()
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "zero max tokens",
			body: `{"model":"m","max_tokens":0,"messages":[{"role":"user","content":"x"}]}`,
			want: "max_tokens must be greater than zero",
		},
		{
			name: "too many stop sequences",
			body: `{"model":"m","max_tokens":10,"stop_sequences":["1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17"],"messages":[{"role":"user","content":"x"}]}`,
			want: "stop_sequences must contain at most 16 non-empty strings",
		},
		{
			name: "empty stop sequence",
			body: `{"model":"m","max_tokens":10,"stop_sequences":["STOP",""],"messages":[{"role":"user","content":"x"}]}`,
			want: "stop_sequences must contain at most 16 non-empty strings",
		},
		{
			name: "temperature out of range",
			body: `{"model":"m","max_tokens":10,"temperature":3,"messages":[{"role":"user","content":"x"}]}`,
			want: "temperature must be in [0, 2]",
		},
		{
			name: "top p out of range",
			body: `{"model":"m","max_tokens":10,"top_p":0,"messages":[{"role":"user","content":"x"}]}`,
			want: "top_p must be in (0, 1]",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requireDetail(t, convertFailure(t, test.body, options), test.want)
		})
	}

	t.Run("stop sequences preserved", func(t *testing.T) {
		body := `{"model":"m","max_tokens":10,"stop_sequences":["STOP","END"],"messages":[{"role":"user","content":"x"}]}`
		converted := convertBody(t, body, options)
		got := converted.ParsingOptions.StopSequences
		if len(got) != 2 || got[0] != "STOP" || got[1] != "END" {
			t.Errorf("stop sequences = %v, want [STOP END]", got)
		}
	})
}

func TestDecodeRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"missing model", `{"messages":[{"role":"user","content":"x"}]}`, "missing field `model`"},
		{"null model", `{"model":null,"messages":[{"role":"user","content":"x"}]}`, "invalid type: null, expected a string"},
		{"missing messages", `{"model":"m"}`, "missing field `messages`"},
		{"null messages", `{"model":"m","messages":null}`, "invalid type: null, expected a sequence"},
		{"missing role", `{"model":"m","messages":[{"content":"x"}]}`, "missing field `role`"},
		{"null role", `{"model":"m","messages":[{"role":null,"content":"x"}]}`, "invalid type: null, expected string or map"},
		{"missing content", `{"model":"m","messages":[{"role":"user"}]}`, "missing field `content`"},
		{"null content", `{"model":"m","messages":[{"role":"user","content":null}]}`, "data did not match any variant of untagged enum MessagesContent"},
		{"numeric content", `{"model":"m","messages":[{"role":"user","content":5}]}`, "data did not match any variant of untagged enum MessagesContent"},
		{"null text block", `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":null}]}]}`, "data did not match any variant of untagged enum MessagesContent"},
		{"null system block", `{"model":"m","system":[{"type":"text","text":null}],"messages":[{"role":"user","content":"x"}]}`, "data did not match any variant of untagged enum MessagesTextOrTextBlocks"},
		{"null tool name", `{"model":"m","tools":[{"name":null,"input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"x"}]}`, "invalid type: null, expected a string"},
		{"null thinking type", `{"model":"m","messages":[{"role":"user","content":"x"}],"thinking":{"type":null}}`, "invalid type: null, expected string or map"},
		{"missing thinking type", `{"model":"m","messages":[{"role":"user","content":"x"}],"thinking":{}}`, "missing field `type`"},
		{"null tool choice type", `{"model":"m","messages":[{"role":"user","content":"x"}],"tool_choice":{"type":null}}`, "invalid type: null, expected variant identifier"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var typed MessagesRequest
			err := json.Unmarshal([]byte(test.body), &typed)
			if err == nil {
				t.Fatalf("expected a decode error")
			}
			if err.Error() != test.want {
				t.Errorf("decode error = %q, want %q", err.Error(), test.want)
			}
		})
	}
}

// runGenerator feeds chunks through a generator and accumulates its events.
func runGenerator(t *testing.T, generator *MessagesChunkGenerator, chunks []stream.OutputChunk) ([]stream.Event, *MessagesResponse) {
	t.Helper()
	accumulator := NewMessagesResponse("mock-id", "deepseek-flash", 0)
	var events []stream.Event
	for _, chunk := range chunks {
		produced := generator.Generate(chunk)
		events = append(events, produced...)
		for _, event := range produced {
			accumulator.Append(event)
		}
	}
	return events, accumulator
}

// eventJSON renders one event the way the SSE transport serializes it.
func eventJSON(t *testing.T, event stream.Event) string {
	t.Helper()
	text, err := jsonx.MarshalCompactString(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return text
}

// requireEvents asserts the serialized events.
func requireEvents(t *testing.T, events []stream.Event, want []string) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("event count = %d, want %d", len(events), len(want))
	}
	for index, event := range events {
		if got := eventJSON(t, event); got != want[index] {
			t.Errorf("event %d = %s, want %s", index, got, want[index])
		}
	}
}

// requireAccumulated asserts the accumulated response JSON.
func requireAccumulated(t *testing.T, accumulator *MessagesResponse, want string) {
	t.Helper()
	text, err := jsonx.MarshalCompactString(accumulator)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if text != want {
		t.Errorf("accumulated response =\n%s\nwant\n%s", text, want)
	}
}

func TestStreamSimpleText(t *testing.T) {
	generator := NewChunkGenerator("mock-id", "deepseek-flash", false)
	events, accumulator := runGenerator(t, generator, []stream.OutputChunk{
		stream.StartChunk{Usage: stream.PromptUsage{PromptTokens: 4, PromptCacheHitTokens: 1}},
		stream.RawChunk{Content: "Hello there!"},
		stream.FinishChunk{Reason: stream.FinishStop, Usage: stream.CompletionUsage{CompletionTokens: 3}},
	})
	requireEvents(t, events, []string{
		`{"type":"message_start","message":{"id":"mock-id","type":"message","role":"assistant","model":"deepseek-flash","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":3,"cache_creation_input_tokens":0,"cache_read_input_tokens":1,"output_tokens":0,"service_tier":"standard"}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"ping"}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello there!"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":3,"cache_creation_input_tokens":0,"cache_read_input_tokens":1,"output_tokens":3,"service_tier":"standard"}}`,
		`{"type":"message_stop"}`,
	})
	wantNames := []string{
		"message_start", "content_block_start", "ping", "content_block_delta",
		"content_block_stop", "message_delta", "message_stop",
	}
	for index, event := range events {
		if event.EventName() != wantNames[index] {
			t.Errorf("event %d name = %q, want %q", index, event.EventName(), wantNames[index])
		}
	}
	requireAccumulated(t, accumulator,
		`{"id":"mock-id","type":"message","role":"assistant","model":"deepseek-flash","content":[{"type":"text","text":"Hello there!"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":3,"cache_creation_input_tokens":0,"cache_read_input_tokens":1,"output_tokens":3,"service_tier":"standard"}}`)

	if _, ok := accumulator.DoneMessage(); ok {
		t.Error("done message reported, want none")
	}
}

func TestStreamThinkingMode(t *testing.T) {
	generator := NewChunkGenerator("mock-id", "deepseek-flash", true).WithSignature("sig-1")
	events, accumulator := runGenerator(t, generator, []stream.OutputChunk{
		stream.StartChunk{Usage: stream.PromptUsage{PromptTokens: 8}},
		stream.ReasoningChunk{Content: "Let me think."},
		stream.RawChunk{Content: "Answer"},
		stream.FinishChunk{Reason: stream.FinishStop, Usage: stream.CompletionUsage{CompletionTokens: 2}},
	})
	requireEvents(t, events, []string{
		`{"type":"message_start","message":{"id":"mock-id","type":"message","role":"assistant","model":"deepseek-flash","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":8,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":0,"service_tier":"standard"}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"ping"}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think."}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-1"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Answer"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":8,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":2,"service_tier":"standard"}}`,
		`{"type":"message_stop"}`,
	})
	requireAccumulated(t, accumulator,
		`{"id":"mock-id","type":"message","role":"assistant","model":"deepseek-flash","content":[{"type":"thinking","thinking":"Let me think.","signature":"sig-1"},{"type":"text","text":"Answer"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":8,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":2,"service_tier":"standard"}}`)
}

func TestStreamToolCalls(t *testing.T) {
	generator := NewChunkGenerator("mock-id", "deepseek-flash", false)
	events, accumulator := runGenerator(t, generator, []stream.OutputChunk{
		stream.StartChunk{Usage: stream.PromptUsage{PromptTokens: 5}},
		stream.ToolCallBeginChunk{},
		stream.ToolCallChunk{ToolName: "get_weather"},
		stream.ToolArgumentsDeltaChunk{Content: `{"location"`},
		stream.ToolArgumentsDeltaChunk{Content: `: "Paris"}`},
		stream.FinishChunk{Reason: stream.FinishToolCalls, Usage: stream.CompletionUsage{CompletionTokens: 4}},
	})
	requireEvents(t, events, []string{
		`{"type":"message_start","message":{"id":"mock-id","type":"message","role":"assistant","model":"deepseek-flash","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":0,"service_tier":"standard"}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_mock-id_0","name":"get_weather","input":{}}}`,
		`{"type":"ping"}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"location\""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":": \"Paris\"}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":4,"service_tier":"standard"}}`,
		`{"type":"message_stop"}`,
	})
	requireAccumulated(t, accumulator,
		`{"id":"mock-id","type":"message","role":"assistant","model":"deepseek-flash","content":[{"type":"tool_use","id":"toolu_mock-id_0","name":"get_weather","input":{"location":"Paris"}}],"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":4,"service_tier":"standard"}}`)
}

func TestStreamTruncatedToolInput(t *testing.T) {
	generator := NewChunkGenerator("mock-id", "deepseek-flash", false)
	_, accumulator := runGenerator(t, generator, []stream.OutputChunk{
		stream.StartChunk{},
		stream.ToolCallChunk{ToolName: "get_weather"},
		stream.ToolArgumentsDeltaChunk{Content: `{"location":`},
		stream.FinishChunk{Reason: stream.FinishToolCalls},
	})
	content, ok := accumulator.Content[0].(MessagesToolUseContent)
	if !ok {
		t.Fatalf("content block = %T, want tool use", accumulator.Content[0])
	}
	text, err := jsonx.Marshal(content.Input)
	if err != nil {
		t.Fatal(err)
	}
	if string(text) != "{}" {
		t.Errorf("truncated input = %s, want {}", text)
	}
}

func TestStreamFinishReasons(t *testing.T) {
	tests := []struct {
		reason stream.FinishReason
		want   string
	}{
		{stream.FinishStop, "end_turn"},
		{stream.FinishLength, "max_tokens"},
		{stream.FinishStopSequence, "stop_sequence"},
		{stream.FinishToolCalls, "tool_use"},
		{stream.FinishContentFilter, "refusal"},
		{stream.FinishEndOfStream, "end_turn"},
	}
	for _, test := range tests {
		t.Run(test.want, func(t *testing.T) {
			generator := NewChunkGenerator("mock-id", "m", false)
			generator.Generate(stream.StartChunk{})
			stopSequence := "STOP"
			events := generator.Generate(stream.FinishChunk{
				Reason:       test.reason,
				StopSequence: &stopSequence,
			})
			delta, ok := events[0].(MessagesMessageDeltaEvent)
			if !ok {
				t.Fatalf("event = %T, want message delta", events[0])
			}
			if delta.Delta.StopReason == nil || string(*delta.Delta.StopReason) != test.want {
				t.Errorf("stop reason = %v, want %s", delta.Delta.StopReason, test.want)
			}
			if delta.Delta.StopSequence == nil || *delta.Delta.StopSequence != "STOP" {
				t.Errorf("stop sequence = %v, want STOP", delta.Delta.StopSequence)
			}
		})
	}
}

func TestStreamFinishIsTerminal(t *testing.T) {
	generator := NewChunkGenerator("mock-id", "m", false)
	generator.Generate(stream.StartChunk{})
	generator.Generate(stream.FinishChunk{Reason: stream.FinishStop})
	if events := generator.Generate(stream.RawChunk{Content: "late"}); len(events) != 0 {
		t.Errorf("events after finish = %d, want 0", len(events))
	}
}

func TestStreamStartOnlyOnce(t *testing.T) {
	generator := NewChunkGenerator("mock-id", "m", false)
	first := generator.Generate(stream.StartChunk{Usage: stream.PromptUsage{PromptTokens: 1}})
	second := generator.Generate(stream.StartChunk{Usage: stream.PromptUsage{PromptTokens: 9}})
	if len(first) != 1 || len(second) != 0 {
		t.Errorf("start events = %d then %d, want 1 then 0", len(first), len(second))
	}
}

func TestResponseAppendIgnoresInvalidEvents(t *testing.T) {
	accumulator := NewMessagesResponse("id", "m", 0)
	accumulator.Append(MessagesContentBlockDeltaEvent{
		Index: 3,
		Delta: MessagesTextDelta{Text: "ignored"},
	})
	accumulator.Append(MessagesContentBlockStopEvent{Index: 3})
	accumulator.Append(MessagesContentBlockStartEvent{
		Index:        0,
		ContentBlock: MessagesTextContent{},
	})
	accumulator.Append(MessagesContentBlockDeltaEvent{
		Index: 0,
		Delta: MessagesThinkingDelta{Thinking: "wrong type"},
	})
	if len(accumulator.Content) != 1 {
		t.Fatalf("content blocks = %d, want 1", len(accumulator.Content))
	}
	text := accumulator.Content[0].(MessagesTextContent)
	if text.Text != "" {
		t.Errorf("text = %q, want empty", text.Text)
	}
	accumulator.Append(MessagesContentBlockStartEvent{
		Index:        5,
		ContentBlock: MessagesTextContent{},
	})
	if len(accumulator.Content) != 1 {
		t.Errorf("content blocks = %d after out-of-order start, want 1", len(accumulator.Content))
	}
}

func TestEventNames(t *testing.T) {
	tests := []struct {
		event stream.Event
		want  string
	}{
		{MessagesMessageStartEvent{}, "message_start"},
		{MessagesContentBlockStartEvent{}, "content_block_start"},
		{MessagesContentBlockDeltaEvent{}, "content_block_delta"},
		{MessagesContentBlockStopEvent{}, "content_block_stop"},
		{MessagesMessageDeltaEvent{}, "message_delta"},
		{MessagesMessageStopEvent{}, "message_stop"},
		{MessagesPingEvent{}, "ping"},
	}
	for _, test := range tests {
		if got := test.event.EventName(); got != test.want {
			t.Errorf("%T event name = %q, want %q", test.event, got, test.want)
		}
	}
}

func TestToolUseContentMarshal(t *testing.T) {
	content := &MessagesToolUseContent{
		ID:    "toolu_1",
		Name:  "get_weather",
		Input: jsonx.MustParse(`{"location":"Paris","days":2}`),
	}
	text, err := jsonx.MarshalCompactString(content)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"location":"Paris","days":2}}`
	if text != want {
		t.Errorf("tool use content = %s, want %s", text, want)
	}
}

func TestNewMessagesResponse(t *testing.T) {
	accumulator := NewMessagesResponse("id-1", "m-1", 99)
	text, err := jsonx.MarshalCompactString(accumulator)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"id-1","type":"message","role":"assistant","model":"m-1","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":0,"service_tier":"standard"}}`
	if text != want {
		t.Errorf("empty response = %s, want %s", text, want)
	}
	withUsage := NewMessagesResponseWithUsage("id-1", "m-1", 0, stream.PromptUsage{
		PromptTokens:         10,
		PromptCacheHitTokens: 4,
	})
	if withUsage.Usage.InputTokens != 6 || withUsage.Usage.CacheReadInputTokens != 4 {
		t.Errorf("usage = %+v", withUsage.Usage)
	}
}

func TestUsageNeverNegative(t *testing.T) {
	usage := NewMessagesUsage(stream.PromptUsage{PromptTokens: 2, PromptCacheHitTokens: 5},
		stream.CompletionUsage{CompletionTokens: 3})
	if usage.InputTokens != 0 {
		t.Errorf("input tokens = %d, want 0", usage.InputTokens)
	}
	if usage.CacheReadInputTokens != 5 || usage.OutputTokens != 3 {
		t.Errorf("usage = %+v", usage)
	}
}

// effortPointer returns a pointer to a reasoning effort level.
func effortPointer(level core.ReasoningEffort) *core.ReasoningEffort { return &level }

// uint64Pointer returns a pointer to a uint64 value.
func uint64Pointer(value uint64) *uint64 { return &value }

// requireEffort asserts an optional reasoning effort.
func requireEffort(t *testing.T, got, want *core.ReasoningEffort) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil || want == nil:
		t.Errorf("reasoning effort = %v, want %v", got, want)
	case *got != *want:
		t.Errorf("reasoning effort = %v, want %v", *got, *want)
	}
}

// requireUint64 asserts an optional unsigned value.
func requireUint64(t *testing.T, got, want *uint64) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil || want == nil:
		t.Errorf("value = %v, want %v", got, want)
	case *got != *want:
		t.Errorf("value = %d, want %d", *got, *want)
	}
}
