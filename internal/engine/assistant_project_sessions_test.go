package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func assistantProjectSessions(t *testing.T, ctx context.Context, eng *Engine, sid, projectID, query string) []store.Session {
	t.Helper()
	result, err := eng.CallTool(ctx, sid, store.ModeChat, tool.AssistantProject, mustJSON(map[string]string{
		"action": "sessions", "project_id": projectID, "query": query,
	}))
	var listed struct {
		ProjectID string          `json:"projectID"`
		Sessions  []store.Session `json:"sessions"`
	}
	if err != nil || !result.Ok || json.Unmarshal([]byte(result.Content), &listed) != nil || listed.ProjectID != projectID || listed.Sessions == nil {
		t.Fatalf("list project sessions: %+v, %v", result, err)
	}
	var fields struct {
		Sessions []map[string]json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(result.Content), &fields); err != nil {
		t.Fatal(err)
	}
	for _, item := range fields.Sessions {
		if len(item) != 6 {
			t.Fatalf("session discovery must return only its six metadata fields: %v", item)
		}
		for _, key := range []string{"id", "title", "projectID", "activeMode", "running", "lastActivityAt"} {
			if _, ok := item[key]; !ok {
				t.Fatalf("missing session metadata field %s", key)
			}
		}
	}
	return listed.Sessions
}

