//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandRunReturnsSignalWithoutOutput(t *testing.T) {
	result := commandTestCall(context.Background(), t.TempDir(), map[string]any{
		"scope": "project", "command": "kill -TERM $$",
	})
	payload := decodeCommandPayload(t, result)
	if !result.Ok || !payload.OK || payload.ExitCode != -1 || payload.Signal != "terminated" || payload.Reason != "non_zero_exit" || !strings.Contains(payload.Error, "signal: terminated") {
		t.Fatalf("signal exit facts lost: %s", result.Content)
	}
	if payload.Stdout != "" || payload.Stderr != "" || payload.TimedOut || payload.Cancelled {
		t.Fatalf("signal exit was confused with output or cancellation: %s", result.Content)
	}
}

func TestBackgroundProcessReturnsSignalInPollAndSnapshot(t *testing.T) {
	runner := NewBuiltinRunner()
	defer runner.Close()
	root := t.TempDir()
	started := decodeBackgroundProcessPayload(t, backgroundToolCall(runner, "signal", root, CommandRun, map[string]any{
		"scope": "project", "command": "kill -TERM $$",
	}))
	result := backgroundToolCall(runner, "signal", root, CommandSession, map[string]any{
		"action": "poll", "process_id": started.ProcessID, "wait_ms": 10000,
	})
	payload := decodeBackgroundProcessPayload(t, result)
	if payload.Running || payload.ExitCode == nil || *payload.ExitCode != -1 || payload.Signal != "terminated" || payload.Reason != "non_zero_exit" || !strings.Contains(payload.Error, "signal: terminated") {
		t.Fatalf("background signal exit facts lost: %s", result.Content)
	}
	items := runner.processes.List("signal")
	if len(items) != 1 || items[0].Signal != payload.Signal || items[0].Error != payload.Error || items[0].Reason != payload.Reason {
		t.Fatalf("snapshot disagrees with poll: %+v", items)
	}
}

func TestCommandRunDoesNotVerifyNegationOrPipeline(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go"), []byte("#!/bin/sh\nprintf 'test failed' >&2\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"! go test", "go test | tail -n 1"} {
		result := commandTestCall(context.Background(), root, map[string]any{
			"scope": "project", "command": command, "env": map[string]string{"PATH": root + ":/usr/bin:/bin"},
		})
		payload := decodeCommandPayload(t, result)
		if !result.Ok || payload.ExitCode != 0 || payload.Stderr != "test failed" || payload.VerificationKind != "" || payload.VerificationStatus != "" {
			t.Fatalf("shell success must not verify the failing test: %q: %s", command, result.Content)
		}
	}
}
