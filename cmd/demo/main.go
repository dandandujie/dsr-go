// Command demo serves the encoding and decoding demo page.
//
// The page renders a protocol request into a DeepSeek V4.1 prompt, highlights
// the prompt's special tokens, and decodes complete assistant output back into
// the protocol's response format.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/encoding/v4"
	"github.com/dandandujie/dsr-go/encoding/v4/dsv41"
	"github.com/dandandujie/dsr-go/recipe/anthropic/messages"
	"github.com/dandandujie/dsr-go/recipe/openai/chatcompletion"
	"github.com/dandandujie/dsr-go/recipe/openai/responses"
	"github.com/dandandujie/dsr-go/recipe/request"
	"github.com/dandandujie/dsr-go/recipe/response"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// defaultAddr is the demo's default listen address.
const defaultAddr = "127.0.0.1:7778"

// demoModel is the model name used when a request does not carry one.
const demoModel = "encoding-decoding-demo"

// highlight describes one prompt marker shown by the demo page.
type highlight struct {
	name        string
	literals    []string
	description string
}

var highlights = []highlight{
	{"bos", []string{v4.BOSToken}, "Begin of sentence. Marks the start of the prompt."},
	{"system", []string{v4.SystemSPToken}, "System turn marker. Starts a system message."},
	{"user", []string{v4.UserSPToken}, "User turn marker. Starts a user message."},
	{"assistant", []string{v4.AssistantSPToken}, "Assistant turn marker. Starts an assistant message."},
	{"thinking_start", []string{v4.ThinkingStartToken}, "Starts the reasoning content."},
	{"thinking_end", []string{v4.ThinkingEndToken}, "Ends the reasoning content."},
	{"latest_reminder", []string{v4.LatestReminderSPToken}, "Starts the latest reminder message."},
	{"eos", []string{v4.EOSToken}, "End of sentence. Terminates the message."},
	{"dsml", []string{v4.DSMLSPToken}, "DeepSeek markup tag. Structures tool calls in the prompt."},
	{"tool_result", []string{"<tool_result>", "</tool_result>"}, "Contains the result of a tool call."},
	{"image", []string{core.ImageSpecialToken}, "Marks one image in the prompt."},
	{"system_reminder", []string{"<system-reminder>", "</system-reminder>"}, "Contains a system message within the conversation."},
}

// segment is one highlighted piece of a prompt.
type segment struct {
	kind        string
	text        string
	name        string
	token       string
	description string
}

func textSegment(text string) segment { return segment{kind: "text", text: text} }

func tokenSegment(item highlight, token string) segment {
	return segment{
		kind:        "token",
		name:        item.name,
		token:       token,
		description: item.description,
	}
}

// value renders the segment as an ordered JSON value.
func (s segment) value() jsonx.Value {
	out := jsonx.NewObject()
	if s.kind == "text" {
		out.Set("kind", "text")
		out.Set("text", s.text)
		return out
	}
	out.Set("kind", "token")
	out.Set("name", s.name)
	out.Set("token", s.token)
	out.Set("description", s.description)
	return out
}

// segmentPrompt splits a prompt into plain text and marker segments.
func segmentPrompt(prompt string) []segment {
	var segments []segment
	rest := prompt
	for {
		start, item, ok := findEarliest(rest)
		if !ok {
			break
		}
		if before := rest[:start]; before != "" {
			segments = append(segments, textSegment(before))
		}
		tag := rest[start:]
		end := len(tag)
		if index := strings.IndexByte(tag, '>'); index >= 0 {
			end = index + 1
		}
		segments = append(segments, tokenSegment(item, tag[:end]))
		rest = tag[end:]
	}
	if rest != "" {
		segments = append(segments, textSegment(rest))
	}
	return segments
}

func findEarliest(rest string) (int, highlight, bool) {
	best := -1
	var bestItem highlight
	for _, item := range highlights {
		for _, literal := range item.literals {
			index := strings.Index(rest, literal)
			if index < 0 {
				continue
			}
			start := tagStart(rest, index, literal)
			if best < 0 || start < best {
				best = start
				bestItem = item
			}
		}
	}
	if best < 0 {
		return 0, highlight{}, false
	}
	return best, bestItem, true
}

func tagStart(rest string, index int, literal string) int {
	if strings.HasPrefix(literal, "<") {
		return index
	}
	start := index
	if start > 0 && rest[start-1] == '/' {
		start--
	}
	if start > 0 && rest[start-1] == '<' {
		start--
	}
	return start
}

// apiFormat names a supported request format.
type apiFormat string

const (
	formatChatCompletions apiFormat = "chat_completions"
	formatResponses       apiFormat = "responses"
	formatMessages        apiFormat = "messages"
)

