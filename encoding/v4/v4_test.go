package v4_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/encoding"
	"github.com/dandandujie/dsr-go/encoding/v4"
	"github.com/dandandujie/dsr-go/encoding/v4/dsv4"
	"github.com/dandandujie/dsr-go/encoding/v4/dsv41"
)

// The reasoning-effort prefixes below are byte-for-byte copies of the Rust
// constants; the tests pin them independently of the implementation.

// reasoningEffortHigh is the default V4 reasoning-effort prefix.
const reasoningEffortHigh = "Reasoning Effort: Absolute maximum with no shortcuts permitted.\n" +
	"You MUST be very thorough in your thinking and comprehensively decompose the problem to resolve the root cause, rigorously stress-testing your logic against all potential paths, edge cases, and adversarial scenarios.\n" +
	"Explicitly write out your entire deliberation process, documenting every intermediate step, considered alternative, and rejected hypothesis to ensure absolutely no assumption is left unchecked.\n\n"

// reasoningEffortMax is the V4 reasoning-effort prefix for maximum effort.
const reasoningEffortMax = "Reasoning Effort: Beyond maximum — exhaustive, relentless, and uncompromising.\n" +
	"You MUST reason with the utmost depth and rigor, leaving absolutely nothing to chance: exhaustively decompose the problem into its most fundamental components, trace every causal chain to its root, and resolve the underlying cause rather than any surface symptom.\n" +
	"Do not stop reasoning until you have independently verified the solution from multiple angles and are certain that no assumption remains unchecked and no error remains undiscovered.\n\n"

// reasoningEffortV41 renders the V4.1 reasoning-effort prefix for a score.
func reasoningEffortV41(score int) string {
	return fmt.Sprintf("Reasoning Effort: %d (range 1-100, the higher the value, the more thorough the reasoning)\n\n", score)
}

// toolSectionV4 is the exact "## Tools" prompt section of a V4 encoding.
const toolSectionV4 = `## Tools

You have access to a set of tools to help answer the user's question. You can invoke tools by writing a "<｜DSML｜tool_calls>" block like the following:

<｜DSML｜tool_calls>
<｜DSML｜invoke name="$TOOL_NAME">
<｜DSML｜parameter name="$PARAMETER_NAME" string="true|false">$PARAMETER_VALUE</｜DSML｜parameter>
...
</｜DSML｜invoke>
<｜DSML｜invoke name="$TOOL_NAME2">
...
</｜DSML｜invoke>
</｜DSML｜tool_calls>

String parameters should be specified as is and set ` + "`string=\"true\"`" + `. For all other types (numbers, booleans, arrays, objects), pass the value in JSON format and set ` + "`string=\"false\"`" + `.

If thinking_mode is enabled (triggered by <think>), you MUST output your complete reasoning inside <think>...</think> BEFORE any tool calls or final response.

Otherwise, output directly after </think> with tool calls or final response.

### Available Tool Schemas

{"name": "get_weather", "description": "Get the weather", "parameters": {"type": "object", "properties": {"location": {"type": "string"}}, "required": ["location"]}}
{"name": "noop", "description": "", "parameters": {}}

You MUST strictly follow the above defined tool name and parameter schemas to invoke tool calls.
`

// toolSectionV41 is the exact "## Tools" prompt section of a V4.1 encoding.
const toolSectionV41 = `## Tools

You have access to a set of tools to help answer the user's question. You can invoke tools by writing a "<｜DSML｜ calls>" block like the following:

<｜DSML｜ calls>
<｜DSML｜ invoke name="$TOOL_NAME">
<｜DSML｜ parameter name="$PARAMETER_NAME" string="true|false">$PARAMETER_VALUE</｜DSML｜ parameter>
...
</｜DSML｜ invoke>
<｜DSML｜ invoke name="$TOOL_NAME2">
...
</｜DSML｜ invoke>
</｜DSML｜ calls>

String parameters should be specified as is and set ` + "`string=\"true\"`" + `. For all other types (numbers, booleans, arrays, objects), pass the value in JSON format and set ` + "`string=\"false\"`" + `.

If thinking_mode is enabled (triggered by <think>), you MUST output your complete reasoning inside <think>...</think> BEFORE any tool calls or final response.

Otherwise, output directly after </think> with tool calls or final response.

### Available Tool Schemas

{"name": "get_weather", "description": "Get the weather", "parameters": {"type": "object", "properties": {"location": {"type": "string"}}, "required": ["location"]}}
{"name": "noop", "description": "", "parameters": {}}

You MUST strictly follow the above defined tool name and parameter schemas to invoke tool calls.
`

func testUser(content string) core.InputMessage { return core.UserMessage(content, nil) }

func testUserImages(content string, sources []core.ImageSource) core.InputMessage {
	return core.UserMessage(content, sources)
}

