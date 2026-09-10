package golden

import (
	"github.com/dandandujie/dsr-go/core"
	"github.com/dandandujie/dsr-go/core/jsonx"
	"github.com/dandandujie/dsr-go/recipe/request"
	"github.com/dandandujie/dsr-go/recipe/stream"
)

// dumpConversion mirrors refgen's dump_conversion.
func dumpConversion(converted *request.ConversationRequest) *jsonx.Object {
	options := jsonx.NewObject()
	options.Set("max_tokens", optionalUint32(converted.InferenceOptions.MaxTokens))
	options.Set("temperature", optionalFloat32(converted.InferenceOptions.Temperature))
	options.Set("top_p", optionalFloat32(converted.InferenceOptions.TopP))
	options.Set("thinking_budget_tokens", optionalUint64(converted.InferenceOptions.ThinkingBudgetTokens))
	options.Set("disable_parallel_tool_use", optionalBool(converted.InferenceOptions.DisableParallelToolUse))

	parsing := jsonx.NewObject()
	parsing.Set("parse_tool_calls", converted.ParsingOptions.ParseToolCalls)
	parsing.Set("tool_call_initial_stage", converted.ParsingOptions.ToolCallInitialStage)
	parsing.Set("parse_json_output", converted.ParsingOptions.ParseJSONOutput)
	parsing.Set("reasoning_initial_stage", reasoningStage(converted.ParsingOptions.ReasoningInitialStage))
	stopSequences := make(jsonx.Array, 0, len(converted.ParsingOptions.StopSequences))
	for _, sequence := range converted.ParsingOptions.StopSequences {
		stopSequences = append(stopSequences, sequence)
	}
	parsing.Set("stop_sequences", stopSequences)

	out := jsonx.NewObject()
	out.Set("conversation", dumpConversation(converted.Conversation))
	out.Set("inference_options", options)
	out.Set("parsing_options", parsing)
	out.Set("model", optionalString(converted.Model))
	out.Set("stream", converted.Stream)
	return out
}

func dumpConversation(conversation *core.Conversation) *jsonx.Object {
	messages := make(jsonx.Array, 0, len(conversation.Messages))
	for _, message := range conversation.Messages {
		messages = append(messages, dumpMessage(message))
	}
	tools := make(jsonx.Array, 0, len(conversation.Tools))
	for _, tool := range conversation.Tools {
		item := jsonx.NewObject()
		item.Set("name", tool.Name)
		item.Set("description", optionalString(tool.Description))
		item.Set("parameters", tool.Parameters)
		item.Set("strict", optionalBool(tool.Strict))
		tools = append(tools, item)
	}
	out := jsonx.NewObject()
	out.Set("messages", messages)
	out.Set("thinking_mode", conversation.ThinkingMode)
	out.Set("tools", tools)
	out.Set("tool_choice", conversation.ToolChoice.String())
	if conversation.ReasoningEffort == nil {
		out.Set("reasoning_effort", nil)
	} else {
		out.Set("reasoning_effort", conversation.ReasoningEffort.String())
	}
	if conversation.ResponseFormat == core.ResponseFormatJSONObject {
		out.Set("response_format", "json_object")
	} else {
		out.Set("response_format", "text")
	}
	return out
}

func dumpMessage(message core.InputMessage) *jsonx.Object {
	out := jsonx.NewObject()
	switch message.Kind {
	case core.MessageSystem:
		out.Set("role", "system")
		out.Set("content", message.Content)
	case core.MessageUser:
		out.Set("role", "user")
		out.Set("content", message.Content)
		out.Set("images", dumpImages(message.ImageSources))
	case core.MessageAssistant:
		out.Set("role", "assistant")
		out.Set("content", message.Content)
		if message.ReasoningContent == "" {
			out.Set("reasoning_content", nil)
		} else {
			out.Set("reasoning_content", message.ReasoningContent)
		}
		if message.ToolCalls == nil {
			out.Set("tool_calls", nil)
		} else {
			calls := make(jsonx.Array, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				item := jsonx.NewObject()
				item.Set("id", call.ID)
				item.Set("name", call.Name)
				item.Set("arguments", call.Arguments)
				calls = append(calls, item)
			}
			out.Set("tool_calls", calls)
		}
	case core.MessageTool:
		out.Set("role", "tool")
		out.Set("content", message.Content)
		out.Set("tool_call_id", message.ToolCallID)
		out.Set("images", dumpImages(message.ImageSources))
	default:
		out.Set("role", "latest_reminder")
		out.Set("content", message.Content)
	}
	return out
}

func dumpImages(sources []core.ImageSource) jsonx.Array {
	out := make(jsonx.Array, 0, len(sources))
	for _, source := range sources {
		item := jsonx.NewObject()
		switch source.Kind {
		case core.ImageSourceDataURL:
			item.Set("kind", "data_url")
			item.Set("data_url", source.DataURL)
		case core.ImageSourceURL:
			item.Set("kind", "url")
			item.Set("url", source.URL)
		default:
			item.Set("kind", "bytes")
			item.Set("len", int64(len(source.Data)))
		}
		item.Set("detail", source.Detail.String())
		out = append(out, item)
	}
	return out
}

func reasoningStage(stage *stream.ReasoningStage) jsonx.Value {
	if stage == nil {
		return nil
	}
	switch *stage {
	case stream.ReasoningStageStart:
		return "start"
	case stream.ReasoningStageReasoning:
		return "reasoning"
	default:
		return "content"
	}
}

func optionalString(value *string) jsonx.Value {
	if value == nil {
		return nil
	}
	return *value
}

func optionalBool(value *bool) jsonx.Value {
	if value == nil {
		return nil
	}
	return *value
}

func optionalUint32(value *uint32) jsonx.Value {
	if value == nil {
		return nil
	}
	return int64(*value)
}

func optionalUint64(value *uint64) jsonx.Value {
	if value == nil {
		return nil
	}
	return int64(*value)
}

func optionalFloat32(value *float32) jsonx.Value {
	if value == nil {
		return nil
	}
	return float64(*value)
}
