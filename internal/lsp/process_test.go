package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

const helperEnv = "PUDDING_LSP_TEST_HELPER"
const helperIgnoreShutdownEnv = "PUDDING_LSP_TEST_IGNORE_SHUTDOWN"

func TestLSPHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	if err := runFakeLSP(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func TestProcessConcurrentRequestsAndDiagnostics(t *testing.T) {
	manager := newTestManager()
	t.Cleanup(func() { closeTestManager(t, manager) })
	spec := testServerSpec(t, "concurrent")
	process, err := manager.Acquire(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if process.PositionEncoding() != "utf-8" {
		t.Fatalf("position encoding = %q", process.PositionEncoding())
	}

	type result struct {
		Value string `json:"value"`
	}
	longDone := make(chan result, 1)
	longErr := make(chan error, 1)
	go func() {
		var got result
		longErr <- process.Request(context.Background(), "test/echo", map[string]any{"value": "long", "delay_ms": 80}, &got)
		longDone <- got
	}()
	var short result
	if err := process.Request(context.Background(), "test/echo", map[string]any{"value": "short"}, &short); err != nil {
		t.Fatal(err)
	}
	if short.Value != "short" {
		t.Fatalf("short result = %+v", short)
	}
	if err := <-longErr; err != nil {
		t.Fatal(err)
	}
	if got := <-longDone; got.Value != "long" {
		t.Fatalf("long result = %+v", got)
	}

	diagnosticURI := process.rootURI + "/main.go"
	deadline := time.Now().Add(time.Second)
	for {
		if snapshot, ok := process.Diagnostics(diagnosticURI); ok {
			if len(snapshot.Diagnostics) != 1 || snapshot.Diagnostics[0].Message != "fake diagnostic" {
				t.Fatalf("unexpected diagnostics: %+v", snapshot)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for diagnostics")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestProcessCancelKeepsSharedServerAlive(t *testing.T) {
	manager := newTestManager()
	t.Cleanup(func() { closeTestManager(t, manager) })
	process, err := manager.Acquire(context.Background(), testServerSpec(t, "cancel"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := process.Request(ctx, "test/wait", nil, nil); err != context.DeadlineExceeded {
		t.Fatalf("cancelled request error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		var seen bool
		if err := process.Request(context.Background(), "test/cancelSeen", nil, &seen); err != nil {
			t.Fatal(err)
		}
		if seen {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake server did not observe $/cancelRequest")
		}
		time.Sleep(5 * time.Millisecond)
	}
	var echo map[string]string
	if err := process.Request(context.Background(), "test/echo", map[string]string{"value": "still-alive"}, &echo); err != nil {
		t.Fatal(err)
	}
	if echo["value"] != "still-alive" {
		t.Fatalf("echo = %+v", echo)
	}
}

func TestManagerAppliesRequestTimeoutAndKeepsServerAlive(t *testing.T) {
	manager := NewManager(
		WithInitializeTimeout(time.Second),
		WithRequestTimeout(40*time.Millisecond),
		WithShutdownTimeout(2*time.Second),
		WithIdleTimeout(0),
		WithReapInterval(0),
	)
	t.Cleanup(func() { closeTestManager(t, manager) })
	spec := testServerSpec(t, "request-timeout")
	if err := manager.Request(context.Background(), spec, "test/wait", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("request timeout error = %v", err)
	}
	var echo map[string]string
	if err := manager.Request(context.Background(), spec, "test/echo", map[string]string{"value": "still-alive"}, &echo); err != nil {
		t.Fatal(err)
	}
	if echo["value"] != "still-alive" {
		t.Fatalf("echo = %+v", echo)
	}
}

func TestManagerRequestRestartsExitedProcess(t *testing.T) {
	manager := newTestManager()
	t.Cleanup(func() { closeTestManager(t, manager) })
	spec := testServerSpec(t, "request-restart")
	process, err := manager.Acquire(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	pid := process.PID()
	process.forceTerminate()
	select {
	case <-process.Done():
	case <-time.After(time.Second):
		t.Fatal("terminated process did not exit")
	}
	var echo map[string]string
	if err := manager.Request(context.Background(), spec, "test/echo", map[string]string{"value": "restarted"}, &echo); err != nil {
		t.Fatal(err)
	}
	restarted, err := manager.Acquire(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.PID() == pid || echo["value"] != "restarted" {
		t.Fatalf("process was not restarted: pid=%d echo=%+v", restarted.PID(), echo)
	}
}

func TestProcessRespondsToServerRequest(t *testing.T) {
	manager := newTestManager()
	t.Cleanup(func() { closeTestManager(t, manager) })
	process, err := manager.Acquire(context.Background(), testServerSpec(t, "server-request"))
	if err != nil {
		t.Fatal(err)
	}
	var ok bool
	if err := process.Request(context.Background(), "test/serverRequest", nil, &ok); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("server request response was not accepted")
	}
}

func TestManagerSynchronizesDocumentVersionsAndDiagnostics(t *testing.T) {
	manager := newTestManager()
	t.Cleanup(func() { closeTestManager(t, manager) })
	spec := testServerSpec(t, "documents")
	uri := fileURI(filepath.Join(spec.Key.LanguageRoot, "document.go"))
	first, err := manager.SyncDocument(context.Background(), spec, Document{URI: uri, LanguageID: "go", Text: "package demo\n"})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed || first.Version != 1 || first.PositionEncoding != "utf-8" {
		t.Fatalf("unexpected first state: %+v", first)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	snapshot, ok, err := manager.PublishedDiagnostics(ctx, spec, uri, first.PreviousDiagnosticGeneration)
	cancel()
	if err != nil || !ok || len(snapshot.Diagnostics) != 1 {
		t.Fatalf("published diagnostics = %+v, %v, %v", snapshot, ok, err)
	}
	second, err := manager.SyncDocument(context.Background(), spec, Document{URI: uri, LanguageID: "go", Text: "package demo\n"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed || second.Version != 1 {
		t.Fatalf("unchanged document state: %+v", second)
	}
	third, err := manager.SyncDocument(context.Background(), spec, Document{URI: uri, LanguageID: "go", Text: "package demo\n\nfunc F() {}\n"})
	if err != nil {
		t.Fatal(err)
	}
	if !third.Changed || third.Version != 2 {
		t.Fatalf("changed document state: %+v", third)
	}
	var counts map[string]int
	if err := manager.Request(context.Background(), spec, "test/documentState", nil, &counts); err != nil {
		t.Fatal(err)
	}
	if counts["opens"] != 1 || counts["changes"] != 1 {
		t.Fatalf("document notifications = %+v", counts)
	}
}

func TestManagerSingleflightCrashRestartAndShutdown(t *testing.T) {
	manager := newTestManager()
	spec := testServerSpec(t, "manager")
	const callers = 12
	processes := make(chan *Process, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			process, err := manager.Acquire(context.Background(), spec)
			processes <- process
			errs <- err
		}()
	}
	wg.Wait()
	close(processes)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	pid := 0
	var first *Process
	for process := range processes {
		if first == nil {
			first = process
			pid = process.PID()
		}
		if process != first || process.PID() != pid {
			t.Fatal("concurrent acquisition started more than one process")
		}
	}
	if err := first.Request(context.Background(), "test/crash", nil, nil); err == nil {
		t.Fatal("crash request should fail")
	}
	select {
	case <-first.Done():
	case <-time.After(time.Second):
		t.Fatal("crashed process did not exit")
	}
	restarted, err := manager.Acquire(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.PID() == pid {
		t.Fatal("manager did not restart crashed process")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-restarted.Done():
	case <-time.After(time.Second):
		t.Fatal("manager shutdown did not stop process")
	}
}

func TestManagerReapsIdleProcess(t *testing.T) {
	manager := NewManager(
		WithInitializeTimeout(time.Second),
		WithShutdownTimeout(2*time.Second),
		WithIdleTimeout(15*time.Millisecond),
		WithReapInterval(0),
	)
	t.Cleanup(func() { closeTestManager(t, manager) })
	process, err := manager.Acquire(context.Background(), testServerSpec(t, "idle"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(25 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.ReapIdle(ctx); err != nil {
		t.Fatal(err)
	}
	if manager.ProcessCount() != 0 {
		t.Fatal("idle process was not removed")
	}
	select {
	case <-process.Done():
	case <-time.After(time.Second):
		t.Fatal("idle process was not stopped")
	}
}

func TestManagerDefaultCapacityEvictsToThreeProcesses(t *testing.T) {
	manager := NewManager(
		WithInitializeTimeout(time.Second),
		WithShutdownTimeout(2*time.Second),
		WithIdleTimeout(0),
		WithReapInterval(0),
	)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close manager: %v", err)
		}
	})
	var first *Process
	for index := 0; index < 4; index++ {
		process, err := manager.Acquire(context.Background(), testServerSpec(t, "capacity-"+strconv.Itoa(index)))
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = process
		}
	}
	if manager.ProcessCount() != 3 {
		t.Fatalf("process count = %d", manager.ProcessCount())
	}
	select {
	case <-first.Done():
	case <-time.After(time.Second):
		t.Fatal("least recently used process was not evicted")
	}
}

func TestManagerForcesUnresponsiveProcessToExit(t *testing.T) {
	manager := NewManager(
		WithInitializeTimeout(time.Second),
		WithShutdownTimeout(40*time.Millisecond),
		WithIdleTimeout(0),
		WithReapInterval(0),
	)
	spec := testServerSpec(t, "forced-close")
	spec.Env = append(spec.Env, helperIgnoreShutdownEnv+"=1")
	process, err := manager.Acquire(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Close(ctx); err == nil {
		t.Fatal("unresponsive server shutdown should report a timeout")
	}
	select {
	case <-process.Done():
	case <-time.After(time.Second):
		t.Fatal("unresponsive process was not force-terminated")
	}
}

func newTestManager() *Manager {
	return NewManager(
		WithInitializeTimeout(time.Second),
		WithShutdownTimeout(2*time.Second),
		WithIdleTimeout(0),
		WithReapInterval(0),
	)
}

func closeTestManager(t *testing.T, manager *Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Errorf("close manager: %v", err)
	}
}

func testServerSpec(t *testing.T, kind string) ServerSpec {
	t.Helper()
	root := t.TempDir()
	return ServerSpec{
		Key:     ProcessKey{LanguageRoot: root, ServerKind: kind},
		Command: os.Args[0],
		Args:    []string{"-test.run=TestLSPHelperProcess"},
		Dir:     root,
		Env:     append(os.Environ(), helperEnv+"=1"),
	}
}

type fakeLSP struct {
	reader          *frameReader
	writeMu         sync.Mutex
	rootURI         string
	cancelSeen      bool
	serverRequest   map[string]json.RawMessage
	nextServerID    int
	ignoreShutdown  bool
	documentOpens   int
	documentChanges int
}

func runFakeLSP() error {
	server := &fakeLSP{
		reader:         newFrameReader(os.Stdin, DefaultMaxMessageBytes, DefaultMaxHeaderBytes),
		serverRequest:  map[string]json.RawMessage{},
		ignoreShutdown: os.Getenv(helperIgnoreShutdownEnv) == "1",
	}
	for {
		payload, err := server.reader.Read()
		if err != nil {
			return err
		}
		var message wireMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			return err
		}
		if message.Method == "" && len(message.ID) > 0 {
			server.handleClientResponse(message)
			continue
		}
		exit, err := server.handleClientMessage(message)
		if err != nil {
			return err
		}
		if exit {
			return nil
		}
	}
}

func (s *fakeLSP) handleClientMessage(message wireMessage) (bool, error) {
	switch message.Method {
	case "initialize":
		var params initializeParams
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return false, err
		}
		if !params.Capabilities.Workspace.Configuration || !params.Capabilities.TextDocument.PublishDiagnostics.RelatedInformation {
			return false, errors.New("client did not advertise required configuration and diagnostics capabilities")
		}
		s.rootURI = params.RootURI
		return false, s.respond(message.ID, map[string]any{
			"capabilities": map[string]any{"positionEncoding": "utf-8"},
		})
	case "initialized":
		return false, s.notify("textDocument/publishDiagnostics", map[string]any{
			"uri": s.rootURI + "/main.go",
			"diagnostics": []map[string]any{{
				"range": map[string]any{
					"start": map[string]int{"line": 0, "character": 1},
					"end":   map[string]int{"line": 0, "character": 2},
				},
				"severity": 1,
				"message":  "fake diagnostic",
			}},
		})
	case "test/echo":
		var params struct {
			Value   string `json:"value"`
			DelayMS int    `json:"delay_ms"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return false, err
		}
		go func(id json.RawMessage, value string, delay time.Duration) {
			time.Sleep(delay)
			_ = s.respond(id, map[string]string{"value": value})
		}(append(json.RawMessage(nil), message.ID...), params.Value, time.Duration(params.DelayMS)*time.Millisecond)
	case "test/wait":
		return false, nil
	case "$/cancelRequest":
		s.cancelSeen = true
	case "test/cancelSeen":
		return false, s.respond(message.ID, s.cancelSeen)
	case "test/serverRequest":
		s.nextServerID++
		serverID := "server-" + strconv.Itoa(s.nextServerID)
		s.serverRequest[serverID] = append(json.RawMessage(nil), message.ID...)
		return false, s.write(wireMessage{
			JSONRPC: "2.0",
			ID:      json.RawMessage(strconv.Quote(serverID)),
			Method:  "workspace/configuration",
			Params:  mustJSON(map[string]any{"items": []map[string]string{{"section": "one"}, {"section": "two"}}}),
		})
	case "test/crash":
		os.Exit(42)
	case "textDocument/didOpen":
		s.documentOpens++
		var params struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return false, err
		}
		return false, s.publishDocumentDiagnostic(params.TextDocument.URI)
	case "textDocument/didChange":
		s.documentChanges++
		var params struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return false, err
		}
		return false, s.publishDocumentDiagnostic(params.TextDocument.URI)
	case "test/documentState":
		return false, s.respond(message.ID, map[string]int{"opens": s.documentOpens, "changes": s.documentChanges})
	case "shutdown":
		if s.ignoreShutdown {
			return false, nil
		}
		return false, s.respond(message.ID, nil)
	case "exit":
		if s.ignoreShutdown {
			return false, nil
		}
		return true, nil
	}
	return false, nil
}

func (s *fakeLSP) publishDocumentDiagnostic(uri string) error {
	return s.notify("textDocument/publishDiagnostics", map[string]any{
		"uri": uri,
		"diagnostics": []map[string]any{{
			"range": map[string]any{
				"start": map[string]int{"line": 0, "character": 0},
				"end":   map[string]int{"line": 0, "character": 1},
			},
			"severity": 2,
			"message":  "document diagnostic",
		}},
	})
}

func (s *fakeLSP) handleClientResponse(message wireMessage) {
	var serverID string
	if err := json.Unmarshal(message.ID, &serverID); err != nil {
		return
	}
	clientID := s.serverRequest[serverID]
	delete(s.serverRequest, serverID)
	if clientID == nil || message.Error != nil {
		return
	}
	var values []any
	ok := json.Unmarshal(message.Result, &values) == nil && len(values) == 2
	_ = s.respond(clientID, ok)
}

func (s *fakeLSP) respond(id json.RawMessage, result any) error {
	return s.write(wireMessage{JSONRPC: "2.0", ID: append(json.RawMessage(nil), id...), Result: mustJSON(result)})
}

func (s *fakeLSP) notify(method string, params any) error {
	return s.write(wireMessage{JSONRPC: "2.0", Method: method, Params: mustJSON(params)})
}

func (s *fakeLSP) write(message wireMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return writeFrame(os.Stdout, payload, DefaultMaxMessageBytes)
}

func mustJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