func testSystem(content string) core.InputMessage { return core.SystemMessage(content) }

func testAssistant(content, reasoning string, toolCalls []core.ToolCall) core.InputMessage {
	return core.AssistantMessage(content, reasoning, toolCalls)
}

func testTool(content, toolCallID string) core.InputMessage {
	return core.ToolMessage(content, nil, toolCallID)
}

func testToolImages(content, toolCallID string, sources []core.ImageSource) core.InputMessage {
	return core.ToolMessage(content, sources, toolCallID)
}

func testReminder(content string) core.InputMessage { return core.LatestReminderMessage(content) }

func testCall(id, name, arguments string) core.ToolCall {
	return core.ToolCall{ID: id, Name: name, Arguments: arguments}
}

func testConversation(messages ...core.InputMessage) *core.Conversation {
	conversation := core.NewConversation()
	conversation.Messages = messages
	return conversation
}

func testString(value string) *string { return &value }

func testEffort(effort core.ReasoningEffort) *core.ReasoningEffort { return &effort }

// weatherTools returns the two tool definitions used by the tool-prompt tests.
func weatherTools() []core.ToolDefinition {
	return []core.ToolDefinition{
		{
			Name:        "get_weather",
			Description: testString("Get the weather"),
			Parameters:  jsonx.MustParse("{\"type\": \"object\", \"properties\": {\"location\": {\"type\": \"string\"}}, \"required\": [\"location\"]}"),
		},
		{Name: "noop", Parameters: jsonx.MustParse("{}")},
	}
}

// toolResultContents returns the contents of every tool result in prompt order.
func toolResultContents(prompt string) []string {
	contents := []string{}
	for {
		start := strings.Index(prompt, "<tool_result>")
		if start < 0 {
			return contents
		}
		rest := prompt[start+len("<tool_result>"):]
		end := strings.Index(rest, "</tool_result>")
		if end < 0 {
			return contents
		}
		contents = append(contents, rest[:end])
		prompt = rest[end+len("</tool_result>"):]
	}
}

func TestTokenConstants(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"BOSToken", v4.BOSToken, "<｜begin▁of▁sentence｜>"},
		{"ThinkingStartToken", v4.ThinkingStartToken, "<think>"},
		{"ThinkingEndToken", v4.ThinkingEndToken, "</think>"},
		{"SystemSPToken", v4.SystemSPToken, "<｜System｜>"},
		{"UserSPToken", v4.UserSPToken, "<｜User｜>"},
		{"AssistantSPToken", v4.AssistantSPToken, "<｜Assistant｜>"},
		{"LatestReminderSPToken", v4.LatestReminderSPToken, "<｜latest_reminder｜>"},
		{"EOSToken", v4.EOSToken, "<｜end▁of▁sentence｜>"},
		{"DSMLSPToken", v4.DSMLSPToken, "｜DSML｜"},
	}
	for _, testCase := range cases {
		if testCase.got != testCase.want {
			t.Errorf("%s = %q, want %q", testCase.name, testCase.got, testCase.want)
		}
	}
}

// TestRenderSimplePrompt pins the framing of a single user turn, including the
// reasoning-effort prefix and the thinking / answer tokens.
func TestRenderSimplePrompt(t *testing.T) {
	thinking := testConversation(testUser("hi"))
	noThinking := testConversation(testUser("hi"))
	noThinking.ThinkingMode = false

	cases := []struct {
		name         string
		port         encoding.PromptEncoding
		conversation *core.Conversation
		want         string
	}{
		{
			name:         "v4 thinking",
			port:         dsv4.New(),
			conversation: thinking,
			want: v4.BOSToken + reasoningEffortHigh + v4.UserSPToken + "hi" +
				v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name:         "v41 thinking",
			port:         dsv41.New(),
			conversation: thinking,
			want: v4.BOSToken + v4.SystemSPToken + reasoningEffortV41(75) + v4.UserSPToken + "hi" +
				v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name:         "v4 no thinking",
			port:         dsv4.New(),
			conversation: noThinking,
			want: v4.BOSToken + v4.UserSPToken + "hi" +
				v4.AssistantSPToken + v4.ThinkingEndToken,
		},
		{
			name:         "v41 no thinking",
			port:         dsv41.New(),
			conversation: noThinking,
			want: v4.BOSToken + v4.UserSPToken + "hi" +
				v4.AssistantSPToken + v4.ThinkingEndToken,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := testCase.port.RenderConversation(testCase.conversation)
			if got.Prompt != testCase.want {
				t.Errorf("prompt = %q, want %q", got.Prompt, testCase.want)
			}
		})
	}
}

