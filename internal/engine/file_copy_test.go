package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
	"github.com/teatak/pudding-core/internal/turnfiles"
)

func TestFileCopyScratchAbsoluteUndo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	homeDir := t.TempDir()
	root, err := home.PrepareCodeScratch(homeDir, "session")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	ms := storetest.New(t)
	if err := ms.CreateSession(ctx, &store.Session{ID: "session", Provider: "mock", Model: "mock", ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{SessionID: "session", TurnID: "turn", UserMessageID: "user", ClientMessageID: "client", UserText: "copy"}); err != nil {
		t.Fatal(err)
	}
	runner := tool.NewBuiltinRunner(tool.WithHomeDir(homeDir))
	t.Cleanup(func() { _ = runner.Close() })
	eng := New(ms, event.NewHub(), registry.Static(mock.New()), ms, WithAttachmentHome(homeDir), WithTools(runner))
	raw, _ := json.Marshal(map[string]any{
		"from": map[string]string{"path": filepath.Join(root, "source.txt")},
		"to":   map[string]string{"path": filepath.Join(root, "target.txt")},
	})
	result := eng.executeAllowedTool(ctx, "session", "turn", store.ModeCode, tool.Call{SessionID: "session", TurnID: "turn", CallID: "copy", Name: tool.FileCopy, Args: raw})
	if !result.Ok {
		t.Fatal(result.Content)
	}
	changes, err := eng.turnFiles.Finish("turn")
	if err != nil || len(changes) != 1 || changes[0].Path != "target.txt" {
		t.Fatalf("wrong scratch artifact: %v %+v", err, changes)
	}
	if _, err := ms.FinishTurn(ctx, store.FinishTurnInput{TurnID: "turn", Status: store.TurnCompleted, FileChanges: changes}); err != nil {
		t.Fatal(err)
	}
	replayer := turnfiles.NewReplayer(ms)
	if _, err := replayer.Apply(ctx, "session", "turn", turnfiles.ReplayUndo, []string{root}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "target.txt")); !os.IsNotExist(err) {
		t.Fatal("scratch undo failed")
	}
	if _, err := replayer.Apply(ctx, "session", "turn", turnfiles.ReplayRedo, []string{root}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"source.txt", "target.txt"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(data) != "source" {
			t.Fatal("redo changed source or failed to restore target")
		}
	}
}

