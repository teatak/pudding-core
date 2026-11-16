package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

const debugMCPTestPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jGZkAAAAASUVORK5CYII="

type debugMCPTestServer struct {
	server  *httptest.Server
	store   *storetest.Store
	browser *tool.BrowserMCPRunner
	home    string
}

func newDebugMCPTestServer(t *testing.T) debugMCPTestServer {
	t.Helper()
	st := storetest.New(t)
	if err := st.PutProviderProfile(context.Background(), &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "mock-model"}}}); err != nil {
		t.Fatal(err)
	}
	homeDir := t.TempDir()
	hub := event.NewHub()
	browser := tool.NewBrowserMCPRunner(homeDir)
	builtins := tool.NewBuiltinRunner(tool.WithHomeDir(homeDir))
	t.Cleanup(func() { _ = builtins.Close() })
	plugins := plugin.NewService(homeDir, st).WithRuntimeSource(browser).WithBuiltinToolAvailability(builtins.ToolAvailable)
	runner := tool.NewMultiRunner(builtins, browser)
	eng := engine.New(st, hub, registry.Static(mock.New()), st, engine.WithAttachmentHome(homeDir), engine.WithTools(runner), engine.WithPlugins(plugins))
	srv := httptest.NewServer(New(eng, st, st, hub).WithHome(homeDir).WithPlugins(plugins).WithBrowserMCP(browser).Handler(testToken, nil))
	t.Cleanup(srv.Close)
	return debugMCPTestServer{server: srv, store: st, browser: browser, home: homeDir}
}