// TestRenderMessageFraming pins the per-message prefixes, reminder framing,
// tool-result framing, and the thinking / answer parts of assistant turns.
func TestRenderMessageFraming(t *testing.T) {
	effortV4 := reasoningEffortHigh

	cases := []struct {
		name         string
		port         encoding.PromptEncoding
		conversation *core.Conversation
		want         string
	}{
		{
			name: "v4 latest reminder",
			port: dsv4.New(),
			conversation: testConversation(
				testUser("hi"), testReminder("rem"), testAssistant("a", "", nil), testUser("again"),
			),
			want: v4.BOSToken + effortV4 + v4.UserSPToken + "hi" + v4.LatestReminderSPToken + "rem" +
				v4.AssistantSPToken + v4.ThinkingStartToken + v4.ThinkingEndToken + "a" + v4.EOSToken +
				v4.UserSPToken + "again" + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name: "v41 latest reminder",
			port: dsv41.New(),
			conversation: testConversation(
				testUser("hi"), testReminder("rem"), testAssistant("a", "", nil), testUser("again"),
			),
			want: v4.BOSToken + v4.SystemSPToken + reasoningEffortV41(75) + v4.UserSPToken + "hi" +
				v4.LatestReminderSPToken + "rem" +
				v4.AssistantSPToken + v4.ThinkingStartToken + v4.ThinkingEndToken + "a" + v4.EOSToken +
				v4.UserSPToken + "again" + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name:         "tool result after user",
			port:         dsv4.New(),
			conversation: testConversation(testUser("q"), testTool("res", "c0")),
			want: v4.BOSToken + effortV4 + v4.UserSPToken + "q" +
				"\n\n<tool_result>res</tool_result>" + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name:         "assistant first message",
			port:         dsv4.New(),
			conversation: testConversation(testAssistant("first", "reason", nil)),
			want: v4.BOSToken + effortV4 + v4.AssistantSPToken + v4.ThinkingEndToken + "first" +
				v4.EOSToken + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name:         "assistant with reasoning",
			port:         dsv4.New(),
			conversation: testConversation(testUser("q"), testAssistant("ans", "thinking", nil)),
			want: v4.BOSToken + effortV4 + v4.UserSPToken + "q" +
				v4.AssistantSPToken + v4.ThinkingStartToken + "thinking" + v4.ThinkingEndToken + "ans" +
				v4.EOSToken + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name:         "assistant without reasoning",
			port:         dsv4.New(),
			conversation: testConversation(testUser("q"), testAssistant("ans", "", nil)),
			want: v4.BOSToken + effortV4 + v4.UserSPToken + "q" +
				v4.AssistantSPToken + v4.ThinkingStartToken + v4.ThinkingEndToken + "ans" +
				v4.EOSToken + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name:         "assistant history in v41",
			port:         dsv41.New(),
			conversation: testConversation(testUser("q"), testAssistant("ans", "thinking", nil)),
			want: v4.BOSToken + v4.SystemSPToken + reasoningEffortV41(75) + v4.UserSPToken + "q" +
				v4.AssistantSPToken + v4.ThinkingStartToken + "thinking" + v4.ThinkingEndToken + "ans" +
				v4.EOSToken + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := testCase.port.RenderConversation(testCase.conversation)
			if got.Prompt != testCase.want {
				t.Errorf("prompt = %q, want %q", got.Prompt, testCase.want)
			}
		})
	}
}

