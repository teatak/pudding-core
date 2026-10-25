package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/teatak/pudding-core/internal/pluginexec"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestSessionLoginAPI(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	cfg := config.NewManager(home)
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	mode := "challenge"
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/login" || r.URL.RawQuery != "" {
			t.Error("incorrect login request", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.PostForm.Get("passwd") != " pass&word " {
			t.Error("password changed")
		}
		w.Header().Set("Content-Type", "application/json")
		switch mode {
		case "challenge":
			fmt.Fprint(w, `{"success":false,"error":{"code":403},"password":"never-echo"}`)
		case "reject":
			fmt.Fprint(w, `{"success":false,"error":{"code":400,"message":"never-echo"}}`)
		case "missing":
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		case "race":
			old, err := cfg.GetPluginConnection(ctx, "nas")
			if err != nil {
				t.Error(err)
				return
			}
			old.Name = "concurrent-edit"
			if err = cfg.PutPluginConnection(ctx, old); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{"success":true,"data":{"sid":"stale-sid"}}`)
		default:
			if r.PostForm.Get("otp") != "123456" {
				t.Error("OTP not forwarded")
			}
			fmt.Fprint(w, `{"success":true,"data":{"sid":"private-sid","csrf":"private-csrf"}}`)
		}
	}))
	defer provider.Close()
	definition := `id: appliance
name: Appliance
connection:
  fields:
    - {id: sid, required: true, secret: true, inject: [{target: query, name: _sid}]}
    - {id: csrf, secret: true, inject: [{target: header, name: X-CSRF}]}
endpoints:
  appliance_api:
    kind: rest
    url: https://example.invalid/api
    url_config: {required: true, label: Server}
auth:
  required: true
  methods:
    - id: login
      type: session
      session:
        endpoint: appliance_api
        path: /login
        encoding: form
        inputs:
          - {id: account, required: true}
          - {id: password, required: true, secret: true}
          - {id: otp, secret: true}
        parameters: {action: login}
        body_fields: {user: account, passwd: password, otp: otp}
        outputs: {sid: data.sid, csrf: data.csrf}
        account_input: account
        success_field: success
        error_code_field: error.code
        challenges: {"403": [otp]}
`
	dir := filepath.Join(home, "plugins", "appliance")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(definition), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.LoadUserDefinitions(filepath.Join(home, "plugins")); err != nil {
		t.Fatal(err)
	}
	st := storetest.New(t)
	hub := event.NewHub()
	plugins := plugin.NewService(home, cfg)
	eng := engine.New(st, hub, nil, cfg, engine.WithPlugins(plugins))
	srv := httptest.NewServer(New(eng, st, cfg, hub).WithPlugins(plugins).Handler(testToken, nil))
	defer srv.Close()
	input := map[string]any{"pluginID": "appliance", "name": "NAS", "authMethodID": "login", "endpointURLs": map[string]string{"appliance_api": provider.URL + "/api"}, "inputs": map[string]string{"account": "tester", "password": " pass&word ", "otp": "123456"}}
	login := func() (int, map[string]any) {
		t.Helper()
		response := req(t, http.MethodPost, srv.URL+"/plugin-connections/nas/login", input)
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"private-sid", "private-csrf", "never-echo", "pass&word", "123456"} {
			if strings.Contains(string(data), secret) {
				t.Fatalf("login response leaked %q", secret)
			}
		}
		var result map[string]any
		if err = json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, result
	}
	status, result := login()
	if status != 200 || result["status"] != "challenge" {
		t.Fatal(status, result)
	}
	if got, _ := cfg.ListPluginConnections(ctx); len(got) != 0 {
		t.Fatal("challenge saved a connection")
	}
	mode = "success"
	status, result = login()
	if status != 200 || result["status"] != "success" {
		t.Fatal(status, result)
	}
	saved, err := cfg.GetPluginConnection(ctx, "nas")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Fields["sid"] != "private-sid" || saved.Fields["csrf"] != "private-csrf" || saved.Account.Login != "tester" || !saved.Auth.SessionEstablished {
		t.Fatal("credentials not saved together")
	}
	contents, err := os.ReadFile(filepath.Join(home, "config", "plugin-connections.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "pass&word") || strings.Contains(string(contents), "123456") {
		t.Fatal("login inputs persisted")
	}
	detail := req(t, http.MethodGet, srv.URL+"/plugin-connections/nas", nil)
	text, _ := io.ReadAll(detail.Body)
	detail.Body.Close()
	if strings.Contains(string(text), "private-") {
		t.Fatal("credential detail exposed managed secrets")
	}
	// A fresh service can resolve the saved session and supply its credentials.
	if err := st.CreateSession(ctx, &store.Session{ID: "chat", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	fresh := plugin.NewService(home, config.NewManager(home))
	binding, err := fresh.ResolveEndpoint(ctx, "chat", "appliance_api", "")
	if err != nil {
		t.Fatal(err)
	}
	if binding.ConnectionFields["sid"] != "private-sid" {
		t.Fatal("saved session unavailable after restart")
	}
	if err := pluginexec.ApplyEndpointAuth(http.Header{}, binding.Auth); err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"reject", "missing"} {
		mode = failure
		_, result = login()
		if result["status"] != "error" {
			t.Fatal(result)
		}
		current, _ := cfg.GetPluginConnection(ctx, "nas")
		if !current.UpdatedAt.Equal(saved.UpdatedAt) || current.Fields["sid"] != saved.Fields["sid"] {
			t.Fatal("failed login changed connection")
		}
	}
	before := calls
	rename := req(t, http.MethodPut, srv.URL+"/plugin-connections/nas", map[string]any{"pluginID": "appliance", "name": "Renamed", "authMethodID": "login", "authType": "session"})
	if rename.StatusCode != 200 {
		t.Fatal(rename.StatusCode, decodeJSON[map[string]any](t, rename))
	}
	rename.Body.Close()
	if calls != before {
		t.Fatal("rename performed login")
	}
	change := req(t, http.MethodPut, srv.URL+"/plugin-connections/nas", map[string]any{"pluginID": "appliance", "authMethodID": "login", "authType": "session", "endpointURLs": map[string]string{"appliance_api": "https://other.example/api"}})
	if change.StatusCode != 400 {
		t.Fatal("changed endpoint reused credentials")
	}
	change.Body.Close()
	mode = "race"
	status, _ = login()
	if status != 409 {
		t.Fatal("concurrent edit was overwritten", status)
	}
	current, _ := cfg.GetPluginConnection(ctx, "nas")
	if current.Name != "concurrent-edit" || current.Fields["sid"] != "private-sid" {
		t.Fatal("stale login overwrote newer connection")
	}
}
