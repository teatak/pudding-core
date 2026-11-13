package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestRepeatedToolFailuresDistinguishProgress(t *testing.T) {
	var guard repeatedToolFailures
	call := tool.Call{Name: tool.FileRead, CallID: "one", Args: json.RawMessage(`{"scope":"project","path":"missing"}`)}
	result := store.ContentPart{Type: store.ContentPartToolResult, CallID: "one", Content: `{"error":"not_found"}`}
	observe := func() bool { return guard.observe([]tool.Call{call}, []store.ContentPart{result}) }
	for i := range 4 {
		if observe() != (i == 3) {
			t.Fatalf("stop at attempt %d", i+1)
		}
		if (guard.reminder() != "") != (i >= 2) {
			t.Fatal("warning timing incorrect")
		}
		call.Args = json.RawMessage(`{ "path": "missing", "scope": "project" }`)
	}
	call.Args = json.RawMessage(`{"path":"other","scope":"project"}`)
	if observe() || guard.count != 1 {
		t.Fatal("changed input did not reset")
	}
	result.Content = `{"error":"permission_denied"}`
	if observe() || guard.count != 1 {
		t.Fatal("changed result did not reset")
	}
	result.Ok = true
	for range 6 {
		if observe() || guard.count != 0 {
			t.Fatal("successful read treated as stuck")
		}
	}
	result.Ok = false
	call.Name = tool.CommandSession
	for range 6 {
		if observe() || guard.count != 0 {
			t.Fatal("process polling treated as stuck")
		}
	}
	call.Name = tool.CommandRun
	call.Args = json.RawMessage(`{"scope":"project","command":"test"}`)
	result.Ok = true // A failed process is still a successful tool invocation.
	for i := range 4 {
		result.Content = fmt.Sprintf(`{"exitCode":1,"stdout":"","stderr":"compile error","durationMs":%d,"outputFiles":{"stderr":{"path":"attempt-%d.txt"}}}`, i, i)
		if observe() != (i == 3) {
			t.Fatal("command timing/artifact path concealed repeated failure")
		}
	}
	result.Content = `{"exitCode":1,"stdout":"same head/tail","stdoutTruncated":true}`
	if observe() || guard.count != 0 {
		t.Fatal("equal previews were treated as equal full output")
	}
	call.Args = json.RawMessage(`{"background":true}`)
	if observe() || guard.count != 0 {
		t.Fatal("background process counted")
	}
}

type repeatingFailureClient struct{ requests []provider.Request }

func (*repeatingFailureClient) Name() string { return "repeat" }
func (c *repeatingFailureClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 3)
	if len(c.requests) > 5 {
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "Trying again."}
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{Index: 0, CallID: fmt.Sprint(len(c.requests)), Name: tool.TimeGetCurrent, ArgsDelta: `{}`}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	}
	close(out)
	return out, nil
}

func TestRepeatedToolFailuresCannotResetWithAssistantText(t *testing.T) {
	eng, st, _, sid := newTestEngine(t, mock.WithDelay(0))
	defer eng.Stop()
	client := &repeatingFailureClient{}
	eng.resolver = mapResolver{"mock": client}
	runner := &recordingToolRunner{defs: []provider.ToolDef{{Name: tool.TimeGetCurrent, Capability: store.ModeChat, InputSchema: json.RawMessage(`{"type":"object"}`)}}, result: tool.Result{Content: `{"error":"same failure"}`}}
	eng.tools = runner
	title := "existing title"
	if _, err := st.UpdateSession(context.Background(), sid, store.SessionUpdate{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "repeat", Text: "test"}); err != nil {
		t.Fatal(err)
	}
	eng.Wait()
	if len(client.requests) != 4 || len(runner.calls) != 4 {
		t.Fatalf("did not stop: requests=%d calls=%d", len(client.requests), len(runner.calls))
	}
	reminder := (&repeatedToolFailures{count: 3}).reminder()
	if strings.Contains(client.requests[0].System, reminder) || !strings.Contains(client.requests[3].System, reminder) {
		t.Fatal("model did not receive recovery reminder")
	}
	events, err := st.EventsAfter(context.Background(), sid, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.Kind != event.TurnFailed || !strings.HasPrefix(last.Error, "repeated_tool_failure:") {
		t.Fatalf("bad termination: %+v", last)
	}
	messages, err := st.ListMessages(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Type == store.ContentPartToolResult {
				count++
			}
		}
	}
	if count != 4 {
		t.Fatalf("lost completed results: %d", count)
	}
}