func TestCrossScopeCopyApprovalAndUndo(t *testing.T) {
	for _, scenario := range []struct {
		name string
		mode store.ApprovalMode
		deny bool
	}{
		{"auto", store.ApprovalAuto, false}, {"ask allow", store.ApprovalAsk, false}, {"ask deny", store.ApprovalAsk, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			root := t.TempDir()
			ms, hub := storetest.New(t), event.NewHub()
			if err := ms.CreateProject(ctx, &store.Project{ID: "project", Name: "copy", RootDirs: []string{root}, ApprovalMode: scenario.mode}); err != nil {
				t.Fatal(err)
			}
			if err := ms.CreateSession(ctx, &store.Session{ID: "session", Provider: "mock", Model: "mock", ProjectID: "project", ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession}); err != nil {
				t.Fatal(err)
			}
			if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{SessionID: "session", TurnID: "turn", UserMessageID: "user", ClientMessageID: "client", UserText: "save temp file"}); err != nil {
				t.Fatal(err)
			}
			runner := tool.NewBuiltinRunner(tool.WithHomeDir(t.TempDir()))
			t.Cleanup(func() { _ = runner.Close() })
			seed := runner.Call(ctx, tool.Call{Name: tool.FileWrite, Args: json.RawMessage(`{"scope":"temp","path":"page.html","content":"hello"}`)})
			if !seed.Ok {
				t.Fatal(seed.Content)
			}
			eng := New(ms, hub, registry.Static(mock.New()), ms, WithTools(runner))
			events, unsubscribe := hub.Subscribe("session")
			defer unsubscribe()
			resultCh := make(chan tool.Result, 1)
			go func() {
				resultCh <- eng.executeAllowedTool(ctx, "session", "turn", store.ModeCode, tool.Call{SessionID: "session", TurnID: "turn", CallID: "copy", Name: tool.FileCopy, Args: json.RawMessage(`{"from":{"scope":"temp","path":"page.html"},"to":{"scope":"project","path":"page.html"}}`)})
			}()
			if scenario.mode == store.ApprovalAsk {
				select {
				case ev := <-events:
					if ev.Kind != event.ApprovalRequested {
						t.Fatalf("unexpected event: %+v", ev)
					}
					if _, err := os.Stat(filepath.Join(root, "page.html")); !os.IsNotExist(err) {
						t.Fatal("copy ran before approval")
					}
					var err error
					if scenario.deny {
						err = eng.DenyApproval(ctx, "session", ev.ApprovalID, "test denial")
					} else {
						err = eng.ApproveApproval(ctx, "session", ev.ApprovalID, ApprovalScopeTurn, nil)
					}
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			select {
			case result := <-resultCh:
				if result.Ok == scenario.deny {
					t.Fatal(result.Content)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			changes, err := eng.turnFiles.Finish("turn")
			if err != nil {
				t.Fatal(err)
			}
			if scenario.deny {
				if len(changes) != 0 {
					t.Fatal("denied copy recorded changes")
				}
				if _, err := os.Stat(filepath.Join(root, "page.html")); !os.IsNotExist(err) {
					t.Fatal("denied copy wrote destination")
				}
				return
			}
			if len(changes) != 1 || changes[0].Path != "page.html" || changes[0].Kind != store.FileChangeAdded {
				t.Fatalf("wrong artifact: %+v", changes)
			}
			if _, err := ms.FinishTurn(ctx, store.FinishTurnInput{TurnID: "turn", Status: store.TurnCompleted, FileChanges: changes}); err != nil {
				t.Fatal(err)
			}
			replayer := turnfiles.NewReplayer(ms)
			if _, err := replayer.Apply(ctx, "session", "turn", turnfiles.ReplayUndo, []string{root}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, "page.html")); !os.IsNotExist(err) {
				t.Fatal("undo did not remove new copy")
			}
			if _, err := replayer.Apply(ctx, "session", "turn", turnfiles.ReplayRedo, []string{root}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, "page.html"))
			if err != nil || string(data) != "hello" {
				t.Fatal("redo failed")
			}
			read := runner.Call(ctx, tool.Call{Name: tool.FileRead, Args: json.RawMessage(`{"scope":"temp","path":"page.html"}`)})
			if !read.Ok {
				t.Fatal("source changed during undo/redo")
			}
		})
	}
}

