package macsay

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/audio/tts"
)

func TestSpeakBeforeStartErrors(t *testing.T) {
	c, err := New(Config{BinaryPath: fakeSay(t, "exit 0\n")})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Speak(context.Background(), tts.Request{Text: "hello"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestSpeakRunsSayAndEmitsEvents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake binary is unix-only")
	}
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	bin := fakeSay(t, "printf '%s\\n' \"$@\" > "+shellQuote(argsFile)+"\n")
	c, err := New(Config{BinaryPath: bin, Voice: "TestVoice", Rate: 180})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Stop(context.Background())

	req := tts.Request{SessionID: "sess", TurnID: "turn", SegmentID: "seg", Text: "hello"}
	if err := c.Speak(context.Background(), req); err != nil {
		t.Fatal(err)
	}

	first := waitEvent(t, c.Events())
	if first.Kind != tts.EventStarted || first.SessionID != "sess" || first.TurnID != "turn" || first.SegmentID != "seg" {
		t.Fatalf("start event = %+v", first)
	}
	second := waitEvent(t, c.Events())
	if second.Kind != tts.EventEnded || second.SessionID != "sess" || second.TurnID != "turn" || second.SegmentID != "seg" {
		t.Fatalf("end event = %+v", second)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	got := string(args)
	for _, want := range []string{"-v\n", "TestVoice\n", "-r\n", "180\n", "hello\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("args %q missing %q", got, want)
		}
	}
}

func TestCancelKillsCurrentSay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake binary is unix-only")
	}
	bin := fakeSay(t, "sleep 5\n")
	c, err := New(Config{BinaryPath: bin})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Stop(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- c.Speak(context.Background(), tts.Request{TurnID: "turn", Text: "hello"})
	}()
	ev := waitEvent(t, c.Events())
	if ev.Kind != tts.EventStarted {
		t.Fatalf("event = %+v, want started", ev)
	}
	if err := c.Cancel(context.Background(), "turn"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Speak did not return after Cancel")
	}
}

func fakeSay(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake binary is unix-only")
	}
	path := filepath.Join(t.TempDir(), "say")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitEvent(t *testing.T, events <-chan tts.Event) tts.Event {
	t.Helper()
	select {
	case ev := <-events:
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return tts.Event{}
	}
}

func shellQuote(s string) string {
	out := "'"
	for _, r := range s {
		if r == '\'' {
			out += "'\\''"
			continue
		}
		out += string(r)
	}
	return out + "'"
}
