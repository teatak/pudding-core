package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/browser"
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
	engine  *engine.Engine
	hub     *event.Hub
	server  *httptest.Server
	store   *storetest.Store
	browser *tool.BrowserMCPRunner
	home    string
}

func newDebugMCPTestServer(t *testing.T, options ...tool.BuiltinOption) debugMCPTestServer {
	t.Helper()
	st := storetest.New(t)
	if err := st.PutProviderProfile(context.Background(), &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "mock-model"}}}); err != nil {
		t.Fatal(err)
	}
	homeDir := t.TempDir()
	hub := event.NewHub()
	browser := tool.NewBrowserMCPRunner(homeDir)
	builtins := tool.NewBuiltinRunner(append([]tool.BuiltinOption{tool.WithHomeDir(homeDir)}, options...)...)
	t.Cleanup(func() { _ = builtins.Close() })
	plugins := plugin.NewService(homeDir, st).WithRuntimeSource(browser).WithBuiltinToolAvailability(builtins.ToolAvailable)
	runner := tool.NewMultiRunner(builtins, browser)
	eng := engine.New(st, hub, registry.Static(mock.New()), st, engine.WithAttachmentHome(homeDir), engine.WithTools(runner), engine.WithPlugins(plugins))
	srv := httptest.NewServer(New(eng, st, st, hub).WithHome(homeDir).WithPlugins(plugins).WithBrowserMCP(browser).Handler(testToken, nil))
	t.Cleanup(srv.Close)
	return debugMCPTestServer{engine: eng, hub: hub, server: srv, store: st, browser: browser, home: homeDir}
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
	return debugMCPListQueryForTest(t, url, "")
}

