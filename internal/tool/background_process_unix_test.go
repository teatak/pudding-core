//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package tool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
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

func TestBackgroundProcessPTYDrainDoesNotWaitForDescendant(t *testing.T) {
	manager := newBackgroundProcessManager(time.Hour)
	t.Cleanup(func() { _ = manager.Close() })
	root := t.TempDir()
	command := `trap '' HUP; /bin/sleep 30 & printf 'holder:%s\n' "$!"`
	process, err := manager.Start("held-pty", "", "", root, []string{root}, CommandSandboxBypass, "", os.Environ(), "/bin/sh", []string{"-c", command}, command, "sh", true)
	if err != nil {
		t.Fatal(err)
	}
	// The descendant deliberately ignores the terminal hangup. Reap it by
	// process group even after the tracked shell has already finished.
	t.Cleanup(func() { _ = terminateCommandProcess(process.cmd) })
	waitBackgroundProcessSignal(t, process.done, "bounded PTY drain with a surviving descendant")
	text := backgroundOutputText(process.logSnapshot(0, backgroundProcessPollDefault, 0).Output)
	var holder int
	if _, err := fmt.Sscanf(strings.TrimSpace(text), "holder:%d", &holder); err != nil {
		t.Fatalf("missing descendant PID in drained output %q: %v", text, err)
	}
	if err := syscall.Kill(holder, 0); err != nil {
		t.Fatalf("descendant did not retain the slave through command completion: %v", err)
	}
	select {
	case <-process.ptyReadDone:
	default:
		t.Fatal("bounded drain left the PTY reader running")
	}
}

func TestBackgroundProcessPTYStopDrainsFinalOutput(t *testing.T) {
	manager := newBackgroundProcessManager(time.Hour)
	t.Cleanup(func() { _ = manager.Close() })
	root := t.TempDir()
	command := `trap 'printf stopped; exit 0' TERM; printf ready; while :; do sleep 1; done`
	process, err := manager.Start("stop-pty", "", "", root, []string{root}, CommandSandboxBypass, "", os.Environ(), "/bin/sh", []string{"-c", command}, command, "sh", true)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(backgroundOutputText(process.logSnapshot(0, backgroundProcessPollDefault, 0).Output), "ready") {
		if time.Now().After(deadline) {
			t.Fatal("PTY shell did not install its termination handler")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := process.stop("test_stop"); err != nil {
		t.Fatal(err)
	}
	output := backgroundOutputText(process.logSnapshot(0, backgroundProcessPollDefault, 0).Output)
	if !strings.Contains(output, "stopped") {
		t.Fatalf("PTY stop lost the termination handler output: %q", output)
	}
	if _, err := process.writeInput(context.Background(), "after-stop\n"); err == nil {
		t.Fatal("stopped PTY still accepted input")
	}
}

func TestBackgroundProcessPTYMasterCanInterruptRead(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	pollable, err := prepareBackgroundProcessPTY(master)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pollable.Close() })
	if _, err := master.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("old PTY master retained ownership: %v", err)
	}
	read := make(chan error, 1)
	go func() {
		_, err := pollable.Read(make([]byte, 1))
		read <- err
	}()
	if err := pollable.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-read:
		if !errors.Is(err, os.ErrClosed) && !errors.Is(err, io.EOF) {
			t.Fatalf("PTY read after close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("closing the master did not interrupt Read while the slave remained open")
	}
}
