package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	terminalsvc "github.com/teatak/pudding-core/internal/terminal"
)

func TestTerminalWebSocketUsesRawResponseWriter(t *testing.T) {
	metadata := memstore.New()
	if err := metadata.CreateSession(context.Background(), &store.Session{ID: "sess", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	hub := event.NewHub()
	eng := engine.New(metadata, hub, registry.Static(mock.New()), metadata)
	server := httptest.NewServer(New(eng, metadata, metadata, hub).WithTerminals(fakeTerminalService{}).Handler(testToken, nil))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/sessions/sess/terminals/term/ws?token=" + testToken
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	typ, payload, err := conn.Read(ctx)
	if err != nil || typ != websocket.MessageText || string(payload) != "terminal-ws-ok" {
		t.Fatalf("type=%v payload=%q err=%v", typ, payload, err)
	}
}

type fakeTerminalService struct{}

func (fakeTerminalService) Create(context.Context, string, terminalsvc.CreateOptions) (*store.Terminal, error) {
	return nil, terminalsvc.ErrUnavailable
}

func (fakeTerminalService) Get(context.Context, string, string) (*store.Terminal, error) {
	return nil, store.ErrNotFound
}

func (fakeTerminalService) List(context.Context, string) ([]*store.Terminal, error) {
	return nil, nil
}

func (fakeTerminalService) Delete(context.Context, string, string) error { return nil }

func (fakeTerminalService) ServeWebSocket(w http.ResponseWriter, request *http.Request, _, _ string) {
	conn, err := websocket.Accept(w, request, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	_ = conn.Write(request.Context(), websocket.MessageText, []byte("terminal-ws-ok"))
}

func (fakeTerminalService) CloseSession(string) {}
func (fakeTerminalService) Close() error        { return nil }
