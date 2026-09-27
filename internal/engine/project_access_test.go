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

func TestTemporaryDirectoryApprovalReuseIsolationAndRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ms := memstore.New()
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
	for _, action := range []string{"approve", "deny", "revoke", "replace-directory"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			ms := memstore.New()
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
				AdditionalProjectDirs []string `json:"additionalProjectDirs"`
				DirectoryScope        string   `json:"directoryScope"`
			}
			if err := json.Unmarshal(ev.Payload, &details); err != nil {
				t.Fatal(err)
			}
			if ev.ApprovalKind != ApprovalKindToolCall || len(details.AdditionalProjectDirs) != 1 || details.AdditionalProjectDirs[0] != extra || details.DirectoryScope != "call" {
				t.Fatalf("missing combined approval: %s", ev.Payload)
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
				err = eng.ApproveApproval(ctx, "s", ev.ApprovalID, ApprovalScopeSession, []string{"/"})
			}
			if err != nil {
				t.Fatal(err)
			}
			result := <-done
			if action != "approve" {
				if result.Ok || len(calls.calls) != 0 {
					t.Fatalf("unauthorized dispatch: %+v", result)
				}
				return
			}
			if !result.Ok || len(calls.calls) != 1 {
				t.Fatalf("dispatch=%+v calls=%v", result, calls.calls)
			}
			call := calls.calls[0]
			var args map[string]any
			_ = json.Unmarshal(call.Args, &args)
			if call.CommandSandbox != tool.CommandSandboxEnforce || args["command"] != command || args["cwd"] != root || len(call.ProjectDirs) != 2 || call.CommandGrant != nil {
				t.Fatalf("execution changed: %+v", call)
			}
			dirs, _ := eng.projectRootDirsForToolCall(ctx, "s", "next", store.ModeCode)
			if len(dirs) != 1 || dirs[0] != root {
				t.Fatalf("one-call permission leaked: %v", dirs)
			}
			if eng.CommandApprovals("s").GrantCount != 0 {
				t.Fatal("one-call approval became reusable")
			}
		})
	}
}
