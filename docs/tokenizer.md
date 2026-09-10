# Use with tokenizer


The bundled tokenizer is a pure-Go implementation of the HuggingFace BPE
pipeline; it loads the same `tokenizer.json` files as the Rust
`tokenizers` crate and produces the same token IDs and decoded text.

```go
loaded, err := tokenizer.Load("static/tokenizers/v41/tokenizer.json")
```

Use the tokenizer that matches your model. This repository bundles copies under
[`static/tokenizers/`](../static/tokenizers/README.md) for the V4 and V4.1
prompt templates; point the loader at `tokenizer.json` in the matching
directory. A tokenizer from the Hugging Face Hub works too, but its special
token spelling must match the encoding's constants, and its image token must
resolve to the same ID.

## Encoding

With a tokenizer attached, an encoding encodes the prompt into model token IDs.
The complete program is in
[`examples/tokenizer`](../examples/tokenizer/main.go); run it with
`go run ./examples/tokenizer`:

```go
converted, err := typed.Convert(request.NewConversionOptions())
if err != nil {
	log.Fatal(err)
}

loaded, err := tokenizer.Load("static/tokenizers/v41/tokenizer.json")
if err != nil {
	log.Fatal(err)
}
encoding := dsv41.New().WithTokenizer(loaded)
ids, err := encoding.Encode(converted.Conversation)
if err != nil {
	log.Fatal(err)
}
fmt.Println(ids)
```

`Tokenizer` implements `encoding.TokenizerEncoder`; any type with an
`EncodeIDs(text string) ([]uint32, error)` method can be attached instead.
Encoding is a two-step operation: `RenderConversation` returns the prompt text
and its image placeholders, and `Encode` tokenizes that prompt with added
special tokens disabled, because the prompt already contains the special token
text.

## Decoding

A backend can return token IDs instead of text. Attach the same tokenizer to
the stream processor:

```go
loaded, err := tokenizer.Load("static/tokenizers/v41/tokenizer.json")
if err != nil {
	log.Fatal(err)
}
ids, err := loaded.EncodeIDs("Hello there!")
if err != nil {
	log.Fatal(err)
}

generator := converted.NewChunkGenerator("id-1", "deepseek-flash")
processor := stream.NewProcessor(generator, converted.ParsingOptions).WithTokenizer(loaded)
for _, id := range ids {
	events, err := processor.Push(stream.NewTokenChunk(id))
	if err != nil {
		log.Fatal(err)
	}
	printEvents(events)
}
printEvents(processor.Finish())
```

The processor decodes IDs incrementally and buffers them while a multi-token
character is incomplete; IDs that produced text count as completion tokens.
Special tokens stay in the decoded text because they drive output parsing. A
`Token` chunk without an attached tokenizer fails the stream with
`stream.ErrMissingTokenizer`.

`Tokenizer` implements `stream.TokenizerDecoder`; any type with a
`DecodeIDs(ids []uint32, skipSpecialTokens bool) (string, error)` method can be
attached instead.

## Validation

`encoding/tokenizer/testdata` holds a corpus of strings with the token IDs and
decoded text produced by HuggingFace `tokenizers` 0.23.2 for both the V4 and
V4.1 files; `go test ./encoding/tokenizer/...` checks every case.
