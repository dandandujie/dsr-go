# Reference generator

`refgen` is the differential-test oracle of this repository. It is a small Rust
binary built on top of the original
[`deepseek-recipe`](https://github.com/deepseek-ai/deepseek-recipe) crates. It reads
golden cases (one JSON object per line) from stdin and writes reference results
to stdout, so the Go rewrite can be compared byte for byte against the Rust
implementation it was ported from.

## Layout

| File | Purpose |
| --- | --- |
| `src/main.rs` | The generator: implements the `convert`, `render`, `encode`, `stream`, `complete`, `tokenize`, `detokenize`, `stringify` and `numbers` operations. |
| `Cargo.toml` | Crate manifest; all dependencies come from the Rust workspace. |
| `../testdata/corpus.jsonl` | The case corpus (inputs), owned by this repository. |
| `../testdata/goldens/rust.jsonl` | The reference results produced by `refgen`. |

## Regenerating the goldens

```sh
# 1. Clone the reference implementation next to this repository.
git clone --depth 1 https://github.com/deepseek-ai/deepseek-recipe /tmp/deepseek-recipe

# 2. Copy this crate into its workspace and register it as a member.
cp -R tools/refgen /tmp/deepseek-recipe/tools/refgen
#    add "tools/refgen" to the members list of /tmp/deepseek-recipe/Cargo.toml

# 3. Build it. CARGO_HOME only needs to point somewhere writable.
cd /tmp/deepseek-recipe
CARGO_HOME=/tmp/dsr-cargo cargo build -p refgen

# 4. Replay the corpus.
cd -   # back to this repository
/tmp/deepseek-recipe/target/debug/refgen < testdata/corpus.jsonl > testdata/goldens/rust.jsonl

# 5. Verify the Go port against the refreshed goldens.
GOCACHE=/tmp/dsr-go-cache GOMODCACHE=/tmp/dsr-go-mod GOPATH=/tmp/dsr-go-path   go test ./golden/...
```

The Go test compares the compact JSON of every case after normalising volatile
timestamps (`created` and `created_at` are replaced by `0`), so field order and
number formatting are checked as strictly as values.

## Adding a case

Append a JSON object to `testdata/corpus.jsonl`, regenerate the goldens, and run
the Go tests. A case has this shape:

```json
{"name": "chat_simple_render", "op": "render", "protocol": "chat_completions",
 "body": {"model": "deepseek-flash", "messages": [{"role": "user", "content": "Hello"}]}}
```

Supported operations: `convert` (conversation and options dump), `render`
(V4.1 prompt plus its image sources), `encode` (prompt token IDs), `stream`
(protocol events for a list of mock inference chunks), `complete` (accumulated
protocol response), `tokenize` / `detokenize` (tokenizer round trips),
`stringify` (Python-style JSON rendering) and `numbers` (float formatting).