func debugMCPListQueryForTest(t *testing.T, url, query string) map[string]debugMCPTool {
	t.Helper()
	endpoint := url + "/mcp"
	if query != "" {
		endpoint += "?" + query
	}
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "test-request", "method": "tools/list", "params": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
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
	var body struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || len(body.Error) > 0 || body.ID != "test-request" {
		t.Fatalf("tools/list: HTTP %d, response %+v", resp.StatusCode, body)
	}
	var result struct {
		Tools []debugMCPTool `json:"tools"`
	}
	if err := json.Unmarshal(body.Result, &result); err != nil {
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
	for _, name := range []string{tool.TimeGetCurrent, tool.FileList, tool.PluginLoad, debugMCPSessionTool, debugMCPToolSearchTool, debugMCPToolCallTool} {
		if _, ok := listed[name]; !ok {
			t.Fatalf("missing tool %s", name)
		}
	}
	if _, ok := listed[tool.MediaRead]; ok {
		t.Fatal("non-core tool should not appear in default compact tools/list")
	}
	allListed := debugMCPListQueryForTest(t, fixture.server.URL, "tools=all")
	if _, ok := allListed[tool.MediaRead]; !ok {
		t.Fatal("non-core tool missing from tools=all list")
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
	if _, ok := debugMCPListQueryForTest(t, fixture.server.URL, "tools=all")["widget_inspect"]; ok {
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
	if _, ok := debugMCPListQueryForTest(t, fixture.server.URL, "tools=all")["widget_inspect"]; !ok {
		t.Fatal("Desktop tool missing after registration")
	}
	if _, ok := debugMCPListForTest(t, fixture.server.URL)["widget_inspect"]; ok {
		t.Fatal("Desktop widget tool should be discovered via pudding_tool_search in compact mode")
	}
	searchRes := debugMCPCallForTest(t, fixture.server.URL, debugMCPToolSearchTool, map[string]any{"group": "widget"})
	var searched struct {
		TotalMatches int `json:"totalMatches"`
		Tools        []struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(searchRes.Content[0]["text"].(string)), &searched); err != nil {
		t.Fatal(err)
	}
	if searched.TotalMatches != 1 || len(searched.Tools) != 1 || searched.Tools[0].Name != "widget_inspect" || !debugMCPObject(searched.Tools[0].InputSchema) {
		t.Fatalf("unexpected widget search result: %+v", searched)
	}
	binding := debugMCPBindingForTest(t, fixture.server.URL, map[string]any{})
	if binding["runtimeID"] != "desktop-test" {
		t.Fatalf("runtime was not selected: %v", binding)
	}
	result := debugMCPCallForTest(t, fixture.server.URL, debugMCPToolCallTool, map[string]any{"name": "widget_inspect", "arguments": map[string]string{"id": "fixture-widget"}})
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
	if _, ok := debugMCPListQueryForTest(t, fixture.server.URL, "tools=all")["widget_inspect"]; ok {
		t.Fatal("tools/list reloaded an explicitly unloaded plugin")
	}
	debugMCPCallForTest(t, fixture.server.URL, tool.PluginLoad, map[string]string{"plugin_id": "widget-authoring"})
	if _, ok := debugMCPListQueryForTest(t, fixture.server.URL, "tools=all")["widget_inspect"]; !ok {
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

func externalMCPPostForTest(t *testing.T, baseURL string) func(string, string, any, int) map[string]any {
	t.Helper()
	return func(path, credential string, input any, expected int) map[string]any {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest("POST", baseURL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Pudding-Binding", credential)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != expected {
			t.Fatalf("%s status %d: %v", path, resp.StatusCode, out["error"])
		}
		return out
	}
}

func approveExternalMCPTurnForTest(t *testing.T, fixture debugMCPTestServer, sessionID string) <-chan error {
	t.Helper()
	events, unsubscribe := fixture.hub.Subscribe(sessionID)
	t.Cleanup(unsubscribe)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	approved := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				approved <- ctx.Err()
				return
			case ev := <-events:
				if ev.Kind == event.ApprovalRequested {
					approved <- fixture.engine.ApproveApproval(ctx, sessionID, ev.ApprovalID, engine.ApprovalScopeTurn, nil)
					return
				}
			}
		}
	}()
	return approved
}

func TestExternalMCPPageEditUsesOneBoundTurn(t *testing.T) {
	fixture := newDebugMCPTestServer(t)
	ctx := context.Background()
	if err := fixture.store.CreateSession(ctx, &store.Session{ID: "bound", Provider: "mock", Model: "mock-model", ActiveMode: store.ModeChat}); err != nil {
		t.Fatal(err)
	}
	post := externalMCPPostForTest(t, fixture.server.URL)
	bind := func(branded bool) string {
		invite := post("/sessions/bound/mcp-agents", "", nil, 201)["invite"]
		in := map[string]any{"invite": invite, "name": "Example Agent"}
		if branded {
			in["title"] = "Example App"
			in["iconPNGBase64"] = debugMCPTestPNG
		} else {
			in["icons"] = []map[string]string{{"src": "data:image/png;base64," + debugMCPTestPNG}}
		}
		credential := post("/agent-mcp/connect", "", in, 200)["credential"].(string)
		post("/agent-mcp/connect", "", in, 404)
		return credential
	}
	credential, other := bind(true), bind(false)
	bindings, err := fixture.store.ListAgentBindings(ctx, "bound")
	if err != nil || len(bindings) != 2 || bindings[0].Title != "Example App" || bindings[0].IconPNGBase64 != debugMCPTestPNG ||
		bindings[1].Title != "" || bindings[1].IconPNGBase64 != "" {
		t.Fatalf("stored client identities: %+v, err=%v", bindings, err)
	}
	turn := post("/agent-mcp/begin", credential, map[string]string{"task": "Edit a page"}, 200)["turnID"].(string)
	execute := func(name string, args map[string]any) map[string]any {
		t.Helper()
		out := post("/agent-mcp/call", credential, map[string]any{"turnID": turn, "tool": name, "arguments": args}, 200)
		content := out["content"].([]any)[0].(map[string]any)["text"].(string)
		if out["isError"] == true {
			t.Fatalf("%s: %s", name, content)
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(content), &fields); err != nil {
			t.Fatal(err)
		}
		return fields
	}
	approved := approveExternalMCPTurnForTest(t, fixture, "bound")
	execute(tool.RequestCapability, map[string]any{"targetMode": "code", "reason": "Edit page for this request", "projectDirs": []string{t.TempDir()}})
	if err := <-approved; err != nil {
		t.Fatal(err)
	}
	for binding, mode := range map[string]string{credential: "code", other: "chat"} {
		state := post("/agent-mcp/tools", binding, nil, 200)
		if state["mode"] != mode {
			t.Fatalf("binding mode %v, want %s", state["mode"], mode)
		}
	}
	if guide := execute(tool.PluginLoad, map[string]any{"plugin_id": "page-authoring"}); guide["content"] == nil || guide["instructionStatus"] != "current" {
		t.Fatal("missing current plugin body")
	}
	page := execute(tool.PageCreate, map[string]any{"name": "Test", "body": ""})
	op := execute(tool.PageEdit, map[string]any{"item_id": page["itemID"], "action": "begin", "from": 0, "to": 0, "expected_hash": page["contentHash"]})
	execute(tool.PageEdit, map[string]any{"item_id": page["itemID"], "action": "commit", "operation_id": op["operationID"], "lease_token": op["leaseToken"], "replacement": "saved"})
	if body := execute(tool.PageRead, map[string]any{"item_id": page["itemID"]})["body"]; body != "saved" {
		t.Fatalf("unexpected body %v", body)
	}
	post("/agent-mcp/end", other, map[string]string{"turnID": turn}, 409)
	post("/agent-mcp/end", credential, map[string]string{"turnID": turn, "summary": "Saved"}, 200)
	post("/agent-mcp/call", credential, map[string]string{"turnID": turn, "tool": tool.TimeGetCurrent}, 409)
	history, err := fixture.store.GetConversationTurn(ctx, "bound", turn)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		ExternalAgent store.ExternalAgentIdentity `json:"externalAgent"`
	}
	if err := json.Unmarshal(history.Messages[0].Metadata, &metadata); err != nil || metadata.ExternalAgent != bindings[0].ExternalAgentIdentity {
		t.Fatalf("external turn identity: %+v, err=%v", metadata, err)
	}
	events, err := fixture.store.EventsAfter(ctx, "bound", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(events, func(ev event.Event) bool {
		return ev.Kind == event.TurnStarted && ev.TurnID == turn && bytes.Equal(ev.Payload, history.Messages[0].Metadata)
	}) {
		t.Fatal("turn.started must carry the same external identity as history")
	}
	var calls int
	for _, m := range history.Messages {
		for _, p := range m.Parts {
			if p.Type == store.ContentPartToolUse {
				calls++
			}
		}
	}
	if fresh := post("/agent-mcp/tools", credential, nil, 200); fresh["mode"] != "chat" {
		t.Fatal("turn-only mode survived end")
	}
	if calls != 6 || history.Status != store.TurnCompleted {
		t.Fatalf("stored calls %d, status %s", calls, history.Status)
	}
	t.Run("disconnect one agent during its next turn", func(t *testing.T) {
		id := post("/agent-mcp/begin", credential, map[string]string{"task": "Second task"}, 200)["turnID"].(string)
		resp := req(t, http.MethodDelete, fixture.server.URL+"/sessions/bound/mcp-agents/"+bindings[0].ID, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("disconnect: %s", resp.Status)
		}
		post("/agent-mcp/status", credential, nil, 404)
		wait, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := fixture.engine.WaitSession(wait, "bound"); err != nil {
			t.Fatal(err)
		}
		stopped, err := fixture.store.GetConversationTurn(ctx, "bound", id)
		if err != nil || stopped.Status != store.TurnCancelled {
			t.Fatalf("disconnected turn: %+v, err=%v", stopped, err)
		}
		post("/agent-mcp/status", other, map[string]string{"turnID": id}, 404)
		next := post("/agent-mcp/begin", other, map[string]string{"task": "Continue independently"}, 200)["turnID"].(string)
		post("/agent-mcp/end", other, map[string]string{"turnID": next}, 200)
		post("/agent-mcp/disconnect", other, nil, 200)
		post("/agent-mcp/status", other, nil, 404)
	})
}

func TestExternalMCPBrowserPluginKeepsTurnApproval(t *testing.T) {
	managed := &fakeBrowserService{tabs: make(map[string]browser.TabSnapshot)}
	fixture := newDebugMCPTestServer(t, tool.WithBrowser(managed))
	ctx := context.Background()
	const sessionID = "browser-approval"
	if err := fixture.store.CreateSession(ctx, &store.Session{ID: sessionID,
		Provider: "mock", Model: "mock-model", ActiveMode: store.ModeChat}); err != nil {
		t.Fatal(err)
	}
	post := externalMCPPostForTest(t, fixture.server.URL)
	invite := post("/sessions/"+sessionID+"/mcp-agents", "", nil, 201)["invite"]
	credential := post("/agent-mcp/connect", "", map[string]any{"invite": invite, "name": "Example Agent"}, 200)["credential"].(string)
	turnID := post("/agent-mcp/begin", credential, map[string]string{"task": "Inspect browser"}, 200)["turnID"].(string)
	approved := approveExternalMCPTurnForTest(t, fixture, sessionID)
	loaded := post("/agent-mcp/call", credential, map[string]any{
		"turnID": turnID, "tool": tool.PluginLoad, "arguments": map[string]string{"plugin_id": "browser"},
	}, 200)
	if err := <-approved; err != nil {
		t.Fatal(err)
	}
	if loaded["isError"] == true {
		t.Fatalf("browser plugin load failed: %v", loaded["content"])
	}
	status := post("/agent-mcp/status", credential, nil, 200)
	if status["mode"] != "work" || status["activeTurnID"] != turnID {
		t.Fatalf("approved browser capability missing from active turn: %v", status)
	}
	result := post("/agent-mcp/call", credential, map[string]string{"turnID": turnID, "tool": tool.BrowserStatus}, 200)
	if result["isError"] == true {
		t.Fatalf("browser status rejected after plugin approval: %v", result["content"])
	}
	root := t.TempDir()
	const projectRule = "Use the project's skill naming convention."
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(projectRule), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.SetSessionDirectories(ctx, sessionID, []string{root}); err != nil {
		t.Fatal(err)
	}
	approved = approveExternalMCPTurnForTest(t, fixture, sessionID)
	loaded = post("/agent-mcp/call", credential, map[string]any{
		"turnID": turnID, "tool": tool.PluginLoad, "arguments": map[string]string{"plugin_id": plugin.BuiltinSkillAuthoringID},
	}, 200)
	if err := <-approved; err != nil {
		t.Fatal(err)
	}
	if loaded["isError"] == true || !slices.ContainsFunc(loaded["content"].([]any), func(block any) bool {
		text, _ := block.(map[string]any)["text"].(string)
		return strings.Contains(text, projectRule)
	}) {
		t.Fatal("plugin-triggered Code approval did not deliver the project rules")
	}
	post("/agent-mcp/end", credential, map[string]string{"turnID": turnID}, 200)
	status = post("/agent-mcp/status", credential, nil, 200)
	if status["mode"] != "chat" || status["activeTurnID"] != "" {
		t.Fatalf("browser capability outlived the turn: %v", status)
	}
}

func TestExternalMCPForegroundCommandLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses POSIX commands")
	}
	fixture := newDebugMCPTestServer(t)
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "build.sh"), []byte("printf 'compiling\\n'\nsleep 1\nprintf 'missing manifest\\n' >&2\nexit 7\n"), 0600); err != nil {
		t.Fatal(err)
	}
	project := &store.Project{ID: "build-project", Name: "Build", RootDirs: []string{root}, ApprovalMode: store.ApprovalFull}
	if err := fixture.store.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	const sessionID = "build-session"
	if err := fixture.store.CreateSession(ctx, &store.Session{ID: sessionID, Provider: "mock", Model: "mock-model",
		ProjectID: project.ID, ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession}); err != nil {
		t.Fatal(err)
	}
	post := externalMCPPostForTest(t, fixture.server.URL)
	invite := post("/sessions/"+sessionID+"/mcp-agents", "", nil, 201)["invite"]
	credential := post("/agent-mcp/connect", "", map[string]any{"invite": invite, "name": "Build Agent"}, 200)["credential"].(string)
	begin := func() string {
		return post("/agent-mcp/begin", credential, map[string]string{"task": "Run build"}, 200)["turnID"].(string)
	}
	call := func(turnID, name string, args map[string]any) map[string]any {
		t.Helper()
		out := post("/agent-mcp/call", credential, map[string]any{"turnID": turnID, "tool": name, "arguments": args}, 200)
		content := out["content"].([]any)[0].(map[string]any)["text"].(string)
		var result map[string]any
		if err := json.Unmarshal([]byte(content), &result); err != nil || out["isError"] == true {
			t.Fatalf("%s: %s (%v)", name, content, err)
		}
		return result
	}
	t.Run("poll through build failure", func(t *testing.T) {
		turnID := begin()
		defer post("/agent-mcp/end", credential, map[string]string{"turnID": turnID, "status": "failed"}, 200)
		started := call(turnID, tool.CommandRun, map[string]any{"command": "sh build.sh", "scope": "project", "yield_ms": 0})
		if started["yielded"] != true || started["running"] != true {
			t.Fatalf("build did not yield a running process: %v", started)
		}
		result := started
		deadline := time.Now().Add(5 * time.Second)
		for result["running"] == true && time.Now().Before(deadline) {
			result = call(turnID, tool.CommandSession, map[string]any{"action": "poll", "process_id": started["processID"],
				"offset": result["nextOffset"], "wait_ms": 1000})
		}
		if result["running"] != false || result["exitCode"] != float64(7) || result["stderr"] != "missing manifest\n" {
			t.Fatalf("lost the build exit code or failure log after yield: %v", result)
		}
	})
	t.Run("stop a yielded command from Pudding", func(t *testing.T) {
		turnID := begin()
		started := call(turnID, tool.CommandRun, map[string]any{"command": "sleep 30", "scope": "project", "yield_ms": 0})
		if started["yielded"] != true || started["running"] != true {
			t.Fatalf("test command did not yield: %v", started)
		}
		post("/sessions/"+sessionID+"/cancel", "", nil, 202)
		deadline := time.Now().Add(5 * time.Second)
		var status map[string]any
		for time.Now().Before(deadline) {
			status = post("/agent-mcp/status", credential, map[string]string{"turnID": turnID}, 200)
			if status["status"] != "running" {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if status["status"] != "cancelled" || status["activeTurnID"] != "" {
			t.Fatalf("agent did not see the cancelled turn: %v", status)
		}
		processURL := fixture.server.URL + "/sessions/" + sessionID + "/processes/" + started["processID"].(string)
		process := decodeJSON[tool.BackgroundProcessLogSnapshot](t, req(t, http.MethodGet, processURL, nil)).Process
		if process.Running || process.Status != "cancelled" {
			t.Fatalf("process did not reach the cancelled state: %+v", process)
		}
	})
}

type mcpScreenshotBrowser struct {
	fakeBrowserService
	capture browser.ScreenshotResult
}

func (f *mcpScreenshotBrowser) Screenshot(ctx context.Context, sessionID, tabID string, _ browser.ScreenshotOptions) (browser.ScreenshotResult, error) {
	tab, err := f.GetTab(ctx, sessionID, tabID)
	result := f.capture
	result.Tab = tab
	return result, err
}

func TestExternalMCPBrowserScreenshotProvidesLocalFallback(t *testing.T) {
	const width, height = 1920, 1080
	var original bytes.Buffer
	if err := png.Encode(&original, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	managed := &mcpScreenshotBrowser{fakeBrowserService{tabs: make(map[string]browser.TabSnapshot)}, browser.ScreenshotResult{
		MIME: "image/png", DataBase64: base64.StdEncoding.EncodeToString(original.Bytes()), Width: width, Height: height,
		ViewportWidth: width, ViewportHeight: height,
	}}
	fixture := newDebugMCPTestServer(t, tool.WithBrowser(managed))
	ctx := context.Background()
	post := externalMCPPostForTest(t, fixture.server.URL)
	homeDir, err := filepath.EvalSymlinks(fixture.home)
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(homeDir, "temp", "mcp-screenshots")
	var savedPath string

	for _, id := range []string{"screenshots", "blocked"} {
		if err := fixture.store.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock-model",
			ActiveMode: store.ModeWork, ModeLease: store.ModeLeaseSession, LoadedPluginIDs: []string{"browser"}}); err != nil {
			t.Fatal(err)
		}
		tab := managed.create(id, id+"-tab")
		invite := post("/sessions/"+id+"/mcp-agents", "", nil, 201)["invite"]
		credential := post("/agent-mcp/connect", "", map[string]any{"invite": invite, "name": "Example Agent"}, 200)["credential"].(string)
		turn := post("/agent-mcp/begin", credential, map[string]string{"task": "Capture a browser screenshot"}, 200)["turnID"].(string)
		if id == "blocked" {
			managed.capture = browser.ScreenshotResult{MIME: "image/png", DataBase64: debugMCPTestPNG,
				Width: 1, Height: 1, ViewportWidth: 1, ViewportHeight: 1}
			if err := os.WriteFile(filepath.Join(folder, id), []byte("blocking file"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		call := post("/agent-mcp/call", credential, map[string]any{
			"turnID": turn, "tool": tool.BrowserScreenshot, "arguments": map[string]string{"tabID": tab.ID},
		}, 200)
		if call["isError"] == true {
			t.Fatalf("browser capture failed: %v", call)
		}
		blocks := call["content"].([]any)
		var metadata struct {
			AttachmentKey string `json:"attachmentKey"`
			Path          string `json:"localImagePath"`
			Folder        string `json:"localImageFolder"`
			ExportError   string `json:"localImageExportError"`
			ModelWidth    int    `json:"modelWidth"`
			ModelHeight   int    `json:"modelHeight"`
		}
		if err := json.Unmarshal([]byte(blocks[0].(map[string]any)["text"].(string)), &metadata); err != nil {
			t.Fatal(err)
		}
		block := blocks[1].(map[string]any)
		if block["type"] != "image" || block["mimeType"] != "image/png" {
			t.Fatalf("unexpected screenshot block: %v / %v", block["type"], block["mimeType"])
		}
		pixels, err := png.DecodeConfig(base64.NewDecoder(base64.StdEncoding, strings.NewReader(block["data"].(string))))
		if err != nil || pixels.Width != metadata.ModelWidth || pixels.Height != metadata.ModelHeight || pixels.Width >= width {
			t.Fatalf("inline image %dx%d does not match the scaled click coordinate metadata %dx%d: %v", pixels.Width, pixels.Height, metadata.ModelWidth, metadata.ModelHeight, err)
		}
		if id == "blocked" {
			if block["data"] != debugMCPTestPNG {
				t.Fatal("small screenshot did not retain its original inline bytes")
			}
			if metadata.ExportError == "" || metadata.Path != "" {
				t.Fatalf("expected the inline image with a visible export error, got %+v", metadata)
			}
		} else {
			savedPath = filepath.Join(folder, id, filepath.Base(metadata.AttachmentKey))
			if metadata.Path != savedPath || metadata.Folder != folder || metadata.ExportError != "" {
				t.Fatalf("unexpected fallback metadata: %+v", metadata)
			}
			data, err := os.ReadFile(savedPath)
			if err != nil || !bytes.Equal(data, original.Bytes()) {
				t.Fatalf("expected the original PNG on disk, err=%v", err)
			}
		}
		post("/agent-mcp/end", credential, map[string]string{"turnID": turn}, 200)
	}

	req, err := http.NewRequest(http.MethodDelete, fixture.server.URL+"/sessions/screenshots", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete session: %d", resp.StatusCode)
	}
	if _, err := os.Stat(savedPath); !os.IsNotExist(err) {
		t.Fatalf("deleted session's screenshot remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(folder, "blocked")); err != nil {
		t.Fatalf("cleanup removed a different session's path: %v", err)
	}
}
