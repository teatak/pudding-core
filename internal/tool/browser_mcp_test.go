package tool

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestBrowserMCPRunnerRegistersAndCallsCanvasTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	runner := NewBrowserMCPRunner()
	srv := httptest.NewServer(runner)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	calls := make(chan map[string]any, 1)
	go fakeBrowserMCPServer(ctx, t, conn, calls)

	var defsReady bool
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		defs, err := runner.Definitions(ctx, "sess_a")
		if err != nil {
			t.Fatalf("definitions: %v", err)
		}
		if HasDefinition(defs, "canvas_markdown") {
			defsReady = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !defsReady {
		t.Fatal("canvas_markdown was not registered")
	}
	sessions := runner.BrowserSessions()
	if len(sessions) != 1 || sessions[0].ServerName != "test" || len(sessions[0].Tools) != 1 {
		t.Fatalf("unexpected browser session snapshot: %+v", sessions)
	}

	res := runner.Call(ctx, Call{
		SessionID: "sess_a",
		CallID:    "call_1",
		Name:      "canvas_markdown",
		Args:      json.RawMessage(`{"title":"Note","content":"Hello"}`),
	})
	if !res.Ok || !strings.Contains(res.Content, `"ok":true`) {
		t.Fatalf("unexpected result: %+v", res)
	}
	select {
	case got := <-calls:
		args, _ := got["arguments"].(map[string]any)
		if args["_pudding_session_id"] != "sess_a" {
			t.Fatalf("missing session injection: %+v", args)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for tool call")
	}
}

func fakeBrowserMCPServer(ctx context.Context, t *testing.T, conn *websocket.Conn, calls chan<- map[string]any) {
	t.Helper()
	for {
		_, payload, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var req struct {
			ID     string         `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(payload, &req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"serverInfo": map[string]any{"name": "test", "version": "1.0"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "canvas_markdown",
				"description": "canvas markdown",
				"capability":  "chat",
				"inputSchema": map[string]any{"type": "object"},
			}}}
		case "tools/call":
			calls <- req.Params
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": `{"ok":true}`}}}
		default:
			result = map[string]any{}
		}
		resp, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
		if err != nil {
			t.Errorf("encode response: %v", err)
			return
		}
		if err := conn.Write(ctx, websocket.MessageText, resp); err != nil {
			return
		}
	}
}
