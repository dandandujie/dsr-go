// Command streaming converts mock backend output into Chat Completions chunks.
//
// The application supplies the backend and the HTTP transport; this example
// feeds the stream processor by hand.
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/openai/chatcompletion"
	"github.com/dandandujie/dsr-go/recipe/request"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

func main() {
	body := []byte(`{
		"model": "deepseek-flash",
		"messages": [{"role": "user", "content": "Hello"}],
		"thinking": {"type": "disabled"}
	}`)

	var typed chatcompletion.ChatCompletionRequest
	if err := json.Unmarshal(body, &typed); err != nil {
		log.Fatal(err)
	}
	converted, err := typed.Convert(request.NewConversionOptions())
	if err != nil {
		log.Fatal(err)
	}

	// The chunk generator uses the converted request's resolved settings.
	generator := converted.NewChunkGenerator("id-1", "deepseek-flash")
	processor := stream.NewProcessor(generator, converted.ParsingOptions)

	// Mock the backend's inference chunks.
	inference := []stream.InferenceChunk{
		stream.NewReadyChunk(pointer("fp-1"), stream.PromptUsage{PromptTokens: 4}),
		stream.NewTextChunk("Hello!", 2),
		stream.NewFinishChunk(stream.InferenceFinishStop),
	}

	emit := func(events []stream.Event) {
		for _, event := range events {
			text, err := jsonx.MarshalCompactString(event)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(text)
		}
	}
	for _, chunk := range inference {
		events, err := processor.Push(chunk)
		if err != nil {
			log.Fatal(err)
		}
		emit(events)
	}
	emit(processor.Finish())
}

func pointer[T any](value T) *T { return &value }