func TestFileCopyExternalAccessApproval(t *testing.T) {
	for _, tc := range []struct {
		name                                       string
		mode                                       store.ApprovalMode
		externalSource, externalTarget, tempTarget bool
		decision                                   string
	}{
		{"source auto", store.ApprovalAuto, true, false, false, "allow"},
		{"destination ask", store.ApprovalAsk, false, true, false, "allow"},
		{"both full", store.ApprovalFull, true, true, false, "allow"},
		{"external to temp", store.ApprovalAuto, true, false, true, "allow"},
		{"denied", store.ApprovalAuto, true, true, false, "deny"},
		{"cancelled", store.ApprovalAuto, false, true, false, "cancel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			root, external := t.TempDir(), t.TempDir()
			ms, hub := storetest.New(t), event.NewHub()
			if err := ms.CreateProject(ctx, &store.Project{ID: "project", Name: "copy", RootDirs: []string{root}, ApprovalMode: tc.mode}); err != nil {
				t.Fatal(err)
			}
			if err := ms.CreateSession(ctx, &store.Session{ID: "session", Provider: "mock", Model: "mock", ProjectID: "project", ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession}); err != nil {
				t.Fatal(err)
			}
			if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{SessionID: "session", TurnID: "turn", UserMessageID: "user", ClientMessageID: "client", UserText: "copy"}); err != nil {
				t.Fatal(err)
			}
			runner := tool.NewBuiltinRunner(tool.WithHomeDir(t.TempDir()))
			t.Cleanup(func() { _ = runner.Close() })
			eng := New(ms, hub, registry.Static(mock.New()), ms, WithTools(tool.NewMultiRunner(runner)))
			source, target := filepath.Join(root, "source.txt"), filepath.Join(root, "target.txt")
			if tc.externalSource {
				source = filepath.Join(external, "source.txt")
			}
			if tc.externalTarget {
				target = filepath.Join(external, "target.txt")
			}
			if err := os.WriteFile(source, []byte("copy-content"), 0o600); err != nil {
				t.Fatal(err)
			}
			to := map[string]string{"path": target}
			if tc.tempTarget {
				to = map[string]string{"scope": "temp", "path": "target.txt"}
			}
			raw, _ := json.Marshal(map[string]any{"from": map[string]string{"path": source}, "to": to})
			call := tool.Call{SessionID: "session", TurnID: "turn", CallID: "copy", Name: tool.FileCopy, Args: raw}
			events, unsubscribe := hub.Subscribe("session")
			defer unsubscribe()
			resultCh := make(chan tool.Result, 1)
			go func() { resultCh <- eng.executeAllowedTool(ctx, "session", "turn", store.ModeCode, call) }()
			select {
			case ev := <-events:
				if ev.Kind != event.ApprovalRequested {
					t.Fatalf("unexpected event: %+v", ev)
				}
				var payload struct {
					FileCopy struct {
						From, To struct {
							Path     string
							External bool
						}
						ExternalAccess bool
					}
				}
				if err := json.Unmarshal(ev.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				if !payload.FileCopy.ExternalAccess || payload.FileCopy.From.External != tc.externalSource || payload.FileCopy.To.External != tc.externalTarget {
					t.Fatalf("wrong approval: %s", ev.Payload)
				}
				if _, err := os.Stat(payload.FileCopy.To.Path); !os.IsNotExist(err) {
					t.Fatal("copy ran before approval")
				}
				var err error
				switch tc.decision {
				case "deny":
					err = eng.DenyApproval(ctx, "session", ev.ApprovalID, "no")
				case "cancel":
					cancel()
				default:
					err = eng.ApproveApproval(ctx, "session", ev.ApprovalID, ApprovalScopeSession, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
			case result := <-resultCh:
				t.Fatalf("copy did not ask: %s", result.Content)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case result := <-resultCh:
				if result.Ok != (tc.decision == "allow") {
					t.Fatal(result.Content)
				}
			case <-time.After(time.Second):
				t.Fatal("copy did not resume or cancel")
			}
			project, err := ms.GetProject(context.Background(), "project")
			if err != nil || len(project.RootDirs) != 1 || project.RootDirs[0] != root {
				t.Fatal("approval expanded project roots")
			}
			eng.mu.Lock()
			grantCount := len(eng.turnProjectAccess)
			eng.mu.Unlock()
			if grantCount != 0 {
				t.Fatal("copy retained directory access")
			}
			changes, err := eng.turnFiles.Finish("turn")
			if err != nil {
				t.Fatal(err)
			}
			wantChanges := 0
			if tc.decision == "allow" && !tc.externalTarget && !tc.tempTarget {
				wantChanges = 1
			}
			if len(changes) != wantChanges {
				t.Fatalf("wrong destination tracking: %+v", changes)
			}
			if wantChanges == 1 {
				if _, err := ms.FinishTurn(context.Background(), store.FinishTurnInput{TurnID: "turn", Status: store.TurnCompleted, FileChanges: changes}); err != nil {
					t.Fatal(err)
				}
				replayer := turnfiles.NewReplayer(ms)
				if _, err := replayer.Apply(context.Background(), "session", "turn", turnfiles.ReplayUndo, []string{root}); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(target); !os.IsNotExist(err) {
					t.Fatal("undo did not remove project destination")
				}
				if _, err := replayer.Apply(context.Background(), "session", "turn", turnfiles.ReplayRedo, []string{root}); err != nil {
					t.Fatal(err)
				}
			}
			if !tc.tempTarget {
				data, err := os.ReadFile(target)
				if tc.decision == "allow" {
					if err != nil || string(data) != "copy-content" {
						t.Fatal("destination not copied")
					}
				} else if !os.IsNotExist(err) {
					t.Fatal("denied/cancelled copy wrote destination")
				}
			}
			data, err := os.ReadFile(source)
			if err != nil || string(data) != "copy-content" {
				t.Fatal("source changed")
			}
			// A later ordinary call receives only the original roots, not this approval.
			call.ProjectDirs, call.CallID = []string{root}, "later-copy"
			later := runner.Call(context.Background(), call)
			if later.Ok {
				t.Fatal("single-copy permission leaked into a later call")
			}
		})
	}
}
