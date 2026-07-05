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

func TestReadSSEReasoningVariants(t *testing.T) {
	out := make(chan provider.Chunk, 8)
	err := readSSE(context.Background(), strings.NewReader(
		`data: {"choices":[{"delta":{"reasoning_content":"deepseek "}}]}`+"\n\n"+
			`data: {"choices":[{"delta":{"reasoning":"ollama "}}]}`+"\n\n"+
			`data: {"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.text","text":"openrouter text "},{"type":"reasoning.summary","summary":"openrouter summary"}]}}]}`+"\n\n"+
			`data: {"choices":[{"delta":{"content":"answer"}}]}`+"\n\n"+
			"data: [DONE]\n\n",
	), out)
	close(out)
	if err != nil {
		t.Fatal(err)
	}
	var thought, text strings.Builder
	for chunk := range out {
		switch chunk.Part {
		case provider.PartThought:
			thought.WriteString(chunk.Delta)
		case provider.PartText:
			text.WriteString(chunk.Delta)
		}
	}
	if got := thought.String(); got != "deepseek ollama openrouter text openrouter summary" {
		t.Fatalf("unexpected reasoning: %q", got)
	}
	if got := text.String(); got != "answer" {
		t.Fatalf("unexpected text: %q", got)
	}
}

func TestReadSSEUsageChunk(t *testing.T) {
	out := make(chan provider.Chunk, 4)
	err := readSSE(context.Background(), strings.NewReader(
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n"+
			`data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":30},"completion_tokens_details":{"reasoning_tokens":7}}}`+"\n\n"+
			"data: [DONE]\n\n",
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
	if usage.InputUncachedTokens != 70 || usage.InputCachedTokens != 30 || usage.OutputContentTokens != 13 || usage.OutputReasoningTokens != 7 {
		t.Fatalf("usage wrong: %+v", usage)
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
			Limits: &provider.ModelLimits{MaxOutputTokens: 123},
			ProviderOptions: &provider.ModelProviderOptions{
				OpenAI: map[string]any{
					"temperature":      0.2,
					"reasoning_effort": "low",
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

	if got.Model != "model-a" || !got.Stream {
		t.Fatalf("unexpected request head: %+v", got)
	}
	if got.StreamOptions == nil || !got.StreamOptions.IncludeUsage {
		t.Fatalf("usage stream option not enabled: %+v", got.StreamOptions)
	}
	if got.Temperature == nil || *got.Temperature != 0.2 || got.MaxCompletionTokens == nil || *got.MaxCompletionTokens != 123 || got.ReasoningEffort != "low" {
		t.Fatalf("model config not applied: %+v", got)
	}
	if len(got.Tools) != 1 || got.Tools[0].Function.Name != "web_fetch" || string(got.Tools[0].Function.Parameters) != `{"type":"object"}` {
		t.Fatalf("tools not applied: %+v", got.Tools)
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
		if got.Messages[i].Role != want[i].Role || got.Messages[i].Content != want[i].Content {
			t.Fatalf("message %d: got %+v want %+v", i, got.Messages[i], want[i])
		}
	}
}

func TestRequestShapeWithToolHistory(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ch := streamForTest(t, srv.URL, provider.Request{
		Model: "model-a",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Text: "hi"},
			{Role: provider.RoleAssistant, Parts: []provider.Part{
				{Type: provider.PartToolUse, CallID: "call_1", Name: "builtin_time_get_current", Args: json.RawMessage(`{"timezone":"Asia/Singapore"}`)},
				{Type: provider.PartToolResult, CallID: "call_1", Name: "builtin_time_get_current", Ok: true, Content: `{"iso":"now"}`},
				{Type: provider.PartText, Text: "现在是中午。"},
			}},
		},
	})
	_ = collect(t, ch)

	if len(got.Messages) != 4 {
		t.Fatalf("unexpected messages: %+v", got.Messages)
	}
	if got.Messages[1].Role != "assistant" || len(got.Messages[1].ToolCalls) != 1 {
		t.Fatalf("assistant tool call message wrong: %+v", got.Messages[1])
	}
	call := got.Messages[1].ToolCalls[0]
	if call.ID != "call_1" || call.Type != "function" || call.Function.Name != "builtin_time_get_current" || call.Function.Arguments != `{"timezone":"Asia/Singapore"}` {
		t.Fatalf("tool call wrong: %+v", call)
	}
	if got.Messages[2].Role != "tool" || got.Messages[2].ToolCallID != "call_1" || got.Messages[2].Content != `{"iso":"now"}` {
		t.Fatalf("tool result message wrong: %+v", got.Messages[2])
	}
	if got.Messages[3].Role != "assistant" || got.Messages[3].Content != "现在是中午。" {
		t.Fatalf("final assistant message wrong: %+v", got.Messages[3])
	}
}

func TestRequestShapeDropsDanglingToolCallHistory(t *testing.T) {
	got := newChatRequestForTest(t, provider.Request{
		Model: "model-a",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Text: "hi"},
			{Role: provider.RoleAssistant, Parts: []provider.Part{
				{Type: provider.PartToolUse, CallID: "call_1", Name: "builtin_time_get_current", Args: json.RawMessage(`{"timezone":"Asia/Singapore"}`)},
				{Type: provider.PartToolUse, CallID: "call_2", Name: "builtin_time_get_current", Args: json.RawMessage(`{"timezone":"UTC"}`)},
				{Type: provider.PartToolResult, CallID: "call_1", Name: "builtin_time_get_current", Ok: true, Content: `{"iso":"now"}`},
			}},
			{Role: provider.RoleUser, Text: "next"},
		},
	})

	if len(got.Messages) != 2 {
		t.Fatalf("unexpected messages: %+v", got.Messages)
	}
	if got.Messages[0].Role != "user" || got.Messages[0].Content != "hi" {
		t.Fatalf("first message wrong: %+v", got.Messages[0])
	}
	if got.Messages[1].Role != "user" || got.Messages[1].Content != "next" {
		t.Fatalf("second message wrong: %+v", got.Messages[1])
	}
	for _, msg := range got.Messages {
		if msg.Role == "tool" || len(msg.ToolCalls) > 0 {
			t.Fatalf("dangling tool history should be removed: %+v", got.Messages)
		}
	}
}

func TestRequestShapeDropsOrphanToolResult(t *testing.T) {
	got := newChatRequestForTest(t, provider.Request{
		Model: "model-a",
		Messages: []provider.Message{
			{Role: provider.RoleAssistant, Parts: []provider.Part{
				{Type: provider.PartToolResult, CallID: "call_1", Name: "builtin_time_get_current", Ok: true, Content: `{"iso":"now"}`},
				{Type: provider.PartText, Text: "继续。"},
			}},
		},
	})

	if len(got.Messages) != 1 {
		t.Fatalf("unexpected messages: %+v", got.Messages)
	}
	if got.Messages[0].Role != "assistant" || got.Messages[0].Content != "继续。" {
		t.Fatalf("orphan tool result should be dropped while text remains: %+v", got.Messages)
	}
}

func TestToolCallChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"web_fetch","arguments":"{\"url\""}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"https://example.com\"}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	chunks := collect(t, streamForTest(t, srv.URL, provider.Request{Model: "m"}))
	if len(chunks) != 3 {
		t.Fatalf("unexpected chunks: %+v", chunks)
	}
	if chunks[0].Tool == nil || chunks[0].Tool.CallID != "call_1" || chunks[0].Tool.Name != "web_fetch" || chunks[0].Tool.ArgsDelta != `{"url"` {
		t.Fatalf("first tool chunk wrong: %+v", chunks[0])
	}
	if chunks[1].Tool == nil || chunks[1].Tool.ArgsDelta != `:"https://example.com"}` {
		t.Fatalf("second tool chunk wrong: %+v", chunks[1])
	}
	if !chunks[2].Done || chunks[2].Finish != provider.FinishToolCalls {
		t.Fatalf("finish chunk wrong: %+v", chunks[2])
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

func TestStreamFinishWithoutDoneEmitsErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n")
	}))
	defer srv.Close()

	chunks := collect(t, streamForTest(t, srv.URL, provider.Request{Model: "m"}))
	last := chunks[len(chunks)-1]
	if last.Err == nil || last.Done {
		t.Fatalf("want Err without Done, got %+v", chunks)
	}
}

