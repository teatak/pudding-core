//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackgroundProcessStopRequestsGracefulTermination(t *testing.T) {
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	root := t.TempDir()
	marker := filepath.Join(root, "terminated.txt")
	start := backgroundToolCall(runner, "sess_graceful", root, CommandRun, map[string]any{
		"scope":   "project",
		"command": `trap 'printf term > "$PUDDING_MARKER"; exit 0' TERM; printf ready; while :; do sleep 1; done`,
		"env":     map[string]string{"PUDDING_MARKER": marker},
	})
	started := decodeBackgroundProcessPayload(t, start)
	deadline := time.Now().Add(2 * time.Second)
	var offset int64
	for time.Now().Before(deadline) {
		poll := decodeBackgroundProcessPayload(t, backgroundToolCall(runner, "sess_graceful", root, CommandSession, map[string]any{
			"action":     "poll",
			"process_id": started.ProcessID,
			"offset":     offset,
		}))
		offset = poll.NextOffset
		if strings.Contains(backgroundOutputText(poll.Output), "ready") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if stop := backgroundToolCall(runner, "sess_graceful", root, CommandSession, map[string]any{"action": "stop", "process_id": started.ProcessID}); !stop.Ok {
		t.Fatalf("stop process: %+v", stop)
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "term" {
		t.Fatalf("process did not receive graceful termination: content=%q err=%v", content, err)
	}
}

func TestBackgroundProcessPTYAcceptsInput(t *testing.T) {
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	root := t.TempDir()
	start := backgroundToolCall(runner, "sess_tty", root, CommandRun, map[string]any{
		"scope":   "project",
		"command": commandHelperCommand("stdin-line"),
		"tty":     true,
	})
	started := decodeBackgroundProcessPayload(t, start)
	if !start.Ok || !started.Running || !started.TTY {
		t.Fatalf("interactive process did not start: result=%+v payload=%+v", start, started)
	}
	write := backgroundToolCall(runner, "sess_tty", root, CommandSession, map[string]any{
		"action":     "write",
		"process_id": started.ProcessID,
		"data":       "hello-tty\n",
	})
	if !write.Ok {
		t.Fatalf("write PTY input: %+v", write)
	}
	poll := decodeBackgroundProcessPayload(t, backgroundToolCall(runner, "sess_tty", root, CommandSession, map[string]any{
		"action":     "poll",
		"process_id": started.ProcessID,
		"wait_ms":    2000,
	}))
	if poll.Running || !strings.Contains(backgroundOutputText(poll.Output), "received:hello-tty") {
		t.Fatalf("PTY input was not observed: %+v", poll)
	}
}
