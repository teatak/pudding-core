package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

type archiveBackgroundRunner struct {
	tool.Runner
	tool.BackgroundProcessController
}

func (*archiveBackgroundRunner) BackgroundProcessCount(id string) int {
	if id == "background" {
		return 1
	}
	return 0
}

func TestSubtaskArchiveEligibilityAndModelTools(t *testing.T) {
	ctx := context.Background()
	eng, st, _ := newCollaborationEngine(t, mock.New())
	eng.tools = &archiveBackgroundRunner{Runner: eng.tools}
	for _, id := range []string{"done", "approval", "question", "background", "running", "queued", "failed", "cancelled"} {
		if err := st.CreateChildSession(ctx, "root", &store.Session{ID: id, Title: id, Provider: "mock", Model: "model"}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: id, TurnID: id, ClientMessageID: id, UserMessageID: id + "-input", UserText: "work"}); err != nil {
			t.Fatal(err)
		}
		if id == "running" {
			continue
		}
		status := store.TurnCompleted
		if id == "failed" {
			status = store.TurnFailed
		}
		if id == "cancelled" {
			status = store.TurnCancelled
		}
		parts := []store.ContentPart{{Type: store.ContentPartText, Text: "result"}}
		if id == "question" {
			parts = []store.ContentPart{{Type: store.ContentPartToolUse, Name: tool.RequestUserInput, CallID: "q", Args: json.RawMessage(`{"title":"Scope?"}`)}, {Type: store.ContentPartToolResult, Name: tool.RequestUserInput, CallID: "q", Ok: true, Content: `{"requestID":"question:q","status":"timeout"}`}}
		}
		if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: id, Status: status, AssistantParts: parts}); err != nil {
			t.Fatal(err)
		}
	}
	eng.approvals["approval"] = &pendingApproval{req: ApprovalRequest{ID: "approval", SessionID: "approval"}}
	if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "queued", ClientMessageID: "more", Text: "more work"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"approval", "question", "background", "running", "queued"} {
		if err := eng.SetSubtaskArchived(ctx, "root", id, true); !errors.Is(err, store.ErrSubtaskArchiveBusy) {
			t.Errorf("archived blocked %s: %v", id, err)
		}
	}
	run := func(name, args string) tool.Result {
		return eng.executeSubtask(ctx, "root", "parent", store.ModeWork, tool.Call{Name: name, CallID: name, Args: json.RawMessage(args)})
	}
	for _, args := range []string{`{}`, `{"session_id":""}`, `{"session_id":" "}`} {
		if result := run(tool.SubtaskArchive, args); result.Ok {
			t.Fatalf("model implicitly archived all children: %+v", result)
		}
	}
	if archives, _ := st.ListSubtaskArchives(ctx, "root"); len(archives) != 0 {
		t.Fatal("rejected archive changed state")
	}
	for _, id := range []string{"failed", "cancelled"} {
		args, _ := json.Marshal(map[string]string{"session_id": id})
		if result := run(tool.SubtaskArchive, string(args)); !result.Ok {
			t.Fatalf("settled %s cannot be archived: %s", id, result.Content)
		}
	}
	// Bulk cleanup remains an explicit user/API operation, not an empty model call.
	if err := eng.SetSubtaskArchived(ctx, "root", "", true); err != nil {
		t.Fatal(err)
	}
	archives, err := st.ListSubtaskArchives(ctx, "root")
	if err != nil || len(archives) != 3 || archives["done"].IsZero() || archives["failed"].IsZero() || archives["cancelled"].IsZero() {
		t.Fatalf("batch eligibility: %v %v", archives, err)
	}
	for _, item := range readCollaborationList(t, eng) {
		if item.SessionID == "done" || item.SessionID == "failed" || item.SessionID == "cancelled" {
			t.Fatal("default model list exposes archives")
		}
	}
	result := run(tool.SubtaskList, `{"include_archived":true}`)
	var payload struct {
		Children []collaborationListItem `json:"children"`
		Total int `json:"total_count"`
		Archived int `json:"archived_count"`
		Limit int `json:"total_limit"`
	}
	if !result.Ok || json.Unmarshal([]byte(result.Content), &payload) != nil || len(payload.Children) != 8 || payload.Total != 8 || payload.Archived != 3 || payload.Limit != 24 {
		t.Fatalf("archive discovery: %+v", result)
	}
	if result := run(tool.SubtaskRestore, `{"session_id":"done"}`); !result.Ok {
		t.Fatal(result.Content)
	}
	if archives, _ := st.ListSubtaskArchives(ctx, "root"); len(archives) != 2 {
		t.Fatal("restore changed unrelated archive state")
	}
	if result := run(tool.SubtaskArchive, `{"session_id":"done"}`); !result.Ok {
		t.Fatal(result.Content)
	}
	if result := eng.executeSubtask(ctx, "done", "child-turn", store.ModeWork, tool.Call{Name: tool.SubtaskRestore, CallID: "recursive", Args: json.RawMessage(`{"session_id":"done"}`)}); result.Ok {
		t.Fatal("child acquired parent archive controls")
	}
	// Stop() must not wait for this synthetic store-only running turn.
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "running", Status: store.TurnCancelled}); err != nil {
		t.Fatal(err)
	}
}
