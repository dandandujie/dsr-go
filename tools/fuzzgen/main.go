// Command fuzzgen generates randomized differential-test cases.
//
// The generated corpus is replayed through both the Go packages (golden/) and
// the original Rust implementation (tools/refgen); every case must produce
// byte-identical output. Generation is deterministic for a given seed.
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"
)

// fence is three backticks, used by the generated JSON-fence outputs.
const fence = "\x60\x60\x60"

func main() {
	count := flag.Int("n", 1500, "number of cases to generate")
	seed := flag.Int64("seed", 20260910, "random seed")
	out := flag.String("o", "-", "output file, - for stdout")
	prefix := flag.String("prefix", "", "prefix added to every case name")
	flag.Parse()

	writer := os.Stdout
	if *out != "-" {
		file, err := os.Create(*out)
		if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		writer = file
	}
	buffered := bufio.NewWriter(writer)
	defer func() { _ = buffered.Flush() }()

	generator := &generator{rand: rand.New(rand.NewSource(*seed)), prefix: *prefix}
	encoder := json.NewEncoder(buffered)
	for index := 0; index < *count; index++ {
		item := generator.caseAt(index)
		if item == nil {
			continue
		}
		if err := encoder.Encode(item); err != nil {
			log.Fatal(err)
		}
	}
}

type generator struct {
	rand   *rand.Rand
	prefix string
}

// caseAt builds one corpus case; the op cycles through the supported kinds.
func (g *generator) caseAt(index int) map[string]any {
	switch index % 10 {
	case 0, 1, 2, 3, 4:
		return g.protocolCase(index, "convert")
	case 5:
		return g.protocolCase(index, "render")
	case 6:
		return g.protocolCase(index, "encode")
	case 7, 8:
		return g.streamCase(index)
	case 9:
		if g.rand.Intn(10) < 6 {
			return g.tokenizerCase(index)
		}
		return g.imageCase(index)
	default:
		return g.tokenizerCase(index)
	}
}

func (g *generator) protocolCase(index int, op string) map[string]any {
	protocol := []string{"chat_completions", "chat_completions", "chat_completions", "messages", "responses"}[g.rand.Intn(5)]
	var body map[string]any
	switch protocol {
	case "chat_completions":
		body = g.chatBody()
	case "messages":
		body = g.messagesBody()
	default:
		body = g.responsesBody()
	}
	item := map[string]any{
		"name":     fmt.Sprintf(g.prefix+"fuzz_%04d_%s_%s", index, protocol, op),
		"op":       op,
		"protocol": protocol,
		"body":     body,
	}
	if op == "encode" {
		tokenizerName := "v41"
		if g.rand.Intn(2) == 0 {
			tokenizerName = "v4"
			item["encoding"] = "v4"
		}
		item["tokenizer"] = tokenizerName
	}
	if op == "render" && g.rand.Intn(2) == 0 {
		item["encoding"] = "v4"
	}
	return item
}

// ---------------------------------------------------------------- chat

