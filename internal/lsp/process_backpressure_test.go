package lsp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func stopHelperReading(t *testing.T, process *Process) {
	t.Helper()
	var stopped bool
	if err := process.Request(context.Background(), "test/stopReading", nil, &stopped); err != nil || !stopped {
		t.Fatalf("stop-reading handshake: %v, %v", stopped, err)
	}
}

func awaitWriter(t *testing.T, process *Process) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(process.writeMu) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("writer did not start")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestProcessBlockedPipeRespectsRequestAndDocumentCancellation(t *testing.T) {
	for _, document := range []bool{false, true} {
		name := "request"
		if document {
			name = "document"
		}
		t.Run(name, func(t *testing.T) {
			manager := newTestManager()
			defer closeTestManager(t, manager)
			spec := testServerSpec(t, "blocked-"+name)
			process, err := manager.Acquire(context.Background(), spec)
			if err != nil {
				t.Fatal(err)
			}
			var ready bool
			if err := process.Request(context.Background(), "test/stopAfterFramePrefix", nil, &ready); err != nil || !ready {
				t.Fatalf("partial-read handshake: %v, %v", ready, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				if document {
					_, err := manager.SyncDocument(ctx, spec, Document{URI: process.rootURI + "/large.ts", LanguageID: "typescript", Text: strings.Repeat("x", 2<<20)})
					result <- err
				} else {
					result <- process.Request(ctx, "test/large", map[string]string{"text": strings.Repeat("x", 2<<20)}, nil)
				}
			}()
			// Marshaling is slower under the race detector. The helper confirms
			// receiving a header and payload byte, so cancellation below tests
			// actual partial I/O instead of a deadline expiring before the send.
			barrierDeadline := time.Now().Add(3 * time.Second)
			for {
				if _, ok := process.Diagnostics(process.rootURI + "/blocked-frame"); ok {
					break
				}
				select {
				case err := <-result:
					t.Fatalf("large write ended before its partial-I/O barrier: %v", err)
				default:
				}
				if time.Now().After(barrierDeadline) {
					t.Fatal("helper did not receive the partial frame")
				}
				time.Sleep(time.Millisecond)
			}
			start := time.Now()
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
					t.Fatalf("blocked pipe ignored cancellation: %v after %s", err, time.Since(start))
				}
			case <-time.After(time.Second):
				t.Fatal("blocked pipe ignored cancellation")
			}
			if process.Alive() || process.Pending() != 0 {
				t.Fatalf("partial frame left a reusable process: alive=%v pending=%d", process.Alive(), process.Pending())
			}
		})
	}
}

func TestProcessCancellationBeforeSendPreservesSharedServer(t *testing.T) {
	manager := newTestManager()
	defer closeTestManager(t, manager)
	spec := testServerSpec(t, "cancel-before-send")
	process, err := manager.Acquire(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := process.Request(ctx, "test/large", map[string]string{"text": strings.Repeat("x", 2<<20)}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-send request cancellation = %v", err)
	}
	if _, err := manager.SyncDocument(ctx, spec, Document{URI: process.rootURI + "/large.ts", LanguageID: "typescript", Text: strings.Repeat("x", 2<<20)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-send document cancellation = %v", err)
	}
	if !process.Alive() || process.Pending() != 0 {
		t.Fatalf("pre-send cancellation discarded a usable process: alive=%v pending=%d", process.Alive(), process.Pending())
	}
	var echo map[string]string
	if err := process.Request(context.Background(), "test/echo", map[string]string{"value": "usable"}, &echo); err != nil || echo["value"] != "usable" {
		t.Fatalf("server could not be reused after pre-send cancellation: %v, %+v", err, echo)
	}
}

func TestProcessCancellationAndShutdownBypassBlockedWriter(t *testing.T) {
	manager := newTestManager()
	defer closeTestManager(t, manager)
	process, err := manager.Acquire(context.Background(), testServerSpec(t, "cancel-backpressure"))
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancelWait := context.WithCancel(context.Background())
	defer cancelWait()
	waiting := make(chan error, 1)
	go func() { waiting <- process.Request(waitCtx, "test/wait", nil, nil) }()
	// A pending entry is installed before its frame is written. Require the
	// helper to acknowledge receiving test/wait before blocking its input.
	barrierCtx, cancelBarrier := context.WithTimeout(context.Background(), time.Second)
	defer cancelBarrier()
	for {
		var seen bool
		if err := process.Request(barrierCtx, "test/waitSeen", nil, &seen); err != nil {
			t.Fatalf("waiting request was not received: %v", err)
		}
		if seen {
			break
		}
		time.Sleep(time.Millisecond)
	}
	stopHelperReading(t, process)
	written := make(chan error, 1)
	go func() { written <- process.Notify("test/large", map[string]string{"text": strings.Repeat("x", 2<<20)}) }()
	awaitWriter(t, process)
	cancelWait()
	select {
	case err := <-waiting:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel result = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel notification blocked the request's return")
	}
	// A queued write's timeout sent no frame and must not interrupt the first writer.
	queuedCtx, cancelQueued := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelQueued()
	if err := process.Request(queuedCtx, "test/echo", nil, nil); !errors.Is(err, context.DeadlineExceeded) || !process.Alive() {
		t.Fatalf("queued request cancellation corrupted process state: %v", err)
	}
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancelClose()
	start := time.Now()
	if err := process.Close(closeCtx); err == nil || time.Since(start) > time.Second {
		t.Fatalf("shutdown waited for a blocked writer: %v after %s", err, time.Since(start))
	}
	select {
	case err := <-written:
		if err == nil {
			t.Fatal("interrupted write reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("closing stdin did not release the writer")
	}
	if process.Alive() || process.Pending() != 0 {
		t.Fatalf("shutdown left pending work: alive=%v pending=%d", process.Alive(), process.Pending())
	}
}
