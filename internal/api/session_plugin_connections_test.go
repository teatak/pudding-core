package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestSessionConnectionOnlyUserSelectionAuthorizesAccounts(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	cfg := config.NewManager(home)
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
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
  accounts_graphql: {kind: graphql, url: 'https://example.com/graphql'}
`), 0600); err != nil {
		t.Fatal(err)
	}
	st := storetest.New(t)
	for _, id := range []string{"one", "two"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}
	plugins := plugin.NewService(home, cfg)
	hub := event.NewHub()
	eng := engine.New(st, hub, nil, cfg, engine.WithPlugins(plugins))
	srv := httptest.NewServer(New(eng, st, cfg, hub).WithPlugins(plugins).Handler(testToken, nil))
	defer srv.Close()
	save := func(id string) {
		t.Helper()
		if err := cfg.PutPluginConnection(ctx, &plugin.Connection{ID: id, Name: id, PluginID: "accounts", Auth: plugin.Auth{Type: "bearer", MethodID: "key", Token: "private-token"}}); err != nil {
			t.Fatal(err)
		}
	}
	save("work")
	checkBlocked := func(session, ref string) {
		t.Helper()
		for _, endpoint := range []string{"accounts_rest", "accounts_graphql"} {
			_, err := plugins.ResolveEndpoint(ctx, session, endpoint, ref)
			var required *plugin.EndpointResolveError
			if !errors.As(err, &required) || required.Reason != "connection_required" || required.PluginID != "accounts" {
				t.Fatalf("account executed without user selection: %v", err)
			}
		}
	}
	checkBlocked("one", "")
	checkBlocked("one", "work")
	route := srv.URL + "/sessions/one/plugins/accounts/connection"
	view := decodeJSON[plugin.SessionConnectionView](t, req(t, http.MethodGet, route, nil))
	raw, _ := json.Marshal(view)
	if !view.Required || view.ConnectionID != "" || len(view.Connections) != 1 || strings.Contains(string(raw), "private-token") {
		t.Fatal("selection metadata", string(raw))
	}
	save("personal")
	if resp := req(t, http.MethodPut, route, map[string]string{"connectionID": "missing"}); resp.StatusCode != 400 {
		t.Fatal(resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	view = decodeJSON[plugin.SessionConnectionView](t, req(t, http.MethodPut, route, map[string]string{"connectionID": "work"}))
	if view.Required || view.ConnectionID != "work" {
		t.Fatal(view)
	}
	for _, endpoint := range []string{"accounts_rest", "accounts_graphql"} {
		b, err := plugins.ResolveEndpoint(ctx, "one", endpoint, "")
		if err != nil || b.ConnectionID != "work" {
			t.Fatal(b, err)
		}
	}
	checkBlocked("one", "personal")
	checkBlocked("two", "work")
	// Saving/enablement cannot replace the selection. Disabling still blocks execution.
	save("third")
	if _, err := plugins.SetEnabled(ctx, "accounts", false); err != nil {
		t.Fatal(err)
	}
	if _, err := plugins.ResolveEndpoint(ctx, "one", "accounts_rest", ""); err == nil {
		t.Fatal("disabled plugin executed")
	}
	decodeJSON[plugin.SessionConnectionView](t, req(t, http.MethodPut, route, map[string]string{"connectionID": "personal"}))
	enabled, _ := cfg.ListPluginEnablement(ctx)
	if enabled["accounts"] {
		t.Fatal("binding enabled plugin")
	}
	plugins.SetEnabled(ctx, "accounts", true)
	b, err := plugins.ResolveEndpoint(ctx, "one", "accounts_rest", "")
	if err != nil || b.ConnectionID != "personal" {
		t.Fatal(b, err)
	}
	// New Service instances share the persisted selection; recreating the same ID does not.
	plugins = plugin.NewService(home, cfg).WithSessionConnections(st)
	b, err = plugins.ResolveEndpoint(ctx, "one", "accounts_rest", "")
	if err != nil || b.ConnectionID != "personal" {
		t.Fatal(b, err)
	}
	cfg.DeletePluginConnection(ctx, "personal")
	checkBlocked("one", "")
	save("personal")
	checkBlocked("one", "personal")
	if resp := req(t, http.MethodPut, srv.URL+"/sessions/missing/plugins/accounts/connection", map[string]string{"connectionID": "work"}); resp.StatusCode != 404 {
		t.Fatal(resp.StatusCode)
	} else {
		resp.Body.Close()
	}
}