func TestAssistantProjectSessionsFiltersAndMetadata(t *testing.T) {
	ctx := context.Background()
	eng, db, sid := newAssistantProjectEngine(t)
	for _, id := range []string{"project", "other", "empty"} {
		if err := db.CreateProject(ctx, &store.Project{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct{ id, projectID string }{
		{"manual", "project"}, {"second", "project"}, {"archived", "project"}, {"other-session", "other"}, {"unbound", ""},
	} {
		if err := db.CreateSession(ctx, &store.Session{ID: item.id, Title: "Review existing work", ProjectID: item.projectID,
			Provider: "configured", Model: "configured-model", ActiveMode: store.ModeWork, ModeLease: store.ModeLeaseSession}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ArchiveSession(ctx, "archived"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: "manual", TurnID: "manual-turn", UserMessageID: "manual-message", ClientMessageID: "manual-input", UserText: "Private existing work", Mode: store.ModeWork}); err != nil {
		t.Fatal(err)
	}
	child, err := db.DispatchChild(ctx, store.DispatchChildInput{
		ParentSessionID: "manual", ParentTurnID: "manual-turn", CallID: "child-call",
		Child: &store.Session{ID: "child", Title: "Review existing work", Provider: "configured", Model: "configured-model"},
		Input: store.QueueInputInput{SessionID: "child", ClientMessageID: "child-input", Text: "Child work", Provider: "configured", Model: "configured-model", Mode: store.ModeWork},
	})
	if err != nil || child.Session.ProjectID != "project" {
		t.Fatalf("create project child fixture: %+v, %v", child, err)
	}
	listed := assistantProjectSessions(t, ctx, eng, sid, "project", "")
	if len(listed) != 2 {
		t.Fatalf("expected only the two ordinary active project sessions: %+v", listed)
	}
	for i, session := range listed {
		if session.ID != "manual" && session.ID != "second" {
			t.Fatalf("unexpected session: %+v", session)
		}
		stored, err := db.GetSession(ctx, session.ID)
		if err != nil || session.ProjectID != "project" || session.Title != stored.Title || session.ActiveMode != stored.ActiveMode || session.Running != stored.Running || !session.LastActivityAt.Equal(stored.LastActivityAt) {
			t.Fatalf("incorrect session metadata: %+v, %v", session, err)
		}
		if i > 0 && session.LastActivityAt.After(listed[i-1].LastActivityAt) {
			t.Fatal("session discovery lost activity ordering")
		}
	}
	for _, tc := range []struct {
		project, query string
		count          int
	}{
		{"project", "REVIEW", 2}, {"project", "manual", 1}, {"project", "absent", 0},
		{"other", "", 1}, {"empty", "", 0},
	} {
		if got := assistantProjectSessions(t, ctx, eng, sid, tc.project, tc.query); len(got) != tc.count {
			t.Fatalf("query %q in %q returned %d sessions, want %d", tc.query, tc.project, len(got), tc.count)
		}
	}
	for _, args := range []string{
		`{"action":"sessions"}`, `{"action":"sessions","project_id":" "}`,
		`{"action":"sessions","project_id":"missing"}`, `{"action":"sessions","project_id":"project","name":"unused"}`,
		`{"action":"list","project_id":"project"}`, `{"action":"create","project_id":"project","name":"unused"}`,
	} {
		result, err := eng.CallTool(ctx, sid, store.ModeChat, tool.AssistantProject, json.RawMessage(args))
		if err != nil || result.Ok || len(eng.PendingApprovals(sid)) != 0 {
			t.Fatalf("invalid discovery arguments succeeded or reached approval: %+v, %v", result, err)
		}
	}
	result := eng.executeAssistantTask(ctx, "second", "", tool.Call{Name: tool.AssistantProject, Args: mustJSON(map[string]string{"action": "sessions", "project_id": "project"})})
	if result.Ok {
		t.Fatal("ordinary session bypassed assistant-only discovery")
	}
	if tasks, err := db.ListAssistantTaskRuns(ctx, sid); err != nil || len(tasks) != 0 {
		t.Fatalf("read-only discovery dispatched work: %v, %v", tasks, err)
	}
}

func TestAssistantProjectSessionsReuseRequiresApproval(t *testing.T) {
	for _, approve := range []bool{true, false} {
		name := "deny"
		if approve {
			name = "approve"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			eng, db, sid := newAssistantProjectEngine(t)
			project := &store.Project{ID: "project", Name: "pudding-core", ApprovalMode: store.ApprovalFull}
			if err := db.CreateProject(ctx, project); err != nil {
				t.Fatal(err)
			}
			worker := &store.Session{ID: "manual-worker", Title: "Existing review", ProjectID: project.ID, Provider: "configured", Model: "configured-model", ActiveMode: store.ModeWork, ModeLease: store.ModeLeaseSession}
			if err := db.CreateSession(ctx, worker); err != nil {
				t.Fatal(err)
			}
			listed := assistantProjectSessions(t, ctx, eng, sid, project.ID, "Existing review")
			if len(listed) != 1 || listed[0].ID != worker.ID {
				t.Fatalf("manual conversation not discovered: %+v", listed)
			}
			type outcome struct {
				result tool.Result
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := eng.CallTool(ctx, sid, store.ModeChat, tool.AssistantTaskDispatch, mustJSON(map[string]string{
					"title": "Continue review", "prompt": "Return a brief acknowledgement.", "session_id": listed[0].ID,
				}))
				done <- outcome{result, err}
			}()
			approval := waitAssistantTaskApproval(t, eng, sid)
			var details struct{ TargetSessionID string }
			if json.Unmarshal(approval.Payload, &details) != nil || details.TargetSessionID != worker.ID {
				t.Fatalf("approval is not for the discovered session: %s", approval.Payload)
			}
			if tasks, err := db.ListAssistantTaskRuns(ctx, sid); err != nil || len(tasks) != 0 {
				t.Fatalf("discovery bypassed dispatch approval: %v, %v", tasks, err)
			}
			var err error
			if approve {
				err = eng.ApproveApproval(ctx, sid, approval.ID, ApprovalScopeTurn, nil)
			} else {
				err = eng.DenyApproval(ctx, sid, approval.ID, "Do not send this task")
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case out := <-done:
				if out.err != nil || out.result.Ok != approve {
					t.Fatalf("dispatch outcome: %+v, %v", out.result, out.err)
				}
				if approve {
					var task store.AssistantTaskRun
					if json.Unmarshal([]byte(out.result.Content), &task) != nil || task.SessionID != worker.ID {
						t.Fatalf("dispatch did not reuse discovered session: %s", out.result.Content)
					}
					waitTurnDone(t, db, worker.ID)
				} else if !strings.Contains(out.result.Content, `"denied"`) {
					t.Fatalf("missing denial: %s", out.result.Content)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			sessions, err := db.ListSessions(ctx)
			if err != nil || len(sessions) != 1 || sessions[0].ID != worker.ID || sessions[0].ActiveMode != store.ModeWork || sessions[0].ModeLease != store.ModeLeaseSession {
				t.Fatalf("reuse created or upgraded a session: %+v, %v", sessions, err)
			}
			stored, err := db.GetProject(ctx, project.ID)
			if err != nil || stored.ApprovalMode != store.ApprovalFull {
				t.Fatalf("reuse changed project policy: %+v, %v", stored, err)
			}
		})
	}
}
