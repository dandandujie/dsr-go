package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// simpleChat is the request body used by the render and decode tests.
const simpleChat = `{"model": "deepseek-flash", "messages": [{"role": "user", "content": "Hello"}]}`

// expectedPrompt is the DeepSeek V4.1 prompt of simpleChat, verified against
// the Rust reference implementation (testdata/goldens/rust.jsonl).
const expectedPrompt = "<｜begin▁of▁sentence｜><｜System｜>Reasoning Effort: 75 (range 1-100, the higher the value, the more thorough the reasoning)\n\n<｜User｜>Hello<｜Assistant｜><think>"

func postJSON(t *testing.T, handler http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func TestRenderHandler(t *testing.T) {
	body := `{"format": "chat_completions", "body": ` + simpleChat + `}`
	recorder := postJSON(t, handleRender, "/api/render", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Prompt   string `json:"prompt"`
		Segments []struct {
			Kind  string `json:"kind"`
			Text  string `json:"text"`
			Name  string `json:"name"`
			Token string `json:"token"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Prompt != expectedPrompt {
		t.Errorf("prompt = %q, want %q", payload.Prompt, expectedPrompt)
	}
	// The prompt starts with BOS, then the system, user, and assistant markers,
	// then the start of the reasoning section.
	var tokens []string
	for _, segment := range payload.Segments {
		if segment.Kind == "token" {
			tokens = append(tokens, segment.Name)
		}
	}
	want := []string{"bos", "system", "user", "assistant", "thinking_start"}
	if strings.Join(tokens, ",") != strings.Join(want, ",") {
		t.Errorf("token segments = %v, want %v", tokens, want)
	}
}

func TestRenderHandlerRejectsInvalidRequest(t *testing.T) {
	body := `{"format": "chat_completions", "body": {"model": "m", "messages": []}}`
	recorder := postJSON(t, handleRender, "/api/render", body)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Message != "Empty input messages" {
		t.Errorf("error message = %q", payload.Error.Message)
	}
}

func TestDecodeHandler(t *testing.T) {
	body := `{"format": "chat_completions", "body": ` + simpleChat + `, "output": "Hello there", "finish_reason": "stop"}`
	recorder := postJSON(t, handleDecode, "/api/decode", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Response struct {
			Object  string `json:"object"`
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		} `json:"response"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Response.Object != "chat.completion" {
		t.Errorf("object = %q", payload.Response.Object)
	}
	if len(payload.Response.Choices) != 1 {
		t.Fatalf("choices = %d", len(payload.Response.Choices))
	}
	choice := payload.Response.Choices[0]
	if choice.Message.Content != "Hello there" {
		t.Errorf("content = %q", choice.Message.Content)
	}
	if choice.FinishReason != "stop" {
		t.Errorf("finish_reason = %q", choice.FinishReason)
	}
}

func TestDecodeHandlerThinkingOutput(t *testing.T) {
	output := "<｜Assistant｜><think>reasoning</think>answer"
	body := `{"format": "chat_completions", "body": ` + simpleChat + `, "output": "` + output + `"}`
	recorder := postJSON(t, handleDecode, "/api/decode", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Response struct {
			Choices []struct {
				Message struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"message"`
			} `json:"choices"`
		} `json:"response"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	message := payload.Response.Choices[0].Message
	if message.ReasoningContent != "reasoning" {
		t.Errorf("reasoning_content = %q", message.ReasoningContent)
	}
	if message.Content != "answer" {
		t.Errorf("content = %q", message.Content)
	}
}

func TestIndexServesTheDemoPage(t *testing.T) {
	recorder := httptest.NewRecorder()
	serveAsset(recorder, "text/html; charset=utf-8", "static/index.html")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "deepseek") {
		t.Error("index.html does not mention deepseek")
	}
}
