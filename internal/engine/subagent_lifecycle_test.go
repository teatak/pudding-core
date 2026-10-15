package engine

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestSubagentResetRuntimeAdmission(t *testing.T) {
	for _, guard := range []string{"approval", "question", "background", "compacting", "uncollected", "stale", "disabled", "stopped"} {
		t.Run(guard, func(t *testing.T) {
			ctx := context.Background()
			eng, st, plugins := newCollaborationEngine(t, mock.New())
			id := "child"
			if guard == "background" {
				id = "background"
				eng.tools = &archiveBackgroundRunner{Runner: eng.tools}
			}
			if err := st.CreateChildSession(ctx, "root", &store.Session{ID: id, Provider: "mock", Model: "model"}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: id, TurnID: "old", ClientMessageID: "old", UserMessageID: "old-input", UserText: "original task"}); err != nil {
				t.Fatal(err)
			}
			parts := []store.ContentPart{{Type: store.ContentPartText, Text: "result"}}
			if guard == "question" {
				parts = []store.ContentPart{
					{Type: store.ContentPartToolUse, Name: tool.RequestUserInput, CallID: "q", Args: json.RawMessage(`{"title":"Scope?"}`)},
					{Type: store.ContentPartToolResult, Name: tool.RequestUserInput, CallID: "q", Ok: true, Content: `{"requestID":"old:q","status":"timeout"}`},
				}
			}
			if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "old", Status: store.TurnCompleted, AssistantParts: parts}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "root", TurnID: "parent", ClientMessageID: "request", UserMessageID: "request-input", UserText: "new independent work"}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = st.FinishTurn(context.Background(), store.FinishTurnInput{TurnID: "parent", Status: store.TurnCancelled}) })
			if guard != "uncollected" {
				if _, err := st.CollectChildResults(ctx, "root"); err != nil {
					t.Fatal(err)
				}
			}
			expected := "old"
			switch guard {
			case "approval":
				eng.approvals["pending"] = &pendingApproval{req: ApprovalRequest{ID: "pending", SessionID: id}}
			case "compacting":
				eng.compacting[id] = true
				defer delete(eng.compacting, id)
			case "stale":
				expected = "stale"
			case "disabled":
				plugins.enabled.Store(false)
			case "stopped":
				if _, err := st.StopCollaboration(ctx, "root"); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := st.ListMessages(ctx, id, 0)
			args, _ := json.Marshal(map[string]any{"session_id": id, "prompt": "fresh objective", "reset_context": true, "expected_turn_id": expected})
			result := eng.executeSubtask(ctx, "root", "parent", store.ModeWork, tool.Call{Name: tool.SubtaskSend, CallID: "attempt", Args: args})
			if result.Ok {
				t.Fatalf("%s did not block reset-and-send: %s", guard, result.Content)
			}
			after, _ := st.ListMessages(ctx, id, 0)
			queued, _ := st.ListQueuedInputs(ctx, id)
			if !reflect.DeepEqual(before, after) || len(queued) != 0 {
				t.Fatal("rejected reset changed history or queued work")
			}
		})
	}
}

func TestSubagentResetCheckpointIsIdleAndRejectsOldRetry(t *testing.T) {
	ctx := context.Background()
	eng, st, _ := newCollaborationEngine(t, mock.New())
	if err := st.CreateChildSession(ctx, "root", &store.Session{ID: "child", Provider: "mock", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "child", TurnID: "failed", ClientMessageID: "old", UserMessageID: "old-input", UserText: "obsolete task"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "failed", Status: store.TurnFailed}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "root", TurnID: "parent", ClientMessageID: "request", UserMessageID: "request-input", UserText: "new work"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = st.FinishTurn(context.Background(), store.FinishTurnInput{TurnID: "parent", Status: store.TurnCancelled}) })
	if _, err := st.CollectChildResults(ctx, "root"); err != nil {
		t.Fatal(err)
	}
	call := tool.Call{Name: tool.SubtaskReset, CallID: "reset", Args: json.RawMessage(`{"session_id":"child","expected_turn_id":"failed"}`)}
	if result := eng.executeSubtask(ctx, "root", "parent", store.ModeWork, call); !result.Ok {
		t.Fatal(result.Content)
	}
	children, err := eng.ListChildSessions(ctx, "root")
	if err != nil || len(children) != 1 {
		t.Fatalf("list: %+v %v", children, err)
	}
	child := children[0]
	if child.Status != "idle" || child.TaskTitle != "" || child.Summary != "" || !child.ResultCollected || !subagentCanReset(child) {
		t.Fatalf("reset checkpoint projected as an old/completed task: %+v", child)
	}
	if _, err := eng.Retry(ctx, "child", "failed", "stale-retry"); err == nil {
		t.Fatal("pre-reset failed task was allowed to restart across the boundary")
	}
	if result := eng.executeSubtask(ctx, "root", "parent", store.ModeWork, call); !result.Ok {
		t.Fatalf("accepted reset did not replay: %s", result.Content)
	}
}
