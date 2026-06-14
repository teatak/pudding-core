package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/provider"
)

func collect(t *testing.T, ch <-chan provider.Chunk) (string, bool, error) {
	t.Helper()
	var text strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return text.String(), false, chunk.Err
		}
		if chunk.Done {
			return text.String(), true, nil
		}
		text.WriteString(chunk.Delta)
	}
	return text.String(), false, nil
}

// happyStream 是 Messages API 实测事件序列的最小固化
// (message_start → text 块 → message_delta(stop_reason) → message_stop)。
const happyStream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_01","role":"assistant","content":[],"model":"claude-opus-4-8","usage":{"input_tokens":10,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: ping
data: {"type":"ping"}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":",世界"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":8}}

event: message_stop
data: {"type":"message_stop"}

`

func TestStreamHappyPath(t *testing.T) {
	var gotPath, gotKey, gotVersion string
	var gotBody messagesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, happyStream)
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()})
	ch, err := client.Stream(context.Background(), provider.Request{
		Model:  "claude-opus-4-8",
		System: "be nice",
		Config: provider.ModelConfig{
			Anthropic: map[string]any{
				"max_tokens":  1234,
				"temperature": 0.4,
			},
		},
		Messages: []provider.Message{
			{Role: provider.RoleUser, Text: "hi"},
			{Role: provider.RoleAssistant, Text: "hello"},
			{Role: provider.RoleUser, Text: "again"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text, done, err := collect(t, ch)
	if err != nil || !done || text != "你好,世界" {
		t.Fatalf("text=%q done=%v err=%v", text, done, err)
	}

	if gotPath != "/v1/messages" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if gotKey != "test-key" || gotVersion != anthropicVersion {
		t.Fatalf("headers wrong: key=%q version=%q", gotKey, gotVersion)
	}
	if !gotBody.Stream || gotBody.MaxTokens != 1234 || gotBody.System != "be nice" {
		t.Fatalf("body wrong: %+v", gotBody)
	}
	if gotBody.Temperature == nil || *gotBody.Temperature != 0.4 {
		t.Fatalf("model config not applied: %+v", gotBody)
	}
	roles := []string{}
	for _, m := range gotBody.Messages {
		roles = append(roles, m.Role)
	}
	if strings.Join(roles, ",") != "user,assistant,user" {
		t.Fatalf("role mapping wrong: %v", roles)
	}
}

// thinking 块(adaptive thinking 下出现在 text 块之前)不得进入正文。
func TestThinkingBlockSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"推理..."}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_stop","index":0}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"答案"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"message_stop"}`+"\n\n")
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	ch, _ := client.Stream(context.Background(), provider.Request{Model: "m"})
	text, done, err := collect(t, ch)
	if err != nil || !done {
		t.Fatalf("done=%v err=%v", done, err)
	}
	if text != "答案" {
		t.Fatalf("thinking deltas must not leak into answer, got %q", text)
	}
}

func TestErrorEventEmitsErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"部分"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`+"\n\n")
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	ch, _ := client.Stream(context.Background(), provider.Request{Model: "m"})
	text, done, err := collect(t, ch)
	if done || err == nil || !strings.Contains(err.Error(), "overloaded_error") {
		t.Fatalf("want overloaded error, got text=%q done=%v err=%v", text, done, err)
	}
}

func TestNon2xxEmitsErrWithRedactedKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"bad key test-key"}}`)
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()})
	ch, _ := client.Stream(context.Background(), provider.Request{Model: "m"})
	_, _, err := collect(t, ch)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("want 401 error, got %v", err)
	}
	if strings.Contains(err.Error(), "test-key") {
		t.Fatalf("api key leaked into error: %v", err)
	}
}

func TestStreamEndsWithoutStopEmitsErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`+"\n\n")
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	ch, _ := client.Stream(context.Background(), provider.Request{Model: "m"})
	text, done, err := collect(t, ch)
	if done || err == nil || text != "partial" {
		t.Fatalf("want truncation error after partial, got text=%q done=%v err=%v", text, done, err)
	}
}

func TestRefusalStopReasonEmitsErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"message_delta","delta":{"stop_reason":"refusal"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"message_stop"}`+"\n\n")
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	ch, _ := client.Stream(context.Background(), provider.Request{Model: "m"})
	_, _, err := collect(t, ch)
	if err == nil || !strings.Contains(err.Error(), "refusal") {
		t.Fatalf("want refusal error, got %v", err)
	}
}

func TestContextCancelTerminatesPromptly(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	ch, err := client.Stream(ctx, provider.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()

	select {
	case chunk, ok := <-ch:
		if !ok || !errors.Is(chunk.Err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %+v ok=%v", chunk, ok)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancel did not terminate promptly")
	}
}