func (g *generator) chatBody() map[string]any {
	body := map[string]any{"model": g.modelName()}
	thinkingDisabled := false
	if g.rand.Intn(3) == 0 {
		if g.rand.Intn(2) == 0 {
			body["thinking"] = map[string]any{"type": "disabled"}
			thinkingDisabled = true
		} else {
			body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": g.rand.Intn(4096)}
		}
	}
	if g.rand.Intn(3) == 0 {
		body["reasoning_effort"] = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}[g.rand.Intn(7)]
	}
	if g.rand.Intn(4) == 0 {
		body["temperature"] = float64(g.rand.Intn(20)) / 10
	}
	if g.rand.Intn(4) == 0 {
		body["max_tokens"] = 1 + g.rand.Intn(4096)
	}
	if g.rand.Intn(5) == 0 {
		body["stop"] = []string{g.word(), g.word()}
	}
	if g.rand.Intn(6) == 0 {
		body["stream"] = true
		if g.rand.Intn(2) == 0 {
			body["stream_options"] = map[string]any{"include_usage": true}
		}
	}
	toolNames := []string{}
	if g.rand.Intn(2) == 0 {
		tools, names := g.chatTools()
		body["tools"] = tools
		toolNames = names
	}
	if len(toolNames) > 0 && g.rand.Intn(2) == 0 {
		switch g.rand.Intn(4) {
		case 0:
			body["tool_choice"] = "auto"
		case 1:
			if !thinkingDisabled {
				body["thinking"] = map[string]any{"type": "disabled"}
				thinkingDisabled = true
			}
			body["tool_choice"] = "required"
		case 2:
			if !thinkingDisabled {
				body["thinking"] = map[string]any{"type": "disabled"}
				thinkingDisabled = true
			}
			body["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": toolNames[g.rand.Intn(len(toolNames))]}}
		default:
			body["tool_choice"] = "none"
		}
	}
	if g.rand.Intn(5) == 0 {
		body["response_format"] = map[string]any{"type": "json_object"}
	}
	body["messages"] = g.chatMessages(toolNames)
	return body
}

func (g *generator) chatTools() ([]map[string]any, []string) {
	count := 1 + g.rand.Intn(2)
	tools := make([]map[string]any, 0, count)
	names := make([]string, 0, count)
	for index := 0; index < count; index++ {
		name := fmt.Sprintf("%s_%d", g.word(), index)
		names = append(names, name)
		parameters := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{"type": "string", "description": g.sentence()},
				"days":     map[string]any{"type": "number", "default": float64(g.rand.Intn(7))},
			},
			"required":             []string{"location"},
			"additionalProperties": false,
		}
		function := map[string]any{"name": name, "parameters": parameters}
		if g.rand.Intn(2) == 0 {
			function["description"] = g.sentence()
		}
		if g.rand.Intn(3) == 0 {
			function["strict"] = g.rand.Intn(2) == 0
		}
		tools = append(tools, map[string]any{"type": "function", "function": function})
	}
	return tools, names
}

func (g *generator) chatMessages(toolNames []string) []map[string]any {
	messages := []map[string]any{}
	if g.rand.Intn(2) == 0 {
		messages = append(messages, map[string]any{"role": "system", "content": g.sentence()})
	}
	turns := 1 + g.rand.Intn(3)
	for turn := 0; turn < turns; turn++ {
		messages = append(messages, g.chatUserMessage())
		if len(toolNames) > 0 && g.rand.Intn(3) == 0 {
			name := toolNames[g.rand.Intn(len(toolNames))]
			callID := fmt.Sprintf("call_%d_%d", turn, g.rand.Intn(1000))
			assistant := map[string]any{
				"role":    "assistant",
				"content": g.maybeText(),
				"tool_calls": []map[string]any{{
					"id":   callID,
					"type": "function",
					"function": map[string]any{
						"name":      name,
						"arguments": "{\"location\": \"" + g.word() + "\"}",
					},
				}},
			}
			if g.rand.Intn(3) == 0 {
				assistant["reasoning_content"] = g.sentence()
			}
			messages = append(messages, assistant)
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": callID,
				"content":      g.sentence(),
			})
			continue
		}
		if g.rand.Intn(3) == 0 {
			messages = append(messages, map[string]any{"role": "assistant", "content": g.sentence()})
		}
	}
	if g.rand.Intn(4) == 0 {
		messages = append(messages, map[string]any{"role": "latest_reminder", "content": g.sentence()})
	}
	return messages
}

func (g *generator) chatUserMessage() map[string]any {
	if g.rand.Intn(3) != 0 {
		return map[string]any{"role": "user", "content": g.sentence()}
	}
	blocks := []map[string]any{{"type": "text", "text": g.sentence()}}
	for count := g.rand.Intn(2); count > 0; count-- {
		switch g.rand.Intn(3) {
		case 0:
			blocks = append(blocks, map[string]any{"type": "image_url", "image_url": map[string]any{
				"url":    "https://example.com/" + g.word() + ".png",
				"detail": []string{"low", "high", "auto", "original"}[g.rand.Intn(4)],
			}})
		case 1:
			blocks = append(blocks, map[string]any{"type": "image_url", "image_url": map[string]any{
				"url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUg" + strings.Repeat("A", 1+g.rand.Intn(8)),
			}})
		default:
			blocks = append(blocks, map[string]any{"type": "text", "text": g.sentence()})
		}
	}
	return map[string]any{"role": "user", "content": blocks}
}

