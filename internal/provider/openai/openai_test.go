package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/provider"
)

func TestStreamHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"hel"}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"lo"}}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ch := streamForTest(t, srv.URL, provider.Request{Model: "m"})
	chunks := collect(t, ch)
	if got := chunksText(chunks); got != "hello" {
		t.Fatalf("unexpected deltas: %q", got)
	}
	if !chunks[len(chunks)-1].Done {
		t.Fatalf("last chunk should be Done: %+v", chunks)
	}
}

func TestRequestShape(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing authorization header")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ch := streamForTest(t, srv.URL, provider.Request{
		Model:  "model-a",
		System: "system prompt",
		Config: provider.ModelConfig{
			OpenAI: map[string]any{
				"temperature":           0.2,
				"max_completion_tokens": 123,
				"reasoning_effort":      "low",
			},
		},
		Messages: []provider.Message{
			{Role: provider.RoleUser, Text: "hi"},
			{Role: provider.RoleAssistant, Text: "hello"},
		},
	})
	_ = collect(t, ch)

	if got.Model != "model-a" || !got.Stream {
		t.Fatalf("unexpected request head: %+v", got)
	}
	if got.Temperature == nil || *got.Temperature != 0.2 || got.MaxCompletionTokens == nil || *got.MaxCompletionTokens != 123 || got.ReasoningEffort != "low" {
		t.Fatalf("model config not applied: %+v", got)
	}
	want := []chatMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	if len(got.Messages) != len(want) {
		t.Fatalf("unexpected messages: %+v", got.Messages)
	}
	for i := range want {
		if got.Messages[i] != want[i] {
			t.Fatalf("message %d: got %+v want %+v", i, got.Messages[i], want[i])
		}
	}
}

func TestNon2xxEmitsErrWithSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad token test-key", http.StatusUnauthorized)
	}))
	defer srv.Close()

	ch := streamForTest(t, srv.URL, provider.Request{Model: "m"})
	chunks := collect(t, ch)
	if len(chunks) != 1 || chunks[0].Err == nil {
		t.Fatalf("want one Err chunk, got %+v", chunks)
	}
	msg := chunks[0].Err.Error()
	if !strings.Contains(msg, "status 401") || strings.Contains(msg, "test-key") {
		t.Fatalf("unexpected error: %s", msg)
	}
}

func TestStreamEndsWithoutDoneEmitsErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"partial"}}]}`+"\n\n")
	}))
	defer srv.Close()

	ch := streamForTest(t, srv.URL, provider.Request{Model: "m"})
	chunks := collect(t, ch)
	if chunksText(chunks) != "partial" || chunks[len(chunks)-1].Err == nil {
		t.Fatalf("want partial delta then Err, got %+v", chunks)
	}
}

func TestContextCancelTerminatesPromptly(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := New(Config{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()})
	ch, err := client.Stream(ctx, provider.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()

	select {
	case chunk, ok := <-ch:
		if !ok || !errors.Is(chunk.Err, context.Canceled) {
			t.Fatalf("want context.Canceled Err chunk, got %+v ok=%v", chunk, ok)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancel did not terminate promptly")
	}
}

func streamForTest(t *testing.T, baseURL string, req provider.Request) <-chan provider.Chunk {
	t.Helper()
	client := New(Config{BaseURL: baseURL, APIKey: "test-key"})
	ch, err := client.Stream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

func collect(t *testing.T, ch <-chan provider.Chunk) []provider.Chunk {
	t.Helper()
	var chunks []provider.Chunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}
	return chunks
}

func chunksText(chunks []provider.Chunk) string {
	var b strings.Builder
	for _, chunk := range chunks {
		b.WriteString(chunk.Delta)
	}
	return b.String()
}
