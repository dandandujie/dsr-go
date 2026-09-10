// Command quickstart converts a Chat Completions request into a DeepSeek V4.1
// prompt.
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
