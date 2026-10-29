//go:build darwin

package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestScratchVerificationNeedsNoApprovalOrProjectDiff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	homeDir, project := t.TempDir(), t.TempDir()
	ms := storetest.New(t)
	if err := ms.CreateProject(ctx, &store.Project{ID: "project", RootDirs: []string{project}, ApprovalMode: store.ApprovalAuto}); err != nil {
		t.Fatal(err)
	}
	if err := ms.CreateSession(ctx, &store.Session{ID: "session", Provider: "mock", Model: "mock", ActiveMode: store.ModeCode, ProjectID: "project"}); err != nil {
		t.Fatal(err)
	}
	hub := event.NewHub()
	events, unsub := hub.Subscribe("session")
	defer unsub()
	runner := tool.NewBuiltinRunner(tool.WithHomeDir(homeDir), tool.WithCommandSandbox(homeDir))
	eng := New(ms, hub, registry.Static(mock.New()), ms, WithAttachmentHome(homeDir), WithTools(runner))
	defer eng.Stop()
	run := func(name string, args any) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(args)
		res := eng.executeAllowedTool(ctx, "session", "turn", store.ModeCode, tool.Call{SessionID: "session", TurnID: "turn", CallID: name, Name: name, Args: raw})
		if !res.Ok {
			t.Fatalf("%s failed: %+v", name, res)
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(res.Content), &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	opened := run(tool.TempWorkspace, map[string]any{"action": "open"})
	scratch := opened["absolutePath"].(string)
	output := filepath.Join(scratch, "verification.log")
	command := run(tool.CommandRun, map[string]any{"scope": "project", "command": "printf verified > " + strconv.Quote(output), "env": map[string]string{"PYTHONPATH": project}})
	if command["exitCode"] != float64(0) {
		t.Fatalf("sandbox execution failed: %+v", command)
	}
	if data, err := os.ReadFile(output); err != nil || string(data) != "verified" {
		t.Fatalf("missing scratch output: %q %v", data, err)
	}
	run(tool.CommandRun, map[string]any{"scope": "project", "command": "sleep 30", "background": true})
	blocked := eng.executeAllowedTool(ctx, "session", "turn", store.ModeCode, tool.Call{SessionID: "session", TurnID: "turn", CallID: "busy-clear", Name: tool.TempWorkspace, Args: json.RawMessage(`{"action":"clear"}`)})
	if blocked.Ok || !strings.Contains(blocked.Content, "workspace_in_use") {
		t.Fatalf("cleared active background workspace: %+v", blocked)
	}
	processes := runner.ListBackgroundProcesses("session")
	if len(processes) != 1 {
		t.Fatalf("processes=%+v", processes)
	}
	if _, err := runner.StopBackgroundProcess("session", processes[0].ProcessID); err != nil {
		t.Fatal(err)
	}
	run(tool.TempWorkspace, map[string]any{"action": "clear"})
	changes, err := eng.turnFiles.Finish("turn")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("temporary outputs entered diff: %+v", changes)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatal("cleanup retained scratch")
	}
	for {
		select {
		case ev := <-events:
			if ev.Kind == event.ApprovalRequested {
				t.Fatal("scratch workflow requested approval")
			}
		default:
			return
		}
	}
}