// TestNormalizeMessages pins mid-conversation system handling for both
// encodings, consecutive-user merging, and empty-message dropping.
func TestNormalizeMessages(t *testing.T) {
	cases := []struct {
		name         string
		port         encoding.PromptEncoding
		conversation *core.Conversation
		want         string
	}{
		{
			name: "v4 merges mid-conversation system into the next user message",
			port: dsv4.New(),
			conversation: testConversation(
				testUser("hi"), testAssistant("answer", "", nil), testSystem("mid"), testUser("next"),
			),
			want: v4.BOSToken + reasoningEffortHigh + v4.UserSPToken + "hi" +
				v4.AssistantSPToken + v4.ThinkingStartToken + v4.ThinkingEndToken + "answer" + v4.EOSToken +
				v4.UserSPToken + "mid\n\nnext" + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name: "v41 keeps mid-conversation system",
			port: dsv41.New(),
			conversation: testConversation(
				testUser("hi"), testAssistant("answer", "", nil), testSystem("mid"), testUser("next"),
			),
			want: v4.BOSToken + v4.SystemSPToken + reasoningEffortV41(75) + v4.UserSPToken + "hi" +
				v4.AssistantSPToken + v4.ThinkingStartToken + v4.ThinkingEndToken + "answer" + v4.EOSToken +
				v4.SystemSPToken + "mid" + v4.UserSPToken + "next" +
				v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name:         "v4 merges leading system messages",
			port:         dsv4.New(),
			conversation: testConversation(testSystem("a"), testSystem("b"), testUser("hi")),
			want: v4.BOSToken + reasoningEffortHigh + "a\n\nb" +
				v4.UserSPToken + "hi" + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name:         "v41 keeps leading system messages separate",
			port:         dsv41.New(),
			conversation: testConversation(testSystem("a"), testSystem("b"), testUser("hi")),
			want: v4.BOSToken + v4.SystemSPToken + reasoningEffortV41(75) + "a" +
				v4.SystemSPToken + "b" +
				v4.UserSPToken + "hi" + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name:         "empty system message stays empty",
			port:         dsv4.New(),
			conversation: testConversation(testSystem(""), testUser("hi")),
			want: v4.BOSToken + reasoningEffortHigh + v4.UserSPToken + "hi" +
				v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name:         "empty system message has no separator",
			port:         dsv4.New(),
			conversation: testConversation(testSystem(""), testSystem("b"), testUser("hi")),
			want: v4.BOSToken + reasoningEffortHigh + "b" + v4.UserSPToken + "hi" +
				v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name:         "v4 merges consecutive user messages",
			port:         dsv4.New(),
			conversation: testConversation(testUser("a"), testUser("b"), testAssistant("x", "", nil), testUser("c")),
			want: v4.BOSToken + reasoningEffortHigh + v4.UserSPToken + "a\n\nb" +
				v4.AssistantSPToken + v4.ThinkingStartToken + v4.ThinkingEndToken + "x" + v4.EOSToken +
				v4.UserSPToken + "c" + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name: "v4 drops an empty mid-conversation system message",
			port: dsv4.New(),
			conversation: testConversation(
				testUser("q"), testAssistant("a", "", nil), testSystem(""),
			),
			want: v4.BOSToken + reasoningEffortHigh + v4.UserSPToken + "q" +
				v4.AssistantSPToken + v4.ThinkingStartToken + v4.ThinkingEndToken + "a" + v4.EOSToken +
				v4.AssistantSPToken + v4.ThinkingStartToken,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := testCase.port.RenderConversation(testCase.conversation)
			if got.Prompt != testCase.want {
				t.Errorf("prompt = %q, want %q", got.Prompt, testCase.want)
			}
		})
	}
}

// TestToolResultOrdering pins the tool-result sorting algorithm: results are
// reordered to match their calls within a contiguous run, unknown call ids rank
// first, and equal ranks keep their relative order.
func TestToolResultOrdering(t *testing.T) {
	calls := []core.ToolCall{
		testCall("a", "fa", "{}"),
		testCall("b", "fb", "{}"),
		testCall("c", "fc", "{}"),
	}

	cases := []struct {
		name         string
		conversation *core.Conversation
		want         []string
	}{
		{
			name: "three results are reordered",
			conversation: testConversation(
				testUser("q"),
				testAssistant("", "", calls),
				testTool("ra", "c"), testTool("rb", "b"), testTool("rc", "a"),
			),
			want: []string{"rc", "rb", "ra"},
		},
		{
			name: "unknown call ids rank first and keep their order",
			conversation: testConversation(
				testUser("q"),
				testAssistant("", "", []core.ToolCall{testCall("a", "fa", "{}"), testCall("b", "fb", "{}")}),
				testTool("r1", "zz"), testTool("r2", "b"), testTool("r3", "a"),
			),
			want: []string{"r1", "r3", "r2"},
		},
		{
			name: "a single result is not reordered",
			conversation: testConversation(
				testUser("q"),
				testAssistant("", "", calls),
				testTool("only", "c"),
			),
			want: []string{"only"},
		},
		{
			name:         "results without a preceding call keep their order",
			conversation: testConversation(testUser("q"), testTool("x", "a"), testTool("y", "b")),
			want:         []string{"x", "y"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			prompt := dsv4.New().RenderConversation(testCase.conversation).Prompt
			if got := toolResultContents(prompt); !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("tool result order = %v, want %v (prompt %q)", got, testCase.want, prompt)
			}
		})
	}

	t.Run("user messages split the run but results still move to their slots", func(t *testing.T) {
		conversation := testConversation(
			testUser("q"),
			testAssistant("", "", []core.ToolCall{testCall("a", "fa", "{}"), testCall("b", "fb", "{}")}),
			testTool("ra", "b"), testUser("interrupt"), testTool("rb", "a"),
		)
		prompt := dsv4.New().RenderConversation(conversation).Prompt
		want := v4.UserSPToken + "<tool_result>rb</tool_result>\n\ninterrupt\n\n<tool_result>ra</tool_result>" +
			v4.AssistantSPToken
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt %q does not contain %q", prompt, want)
		}
	})
}

