package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestWebPluginChatLifecyclePreservesConfiguration(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	cfg := config.NewManager(home)
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	tavilyKey, deepseekKey, selected := "tvly-existing", "ds-existing", "tavily"
	patch := func(update config.WebToolsUpdate) *config.WebToolsView {
		t.Helper()
		view, err := cfg.PatchWebTools(ctx, update)
		if err != nil {
			t.Fatal(err)
		}
		return view
	}
	// The existing storage and provider selection are reused without migration.
	initial := patch(config.WebToolsUpdate{SearchProvider: &selected, FetchProvider: &selected,
		Providers: map[string]config.WebToolProviderUpdate{"tavily": {APIKey: &tavilyKey}, "deepseek": {APIKey: &deepseekKey}}})
	plugins := plugin.NewService(home, cfg)
	assertEnabled := func(want bool) {
		t.Helper()
		defs, err := plugins.ListDefinitions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, def := range defs {
			if def.ID == plugin.BuiltinWebSearchFetchID {
				if def.Enabled != want || def.RequiredMode != "chat" || def.Runtime != "" || def.Source != plugin.SourceBuiltin || def.CanUninstall || len(def.Tools) != 2 {
					t.Fatalf("unexpected Web plugin: %+v", def)
				}
				return
			}
		}
		t.Fatal("Web plugin missing without a client runtime")
	}
	assertEnabled(true)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		wantKey := "Bearer " + tavilyKey
		if strings.HasPrefix(r.URL.Path, "/deepseek/") {
			wantKey = "Bearer " + deepseekKey
		}
		if r.Header.Get("Authorization") != wantKey {
			t.Error("request used the wrong provider credentials")
		}
		switch r.URL.Path {
		case "/tavily/search", "/deepseek/search":
			_, _ = w.Write([]byte(`{"results":[]}`))
		case "/tavily/fetch":
			_, _ = w.Write([]byte(`{"results":[{"url":"https://example.com","raw_content":"Page"}]}`))
		case "/deepseek/fetch":
			_, _ = w.Write([]byte(`{"title":"Page","content":"Markdown"}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	runner := tool.NewBuiltinRunner(tool.WithWebConfig(cfg), tool.WithTavilyEndpoints(srv.URL+"/tavily/search", srv.URL+"/tavily/fetch"), tool.WithDeepSeekEndpoints(srv.URL+"/deepseek/search", srv.URL+"/deepseek/fetch"))
	st := storetest.New(t)
	e := New(st, event.NewHub(), nil, st, WithPlugins(plugins), WithTools(runner))
	if err := st.CreateSession(ctx, &store.Session{ID: "web-session", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "web-session", TurnID: "web-turn", UserMessageID: "user", ClientMessageID: "input", UserText: "Search the web"}); err != nil {
		t.Fatal(err)
	}
	definitions := func(engine *Engine, want bool) []provider.ToolDef {
		t.Helper()
		var chat []provider.ToolDef
		for _, mode := range []store.AgentMode{store.ModeChat, store.ModeWork, store.ModeCode} {
			defs, err := engine.toolDefinitions(ctx, "web-session", mode)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{tool.WebSearch, tool.WebFetch} {
				if tool.HasDefinition(defs, name) != want {
					t.Fatalf("Web visibility in %s: %s present=%v", mode, name, !want)
				}
			}
			if mode == store.ModeChat {
				chat = defs
			}
		}
		return chat
	}
	callIndex := 0
	run := func(name string, defs []provider.ToolDef, reason, providerName string) {
		t.Helper()
		callIndex++
		parts := &turnPartAccumulator{}
		parts.AppendTool(provider.ToolCallChunk{CallID: fmt.Sprintf("web-%d", callIndex), Name: name, ArgsDelta: `{"query":"test","url":"https://example.com"}`})
		status, message, _, _ := e.executePendingTools(ctx, "web-session", "web-turn", store.ModeChat, defs, parts)
		if status != store.TurnRunning {
			t.Fatal(status, message)
		}
		for _, part := range parts.Parts() {
			if part.Type != store.ContentPartToolResult {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(part.Content), &payload); err != nil {
				t.Fatal(err)
			}
			if part.Ok != (reason == "") || (reason != "" && payload["reason"] != reason) || (providerName != "" && payload["provider"] != providerName) {
				t.Fatalf("unexpected Web result: %+v", part)
			}
			return
		}
		t.Fatal("missing Web result")
	}
	defs := definitions(e, false)
	run(tool.WebSearch, defs, "plugin_not_loaded", "")
	if requests.Load() != 0 {
		t.Fatal("unloaded plugin made a request")
	}
	load := tool.Call{Name: tool.PluginLoad, Args: json.RawMessage(fmt.Sprintf(`{"plugin_id":%q}`, plugin.BuiltinWebSearchFetchID))}
	out, changed, _ := e.loadPlugin(ctx, "web-session", load, store.ModeChat)
	if !out.Ok || !changed {
		t.Fatalf("Web must load in Chat: %+v", out)
	}
	defs = definitions(e, true)
	run(tool.WebSearch, defs, "", "tavily")
	run(tool.WebFetch, defs, "", "tavily")
	if _, err := plugins.SetEnabled(ctx, plugin.BuiltinWebSearchFetchID, false); err != nil {
		t.Fatal(err)
	}
	assertEnabled(false)
	current, err := cfg.WebTools(ctx)
	if err != nil || !reflect.DeepEqual(initial, current) {
		t.Fatalf("disable changed existing credentials or selection: %+v %v", current, err)
	}
	definitions(e, false)
	run(tool.WebFetch, defs, "plugin_disabled", "") // Reject stale schemas too.
	if requests.Load() != 2 {
		t.Fatal("disabled plugin made a request")
	}
	deepseekKey = "ds-replaced"
	view := patch(config.WebToolsUpdate{Providers: map[string]config.WebToolProviderUpdate{"deepseek": {APIKey: &deepseekKey}}})
	if view.SearchProvider != "tavily" || view.FetchProvider != "tavily" {
		t.Fatal("saving a key switched providers")
	}
	assertEnabled(false)
	selected = "deepseek"
	patch(config.WebToolsUpdate{SearchProvider: &selected, FetchProvider: &selected})
	assertEnabled(false)
	if _, err := plugins.SetEnabled(ctx, plugin.BuiltinWebSearchFetchID, true); err != nil {
		t.Fatal(err)
	}
	defs = definitions(e, true)
	run(tool.WebSearch, defs, "", "deepseek")
	run(tool.WebFetch, defs, "", "deepseek")
	if len(e.PendingApprovals("web-session")) != 0 {
		t.Fatal("Chat web calls requested a capability upgrade")
	}
	// A new daemon reuses the session load and existing local configuration.
	restarted := config.NewManager(home)
	if err := restarted.Prepare(); err != nil {
		t.Fatal(err)
	}
	definitions(New(st, event.NewHub(), nil, st, WithPlugins(plugin.NewService(home, restarted)), WithTools(runner)), true)
	if requests.Load() != 4 {
		t.Fatalf("unexpected upstream request count: %d", requests.Load())
	}
	// Saving another standard account never selects it. Explicitly binding it
	// changes the next Chat call without a mode upgrade or plugin reload.
	if err := cfg.PutPluginConnection(ctx, &plugin.Connection{ID:"second-account",PluginID:plugin.BuiltinWebSearchFetchID,Auth:plugin.Auth{MethodID:"deepseek",Type:plugin.AuthTypeBearer,Token:"ds-second"}}); err != nil { t.Fatal(err) }
	if _, key, err := cfg.ResolveWebTool(ctx,"search"); err != nil || key != deepseekKey { t.Fatal("saving selected another account",err) }
	if _, err := plugins.PatchConnectionBindings(ctx,plugin.BuiltinWebSearchFetchID,map[string]string{"search":"second-account"}); err != nil { t.Fatal(err) }
	deepseekKey = "ds-second"
	run(tool.WebSearch,defs,"","deepseek")
	if len(e.PendingApprovals("web-session")) != 0 { t.Fatal("standard connection required a mode upgrade") }
	out, changed = e.unloadPlugin(ctx, "web-session", tool.Call{Name: tool.PluginUnload, Args: json.RawMessage(fmt.Sprintf(`{"plugin_id":%q}`, plugin.BuiltinWebSearchFetchID))})
	if !out.Ok || !changed {
		t.Fatal(out)
	}
	definitions(e, false)
	assertEnabled(true)
}
