package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestTurnFileChangesDropCreatedThenDeletedScript(t *testing.T) {
	for _, cleanup := range []string{
		`python3 temporary.py`, // The script can unlink itself; no static output path.
		`sh -c 'rm temporary.py'`,
		`rm temporary.py`,
	} {
		t.Run(cleanup, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "temporary.py")
			runner := &recordingToolRunner{result: tool.Result{Ok: true}, callFunc: func(call tool.Call) {
				if call.Name == tool.FileWrite {
					if err := os.WriteFile(path, []byte("print('temporary')\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}}
			eng := New(memstore.New(), event.NewHub(), registry.Static(mock.New()), nil, WithTools(runner))
			eng.callTrackedTool(context.Background(), "session", "turn", store.ModeCode, tool.Call{
				CallID: "create", Name: tool.FileWrite, ProjectDirs: []string{root},
				Args: json.RawMessage(`{"scope":"project","path":"temporary.py","content":"print('temporary')\n"}`),
			})
			args, _ := json.Marshal(map[string]any{"scope": "project", "command": cleanup})
			eng.callTrackedTool(context.Background(), "session", "turn", store.ModeCode, tool.Call{
				CallID: "execute", Name: tool.CommandRun, ProjectDirs: []string{root}, Args: args,
			})
			changes, err := eng.turnFiles.Finish("turn")
			if err != nil {
				t.Fatal(err)
			}
			if len(changes) != 0 {
				t.Fatalf("deleted temporary script remains in final card: %+v", changes)
			}
		})
	}
}

func TestTurnFileChangesActualSelfDeletingScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture executes a POSIX shell script")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	root := t.TempDir()
	ms := memstore.New()
	if err := ms.CreateSession(ctx, &store.Session{ID: "session", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "session", TurnID: "turn", UserMessageID: "user", ClientMessageID: "client", UserText: "run temporary script",
	}); err != nil {
		t.Fatal(err)
	}
	runner := tool.NewBuiltinRunner(tool.WithCommandSandbox(t.TempDir()))
	eng := New(ms, event.NewHub(), registry.Static(mock.New()), nil, WithTools(runner))
	for _, step := range []struct {
		name string
		args string
	}{
		{tool.FileWrite, `{"scope":"project","path":"temporary.sh","content":"#!/bin/sh\nprintf 'ran successfully'\nrm -- \"$0\"\n"}`},
		{tool.CommandRun, `{"scope":"project","command":"sh temporary.sh","timeout_ms":5000}`},
	} {
		result := eng.callTrackedTool(ctx, "session", "turn", store.ModeCode, tool.Call{
			SessionID: "session", TurnID: "turn", CallID: step.name, Name: step.name, Args: json.RawMessage(step.args),
			ProjectDirs: []string{root}, CommandSandbox: tool.CommandSandboxEnforce, CommandStateKey: "self-delete-test",
		})
		if !result.Ok {
			t.Fatalf("%s failed: %+v", step.name, result)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "temporary.sh")); !os.IsNotExist(err) {
		t.Fatalf("script was not deleted: %v", err)
	}
	eng.finishTurn("session", "turn", store.ModeCode, store.TurnCompleted, "", nil)
	turn, err := ms.GetConversationTurn(ctx, "session", "turn")
	if err != nil {
		t.Fatal(err)
	}
	if len(turn.FileChanges) != 0 {
		t.Fatalf("canonical turn retained temporary script: %+v", turn.FileChanges)
	}
}
