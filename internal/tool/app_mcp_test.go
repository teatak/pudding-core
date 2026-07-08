package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/app"
)

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

	defs, err := runner.Definitions(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected one tool, got %+v", defs)
	}
	if !strings.HasPrefix(defs[0].Name, appMCPToolPrefix) || !strings.Contains(defs[0].Description, "linear") {
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

	defs, err := runner.Definitions(ctx, "session-1")
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

type fakeAppMCPSource struct {
	bindings []*app.EndpointBinding
}

func (f fakeAppMCPSource) ListEndpointBindings(context.Context, string) ([]*app.EndpointBinding, error) {
	return f.bindings, nil
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