// ---------------------------------------------------------------- messages

func (g *generator) messagesBody() map[string]any {
	body := map[string]any{"model": g.modelName(), "max_tokens": 1 + g.rand.Intn(4096)}
	if g.rand.Intn(2) == 0 {
		body["system"] = g.sentence()
	}
	if g.rand.Intn(4) == 0 {
		thinking := map[string]any{"type": []string{"enabled", "disabled", "adaptive"}[g.rand.Intn(3)]}
		if g.rand.Intn(2) == 0 {
			thinking["budget_tokens"] = 1024 + g.rand.Intn(8192)
		}
		body["thinking"] = thinking
	}
	if g.rand.Intn(4) == 0 {
		body["temperature"] = float64(g.rand.Intn(15)) / 10
	}
	if g.rand.Intn(4) == 0 {
		body["stop_sequences"] = []string{g.word()}
	}
	toolNames := []string{}
	if g.rand.Intn(2) == 0 {
		count := 1 + g.rand.Intn(2)
		tools := make([]map[string]any, 0, count)
		for index := 0; index < count; index++ {
			name := fmt.Sprintf("%s_%d", g.word(), index)
			toolNames = append(toolNames, name)
			tool := map[string]any{
				"name":         name,
				"input_schema": map[string]any{"type": "object", "properties": map[string]any{"location": map[string]any{"type": "string"}}},
			}
			if g.rand.Intn(2) == 0 {
				tool["description"] = g.sentence()
			}
			tools = append(tools, tool)
		}
		body["tools"] = tools
	}
	if len(toolNames) > 0 && g.rand.Intn(2) == 0 {
		switch g.rand.Intn(3) {
		case 0:
			body["tool_choice"] = map[string]any{"type": "auto"}
		case 1:
			body["tool_choice"] = map[string]any{"type": "any"}
		default:
			body["tool_choice"] = map[string]any{"type": "tool", "name": toolNames[g.rand.Intn(len(toolNames))]}
		}
	}
	body["messages"] = g.messagesMessages(toolNames)
	return body
}

func (g *generator) messagesMessages(toolNames []string) []map[string]any {
	messages := []map[string]any{}
	turns := 1 + g.rand.Intn(3)
	for turn := 0; turn < turns; turn++ {
		messages = append(messages, map[string]any{"role": "user", "content": g.messagesUserContent()})
		if len(toolNames) > 0 && g.rand.Intn(3) == 0 {
			name := toolNames[g.rand.Intn(len(toolNames))]
			callID := fmt.Sprintf("toolu_%d_%d", turn, g.rand.Intn(1000))
			blocks := []map[string]any{}
			if g.rand.Intn(2) == 0 {
				blocks = append(blocks, map[string]any{"type": "thinking", "thinking": g.sentence()})
			}
			blocks = append(blocks, map[string]any{
				"type":  "tool_use",
				"id":    callID,
				"name":  name,
				"input": map[string]any{"location": g.word()},
			})
			messages = append(messages, map[string]any{"role": "assistant", "content": blocks})
			messages = append(messages, map[string]any{"role": "user", "content": []map[string]any{{
				"type":        "tool_result",
				"tool_use_id": callID,
				"content":     g.sentence(),
			}}})
			continue
		}
		if g.rand.Intn(3) == 0 {
			messages = append(messages, map[string]any{"role": "assistant", "content": g.sentence()})
		}
	}
	return messages
}

