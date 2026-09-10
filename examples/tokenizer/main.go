// Command tokenizer encodes a rendered prompt into DeepSeek V4.1 token IDs and
// decodes them again.
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/dandandujie/dsr-go/encoding/tokenizer"
	"github.com/dandandujie/dsr-go/encoding/v4/dsv41"
	"github.com/dandandujie/dsr-go/recipe/openai/chatcompletion"
	"github.com/dandandujie/dsr-go/recipe/request"
)

func main() {
	var typed chatcompletion.ChatCompletionRequest
	if err := json.Unmarshal([]byte(`{"model": "deepseek-flash", "messages": [{"role": "user", "content": "Hello"}]}`), &typed); err != nil {
		log.Fatal(err)
	}
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

	// A backend that reports token IDs can be decoded with the same tokenizer.
	text, err := loaded.DecodeIDs(ids, false)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(text)
}