// TestToolPromptRendering pins the full tool-definition prompt for both
// encodings, including the Python-style JSON schemas.
func TestToolPromptRendering(t *testing.T) {
	conversation := testConversation(testSystem("sys"), testUser("q"))
	conversation.Tools = weatherTools()

	cases := []struct {
		name string
		port v4.Encoding
		want string
	}{
		{
			name: "v4",
			port: dsv4.New(),
			want: v4.BOSToken + reasoningEffortHigh + "sys\n\n" + toolSectionV4 +
				v4.UserSPToken + "q" + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name: "v41",
			port: dsv41.New(),
			want: v4.BOSToken + v4.SystemSPToken + reasoningEffortV41(75) + "sys\n\n" + toolSectionV41 +
				v4.UserSPToken + "q" + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := v4.RenderConversation(testCase.port, conversation)
			if got.Prompt != testCase.want {
				t.Errorf("prompt = %q, want %q", got.Prompt, testCase.want)
			}
		})
	}
}

// TestToolCallRendering pins assistant tool-call blocks, parameter templates,
// and the argument fallback for non-object arguments.
func TestToolCallRendering(t *testing.T) {
	t.Run("v4 tool call", func(t *testing.T) {
		conversation := testConversation(
			testUser("q"),
			testAssistant("", "", []core.ToolCall{
				testCall("c1", "get_weather", "{\"location\": \"Paris\", \"days\": 3}"),
			}),
			testTool("sunny", "c1"),
		)
		want := v4.BOSToken + reasoningEffortHigh + v4.UserSPToken + "q" +
			v4.AssistantSPToken + v4.ThinkingStartToken + v4.ThinkingEndToken +
			"\n\n<｜DSML｜tool_calls>\n" +
			"<｜DSML｜invoke name=\"get_weather\">\n" +
			"<｜DSML｜parameter name=\"location\" string=\"true\">Paris</｜DSML｜parameter>\n" +
			"<｜DSML｜parameter name=\"days\" string=\"false\">3</｜DSML｜parameter>\n" +
			"</｜DSML｜invoke>\n" +
			"</｜DSML｜tool_calls>" +
			v4.EOSToken +
			v4.UserSPToken + "<tool_result>sunny</tool_result>" +
			v4.AssistantSPToken + v4.ThinkingStartToken
		got := dsv4.New().RenderConversation(conversation).Prompt
		if got != want {
			t.Errorf("prompt = %q, want %q", got, want)
		}
	})

	t.Run("v41 tool call", func(t *testing.T) {
		conversation := testConversation(
			testUser("q"),
			testAssistant("", "", []core.ToolCall{testCall("c1", "f", "{\"s\": \"v\"}")}),
		)
		prompt := dsv41.New().RenderConversation(conversation).Prompt
		want := "\n\n<｜DSML｜ calls>\n" +
			"<｜DSML｜ invoke name=\"f\">\n" +
			"<｜DSML｜ parameter name=\"s\" string=\"true\">v</｜DSML｜ parameter>\n" +
			"</｜DSML｜ invoke>\n" +
			"</｜DSML｜ calls>" + v4.EOSToken
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt %q does not contain %q", prompt, want)
		}
	})

	t.Run("arguments that are not a JSON object fall back to a string parameter", func(t *testing.T) {
		cases := []struct {
			name      string
			arguments string
		}{
			{"invalid JSON", "not json"},
			{"JSON array", "[1, 2]"},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				conversation := testConversation(
					testUser("q"),
					testAssistant("", "", []core.ToolCall{testCall("c1", "f", testCase.arguments)}),
				)
				prompt := dsv4.New().RenderConversation(conversation).Prompt
				want := "<｜DSML｜parameter name=\"arguments\" string=\"true\">" + testCase.arguments +
					"</｜DSML｜parameter>"
				if !strings.Contains(prompt, want) {
					t.Errorf("prompt %q does not contain %q", prompt, want)
				}
			})
		}
	})

	t.Run("parameter values use Python-style JSON", func(t *testing.T) {
		conversation := testConversation(
			testUser("q"),
			testAssistant("", "", []core.ToolCall{testCall("c1", "f",
				"{\"text\": \"hello\", \"count\": 3, \"ratio\": 1.5e-7, \"flag\": true, \"nested\": {\"list\": [1, 2, \"x\"], \"none\": null}, \"unicode\": \"中文\", \"empty\": {}}")}),
		)
		prompt := dsv4.New().RenderConversation(conversation).Prompt
		want := "<｜DSML｜invoke name=\"f\">\n" +
			"<｜DSML｜parameter name=\"text\" string=\"true\">hello</｜DSML｜parameter>\n" +
			"<｜DSML｜parameter name=\"count\" string=\"false\">3</｜DSML｜parameter>\n" +
			"<｜DSML｜parameter name=\"ratio\" string=\"false\">1.5e-07</｜DSML｜parameter>\n" +
			"<｜DSML｜parameter name=\"flag\" string=\"false\">true</｜DSML｜parameter>\n" +
			"<｜DSML｜parameter name=\"nested\" string=\"false\">{\"list\": [1, 2, \"x\"], \"none\": null}</｜DSML｜parameter>\n" +
			"<｜DSML｜parameter name=\"unicode\" string=\"true\">中文</｜DSML｜parameter>\n" +
			"<｜DSML｜parameter name=\"empty\" string=\"false\">{}</｜DSML｜parameter>\n" +
			"</｜DSML｜invoke>"
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt %q does not contain %q", prompt, want)
		}
	})
}

