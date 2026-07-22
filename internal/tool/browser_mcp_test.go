package tool

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/teatak/pudding-core/internal/app"
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
	go fakeBrowserMCPServer(ctx, t, conn, "runtime_a", calls)
	runtimeCtx := app.WithRuntimeID(ctx, "runtime_a")

	var defsReady bool
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		defs, err := runner.Definitions(runtimeCtx, "sess_a")
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
	if defs, err := runner.Definitions(ctx, "sess_a"); err != nil || HasDefinition(defs, "canvas_markdown") {
		t.Fatalf("runtime tool must not be exposed without runtime identity: defs=%+v err=%v", defs, err)
	}
	sessions := runner.BrowserSessions()
	if len(sessions) != 1 || sessions[0].ServerName != "test" || sessions[0].RuntimeID != "runtime_a" || len(sessions[0].Tools) != 1 {
		t.Fatalf("unexpected browser session snapshot: %+v", sessions)
	}
	if sessions[0].Tools[0].AppID != "canvas" {
		t.Fatalf("canvas tool missing app ownership: %+v", sessions[0].Tools[0])
	}
	runtimeApps, err := runner.ListRuntimeDefinitions(ctx, "runtime_a")
	if err != nil || len(runtimeApps) != 1 || runtimeApps[0].ID != "canvas" || len(runtimeApps[0].Tools) != 1 {
		t.Fatalf("unexpected runtime apps: apps=%+v err=%v", runtimeApps, err)
	}
	skill, err := runner.ReadRuntimeSkill(runtimeCtx, "runtime_a", "canvas", "canvas")
	if err != nil || skill.Content != "# Canvas" {
		t.Fatalf("unexpected runtime skill: skill=%+v err=%v", skill, err)
	}

	res := runner.Call(runtimeCtx, Call{
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

func TestBrowserMCPRunnerRoutesToolsToExplicitRuntime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	runner := NewBrowserMCPRunner()
	srv := httptest.NewServer(runner)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	connA, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial runtime a: %v", err)
	}
	defer connA.Close(websocket.StatusNormalClosure, "")
	connB, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial runtime b: %v", err)
	}
	defer connB.Close(websocket.StatusNormalClosure, "")

	callsA := make(chan map[string]any, 1)
	callsB := make(chan map[string]any, 1)
	go fakeBrowserMCPServer(ctx, t, connA, "runtime_a", callsA)
	go fakeBrowserMCPServer(ctx, t, connB, "runtime_b", callsB)

	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		defsA, _ := runner.Definitions(app.WithRuntimeID(ctx, "runtime_a"), "sess_a")
		defsB, _ := runner.Definitions(app.WithRuntimeID(ctx, "runtime_b"), "sess_a")
		if HasDefinition(defsA, "canvas_markdown") && HasDefinition(defsB, "canvas_markdown") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	res := runner.Call(app.WithRuntimeID(ctx, "runtime_a"), Call{
		SessionID: "sess_a",
		CallID:    "call_a",
		Name:      "canvas_markdown",
		Args:      json.RawMessage(`{"title":"A"}`),
	})
	if !res.Ok {
		t.Fatalf("runtime a call failed: %+v", res)
	}
	select {
	case <-callsA:
	case <-ctx.Done():
		t.Fatal("runtime a did not receive its tool call")
	}
	select {
	case got := <-callsB:
		t.Fatalf("runtime b received runtime a call: %+v", got)
	default:
	}
}

func TestBrowserToolArgsInjectsSessionForUITools(t *testing.T) {
	args, err := browserToolArgs(Call{
		SessionID: "sess_b",
		Name:      RequestUserInput,
		Args:      json.RawMessage(`{"title":"New order"}`),
	})
	if err != nil {
		t.Fatalf("browserToolArgs: %v", err)
	}
	if args["_pudding_session_id"] != "sess_b" {
		t.Fatalf("missing session injection: %+v", args)
	}

	args, err = browserToolArgs(Call{
		SessionID: "sess_b",
		Name:      "browser_navigate",
		Args:      json.RawMessage(`{"url":"https://example.test"}`),
	})
	if err != nil {
		t.Fatalf("browserToolArgs: %v", err)
	}
	if _, ok := args["_pudding_session_id"]; ok {
		t.Fatalf("unexpected session injection for generic browser tool: %+v", args)
	}
}

func fakeBrowserMCPServer(ctx context.Context, t *testing.T, conn *websocket.Conn, runtimeID string, calls chan<- map[string]any) {
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
			result = map[string]any{
				"serverInfo":  map[string]any{"name": "test", "version": "1.0"},
				"runtimeInfo": map[string]any{"id": runtimeID, "type": "desktop"},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "canvas_markdown",
				"description": "canvas markdown",
				"capability":  "chat",
				"appID":       "canvas",
				"inputSchema": map[string]any{"type": "object"},
			}}}
		case "apps/list":
			result = map[string]any{"apps": []map[string]any{{
				"id":             "canvas",
				"name":           "Canvas",
				"requiredMode":   "chat",
				"defaultSkillID": "canvas",
				"skills": []map[string]any{{
					"id": "canvas", "name": "Canvas", "path": "skills/canvas/SKILL.md",
				}},
			}}}
		case "apps/skills/read":
			result = map[string]any{
				"id": "canvas", "name": "Canvas", "path": "skills/canvas/SKILL.md", "content": "# Canvas",
			}
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
