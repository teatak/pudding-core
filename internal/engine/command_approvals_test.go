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

func TestSandboxAutonomyApprovalModes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ms := memstore.New()
	eng := New(ms, event.NewHub(), registry.Static(mock.New()), ms)
	t.Cleanup(eng.Stop)
	for _, mode := range []store.ApprovalMode{store.ApprovalAuto, store.ApprovalAsk, store.ApprovalFull} {
		id := string(mode)
		if err := ms.CreateProject(ctx, &store.Project{ID: id, RootDirs: []string{root}, ApprovalMode: mode}); err != nil {
			t.Fatal(err)
		}
		if err := ms.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock", ProjectID: id}); err != nil {
			t.Fatal(err)
		}
		for _, command := range []string{
			`git ls-tree -r -l HEAD | awk '$4 > 200000 {print $5}'`,
			`echo "count: $(git ls-files | wc -l)"`,
			`for d in . docs; do ls "$d"; done`,
			`python3 -c 'print(1)'; for d in . docs; do ls "$d"; done`,
			`for d in $(ls); do git -C "$d" status; done`,
			`f() { printf '%s' "$1"; }; f ok`,
			`n=$(git ls-files | wc -l); echo "$n"`,
		} {
			raw, _ := json.Marshal(map[string]any{"scope": "project", "cwd": root, "command": command})
			risk, ok := tool.ClassifyToolCallForProject(tool.CommandRun, raw, []string{root})
			if !ok || !risk.LowRisk {
				t.Fatalf("project code not eligible for sandbox Auto: %+v", risk)
			}
			_, required, err := eng.toolCallApprovalRequired(ctx, id, risk, nil)
			if err != nil || required != (mode == store.ApprovalAsk) {
				t.Fatalf("mode=%s command=%s required=%v err=%v", mode, command, required, err)
			}
		}
	}
}

func TestAutoDynamicCommandDispatchKeepsSandboxAndScope(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ms := memstore.New()
	if err := ms.CreateProject(ctx, &store.Project{ID: "project", RootDirs: []string{root}, ApprovalMode: store.ApprovalAuto}); err != nil {
		t.Fatal(err)
	}
	if err := ms.CreateSession(ctx, &store.Session{ID: "session", Provider: "mock", Model: "mock", ProjectID: "project"}); err != nil {
		t.Fatal(err)
	}
	hub := event.NewHub()
	events, unsubscribe := hub.Subscribe("session")
	defer unsubscribe()
	calls := &recordingToolRunner{defs: tool.BuiltinDefinitions(), result: tool.Result{Ok: true, Content: `{"ok":true}`}}
	eng := New(ms, hub, registry.Static(mock.New()), ms, WithTools(&approvalDetailsRecordingToolRunner{recordingToolRunner: calls}))
	t.Cleanup(eng.Stop)
	for _, background := range []bool{false, true} {
		raw, _ := json.Marshal(map[string]any{"scope": "project", "command": `for f in *; do printf '%s' "$f"; done`, "background": background})
		runCtx, cancel := context.WithTimeout(ctx, time.Second)
		result := eng.executeAllowedTool(runCtx, "session", "turn", store.ModeCode, tool.Call{Name: tool.CommandRun, CallID: "call", SessionID: "session", Args: raw})
		cancel()
		if !result.Ok {
			t.Fatalf("unexpected approval or dispatch failure: %+v", result)
		}
		call := calls.calls[len(calls.calls)-1]
		if call.CommandSandbox != tool.CommandSandboxEnforce || call.SessionID != "session" || string(call.Args) != string(raw) || len(call.ProjectDirs) != 1 || call.ProjectDirs[0] != root || call.CommandGrant != nil {
			t.Fatalf("autonomy changed execution authority: %+v", call)
		}
	}
	for {
		select {
		case ev := <-events:
			if ev.Kind == event.ApprovalRequested {
				t.Fatal("syntax generated an approval")
			}
		default:
			if status := eng.CommandApprovals("session"); status.GrantCount != 0 || status.ReusedCount != 0 || len(status.ApprovalReasons) != 0 {
				t.Fatalf("autonomy created an implicit reusable grant: %+v", status)
			}
			return
		}
	}
}

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