func debugMCPRequestForTest(t *testing.T, srvURL, method string, params any, notification bool) (json.RawMessage, http.Header) {
	t.Helper()
	payload := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if !notification {
		payload["id"] = "test-request"
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequest(http.MethodPost, srvURL+"/mcp", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("MCP-Protocol-Version", debugMCPProtocolVersion)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if notification {
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("notification HTTP status = %d", resp.StatusCode)
		}
		return nil, resp.Header
	}
	var body struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || len(body.Error) > 0 || body.ID != "test-request" {
		t.Fatalf("%s: HTTP %d, response %+v", method, resp.StatusCode, body)
	}
	return body.Result, resp.Header
}

func debugMCPListForTest(t *testing.T, url string) map[string]debugMCPTool {
	t.Helper()
	data, _ := debugMCPRequestForTest(t, url, "tools/list", map[string]any{}, false)
	var result struct {
		Tools []debugMCPTool `json:"tools"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	tools := make(map[string]debugMCPTool, len(result.Tools))
	for _, item := range result.Tools {
		if !debugMCPObject(item.InputSchema) {
			t.Fatalf("invalid tool schema for %s: %s", item.Name, item.InputSchema)
		}
		tools[item.Name] = item
	}
	return tools
}

func debugMCPCallForTest(t *testing.T, url, name string, args any) debugMCPResult {
	t.Helper()
	data, _ := debugMCPRequestForTest(t, url, "tools/call", map[string]any{"name": name, "arguments": args}, false)
	var result debugMCPResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("%s failed: %+v", name, result.Content)
	}
	return result
}

func debugMCPBindingForTest(t *testing.T, url string, args any) map[string]any {
	t.Helper()
	result := debugMCPCallForTest(t, url, debugMCPSessionTool, args)
	var binding map[string]any
	if len(result.Content) != 1 {
		t.Fatalf("unexpected binding content: %+v", result.Content)
	}
	text, _ := result.Content[0]["text"].(string)
	if err := json.Unmarshal([]byte(text), &binding); err != nil {
		t.Fatal(err)
	}
	return binding
}

func TestDebugMCPListsTools(t *testing.T) {
	fixture := newDebugMCPTestServer(t)
	data, headers := debugMCPRequestForTest(t, fixture.server.URL, "initialize", map[string]any{
		"protocolVersion": debugMCPProtocolVersion, "capabilities": map[string]any{},
		"clientInfo": map[string]string{"name": "test", "version": "1"},
	}, false)
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools struct {
				ListChanged bool `json:"listChanged"`
			} `json:"tools"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(data, &initialized); err != nil {
		t.Fatal(err)
	}
	if initialized.ProtocolVersion != debugMCPProtocolVersion || initialized.Capabilities.Tools.ListChanged || headers.Get("Mcp-Session-Id") != "" {
		t.Fatalf("unexpected initialization: %s, headers %v", data, headers)
	}
	debugMCPRequestForTest(t, fixture.server.URL, "notifications/initialized", map[string]any{}, true)
	debugMCPRequestForTest(t, fixture.server.URL, "ping", map[string]any{}, false)
	listed := debugMCPListForTest(t, fixture.server.URL)
	for _, name := range []string{tool.TimeGetCurrent, tool.FileList, tool.PluginLoad, debugMCPSessionTool} {
		if _, ok := listed[name]; !ok {
			t.Fatalf("missing tool %s", name)
		}
	}
	first := debugMCPBindingForTest(t, fixture.server.URL, map[string]any{})
	second := debugMCPBindingForTest(t, fixture.server.URL, map[string]any{})
	if first["sessionID"] != second["sessionID"] || first["mode"] != "code" || first["debug"] != true {
		t.Fatalf("debug session not reused: first=%v second=%v", first, second)
	}
}

func TestDebugMCPCallsBuiltinTools(t *testing.T) {
	fixture := newDebugMCPTestServer(t)
	result := debugMCPCallForTest(t, fixture.server.URL, tool.TimeGetCurrent, map[string]string{"timezone": "UTC"})
	if len(result.Content) != 1 || result.Content[0]["type"] != "text" {
		t.Fatalf("time content = %+v", result.Content)
	}
	binding := debugMCPBindingForTest(t, fixture.server.URL, map[string]any{})
	sessionID := binding["sessionID"].(string)
	image, err := base64.StdEncoding.DecodeString(debugMCPTestPNG)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := attachment.NewService(fixture.home).StoreReader(sessionID, "test.png", "image/png", bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	media := debugMCPCallForTest(t, fixture.server.URL, tool.MediaRead, map[string]string{"source": "attachment", "attachmentKey": stored.AttachmentKey})
	images := 0
	for _, content := range media.Content {
		if content["type"] == "image" {
			images++
			if content["mimeType"] != "image/png" || content["data"] != debugMCPTestPNG {
				t.Fatalf("media image changed: %+v", content)
			}
		}
	}
	if images != 1 {
		t.Fatalf("media returned %d images", images)
	}
	session, err := fixture.store.GetSession(context.Background(), sessionID)
	if err != nil || session.Running {
		t.Fatalf("tool turn not finished: session=%+v err=%v", session, err)
	}
	messages, err := fixture.store.ListMessages(context.Background(), sessionID, 0)
	if err != nil || len(messages) == 0 {
		t.Fatalf("tool calls did not persist ordinary history: count=%d err=%v", len(messages), err)
	}
}

func TestDebugMCPCallsDesktopTool(t *testing.T) {
	fixture := newDebugMCPTestServer(t)
	if _, ok := debugMCPListForTest(t, fixture.server.URL)["widget_inspect"]; ok {
		t.Fatal("widget tool appeared before Desktop connected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(fixture.server.URL, "http")+"/mcp/ws?token="+testToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	calls := make(chan map[string]any, 1)
	go debugMCPDesktopFixture(ctx, conn, calls)
	// Wait only for the ordinary asynchronous runtime registration handshake.
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		definitions, err := fixture.browser.ListRuntimeDefinitions(ctx, "desktop-test")
		if err != nil {
			t.Fatal(err)
		}
		if len(definitions) > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("Desktop registration did not finish")
		case <-ticker.C:
		}
	}
	if _, ok := debugMCPListForTest(t, fixture.server.URL)["widget_inspect"]; !ok {
		t.Fatal("Desktop tool missing after registration")
	}
	binding := debugMCPBindingForTest(t, fixture.server.URL, map[string]any{})
	if binding["runtimeID"] != "desktop-test" {
		t.Fatalf("runtime was not selected: %v", binding)
	}
	result := debugMCPCallForTest(t, fixture.server.URL, "widget_inspect", map[string]string{"id": "fixture-widget"})
	images := 0
	for _, content := range result.Content {
		if content["type"] == "image" {
			images++
			if content["data"] != debugMCPTestPNG || content["mimeType"] != "image/png" {
				t.Fatalf("Desktop PNG changed: %+v", content)
			}
		}
	}
	if images != 1 {
		t.Fatalf("Desktop returned %d images instead of one", images)
	}
	select {
	case call := <-calls:
		args := call["arguments"].(map[string]any)
		if args["_pudding_session_id"] != binding["sessionID"] || args["id"] != "fixture-widget" {
			t.Fatalf("wrong Desktop tool scope: %v", args)
		}
	case <-ctx.Done():
		t.Fatal("Desktop did not receive tools/call")
	}
	debugMCPCallForTest(t, fixture.server.URL, tool.PluginUnload, map[string]string{"plugin_id": "widget-authoring"})
	if _, ok := debugMCPListForTest(t, fixture.server.URL)["widget_inspect"]; ok {
		t.Fatal("tools/list reloaded an explicitly unloaded plugin")
	}
	debugMCPCallForTest(t, fixture.server.URL, tool.PluginLoad, map[string]string{"plugin_id": "widget-authoring"})
	if _, ok := debugMCPListForTest(t, fixture.server.URL)["widget_inspect"]; !ok {
		t.Fatal("explicitly reloaded Desktop tool missing")
	}
}

func debugMCPDesktopFixture(ctx context.Context, conn *websocket.Conn, calls chan<- map[string]any) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if json.Unmarshal(data, &request) != nil || len(request.ID) == 0 {
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]string{"name": "Desktop test", "version": "1"}, "runtimeInfo": map[string]string{"id": "desktop-test", "type": "desktop"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "widget_inspect", "description": "Inspect the fixture widget", "capability": "code", "pluginID": "widget-authoring", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]string{"type": "string"}}}}}}
		case "plugins/list":
			result = map[string]any{"plugins": []any{map[string]string{"id": "widget-authoring", "name": "Widget Authoring", "requiredMode": "code"}}}
		case "tools/call":
			select {
			case calls <- request.Params:
			case <-ctx.Done():
				return
			}
			result = debugMCPResult{Content: []map[string]any{{"type": "text", "text": `{"ok":true,"widgetID":"fixture-widget"}`}, {"type": "image", "mimeType": "image/png", "data": debugMCPTestPNG}}}
		default:
			result = map[string]any{}
		}
		response, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		if err != nil || conn.Write(ctx, websocket.MessageText, response) != nil {
			return
		}
	}
}

