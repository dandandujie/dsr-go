package golden

import (
	"encoding/json"
	"testing"

	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/encoding/v4/dsv41"
	"github.com/dandandujie/dsr-go/recipe/anthropic/messages"
	"github.com/dandandujie/dsr-go/recipe/openai/chatcompletion"
	"github.com/dandandujie/dsr-go/recipe/openai/responses"
	"github.com/dandandujie/dsr-go/recipe/request"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// fuzzSeeds returns corpus request bodies used as fuzzing seeds.
func fuzzSeeds(tb testing.TB) [][]byte {
	tb.Helper()
	cases, _ := loadCorpus(tb)
	seeds := make([][]byte, 0, len(cases))
	for _, item := range cases {
		if len(item.Body) > 0 {
			seeds = append(seeds, item.Body)
		}
	}
	seeds = append(seeds,
		[]byte(`{"model": "m", "messages": []}`),
		[]byte(`{"model": "m", "messages": [{"role": "assistant"}]}`),
		[]byte(`{"model": "m", "input": null}`),
		[]byte(`{"model": "m", "max_tokens": 1, "messages": [{"role": "user", "content": null}]}`),
	)
	return seeds
}

// FuzzChatCompletionsConvert checks that malformed Chat Completions requests
// never panic, and that accepted requests render a prompt.
func FuzzChatCompletionsConvert(f *testing.F) {
	for _, seed := range fuzzSeeds(f) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		var typed chatcompletion.ChatCompletionRequest
		if err := json.Unmarshal(body, &typed); err != nil {
			return
		}
		converted, err := typed.Convert(request.NewConversionOptions())
		if err != nil {
			return
		}
		_ = dsv41.New().RenderConversation(converted.Conversation)
	})
}

// FuzzResponsesConvert checks that malformed Responses requests never panic.
func FuzzResponsesConvert(f *testing.F) {
	for _, seed := range fuzzSeeds(f) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		var typed responses.ResponsesRequest
		if err := json.Unmarshal(body, &typed); err != nil {
			return
		}
		converted, err := typed.Convert(request.NewConversionOptions())
		if err != nil {
			return
		}
		_ = dsv41.New().RenderConversation(converted.Conversation)
	})
}

// FuzzMessagesConvert checks that malformed Messages requests never panic.
func FuzzMessagesConvert(f *testing.F) {
	for _, seed := range fuzzSeeds(f) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		var typed messages.MessagesRequest
		if err := json.Unmarshal(body, &typed); err != nil {
			return
		}
		converted, err := typed.Convert(request.NewConversionOptions())
		if err != nil {
			return
		}
		_ = dsv41.New().RenderConversation(converted.Conversation)
	})
}

// FuzzStreamProcessor drives arbitrary model output through the parser and the
// Chat Completions generator: no input may panic, and every event must
// serialize.
func FuzzStreamProcessor(f *testing.F) {
	f.Add("Hello", uint8(0))
	f.Add("reasoning</think>answer", uint8(1))
	f.Add("<｜DSML｜ calls>\n<｜DSML｜ invoke name=\"f\">\n<｜DSML｜ parameter name=\"a\" string=\"true\">1</｜DSML｜ parameter>\n</｜DSML｜ invoke>\n</｜DSML｜ calls>", uint8(2))
	f.Add("```json\n{\"a\": 1}\n```", uint8(3))
	f.Fuzz(func(t *testing.T, output string, flags uint8) {
		body := []byte(`{"model": "m", "messages": [{"role": "user", "content": "q"}]}`)
		var typed chatcompletion.ChatCompletionRequest
		if err := json.Unmarshal(body, &typed); err != nil {
			t.Fatal(err)
		}
		converted, err := typed.Convert(request.NewConversionOptions().WithDefaultThinkingMode(flags%2 == 0))
		if err != nil {
			t.Fatal(err)
		}
		converted.ParsingOptions.ParseJSONOutput = flags%3 == 1
		if flags%4 == 2 {
			stage := stream.ReasoningStageStart
			converted.ParsingOptions.ReasoningInitialStage = &stage
		}
		generator := converted.NewChunkGenerator("id", "m")
		processor := stream.NewProcessor(generator, converted.ParsingOptions)
		// Feed the output both as one chunk and split into three pieces.
		for split := 0; split < 3; split++ {
			processor = stream.NewProcessor(converted.NewChunkGenerator("id", "m"), converted.ParsingOptions)
			for _, piece := range splitText(output, split) {
				events, err := processor.Push(stream.NewTextChunk(piece, 1))
				if err != nil {
					return
				}
				for _, event := range events {
					if _, err := jsonx.MarshalCompact(event); err != nil {
						t.Fatalf("event does not serialize: %v", err)
					}
				}
			}
			for _, event := range processor.Finish() {
				if _, err := jsonx.MarshalCompact(event); err != nil {
					t.Fatalf("event does not serialize: %v", err)
				}
			}
		}
	})
}

// splitText splits text into split+1 roughly equal byte-aligned pieces.
func splitText(text string, split int) []string {
	if split <= 0 || len(text) < split+1 {
		return []string{text}
	}
	pieces := make([]string, 0, split+1)
	size := len(text) / (split + 1)
	for index := 0; index < split; index++ {
		pieces = append(pieces, text[index*size:(index+1)*size])
	}
	pieces = append(pieces, text[split*size:])
	return pieces
}
