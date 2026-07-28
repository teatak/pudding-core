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
		if chunk.Part == "" || chunk.Part == provider.PartText {
			text.WriteString(chunk.Delta)
		}
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
			Limits: &provider.ModelLimits{MaxOutputTokens: 1234},
			ProviderOptions: &provider.ModelProviderOptions{
				Anthropic: map[string]any{
					"temperature":   0.4,
					"output_config": map[string]any{"effort": "max"},
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
	outputConfig, ok := gotBody.OutputConfig.(map[string]any)
	if !ok || outputConfig["effort"] != "max" {
		t.Fatalf("output config not applied: %+v", gotBody.OutputConfig)
	}
	if len(gotBody.Tools) != 1 || gotBody.Tools[0].Name != "web_fetch" || string(gotBody.Tools[0].InputSchema) != `{"type":"object"}` {
		t.Fatalf("tools not applied: %+v", gotBody.Tools)
	}
	roles := []string{}
	for _, m := range gotBody.Messages {
		roles = append(roles, m.Role)
	}
	if strings.Join(roles, ",") != "user,assistant,user" {
		t.Fatalf("role mapping wrong: %v", roles)
	}
}

func TestReadSSEUsage(t *testing.T) {
	out := make(chan provider.Chunk, 8)
	err := readSSE(context.Background(), strings.NewReader(happyStream), out)
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
	if usage.InputUncachedTokens != 10 || usage.OutputContentTokens != 8 {
		t.Fatalf("usage wrong: %+v", usage)
	}
}

func TestRequestShapeWithToolHistory(t *testing.T) {
	var gotBody messagesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, happyStream)
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	ch, err := client.Stream(context.Background(), provider.Request{
		Model: "claude-opus-4-8",
		Messages: []provider.Message{
			{Role: provider.RoleAssistant, Parts: []provider.Part{
				{Type: provider.PartToolUse, CallID: "call_1", Name: "first", Args: json.RawMessage(`{"value":1}`)},
				{Type: provider.PartToolUse, CallID: "call_2", Name: "second", Args: json.RawMessage(`{"value":2}`)},
				{Type: provider.PartToolResult, CallID: "call_1", Name: "first", Ok: true, Content: `{"result":1}`},
				{Type: provider.PartToolResult, CallID: "call_2", Name: "second", Ok: true, Content: `{"result":2}`},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, _ = collect(t, ch)

	if len(gotBody.Messages) != 2 {
		t.Fatalf("unexpected messages: %+v", gotBody.Messages)
	}
	if gotBody.Messages[0].Role != "assistant" || gotBody.Messages[1].Role != "user" {
		t.Fatalf("roles wrong: %+v", gotBody.Messages)
	}
	assistantBlocks, ok := gotBody.Messages[0].Content.([]any)
	if !ok || len(assistantBlocks) != 2 {
		t.Fatalf("assistant content wrong: %+v", gotBody.Messages[0].Content)
	}
	for i, callID := range []string{"call_1", "call_2"} {
		toolUse := assistantBlocks[i].(map[string]any)
		if toolUse["type"] != "tool_use" || toolUse["id"] != callID {
			t.Fatalf("tool_use block %d wrong: %+v", i, toolUse)
		}
	}
	userBlocks, ok := gotBody.Messages[1].Content.([]any)
	if !ok || len(userBlocks) != 2 {
		t.Fatalf("user content wrong: %+v", gotBody.Messages[1].Content)
	}
	for i, callID := range []string{"call_1", "call_2"} {
		toolResult := userBlocks[i].(map[string]any)
		if toolResult["type"] != "tool_result" || toolResult["tool_use_id"] != callID {
			t.Fatalf("tool_result block %d wrong: %+v", i, toolResult)
		}
	}
}

func TestToolUseChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"web_fetch","input":{}}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"url\""}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":":\"https://example.com\"}"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"message_stop"}`+"\n\n")
	}))
	defer srv.Close()

	client := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	ch, _ := client.Stream(context.Background(), provider.Request{Model: "m"})
	var chunks []provider.Chunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
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
	var text, thought strings.Builder
	done := false
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if chunk.Done {
			done = true
			break
		}
		switch chunk.Part {
		case provider.PartThought:
			thought.WriteString(chunk.Delta)
		case "", provider.PartText:
			text.WriteString(chunk.Delta)
		}
	}
	if !done {
		t.Fatal("stream did not finish")
	}
	if text.String() != "答案" || thought.String() != "推理..." {
		t.Fatalf("unexpected parts: text=%q thought=%q", text.String(), thought.String())
	}
}

func TestContinuationReplaysThinkingRedactionAndToolUse(t *testing.T) {
	out := make(chan provider.Chunk, 12)
	err := readSSE(context.Background(), strings.NewReader(
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`+"\n\n"+
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private reasoning"}}`+"\n\n"+
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signed-state"}}`+"\n\n"+
			`data: {"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"redacted-cipher"}}`+"\n\n"+
			`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"call_1","name":"lookup","input":{}}}`+"\n\n"+
			`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"q\":\"value\"}"}}`+"\n\n"+
			`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}`+"\n\n"+
			`data: {"type":"message_stop"}`+"\n\n",
	), out)
	close(out)
	if err != nil {
		t.Fatal(err)
	}
	var continuation *provider.Continuation
	for chunk := range out {
		if chunk.Done {
			continuation = chunk.Continuation
		}
	}
	if continuation == nil || continuation.Kind != provider.ContinuationAnthropic {
		t.Fatalf("missing anthropic continuation: %+v", continuation)
	}

	messages := messagesFor(provider.Message{
		Role: provider.RoleAssistant,
		Parts: []provider.Part{
			{Type: provider.PartThought, Text: "display-only summary"},
			{Type: provider.PartToolUse, CallID: "call_1", Name: "lookup", Args: json.RawMessage(`{"q":"value"}`)},
			{Type: provider.PartToolResult, CallID: "call_1", Name: "lookup", Ok: true, Content: `{"answer":1}`},
		},
		Continuations: []provider.Continuation{*continuation},
	})
	if len(messages) != 2 {
		t.Fatalf("got %d messages, want 2: %+v", len(messages), messages)
	}
	blocks, ok := messages[0].Content.([]contentBlock)
	if !ok || len(blocks) != 3 {
		t.Fatalf("assistant continuation changed: %#v", messages[0].Content)
	}
	if blocks[0].Type != "thinking" || blocks[0].Thinking != "private reasoning" || blocks[0].Signature != "signed-state" {
		t.Fatalf("thinking block changed: %+v", blocks[0])
	}
	if blocks[1].Type != "redacted_thinking" || blocks[1].Data != "redacted-cipher" {
		t.Fatalf("redacted block changed: %+v", blocks[1])
	}
	if blocks[2].Type != "tool_use" ||
		blocks[2].ID != "call_1" ||
		blocks[2].Name != "lookup" ||
		string(blocks[2].Input) != `{"q":"value"}` {
		t.Fatalf("tool block changed: %+v", blocks[2])
	}
	results, ok := messages[1].Content.([]contentBlock)
	if !ok || len(results) != 1 || results[0].Type != "tool_result" || results[0].ToolUseID != "call_1" {
		t.Fatalf("tool result changed: %#v", messages[1].Content)
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

func TestMessagesForImagePart(t *testing.T) {
	messages := messagesFor(provider.Message{
		Role: provider.RoleUser,
		Parts: []provider.Part{
			{Type: provider.PartText, Text: "看图"},
			{Type: provider.PartImage, MIME: "image/png", Data: []byte("png")},
		},
	})
	data, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, `"type":"image"`) || !strings.Contains(body, `"media_type":"image/png"`) || !strings.Contains(body, `"data":"cG5n"`) {
		t.Fatalf("image part not serialized for anthropic: %s", body)
	}
}
