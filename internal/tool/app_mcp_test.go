package tool

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/provider"
)

func TestAppMCPProviderToolNameUsesToolAndHash(t *testing.T) {
	binding := &app.EndpointBinding{
		AppID:        "sequential-thinking",
		EndpointName: "sequential_thinking_mcp",
	}
	got := appMCPProviderToolName(binding, "sequentialthinking")
	if want := "app_mcp__sequentialthinking__66e7fc2f"; got != want {
		t.Fatalf("unexpected provider tool name: got %q want %q", got, want)
	}

	otherConnection := *binding
	otherConnection.ConnectionID = "secondary"
	if other := appMCPProviderToolName(&otherConnection, "sequentialthinking"); other == got {
		t.Fatalf("connection must contribute to provider tool name hash: %q", other)
	}

	longName := appMCPProviderToolName(binding, strings.Repeat("tool", 20))
	if len(longName) != appMCPMaxToolNameLen {
		t.Fatalf("long provider tool name must be capped at %d bytes: %q (%d)", appMCPMaxToolNameLen, longName, len(longName))
	}
}

func TestAppMCPCacheKeyChangesWithCredentialsAndInjectionRules(t *testing.T) {
	binding := &app.EndpointBinding{
		AppID:        "example",
		ConnectionID: "primary",
		EndpointName: "example_mcp",
		Endpoint: app.Endpoint{
			Kind:      app.EndpointKindMCP,
			Transport: app.EndpointTransportStreamableHTTP,
			URL:       "https://example.test/mcp",
		},
		Auth: app.Auth{Type: app.AuthTypeBearer, Token: "first-secret"},
		ConnectionFieldDefs: []app.ConnectionField{{
			ID: "team",
			Inject: []app.ConnectionFieldInject{{
				Target: "header",
				Name:   "X-Team",
			}},
		}},
	}
	initial := appMCPBindingsCacheKey([]*app.EndpointBinding{binding})
	binding.Auth.Token = "second-secret"
	if rotated := appMCPBindingsCacheKey([]*app.EndpointBinding{binding}); rotated == initial {
		t.Fatal("credential rotation reused the stale MCP cache entry")
	}
	binding.Auth.Token = "first-secret"
	binding.ConnectionFieldDefs[0].Inject[0].Name = "X-Workspace"
	if changed := appMCPBindingsCacheKey([]*app.EndpointBinding{binding}); changed == initial {
		t.Fatal("connection injection change reused the stale MCP cache entry")
	}
	if strings.Contains(initial, "first-secret") {
		t.Fatal("MCP cache key exposed credential material")
	}
}

func TestAppMCPRunnerClearsStaleSessionTools(t *testing.T) {
	staleDef := provider.ToolDef{Name: "app_mcp__stale", AppID: "old-app"}
	staleTool := appMCPDiscoveredTool{
		binding:    &app.EndpointBinding{AppID: "old-app"},
		remoteName: "stale",
	}

	runner := NewAppMCPRunner(fakeAppMCPSource{})
	runner.setSessionTools("session-1", []provider.ToolDef{staleDef}, map[string]appMCPDiscoveredTool{staleDef.Name: staleTool})
	defs, err := runner.DefinitionsForApps(context.Background(), "session-1", nil)
	if err != nil || len(defs) != 0 {
		t.Fatalf("empty App scope definitions = %+v, err = %v", defs, err)
	}
	if _, ok := runner.lookup("session-1", staleDef.Name); ok {
		t.Fatal("stale MCP route survived an empty App scope")
	}

	runner.source = fakeAppMCPSource{err: errors.New("connection config unavailable")}
	runner.setSessionTools("session-1", []provider.ToolDef{staleDef}, map[string]appMCPDiscoveredTool{staleDef.Name: staleTool})
	defs, err = runner.DefinitionsForApps(context.Background(), "session-1", []string{"new-app"})
	if err != nil || len(defs) != 0 {
		t.Fatalf("failed binding lookup definitions = %+v, err = %v", defs, err)
	}
	if _, ok := runner.lookup("session-1", staleDef.Name); ok {
		t.Fatal("stale MCP route survived a binding lookup failure")
	}
}

