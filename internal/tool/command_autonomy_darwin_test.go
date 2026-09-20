//go:build darwin

package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMacOSCommandAutonomyStillEnforcesSandbox(t *testing.T) {
	project, outside := t.TempDir(), t.TempDir()
	privateFile := filepath.Join(outside, "private.txt")
	if err := os.WriteFile(privateFile, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(privateFile, filepath.Join(project, "external")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "reader.sh"), []byte("#!/bin/sh\ncat \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithCommandSandbox(t.TempDir()))
	t.Cleanup(func() { _ = runner.Close() })
	for _, tc := range []struct {
		name, command string
		denied        bool
	}{
		{"project write", `for name in output; do printf ok > "$name"; done; cat output`, false},
		{"dynamic read", "target=" + quoteShellArg(privateFile) + `; cat "$target"`, true},
		{"dynamic write", "target=" + quoteShellArg(privateFile) + `; printf changed > "$target"`, true},
		{"child process", "target=" + quoteShellArg(privateFile) + `; ./reader.sh "$target"`, true},
		{"dynamic symlink", `target=external; cat "$target"`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"scope": "project", "command": tc.command})
			risk, ok := ClassifyToolCallForProject(CommandRun, raw, []string{project})
			if !ok || !risk.LowRisk {
				t.Fatalf("expected sandbox execution without syntax approval: %+v", risk)
			}
			result := runner.Call(context.Background(), Call{
				Name: CommandRun, CallID: tc.name, SessionID: "autonomy", Args: raw,
				ProjectDirs: []string{project}, CommandSandbox: CommandSandboxEnforce,
			})
			payload := decodeCommandPayload(t, result)
			if payload.Execution != "sandbox" || payload.SandboxDenied != tc.denied || (payload.ExitCode != 0) != tc.denied {
				t.Fatalf("sandbox enforcement changed: result=%+v payload=%+v", result, payload)
			}
		})
	}
	data, err := os.ReadFile(privateFile)
	if err != nil || string(data) != "untouched" {
		t.Fatalf("private file changed: %q %v", data, err)
	}
	// Background execution must inherit the same boundary, without host retry.
	command := "target=" + quoteShellArg(privateFile) + `; ./reader.sh "$target"`
	started := decodeBackgroundProcessPayload(t, backgroundToolCall(runner, "autonomy", project, CommandRun, map[string]any{
		"scope": "project", "command": command, "background": true,
	}))
	if started.Execution != "sandbox" || started.ProcessID == "" {
		t.Fatalf("background escaped sandbox: %+v", started)
	}
	finished := decodeBackgroundProcessPayload(t, backgroundToolCall(runner, "autonomy", project, CommandSession, map[string]any{
		"action": "poll", "process_id": started.ProcessID, "wait_ms": 2000,
	}))
	if finished.Running || !finished.SandboxDenied {
		t.Fatalf("background outside read was not denied: %+v", finished)
	}
}
