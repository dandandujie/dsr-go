# Development

## Requirements

- Go 1.26 or newer. The module has no cgo dependencies.
- Optional: Rust (stable) and a checkout of
  [deepseek-recipe](https://github.com/deepseek-ai/deepseek-recipe) to rebuild
  the differential-test goldens.

## Layout

```
core/                     conversation, message, image, and tool types
core/jsonx/               order-preserving JSON model (serde_json compatible)
encoding/                 PromptEncoding, RenderedPrompt
encoding/tokenizer/       pure-Go HuggingFace BPE tokenizer
encoding/v4/              shared V4/V4.1 rendering
encoding/v4/dsv4/         DeepSeek V4 encoding
encoding/v4/dsv41/        DeepSeek V4.1 encoding
image/                    image fetching and V4.1 preprocessing
recipe/request/           ConversationRequest, ConversionOptions, errors
recipe/response/          ProtocolResponse and error bodies
recipe/stream/            state machine, stream processor, inference chunks
recipe/internal/protocol/ image and validation helpers shared by adapters
recipe/openai/            Chat Completions and Responses adapters
recipe/anthropic/         Messages adapter
cmd/server/               example HTTP API with mock inference
cmd/demo/                 encoding/decoding demo page
examples/                 small runnable programs used by the README
static/tokenizers/        bundled V4 and V4.1 tokenizer files
testdata/                 differential-test corpus and reference results
golden/                   differential tests
tools/refgen/             Rust reference generator
```

## Build and test

```sh
go build ./...
go vet ./...
go test ./...
gofmt -l .
```

The demo and the example server can be started directly:

```sh
go run ./cmd/demo      # http://127.0.0.1:7778
go run ./cmd/server    # http://127.0.0.1:7777
```

## Differential testing

The port is validated against the original Rust implementation. The test
corpus lives in `testdata/` and the reference results in `testdata/goldens/`;
`golden/` replays every case through the Go packages and requires byte-identical
output (timestamps are normalised).

| File | Contents |
| --- | --- |
| `testdata/corpus.jsonl` | 148 hand-written cases for all three protocols. |
| `testdata/corpus_fuzz.jsonl` | 2000 randomized cases from `tools/fuzzgen`. |
| `testdata/goldens/rust.jsonl` | Reference results for the hand-written cases. |
| `testdata/goldens/rust_fuzz.jsonl` | Reference results for the randomized cases. |

The reference results are produced by [`tools/refgen`](../tools/refgen), a Rust
binary built on the original crates. See
[`tools/refgen/README.md`](../tools/refgen/README.md) for the regeneration
procedure, including how to add cases and how to run the campaign against a
larger randomized corpus.

`golden/fuzz_test.go` holds Go fuzz targets for the converters and the stream
parser; a short campaign per target covers millions of malformed inputs:

```sh
go test ./golden/ -run FuzzChatCompletionsConvert -fuzz FuzzChatCompletionsConvert -fuzztime 60s
go test ./golden/ -run FuzzResponsesConvert -fuzz FuzzResponsesConvert -fuzztime 60s
go test ./golden/ -run FuzzMessagesConvert -fuzz FuzzMessagesConvert -fuzztime 60s
go test ./golden/ -run FuzzStreamProcessor -fuzz FuzzStreamProcessor -fuzztime 60s
```

## Conventions

- JSON output must match serde's: field order follows the struct declaration
  order, `Option<T>` becomes `null` unless the Rust field has
  `skip_serializing_if`, and `Option<Option<T>>` becomes `*jsonx.Optional[T]`.
- Arbitrary JSON in request schemas is decoded as `json.RawMessage` and parsed
  with `jsonx.Parse`, so object key order survives.
- Structs that are serialized use `jsonx.MarshalCompact` when custom
  `MarshalJSON` is needed; plain `jsonx.Marshal` handles value trees.
- Exported identifiers have doc comments; errors returned to API clients keep the
  wording of the Rust implementation.

## Static assets

`static/tokenizers/v4/tokenizer.json` and
`static/tokenizers/v41/tokenizer.json` are the bundled tokenizer files from
the original repository. Their notices are in
`static/tokenizers/README.md`.