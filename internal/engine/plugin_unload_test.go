package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestPluginUnloadIsSessionScopedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	ms := storetest.New(t)
	plugins := plugin.NewService(t.TempDir(), nil)
	eng := New(ms, event.NewHub(), nil, ms, WithPlugins(plugins))
	if err := ms.CreateSession(ctx, &store.Session{
		ID:              "session-a",
		Provider:        "mock",
		Model:           "mock",
		LoadedPluginIDs: []string{plugin.BuiltinBrowserID, plugin.BuiltinCaptureID},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.CreateSession(ctx, &store.Session{
		ID:              "session-b",
		Provider:        "mock",
		Model:           "mock",
		LoadedPluginIDs: []string{plugin.BuiltinBrowserID},
	}); err != nil {
		t.Fatal(err)
	}

	defs, err := eng.toolDefinitions(ctx, "session-a", store.ModeChat)
	if err != nil {
		t.Fatal(err)
	}
	unloadDef, ok := providerToolDefinition(defs, tool.PluginUnload)
	if !ok {
		t.Fatal("loaded session missing App unload tool")
	}
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(unloadDef.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if got := schema.Properties["plugin_id"].Enum; len(got) != 2 || got[0] != plugin.BuiltinBrowserID || got[1] != plugin.BuiltinCaptureID {
		t.Fatalf("App unload enum = %+v", got)
	}

	call := tool.Call{CallID: "unload-browser", Name: tool.PluginUnload, Args: json.RawMessage(`{"plugin_id":"browser"}`)}
	result, changed := eng.unloadPlugin(ctx, "session-a", call)
	if !result.Ok || !changed || !containsJSONField(result.Content, `"newlyUnloaded":true`) {
		t.Fatalf("App unload result = %+v, changed=%v", result, changed)
	}
	sessionA, err := ms.GetSession(ctx, "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessionA.LoadedPluginIDs) != 1 || sessionA.LoadedPluginIDs[0] != plugin.BuiltinCaptureID {
		t.Fatalf("session-a loaded Apps = %+v", sessionA.LoadedPluginIDs)
	}
	sessionB, err := ms.GetSession(ctx, "session-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessionB.LoadedPluginIDs) != 1 || sessionB.LoadedPluginIDs[0] != plugin.BuiltinBrowserID {
		t.Fatalf("unload leaked into session-b: %+v", sessionB.LoadedPluginIDs)
	}

	result, changed = eng.unloadPlugin(ctx, "session-a", call)
	if !result.Ok || changed || !containsJSONField(result.Content, `"alreadyUnloaded":true`) {
		t.Fatalf("repeated App unload result = %+v, changed=%v", result, changed)
	}
}

func TestPluginUnloadToolHiddenWithoutLoadedPlugins(t *testing.T) {
	ctx := context.Background()
	ms := storetest.New(t)
	eng := New(ms, event.NewHub(), nil, ms, WithPlugins(plugin.NewService(t.TempDir(), nil)))
	if err := ms.CreateSession(ctx, &store.Session{ID: "session", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	defs, err := eng.toolDefinitions(ctx, "session", store.ModeChat)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := providerToolDefinition(defs, tool.PluginUnload); ok {
		t.Fatal("App unload tool should be hidden when no Apps are loaded")
	}
}

func TestPluginUnloadRebuildsNextProviderRequest(t *testing.T) {
	ctx := context.Background()
	ms := storetest.New(t)
	client := &pluginUnloadClient{}
	eng := New(ms, event.NewHub(), mapResolver{"app-unload": client}, ms, WithPlugins(plugin.NewService(t.TempDir(), nil)))
	if err := ms.CreateSession(ctx, &store.Session{
		ID:              "session",
		Title:           "app unload",
		Provider:        "app-unload",
		Model:           "app-unload-model",
		LoadedPluginIDs: []string{plugin.BuiltinBrowserID},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "app-unload",
		Protocol:    "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "app-unload-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 2},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: "session", ClientMessageID: "unload", Text: "完成后卸载浏览器"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, "session")
	if len(client.requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(client.requests))
	}
	if !hasToolDef(client.requests[0].Tools, tool.PluginUnload) {
		t.Fatalf("initial provider request missing App unload tool: %+v", client.requests[0].Tools)
	}
	if hasToolDef(client.requests[1].Tools, tool.PluginUnload) {
		t.Fatal("App unload tool remained after the last loaded App was unloaded")
	}
	sess, err := ms.GetSession(ctx, "session")
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.LoadedPluginIDs) != 0 {
		t.Fatalf("loaded Apps after model unload = %+v", sess.LoadedPluginIDs)
	}
}

type pluginUnloadClient struct {
	requests []provider.Request
}

func (c *pluginUnloadClient) Name() string { return "app-unload" }

func (c *pluginUnloadClient) Stream(_ context.Context, request provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, request)
	out := make(chan provider.Chunk, 2)
	if len(c.requests) == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index: 0, CallID: "unload-browser", Name: tool.PluginUnload, ArgsDelta: `{"plugin_id":"browser"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "完成"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

func containsJSONField(content, field string) bool {
	return len(content) > 0 && json.Valid([]byte(content)) && strings.Contains(content, field)
}
