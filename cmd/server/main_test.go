package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func post(t *testing.T, handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func TestChatCompletionsCompleteResponse(t *testing.T) {
	body := `{"model": "deepseek-flash", "messages": [{"role": "user", "content": "Hi"}], "thinking": {"type": "disabled"}}`
	recorder := post(t, handleChatCompletions, body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID != mockID || payload.Object != "chat.completion" {
		t.Errorf("id/object = %q/%q", payload.ID, payload.Object)
	}
	if got := payload.Choices[0].Message.Content; got != "Hello world!" {
		t.Errorf("content = %q", got)
	}
	if got := payload.Choices[0].FinishReason; got != "stop" {
		t.Errorf("finish_reason = %q", got)
	}
	if payload.Usage.CompletionTokens != 2 {
		t.Errorf("completion_tokens = %d", payload.Usage.CompletionTokens)
	}
}

func TestChatCompletionsStreaming(t *testing.T) {
	body := `{"model": "deepseek-flash", "messages": [{"role": "user", "content": "Hi"}], "thinking": {"type": "disabled"}, "stream": true}`
	recorder := post(t, handleChatCompletions, body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Errorf("content type = %q", contentType)
	}
	// The mock backend sends two text chunks, so the stream is: start, text,
	// text, finish usage, and the [DONE] sentinel.
	frames := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n\n")
	if len(frames) != 5 {
		t.Fatalf("frames = %d: %q", len(frames), recorder.Body.String())
	}
	for _, frame := range frames[:4] {
		if !strings.HasPrefix(frame, "data: {") {
			t.Errorf("frame %q is not an unnamed data frame", frame)
		}
	}
	if frames[4] != "data: [DONE]" {
		t.Errorf("last frame = %q", frames[4])
	}
	var chunk struct {
		Object  string `json:"object"`
		Choices []struct {
			Delta struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(frames[0], "data: ")), &chunk); err != nil {
		t.Fatal(err)
	}
	if chunk.Object != "chat.completion.chunk" || chunk.Choices[0].Delta.Role != "assistant" {
		t.Errorf("first chunk = %+v", chunk)
	}
}

func TestChatCompletionsRejectsInvalidBody(t *testing.T) {
	recorder := post(t, handleChatCompletions, `{"model": "m", "messages": []}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "invalid_request_error") {
		t.Errorf("body = %s", recorder.Body.String())
	}
}
