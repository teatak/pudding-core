package tool

import (
	"io"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// delayedTailPTY holds an already available tail until the waiter either
// starts its bounded drain or closes the master. Closing first loses the tail,
// exactly as closing a PTY before its independent reader has caught up does.
type delayedTailPTY struct {
	readingTail chan struct{}
	draining    chan struct{}
	closed      chan struct{}
	closeOnce   sync.Once
	drainOnce   sync.Once
	reads       int
}

func (p *delayedTailPTY) Read(dst []byte) (int, error) {
	p.reads++
	if p.reads == 1 {
		return copy(dst, "sandbox-tty\r\n"), nil
	}
	close(p.readingTail)
	select {
	case <-p.closed:
		return 0, io.ErrClosedPipe
	case <-p.draining:
		return copy(dst, "received:sandbox-tty\r\n"), io.EOF
	}
}

func (p *delayedTailPTY) Write(data []byte) (int, error) { return len(data), nil }

func (p *delayedTailPTY) Close() error {
	p.closeOnce.Do(func() { close(p.closed) })
	return nil
}

func (p *delayedTailPTY) SetReadDeadline(time.Time) error {
	p.drainOnce.Do(func() { close(p.draining) })
	return nil
}

func TestBackgroundProcessPTYWaitDrainsBeforeCompletion(t *testing.T) {
	args := commandHelperArgs("exit", "0")
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	terminal := &delayedTailPTY{
		readingTail: make(chan struct{}), draining: make(chan struct{}), closed: make(chan struct{}),
	}
	process := &backgroundProcess{
		manager: newBackgroundProcessManager(time.Hour), cmd: cmd,
		id: "drain", sessionID: "session", running: true, tty: true,
		stdin: terminal, pty: terminal, ptyReadDone: make(chan struct{}), done: make(chan struct{}),
	}
	t.Cleanup(func() {
		_ = terminal.Close()
		_ = cmd.Process.Kill()
		process.cancelExpiry()
	})
	go process.readPTYOutput()
	waitBackgroundProcessSignal(t, terminal.readingTail, "PTY reader reaching the pending tail")
	go process.wait()
	waitBackgroundProcessSignal(t, process.done, "process completion")
	got := backgroundOutputText(process.logSnapshot(0, backgroundProcessPollDefault, 0).Output)
	if got != "sandbox-tty\r\nreceived:sandbox-tty\r\n" {
		t.Fatalf("completed PTY lost its pending output: %q", got)
	}
	select {
	case <-process.ptyReadDone:
	default:
		t.Fatal("process completed before its PTY reader returned")
	}
}

func waitBackgroundProcessSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
