package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestAppUnloadIsSessionScopedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	apps := app.NewService(t.TempDir(), nil)
	eng := New(ms, event.NewHub(), nil, ms, WithApps(apps))
	if err := ms.CreateSession(ctx, &store.Session{
		ID:           "session-a",
		Provider:     "mock",
		Model:        "mock",
		LoadedAppIDs: []string{app.BuiltinBrowserID, app.BuiltinCaptureID},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.CreateSession(ctx, &store.Session{
		ID:           "session-b",
		Provider:     "mock",
		Model:        "mock",
		LoadedAppIDs: []string{app.BuiltinBrowserID},
	}); err != nil {
		t.Fatal(err)
	}

	defs, err := eng.toolDefinitions(ctx, "session-a", store.ModeChat)
	if err != nil {
		t.Fatal(err)
	}
	unloadDef, ok := providerToolDefinition(defs, tool.AppUnload)
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
	if got := schema.Properties["app_id"].Enum; len(got) != 2 || got[0] != app.BuiltinBrowserID || got[1] != app.BuiltinCaptureID {
		t.Fatalf("App unload enum = %+v", got)
	}

	call := tool.Call{CallID: "unload-browser", Name: tool.AppUnload, Args: json.RawMessage(`{"app_id":"browser"}`)}
	result, changed := eng.unloadApp(ctx, "session-a", call)
	if !result.Ok || !changed || !containsJSONField(result.Content, `"newlyUnloaded":true`) {
		t.Fatalf("App unload result = %+v, changed=%v", result, changed)
	}
	sessionA, err := ms.GetSession(ctx, "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessionA.LoadedAppIDs) != 1 || sessionA.LoadedAppIDs[0] != app.BuiltinCaptureID {
		t.Fatalf("session-a loaded Apps = %+v", sessionA.LoadedAppIDs)
	}
	sessionB, err := ms.GetSession(ctx, "session-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessionB.LoadedAppIDs) != 1 || sessionB.LoadedAppIDs[0] != app.BuiltinBrowserID {
		t.Fatalf("unload leaked into session-b: %+v", sessionB.LoadedAppIDs)
	}

	result, changed = eng.unloadApp(ctx, "session-a", call)
	if !result.Ok || changed || !containsJSONField(result.Content, `"alreadyUnloaded":true`) {
		t.Fatalf("repeated App unload result = %+v, changed=%v", result, changed)
	}
}

func TestAppUnloadToolHiddenWithoutLoadedApps(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	eng := New(ms, event.NewHub(), nil, ms, WithApps(app.NewService(t.TempDir(), nil)))
	if err := ms.CreateSession(ctx, &store.Session{ID: "session", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	defs, err := eng.toolDefinitions(ctx, "session", store.ModeChat)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := providerToolDefinition(defs, tool.AppUnload); ok {
		t.Fatal("App unload tool should be hidden when no Apps are loaded")
	}
}

func TestAppUnloadRebuildsNextProviderRequest(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	client := &appUnloadClient{}
	eng := New(ms, event.NewHub(), mapResolver{"app-unload": client}, ms, WithApps(app.NewService(t.TempDir(), nil)))
	if err := ms.CreateSession(ctx, &store.Session{
		ID:           "session",
		Title:        "app unload",
		Provider:     "app-unload",
		Model:        "app-unload-model",
		LoadedAppIDs: []string{app.BuiltinBrowserID},
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
	if !hasToolDef(client.requests[0].Tools, tool.AppUnload) {
		t.Fatalf("initial provider request missing App unload tool: %+v", client.requests[0].Tools)
	}
	if hasToolDef(client.requests[1].Tools, tool.AppUnload) {
		t.Fatal("App unload tool remained after the last loaded App was unloaded")
	}
	sess, err := ms.GetSession(ctx, "session")
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.LoadedAppIDs) != 0 {
		t.Fatalf("loaded Apps after model unload = %+v", sess.LoadedAppIDs)
	}
}

type appUnloadClient struct {
	requests []provider.Request
}

func (c *appUnloadClient) Name() string { return "app-unload" }

func (c *appUnloadClient) Stream(_ context.Context, request provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, request)
	out := make(chan provider.Chunk, 2)
	if len(c.requests) == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index: 0, CallID: "unload-browser", Name: tool.AppUnload, ArgsDelta: `{"app_id":"browser"}`,
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
