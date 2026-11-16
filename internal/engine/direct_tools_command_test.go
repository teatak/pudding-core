//go:build !windows

package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestDirectToolForegroundCommandFinishesBeforeTurn(t *testing.T) {
	ctx := context.Background()
	runner := tool.NewBuiltinRunner(tool.WithHomeDir(t.TempDir()))
	eng, db := newDirectToolEngine(t, WithTools(runner))
	if err := db.CreateProject(ctx, &store.Project{ID: "command-project", RootDirs: []string{t.TempDir()}, ApprovalMode: store.ApprovalFull}); err != nil {
		t.Fatal(err)
	}
	session, err := eng.DebugSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	projectID := "command-project"
	if _, err := db.UpdateSession(ctx, session.ID, store.SessionUpdate{ProjectID: &projectID}); err != nil {
		t.Fatal(err)
	}
	// yield_ms=0 is a normal command option. The shell performs finite work;
	// there are no sleep barriers or injected execution timing hooks.
	args := json.RawMessage(`{"scope":"project","command":"printf mcp-early; i=0; while [ \"$i\" -lt 20000 ]; do i=$((i+1)); done; printf mcp-late","yield_ms":0}`)
	result, err := eng.CallTool(ctx, session.ID, store.ModeCode, tool.CommandRun, args)
	var output struct {
		Running   bool `json:"running"`
		Yielded   bool `json:"yielded"`
		Cancelled bool `json:"cancelled"`
		ExitCode  *int `json:"exitCode"`
		Stdout    string `json:"stdout"`
	}
	if err != nil || !result.Ok || json.Unmarshal([]byte(result.Content), &output) != nil || output.Running || output.Yielded || output.Cancelled || output.ExitCode == nil || *output.ExitCode != 0 || output.Stdout != "mcp-earlymcp-late" {
		t.Fatalf("foreground command ended with its MCP call before completion: %+v, %v", result, err)
	}
	if result.Name != tool.CommandRun || result.CallID == "" {
		t.Fatal("final response lost the requested command identity")
	}
	messages, err := db.ListMessages(ctx, session.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	uses, results := map[string]bool{}, map[string]bool{}
	for _, message := range messages {
		for _, part := range message.Parts {
			switch part.Type {
			case store.ContentPartToolUse:
				uses[part.CallID] = true
			case store.ContentPartToolResult:
				results[part.CallID] = true
			}
		}
	}
	for callID := range uses {
		if !results[callID] {
			t.Fatal("command or polling call lacks a canonical result")
		}
	}
}

func TestDirectToolForegroundPollRestoresCommandFailure(t *testing.T) {
	polled := tool.Result{CallID: "command-call", Name: tool.CommandRun, Ok: true,
		Content: `{"ok":true,"running":false,"reason":"timed_out","timedOut":true,"stdout":"partial output"}`}
	result, err := directForegroundCommandResult(polled, "timed_out")
	var payload struct {
		OK       bool   `json:"ok"`
		TimedOut bool   `json:"timedOut"`
		Stdout   string `json:"stdout"`
	}
	if err != nil || json.Unmarshal([]byte(result.Content), &payload) != nil || result.Ok || payload.OK || !payload.TimedOut || payload.Stdout != "partial output" {
		t.Fatalf("successful polling hid command timeout: %+v, %v", result, err)
	}
	if result.CallID != polled.CallID || result.Name != polled.Name {
		t.Fatal("command outcome conversion changed its identity")
	}
}
