//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package tool

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	// Emergency cleanup on a failed assertion must not replace the normal
	// process-group cleanup performed before process.done is closed.
	t.Cleanup(func() { process.signalStop(true) })
	waitBackgroundProcessSignal(t, process.done, "bounded PTY drain with a surviving descendant")
	text := backgroundOutputText(process.logSnapshot(0, backgroundProcessPollDefault, 0).Output)
	if !strings.Contains(text, "holder:") {
		t.Fatalf("missing descendant PID in drained output %q", text)
	}
	select {
	case <-process.ptyReadDone:
	default:
		t.Fatal("bounded drain left the PTY reader running")
	}
}

func TestBackgroundProcessCompletionCleansDescendants(t *testing.T) {
	for _, mode := range []struct {
		name      string
		tty       bool
		redirects string
	}{
		{name: "PTY", tty: true},
		{name: "pipes"},
		{name: "closed_pipes", redirects: ">/dev/null 2>&1"},
	} {
		t.Run(mode.name, func(t *testing.T) {
			manager := newBackgroundProcessManager(time.Hour)
			root := t.TempDir()
			gate := filepath.Join(root, "release")
			marker := filepath.Join(root, "survived")
			env := append(os.Environ(), "PUDDING_TEST_GATE="+gate, "PUDDING_TEST_MARKER="+marker)
			command := `trap '' HUP TERM; (while [ ! -e "$PUDDING_TEST_GATE" ]; do sleep 0.02; done; printf survived > "$PUDDING_TEST_MARKER"; sleep 30) ` + mode.redirects + ` & printf 'child:%s\n' "$!"`
			process, err := manager.Start("descendant", "", "", root, []string{root}, CommandSandboxBypass, "", env, "/bin/sh", []string{"-c", command}, command, "sh", mode.tty)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { process.signalStop(true); _ = manager.Close() })
			waitBackgroundProcessSignal(t, process.done, "completion and descendant cleanup")
			if err := process.stop("stopped"); err != nil {
				t.Fatal(err)
			}
			manager.CloseSession("descendant")
			if err := os.WriteFile(gate, []byte("go"), 0600); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(200 * time.Millisecond)
			for time.Now().Before(deadline) {
				if content, err := os.ReadFile(marker); err == nil {
					t.Fatalf("descendant ran after completion, stop and CloseSession: %q", content)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

func TestBackgroundProcessWaitObservesExitWithoutReaping(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 7")
	configureCommandProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	// Also exercise registration after a very short-lived child has exited.
	time.Sleep(20 * time.Millisecond)
	if err := waitForBackgroundProcessExit(cmd); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("exit observer reaped or changed the child's status: %v", err)
	}
}

func TestBackgroundProcessFinishedSignalCannotReachReusedGroup(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 30")
	configureCommandProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	defer func() { _ = terminateCommandProcess(cmd); <-waited }()
	// Simulate a retained record whose old group ID now identifies a new job.
	// signalsDone closes ownership before Wait can release the original PID.
	process := &backgroundProcess{cmd: cmd, running: true, signalsDone: true, done: make(chan struct{})}
	close(process.done)
	if err := process.stop("stopped"); err != nil {
		t.Fatal(err)
	}
	process.signalStop(true)
	select {
	case err := <-waited:
		waited <- err
		t.Fatalf("a signal escaped the closed ownership interval: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestBackgroundProcessCloseSessionHoldsQuotaUntilCompletion(t *testing.T) {
	manager := newBackgroundProcessManager(time.Hour)
	t.Cleanup(func() { _ = manager.Close() })
	root := t.TempDir()
	gate := filepath.Join(root, "exit")
	env := append(os.Environ(), "PUDDING_TEST_GATE="+gate)
	command := `trap '' TERM; printf ready; while [ ! -e "$PUDDING_TEST_GATE" ]; do sleep 0.02; done`
	process, err := manager.Start("quota", "", "", root, []string{root}, CommandSandboxBypass, "", env, "/bin/sh", []string{"-c", command}, command, "sh", true)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(backgroundOutputText(process.logSnapshot(0, backgroundProcessPollDefault, 0).Output), "ready") {
		if time.Now().After(deadline) {
			t.Fatal("shell did not install its stop handler")
		}
		time.Sleep(time.Millisecond)
	}
	closed := make(chan struct{})
	go func() { manager.CloseSession("quota"); close(closed) }()
	defer func() { _ = os.WriteFile(gate, nil, 0600); <-closed }()
	for {
		process.mu.Lock()
		stopping := process.requestedStopReason != ""
		process.mu.Unlock()
		if stopping {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session cleanup did not begin")
		}
		time.Sleep(time.Millisecond)
	}
	manager.mu.Lock()
	count := manager.runningTotal
	manager.mu.Unlock()
	if count != 1 {
		t.Fatalf("closing session released quota while its process still runs: %d", count)
	}
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	waitBackgroundProcessSignal(t, closed, "session cleanup")
	manager.mu.Lock()
	count = manager.runningTotal
	manager.mu.Unlock()
	if count != 0 {
		t.Fatalf("completed cleanup retained quota: %d", count)
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