// adapter binds one protocol package to the demo without generics.
type adapter struct {
	convert     func(body []byte) (*request.ConversationRequest, error)
	newResponse func(id, model string, created uint64) response.ProtocolResponse
}

func adapterFor(format apiFormat) (adapter, error) {
	switch format {
	case formatChatCompletions:
		return adapter{
			convert: func(body []byte) (*request.ConversationRequest, error) {
				var typed chatcompletion.ChatCompletionRequest
				if err := json.Unmarshal(body, &typed); err != nil {
					return nil, request.BadRequestf("invalid request body: %v", err)
				}
				return typed.Convert(request.NewConversionOptions())
			},
			newResponse: func(id, model string, created uint64) response.ProtocolResponse {
				return chatcompletion.NewChatCompletionResponse(id, model, created)
			},
		}, nil
	case formatResponses:
		return adapter{
			convert: func(body []byte) (*request.ConversationRequest, error) {
				var typed responses.ResponsesRequest
				if err := json.Unmarshal(body, &typed); err != nil {
					return nil, request.BadRequestf("invalid request body: %v", err)
				}
				return typed.Convert(request.NewConversionOptions())
			},
			newResponse: func(id, model string, created uint64) response.ProtocolResponse {
				return responses.NewResponsesResponse(id, model, created)
			},
		}, nil
	case formatMessages:
		return adapter{
			convert: func(body []byte) (*request.ConversationRequest, error) {
				var typed messages.MessagesRequest
				if err := json.Unmarshal(body, &typed); err != nil {
					return nil, request.BadRequestf("invalid request body: %v", err)
				}
				return typed.Convert(request.NewConversionOptions())
			},
			newResponse: func(id, model string, created uint64) response.ProtocolResponse {
				return messages.NewMessagesResponse(id, model, created)
			},
		}, nil
	}
	return adapter{}, request.BadRequestf("unknown format: %s", format)
}

// convertBody deserializes a request body and adds the demo's default model.
func convertBody(format apiFormat, body json.RawMessage) (*request.ConversationRequest, error) {
	body = withDefaultModel(body)
	impl, err := adapterFor(format)
	if err != nil {
		return nil, err
	}
	return impl.convert(body)
}

func withDefaultModel(body json.RawMessage) json.RawMessage {
	if len(body) == 0 {
		return json.RawMessage("{\"model\": \"" + demoModel + "\"}")
	}
	value, err := jsonx.Parse(body)
	if err != nil {
		return body
	}
	object, ok := value.(*jsonx.Object)
	if !ok {
		return body
	}
	if !object.Has("model") {
		object.Set("model", demoModel)
	}
	encoded, err := jsonx.Marshal(object)
	if err != nil {
		return body
	}
	return encoded
}

// renderRequest is the body of POST /api/render.
type renderRequest struct {
	Format apiFormat       `json:"format"`
	Body   json.RawMessage `json:"body"`
}

// decodeRequest is the body of POST /api/decode.
type decodeRequest struct {
	Format       apiFormat       `json:"format"`
	Body         json.RawMessage `json:"body"`
	Output       string          `json:"output"`
	FinishReason string          `json:"finish_reason"`
}

func main() {
	addr := os.Getenv("DEMO_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(writer http.ResponseWriter, _ *http.Request) {
		serveAsset(writer, "text/html; charset=utf-8", "static/index.html")
	})
	mux.HandleFunc("GET /app.js", func(writer http.ResponseWriter, _ *http.Request) {
		serveAsset(writer, "text/javascript; charset=utf-8", "static/app.js")
	})
	mux.HandleFunc("GET /style.css", func(writer http.ResponseWriter, _ *http.Request) {
		serveAsset(writer, "text/css; charset=utf-8", "static/style.css")
	})
	mux.HandleFunc("GET /favicon-32x32.png", func(writer http.ResponseWriter, _ *http.Request) {
		serveAsset(writer, "image/png", "static/favicon-32x32.png")
	})
	mux.HandleFunc("POST /api/render", handleRender)
	mux.HandleFunc("POST /api/decode", handleDecode)
	log.Printf("deepseek-recipe demo page: http://%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func serveAsset(writer http.ResponseWriter, contentType, path string) {
	data, err := demoAssets.ReadFile(path)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = writer.Write(data)
}

func handleRender(writer http.ResponseWriter, httpRequest *http.Request) {
	var payload renderRequest
	if err := json.NewDecoder(httpRequest.Body).Decode(&payload); err != nil {
		writeError(writer, requestErrorf("invalid request body: %v", err))
		return
	}
	converted, err := convertBody(payload.Format, payload.Body)
	if err != nil {
		writeError(writer, err)
		return
	}
	rendered := dsv41.New().RenderConversation(converted.Conversation)
	out := jsonx.NewObject()
	out.Set("prompt", rendered.Prompt)
	out.Set("segments", segmentValues(segmentPrompt(rendered.Prompt)))
	writeJSON(writer, http.StatusOK, out)
}

