package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

func TestHeadlessScreenshotSessionApproval(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	root := t.TempDir()
	chrome := filepath.Join(t.TempDir(), "Google Chrome.app", "Contents", "MacOS", "Google Chrome")
	if err := os.MkdirAll(filepath.Dir(chrome), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chrome, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "page.html"), []byte("<h1>test</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"project", "other-project"} {
		if err := ms.CreateProject(ctx, &store.Project{ID: id, RootDirs: []string{root}, ApprovalMode: store.ApprovalAuto}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"session", "other-session"} {
		if err := ms.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock", ActiveMode: store.ModeCode, ProjectID: "project"}); err != nil {
			t.Fatal(err)
		}
	}
	hub := event.NewHub()
	calls := &recordingToolRunner{defs: tool.BuiltinDefinitions(), result: tool.Result{Ok: true, Content: `{"ok":true}`}}
	runner := &approvalDetailsRecordingToolRunner{recordingToolRunner: calls}
	eng := New(ms, hub, registry.Static(mock.New()), ms, WithTools(runner))
	defer eng.Stop()
	base := "'" + chrome + "' --headless=new --user-data-dir=.preview-profile --screenshot=one.png --window-size=800,600 page.html"
	execute := func(name, sessionID, command string, wantApproval bool, scope ApprovalScope, deny bool) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			sub, unsub := hub.Subscribe(sessionID)
			defer unsub()
			runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			done := make(chan tool.Result, 1)
			raw, _ := json.Marshal(map[string]any{"scope": "project", "execution": "host", "host_access_reason": "headless rendering", "command": command})
			go func() {
				done <- eng.executeAllowedTool(runCtx, sessionID, "turn_"+name, store.ModeCode, tool.Call{SessionID: sessionID, CallID: "call_" + name, Name: tool.CommandRun, Args: raw})
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
						t.Fatalf("unexpected repeated approval: %s", ev.Payload)
					}
					var payload map[string]any
					_ = json.Unmarshal(ev.Payload, &payload)
					if payload["sessionGrant"] == nil {
						t.Fatalf("missing bounded session option: %s", ev.Payload)
					}
					var err error
					if deny {
						err = eng.DenyApproval(ctx, sessionID, ev.ApprovalID, "denied")
					} else {
						err = eng.ApproveApproval(ctx, sessionID, ev.ApprovalID, scope, nil)
					}
					if err != nil {
						t.Fatal(err)
					}
				case result := <-done:
					if result.Ok == deny || (approvals > 0) != wantApproval {
						t.Fatalf("approvals=%d result=%+v", approvals, result)
					}
					if !deny && calls.calls[len(calls.calls)-1].CommandSandbox != tool.CommandSandboxBypass {
						t.Fatal("approved host command did not use the approved execution boundary")
					}
					return
				case <-runCtx.Done():
					t.Fatal("command/approval did not finish")
				}
			}
		})
	}
	execute("allow_once", "session", base, true, ApprovalScopeTurn, false)
	execute("once_not_remembered", "session", base, true, ApprovalScopeTurn, true)
	execute("deny_not_remembered", "session", base, true, ApprovalScopeSession, false)
	for _, name := range []string{"two.png", "three.png", "four.png"} {
		command := strings.ReplaceAll(strings.ReplaceAll(base, "one.png", name), "800,600", "1200,900")
		execute("repeat_"+name, "session", command, false, ApprovalScopeTurn, false)
	}
	execute("other_session", "other-session", base, true, ApprovalScopeTurn, false)
	execute("other_profile", "session", strings.ReplaceAll(base, ".preview-profile", ".other-profile"), true, ApprovalScopeTurn, false)
	projectID := "other-project"
	if _, err := ms.UpdateSession(ctx, "session", store.SessionUpdate{ProjectID: &projectID}); err != nil {
		t.Fatal(err)
	}
	execute("other_project", "session", base, true, ApprovalScopeTurn, false)
	eng.ReleaseSessionResources("session")
	projectID = "project"
	if _, err := ms.UpdateSession(ctx, "session", store.SessionUpdate{ProjectID: &projectID}); err != nil {
		t.Fatal(err)
	}
	execute("released", "session", base, true, ApprovalScopeSession, false)
	eng.Stop()
	eng = New(ms, hub, registry.Static(mock.New()), ms, WithTools(runner))
	defer eng.Stop()
	execute("restart", "session", base, true, ApprovalScopeTurn, false)
}
