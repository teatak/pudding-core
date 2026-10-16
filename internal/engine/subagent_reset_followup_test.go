package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestSubagentResetListKeepsTaskLabelsInsideCurrentContext(t *testing.T) {
	ctx := context.Background()
	eng, st, _ := newCollaborationEngine(t, mock.New())
	if err := st.CreateChildSession(ctx, "root", &store.Session{ID: "child", Title: "Original identity", Provider: "mock", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	begin := func(session, id, text string) {
		t.Helper()
		if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: session, TurnID: id, ClientMessageID: id, UserMessageID: id + "-input", UserText: text}); err != nil {
			t.Fatal(err)
		}
	}
	begin("child", "old", "OBSOLETE_TASK")
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "old", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CollectChildResults(ctx, "root"); err != nil {
		t.Fatal(err)
	}
	begin("root", "request", "Independent work")
	if _, err := eng.resetSubagent(ctx, "root", "request", "reset", "child", "old", nil, store.ModeWork); err != nil {
		t.Fatal(err)
	}
	check := func(wantTitle string) {
		t.Helper()
		views, err := eng.ListChildSessions(ctx, "root")
		if err != nil || len(views) != 1 {
			t.Fatalf("list: %+v %v", views, err)
		}
		if views[0].TaskTitle != wantTitle || (wantTitle == "" && views[0].Summary != "") || views[0].Session.Title != "Original identity" {
			t.Errorf("task label crossed reset or changed identity: %+v", views[0])
		}
	}
	check("")
	// A system continuation has no new task label. Paginated lookup must stop
	// at the reset instead of finding the old input on the preceding page.
	if _, err := st.BeginSystemTurn(ctx, store.BeginSystemTurnInput{SessionID: "child", TurnID: "continuation", ClientMessageID: "continuation", SystemMessageID: "continuation-input", Text: "System continuation"}); err != nil {
		t.Fatal(err)
	}
	check("")
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "continuation", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	begin("child", "fresh", "FRESH_TASK")
	check("FRESH_TASK")
	if _, err := st.GetMessage(ctx, "child", "old-input"); err != nil {
		t.Fatalf("reset removed historical input: %v", err)
	}
}

func TestSubagentListReportsRemainingSharedFreshContexts(t *testing.T) {
	ctx := context.Background()
	eng, st, _ := newCollaborationEngine(t, mock.New())
	if err := st.CreateChildSession(ctx, "root", &store.Session{ID: "existing", Provider: "mock", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	for _, input := range []store.BeginTurnInput{
		{SessionID: "existing", TurnID: "old", ClientMessageID: "old", UserMessageID: "old-input", UserText: "old task"},
		{SessionID: "root", TurnID: "request", ClientMessageID: "request", UserMessageID: "request-input", UserText: "new tasks"},
	} {
		if _, err := st.BeginTurn(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "old", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CollectChildResults(ctx, "root"); err != nil {
		t.Fatal(err)
	}
	check := func(used int) {
		t.Helper()
		result := eng.executeSubtask(ctx, "root", "request", store.ModeWork, tool.Call{Name: tool.SubtaskList, CallID: "list", Args: json.RawMessage(`{}`)})
		var payload map[string]json.RawMessage
		if !result.Ok || json.Unmarshal([]byte(result.Content), &payload) != nil {
			t.Fatalf("list: %+v", result)
		}
		for key, want := range map[string]int{"fresh_context_used": used, "fresh_context_remaining": store.SubtaskDispatchLimit - used} {
			var got int
			if err := json.Unmarshal(payload[key], &got); err != nil || got != want {
				t.Errorf("%s=%s, want %d", key, payload[key], want)
			}
		}
	}
	check(0)
	for i, id := range []string{"new-one", "new-two"} {
		if _, err := st.DispatchChild(ctx, store.DispatchChildInput{ParentSessionID: "root", ParentTurnID: "request", CallID: id, Child: &store.Session{ID: id, Provider: "mock", Model: "model"}, Input: store.QueueInputInput{SessionID: id, ClientMessageID: id, Text: "new task"}}); err != nil {
			t.Fatal(err)
		}
		check(i + 1)
	}
	for i := 0; i < 2; i++ {
		if _, err := eng.resetSubagent(ctx, "root", "request", "reset", "existing", "old", nil, store.ModeWork); err != nil {
			t.Fatal(err)
		}
		check(3) // Accepted reset and its replay consume only one shared slot.
	}
}
