package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

type nextPromptHookRunner struct {
	*tool.BuiltinRunner
	hook func(tool.Call)
}

func (r *nextPromptHookRunner) Call(ctx context.Context, call tool.Call) tool.Result {
	result := r.BuiltinRunner.Call(ctx, call)
	if call.Name == tool.SetNextPrompt {
		r.hook(call)
	}
	return result
}

type nextPromptClient struct {
	mu        sync.Mutex
	requests  []provider.Request
	responses [][]provider.Chunk
}

func (c *nextPromptClient) Name() string { return "next-prompt-test" }
func (c *nextPromptClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	index := len(c.requests)
	c.requests = append(c.requests, req)
	chunks := []provider.Chunk{{Part: provider.PartText, Delta: "完成"}, {Done: true, Finish: provider.FinishStop}}
	if index < len(c.responses) {
		chunks = c.responses[index]
	}
	out := make(chan provider.Chunk, len(chunks))
	for _, chunk := range chunks {
		out <- chunk
	}
	close(out)
	return out, nil
}

func (c *nextPromptClient) snapshot() []provider.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]provider.Request(nil), c.requests...)
}

func TestNextPromptDoesNotRequireAnotherModelRequest(t *testing.T) {
	text := provider.Chunk{Part: provider.PartText, Delta: "任务已完成。"}
	thought := provider.Chunk{Part: provider.PartThought, Delta: "Thinking"}
	stop := provider.Chunk{Done: true, Finish: provider.FinishStop}
	toolStop := provider.Chunk{Done: true, Finish: provider.FinishToolCalls,
		Continuation: &provider.Continuation{Kind: provider.ContinuationGoogle, Data: json.RawMessage(`[{"thoughtSignature":"tip-state"}]`)}}
	call := func(id, name, args string, index int) provider.Chunk {
		return provider.Chunk{Tool: &provider.ToolCallChunk{CallID: id, Name: name, ArgsDelta: args, Index: index}}
	}
	tip := call("tip", tool.SetNextPrompt, `{"text":"帮我补上测试"}`, 0)
	timeCall := call("time", tool.TimeGetCurrent, `{}`, 1)
	for _, tc := range []struct {
		name      string
		responses [][]provider.Chunk
		calls     int
		tipResult bool
	}{
		{"optional omission", [][]provider.Chunk{{text, stop}}, 1, false},
		{"answer and suggestion", [][]provider.Chunk{{text, tip, toolStop}}, 1, true},
		{"invalid suggestion is not retried", [][]provider.Chunk{{text, call("tip", tool.SetNextPrompt, `{"text":" "}`, 0), toolStop}}, 1, true},
		{"suggestion alone needs an answer", [][]provider.Chunk{{tip, toolStop}}, 2, true},
		{"reasoning is not a final answer", [][]provider.Chunk{{thought, tip, toolStop}}, 2, true},
		{"whitespace is not a final answer", [][]provider.Chunk{{{Part: provider.PartText, Delta: " \n"}, tip, toolStop}}, 2, true},
		{"mixed task tools continue", [][]provider.Chunk{{text, tip, timeCall, toolStop}}, 2, true},
		{"earlier answer cannot terminate a later call", [][]provider.Chunk{{text, timeCall, toolStop}, {tip, toolStop}}, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			ms := storetest.New(t)
			client := &nextPromptClient{responses: tc.responses}
			runner := tool.NewBuiltinRunner()
			eng := New(ms, event.NewHub(), mapResolver{"capture": client}, ms, WithTools(runner))
			t.Cleanup(func() { eng.Wait(); runner.Close() })
			const sid = "next-prompt"
			if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "test", Provider: "capture", Model: "test"}); err != nil {
				t.Fatal(err)
			}
			if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{DisplayName: "capture", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "test", Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
				t.Fatal(err)
			}
			submitted, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "first", Text: "完成任务"})
			if err != nil {
				t.Fatal(err)
			}
			waitTurnDone(t, ms, sid)
			turn, err := ms.GetConversationTurn(ctx, sid, submitted.TurnID)
			if err != nil {
				t.Fatal(err)
			}
			if turn.Status != store.TurnCompleted {
				t.Fatalf("status = %s, error = %s", turn.Status, turn.Error)
			}
			requests := client.snapshot()
			if len(requests) != tc.calls {
				t.Fatalf("model requests = %d, want %d", len(requests), tc.calls)
			}
			if !tool.HasDefinition(requests[0].Tools, tool.SetNextPrompt) {
				t.Fatal("suggestion tool missing")
			}
			// The next user request must replay paired native call/result history,
			// even though no request was made just to acknowledge the UI tool.
			if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "second", Text: "谢谢"}); err != nil {
				t.Fatal(err)
			}
			waitTurnDone(t, ms, sid)
			requests = client.snapshot()
			if len(requests) != tc.calls+1 {
				t.Fatalf("unexpected follow-up request count: %d", len(requests))
			}
			uses, results := 0, 0
			nativeState := false
			for _, msg := range requests[len(requests)-1].Messages {
				if provider.ContinuationFor(msg, provider.ContinuationGoogle) != nil {
					nativeState = true
				}
				for _, part := range msg.Parts {
					if part.Name != tool.SetNextPrompt {
						continue
					}
					if part.Type == provider.PartToolUse {
						uses++
					}
					if part.Type == provider.PartToolResult {
						results++
					}
				}
			}
			if uses != results || (tc.tipResult && uses != 1) {
				t.Fatalf("unpaired suggestion history: uses=%d results=%d", uses, results)
			}
			if tc.tipResult && !nativeState {
				t.Fatal("native continuation was lost before the next user request")
			}
		})
	}
}

