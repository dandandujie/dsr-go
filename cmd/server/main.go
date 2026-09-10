// Command server is the Go port of the server-rs example: an HTTP API with
// mock inference for Chat Completions, Responses, and Messages.
//
// Requests use DeepSeek V4.1 prompt rendering, resolve images with the default
// image resolver, and use the server's mock inference backend. The server
// supports SSE and complete JSON responses.
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/encoding/v4/dsv41"
	"github.com/dandandujie/dsr-go/image"
	"github.com/dandandujie/dsr-go/recipe/anthropic/messages"
	"github.com/dandandujie/dsr-go/recipe/openai/chatcompletion"
	"github.com/dandandujie/dsr-go/recipe/openai/responses"
	"github.com/dandandujie/dsr-go/recipe/request"
	"github.com/dandandujie/dsr-go/recipe/response"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

const (
	// mockID is the response ID reported by the mock backend.
	mockID = "mock-id"
	// mockModel is the model name reported by the mock backend.
	mockModel = "deepseek-flash"
	// defaultAddr is the server's default listen address.
	defaultAddr = "127.0.0.1:7777"
)

// protocolAdapter binds one protocol package to the server.
type protocolAdapter struct {
	// newResponse builds an empty accumulator for the protocol.
	newResponse func(id, model string, created uint64) response.ProtocolResponse
	// configure applies protocol-specific generator settings.
	configure func(generator stream.Generator) stream.Generator
}

// preparedResponse is a validated request's prompt, resolved images, and
// protocol response events.
type preparedResponse struct {
	// Prompt is the DeepSeek V4.1 prompt, unused by the mock backend.
	Prompt string
	// MultiModalData holds the images resolved from the request.
	MultiModalData *core.MultiModalData
	// Frames holds complete SSE frames, including the terminal sentinel.
	Frames []string
}