func TestAppMCPRunnerDiscoversAndCallsStreamableHTTPTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var sawInitialized bool
	var sawCall bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", req.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if req.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("missing auth header: %s", req.Header.Get("Authorization"))
		}
		if req.Header.Get("X-Team") != "pudding" {
			t.Errorf("missing connection header: %s", req.Header.Get("X-Team"))
		}
		var rpc struct {
			ID     string         `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(req.Body).Decode(&rpc); err != nil {
			t.Errorf("decode rpc: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch rpc.Method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "sid-1")
			writeAppMCPTestResponse(t, w, rpc.ID, map[string]any{
				"protocolVersion": "2025-06-18",
				"serverInfo":      map[string]any{"name": "fake", "version": "1.0"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			})
		case "notifications/initialized":
			if req.Header.Get("Mcp-Session-Id") != "sid-1" {
				t.Errorf("missing session id on initialized: %s", req.Header.Get("Mcp-Session-Id"))
			}
			sawInitialized = true
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if req.Header.Get("Mcp-Session-Id") != "sid-1" {
				t.Errorf("missing session id on list: %s", req.Header.Get("Mcp-Session-Id"))
			}
			w.Header().Set("Content-Type", "application/json")
			writeAppMCPTestResponse(t, w, rpc.ID, map[string]any{"tools": []map[string]any{{
				"name":        "search_issues",
				"description": "Search issues",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
			}}})
		case "tools/call":
			if rpc.Params["name"] != "search_issues" {
				t.Errorf("unexpected remote tool name: %+v", rpc.Params)
			}
			sawCall = true
			w.Header().Set("Content-Type", "application/json")
			writeAppMCPTestResponse(t, w, rpc.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": `{"ok":true}`}},
			})
		default:
			t.Errorf("unexpected method: %s", rpc.Method)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	runner := NewAppMCPRunner(fakeAppMCPSource{bindings: []*app.EndpointBinding{{
		AppID:        "linear",
		ConnectionID: "linear-main",
		EndpointName: "linear_mcp",
		Endpoint: app.Endpoint{
			Kind:      app.EndpointKindMCP,
			Transport: app.EndpointTransportStreamableHTTP,
			URL:       srv.URL,
		},
		Auth:             app.Auth{Type: app.AuthTypeBearer, Token: "secret"},
		ConnectionFields: map[string]string{"team": "pudding"},
		ConnectionFieldDefs: []app.ConnectionField{{
			ID: "team",
			Inject: []app.ConnectionFieldInject{{
				Target: "header",
				Name:   "X-Team",
			}},
		}},
	}}})

	defs, err := runner.DefinitionsForApps(ctx, "session-1", []string{"linear"})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected one tool, got %+v", defs)
	}
	if !strings.HasPrefix(defs[0].Name, appMCPToolPrefix) || !strings.Contains(defs[0].Description, "linear") || defs[0].AppID != "linear" {
		t.Fatalf("unexpected definition: %+v", defs[0])
	}
	if !sawInitialized {
		t.Fatal("initialized notification was not sent")
	}

	res := runner.Call(ctx, Call{
		SessionID: "session-1",
		CallID:    "call-1",
		Name:      defs[0].Name,
		Args:      json.RawMessage(`{"query":"bug"}`),
	})
	if !res.Ok || !strings.Contains(res.Content, `"ok":true`) || !sawCall {
		t.Fatalf("unexpected result: %+v sawCall=%v", res, sawCall)
	}
	other := runner.Call(ctx, Call{SessionID: "session-2", CallID: "call-other", Name: defs[0].Name, Args: json.RawMessage(`{}`)})
	if other.Ok || !strings.Contains(other.Content, `"reason":"unknown_tool"`) {
		t.Fatalf("app MCP tool leaked across sessions: %+v", other)
	}
}

func TestAppMCPRunnerDiscoversAndCallsStdioTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	runner := NewAppMCPRunner(fakeAppMCPSource{bindings: []*app.EndpointBinding{{
		AppID:        "local",
		ConnectionID: "local-main",
		EndpointName: "local_mcp",
		Endpoint: app.Endpoint{
			Kind:      app.EndpointKindMCP,
			Transport: app.EndpointTransportStdio,
			Command:   os.Args[0],
			Args:      []string{"-test.run=TestAppMCPStdioServerHelper", "--"},
			Env:       map[string]string{"PUDDING_APP_MCP_STDIO_HELPER": "1"},
		},
		ConnectionFields: map[string]string{"apiKey": "abc"},
		ConnectionFieldDefs: []app.ConnectionField{{
			ID: "apiKey",
			Inject: []app.ConnectionFieldInject{{
				Target: "env",
				Name:   "FAKE_MCP_TOKEN",
			}},
		}},
	}}})

	defs, err := runner.DefinitionsForApps(ctx, "session-1", []string{"local"})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || !strings.HasPrefix(defs[0].Name, appMCPToolPrefix) {
		t.Fatalf("unexpected definitions: %+v", defs)
	}

	res := runner.Call(ctx, Call{
		SessionID: "session-1",
		CallID:    "call-stdio",
		Name:      defs[0].Name,
		Args:      json.RawMessage(`{"query":"hello"}`),
	})
	if !res.Ok || !strings.Contains(res.Content, "called hello") {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestAppMCPStdioServerHelper(t *testing.T) {
	if os.Getenv("PUDDING_APP_MCP_STDIO_HELPER") != "1" {
		return
	}
	if os.Getenv("FAKE_MCP_TOKEN") != "abc" {
		t.Fatalf("missing env")
	}
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for {
		var req struct {
			ID     string         `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := dec.Decode(&req); err != nil {
			return
		}
		if req.ID == "" {
			continue
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2025-06-18",
				"serverInfo":      map[string]any{"name": "stdio-test", "version": "1.0"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "local_search",
				"description": "Local search",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
			}}}
		case "tools/call":
			args, _ := req.Params["arguments"].(map[string]any)
			query, _ := args["query"].(string)
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "called " + query}}}
		default:
			result = map[string]any{}
		}
		if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}); err != nil {
			return
		}
	}
}

