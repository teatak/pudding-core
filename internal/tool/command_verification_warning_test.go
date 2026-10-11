package tool

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestVerificationPipelineWarnsWithoutChangingShellExitStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX pipeline semantics")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go"), []byte("#!/bin/sh\nprintf 'test failed' >&2\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	result := commandTestCall(context.Background(), root, map[string]any{"scope": "project", "command": "./go test | tail -n 1"})
	fields := decodeToolResult(t, result)
	if !result.Ok || fields["exitCode"] != float64(0) || fields["verificationStatus"] != nil || !strings.Contains(fields["stderr"].(string), "test failed") {
		t.Fatalf("changed shell semantics or inferred test success: %s", result.Content)
	}
	warning, _ := fields["verificationWarning"].(map[string]any)
	if warning["code"] != "pipeline_exit_status" || !strings.Contains(warning["message"].(string), "does not prove") {
		t.Fatalf("missing actionable verification warning: %s", result.Content)
	}
	// Background start, poll and the REST snapshot must expose the same facts.
	runner := NewBuiltinRunner()
	defer runner.Close()
	start := backgroundToolCall(runner, "pipeline", root, CommandRun, map[string]any{"scope": "project", "command": "./go test | tail -n 1"})
	started := decodeBackgroundProcessPayload(t, start)
	if !start.Ok {
		t.Fatal(start.Content)
	}
	process := runner.processes.Get("pipeline", started.ProcessID)
	select {
	case <-process.done:
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline did not finish")
	}
	poll := backgroundToolCall(runner, "pipeline", root, CommandSession, map[string]any{"action": "poll", "process_id": started.ProcessID})
	for _, result := range []Result{start, poll} {
		if got := decodeToolResult(t, result)["verificationWarning"]; string(resultJSON(got)) != string(resultJSON(warning)) {
			t.Fatalf("background result lost warning: %s", result.Content)
		}
	}
	if snapshot := runner.ListBackgroundProcesses("pipeline")[0]; snapshot.Running || snapshot.ExitCode == nil || *snapshot.ExitCode != 0 || string(resultJSON(snapshot.VerificationWarning)) != string(resultJSON(warning)) {
		t.Fatalf("background snapshot changed exit facts: %+v", snapshot)
	}
	fields["stdout"] = strings.Repeat("long log\n", 3000)
	preview := ModelResultContent(CommandRun, true, jsonString(fields), "turn", "call", true)
	if got := decodeToolResult(t, Result{Content: preview})["verificationWarning"]; string(resultJSON(got)) != string(resultJSON(warning)) {
		t.Fatalf("long result lost verification warning: %s", preview)
	}
}

func TestVerificationPipelineWarningUsesShellStructure(t *testing.T) {
	for _, tc := range []struct {
		command string
		warn    bool
	}{
		{"npm --prefix web test | tail -n 1", true},
		{"npx tsc | tee build.log | tail", true},
		{"node --test test.js 2>&1 | cat", true},
		{"cd web && (go test ./...) | tail", true},
		{"! go test ./... | cat", true},
		{"env TEST=1 go test ./... | cat", true},
		{"go test ./... | > log", true},
		{"go test ./...", false},
		{"cat data | go test ./...", false},
		{`printf 'go test | tail'`, false},
		{"printf log | tail", false},
		{"go help test | cat", false},
		{"f() { go test | cat; }; printf defined", false},
		{`"$TOOL" test | tail`, false},
	} {
		result := commandResult(Result{}, commandRunArgs{Command: tc.command}, "sh", "/project", nil, nil, 0, newTruncatingBuffer(1024), newTruncatingBuffer(1024), false, false, 0, "", nil)
		fields := decodeToolResult(t, result)
		want := tc.warn && runtime.GOOS != "windows"
		if got := fields["verificationWarning"] != nil; got != want {
			t.Errorf("%q: warning=%v, want %v", tc.command, got, want)
		}
	}
}
