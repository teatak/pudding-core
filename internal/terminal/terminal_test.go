package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestManagerStreamsPTYAndScopesTerminals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ConPTY is not implemented")
	}
	metadata := memstore.New()
	ctx := context.Background()
	for _, sessionID := range []string{"sess_a", "sess_b"} {
		if err := metadata.CreateSession(ctx, &store.Session{ID: sessionID, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}
	manager, err := NewManager(metadata)
	if err != nil {
		t.Fatal(err)
	}
	manager.shellPath = "/bin/cat"
	t.Cleanup(func() { _ = manager.Close() })

	item, err := manager.Create(ctx, "sess_a", CreateOptions{CWD: t.TempDir(), Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if item.SessionID != "sess_a" || item.Status != store.TerminalRunning {
		t.Fatalf("terminal = %+v", item)
	}
	if other, err := manager.List(ctx, "sess_b"); err != nil || len(other) != 0 {
		t.Fatalf("other session terminals = %+v, err=%v", other, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		manager.ServeWebSocket(w, request, "sess_a", item.ID)
	}))
	defer server.Close()

	readCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(readCtx, "ws"+server.URL[len("http"):], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	typ, payload, err := conn.Read(readCtx)
	if err != nil || typ != websocket.MessageText {
		t.Fatalf("initial status type=%v payload=%q err=%v", typ, payload, err)
	}
	var status statusMessage
	if err := json.Unmarshal(payload, &status); err != nil || status.Status != store.TerminalRunning {
		t.Fatalf("initial status = %+v, err=%v", status, err)
	}

	marker := []byte("pudding-terminal-test")
	input, _ := json.Marshal(clientMessage{Type: "input", Data: string(marker) + "\n"})
	if err := conn.Write(readCtx, websocket.MessageText, input); err != nil {
		t.Fatal(err)
	}
	var output []byte
	for !bytes.Contains(output, marker) {
		typ, payload, err = conn.Read(readCtx)
		if err != nil {
			t.Fatal(err)
		}
		if typ == websocket.MessageBinary {
			output = append(output, payload...)
		}
	}

	if err := manager.Delete(ctx, "sess_a", item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Get(ctx, "sess_a", item.ID); err != store.ErrNotFound {
		t.Fatalf("deleted terminal err=%v", err)
	}
}

func TestManagerRejectsProjectCWDOutsideRoots(t *testing.T) {
	metadata := memstore.New()
	ctx := context.Background()
	root := t.TempDir()
	project := &store.Project{ID: "project", RootDirs: []string{root}, ApprovalMode: store.ApprovalAuto}
	if err := metadata.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := metadata.CreateSession(ctx, &store.Session{ID: "sess", Provider: "mock", Model: "mock", ProjectID: project.ID}); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(metadata)
	if err != nil {
		t.Fatal(err)
	}
	manager.shellPath = "/bin/cat"
	t.Cleanup(func() { _ = manager.Close() })
	if _, err := manager.Create(ctx, "sess", CreateOptions{CWD: t.TempDir()}); err != ErrInvalidCWD {
		t.Fatalf("outside project cwd err=%v", err)
	}
}

func TestManagerUsesProjectNameAsTerminalTitle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ConPTY is not implemented")
	}
	metadata := memstore.New()
	ctx := context.Background()
	root := t.TempDir()
	project := &store.Project{ID: "project", Name: "transformer", RootDirs: []string{root}, ApprovalMode: store.ApprovalAuto}
	if err := metadata.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := metadata.CreateSession(ctx, &store.Session{ID: "sess", Provider: "mock", Model: "mock", ProjectID: project.ID}); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(metadata)
	if err != nil {
		t.Fatal(err)
	}
	manager.shellPath = "/bin/cat"
	t.Cleanup(func() { _ = manager.Close() })

	item, err := manager.Create(ctx, "sess", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "transformer" {
		t.Fatalf("terminal title = %q", item.Title)
	}
}

func TestManagerDoesNotRestoreTerminalsAcrossRestarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ConPTY is not implemented")
	}
	metadata := memstore.New()
	ctx := context.Background()
	if err := metadata.CreateSession(ctx, &store.Session{ID: "sess", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(metadata)
	if err != nil {
		t.Fatal(err)
	}
	manager.shellPath = "/bin/cat"
	if _, err := manager.Create(ctx, "sess", CreateOptions{CWD: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewManager(metadata)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	items, err := restarted.List(ctx, "sess")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("restarted manager restored temporary terminals: %+v", items)
	}
}