func TestStreamOptionsUnsupportedRetriesWithoutUsage(t *testing.T) {
	var requests []chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got chatRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, got)
		if len(requests) == 1 {
			http.Error(w, "unknown field stream_options", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"ok"}}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	chunks := collect(t, streamForTest(t, srv.URL, provider.Request{Model: "m"}))
	if got := chunksText(chunks); got != "ok" {
		t.Fatalf("unexpected deltas: %q", got)
	}
	if len(requests) != 2 {
		t.Fatalf("want retry once, got %d requests", len(requests))
	}
	if requests[0].StreamOptions == nil || !requests[0].StreamOptions.IncludeUsage {
		t.Fatalf("first request should include usage: %+v", requests[0].StreamOptions)
	}
	if requests[1].StreamOptions != nil {
		t.Fatalf("retry should omit stream_options: %+v", requests[1].StreamOptions)
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

func newChatRequestForTest(t *testing.T, req provider.Request) chatRequest {
	t.Helper()
	client := New(Config{BaseURL: "http://example.test", APIKey: "test-key"})
	httpReq, err := client.newRequest(context.Background(), req, true)
	if err != nil {
		t.Fatal(err)
	}
	var got chatRequest
	if err := json.NewDecoder(httpReq.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestChatMessagesForImagePart(t *testing.T) {
	msgs := chatMessagesFor(provider.Message{
		Role: provider.RoleUser,
		Parts: []provider.Part{
			{Type: provider.PartText, Text: "看图"},
			{Type: provider.PartImage, MIME: "image/png", Data: []byte("png")},
		},
	})
	data, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, `"type":"image_url"`) || !strings.Contains(body, `data:image/png;base64,cG5n`) {
		t.Fatalf("image part not serialized for chat completions: %s", body)
	}
}

func TestChatMessagesForAudioPart(t *testing.T) {
	msgs := chatMessagesFor(provider.Message{
		Role: provider.RoleUser,
		Parts: []provider.Part{
			{Type: provider.PartText, Text: "听一下"},
			{Type: provider.PartAudio, MIME: "audio/wav", Data: []byte("wav")},
		},
	})
	data, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, `"type":"input_audio"`) || !strings.Contains(body, `"data":"d2F2"`) || !strings.Contains(body, `"format":"wav"`) {
		t.Fatalf("audio part not serialized for chat completions: %s", body)
	}
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