// TestToolChoiceRequired pins the tool-call block opened for a required tool
// choice.
func TestToolChoiceRequired(t *testing.T) {
	base := func() *core.Conversation {
		conversation := testConversation(testUser("q"))
		conversation.Tools = weatherTools()
		return conversation
	}

	cases := []struct {
		name         string
		port         encoding.PromptEncoding
		conversation *core.Conversation
		wantSuffix   string
	}{
		{
			name:         "v4 required",
			port:         dsv4.New(),
			conversation: func() *core.Conversation { c := base(); c.ToolChoice = core.ToolChoiceRequired; return c }(),
			wantSuffix:   v4.AssistantSPToken + v4.ThinkingStartToken + "\n\n<｜DSML｜tool_calls>\n",
		},
		{
			name:         "v41 required",
			port:         dsv41.New(),
			conversation: func() *core.Conversation { c := base(); c.ToolChoice = core.ToolChoiceRequired; return c }(),
			wantSuffix:   v4.AssistantSPToken + v4.ThinkingStartToken + "\n\n<｜DSML｜ calls>\n",
		},
		{
			name: "required without thinking",
			port: dsv4.New(),
			conversation: func() *core.Conversation {
				c := base()
				c.ToolChoice = core.ToolChoiceRequired
				c.ThinkingMode = false
				return c
			}(),
			wantSuffix: v4.AssistantSPToken + v4.ThinkingEndToken + "\n\n<｜DSML｜tool_calls>\n",
		},
		{
			name:         "auto has no block",
			port:         dsv4.New(),
			conversation: base(),
			wantSuffix:   v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name: "required without tools has no block",
			port: dsv4.New(),
			conversation: func() *core.Conversation {
				c := testConversation(testUser("q"))
				c.ToolChoice = core.ToolChoiceRequired
				return c
			}(),
			wantSuffix: v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name: "none with tools has no block",
			port: dsv4.New(),
			conversation: func() *core.Conversation {
				c := base()
				c.ToolChoice = core.ToolChoiceNone
				return c
			}(),
			wantSuffix: v4.AssistantSPToken + v4.ThinkingStartToken,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			prompt := testCase.port.RenderConversation(testCase.conversation).Prompt
			if !strings.HasSuffix(prompt, testCase.wantSuffix) {
				t.Errorf("prompt %q does not end with %q", prompt, testCase.wantSuffix)
			}
			if testCase.wantSuffix == v4.AssistantSPToken+v4.ThinkingStartToken &&
				strings.Contains(prompt, "｜DSML｜tool_calls>\n") && testCase.name == "none with tools has no block" {
				t.Errorf("prompt %q unexpectedly opens a tool-call block", prompt)
			}
		})
	}
}

// TestResponseFormatJSONObject pins the schema section appended for a
// JSON-object response format.
func TestResponseFormatJSONObject(t *testing.T) {
	schema := "\n\n## Response Format:\n\nYou MUST strictly adhere to the following schema to reply:\n" +
		"{\"type\": \"json_object\"}"

	jsonObject := testConversation(testUser("q"))
	jsonObject.ResponseFormat = core.ResponseFormatJSONObject

	cases := []struct {
		name         string
		port         encoding.PromptEncoding
		conversation *core.Conversation
		want         string
	}{
		{
			name:         "v4 creates an empty system message",
			port:         dsv4.New(),
			conversation: jsonObject,
			want: v4.BOSToken + reasoningEffortHigh + schema +
				v4.UserSPToken + "q" + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name:         "v41 creates an empty system message",
			port:         dsv41.New(),
			conversation: jsonObject,
			want: v4.BOSToken + v4.SystemSPToken + reasoningEffortV41(75) + schema +
				v4.UserSPToken + "q" + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
		{
			name: "appends to an existing system message",
			port: dsv4.New(),
			conversation: func() *core.Conversation {
				c := testConversation(testSystem("sys"), testUser("q"))
				c.ResponseFormat = core.ResponseFormatJSONObject
				return c
			}(),
			want: v4.BOSToken + reasoningEffortHigh + "sys" + schema +
				v4.UserSPToken + "q" + v4.AssistantSPToken + v4.ThinkingStartToken,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := testCase.port.RenderConversation(testCase.conversation)
			if got.Prompt != testCase.want {
				t.Errorf("prompt = %q, want %q", got.Prompt, testCase.want)
			}
		})
	}

	t.Run("text format has no schema", func(t *testing.T) {
		prompt := dsv4.New().RenderConversation(testConversation(testUser("q"))).Prompt
		if strings.Contains(prompt, "## Response Format") {
			t.Errorf("prompt %q unexpectedly contains a response format schema", prompt)
		}
	})

	t.Run("tools precede the schema", func(t *testing.T) {
		conversation := testConversation(testSystem("sys"), testUser("q"))
		conversation.Tools = weatherTools()
		conversation.ResponseFormat = core.ResponseFormatJSONObject
		prompt := dsv4.New().RenderConversation(conversation).Prompt
		toolsIndex := strings.Index(prompt, "## Tools")
		schemaIndex := strings.Index(prompt, "## Response Format:")
		if toolsIndex < 0 || schemaIndex < 0 || toolsIndex > schemaIndex {
			t.Errorf("prompt %q does not contain the tools section before the schema", prompt)
		}
		if !strings.Contains(prompt, "invoke tool calls.\n\n\n## Response Format:") {
			t.Errorf("prompt %q does not separate the tools section from the schema", prompt)
		}
	})
}

