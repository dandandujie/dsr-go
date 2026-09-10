// Package anthropic exposes the Anthropic protocol adapter.
//
// It re-exports the request and response types of recipe/anthropic/messages for
// callers that prefer a single import, mirroring the Rust crate's
// deepseek_recipe::anthropic module.
package anthropic

import "github.com/dandandujie/dsr-go/recipe/anthropic/messages"

// Messages request and response types.
type (
	// MessagesRequest is an Anthropic Messages request.
	MessagesRequest = messages.MessagesRequest
	// MessagesResponse is a complete Messages response, also usable as an event
	// accumulator.
	MessagesResponse = messages.MessagesResponse
	// MessagesChunkGenerator converts parsed output into Messages events.
	MessagesChunkGenerator = messages.MessagesChunkGenerator
)
