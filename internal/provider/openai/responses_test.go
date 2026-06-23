package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestResponsesReadSSEUsage(t *testing.T) {
	out := make(chan provider.Chunk, 3)
	err := readResponsesSSE(context.Background(), strings.NewReader(
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":100,"output_tokens":20,"input_tokens_details":{"cached_tokens":25},"output_tokens_details":{"reasoning_tokens":5}}}}`+"\n\n",
	), out)
	close(out)
	if err != nil {
		t.Fatal(err)
	}
	var usage *provider.UsageInfo
	done := false
	for chunk := range out {
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if chunk.Done {
			done = true
		}
	}
	if !done || usage == nil {
		t.Fatalf("missing usage or done: done=%v usage=%+v", done, usage)
	}
	if usage.InputUncachedTokens != 75 || usage.InputCachedTokens != 25 || usage.OutputContentTokens != 15 || usage.OutputReasoningTokens != 5 {
		t.Fatalf("usage wrong: %+v", usage)
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
			Limits: &provider.ModelLimits{MaxOutputTokens: 456},
			ProviderOptions: &provider.ModelProviderOptions{
				OpenAI: map[string]any{
					"temperature":      0.3,
					"reasoning_effort": "medium",
				},
			},
		},
		Tools: []provider.ToolDef{{
			Name:        "web_fetch",
			Description: "Fetch a URL",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
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
	if len(got.Tools) != 1 || got.Tools[0].Name != "web_fetch" || string(got.Tools[0].Parameters) != `{"type":"object"}` {
		t.Fatalf("tools not applied: %+v", got.Tools)
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

func TestResponsesRequestShapeWithToolHistory(t *testing.T) {
	var got responsesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(w, `data: {"type":"response.completed"}`+"\n\n")
	}))
	defer srv.Close()

	ch := responsesStreamForTest(t, srv.URL, provider.Request{
		Model: "model-a",
		Messages: []provider.Message{
			{Role: provider.RoleAssistant, Parts: []provider.Part{
				{Type: provider.PartToolUse, CallID: "call_1", Name: "builtin_time_get_current", Args: json.RawMessage(`{"timezone":"Asia/Singapore"}`)},
				{Type: provider.PartToolResult, CallID: "call_1", Name: "builtin_time_get_current", Ok: true, Content: `{"iso":"now"}`},
			}},
		},
	})
	_ = collect(t, ch)

	if len(got.Input) != 2 {
		t.Fatalf("unexpected input: %+v", got.Input)
	}
	if got.Input[0].Type != "function_call" || got.Input[0].CallID != "call_1" || got.Input[0].Name != "builtin_time_get_current" || got.Input[0].Arguments != `{"timezone":"Asia/Singapore"}` {
		t.Fatalf("function_call item wrong: %+v", got.Input[0])
	}
	if got.Input[1].Type != "function_call_output" || got.Input[1].CallID != "call_1" || got.Input[1].Output != `{"iso":"now"}` {
		t.Fatalf("function_call_output item wrong: %+v", got.Input[1])
	}
}

func TestResponsesToolCallChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"type":"response.output_item.added","item":{"id":"call_1","type":"function_call","name":"web_fetch"}}`+"\n\n")
		fmt.Fprint(w, `data: {"type":"response.function_call_arguments.delta","item_id":"call_1","delta":"{\"url\""}`+"\n\n")
		fmt.Fprint(w, `data: {"type":"response.function_call_arguments.delta","item_id":"call_1","delta":":\"https://example.com\"}"}`+"\n\n")
		fmt.Fprint(w, `data: {"type":"response.completed"}`+"\n\n")
	}))
	defer srv.Close()

	chunks := collect(t, responsesStreamForTest(t, srv.URL, provider.Request{Model: "m"}))
	if len(chunks) != 4 {
		t.Fatalf("unexpected chunks: %+v", chunks)
	}
	if chunks[0].Tool == nil || chunks[0].Tool.CallID != "call_1" || chunks[0].Tool.Name != "web_fetch" {
		t.Fatalf("tool start wrong: %+v", chunks[0])
	}
	if chunks[1].Tool == nil || chunks[1].Tool.ArgsDelta != `{"url"` {
		t.Fatalf("first args chunk wrong: %+v", chunks[1])
	}
	if !chunks[3].Done || chunks[3].Finish != provider.FinishToolCalls {
		t.Fatalf("finish chunk wrong: %+v", chunks[3])
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