// TestImageSources pins the image-source collection order, the merging of
// consecutive user messages, and that rendering does not mutate the input.
func TestImageSources(t *testing.T) {
	conversation := testConversation(
		testUserImages("look <｜image｜>", []core.ImageSource{
			core.DataURLImageSource("data:image/png;base64,AAA", core.ImageDetailLow),
			core.URLImageSource("http://x/y.png", core.ImageDetailHigh),
		}),
		testAssistant("", "", []core.ToolCall{testCall("c1", "f", "{}")}),
		testToolImages("res", "c1", []core.ImageSource{
			core.BytesImageSource([]byte{1, 2, 3}, core.ImageDetailAuto),
		}),
	)

	rendered := dsv4.New().RenderConversation(conversation)
	want := []core.ImageSource{
		core.DataURLImageSource("data:image/png;base64,AAA", core.ImageDetailLow),
		core.URLImageSource("http://x/y.png", core.ImageDetailHigh),
		core.BytesImageSource([]byte{1, 2, 3}, core.ImageDetailAuto),
	}
	if !reflect.DeepEqual(rendered.ImageSources, want) {
		t.Errorf("image sources = %#v, want %#v", rendered.ImageSources, want)
	}
	if len(conversation.Messages[0].ImageSources) != 2 || len(conversation.Messages[2].ImageSources) != 1 {
		t.Errorf("rendering mutated the input conversation images")
	}

	t.Run("consecutive user messages merge their images in order", func(t *testing.T) {
		merged := testConversation(
			testUserImages("one", []core.ImageSource{
				core.URLImageSource("http://a/1.png", core.ImageDetailHigh),
			}),
			testUserImages("two", []core.ImageSource{
				core.URLImageSource("http://a/2.png", core.ImageDetailOriginal),
			}),
		)
		rendered := dsv41.New().RenderConversation(merged)
		want := []core.ImageSource{
			core.URLImageSource("http://a/1.png", core.ImageDetailHigh),
			core.URLImageSource("http://a/2.png", core.ImageDetailOriginal),
		}
		if !reflect.DeepEqual(rendered.ImageSources, want) {
			t.Errorf("image sources = %#v, want %#v", rendered.ImageSources, want)
		}
		if !strings.Contains(rendered.Prompt, v4.UserSPToken+"one\n\ntwo") {
			t.Errorf("prompt %q does not merge consecutive user messages", rendered.Prompt)
		}
		if len(merged.Messages[0].ImageSources) != 1 {
			t.Errorf("rendering mutated the merged input images: %#v", merged.Messages[0].ImageSources)
		}
	})

	t.Run("no images", func(t *testing.T) {
		rendered := dsv4.New().RenderConversation(testConversation(testUser("hi")))
		if len(rendered.ImageSources) != 0 {
			t.Errorf("image sources = %#v, want none", rendered.ImageSources)
		}
	})
}

