package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
)

func TestPluginMCPRequiresUserSessionSelectionAndRechecksCachedTools(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	cfg := config.NewManager(home)
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	st, err := sqlitestore.Open(filepath.Join(home, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, id := range []string{"one", "two"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}
	var requests, calls atomic.Int32
	var wantToken atomic.Value
	wantToken.Store("Bearer work")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		if r.Header.Get("Authorization") != wantToken.Load().(string) {
			t.Errorf("used wrong account: %s", r.Header.Get("Authorization"))
		}
		var rpc struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		json.NewDecoder(r.Body).Decode(&rpc)
		if len(rpc.ID) == 0 {
			w.WriteHeader(202)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		result := map[string]any{}
		switch rpc.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": pluginMCPProtocolVersion, "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "test", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "lookup", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			calls.Add(1)
			result = map[string]any{"content": []map[string]string{{"type": "text", "text": "done"}}}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": result})
	}))
	defer srv.Close()
	dir := filepath.Join(home, "plugins", "accounts")
	os.MkdirAll(dir, 0700)
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(fmt.Sprintf(`id: accounts
name: Accounts
auth:
  required: true
  methods: [{id: key, type: bearer}]
endpoints:
  mcp:
    kind: mcp
    transport: streamable_http
    url: %s
`, srv.URL)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"work", "personal"} {
		if err := cfg.PutPluginConnection(ctx, &plugin.Connection{ID: id, PluginID: "accounts", Auth: plugin.Auth{Type: "bearer", MethodID: "key", Token: id}}); err != nil {
			t.Fatal(err)
		}
	}
	service := plugin.NewService(home, cfg).WithSessionConnections(st)
	runner := NewPluginMCPRunner(service)
	defs, err := runner.DefinitionsForPlugins(ctx, "one", []string{"accounts"})
	if err != nil || len(defs) != 0 || requests.Load() != 0 {
		t.Fatal("unselected accounts discovered", defs, err, requests.Load())
	}
	if _, err := service.SetSessionConnection(ctx, "one", "accounts", "work"); err != nil {
		t.Fatal(err)
	}
	defs, err = runner.DefinitionsForPlugins(ctx, "one", []string{"accounts"})
	if err != nil || len(defs) != 1 {
		t.Fatal(defs, err)
	}
	oldName := defs[0].Name
	if result := runner.Call(ctx, Call{SessionID: "one", Name: oldName, Args: json.RawMessage(`{}`)}); !result.Ok || calls.Load() != 1 {
		t.Fatal(result, calls.Load())
	}
	before := requests.Load()
	other, err := runner.DefinitionsForPlugins(ctx, "two", []string{"accounts"})
	if err != nil || len(other) != 0 || requests.Load() != before {
		t.Fatal("leaked discovery to another session", other, err)
	}
	if _, err := service.SetSessionConnection(ctx, "one", "accounts", "personal"); err != nil {
		t.Fatal(err)
	}
	if result := runner.Call(ctx, Call{SessionID: "one", Name: oldName, Args: json.RawMessage(`{}`)}); result.Ok || requests.Load() != before {
		t.Fatal("cached tool used old account", result)
	}
	wantToken.Store("Bearer personal")
	defs, err = runner.DefinitionsForPlugins(ctx, "one", []string{"accounts"})
	if err != nil || len(defs) != 1 || defs[0].Name == oldName {
		t.Fatal(defs, err)
	}
	if result := runner.Call(ctx, Call{SessionID: "one", Name: defs[0].Name, Args: json.RawMessage(`{}`)}); !result.Ok || calls.Load() != 2 {
		t.Fatal(result)
	}
	before = requests.Load()
	cfg.DeletePluginConnection(ctx, "personal")
	if result := runner.Call(ctx, Call{SessionID: "one", Name: defs[0].Name, Args: json.RawMessage(`{}`)}); result.Ok || requests.Load() != before {
		t.Fatal("deleted connection used cache", result)
	}
}
