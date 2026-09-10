# Streaming response


`StreamProcessor` converts backend output into Messages, Chat Completions, or
Responses events. The examples below use mock inference and print Chat
Completions chunks. The application supplies the backend and HTTP transport.

Install the packages:

```sh
go get github.com/dandandujie/dsr-go
```

The complete program is in
[`examples/streaming`](../examples/streaming/main.go); run it with
`go run ./examples/streaming`:

```go
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
		stream.NewReadyChunk(nil, stream.PromptUsage{PromptTokens: 4}),
		stream.NewTextChunk("Hello!", 2),
		stream.NewFinishChunk(stream.InferenceFinishStop),
	}
	for _, chunk := range inference {
		events, err := processor.Push(chunk)
		if err != nil {
			log.Fatal(err)
		}
		printEvents(events)
	}
	printEvents(processor.Finish())
}

func printEvents(events []stream.Event) {
	for _, event := range events {
		text, err := jsonx.MarshalCompactString(event)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(text)
	}
}
```

`Push` processes one backend chunk and returns the protocol events it produced;
`Finish` completes the stream when the backend source ends. Events implement
`stream.Event`; `EventName` returns the SSE event name, or an empty string for
an unnamed data event. Serialize them with `jsonx.MarshalCompact` to get the
exact JSON the Rust implementation produces.

## Response events

| Protocol | Event types | Terminal sentinel |
| --- | --- | --- |
| Chat Completions | `ChatCompletionChunk` (unnamed) | `data: [DONE]` |
| Responses | one type per Responses stream event, named by `EventName` | none |
| Messages | `message_start`, `content_block_delta`, ... | none |

`ProtocolResponse.DoneMessage` reports whether a protocol needs a final
transport sentinel; the example server uses it when writing SSE frames.

## Complete responses

Accumulate events into a response with `ProtocolResponse.Append`, then
serialize it:

```go
accumulator := chatcompletion.NewChatCompletionResponse("id-1", "deepseek-flash", created)
for _, chunk := range inference {
	events, err := processor.Push(chunk)
	if err != nil {
		log.Fatal(err)
	}
	for _, event := range events {
		accumulator.Append(event)
	}
}
for _, event := range processor.Finish() {
	accumulator.Append(event)
}
text, err := jsonx.MarshalCompactString(accumulator)
```

`cmd/server` shows the same flow behind an HTTP handler.

## Token IDs

A backend can return token IDs instead of text. Attach the same tokenizer to
the stream processor with `WithTokenizer`; see the
[tokenizer guide](tokenizer.md).
