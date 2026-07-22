//go:build darwin

package tool

import (
	"strings"
	"testing"
)

func TestBackgroundProcessSandboxedPTYAcceptsInput(t *testing.T) {
	runner := NewBuiltinRunner(WithCommandSandbox(t.TempDir()))
	t.Cleanup(func() { _ = runner.Close() })
	root := t.TempDir()
	start := backgroundToolCall(runner, "sess_sandbox_tty", root, CommandRun, map[string]any{
		"scope":   "project",
		"command": `read value; printf 'received:%s\n' "$value"`,
		"tty":     true,
	})
	started := decodeBackgroundProcessPayload(t, start)
	if !start.Ok || !started.Running || !started.Sandboxed || !started.TTY {
		t.Fatalf("sandboxed interactive process did not start: result=%+v payload=%+v", start, started)
	}
	write := backgroundToolCall(runner, "sess_sandbox_tty", root, CommandSession, map[string]any{
		"action":     "write",
		"process_id": started.ProcessID,
		"data":       "sandbox-tty\n",
	})
	if !write.Ok {
		t.Fatalf("write sandboxed PTY input: %+v", write)
	}
	poll := decodeBackgroundProcessPayload(t, backgroundToolCall(runner, "sess_sandbox_tty", root, CommandSession, map[string]any{
		"action":     "poll",
		"process_id": started.ProcessID,
		"wait_ms":    2000,
	}))
	if poll.Running || poll.SandboxDenied || !strings.Contains(backgroundOutputText(poll.Output), "received:sandbox-tty") {
		t.Fatalf("sandboxed PTY input was not observed: %+v output=%q", poll, backgroundOutputText(poll.Output))
	}
}