func (g *generator) messagesUserContent() any {
	if g.rand.Intn(3) != 0 {
		return g.sentence()
	}
	blocks := []map[string]any{{"type": "text", "text": g.sentence()}}
	if g.rand.Intn(2) == 0 {
		blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{
			"type":       "base64",
			"media_type": "image/png",
			"data":       "iVBORw0KGgoAAAANSUhEUg" + strings.Repeat("B", 1+g.rand.Intn(6)),
		}})
	}
	if g.rand.Intn(4) == 0 {
		blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{
			"type": "url",
			"url":  "https://example.com/" + g.word() + ".jpg",
		}})
	}
	if g.rand.Intn(4) == 0 {
		blocks = append(blocks, map[string]any{"type": "document", "source": map[string]any{
			"type":       "base64",
			"media_type": "application/pdf",
			"data":       "JVBERi0xLjQK",
		}})
	}
	return blocks
}

// ---------------------------------------------------------------- responses

func (g *generator) responsesBody() map[string]any {
	body := map[string]any{"model": g.modelName()}
	if g.rand.Intn(2) == 0 {
		body["instructions"] = g.sentence()
	}
	if g.rand.Intn(3) == 0 {
		body["thinking"] = map[string]any{"type": []string{"enabled", "disabled"}[g.rand.Intn(2)]}
	}
	if g.rand.Intn(4) == 0 {
		body["reasoning"] = map[string]any{"effort": []string{"low", "medium", "high"}[g.rand.Intn(3)]}
	}
	if g.rand.Intn(4) == 0 {
		body["max_output_tokens"] = 1 + g.rand.Intn(2048)
	}
	if g.rand.Intn(5) == 0 {
		body["text"] = map[string]any{"format": map[string]any{"type": "json_object"}}
	}
	if g.rand.Intn(3) == 0 {
		count := 1 + g.rand.Intn(2)
		tools := make([]map[string]any, 0, count)
		for index := 0; index < count; index++ {
			tools = append(tools, map[string]any{
				"type":        "function",
				"name":        fmt.Sprintf("%s_%d", g.word(), index),
				"description": g.sentence(),
				"parameters":  map[string]any{"type": "object", "properties": map[string]any{"location": map[string]any{"type": "string"}}},
			})
		}
		body["tools"] = tools
		if g.rand.Intn(2) == 0 {
			body["tool_choice"] = []string{"auto", "required", "none"}[g.rand.Intn(3)]
		}
	}
	body["input"] = g.responsesInput()
	if g.rand.Intn(6) == 0 {
		body["stream"] = true
	}
	return body
}

func (g *generator) responsesInput() any {
	if g.rand.Intn(3) == 0 {
		return g.sentence()
	}
	items := []map[string]any{}
	turns := 1 + g.rand.Intn(3)
	for turn := 0; turn < turns; turn++ {
		content := []map[string]any{{"type": "input_text", "text": g.sentence()}}
		if g.rand.Intn(3) == 0 {
			content = append(content, map[string]any{
				"type":      "input_image",
				"image_url": "https://example.com/" + g.word() + ".png",
				"detail":    []string{"low", "high", "auto"}[g.rand.Intn(3)],
			})
		}
		items = append(items, map[string]any{"type": "message", "role": "user", "content": content})
		if g.rand.Intn(3) == 0 {
			callID := fmt.Sprintf("call_%d_%d", turn, g.rand.Intn(1000))
			items = append(items, map[string]any{
				"type":      "function_call",
				"call_id":   callID,
				"name":      g.word(),
				"arguments": "{\"a\": 1}",
			})
			items = append(items, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  g.sentence(),
			})
		}
	}
	return items
}

// ---------------------------------------------------------------- streaming

