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
	start := backgroundToolCall(runner, "sess_graceful", root, CommandStart, map[string]any{
		"scope": "project",
		"argv": []string{
			"/bin/sh",
			"-c",
			`trap 'printf term > "$1"; exit 0' TERM; printf ready; while :; do sleep 1; done`,
			"pudding",
			marker,
		},
	})
	started := decodeBackgroundProcessPayload(t, start)
	deadline := time.Now().Add(2 * time.Second)
	var offset int64
	for time.Now().Before(deadline) {
		poll := decodeBackgroundProcessPayload(t, backgroundToolCall(runner, "sess_graceful", root, CommandPoll, map[string]any{
			"process_id": started.ProcessID,
			"offset":     offset,
		}))
		offset = poll.NextOffset
		if strings.Contains(backgroundOutputText(poll.Output), "ready") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if stop := backgroundToolCall(runner, "sess_graceful", root, CommandStop, map[string]any{"process_id": started.ProcessID}); !stop.Ok {
		t.Fatalf("stop process: %+v", stop)
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "term" {
		t.Fatalf("process did not receive graceful termination: content=%q err=%v", content, err)
	}
}
