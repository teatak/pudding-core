package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestPluginConnectionBindingsAPIAndNativeWebCompatibility(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	cfg := config.NewManager(home)
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	plugins := plugin.NewService(home, cfg)
	st := storetest.New(t)
	hub := event.NewHub()
	eng := engine.New(st, hub, nil, cfg)
	srv := httptest.NewServer(New(eng, st, cfg, hub).WithPlugins(plugins).Handler(testToken, nil))
	defer srv.Close()
	for _, name := range []string{"tavily", "deepseek"} {
		resp := req(t, http.MethodPut, srv.URL+"/plugin-connections/"+name, map[string]any{"pluginID": "web", "name": name, "authMethodID": name, "authType": "bearer", "token": name + "-secret"})
		if resp.StatusCode != 200 {
			t.Fatal(resp.StatusCode)
		}
		c := decodeJSON[plugin.ConnectionView](t, resp)
		if c.PluginID != plugin.BuiltinWebSearchFetchID || !c.TokenSet {
			t.Fatal(c)
		}
	}
	view := decodeJSON[plugin.ConnectionBindingsView](t, req(t, http.MethodGet, srv.URL+"/plugins/web/connection-bindings", nil))
	if len(view.Bindings) != 0 {
		t.Fatal("saving implicitly selected a connection", view)
	}
	resp := req(t, http.MethodPut, srv.URL+"/plugins/web/enabled", map[string]any{"enabled": false})
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	resp.Body.Close()
	resp = req(t, http.MethodPatch, srv.URL+"/plugins/web/connection-bindings", map[string]any{"bindings": map[string]string{"active": "deepseek"}})
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	resp.Body.Close()
	legacy := decodeJSON[config.WebToolsView](t, req(t, http.MethodGet, srv.URL+"/tools/web", nil))
	if legacy.SearchProvider != "deepseek" || legacy.FetchProvider != "deepseek" {
		t.Fatal(legacy)
	}
	enabled, _ := cfg.ListPluginEnablement(ctx)
	if enabled[plugin.BuiltinWebSearchFetchID] {
		t.Fatal("selecting enabled plugin")
	}
	resp = req(t, http.MethodPatch, srv.URL+"/tools/web", map[string]any{"providers": map[string]any{"deepseek": map[string]string{"apiKey": "updated"}}})
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	resp.Body.Close()
	conn, err := cfg.GetPluginConnection(ctx, "deepseek")
	if err != nil || conn.Auth.Token != "updated" {
		t.Fatal("old API did not update standard connection", conn, err)
	}
	resp = req(t, http.MethodDelete, srv.URL+"/plugin-connections/deepseek", nil)
	if resp.StatusCode != 204 {
		t.Fatal(resp.StatusCode)
	}
	resp.Body.Close()
	view = decodeJSON[plugin.ConnectionBindingsView](t, req(t, http.MethodGet, srv.URL+"/plugins/web-search-fetch/connection-bindings", nil))
	if len(view.Bindings) != 0 {
		t.Fatal(view)
	}
	// The same declaration and binding API works for an installed REST plugin.
	dir := filepath.Join(home, "plugins", "binding-demo")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte("id: binding-demo\nname: Binding demo\nauth:\n  required: true\n  methods:\n    - id: key\n      type: bearer\nconnection:\n  selection: single_active\nendpoints:\n  demo_api:\n    kind: rest\n    url: https://example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	resp = req(t, http.MethodPut, srv.URL+"/plugin-connections/demo", map[string]any{"pluginID": "binding-demo", "authMethodID": "key", "authType": "bearer", "token": "demo"})
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode, decodeJSON[map[string]any](t, resp))
	}
	resp.Body.Close()
	if _, err := plugins.ResolveEndpoint(ctx, "session", "demo_api", ""); err == nil {
		t.Fatal("only saved connection was implicitly selected")
	}
	resp = req(t, http.MethodPatch, srv.URL+"/plugins/binding-demo/connection-bindings", map[string]any{"bindings": map[string]string{"account": "tavily"}})
	if resp.StatusCode != 400 {
		t.Fatal("foreign connection accepted", resp.StatusCode)
	}
	resp.Body.Close()
	resp = req(t, http.MethodPatch, srv.URL+"/plugins/binding-demo/connection-bindings", map[string]any{"bindings": map[string]string{"active": "demo"}})
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	resp.Body.Close()
	binding, err := plugins.ResolveEndpoint(ctx, "session", "demo_api", "")
	if err != nil || binding.ConnectionID != "demo" {
		t.Fatal(binding, err)
	}
}

func TestConnectionOnlyPluginsSupportExplicitSelection(t *testing.T) {
	for _, selection := range []string{"user", "single_active"} {
		for _, auth := range []string{"", "auth: {required: false}\n"} {
			authName := "no-auth"
			if auth != "" {
				authName = "optional-auth"
			}
			t.Run(selection+"/"+authName, func(t *testing.T) {
				ctx := context.Background()
				home := t.TempDir()
				cfg := config.NewManager(home)
				if err := cfg.Prepare(); err != nil {
					t.Fatal(err)
				}
				dir := filepath.Join(home, "plugins", "connection-only")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				definition := "id: connection-only\nname: Connection only\n" + auth + "connection:\n  selection: " + selection + "\n  fields:\n    - {id: workspace, label: Workspace, required: true}\nendpoints:\n  config_api: {kind: rest, url: 'https://example.com'}\n"
				if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(definition), 0600); err != nil {
					t.Fatal(err)
				}
				st := storetest.New(t)
				if err := st.CreateSession(ctx, &store.Session{ID: "chat", Provider: "mock", Model: "mock"}); err != nil {
					t.Fatal(err)
				}
				plugins := plugin.NewService(home, cfg)
				hub := event.NewHub()
				eng := engine.New(st, hub, nil, cfg, engine.WithPlugins(plugins))
				srv := httptest.NewServer(New(eng, st, cfg, hub).WithPlugins(plugins).Handler(testToken, nil))
				defer srv.Close()
				resp := req(t, http.MethodPut, srv.URL+"/plugin-connections/workspace", map[string]any{"pluginID": "connection-only", "authType": "none", "fields": map[string]string{"workspace": "work"}})
				if resp.StatusCode != 200 {
					t.Fatal("connection could not be saved", resp.StatusCode, decodeJSON[map[string]any](t, resp))
				}
				resp.Body.Close()
				initial, err := plugins.ResolveEndpoint(ctx, "chat", "config_api", "")
				if selection == "user" {
					if err != nil || initial.ConnectionID != "workspace" {
						t.Fatal("sole configuration-only connection was not resolved", initial, err)
					}
				} else if err == nil {
					t.Fatal("single-active connection was implicitly selected")
				}
				if selection == "single_active" {
					resp = req(t, http.MethodPatch, srv.URL+"/plugins/connection-only/connection-bindings", map[string]any{"bindings": map[string]string{"active": "workspace"}})
				} else {
					resp = req(t, http.MethodPut, srv.URL+"/sessions/chat/plugins/connection-only/connection", map[string]string{"connectionID": "workspace"})
				}
				if resp.StatusCode != 200 {
					t.Fatal("connection could not be selected", resp.StatusCode, decodeJSON[map[string]any](t, resp))
				}
				resp.Body.Close()
				binding, err := plugins.ResolveEndpoint(ctx, "chat", "config_api", "")
				if err != nil || binding.ConnectionID != "workspace" || binding.ConnectionFields["workspace"] != "work" {
					t.Fatal(binding, err)
				}
			})
		}
	}
}