// streamCase builds a mock backend output and splits it into random chunks.
func (g *generator) streamCase(index int) map[string]any {
	protocol := []string{"chat_completions", "chat_completions", "messages", "responses"}[g.rand.Intn(4)]
	var body map[string]any
	switch protocol {
	case "chat_completions":
		body = map[string]any{
			"model":    "m",
			"messages": []map[string]any{{"role": "user", "content": "q"}},
			"thinking": map[string]any{"type": "disabled"},
		}
		if g.rand.Intn(2) == 0 {
			body["tools"] = []map[string]any{{"type": "function", "function": map[string]any{
				"name":       "get_weather",
				"parameters": map[string]any{"type": "object"},
			}}}
		}
		if g.rand.Intn(4) == 0 {
			body["stop"] = "STOP"
		}
		if g.rand.Intn(5) == 0 {
			body["response_format"] = map[string]any{"type": "json_object"}
			body["messages"] = []map[string]any{
				{"role": "system", "content": "answer in json"},
				{"role": "user", "content": "q"},
			}
		}
	case "messages":
		body = map[string]any{
			"model":      "m",
			"max_tokens": 100,
			"messages":   []map[string]any{{"role": "user", "content": "q"}},
			"thinking":   map[string]any{"type": "disabled"},
		}
		if g.rand.Intn(2) == 0 {
			body["tools"] = []map[string]any{{"name": "get_weather", "input_schema": map[string]any{"type": "object"}}}
		}
	default:
		body = map[string]any{
			"model":    "m",
			"input":    "q",
			"thinking": map[string]any{"type": "disabled"},
		}
		if g.rand.Intn(2) == 0 {
			body["tools"] = []map[string]any{{"type": "function", "name": "get_weather", "parameters": map[string]any{"type": "object"}}}
		}
	}
	if g.rand.Intn(3) == 0 {
		body["thinking"] = map[string]any{"type": "enabled"}
	}

	output := g.streamOutput()
	chunks := g.splitChunks(output)
	chunkSpecs := make([]map[string]any, 0, len(chunks)+2)
	if g.rand.Intn(4) != 0 {
		chunkSpecs = append(chunkSpecs, map[string]any{
			"kind": "ready", "system_fingerprint": "fp-" + g.word(),
			"prompt_tokens": 1 + g.rand.Intn(100), "prompt_cache_hit_tokens": g.rand.Intn(10),
		})
	}
	for _, chunk := range chunks {
		if g.rand.Intn(6) == 0 {
			chunkSpecs = append(chunkSpecs, map[string]any{"kind": "token_text", "text": chunk})
			continue
		}
		chunkSpecs = append(chunkSpecs, map[string]any{
			"kind": "text", "content": chunk, "content_tokens": 1 + g.rand.Intn(5),
		})
	}
	if g.rand.Intn(4) != 0 {
		chunkSpecs = append(chunkSpecs, map[string]any{
			"kind":          "finish",
			"finish_reason": []string{"stop", "stop", "length", "content_filter"}[g.rand.Intn(4)],
		})
	}
	item := map[string]any{
		"name":     fmt.Sprintf(g.prefix+"fuzz_%04d_%s_stream", index, protocol),
		"op":       "stream",
		"protocol": protocol,
		"body":     body,
		"chunks":   chunkSpecs,
	}
	if g.rand.Intn(2) == 0 || containsTokenText(chunkSpecs) {
		item["tokenizer"] = "v41"
	}
	return item
}

func containsTokenText(specs []map[string]any) bool {
	for _, spec := range specs {
		if spec["kind"] == "token_text" {
			return true
		}
	}
	return false
}

// streamOutput builds a random assistant output with reasoning, tool calls,
// JSON output and stop sequences.
func (g *generator) streamOutput() string {
	parts := []string{}
	if g.rand.Intn(2) == 0 {
		parts = append(parts, g.sentence(), g.reasoningTail())
	}
	switch g.rand.Intn(6) {
	case 0:
		parts = append(parts, toolCallMarkup(g.word(), g.word(), g.rand.Intn(3)))
	case 1:
		parts = append(parts, fence+"json\n{\"a\": "+fmt.Sprint(g.rand.Intn(100))+"}\n"+fence)
	case 2:
		parts = append(parts, "pre {\"a\": 1} post")
	case 3:
		parts = append(parts, "answer STOP tail")
	case 4:
		parts = append(parts, toolCallMarkupPartial(g.word(), g.word()))
	default:
		parts = append(parts, g.sentence())
	}
	if g.rand.Intn(4) == 0 {
		parts = append(parts, "\n\n"+toolCallMarkup(g.word(), g.word(), 1))
	}
	if g.rand.Intn(6) == 0 {
		parts = append(parts, "</think>extra")
	}
	return strings.Join(parts, "")
}