// TestReasoningEffort pins the exact reasoning-effort text of both encodings.
func TestReasoningEffort(t *testing.T) {
	v4Cases := []struct {
		name         string
		index        int
		thinkingMode bool
		effort       *core.ReasoningEffort
		want         string
	}{
		{"default", 0, true, nil, reasoningEffortHigh},
		{"high", 0, true, testEffort(core.ReasoningEffortHigh), reasoningEffortHigh},
		{"xhigh", 0, true, testEffort(core.ReasoningEffortXhigh), reasoningEffortHigh},
		{"low", 0, true, testEffort(core.ReasoningEffortLow), ""},
		{"max", 0, true, testEffort(core.ReasoningEffortMax), reasoningEffortMax},
		{"later message", 1, true, testEffort(core.ReasoningEffortHigh), ""},
		{"no thinking", 0, false, testEffort(core.ReasoningEffortHigh), ""},
	}
	for _, testCase := range v4Cases {
		t.Run("v4 "+testCase.name, func(t *testing.T) {
			got := dsv4.New().RenderReasoningEffort(testCase.index, testCase.thinkingMode, testCase.effort)
			if got != testCase.want {
				t.Errorf("effort = %q, want %q", got, testCase.want)
			}
		})
	}

	v41Cases := []struct {
		name         string
		index        int
		thinkingMode bool
		effort       *core.ReasoningEffort
		score        int
		wantEmpty    bool
	}{
		{"default", 0, true, nil, 75, false},
		{"high", 0, true, testEffort(core.ReasoningEffortHigh), 75, false},
		{"xhigh", 0, true, testEffort(core.ReasoningEffortXhigh), 75, false},
		{"low", 0, true, testEffort(core.ReasoningEffortLow), 50, false},
		{"max", 0, true, testEffort(core.ReasoningEffortMax), 100, false},
		{"later message", 1, true, testEffort(core.ReasoningEffortHigh), 0, true},
		{"no thinking", 0, false, testEffort(core.ReasoningEffortMax), 0, true},
	}
	for _, testCase := range v41Cases {
		t.Run("v41 "+testCase.name, func(t *testing.T) {
			got := dsv41.New().RenderReasoningEffort(testCase.index, testCase.thinkingMode, testCase.effort)
			want := ""
			if !testCase.wantEmpty {
				want = reasoningEffortV41(testCase.score)
			}
			if got != want {
				t.Errorf("effort = %q, want %q", got, want)
			}
		})
	}

	t.Run("rendered prefixes", func(t *testing.T) {
		max := testConversation(testUser("hi"))
		max.ReasoningEffort = testEffort(core.ReasoningEffortMax)
		prompt := dsv4.New().RenderConversation(max).Prompt
		if !strings.HasPrefix(prompt, v4.BOSToken+reasoningEffortMax) {
			t.Errorf("prompt %q does not start with the maximum-effort prefix", prompt)
		}

		low := testConversation(testUser("hi"))
		low.ReasoningEffort = testEffort(core.ReasoningEffortLow)
		prompt = dsv41.New().RenderConversation(low).Prompt
		if !strings.HasPrefix(prompt, v4.BOSToken+v4.SystemSPToken+reasoningEffortV41(50)) {
			t.Errorf("prompt %q does not start with the low-effort prefix", prompt)
		}
	})
}

// portTestTokenizer records the encoded text and returns configured results.
type portTestTokenizer struct {
	text string
	ids  []uint32
	err  error
}

func (tokenizer *portTestTokenizer) EncodeIDs(text string) ([]uint32, error) {
	tokenizer.text = text
	if tokenizer.err != nil {
		return nil, tokenizer.err
	}
	return tokenizer.ids, nil
}

func TestEncode(t *testing.T) {
	conversation := testConversation(testUser("hi"))

	t.Run("missing tokenizer", func(t *testing.T) {
		_, err := dsv4.New().Encode(conversation)
		if err == nil || err.Error() != "no tokenizer is attached to the encoding" {
			t.Fatalf("err = %v, want the missing-tokenizer error", err)
		}
		var encodingError *encoding.EncodingError
		if !errors.As(err, &encodingError) || encodingError.Kind != encoding.EncodingMissingTokenizer {
			t.Errorf("err = %#v, want kind EncodingMissingTokenizer", err)
		}
	})

	t.Run("encodes the rendered prompt", func(t *testing.T) {
		tokenizer := &portTestTokenizer{ids: []uint32{1, 2, 3}}
		port := dsv4.New().WithTokenizer(tokenizer)
		ids, err := port.Encode(conversation)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(ids, []uint32{1, 2, 3}) {
			t.Errorf("ids = %v, want [1 2 3]", ids)
		}
		if want := port.RenderConversation(conversation).Prompt; tokenizer.text != want {
			t.Errorf("encoded text = %q, want %q", tokenizer.text, want)
		}
	})

	t.Run("wraps tokenizer failures", func(t *testing.T) {
		tokenizer := &portTestTokenizer{err: errors.New("boom")}
		_, err := dsv41.New().WithTokenizer(tokenizer).Encode(conversation)
		if err == nil || err.Error() != "failed to encode conversation: boom" {
			t.Fatalf("err = %v, want the wrapped encode error", err)
		}
		var encodingError *encoding.EncodingError
		if !errors.As(err, &encodingError) || encodingError.Kind != encoding.EncodingEncode ||
			encodingError.Detail != "boom" {
			t.Errorf("err = %#v, want kind EncodingEncode with detail boom", err)
		}
	})
}