func TestApplyEndpointConnectionEnvDoesNotOverrideEndpointEnv(t *testing.T) {
	got, err := applyEndpointConnectionEnv(
		map[string]string{"FAKE_MCP_TOKEN": "custom", "BASE_ONLY": "base"},
		map[string]string{"apiKey": "connection", "extra": "extra-value"},
		[]app.ConnectionField{{
			ID: "apiKey",
			Inject: []app.ConnectionFieldInject{{
				Target: "env",
				Name:   "FAKE_MCP_TOKEN",
			}},
		}, {
			ID: "extra",
			Inject: []app.ConnectionFieldInject{{
				Target: "env",
				Name:   "EXTRA_ENV",
			}},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got["FAKE_MCP_TOKEN"] != "custom" || got["BASE_ONLY"] != "base" || got["EXTRA_ENV"] != "extra-value" {
		t.Fatalf("unexpected env merge: %+v", got)
	}
}

func TestAppMCPStdioEnvDoesNotInheritDaemonSecrets(t *testing.T) {
	t.Setenv("PUDDING_DAEMON_TOKEN", "daemon-secret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "cloud-secret")

	env, err := appMCPStdioEnv(map[string]string{
		"PUDDING_APP_MCP_STDIO_HELPER": "1",
		"FAKE_MCP_TOKEN":               "connection-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := appMCPEnvValue(env, "PUDDING_DAEMON_TOKEN"); got != "" {
		t.Fatalf("daemon token leaked to App MCP process: %q", got)
	}
	if got := appMCPEnvValue(env, "AWS_SECRET_ACCESS_KEY"); got != "" {
		t.Fatalf("cloud credential leaked to App MCP process: %q", got)
	}
	if got := appMCPEnvValue(env, "FAKE_MCP_TOKEN"); got != "connection-secret" {
		t.Fatalf("explicit connection env = %q", got)
	}
	if got := appMCPEnvValue(env, "PATH"); got == "" {
		t.Fatal("App MCP PATH is empty")
	}
}

func TestAppMCPResolveCommandUsesSuppliedEnvironmentOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable")
	}
	processBin := t.TempDir()
	executable := filepath.Join(processBin, "pudding-mcp-path-fixture")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", processBin)

	if _, err := appMCPResolveCommand("pudding-mcp-path-fixture", []string{"PATH=" + t.TempDir()}); err == nil {
		t.Fatal("expected supplied MCP environment to exclude the daemon process PATH")
	}
}

func TestReadAppMCPSSEFindsMatchingResponse(t *testing.T) {
	raw := strings.NewReader("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":\"other\",\"result\":{}}\n\n" +
		"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":\"42\",\"result\":{\"ok\":true}}\n\n")
	got, err := readAppMCPSSE(context.Background(), raw, "42")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"ok":true}` {
		t.Fatalf("unexpected result: %s", got)
	}
}

func TestReadAppMCPSSELimitsCombinedDataLines(t *testing.T) {
	line := strings.Repeat("x", appMCPMaxResponseBytes/2+1)
	raw := strings.NewReader("data: " + line + "\ndata: " + line + "\n\n")
	if _, err := readAppMCPSSE(context.Background(), raw, "42"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized SSE event error = %v", err)
	}
}

type fakeAppMCPSource struct {
	bindings []*app.EndpointBinding
	err      error
}

func (f fakeAppMCPSource) ListEndpointBindings(context.Context, string) ([]*app.EndpointBinding, error) {
	return f.bindings, f.err
}

func writeAppMCPTestResponse(t *testing.T, w http.ResponseWriter, id string, result any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