func TestDebugMCPSwitchesSession(t *testing.T) {
	fixture := newDebugMCPTestServer(t)
	ctx := context.Background()
	debug := debugMCPBindingForTest(t, fixture.server.URL, map[string]any{})
	profile, err := fixture.store.OpenAssistant(ctx, "Puddy", "mock", "mock-model", store.AssistantAppearance{"version": 1})
	if err != nil {
		t.Fatal(err)
	}
	bound := debugMCPBindingForTest(t, fixture.server.URL, map[string]string{"sessionID": "personal"})
	if bound["sessionID"] != profile.Assistant.SessionID || bound["mode"] != "chat" || bound["debug"] != false {
		t.Fatalf("wrong assistant binding: %v", bound)
	}
	if _, ok := debugMCPListForTest(t, fixture.server.URL)[tool.AssistantRename]; !ok {
		t.Fatal("assistant tools missing from its own session")
	}
	debugMCPCallForTest(t, fixture.server.URL, tool.AssistantRename, map[string]string{"name": "MCP Puddy"})
	profile, err = fixture.store.GetAssistant(ctx)
	if err != nil || profile.Assistant.Name != "MCP Puddy" {
		t.Fatalf("assistant tool did not use bound session: profile=%+v err=%v", profile, err)
	}
	bound = debugMCPBindingForTest(t, fixture.server.URL, map[string]string{"sessionID": profile.Assistant.SessionID, "mode": "work"})
	session, err := fixture.store.GetSession(ctx, profile.Assistant.SessionID)
	if err != nil || session.ActiveMode != store.ModeWork || session.ModeLease != store.ModeLeaseSession || len(session.LoadedPluginIDs) != 0 || bound["mode"] != "work" {
		t.Fatalf("existing session mode/plugin state = %+v, err=%v", session, err)
	}
	bound = debugMCPBindingForTest(t, fixture.server.URL, map[string]string{"sessionID": "debug", "mode": "chat"})
	if bound["sessionID"] != debug["sessionID"] || bound["mode"] != "chat" {
		t.Fatalf("debug binding did not switch back: %v", bound)
	}
	if _, ok := debugMCPListForTest(t, fixture.server.URL)[tool.FileList]; ok {
		t.Fatal("Code tool remained visible after changing mode to Chat")
	}
}
