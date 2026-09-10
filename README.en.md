<div align="center">
  <img src="static/deepseek-whale.svg" alt="DeepSeek" width="240">
</div>

# dsr-go

[![CI](https://github.com/dandandujie/dsr-go/actions/workflows/ci.yml/badge.svg)](https://github.com/dandandujie/dsr-go/actions/workflows/ci.yml)

**English** | [中文](README.md)

dsr-go is a pure-Go port of
[deepseek-recipe](https://github.com/deepseek-ai/deepseek-recipe). It uniformly
converts API requests in different formats into the Conversation format, encodes
them into prompts for DeepSeek models, and converts model output into responses
in the corresponding format. Use these packages to connect an inference backend
to API services that support multiple formats. Model inference, tool execution,
and HTTP transport must be provided externally.

The port is verified against the original Rust implementation: the same inputs
produce byte-identical prompts, token IDs, streaming events, and responses (see
[Differential testing](#differential-testing)).

## Supported scope

- **Request/response formats:** Conversion of Messages, Chat Completions, and
  Responses requests, streaming responses, and complete responses. Supports
  text, images, thinking, and client tool calls.
- **Prompts:** Encoding of DeepSeek V4 and V4.1 conversations into prompts or
  token IDs, including a pure-Go HuggingFace BPE tokenizer.
- **Generation settings:** Thinking mode, reasoning effort, `temperature`, `top_p`,
  and output token limits.
- **Output parsing:** Thinking, tool calls, JSON object output, and stop
  sequences.
- **Images:** Provided as base64 or external URLs, with DeepSeek V4.1
  preprocessing implemented in pure Go (no OpenCV or cgo).
- **Tool definitions:** Function tools; the Responses API also supports tool
  namespaces and the `apply_patch` custom tool.

## Not yet supported

The same gaps as the Rust implementation:

- Token probabilities (`logprobs` and `top_logprobs`).
- Document content, audio/video input, and file retrieval by `file_id`.
- Server tool execution, such as `web_search`.
- JSON Schema and regex output constraints, or enforcement of tool `strict` settings.
- Multiple completions per Chat Completions request (`n > 1`).
- Responses custom tool definitions other than `apply_patch`.
- Responses conversation storage and context retrieval through
  `previous_response_id`.
- Responses encrypted thinking content (`encrypted_content`).

Differences from the Rust project:

- The PyO3 Python bindings are **not** ported; use the Go packages or the
  example HTTP servers instead.
- Image preprocessing uses pure Go (`image`, `golang.org/x/image`, and a WebP
  encoder) instead of OpenCV, so the build needs no cgo and no native
  dependencies. Resizing uses Catmull-Rom instead of OpenCV's `INTER_CUBIC`, so
  pixels can differ slightly on high-contrast edges; images are written as
  lossless WebP instead of lossy quality 90, which is pixel-exact but larger.
  Target dimensions, token accounting, and padding are identical.
- The tokenizer is implemented in this repository and reads the bundled
  `tokenizer.json` files directly.

## Installing

```sh
go get github.com/dandandujie/dsr-go
```

The module has no cgo dependencies. The only third-party requirements are
`golang.org/x/image` (image decoding) and
`github.com/santhosh-tekuri/jsonschema/v6` (tool parameter schema validation).

## Using dsr-go

To convert a Chat Completions request into a DeepSeek V4.1 prompt, see
[`examples/quickstart`](examples/quickstart/main.go):

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/dandandujie/dsr-go/encoding/v4/dsv41"
	"github.com/dandandujie/dsr-go/recipe/openai/chatcompletion"
	"github.com/dandandujie/dsr-go/recipe/request"
)

func main() {
	body := []byte(`{
		"model": "deepseek-flash",
		"messages": [{"role": "user", "content": "Hello"}]
	}`)

	var typed chatcompletion.ChatCompletionRequest
	if err := json.Unmarshal(body, &typed); err != nil {
		log.Fatal(err)
	}
	converted, err := typed.Convert(request.NewConversionOptions())
	if err != nil {
		log.Fatal(err)
	}
	rendered := dsv41.New().RenderConversation(converted.Conversation)
	fmt.Println(rendered.Prompt)
}
```

Run it from the repository root:

```sh
go run ./examples/quickstart
```

### Streaming responses

`StreamProcessor` converts backend output into Messages, Chat Completions, or
Responses events. See [`examples/streaming`](examples/streaming/main.go) and the
[streaming guide](docs/streaming.md).

### Encoding and decoding with a tokenizer

Attach a tokenizer to an encoding to obtain token IDs, or to a stream processor
to decode token IDs produced by the backend. See
[`examples/tokenizer`](examples/tokenizer/main.go) and the
[tokenizer guide](docs/tokenizer.md).

### Encoding & decoding demo

Run the demo from the repository root:

```sh
go run ./cmd/demo
```

Open <http://127.0.0.1:7778>. Set `DEMO_ADDR` to listen elsewhere.

### Example server

```sh
go run ./cmd/server
```

The server listens on <http://127.0.0.1:7777> and serves `POST /v1/chat/completions`,
`POST /v1/responses`, and `POST /v1/messages` with mock inference. Set
`SERVER_ADDR` to listen elsewhere.

## Packages

| Package | Purpose |
| --- | --- |
| [`core`](core) | Shared conversation, message, image, and tool types. |
| [`core/jsonx`](core/jsonx) | Order-preserving JSON values matching `serde_json`. |
| [`encoding`](encoding) | Prompt rendering and token encoding. |
| [`encoding/tokenizer`](encoding/tokenizer) | Pure-Go HuggingFace BPE tokenizer. |
| [`encoding/v4/dsv4`](encoding/v4/dsv4), [`encoding/v4/dsv41`](encoding/v4/dsv41) | DeepSeek V4 and V4.1 prompt rendering. |
| [`image`](image) | Image fetching and V4.1 preprocessing. |
| [`recipe`](recipe) | Protocol conversion and model output parsing. |
| [`recipe/openai/chatcompletion`](recipe/openai/chatcompletion) | Chat Completions adapter. |
| [`recipe/openai/responses`](recipe/openai/responses) | Responses adapter. |
| [`recipe/anthropic/messages`](recipe/anthropic/messages) | Messages adapter. |
| [`cmd/server`](cmd/server) | HTTP API example with mock inference. |
| [`cmd/demo`](cmd/demo) | Encoding and decoding demo page. |

## Differential testing

The port is validated against the original Rust implementation.

`testdata/corpus.jsonl` holds 148 hand-written cases, and
`testdata/corpus_fuzz.jsonl` holds 2000 randomized cases produced by
[`tools/fuzzgen`](tools/fuzzgen). The matching reference results are in
`testdata/goldens/`, generated by the Rust
[`tools/refgen`](tools/refgen) binary. `golden/` replays every case through the
Go packages and requires a byte-identical result (after normalising
timestamps). The cases cover request conversion (including every validation
error), V4 and V4.1 prompt rendering, prompt token IDs, streaming events with
randomly split chunks, accumulated responses, image resolution, tokenizer
round trips, and Python-style JSON and float formatting for all three
protocols.

```sh
go test ./golden/...
```

`encoding/tokenizer` is additionally checked against the HuggingFace
`tokenizers` 0.23.2 oracle: 336 strings per bundled tokenizer file, with
expected IDs and decoded text. `image` ports the unit tests of the Rust image
crate and exercises the HTTP fetcher with `httptest` servers.

[`golden/fuzz_test.go`](golden/fuzz_test.go) adds Go fuzz targets for the three
request converters and for the stream parser. Each target survives millions of
malformed inputs without a panic or a hang:

```sh
go test ./golden/ -run FuzzStreamProcessor -fuzz FuzzStreamProcessor -fuzztime 60s
```

### Reproducing the campaign

```sh
# 1. Generate more randomized cases (deterministic for a given seed).
go run ./tools/fuzzgen -n 2000 -seed 20260910 -o /tmp/corpus_fuzz.jsonl

# 2. Produce the reference results with the Rust implementation.
#    See tools/refgen/README.md for the build steps.
/path/to/refgen < /tmp/corpus_fuzz.jsonl > /tmp/rust_fuzz.jsonl

# 3. Compare the Go implementation against them.
DSR_EXTRA_CORPUS=/tmp/corpus_fuzz.jsonl DSR_EXTRA_GOLDEN=/tmp/rust_fuzz.jsonl \
  go test ./golden/...
```

## Development

See the [development guide](docs/development.md).

## Credits

dsr-go is an independent Go port of
[deepseek-recipe](https://github.com/deepseek-ai/deepseek-recipe). The
differential-test goldens are generated from that implementation, and the
bundled tokenizer files come from it. See [NOTICE](NOTICE).

## License

Project code and public documentation are licensed under the
[MIT License](LICENSE). Bundled tokenizer notices are in
[static/tokenizers/README.md](static/tokenizers/README.md).

## Friends

- [LINUX DO](https://linux.do)
