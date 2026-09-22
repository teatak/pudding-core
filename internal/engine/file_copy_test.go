package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
	"github.com/teatak/pudding-core/internal/turnfiles"
)

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
			ms, hub := memstore.New(), event.NewHub()
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
