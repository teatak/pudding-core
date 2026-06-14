package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
)

func TestResponsesStreamHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `event: response.output_text.delta`+"\n")
		fmt.Fprint(w, `data: {"type":"response.output_text.delta","delta":"hel"}`+"\n\n")
		fmt.Fprint(w, `event: response.output_text.delta`+"\n")
		fmt.Fprint(w, `data: {"type":"response.output_text.delta","delta":"lo"}`+"\n\n")
		fmt.Fprint(w, `event: response.completed`+"\n")
		fmt.Fprint(w, `data: {"type":"response.completed"}`+"\n\n")
	}))
	defer srv.Close()

	ch := responsesStreamForTest(t, srv.URL, provider.Request{Model: "m"})
	chunks := collect(t, ch)
	if got := chunksText(chunks); got != "hello" {
		t.Fatalf("unexpected deltas: %q", got)
	}
	if !chunks[len(chunks)-1].Done {
		t.Fatalf("last chunk should be Done: %+v", chunks)
	}
}

func TestResponsesRequestShape(t *testing.T) {
	var got responsesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing authorization header")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(w, `data: {"type":"response.completed"}`+"\n\n")
	}))
	defer srv.Close()

	ch := responsesStreamForTest(t, srv.URL, provider.Request{
		Model:  "model-a",
		System: "system prompt",
		Config: provider.ModelConfig{
			OpenAI: map[string]any{
				"temperature":           0.3,
				"max_completion_tokens": 456,
				"reasoning_effort":      "medium",
			},
		},
		Messages: []provider.Message{
			{Role: provider.RoleUser, Text: "hi"},
			{Role: provider.RoleAssistant, Text: "hello"},
		},
	})
	_ = collect(t, ch)

	if got.Model != "model-a" || !got.Stream || got.Store == nil || *got.Store {
		t.Fatalf("unexpected request head: %+v", got)
	}
	if got.Temperature == nil || *got.Temperature != 0.3 || got.MaxOutputTokens == nil || *got.MaxOutputTokens != 456 || got.Reasoning == nil || got.Reasoning.Effort != "medium" {
		t.Fatalf("model config not applied: %+v", got)
	}
	if got.Instructions != "system prompt" {
		t.Fatalf("unexpected instructions: %q", got.Instructions)
	}
	want := []responsesInputMessage{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	if len(got.Input) != len(want) {
		t.Fatalf("unexpected input: %+v", got.Input)
	}
	for i := range want {
		if got.Input[i] != want[i] {
			t.Fatalf("input %d: got %+v want %+v", i, got.Input[i], want[i])
		}
	}
}

func TestResponsesStreamFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"type":"response.output_text.delta","delta":"partial"}`+"\n\n")
		fmt.Fprint(w, `data: {"type":"response.failed","response":{"error":{"message":"boom"}}}`+"\n\n")
	}))
	defer srv.Close()

	chunks := collect(t, responsesStreamForTest(t, srv.URL, provider.Request{Model: "m"}))
	if chunksText(chunks) != "partial" || chunks[len(chunks)-1].Err == nil {
		t.Fatalf("want partial text then error chunk, got %+v", chunks)
	}
}

func responsesStreamForTest(t *testing.T, baseURL string, req provider.Request) <-chan provider.Chunk {
	t.Helper()
	client := NewResponses(Config{BaseURL: baseURL, APIKey: "test-key"})
	ch, err := client.Stream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return ch
}
