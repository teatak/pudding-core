package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestTemporaryDirectoryApprovalReuseIsolationAndRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ms := storetest.New(t)
	root, extra := t.TempDir(), t.TempDir()
	extra, _ = filepath.EvalSymlinks(extra)
	if err := ms.CreateProject(ctx, &store.Project{ID: "p", RootDirs: []string{root}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		if err := ms.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock", ProjectID: "p", ActiveMode: store.ModeCode}); err != nil {
			t.Fatal(err)
		}
	}
	hub := event.NewHub()
	eng := New(ms, hub, registry.Static(mock.New()), ms)
	events, unsub := hub.Subscribe("one")
	defer unsub()
	request := func(dir string) tool.Result {
		raw, _ := json.Marshal(map[string]any{"targetMode": "code", "reason": "read input", "projectDirs": []string{dir}})
		result, _, _ := eng.requestCapabilityApproval(ctx, "one", "turn", tool.Call{Name: tool.RequestCapability, CallID: "cap", Args: raw}, store.ModeCode)
		return result
	}
	done := make(chan tool.Result, 1)
	go func() { done <- request(extra) }()
	var ev event.Event
	select {
	case ev = <-events:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := eng.ApproveApproval(ctx, "one", ev.ApprovalID, ApprovalScopeSession, nil); err != nil {
		t.Fatal(err)
	}
	if result := <-done; !result.Ok {
		t.Fatal(result)
	}
	project, _ := ms.GetProject(ctx, "p")
	if len(project.RootDirs) != 1 || project.RootDirs[0] != root {
		t.Fatalf("shared project mutated: %+v", project)
	}
	dirs, _ := eng.projectRootDirsForToolCall(ctx, "one", "next", store.ModeCode)
	if !strings.Contains(strings.Join(dirs, "\n"), extra) {
		t.Fatalf("session lease lost: %v", dirs)
	}
	dirs, _ = eng.projectRootDirsForToolCall(ctx, "two", "next", store.ModeCode)
	if strings.Contains(strings.Join(dirs, "\n"), extra) {
		t.Fatal("lease leaked to another session")
	}
	subdir := filepath.Join(extra, "nested")
	if err := os.Mkdir(subdir, 0700); err != nil {
		t.Fatal(err)
	}
	if result := request(subdir); !result.Ok || !strings.Contains(result.Content, "already_available") {
		t.Fatalf("repeated approval: %+v", result)
	}
	eng.RevokeCommandApprovals("one")
	dirs, _ = eng.projectRootDirsForToolCall(ctx, "one", "next", store.ModeCode)
	if strings.Contains(strings.Join(dirs, "\n"), extra) {
		t.Fatal("revoked lease survived")
	}
	go func() { done <- request(extra) }()
	for {
		select {
		case ev = <-events:
			if ev.Kind == event.ApprovalRequested {
				goto pending
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
pending:
	eng.RevokeCommandApprovals("one")
	if err := eng.ApproveApproval(ctx, "one", ev.ApprovalID, ApprovalScopeSession, nil); err == nil {
		t.Fatal("revoked pending grant accepted")
	}

	if result := <-done; result.Ok || !strings.Contains(result.Content, "approval_context_changed") {
		t.Fatalf("stale approval did not release waiting turn: %+v", result)
	}
}

func TestCommandDirectoryAndRiskUseOneApproval(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX project sandbox; Windows host approval is tested separately")
	}

	for _, action := range []string{"approve", "approve-session", "deny", "revoke", "replace-directory"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			ms := storetest.New(t)
			hub := event.NewHub()
			root, extra := t.TempDir(), t.TempDir()
			root, _ = filepath.EvalSymlinks(root)
			extra, _ = filepath.EvalSymlinks(extra)
			if err := ms.CreateProject(ctx, &store.Project{ID: "p", RootDirs: []string{root}, ApprovalMode: store.ApprovalAsk}); err != nil {
				t.Fatal(err)
			}
			if err := ms.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock", ProjectID: "p", ActiveMode: store.ModeCode}); err != nil {
				t.Fatal(err)
			}
			calls := &recordingToolRunner{defs: tool.BuiltinDefinitions(), result: tool.Result{Ok: true}}
			eng := New(ms, hub, registry.Static(mock.New()), ms, WithTools(&approvalDetailsRecordingToolRunner{recordingToolRunner: calls}))
			t.Cleanup(eng.Stop)
			events, unsub := hub.Subscribe("s")
			defer unsub()
			command := "cat " + filepath.Join(extra, "input.txt")
			raw, _ := json.Marshal(map[string]any{"scope": "project", "command": command})
			done := make(chan tool.Result, 1)
			go func() {
				done <- eng.executeAllowedTool(ctx, "s", "turn", store.ModeCode, tool.Call{SessionID: "s", TurnID: "turn", Name: tool.CommandRun, CallID: "cmd", Args: raw})
			}()
			var ev event.Event
			select {
			case ev = <-events:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var details struct {
				AdditionalProjectDirs []string                  `json:"additionalProjectDirs"`
				DirectoryScope        string                    `json:"directoryScope"`
				SessionGrant          *tool.CommandSessionGrant `json:"sessionGrant"`
			}
			if err := json.Unmarshal(ev.Payload, &details); err != nil {
				t.Fatal(err)
			}
			if ev.ApprovalKind != ApprovalKindToolCall || len(details.AdditionalProjectDirs) != 1 || details.AdditionalProjectDirs[0] != extra || details.DirectoryScope != "call" {
				t.Fatalf("missing combined approval: %s", ev.Payload)
			}
			if details.SessionGrant == nil || details.SessionGrant.Kind != "sandbox_command" {
				t.Fatalf("combined approval lost session reuse: %s", ev.Payload)
			}
			switch action {
			case "revoke":
				eng.RevokeCommandApprovals("s")
			case "replace-directory":
				if err := os.Rename(extra, extra+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(extra, 0700); err != nil {
					t.Fatal(err)
				}
				defer os.RemoveAll(extra + "-old")
			}
			var err error
			if action == "deny" {
				err = eng.DenyApproval(ctx, "s", ev.ApprovalID, "no")
			} else {
				scope := ApprovalScopeSession
				if action == "approve" {
					scope = ApprovalScopeTurn
				}
				err = eng.ApproveApproval(ctx, "s", ev.ApprovalID, scope, []string{"/"})
			}
			if err != nil {
				t.Fatal(err)
			}
			result := <-done
			if action != "approve" && action != "approve-session" {
				if result.Ok || len(calls.calls) != 0 {
					t.Fatalf("unauthorized dispatch: %+v", result)
				}
				if eng.CommandApprovals("s").GrantCount != 0 {
					t.Fatal("invalid approval created a grant")
				}
				return
			}
			if !result.Ok || len(calls.calls) != 1 {
				t.Fatalf("dispatch=%+v calls=%v", result, calls.calls)
			}
			call := calls.calls[0]
			var args map[string]any
			_ = json.Unmarshal(call.Args, &args)
			if call.CommandSandbox != tool.CommandSandboxEnforce || args["command"] != command || args["cwd"] != root || len(call.ProjectDirs) != 2 || call.CommandGrant == nil {
				t.Fatalf("execution changed: %+v", call)
			}
			dirs, _ := eng.projectRootDirsForToolCall(ctx, "s", "next", store.ModeCode)
			if len(dirs) != 1 || dirs[0] != root {
				t.Fatalf("one-call permission leaked: %v", dirs)
			}
			if action == "approve" {
				if eng.CommandApprovals("s").GrantCount != 0 {
					t.Fatal("one-call approval became reusable")
				}
				return
			}
			for i := 0; i < 4; i++ {
				if err := os.WriteFile(filepath.Join(extra, "input.txt"), []byte(fmt.Sprint(i)), 0600); err != nil {
					t.Fatal(err)
				}
				result := eng.executeAllowedTool(ctx, "s", "next", store.ModeCode, tool.Call{SessionID: "s", Name: tool.CommandRun, CallID: "repeat", Args: raw})
				if !result.Ok {
					t.Fatalf("repeat %d requested another approval: %+v", i, result)
				}
			}
			if status := eng.CommandApprovals("s"); status.GrantCount != 1 || status.ReusedCount != 0 || status.DiagnosticsEnabled || len(status.ProjectDirs) != 0 {
				t.Fatalf("unexpected combined grant scope: %+v", status)
			}
		})
	}
}

func TestTemporaryDirectoriesRefreshInProviderContext(t *testing.T) {
	for _, scope := range []ApprovalScope{ApprovalScopeTurn, ApprovalScopeSession} {
		t.Run(string(scope), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ms := storetest.New(t)
			root, extra := t.TempDir(), t.TempDir()
			extra, _ = filepath.EvalSymlinks(extra)
			if err := ms.CreateProject(ctx, &store.Project{ID: "p", RootDirs: []string{root}}); err != nil {
				t.Fatal(err)
			}
			if err := ms.CreateSession(ctx, &store.Session{ID: "s", Title: "Directory context", Provider: "mock", Model: "mock", ProjectID: "p", ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession}); err != nil {
				t.Fatal(err)
			}
			hub := event.NewHub()
			var eng *Engine
			var requests []provider.Request
			client := &backgroundEngineClient{step: func(ctx context.Context, req provider.Request, step int) (<-chan provider.Chunk, error) {
				requests = append(requests, req)
				want := step == 2 || step == 3 && scope == ApprovalScopeSession
				// Directory access is JSON in the prompt; Windows backslashes are escaped.
				encodedExtra, _ := json.Marshal(extra)
				visible := strings.Contains(req.System, string(encodedExtra))
				if visible != want {
					t.Errorf("step %d: temporary directory visible=%v, want=%v", step, visible, want)
					return nil, fmt.Errorf("step %d: temporary directory visible=%v, want=%v", step, visible, want)
				}
				switch step {
				case 1:
					raw, _ := json.Marshal(map[string]any{"targetMode": "code", "projectDirs": []string{extra}})
					return smokeToolStream("cap", tool.RequestCapability, string(raw)), nil
				case 2:
					return smokeTextStream("approved"), nil
				case 3:
					eng.RevokeCommandApprovals("s")
					return smokeToolStream("time", tool.TimeGetCurrent, `{}`), nil
				default:
					return smokeTextStream("revoked"), nil
				}
			}}
			eng = New(ms, hub, registry.Static(client), ms, WithTools(tool.NewBuiltinRunner()))
			t.Cleanup(eng.Stop)
			if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{DisplayName: "mock", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "mock"}}}); err != nil {
				t.Fatal(err)
			}
			events, unsub := hub.Subscribe("s")
			defer unsub()
			if _, err := eng.Submit(ctx, SubmitInput{SessionID: "s", ClientMessageID: "first", Text: "authorize directory"}); err != nil {
				t.Fatal(err)
			}
			for {
				select {
				case ev := <-events:
					if ev.Kind == event.TurnFailed {
						t.Fatalf("turn failed before approval: %+v", ev)
					}
					if ev.Kind != event.ApprovalRequested {
						continue
					}
					if err := eng.ApproveApproval(ctx, "s", ev.ApprovalID, scope, nil); err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				break
			}
			waitTurnDone(t, ms, "s")
			if len(requests) != 2 {
				t.Fatalf("first turn requests=%d", len(requests))
			}
			if _, err := eng.Submit(ctx, SubmitInput{SessionID: "s", ClientMessageID: "second", Text: "check lifetime"}); err != nil {
				t.Fatal(err)
			}
			waitTurnDone(t, ms, "s")
			if len(requests) != 4 {
				t.Fatalf("second turn requests=%d", len(requests))
			}
			project, _ := ms.GetProject(ctx, "p")
			if len(project.RootDirs) != 1 {
				t.Fatal("temporary access persisted in project")
			}
		})
	}
}

