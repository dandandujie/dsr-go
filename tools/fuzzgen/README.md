# Randomized case generator

`fuzzgen` produces the randomized half of the differential-test corpus. It is a
deterministic generator: the same `-seed` and `-n` always produce the same
cases.

```sh
go run ./tools/fuzzgen -n 2000 -seed 20260910 -o testdata/corpus_fuzz.jsonl

# Ad-hoc campaigns should use -prefix so case names stay unique.
```

The committed `testdata/corpus_fuzz.jsonl` (2000 cases) and its reference
results `testdata/goldens/rust_fuzz.jsonl` are produced by exactly that
command. To run a larger campaign, see
[`../refgen/README.md`](../refgen/README.md).

## What it generates

| Operation | Share | Purpose |
| --- | --- | --- |
| `convert` | 50% | Requests for all three protocols: messages, tool calls, tools and tool choices, images, thinking and effort settings, sampling parameters, stop sequences, response formats, plus invalid inputs that must produce the same errors. |
| `render` | 10% | V4.1 and V4 prompts for those requests. |
| `encode` | 10% | Prompts encoded into token IDs with the bundled tokenizers. |
| `stream` | 20% | Mock backend output (reasoning, answers, tool-call markup, JSON output, stop sequences) split into random rune-aligned chunks, including partial markup, `token_text` chunks that exercise the tokenizer decoder, and missing ready/finish chunks. |
| `tokenize` / `detokenize` | 7.5% | Random text (CJK, emoji, whitespace, control characters, prompt markers) and random token IDs. |
| `image` | 3% | Image sources: base64 and percent-encoded data URLs, unsupported media types, HTTP URLs (including transient and permanent fetch failures), raw bytes, and empty images. |

Timestamps are normalised by the comparison harness, so cases stay reproducible
across runs.