func main() {
	addr := os.Getenv("SERVER_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", handleChatCompletions)
	mux.HandleFunc("POST /v1/responses", handleResponses)
	mux.HandleFunc("POST /v1/messages", handleMessages)
	log.Printf("deepseek-recipe example server: http://%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func handleChatCompletions(writer http.ResponseWriter, httpRequest *http.Request) {
	body, err := readBody(httpRequest)
	if err != nil {
		writeError(writer, request.BadRequestf("invalid JSON request: %v", err))
		return
	}
	var typed chatcompletion.ChatCompletionRequest
	if err := json.Unmarshal(body, &typed); err != nil {
		writeError(writer, request.BadRequestf("invalid JSON request: %v", err))
		return
	}
	includeUsage := typed.IncludeUsage()
	converted, err := typed.Convert(request.NewConversionOptions())
	if err != nil {
		writeError(writer, err)
		return
	}
	adapter := protocolAdapter{
		newResponse: func(id, model string, created uint64) response.ProtocolResponse {
			return chatcompletion.NewChatCompletionResponse(id, model, created)
		},
		configure: func(generator stream.Generator) stream.Generator {
			if typed, ok := generator.(*chatcompletion.ChatCompletionChunkGenerator); ok {
				return typed.WithIncludeUsage(includeUsage)
			}
			return generator
		},
	}
	serve(writer, converted, adapter)
}

func handleResponses(writer http.ResponseWriter, httpRequest *http.Request) {
	body, err := readBody(httpRequest)
	if err != nil {
		writeError(writer, request.BadRequestf("invalid JSON request: %v", err))
		return
	}
	var typed responses.ResponsesRequest
	if err := json.Unmarshal(body, &typed); err != nil {
		writeError(writer, request.BadRequestf("invalid JSON request: %v", err))
		return
	}
	customToolNames := typed.CustomToolNames()
	converted, err := typed.Convert(request.NewConversionOptions())
	if err != nil {
		writeError(writer, err)
		return
	}
	adapter := protocolAdapter{
		newResponse: func(id, model string, created uint64) response.ProtocolResponse {
			return responses.NewResponsesResponse(id, model, created)
		},
		configure: func(generator stream.Generator) stream.Generator {
			if typed, ok := generator.(*responses.ResponsesChunkGenerator); ok {
				return typed.WithCustomToolNames(customToolNames)
			}
			return generator
		},
	}
	serve(writer, converted, adapter)
}

func handleMessages(writer http.ResponseWriter, httpRequest *http.Request) {
	body, err := readBody(httpRequest)
	if err != nil {
		writeError(writer, request.BadRequestf("invalid JSON request: %v", err))
		return
	}
	var typed messages.MessagesRequest
	if err := json.Unmarshal(body, &typed); err != nil {
		writeError(writer, request.BadRequestf("invalid JSON request: %v", err))
		return
	}
	converted, err := typed.Convert(request.NewConversionOptions())
	if err != nil {
		writeError(writer, err)
		return
	}
	adapter := protocolAdapter{
		newResponse: func(id, model string, created uint64) response.ProtocolResponse {
			return messages.NewMessagesResponse(id, model, created)
		},
	}
	serve(writer, converted, adapter)
}

// serve returns a complete response or an SSE stream, matching the request's
// transport setting.
func serve(writer http.ResponseWriter, converted *request.ConversationRequest, adapter protocolAdapter) {
	if !converted.Stream {
		complete, err := completeResponse(converted, adapter)
		if err != nil {
			writeError(writer, err)
			return
		}
		writeValue(writer, http.StatusOK, complete)
		return
	}
	prepared, err := prepareResponse(converted, adapter)
	if err != nil {
		writeError(writer, err)
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	flusher, _ := writer.(http.Flusher)
	for _, frame := range prepared.Frames {
		if _, err := io.WriteString(writer, frame); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// prepareResponse renders the prompt, resolves images, and converts the mock
// backend output into complete SSE frames.
func prepareResponse(converted *request.ConversationRequest, adapter protocolAdapter) (*preparedResponse, error) {
	rendered := dsv41.New().RenderConversation(converted.Conversation)
	resolver := image.NewImageResolver(image.NewHTTPImageFetcher(), image.V41ImagePreprocessor{})
	quota := image.NewImageQuota()
	multiModal, err := resolver.Resolve(rendered.ImageSources, quota)
	if err != nil {
		return nil, imageError(err)
	}
	_, frames, err := runMockInference(converted, adapter)
	if err != nil {
		return nil, err
	}
	return &preparedResponse{
		Prompt:         rendered.Prompt,
		MultiModalData: multiModal,
		Frames:         frames,
	}, nil
}

// completeResponse aggregates the mock backend output into one response.
func completeResponse(converted *request.ConversationRequest, adapter protocolAdapter) (jsonx.Value, error) {
	accumulator, _, err := runMockInferenceWith(converted, adapter, false)
	if err != nil {
		return nil, err
	}
	text, err := jsonx.MarshalCompactString(accumulator)
	if err != nil {
		return nil, request.Internal(err.Error())
	}
	return jsonx.ParseString(text)
}

// runMockInference drives the mock backend and returns the accumulator and the
// SSE frames it produced.
func runMockInference(converted *request.ConversationRequest, adapter protocolAdapter) (response.ProtocolResponse, []string, error) {
	return runMockInferenceWith(converted, adapter, true)
}

func runMockInferenceWith(converted *request.ConversationRequest, adapter protocolAdapter, frames bool) (response.ProtocolResponse, []string, error) {
	if converted.NewChunkGenerator == nil {
		return nil, nil, request.Internal("request has no chunk generator")
	}
	generator := converted.NewChunkGenerator(mockID, mockModel)
	if adapter.configure != nil {
		generator = adapter.configure(generator)
	}
	processor := stream.NewProcessor(generator, converted.ParsingOptions)
	accumulator := adapter.newResponse(mockID, mockModel, uint64(time.Now().Unix()))
	var output []string
	emit := func(events []stream.Event) error {
		for _, event := range events {
			accumulator.Append(event)
			if !frames {
				continue
			}
			data, err := jsonx.MarshalCompact(event)
			if err != nil {
				return err
			}
			if name := event.EventName(); name != "" {
				output = append(output, "event: "+name+"\ndata: "+string(data)+"\n\n")
			} else {
				output = append(output, "data: "+string(data)+"\n\n")
			}
		}
		return nil
	}
	for _, chunk := range mockInference() {
		events, err := processor.Push(chunk)
		if err != nil {
			return nil, nil, request.Internal(err.Error())
		}
		if err := emit(events); err != nil {
			return nil, nil, request.Internal(err.Error())
		}
	}
	if err := emit(processor.Finish()); err != nil {
		return nil, nil, request.Internal(err.Error())
	}
	if frames {
		if done, ok := accumulator.DoneMessage(); ok {
			output = append(output, "data: "+done+"\n\n")
		}
	}
	return accumulator, output, nil
}

// mockInference is the fixed backend output used by the example server.
func mockInference() []stream.InferenceChunk {
	return []stream.InferenceChunk{
		stream.NewReadyChunk(nil, stream.PromptUsage{}),
		stream.NewTextChunk("Hello ", 1),
		stream.NewTextChunk("world!", 1),
		stream.NewFinishChunk(stream.InferenceFinishStop),
	}
}

// imageError maps an image failure to a conversion error. Invalid input is a
// client error; client construction and token-budget failures are internal.
func imageError(err error) error {
	var imageErr *image.ImageError
	if ok := asImageError(err, &imageErr); ok {
		if imageErr.Kind == image.KindClient || imageErr.Kind == image.KindTokenBudget {
			return request.Internal(imageErr.Error())
		}
	}
	return request.BadRequest(err.Error())
}

func asImageError(err error, target **image.ImageError) bool {
	for err != nil {
		if typed, ok := err.(*image.ImageError); ok {
			*target = typed
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

func readBody(request *http.Request) ([]byte, error) {
	defer func() { _ = request.Body.Close() }()
	return io.ReadAll(request.Body)
}

func writeValue(writer http.ResponseWriter, status int, value jsonx.Value) {
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