func TestNextPromptRespectsSteeringAndCancellation(t *testing.T) {
	for _, action := range []string{"steer", "cancel"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			ms := storetest.New(t)
			client := &nextPromptClient{responses: [][]provider.Chunk{{
				{Part: provider.PartText, Delta: "任务已完成。"},
				{Tool: &provider.ToolCallChunk{CallID: "tip", Name: tool.SetNextPrompt, ArgsDelta: `{"text":"帮我补上测试"}`}},
				{Done: true, Finish: provider.FinishToolCalls},
			}}}
			runner := &nextPromptHookRunner{BuiltinRunner: tool.NewBuiltinRunner()}
			eng := New(ms, event.NewHub(), mapResolver{"capture": client}, ms, WithTools(runner))
			t.Cleanup(func() { eng.Wait(); runner.Close() })
			actionErrors := make(chan error, 1)
			runner.hook = func(call tool.Call) {
				if action == "cancel" {
					actionErrors <- eng.Cancel(call.SessionID)
				} else {
					_, err := eng.Steer(ctx, SteerInput{SessionID: call.SessionID, TurnID: call.TurnID, ClientMessageID: "steer", Text: "追加检查"})
					actionErrors <- err
				}
			}
			const sid = "next-prompt-control"
			if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "test", Provider: "capture", Model: "test"}); err != nil {
				t.Fatal(err)
			}
			if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{DisplayName: "capture", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "test", Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
				t.Fatal(err)
			}
			submitted, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "first", Text: "完成任务"})
			if err != nil {
				t.Fatal(err)
			}
			waitTurnDone(t, ms, sid)
			select {
			case err := <-actionErrors:
				if err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatal("suggestion tool was not executed")
			}
			turn, err := ms.GetConversationTurn(ctx, sid, submitted.TurnID)
			if err != nil {
				t.Fatal(err)
			}
			wantStatus, wantCalls := store.TurnCompleted, 2
			if action == "cancel" {
				wantStatus, wantCalls = store.TurnCancelled, 1
			}
			requests := client.snapshot()
			if turn.Status != wantStatus || len(requests) != wantCalls {
				t.Fatalf("status=%s requests=%d", turn.Status, len(requests))
			}
			if action == "steer" {
				found := false
				for _, message := range requests[1].Messages {
					found = found || strings.Contains(message.Text, "追加检查")
				}
				if !found {
					t.Fatal("terminal suggestion swallowed the steer")
				}
			}
		})
	}
}
