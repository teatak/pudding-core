package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestSandboxSessionCommandApprovals(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ms := memstore.New()
	for _, id := range []string{"a", "b"} {
		if err := ms.CreateProject(ctx, &store.Project{ID: id, RootDirs: []string{root}, ApprovalMode: store.ApprovalAuto}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"one", "two"} {
		if err := ms.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock", ActiveMode: store.ModeCode, ProjectID: "a"}); err != nil {
			t.Fatal(err)
		}
	}
	hub := event.NewHub()
	calls := &recordingToolRunner{defs: tool.BuiltinDefinitions(), result: tool.Result{Ok: true, Content: `{"ok":true}`}}
	eng := New(ms, hub, registry.Static(mock.New()), ms, WithTools(&approvalDetailsRecordingToolRunner{recordingToolRunner: calls}))
	t.Cleanup(func() { eng.Stop() })
	execute := func(session, command string, wantApproval bool, scope ApprovalScope, beforeApprove func(), wantOK bool) {
		t.Helper()
		sub, unsub := hub.Subscribe(session)
		defer unsub()
		runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		raw, _ := json.Marshal(map[string]any{"scope": "project", "command": command, "env": map[string]string{"PYTHONPATH": root}})
		done := make(chan tool.Result, 1)
		go func() {
			done <- eng.executeAllowedTool(runCtx, session, "turn", store.ModeCode, tool.Call{SessionID: session, Name: tool.CommandRun, CallID: "call", Args: raw})
		}()
		approvals := 0
		for {
			select {
			case ev := <-sub:
				if ev.Kind != event.ApprovalRequested {
					continue
				}
				approvals++
				if !wantApproval || approvals > 1 {
					t.Fatalf("unexpected approval: %s", ev.Payload)
				}
				if !strings.Contains(string(ev.Payload), "sandbox_command") || !strings.Contains(string(ev.Payload), "custom_environment") {
					t.Fatalf("missing scope/reasons: %s", ev.Payload)
				}
				if beforeApprove != nil {
					beforeApprove()
				}
				if err := eng.ApproveApproval(ctx, session, ev.ApprovalID, scope, nil); err != nil {
					t.Fatal(err)
				}
			case result := <-done:
				if result.Ok != wantOK || (approvals > 0) != wantApproval {
					t.Fatalf("result=%+v approvals=%d", result, approvals)
				}
				if wantOK && calls.calls[len(calls.calls)-1].CommandSandbox != tool.CommandSandboxEnforce {
					t.Fatal("sandbox lease escaped to host")
				}
				if !wantOK && !strings.Contains(result.Content, "approval_context_changed") {
					t.Fatalf("missing changed context: %s", result.Content)
				}
				return
			case <-runCtx.Done():
				t.Fatal("command did not finish")
			}
		}
	}
	execute("one", "cat file.txt", true, ApprovalScopeTurn, nil, true)
	if eng.CommandApprovals("one").GrantCount != 0 {
		t.Fatal("once approval remembered")
	}
	execute("one", "cat file.txt", true, ApprovalScopeSession, nil, true)
	execute("one", "cat file.txt", false, ApprovalScopeTurn, nil, true)
	status := eng.CommandApprovals("one")
	if status.GrantCount != 1 || status.ReusedCount != 1 || status.ApprovalReasons["custom_environment"] != 2 {
		t.Fatalf("status=%+v", status)
	}
	status.ApprovalReasons["custom_environment"] = 999
	if eng.CommandApprovals("one").ApprovalReasons["custom_environment"] == 999 {
		t.Fatal("status exposed mutable authority")
	}
	execute("two", "cat file.txt", true, ApprovalScopeTurn, nil, true)
	execute("one", "cat changed.txt", true, ApprovalScopeTurn, nil, true)
	eng.RevokeCommandApprovals("one")
	execute("one", "cat file.txt", true, ApprovalScopeSession, nil, true)
	execute("one", "cat changed.txt", true, ApprovalScopeSession, func() { eng.RevokeCommandApprovals("one") }, false)
	if eng.CommandApprovals("one").GrantCount != 0 {
		t.Fatal("pending approval resurrected revoked grant")
	}
	execute("one", "cat file.txt", true, ApprovalScopeSession, nil, true)
	// A project transition must invalidate prior grants even if switched back
	// before another command is issued (the API calls these invalidators).
	for _, project := range []string{"b", "a"} {
		if _, err := ms.UpdateSession(ctx, "one", store.SessionUpdate{ProjectID: &project}); err != nil {
			t.Fatal(err)
		}
		eng.RevokeCommandApprovals("one")
	}
	execute("one", "cat file.txt", true, ApprovalScopeSession, nil, true)
	eng.RevokeProjectCommandApprovals("a")
	execute("one", "cat file.txt", true, ApprovalScopeSession, nil, true)
	// A store mutation while the approval is on screen also fails closed.
	execute("one", "cat changed.txt", true, ApprovalScopeSession, func() {
		project := "b"
		_, err := ms.UpdateSession(ctx, "one", store.SessionUpdate{ProjectID: &project})
		if err != nil {
			t.Fatal(err)
		}
	}, false)
	execute("one", "cat file.txt", true, ApprovalScopeSession, nil, true)
	eng.Stop()
	eng = New(ms, hub, registry.Static(mock.New()), ms, WithTools(&approvalDetailsRecordingToolRunner{recordingToolRunner: calls}))
	execute("one", "cat file.txt", true, ApprovalScopeSession, nil, true)
}
