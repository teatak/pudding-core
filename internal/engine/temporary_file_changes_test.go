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
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
	"github.com/teatak/pudding-core/internal/turnfiles"
)

func TestDeclaredCommandChangesPersistAfterFailureAndUndo(t *testing.T) {
	t.Run("synchronous", func(t *testing.T) { testDeclaredCommandChanges(t, false) })
	t.Run("yielded", func(t *testing.T) { testDeclaredCommandChanges(t, true) })
}

func testDeclaredCommandChanges(t *testing.T, yield bool) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture executes a POSIX shell script")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	root := t.TempDir()
	for name, content := range map[string]string{
		"edited.txt":    "existing uncommitted edits\n",
		"deleted.txt":   "deleted original\n",
		"untouched.txt": "user content\n",
		"unrelated.txt": "before unrelated command effect\n",
		"script.sh":     "sleep 0.2\nprintf 'changed\\n' > edited.txt\nprintf 'generated\\n' > added.txt\nrm deleted.txt\nprintf 'temporary\\n' > temporary.txt\nrm temporary.txt\nprintf 'unrelated command effect\\n' > unrelated.txt\nexit 7\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	st := storetest.New(t)
	if err := st.CreateSession(ctx, &store.Session{ID: "session", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "session", TurnID: "turn", UserMessageID: "user", ClientMessageID: "client", UserText: "run script"}); err != nil {
		t.Fatal(err)
	}
	runner := tool.NewBuiltinRunner(tool.WithCommandSandbox(t.TempDir()))
	eng := New(st, event.NewHub(), registry.Static(mock.New()), nil, WithTools(runner))
	defer eng.Stop()
	input := map[string]any{"scope": "project", "command": "sh script.sh", "timeout_ms": 5000, "mutation_paths": []string{"edited.txt", "added.txt", "deleted.txt", "temporary.txt", "untouched.txt"}}
	if yield {
		input["yield_ms"] = 0
	}
	args, _ := json.Marshal(input)
	result := eng.callTrackedTool(ctx, "session", "turn", store.ModeCode, tool.Call{
		SessionID: "session", TurnID: "turn", CallID: "script", Name: tool.CommandRun,
		ProjectDirs: []string{root}, CommandSandbox: tool.CommandSandboxEnforce, CommandStateKey: "declared-mutations",
		Args: args,
	})
	if yield {
		var pending struct {
			ProcessID string `json:"processID"`
			Running   bool   `json:"running"`
		}
		if err := json.Unmarshal([]byte(result.Content), &pending); err != nil || !pending.Running || pending.ProcessID == "" {
			t.Fatalf("not yielded: %s", result.Content)
		}
		pollArgs, _ := json.Marshal(map[string]any{"action": "poll", "process_id": pending.ProcessID, "wait_ms": 5000})
		result = eng.callTrackedTool(ctx, "session", "turn", store.ModeCode, tool.Call{SessionID: "session", TurnID: "turn", CallID: "poll", Name: tool.CommandSession, ProjectDirs: []string{root}, Args: pollArgs})
	}
	var output struct {
		ExitCode int      `json:"exitCode"`
		Paths    []string `json:"mutationPaths"`
	}
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil || !result.Ok || output.ExitCode != 7 || len(output.Paths) != 5 {
		t.Fatalf("command result: %s err=%v", result.Content, err)
	}
	eng.finishTurn("session", "turn", store.ModeCode, store.TurnFailed, "script failed", nil)
	turn, err := st.GetConversationTurn(ctx, "session", "turn")
	if err != nil || len(turn.FileChanges) != 3 {
		t.Fatalf("changes=%+v err=%v", turn, err)
	}
	wantKinds := map[string]store.FileChangeKind{"edited.txt": store.FileChangeModified, "added.txt": store.FileChangeAdded, "deleted.txt": store.FileChangeDeleted}
	for _, change := range turn.FileChanges {
		if change.Kind != wantKinds[change.Path] || change.Origin != store.FileChangeOriginCommandObserved {
			t.Fatalf("wrong change attribution: %+v", change)
		}
	}
	replayer := turnfiles.NewReplayer(st)
	undo, err := replayer.Apply(ctx, "session", "turn", turnfiles.ReplayUndo, []string{root}, turn.FileChangeRevision)
	if err != nil {
		t.Fatal(err)
	}
	assertFile := func(name, want string) {
		t.Helper()
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(content) != want {
			t.Fatalf("%s = %q err=%v", name, content, err)
		}
	}
	assertFile("edited.txt", "existing uncommitted edits\n")
	assertFile("deleted.txt", "deleted original\n")
	assertFile("untouched.txt", "user content\n")
	assertFile("unrelated.txt", "unrelated command effect\n")
	if _, err := os.Stat(filepath.Join(root, "added.txt")); !os.IsNotExist(err) {
		t.Fatalf("added file survived undo: %v", err)
	}
	if _, err := replayer.Apply(ctx, "session", "turn", turnfiles.ReplayRedo, []string{root}, undo.Revision); err != nil {
		t.Fatal(err)
	}
	assertFile("edited.txt", "changed\n")
	assertFile("added.txt", "generated\n")
	assertFile("unrelated.txt", "unrelated command effect\n")
	if _, err := os.Stat(filepath.Join(root, "deleted.txt")); !os.IsNotExist(err) {
		t.Fatalf("deleted file survived redo: %v", err)
	}
}

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
			eng := New(storetest.New(t), event.NewHub(), registry.Static(mock.New()), nil, WithTools(runner))
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
	ms := storetest.New(t)
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
