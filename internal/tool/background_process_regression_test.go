package tool

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestBackgroundProcessOutputPreservesSplitUTF8(t *testing.T) {
	var buffer backgroundProcessOutputBuffer
	stdout := []byte("中")
	stderr := []byte("文")
	buffer.Append(ProgressStdout, stdout[:1])
	buffer.Append(ProgressStderr, stderr[:2])
	if buffer.nextOffset != 0 {
		t.Fatalf("incomplete runes were published: offset=%d", buffer.nextOffset)
	}
	buffer.Append(ProgressStdout, stdout[1:])
	buffer.Append(ProgressStderr, stderr[2:])
	chunks, next, truncated, more := buffer.Read(0, backgroundProcessPollDefault)
	if got := backgroundOutputText(chunks); got != "中文" || next != 6 || truncated || more {
		t.Fatalf("split UTF-8 corrupted: output=%q next=%d truncated=%v more=%v", got, next, truncated, more)
	}
	if len(chunks) != 2 || chunks[0].Stream != ProgressStdout || chunks[1].Stream != ProgressStderr || chunks[1].Offset != 3 {
		t.Fatalf("stream identity or offsets lost: %+v", chunks)
	}
}

func TestBackgroundProcessOutputFlushesIncompleteUTF8OnCompletion(t *testing.T) {
	args := commandHelperArgs("exit", "0")
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	process := &backgroundProcess{
		manager: newBackgroundProcessManager(time.Hour), cmd: cmd,
		id: "utf8", running: true, done: make(chan struct{}),
	}
	defer process.cancelExpiry()
	process.output.Append(ProgressStdout, []byte{'x', 0xe4, 0xb8})
	process.output.Append(ProgressStderr, []byte{0xe6})
	process.wait()
	snapshot := process.logSnapshot(0, backgroundProcessPollDefault, 0)
	if snapshot.Process.Running || snapshot.TailOffset != 7 {
		t.Fatalf("completion did not publish UTF-8 tails: %+v", snapshot)
	}
	streams := map[string]string{}
	for _, chunk := range snapshot.Output {
		streams[chunk.Stream] += chunk.Content
	}
	if streams[ProgressStdout] != "x�" || streams[ProgressStderr] != "�" {
		t.Fatalf("incomplete EOF tails lost or combined across streams: %+v", streams)
	}
	process.output.Finish()
	if process.output.nextOffset != 7 {
		t.Fatal("finishing an already completed stream duplicated the tail")
	}
}

func TestBackgroundProcessSplitUTF8RetainsValidRingOffsets(t *testing.T) {
	var buffer backgroundProcessOutputBuffer
	buffer.Append(ProgressStdout, []byte(strings.Repeat("a", backgroundProcessOutputLimit)))
	character := []byte("中")
	buffer.Append(ProgressStdout, character[:2])
	// Read buffers are reusable: retaining a prefix must copy its bytes.
	character[0] = 0
	buffer.Append(ProgressStdout, []byte{0xad, 0xff, 'z'})
	if buffer.bytes > backgroundProcessOutputLimit || buffer.nextOffset != backgroundProcessOutputLimit+7 {
		t.Fatalf("unexpected capacity or decoded byte offset: bytes=%d offset=%d", buffer.bytes, buffer.nextOffset)
	}
	chunks, next, truncated, more := buffer.Read(backgroundProcessOutputLimit-1, 8)
	if got := backgroundOutputText(chunks); got != "a中�z" || !utf8.ValidString(got) || next != buffer.nextOffset || truncated || more {
		t.Fatalf("ring corrupted split/invalid UTF-8: %q next=%d truncated=%v more=%v", got, next, truncated, more)
	}
}

type backgroundRejectedCommandRunner struct{ cmd *exec.Cmd }

func (r *backgroundRejectedCommandRunner) Prepare(commandSpec) (*commandExecution, error) {
	r.cmd = exec.Command(os.Args[0])
	return &commandExecution{Cmd: r.cmd}, nil
}

func TestBackgroundProcessRejectedStartClosesPipes(t *testing.T) {
	for _, reason := range []string{"closed", "session_limit", "global_limit"} {
		t.Run(reason, func(t *testing.T) {
			runner := &backgroundRejectedCommandRunner{}
			manager := newBackgroundProcessManager(time.Hour, runner)
			switch reason {
			case "closed":
				_ = manager.Close()
			case "session_limit":
				manager.runningBySession["session"] = backgroundProcessPerSessionLimit
			case "global_limit":
				manager.runningTotal = backgroundProcessGlobalLimit
			}
			_, err := manager.Start("session", "", "", t.TempDir(), nil, CommandSandboxBypass, "", nil, "unused", nil, "unused", "", false)
			if err == nil {
				t.Fatal("rejected command started")
			}
			if reader, ok := runner.cmd.Stdin.(*os.File); ok {
				defer reader.Close()
				if _, err := reader.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Fatalf("rejected start retained its pipe reader: %v", err)
				}
			}
		})
	}
}
