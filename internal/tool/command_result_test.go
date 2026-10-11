package tool

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCommandResultPreservesExitReasonAlongsideSandboxEvidence(t *testing.T) {
	for _, tc := range []struct {
		reason              string
		timedOut, cancelled bool
	}{
		{"non_zero_exit", false, false}, {"timed_out", true, false}, {"cancelled", false, true},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			stderr := newTruncatingBuffer(commandOutputLimitBytes)
			_, _ = stderr.Write([]byte("sandbox: deny(1) file-read-data"))
			result := commandResult(Result{}, commandRunArgs{Command: "go test"}, "sh", t.TempDir(), nil,
				&commandExecution{Sandboxed: true}, -1, newTruncatingBuffer(commandOutputLimitBytes), stderr,
				tc.timedOut, tc.cancelled, time.Second, tc.reason, errors.New("signal: killed"))
			payload := decodeCommandPayload(t, result)
			if payload.Reason != tc.reason || !payload.SandboxDenied || payload.TimedOut != tc.timedOut || payload.Cancelled != tc.cancelled || payload.Error != "signal: killed" {
				t.Fatalf("sandbox evidence overwrote process facts: %s", result.Content)
			}
		})
	}
}

func TestCommandPermissionTextDoesNotReplaceFailure(t *testing.T) {
	runner := NewBuiltinRunner()
	defer runner.Close()
	runner.setCommandRunner(&recordingCommandRunner{sandboxed: true, sandboxKind: "test-sandbox"})
	root := t.TempDir()
	args := map[string]any{"scope": "project", "command": commandHelperCommand("permission-error")}
	result := backgroundToolCall(runner, "permission", root, CommandRun, args)
	started := decodeBackgroundProcessPayload(t, result)
	result = backgroundToolCall(runner, "permission", root, CommandSession, map[string]any{
		"action": "poll", "process_id": started.ProcessID, "wait_ms": 10000,
	})
	payload := decodeBackgroundProcessPayload(t, result)
	if payload.Running || payload.SandboxDenied || payload.Reason != "non_zero_exit" || payload.Error == "" || !strings.Contains(backgroundOutputText(payload.Output), "malformed file") {
		t.Fatalf("background toolchain failure was relabeled: %s", result.Content)
	}
	args["background"] = false
	raw := resultJSON(args)
	result = runner.Call(context.Background(), Call{Name: CommandRun, Args: raw, ProjectDirs: []string{root}})
	foreground := decodeCommandPayload(t, result)
	if !result.Ok || foreground.SandboxDenied || foreground.Reason != "non_zero_exit" || foreground.ExitCode != 1 || foreground.Error == "" || !strings.Contains(foreground.Stderr, "malformed file") {
		t.Fatalf("foreground toolchain failure was relabeled: %s", result.Content)
	}
}

func TestBackgroundProcessStopPreservesReasonWithSandboxEvidence(t *testing.T) {
	runner := NewBuiltinRunner()
	defer runner.Close()
	runner.setCommandRunner(&recordingCommandRunner{sandboxed: true})
	root := t.TempDir()
	started := decodeBackgroundProcessPayload(t, backgroundToolCall(runner, "stop-evidence", root, CommandRun, map[string]any{
		"scope": "project", "command": commandHelperCommand("sandbox-denied-wait"),
	}))
	process := runner.processes.Get("stop-evidence", started.ProcessID)
	deadline := time.Now().Add(10 * time.Second)
	for {
		process.mu.Lock()
		hasEvidence := process.sandboxDenialOutput
		process.mu.Unlock()
		if hasEvidence {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command did not print its denial evidence")
		}
		time.Sleep(10 * time.Millisecond)
	}
	result := backgroundToolCall(runner, "stop-evidence", root, CommandSession, map[string]any{
		"action": "stop", "process_id": started.ProcessID,
	})
	payload := decodeBackgroundProcessPayload(t, result)
	if payload.Running || payload.Reason != "stopped" || payload.Status != "stopped" || !payload.SandboxDenied {
		t.Fatalf("denial evidence replaced the requested stop: %s", result.Content)
	}
}