func TestCombinedDirectoryGrantInvalidation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sandbox grants")
	}
	for _, change := range []string{"once", "arguments", "directory-replaced", "revoke", "project", "release", "restart"} {
		t.Run(change, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ms := storetest.New(t)
			root, extra := t.TempDir(), t.TempDir()
			extra, _ = filepath.EvalSymlinks(extra)
			for _, id := range []string{"p", "other"} {
				if err := ms.CreateProject(ctx, &store.Project{ID: id, RootDirs: []string{root}, ApprovalMode: store.ApprovalAuto}); err != nil {
					t.Fatal(err)
				}
			}
			if err := ms.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock", ProjectID: "p", ActiveMode: store.ModeCode}); err != nil {
				t.Fatal(err)
			}
			hub := event.NewHub()
			calls := &recordingToolRunner{defs: tool.BuiltinDefinitions(), result: tool.Result{Ok: true}}
			newEngine := func() *Engine {
				return New(ms, hub, registry.Static(mock.New()), ms, WithTools(&approvalDetailsRecordingToolRunner{recordingToolRunner: calls}))
			}
			eng := newEngine()
			t.Cleanup(func() { eng.Stop() })
			events, unsub := hub.Subscribe("s")
			defer unsub()
			command := "cat " + filepath.Join(extra, "input.txt")
			for attempt := 0; attempt < 2; attempt++ {
				raw, _ := json.Marshal(map[string]any{"scope": "project", "command": command})
				done := make(chan tool.Result, 1)
				go func() {
					done <- eng.executeAllowedTool(ctx, "s", "turn", store.ModeCode, tool.Call{Name: tool.CommandRun, SessionID: "s", CallID: "call", Args: raw})
				}()
				approved := false
				for !approved {
					select {
					case ev := <-events:
						if ev.Kind != event.ApprovalRequested {
							continue
						}
						var err error
						if attempt == 1 {
							err = eng.DenyApproval(ctx, "s", ev.ApprovalID, "test boundary")
						} else {
							scope := ApprovalScopeSession
							if change == "once" {
								scope = ApprovalScopeTurn
							}
							err = eng.ApproveApproval(ctx, "s", ev.ApprovalID, scope, []string{"/"})
						}
						if err != nil {
							t.Fatal(err)
						}
						approved = true
					case result := <-done:
						t.Fatalf("executed without fresh approval: %+v", result)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				result := <-done
				if result.Ok != (attempt == 0) {
					t.Fatal(result)
				}
				if attempt == 1 {
					break
				}
				switch change {
				case "arguments":
					command = "cat " + filepath.Join(extra, "other.txt")
				case "directory-replaced":
					if err := os.Rename(extra, extra+"-old"); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { os.RemoveAll(extra + "-old") })
					if err := os.Mkdir(extra, 0700); err != nil {
						t.Fatal(err)
					}
				case "revoke":
					eng.RevokeCommandApprovals("s")
				case "project":
					id := "other"
					if _, err := ms.UpdateSession(ctx, "s", store.SessionUpdate{ProjectID: &id}); err != nil {
						t.Fatal(err)
					}
				case "release":
					eng.ReleaseSessionResources("s")
				case "restart":
					eng.Stop()
					eng = newEngine()
				}
			}
			if len(calls.calls) != 1 {
				t.Fatalf("unapproved dispatch: %d", len(calls.calls))
			}
		})
	}
}
