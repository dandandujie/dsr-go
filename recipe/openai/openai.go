// Package openai exposes the OpenAI protocol adapters.
//
// It re-exports the request and response types of recipe/openai/chatcompletion
// and recipe/openai/responses for callers that prefer a single import, mirroring
// the Rust crate's deepseek_recipe::openai module.
package openai

import (
	"github.com/dandandujie/dsr-go/recipe/openai/chatcompletion"
	"github.com/dandandujie/dsr-go/recipe/openai/responses"
)

// Chat Completions request and response types.
type (
	// ChatCompletionRequest is a Chat Completions request.
	ChatCompletionRequest = chatcompletion.ChatCompletionRequest
	// ChatCompletionResponse is a complete Chat Completions response, also
	// usable as a chunk accumulator.
	ChatCompletionResponse = chatcompletion.ChatCompletionResponse
	// ChatCompletionChunk is one streamed Chat Completions chunk.
	ChatCompletionChunk = chatcompletion.ChatCompletionChunk
	// ChatCompletionChunkGenerator converts parsed output into chunks.
	ChatCompletionChunkGenerator = chatcompletion.ChatCompletionChunkGenerator
)

// Responses request and response types.
type (
	// ResponsesRequest is a Responses request.
	ResponsesRequest = responses.ResponsesRequest
	// ResponsesResponse is a complete Responses response, also usable as an
	// event accumulator.
	ResponsesResponse = responses.ResponsesResponse
	// ResponsesChunkGenerator converts parsed output into Responses events.
	ResponsesChunkGenerator = responses.ResponsesChunkGenerator
)
