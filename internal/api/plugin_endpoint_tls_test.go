package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/widget"
)

func TestSessionTLSLoginPersistsAndAppliesToWidgetRequests(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	cfg := config.NewManager(home)
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/login" {
			fmt.Fprint(w, `{"data":{"sid":"managed-session"}}`)
			return
		}
		if r.URL.Query().Get("_sid") != "managed-session" {
			t.Error("missing connection credential")
		}
		fmt.Fprint(w, `{"data":{"files":[]}}`)
	}))
	target.Config.ErrorLog = log.New(io.Discard, "", 0)
	target.StartTLS()
	defer target.Close()
	definition := `id: nas
name: NAS
auth:
  required: true
  methods:
    - id: login
      type: session
      session:
        endpoint: api
        path: /login
        encoding: form
        inputs: [{id: account, required: true}, {id: password, required: true, secret: true}]
        body_fields: {account: account, password: password}
        outputs: {sid: data.sid}
        account_input: account
    - {id: api-key, type: bearer}
connection:
  fields:
    - {id: sid, required: true, secret: true, inject: [{target: query, name: _sid}]}
endpoints:
  api: {kind: rest, url: 'https://nas.example/api', url_config: {label: Server, required: true}}
  second: {kind: rest, url: 'https://nas.example/api', url_config: {label: Second server}}
`
	dir := filepath.Join(home, "plugins", "nas")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(definition), 0600); err != nil {
		t.Fatal(err)
	}
	st := storetest.New(t)
	hub := event.NewHub()
	eng := engine.New(st, hub, nil, cfg)
	service := plugin.NewService(home, cfg)
	api := New(eng, st, cfg, hub).WithPlugins(service)
	server := httptest.NewServer(api.Handler(testToken, nil))
	defer server.Close()
	body := map[string]any{"pluginID": "nas", "name": "NAS", "authMethodID": "login", "endpointURLs": map[string]string{"api": target.URL + "/api", "second": target.URL + "/api"}, "inputs": map[string]string{"account": "tester", "password": "secret"}}
	response := req(t, http.MethodPost, server.URL+"/plugin-connections/one/login", body)
	result := decodeJSON[map[string]any](t, response)
	if result["status"] != "error" {
		t.Fatal("default login ignored self-signed certificate", result)
	}
	connections, _ := cfg.ListPluginConnections(ctx)
	if len(connections) != 0 {
		t.Fatal("failed TLS login saved connection")
	}
	body["endpointInsecureTLS"] = map[string]bool{"api": true}
	response = req(t, http.MethodPost, server.URL+"/plugin-connections/one/login", body)
	result = decodeJSON[map[string]any](t, response)
	if result["status"] != "success" {
		t.Fatal(result)
	}
	// Reload both configuration and service, as after a Core restart.
	cfg = config.NewManager(home)
	service = plugin.NewService(home, cfg)
	saved, err := cfg.GetPluginConnection(ctx, "one")
	if err != nil || !saved.EndpointInsecureTLS["api"] {
		t.Fatal(saved, err)
	}
	detail := req(t, http.MethodGet, server.URL+"/plugin-connections/one", nil)
	view := decodeJSON[plugin.ConnectionDetailView](t, detail)
	if !view.EndpointInsecureTLS["api"] || len(view.Fields) != 0 {
		t.Fatal("TLS preference missing or managed credentials exposed")
	}
	binding, _, err := service.ResolveBoundEndpoint(ctx, "nas", "api", "one")
	if err != nil || !binding.InsecureTLS {
		t.Fatal(binding, err)
	}
	result = api.executeWidgetRequest(ctx, binding, widget.Operation{Kind: "rest"}, widget.Request{Method: "GET", Path: "/files"})
	if result["ok"] != true {
		t.Fatal("widget request failed after successful login", result)
	}
	second, _, err := service.ResolveBoundEndpoint(ctx, "nas", "second", "one")
	if err != nil {
		t.Fatal(err)
	}
	result = api.executeWidgetRequest(ctx, second, widget.Operation{Kind: "rest"}, widget.Request{Method: "GET", Path: "/files"})
	if result["ok"] == true || second.InsecureTLS {
		t.Fatal("TLS policy escaped to another endpoint")
	}
	strict := plugin.CloneConnection(saved)
	strict.ID = "strict"
	strict.EndpointInsecureTLS = nil
	if err := cfg.PutPluginConnection(ctx, strict); err != nil {
		t.Fatal(err)
	}
	isolated, _, err := service.ResolveBoundEndpoint(ctx, "nas", "api", "strict")
	if err != nil {
		t.Fatal(err)
	}
	result = api.executeWidgetRequest(ctx, isolated, widget.Operation{Kind: "rest"}, widget.Request{Method: "GET", Path: "/files"})
	if result["ok"] == true {
		t.Fatal("TLS policy escaped to another connection")
	}
	// Disabling the option must not reuse the insecure connection pool or persist
	// a failed login over the previous working connection.
	body["endpointInsecureTLS"] = map[string]bool{}
	response = req(t, http.MethodPost, server.URL+"/plugin-connections/one/login", body)
	result = decodeJSON[map[string]any](t, response)
	if result["status"] != "error" {
		t.Fatal("strict login reused insecure TLS policy")
	}
	retained, _ := cfg.GetPluginConnection(ctx, "one")
	if !retained.EndpointInsecureTLS["api"] {
		t.Fatal("failed login overwrote old TLS settings")
	}
	body["endpointInsecureTLS"] = map[string]bool{"unknown": true}
	response = req(t, http.MethodPost, server.URL+"/plugin-connections/one/login", body)
	if response.StatusCode != 400 {
		t.Fatal("unknown TLS endpoint accepted")
	}
	response.Body.Close()
	// Ordinary token connections use the same persisted TLS options. Omitted
	// preferences retain the opt-in on rename and drop it on a server change.
	keyBody := map[string]any{"pluginID": "nas", "authMethodID": "api-key", "authType": "bearer", "token": "key", "fields": map[string]string{"sid": "managed-session"}, "endpointURLs": map[string]string{"api": target.URL + "/api"}, "endpointInsecureTLS": map[string]bool{"api": true}}
	response = req(t, http.MethodPut, server.URL+"/plugin-connections/key", keyBody)
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode, decodeJSON[map[string]any](t, response))
	}
	response.Body.Close()
	delete(keyBody, "endpointInsecureTLS")
	keyBody["name"] = "Renamed key"
	response = req(t, http.MethodPut, server.URL+"/plugin-connections/key", keyBody)
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	response.Body.Close()
	keyConnection, _ := cfg.GetPluginConnection(ctx, "key")
	if !keyConnection.EndpointInsecureTLS["api"] {
		t.Fatal("rename reset ordinary connection TLS preference")
	}
	keyBody["endpointURLs"] = map[string]string{"api": "https://new.test/api"}
	response = req(t, http.MethodPut, server.URL+"/plugin-connections/key", keyBody)
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	response.Body.Close()
	keyConnection, _ = cfg.GetPluginConnection(ctx, "key")
	if len(keyConnection.EndpointInsecureTLS) != 0 {
		t.Fatal("server change retained previous certificate exception")
	}

}