func (g *generator) reasoningTail() string {
	if g.rand.Intn(2) == 0 {
		return "</think>"
	}
	return " </think> "
}

func toolCallMarkup(name, key string, params int) string {
	var builder strings.Builder
	builder.WriteString("<｜DSML｜ calls>\n<｜DSML｜ invoke name=\"" + name + "\">\n")
	for index := 0; index < params; index++ {
		parameter := fmt.Sprintf("%s%d", key, index)
		if index%2 == 0 {
			builder.WriteString("<｜DSML｜ parameter name=\"" + parameter + "\" string=\"true\">" + key + "</｜DSML｜ parameter>\n")
		} else {
			builder.WriteString("<｜DSML｜ parameter name=\"" + parameter + "\" string=\"false\">" + fmt.Sprint(index) + "</｜DSML｜ parameter>\n")
		}
	}
	builder.WriteString("</｜DSML｜ invoke>\n</｜DSML｜ calls>")
	return builder.String()
}

func toolCallMarkupPartial(name, key string) string {
	return "<｜DSML｜ calls>\n<｜DSML｜ invoke name=\"" + name + "\">\n<｜DSML｜ parameter name=\"" + key + "\" string=\"true\">par"
}

// splitChunks splits text into random rune-aligned chunks.
func (g *generator) splitChunks(text string) []string {
	var chunks []string
	remaining := text
	for len(remaining) > 0 {
		size := 1 + g.rand.Intn(8)
		if size >= len(remaining) {
			chunks = append(chunks, remaining)
			break
		}
		cut := size
		for cut < len(remaining) && !utf8.RuneStart(remaining[cut]) {
			cut++
		}
		chunks = append(chunks, remaining[:cut])
		remaining = remaining[cut:]
	}
	if len(chunks) == 0 {
		chunks = []string{""}
	}
	return chunks
}

// ---------------------------------------------------------------- tokenizer

func (g *generator) tokenizerCase(index int) map[string]any {
	name := "v41"
	if g.rand.Intn(2) == 0 {
		name = "v4"
	}
	if g.rand.Intn(3) == 0 {
		ids := make([]uint32, 0, 1+g.rand.Intn(24))
		for count := 1 + g.rand.Intn(24); count > 0; count-- {
			switch g.rand.Intn(4) {
			case 0:
				ids = append(ids, uint32(g.rand.Intn(128)))
			case 1:
				ids = append(ids, 128000+uint32(g.rand.Intn(1280)))
			default:
				ids = append(ids, uint32(g.rand.Intn(129280)))
			}
		}
		return map[string]any{
			"name":                fmt.Sprintf(g.prefix+"fuzz_%04d_detokenize_%s", index, name),
			"op":                  "detokenize",
			"ids":                 ids,
			"skip_special_tokens": g.rand.Intn(2) == 0,
			"tokenizer":           name,
		}
	}
	return map[string]any{
		"name":      fmt.Sprintf(g.prefix+"fuzz_%04d_tokenize_%s", index, name),
		"op":        "tokenize",
		"text":      g.randomText(),
		"tokenizer": name,
	}
}

