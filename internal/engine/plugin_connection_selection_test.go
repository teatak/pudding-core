package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestPluginLoadSelectsAndRemembersConnection(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	for _, id := range []string{"one", "two"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}
	home := t.TempDir()
	dir := filepath.Join(home, "plugins", "accounts")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(`id: accounts
name: Accounts
auth:
  required: true
  methods: [{id: key, type: bearer}]
endpoints:
  accounts_rest: {kind: rest, url: 'https://example.com'}
`), 0600); err != nil {
		t.Fatal(err)
	}
	save := func(id string) {
		t.Helper()
		if err := st.Manager.PutPluginConnection(ctx, &plugin.Connection{
			ID: id, Name: id, PluginID: "accounts",
			Account: &plugin.ConnectionAccount{ID: id, Login: id + "@example.com"},
			Auth:    plugin.Auth{Type: "bearer", MethodID: "key", Token: "private-" + id},
		}); err != nil {
			t.Fatal(err)
		}
	}
	save("work")
	plugins := plugin.NewService(home, st.Manager)
	eng := New(st, event.NewHub(), nil, st, WithPlugins(plugins))
	t.Cleanup(eng.Stop)
	load := func(connection string) tool.Result {
		t.Helper()
		raw, err := json.Marshal(map[string]string{"plugin_id": "accounts", "connection_id": connection})
		if err != nil {
			t.Fatal(err)
		}
		result, changed := eng.loadPlugin(ctx, "one", tool.Call{Name: tool.PluginLoad, Args: raw}, store.ModeWork)
		if result.Ok && connection != "" && !changed {
			t.Fatal("connection selection must refresh model tools")
		}
		return result
	}
	if result := load(""); !result.Ok || !strings.Contains(result.Content, `"connectionID":"work"`) || strings.Contains(result.Content, `"connectionRequired":true`) {
		t.Fatal("sole account must load without asking for selection", result)
	}
	save("personal")
	result := load("")
	var payload struct {
		ConnectionRequired bool                    `json:"connectionRequired"`
		Connections        []plugin.ConnectionView `json:"connections"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatal(err)
	}
	if !result.Ok || !payload.ConnectionRequired || len(payload.Connections) != 2 || strings.Contains(result.Content, "private-") || !strings.Contains(result.Content, "work@example.com") {
		t.Fatal("load must expose account choices without secrets", result)
	}
	for _, connection := range []string{"work", "personal"} {
		result = load(connection)
		if !result.Ok || !strings.Contains(result.Content, `"connectionID":"`+connection+`"`) || !strings.Contains(result.Content, `"alreadyLoaded":true`) {
			t.Fatal("model could not select account on loaded plugin", result)
		}
		// A later load with no account must reuse the binding.
		if result := load(""); !result.Ok || !strings.Contains(result.Content, `"connectionID":"`+connection+`"`) {
			t.Fatal("binding was not reused", result)
		}
		binding, err := plugins.ResolveEndpoint(ctx, "one", "accounts_rest", "")
		if err != nil || binding.ConnectionID != connection {
			t.Fatal(binding, err)
		}
	}
	if result := load("missing"); result.Ok {
		t.Fatal("unknown connection accepted", result)
	}
	if result := load(""); !result.Ok || !strings.Contains(result.Content, `"connectionID":"personal"`) {
		t.Fatal("failed selection replaced binding", result)
	}
	other, err := plugins.SessionConnection(ctx, "two", "accounts")
	if err != nil || !other.Required || other.ConnectionID != "" {
		t.Fatal("selection leaked to another chat", other, err)
	}
	reloaded := plugin.NewService(home, st.Manager).WithSessionConnections(st)
	binding, err := reloaded.ResolveEndpoint(ctx, "one", "accounts_rest", "")
	if err != nil || binding.ConnectionID != "personal" {
		t.Fatal("model selection was not persisted", binding, err)
	}
}
