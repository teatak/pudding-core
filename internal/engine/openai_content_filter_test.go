package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/openai"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestOpenAIContentFilterFailsTurnWithoutExecutingStreamedTools(t *testing.T) {
	const partialText = "Partial answer."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Complete tool arguments and some text can arrive before the terminal
		// filter reason. The tool must remain unexecuted when that reason arrives.
		frames := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"filtered_call","type":"function","function":{"name":"builtin_time_get_current","arguments":"{\"timezone\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"UTC\"}"}}]}}]}`,
			`{"choices":[{"delta":{"content":"` + partialText + `"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"content_filter"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3}}`,
			`[DONE]`,
		}
		for _, frame := range frames {
			fmt.Fprintf(w, "data: %s\n\n", frame)
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	ms := memstore.New()
	const sessionID = "content-filter-session"
	if err := ms.CreateSession(ctx, &store.Session{
		ID: sessionID, Title: "Content filter", Provider: "openai", Model: "test-model",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		ID: "openai", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{ID: "test-model", Capabilities: &store.ModelCaps{Tools: true}}},
	}); err != nil {
		t.Fatal(err)
	}
	runner := &recordingToolRunner{
		defs: []provider.ToolDef{{
			Name: tool.TimeGetCurrent, Capability: store.ModeChat,
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
		result: tool.Result{Ok: true, Content: `{"time":"now"}`},
	}
	client := openai.New(openai.Config{BaseURL: srv.URL, HTTPClient: &http.Client{Timeout: 2 * time.Second}})
	eng := New(ms, event.NewHub(), mapResolver{"openai": client}, ms, WithTools(runner))
	defer eng.Stop()
	if _, err := eng.Submit(ctx, SubmitInput{
		SessionID: sessionID, ClientMessageID: "filtered-request", Text: "Check the time",
	}); err != nil {
		t.Fatal(err)
	}
	eng.Wait()

	turns, err := ms.ListTurnsPage(ctx, sessionID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns.Turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(turns.Turns))
	}
	turn := turns.Turns[0]
	if turn.Status != store.TurnFailed || !strings.Contains(turn.Error, "content_filter") {
		t.Errorf("filtered turn status=%s error=%q, want failed with content_filter", turn.Status, turn.Error)
	}
	if len(runner.calls) != 0 {
		t.Errorf("filtered tool calls must not execute: %+v", runner.calls)
	}

	sawText, sawTool := false, false
	for _, msg := range turn.Messages {
		if msg.Role != store.RoleAssistant {
			continue
		}
		if msg.Text == partialText {
			sawText = true
			if !msg.Interrupted {
				t.Error("partial assistant text must be marked interrupted")
			}
		}
		for _, part := range msg.Parts {
			if part.Type == store.ContentPartToolUse && part.CallID == "filtered_call" && string(part.Args) == `{"timezone":"UTC"}` {
				sawTool = true
			}
		}
	}
	if !sawText || !sawTool {
		t.Errorf("streamed text and pending tool must be preserved: text=%v tool=%v", sawText, sawTool)
	}

	events, err := ms.EventsAfter(ctx, sessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	sawFailure := false
	for _, ev := range events {
		if ev.Kind == event.TurnCompleted {
			t.Error("filtered response must not emit turn.completed")
		}
		if ev.Kind == event.TurnFailed {
			sawFailure = true
			if !ev.Interrupted || !strings.Contains(ev.Error, "content_filter") {
				t.Errorf("failed event must retain the filter reason and interrupted state: %+v", ev)
			}
		}
	}
	if !sawFailure {
		t.Error("filtered response must emit turn.failed")
	}
}