// randomText mixes words, CJK, emoji, whitespace, control characters and prompt
// markers.
func (g *generator) randomText() string {
	var builder strings.Builder
	pieces := 1 + g.rand.Intn(6)
	for index := 0; index < pieces; index++ {
		switch g.rand.Intn(10) {
		case 0:
			builder.WriteString(g.word())
		case 1:
			builder.WriteString("你好，世界")
		case 2:
			builder.WriteString("こんにちは")
		case 3:
			builder.WriteString("🙂🚀")
		case 4:
			builder.WriteString([]string{"  ", "\n", "\n\n", "\t", " \n ", "\r\n", "   "}[g.rand.Intn(7)])
		case 5:
			builder.WriteString([]string{"<｜begin▁of▁sentence｜>", "<｜end▁of▁sentence｜>", "<｜User｜>", "<｜Assistant｜>", "<｜DSML｜ calls>", "<think>", "</think>", "<｜image｜>"}[g.rand.Intn(8)])
		case 6:
			builder.WriteString([]string{fence + "code" + fence, "\"quoted\"", "'single'", "a\b"}[g.rand.Intn(4)])
		case 7:
			builder.WriteString(fmt.Sprint(g.rand.Intn(100000)))
		case 8:
			builder.WriteString([]string{"json", "{", "}", "[", "]", ":", ","}[g.rand.Intn(7)])
		default:
			builder.WriteString(g.sentence())
		}
	}
	return builder.String()
}

// ---------------------------------------------------------------- images

// imageCase builds an image-resolution case for the image package.
func (g *generator) imageCase(index int) map[string]any {
	count := 1 + g.rand.Intn(4)
	sources := make([]map[string]any, 0, count)
	for position := 0; position < count; position++ {
		detail := []string{"low", "high", "original", "auto"}[g.rand.Intn(4)]
		suffix := fmt.Sprintf("%d-%d", index, position)
		switch g.rand.Intn(8) {
		case 0, 1:
			payload := base64.StdEncoding.EncodeToString([]byte(g.sentence() + suffix))
			sources = append(sources, map[string]any{"kind": "data_url", "data_url": "data:image/png;base64," + payload, "detail": detail})
		case 2:
			payload := base64.StdEncoding.EncodeToString([]byte(strings.Repeat(g.word(), 1+g.rand.Intn(40)) + suffix))
			sources = append(sources, map[string]any{"kind": "data_url", "data_url": "data:image/jpeg;base64," + payload, "detail": detail})
		case 3:
			sources = append(sources, map[string]any{"kind": "data_url", "data_url": "data:image/webp," + url.QueryEscape(g.sentence()+suffix), "detail": detail})
		case 4:
			sources = append(sources, map[string]any{"kind": "data_url", "data_url": "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte(suffix)), "detail": detail})
		case 5:
			url := "https://example.com/" + g.word() + "/" + suffix + ".png"
			if g.rand.Intn(5) == 0 {
				url = "https://example.com/fail-once/" + suffix + ".png"
			}
			if g.rand.Intn(8) == 0 {
				url = "https://example.com/fail-always/" + suffix + ".png"
			}
			sources = append(sources, map[string]any{"kind": "url", "url": url, "detail": detail})
		case 6:
			sources = append(sources, map[string]any{"kind": "bytes", "hex": hex.EncodeToString([]byte(g.sentence() + suffix)), "detail": detail})
		default:
			sources = append(sources, map[string]any{"kind": "bytes", "hex": "", "detail": detail})
		}
	}
	return map[string]any{
		"name":    fmt.Sprintf(g.prefix+"fuzz_%04d_image", index),
		"op":      "image",
		"sources": sources,
	}
}

// ---------------------------------------------------------------- helpers

var words = []string{
	"weather", "paris", "answer", "tool", "value", "delta", "json", "alpha",
	"beta", "gamma", "result", "note", "data", "item", "query", "response",
}

func (g *generator) word() string { return words[g.rand.Intn(len(words))] }

func (g *generator) sentence() string {
	count := 1 + g.rand.Intn(6)
	parts := make([]string, 0, count)
	for index := 0; index < count; index++ {
		parts = append(parts, g.word())
	}
	return strings.Join(parts, " ")
}

func (g *generator) maybeText() any {
	if g.rand.Intn(4) == 0 {
		return nil
	}
	if g.rand.Intn(6) == 0 {
		return ""
	}
	return g.sentence()
}

func (g *generator) modelName() string {
	return []string{"deepseek-flash", "deepseek-reasoner", "m"}[g.rand.Intn(3)]
}
