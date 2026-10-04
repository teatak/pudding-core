package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/plugin"
)

func TestBrowserToolResultPreservesWidgetScreenshot(t *testing.T) {
	home := t.TempDir()
	imageBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'E', 'N', 'D'}
	raw, err := json.Marshal(map[string]any{"content": []any{
		map[string]any{"type": "text", "text": `{"runtime":{"text":"Widget ready"}}`},
		map[string]any{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(imageBytes)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	result := browserToolResult(Call{SessionID: "sess_canvas", Name: "widget_inspect"}, raw, home)
	if !result.Ok || len(result.Attachments) != 1 || len(result.ContextAttachments) != 1 {
		t.Fatalf("widget inspection lost screenshot: %+v", result)
	}
	if result.ContextAttachments[0].AttachmentKey != result.Attachments[0].AttachmentKey || result.Attachments[0].Origin != attachment.OriginTool {
		t.Fatalf("screenshot not routed to model: %+v", result)
	}
	path, ok, err := attachment.NewService(home).Path("sess_canvas", result.Attachments[0].AttachmentKey)
	if err != nil || !ok {
		t.Fatalf("stored screenshot missing: ok=%v err=%v", ok, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(imageBytes) {
		t.Fatalf("stored screenshot changed: err=%v", err)
	}
}

func TestBrowserToolResultRejectsInspectionWithoutScreenshot(t *testing.T) {
	result := browserToolResult(Call{SessionID: "sess_canvas", Name: "widget_inspect"}, json.RawMessage(`{"content":[{"type":"text","text":"{}"}]}`), t.TempDir())
	if result.Ok || !strings.Contains(result.Content, "widget_screenshot_missing") {
		t.Fatalf("missing screenshot must be explicit: %+v", result)
	}
}

func TestBrowserMCPRunnerRegistersAndCallsWidgetTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	runner := NewBrowserMCPRunner(t.TempDir())
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
	runtimeCtx := plugin.WithRuntimeID(ctx, "runtime_a")

	var defsReady bool
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		defs, err := runner.Definitions(runtimeCtx, "sess_a")
		if err != nil {
			t.Fatalf("definitions: %v", err)
		}
		if HasDefinition(defs, "widget_draft_open") {
			defsReady = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !defsReady {
		t.Fatal("widget_draft_open was not registered")
	}
	if defs, err := runner.Definitions(ctx, "sess_a"); err != nil || HasDefinition(defs, "widget_draft_open") {
		t.Fatalf("runtime tool must not be exposed without runtime identity: defs=%+v err=%v", defs, err)
	}
	sessions := runner.BrowserSessions()
	if len(sessions) != 1 || sessions[0].ServerName != "test" || sessions[0].RuntimeID != "runtime_a" || len(sessions[0].Tools) != 1 {
		t.Fatalf("unexpected browser session snapshot: %+v", sessions)
	}
	if sessions[0].Tools[0].PluginID != "widget-authoring" {
		t.Fatalf("widget tool missing app ownership: %+v", sessions[0].Tools[0])
	}
	runtimePlugins, err := runner.ListRuntimeDefinitions(ctx, "runtime_a")
	if err != nil || len(runtimePlugins) != 1 || runtimePlugins[0].ID != "widget-authoring" || len(runtimePlugins[0].Tools) != 1 {
		t.Fatalf("unexpected runtime plugins: plugins=%+v err=%v", runtimePlugins, err)
	}
	skill, err := runner.ReadRuntimeSkill(runtimeCtx, "runtime_a", "widget-authoring", "widget-authoring")
	if err != nil || skill.Content != "# Widget Authoring" {
		t.Fatalf("unexpected runtime skill: skill=%+v err=%v", skill, err)
	}

	res := runner.Call(runtimeCtx, Call{
		SessionID: "sess_a",
		CallID:    "call_1",
		Name:      "widget_draft_open",
		Args:      json.RawMessage(`{"name":"Note"}`),
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
	runner := NewBrowserMCPRunner(t.TempDir())
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
		defsA, _ := runner.Definitions(plugin.WithRuntimeID(ctx, "runtime_a"), "sess_a")
		defsB, _ := runner.Definitions(plugin.WithRuntimeID(ctx, "runtime_b"), "sess_a")
		if HasDefinition(defsA, "widget_draft_open") && HasDefinition(defsB, "widget_draft_open") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	res := runner.Call(plugin.WithRuntimeID(ctx, "runtime_a"), Call{
		SessionID: "sess_a",
		CallID:    "call_a",
		Name:      "widget_draft_open",
		Args:      json.RawMessage(`{"name":"A"}`),
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

func TestBrowserToolArgsInjectsSessionForWidgetTools(t *testing.T) {
	for _, tt := range []struct {
		name string
		args json.RawMessage
	}{
		{name: "widget_list", args: json.RawMessage(`{}`)},
		{name: "widget_draft_open", args: json.RawMessage(`{"name":"Note","_pudding_session_id":"invented"}`)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args, err := browserToolArgs(Call{SessionID: "sess_widget", Name: tt.name, Args: tt.args})
			if err != nil {
				t.Fatalf("browserToolArgs: %v", err)
			}
			if args["_pudding_session_id"] != "sess_widget" {
				t.Fatalf("widget session identity must come from engine: %+v", args)
			}
		})
	}
}

func TestBrowserToolArgsInjectsSessionForUITools(t *testing.T) {
	args, err := browserToolArgs(Call{
		SessionID: "sess_b",
		TurnID:    "turn_b",
		CallID:    "question",
		Name:      RequestUserInput,
		Args:      json.RawMessage(`{"title":"New order","_pudding_request_id":"invented"}`),
	})
	if err != nil {
		t.Fatalf("browserToolArgs: %v", err)
	}
	if args["_pudding_session_id"] != "sess_b" {
		t.Fatalf("missing session injection: %+v", args)
	}
	if args["_pudding_request_id"] != "turn_b:question" {
		t.Fatalf("question identity must come from engine: %+v", args)
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
				"name":        "widget_draft_open",
				"description": "open widget draft",
				"capability":  "code",
				"pluginID":    "widget-authoring",
				"inputSchema": map[string]any{"type": "object"},
			}}}
		case "plugins/list":
			result = map[string]any{"plugins": []map[string]any{{
				"id":             "widget-authoring",
				"name":           "Widget Authoring",
				"requiredMode":   "code",
				"defaultSkillID": "widget-authoring",
				"skills": []map[string]any{{
					"id": "widget-authoring", "name": "Widget Authoring", "path": "skills/widget-authoring/SKILL.md",
				}},
			}}}
		case "plugins/skills/read":
			result = map[string]any{
				"id": "widget-authoring", "name": "Widget Authoring", "path": "skills/widget-authoring/SKILL.md", "content": "# Widget Authoring",
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
