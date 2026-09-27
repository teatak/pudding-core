package engine

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
	"github.com/teatak/pudding-core/internal/tool"
)

func readCollaborationList(t *testing.T, eng *Engine) []collaborationListItem {
	t.Helper()
	result := eng.executeAllowedTool(context.Background(), "root", "parent_turn", store.ModeWork, tool.Call{
		Name: tool.CollaborationList, CallID: "list", Args: json.RawMessage(`{}`),
	})
	if !result.Ok {
		t.Fatalf("list failed: %s", result.Content)
	}
	var payload struct {
		Children []collaborationListItem `json:"children"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Children == nil {
		t.Fatalf("children must be an array: %s", result.Content)
	}
	return payload.Children
}

func TestCollaborationListReportsUnansweredQuestions(t *testing.T) {
	for _, kind := range []string{"memory", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			eng, memory, _ := newCollaborationEngine(t, mock.New())
			var st store.Store = memory
			if kind == "sqlite" {
				db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "test.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				root, err := memory.GetSession(ctx, "root")
				if err != nil {
					t.Fatal(err)
				}
				if err := db.CreateSession(ctx, root); err != nil {
					t.Fatal(err)
				}
				st, eng.store = db, db
			}
			if err := st.CreateChildSession(ctx, "root", &store.Session{ID: "child", Title: "研究", Provider: "mock", Model: "model"}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "child", TurnID: "first", ClientMessageID: "first", UserMessageID: "first", UserText: "研究主题"}); err != nil {
				t.Fatal(err)
			}
			parts := []store.ContentPart{}
			for _, status := range []string{"awaiting_user", "timeout", "dismissed", "cancelled"} {
				content, _ := json.Marshal(map[string]string{"requestID": "first:" + status, "status": status})
				parts = append(parts,
					store.ContentPart{Type: store.ContentPartToolUse, Name: tool.RequestUserInput, CallID: status, Args: json.RawMessage(`{"title":"确认范围"}`)},
					store.ContentPart{Type: store.ContentPartToolResult, Name: tool.RequestUserInput, CallID: status, Ok: true, Content: string(content)},
				)
			}
			if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "first", Status: store.TurnCompleted, AssistantParts: parts}); err != nil {
				t.Fatal(err)
			}
			check := func(want int) {
				t.Helper()
				if item := readCollaborationList(t, eng)[0]; item.PendingUserInputs != want {
					t.Fatalf("pending questions = %d, want %d; snapshot: %+v", item.PendingUserInputs, want, item)
				}
			}
			// A completed turn can still need input. This also covers recovery
			// without any live request state, as after a daemon restart.
			check(2)
			queue := func(status string) {
				t.Helper()
				if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "child", ClientMessageID: "input-flow-first:" + status, Text: "确认", Provider: "mock", Model: "model"}); err != nil {
					t.Fatal(err)
				}
			}
			queue("awaiting_user")
			check(1)
			if _, err := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: "child", TurnID: "answer", UserMessageID: "answer"}); err != nil {
				t.Fatal(err)
			}
			// An older unanswered question remains visible after another answer
			// starts a continuation; the answered question must not reappear.
			check(1)
			queue("timeout")
			check(0)
			cancelled := store.QueuedInputCancelled
			if _, err := st.UpdateQueuedInput(ctx, store.UpdateQueuedInputInput{SessionID: "child", ClientMessageID: "input-flow-first:timeout", Status: &cancelled}); err != nil {
				t.Fatal(err)
			}
			check(1)
		})
	}
}

func TestCollaborationListReportsLiveQuestions(t *testing.T) {
	ctx := context.Background()
	eng, st, _ := newCollaborationEngine(t, mock.New())
	if err := st.CreateChildSession(ctx, "root", &store.Session{ID: "child", Provider: "mock", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Minute)
	request := &pendingUserInput{UserInputRequest: UserInputRequest{ID: "turn:q", SessionID: "child", TurnID: "turn", Status: "waiting", Deadline: &deadline}, ctx: ctx}
	eng.inputRequests = map[string]*pendingUserInput{
		"child:turn:q": request,
		"other:turn:q": {UserInputRequest: UserInputRequest{ID: "turn:q", SessionID: "other", Status: "waiting", Deadline: &deadline}, ctx: ctx},
	}
	if item := readCollaborationList(t, eng)[0]; item.PendingUserInputs != 1 {
		t.Fatalf("live question missing or crossed sessions: %+v", item)
	}
	request.Status = "answered"
	if item := readCollaborationList(t, eng)[0]; item.PendingUserInputs != 0 {
		t.Fatalf("answered live question remains pending: %+v", item)
	}
}

func TestCollaborationListIsScopedReadOnlyAndCurrent(t *testing.T) {
	ctx := context.Background()
	eng, st, _ := newCollaborationEngine(t, mock.New())
	if got := readCollaborationList(t, eng); len(got) != 0 {
		t.Fatalf("empty list: %+v", got)
	}
	if err := st.CreateSession(ctx, &store.Session{ID: "other", Provider: "mock", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"root", "child"}, {"other", "private_child"}} {
		if err := st.CreateChildSession(ctx, pair[0], &store.Session{ID: pair[1], Title: "北京天气", Provider: "mock", Model: "model"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "child", TurnID: "first", ClientMessageID: "first", UserMessageID: "first", UserText: "查询北京天气。"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "first", Status: store.TurnCompleted, AssistantParts: []store.ContentPart{{Type: store.ContentPartText, Text: "北京晴，26°C。更多详情。"}}}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.ListMessages(ctx, "root", 0)
	items := readCollaborationList(t, eng)
	if len(items) != 1 || items[0].SessionID != "child" || items[0].Title != "北京天气" || items[0].TaskTitle != "北京天气" || items[0].Status != "completed" || items[0].Summary != "北京晴，26°C" || items[0].ResultCollected {
		t.Fatalf("completed snapshot: %+v", items)
	}
	after, _ := st.ListMessages(ctx, "root", 0)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("listing collected results or changed parent messages")
	}
	if _, err := st.CollectChildResults(ctx, "root"); err != nil {
		t.Fatal(err)
	}
	if !readCollaborationList(t, eng)[0].ResultCollected {
		t.Fatal("collected result not reflected")
	}
	if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "child", ClientMessageID: "reuse", Text: "新任务：查询上海天气，沿用格式。", Provider: "mock", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	queued := readCollaborationList(t, eng)[0]
	if queued.Status != "queued" || queued.TaskTitle != "查询上海天气" || queued.Summary != "查询上海天气，沿用格式" || queued.ResultCollected {
		t.Fatalf("queued reuse: %+v", queued)
	}
	if _, err := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: "child", TurnID: "reuse", UserMessageID: "reuse"}); err != nil {
		t.Fatal(err)
	}
	eng.mu.Lock()
	eng.approvals["approval"] = &pendingApproval{req: ApprovalRequest{ID: "approval", SessionID: "child"}}
	eng.mu.Unlock()
	item := readCollaborationList(t, eng)[0]
	if item.Status != "running" || item.LatestTurnID != "reuse" || item.PendingApprovals != 1 || item.TaskTitle != queued.TaskTitle {
		t.Fatalf("running with approval: %+v", item)
	}
	// The tool and the UI read the same projection, without mutating task names.
	views, err := eng.ListChildSessions(ctx, "root")
	if err != nil || len(views) != 1 || views[0].Status != item.Status || views[0].TaskTitle != item.TaskTitle || views[0].Summary != item.Summary || views[0].PendingApprovals != item.PendingApprovals {
		t.Fatalf("UI/list mismatch: %+v %+v %v", views, item, err)
	}
	session, err := st.GetSession(ctx, "child")
	if err != nil || session.Title != "北京天气" {
		t.Fatalf("list changed the stored title: %+v %v", session, err)
	}
}

func TestCollaborationListKeepsAppModeAndParentBoundaries(t *testing.T) {
	ctx := context.Background()
	eng, st, apps := newCollaborationEngine(t, mock.New())
	hasList := func(sessionID string, mode store.AgentMode) bool {
		t.Helper()
		defs, err := eng.toolDefinitions(ctx, sessionID, mode)
		if err != nil {
			t.Fatal(err)
		}
		for _, def := range defs {
			if def.Name == tool.CollaborationList {
				return true
			}
		}
		return false
	}
	if !hasList("root", store.ModeWork) {
		t.Fatal("loaded collaboration app is missing list")
	}
	call := tool.Call{Name: tool.CollaborationList, CallID: "list", Args: json.RawMessage(`{}`)}
	if hasList("root", store.ModeChat) || eng.executeCollaboration(ctx, "root", "", store.ModeChat, call).Ok {
		t.Fatal("list bypassed the work mode gate")
	}
	call.Args = json.RawMessage(`{"session_id":"other"}`)
	if eng.executeCollaboration(ctx, "root", "", store.ModeWork, call).Ok {
		t.Fatal("list accepted a target session override")
	}
	call.Args = json.RawMessage(`{}`)
	if err := st.CreateChildSession(ctx, "root", &store.Session{ID: "child", Provider: "mock", Model: "model", LoadedAppIDs: []string{app.BuiltinCollaborationID}}); err != nil {
		t.Fatal(err)
	}
	if hasList("child", store.ModeWork) || eng.executeCollaboration(ctx, "child", "", store.ModeWork, call).Ok {
		t.Fatal("child received parent collaboration access")
	}
	apps.enabled.Store(false)
	if hasList("root", store.ModeWork) || eng.executeCollaboration(ctx, "root", "", store.ModeWork, call).Ok {
		t.Fatal("disabled collaboration app still exposes list")
	}
	apps.enabled.Store(true)
	ids := []string{}
	if _, err := st.UpdateSession(ctx, "root", store.SessionUpdate{LoadedAppIDs: &ids}); err != nil {
		t.Fatal(err)
	}
	if hasList("root", store.ModeWork) || eng.executeCollaboration(ctx, "root", "", store.ModeWork, call).Ok {
		t.Fatal("unloaded collaboration app still exposes list")
	}
}

func TestCollaborationListThenSendReusesChildWithoutRequiringReuse(t *testing.T) {
	ctx := context.Background()
	eng, st, _ := newCollaborationEngine(t, mock.New(mock.WithScript([]string{"上海晴朗。"})))
	if err := st.CreateChildSession(ctx, "root", &store.Session{ID: "weather", Title: "北京天气", Provider: "mock", Model: "model", ActiveMode: store.ModeWork, ModeLease: store.ModeLeaseSession}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "weather", TurnID: "old", ClientMessageID: "old", UserMessageID: "old", UserText: "查询北京天气。"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "old", Status: store.TurnCompleted, AssistantParts: []store.ContentPart{{Type: store.ContentPartText, Text: "北京晴朗。"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "root", TurnID: "parent_turn", ClientMessageID: "root", UserMessageID: "root", UserText: "继续查询天气"}); err != nil {
		t.Fatal(err)
	}
	listed := readCollaborationList(t, eng)
	args, _ := json.Marshal(map[string]string{"session_id": listed[0].SessionID, "prompt": "新任务：查询上海天气，沿用格式。"})
	result := eng.executeAllowedTool(ctx, "root", "parent_turn", store.ModeWork, tool.Call{Name: tool.CollaborationSend, CallID: "reuse", Args: args})
	if !result.Ok {
		t.Fatalf("send from list failed: %s", result.Content)
	}
	eng.Wait()
	items := readCollaborationList(t, eng)
	if len(items) != 1 || items[0].SessionID != "weather" || items[0].TaskTitle != "查询上海天气" || items[0].Status != "completed" || items[0].Summary != "上海晴朗" {
		t.Fatalf("reuse created another child or retained old task data: %+v", items)
	}
	messages, err := st.ListMessages(ctx, "weather", 0)
	if err != nil || len(messages) != 4 {
		t.Fatalf("reuse did not retain both turns: %d messages, %v", len(messages), err)
	}
	result = eng.executeAllowedTool(ctx, "root", "parent_turn", store.ModeWork, tool.Call{Name: tool.CollaborationDispatch, CallID: "new", Args: json.RawMessage(`{"title":"独立研究","prompt":"研究新的主题"}`)})
	if !result.Ok {
		t.Fatalf("idle child must not prevent a deliberate new dispatch: %s", result.Content)
	}
	eng.Wait()
	if got := readCollaborationList(t, eng); len(got) != 2 {
		t.Fatalf("new dispatch was forced to reuse: %+v", got)
	}
}
