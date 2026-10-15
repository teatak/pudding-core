package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestSubagentResetAdmissionPreservesBlockedContexts(t *testing.T) {
	for _, reason := range []string{"running", "queued", "editing", "approval", "question", "background", "compaction", "uncollected", "stale"} {
		t.Run(reason, func(t *testing.T) {
			ctx := context.Background()
			eng, st, _ := newCollaborationEngine(t, mock.New())
			const childID = "background"
			if err := st.CreateChildSession(ctx, "root", &store.Session{ID: childID, Title: "Old task", Provider: "mock", Model: "model"}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: childID, TurnID: "old", ClientMessageID: "old", UserMessageID: "old-input", UserText: "old objective"}); err != nil {
				t.Fatal(err)
			}
			if reason != "running" {
				parts := store.TextPart("old result")
				if reason == "question" {
					parts = []store.ContentPart{
						{Type: store.ContentPartToolUse, Name: tool.RequestUserInput, CallID: "q", Args: json.RawMessage(`{"title":"Scope?"}`)},
						{Type: store.ContentPartToolResult, Name: tool.RequestUserInput, CallID: "q", Ok: true, Content: `{"requestID":"old:q","status":"timeout"}`},
					}
				}
				if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "old", Status: store.TurnFailed, AssistantParts: parts}); err != nil {
					t.Fatal(err)
				}
				if reason != "uncollected" {
					if _, err := st.CollectChildResults(ctx, "root"); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "root", TurnID: "request", ClientMessageID: "request", UserMessageID: "request-input", UserText: "independent task"}); err != nil {
				t.Fatal(err)
			}
			expected, want := "old", store.ErrSubagentResetBusy
			switch reason {
			case "queued", "editing":
				if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: childID, ClientMessageID: "pending", Text: "must preserve"}); err != nil {
					t.Fatal(err)
				}
				if reason == "editing" {
					status := store.QueuedInputEditing
					if _, err := st.UpdateQueuedInput(ctx, store.UpdateQueuedInputInput{SessionID: childID, ClientMessageID: "pending", Status: &status}); err != nil {
						t.Fatal(err)
					}
				}
			case "approval":
				eng.approvals["pending"] = &pendingApproval{req: ApprovalRequest{ID: "pending", SessionID: childID}}
			case "background":
				eng.tools = &archiveBackgroundRunner{Runner: eng.tools}
			case "compaction":
				eng.compacting[childID] = true
				defer delete(eng.compacting, childID)
			case "uncollected":
				want = store.ErrSubagentResultUncollected
			case "stale":
				expected, want = "older-snapshot", store.ErrSubagentStateChanged
			}
			before, err := st.ListMessages(ctx, childID, 0)
			if err != nil {
				t.Fatal(err)
			}
			queueBefore, err := st.ListQueuedInputs(ctx, childID)
			if err != nil {
				t.Fatal(err)
			}
			prompt := "new self-contained task"
			if _, err := eng.resetSubagent(ctx, "root", "request", "reset", childID, expected, &prompt, store.ModeWork); !errors.Is(err, want) {
				t.Fatalf("reset admission: got %v, want %v", err, want)
			}
			after, err := st.ListMessages(ctx, childID, 0)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected reset changed history: %v", err)
			}
			queueAfter, err := st.ListQueuedInputs(ctx, childID)
			if err != nil || !reflect.DeepEqual(queueBefore, queueAfter) {
				t.Fatalf("rejected reset changed queued work: %v", err)
			}
			if used, err := st.SubagentFreshContextCount(ctx, "root", "request"); err != nil || used != 0 {
				t.Fatalf("rejected reset consumed quota: %d %v", used, err)
			}
		})
	}
}