func handleDecode(writer http.ResponseWriter, httpRequest *http.Request) {
	var payload decodeRequest
	if err := json.NewDecoder(httpRequest.Body).Decode(&payload); err != nil {
		writeError(writer, requestErrorf("invalid request body: %v", err))
		return
	}
	value, err := decodeOutput(payload)
	if err != nil {
		writeError(writer, err)
		return
	}
	out := jsonx.NewObject()
	out.Set("response", value)
	out.Set("segments", segmentValues(segmentPrompt(payload.Output)))
	writeJSON(writer, http.StatusOK, out)
}

func segmentValues(segments []segment) jsonx.Array {
	values := make(jsonx.Array, 0, len(segments))
	for _, item := range segments {
		values = append(values, item.value())
	}
	return values
}

// decodeOutput converts complete assistant output into a protocol response.
func decodeOutput(payload decodeRequest) (jsonx.Value, error) {
	converted, err := convertBody(payload.Format, payload.Body)
	if err != nil {
		return nil, err
	}
	// This endpoint always accumulates a complete response, including usage,
	// even when the input request originally selected streaming transport.
	converted.Stream = false
	output := payload.Output
	finishReason := inferenceFinishReason(payload.FinishReason)
	if index := strings.Index(output, v4.EOSToken); index >= 0 {
		output = output[:index]
		finishReason = stream.InferenceFinishStop
	}
	// The demo accepts complete assistant output. Consume its leading frame
	// markers here; the stream parser normally starts after a prompt prefill.
	output = strings.TrimPrefix(output, v4.AssistantSPToken)
	thinking := false
	if rest, ok := strings.CutPrefix(output, v4.ThinkingStartToken); ok {
		thinking, output = true, rest
	} else {
		output = strings.TrimPrefix(output, v4.ThinkingEndToken)
	}
	converted.Conversation.ThinkingMode = thinking
	converted.ParsingOptions.ReasoningInitialStage = nil
	if thinking {
		stage := stream.ReasoningStageStart
		converted.ParsingOptions.ReasoningInitialStage = &stage
	}
	converted.ParsingOptions.ToolCallInitialStage = false
	model := demoModel
	if converted.Model != nil {
		model = *converted.Model
	}
	const id = "demo-id"
	if converted.NewChunkGenerator == nil {
		return nil, request.Internal("request has no chunk generator")
	}
	impl, err := adapterFor(payload.Format)
	if err != nil {
		return nil, err
	}
	generator := converted.NewChunkGenerator(id, model)
	processor := stream.NewProcessor(generator, converted.ParsingOptions)
	accumulator := impl.newResponse(id, model, uint64(time.Now().Unix()))
	inference := []stream.InferenceChunk{
		// Raw text alone does not supply tokenizer or backend usage data.
		stream.NewTextChunk(output, 0),
		stream.NewFinishChunk(finishReason),
	}
	for _, chunk := range inference {
		events, err := processor.Push(chunk)
		if err != nil {
			return nil, request.Internal(err.Error())
		}
		for _, event := range events {
			accumulator.Append(event)
		}
	}
	for _, event := range processor.Finish() {
		accumulator.Append(event)
	}
	text, err := jsonx.MarshalCompactString(accumulator)
	if err != nil {
		return nil, request.Internal(err.Error())
	}
	value, err := jsonx.ParseString(text)
	if err != nil {
		return nil, request.Internal(err.Error())
	}
	return value, nil
}

func inferenceFinishReason(reason string) stream.InferenceFinishReason {
	switch reason {
	case "length":
		return stream.InferenceFinishLength
	case "content_filter":
		return stream.InferenceFinishContentFilter
	default:
		return stream.InferenceFinishStop
	}
}

func requestErrorf(format string, args ...any) error {
	return request.BadRequestf(format, args...)
}

func writeJSON(writer http.ResponseWriter, status int, value jsonx.Value) {
	data, err := jsonx.Marshal(value)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(data)
}

func writeError(writer http.ResponseWriter, err error) {
	conversion, ok := err.(*request.ConversionError)
	if !ok {
		conversion = request.Internal(err.Error())
	}
	body := response.ConversionResponse(conversion)
	data, marshalErr := jsonx.MarshalCompact(body)
	if marshalErr != nil {
		http.Error(writer, marshalErr.Error(), http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(conversion.StatusCode())
	_, _ = writer.Write(data)
}
