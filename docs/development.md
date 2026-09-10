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

The port is validated against the original Rust implementation. Every case in
`testdata/corpus.jsonl` is replayed through both implementations and the compact
JSON results must match byte for byte (timestamps are normalised).

The reference results in `testdata/goldens/rust.jsonl` are produced by
[`tools/refgen`](tools/refgen), a Rust binary that depends on the original
crates. See [`tools/refgen/README.md`](tools/refgen/README.md) for the complete
regeneration procedure.

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
