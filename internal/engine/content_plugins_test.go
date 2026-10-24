package engine

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestContentPluginsLoadIndependently(t *testing.T) {
	groups := map[string][]string{
		plugin.BuiltinArtifactsID:      {tool.ArtifactList, tool.ArtifactOpen},
		plugin.BuiltinPageAuthoringID:  {tool.PageCreate, tool.PageRead, tool.PageEdit, tool.PageRename},
		plugin.BuiltinSheetAuthoringID: {tool.SheetCreate, tool.SheetRead, tool.SheetEdit},
	}
	for id, want := range groups {
		t.Run(id, func(t *testing.T) {
			ctx := context.Background()
			st := storetest.New(t)
			cfg := config.NewManager(t.TempDir())
			if err := cfg.Prepare(); err != nil {
				t.Fatal(err)
			}
			plugins := plugin.NewService(t.TempDir(), cfg)
			e := New(st, event.NewHub(), nil, st, WithPlugins(plugins))
			if err := st.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock"}); err != nil {
				t.Fatal(err)
			}
			assertTools := func(loaded ...string) {
				t.Helper()
				defs, err := e.toolDefinitions(ctx, "s", store.ModeChat)
				if err != nil {
					t.Fatal(err)
				}
				for owner, names := range groups {
					allowed := false
					for _, id := range loaded {
						allowed = allowed || id == owner
					}
					for _, name := range names {
						if tool.HasDefinition(defs, name) != allowed {
							t.Fatalf("%s exposed incorrectly; loaded=%v", name, loaded)
						}
					}
				}
			}
			load := func(id string) {
				t.Helper()
				raw, _ := json.Marshal(map[string]string{"plugin_id": id})
				out, changed, _ := e.loadPlugin(ctx, "s", tool.Call{Name: tool.PluginLoad, Args: raw}, store.ModeChat)
				if !out.Ok || !changed {
					t.Fatalf("load %s: %+v", id, out)
				}
			}
			assertTools()
			load(id)
			assertTools(id)
			sess, err := st.GetSession(ctx, "s")
			if err != nil || !reflect.DeepEqual(sess.LoadedPluginIDs, []string{id}) {
				t.Fatalf("loading one type expanded session state: %+v, %v", sess, err)
			}
			def, _ := plugin.BuiltinDefinition(id)
			if len(def.Tools) != len(want) {
				t.Fatalf("catalog tools disagree: %+v", def.Tools)
			}
			skill, err := plugins.ReadSkill(ctx, id, def.DefaultSkillID)
			if err != nil || skill.ID != id || skill.Content == "" {
				t.Fatalf("missing independent instructions: %+v, %v", skill, err)
			}
			// A second content capability can coexist, without coupling either switch.
			other := plugin.BuiltinArtifactsID
			if id == other {
				other = plugin.BuiltinPageAuthoringID
			}
			load(other)
			assertTools(id, other)
			if _, err := plugins.SetEnabled(ctx, id, false); err != nil {
				t.Fatal(err)
			}
			assertTools(other)
			if _, err := plugins.SetEnabled(ctx, id, true); err != nil {
				t.Fatal(err)
			}
			assertTools(id, other)
			raw, _ := json.Marshal(map[string]string{"plugin_id": id})
			out, changed := e.unloadPlugin(ctx, "s", tool.Call{Name: tool.PluginUnload, Args: raw})
			if !out.Ok || !changed {
				t.Fatal(out)
			}
			assertTools(other)
		})
	}
}

func TestLegacyContentPluginCannotExecuteTypeTools(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	e := New(st, event.NewHub(), nil, st, WithPlugins(plugin.NewService(t.TempDir(), nil)))
	// Historical sessions normalize studio -> artifacts, without loading all types.
	if err := st.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock", LoadedPluginIDs: []string{"studio"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "s", TurnID: "turn", UserMessageID: "user", ClientMessageID: "input", UserText: "Inspect content"}); err != nil {
		t.Fatal(err)
	}
	defs, err := e.toolDefinitions(ctx, "s", store.ModeChat)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{tool.ArtifactList, tool.ArtifactOpen} {
		if !tool.HasDefinition(defs, name) {
			t.Fatal("missing common capability", name)
		}
	}
	parts := &turnPartAccumulator{}
	for index, name := range []string{tool.PageCreate, tool.SheetCreate} {
		if tool.HasDefinition(defs, name) {
			t.Fatal("historical load exposed type tool", name)
		}
		parts.AppendTool(provider.ToolCallChunk{Index: index, CallID: name, Name: name, ArgsDelta: `{"name":"Unexpected","body":"must not write"}`})
	}
	status, message, _, _ := e.executePendingTools(ctx, "s", "turn", store.ModeChat, defs, parts)
	if status != store.TurnRunning {
		t.Fatal(status, message)
	}
	count := 0
	for _, part := range parts.Parts() {
		if part.Type == store.ContentPartToolResult {
			count++
			if part.Ok || !strings.Contains(part.Content, `"reason":"plugin_not_loaded"`) {
				t.Fatalf("unloaded tool not blocked: %+v", part)
			}
		}
	}
	if count != 2 {
		t.Fatal("missing rejected tool results", count)
	}
	items, err := st.ListStudioItems(ctx, store.StudioItemsActive)
	if err != nil || len(items) != 0 {
		t.Fatal("unloaded creation changed data", items, err)
	}
}

func TestLegacyPageAliasesRespectPluginAvailability(t *testing.T) {
	for _, scenario := range []struct {
		name, reason                     string
		loaded, disabled, omitDefinition bool
	}{
		{name: "not loaded", reason: "plugin_not_loaded"},
		{name: "disabled after request", loaded: true, disabled: true, reason: "plugin_disabled"},
		{name: "not offered", loaded: true, omitDefinition: true, reason: "plugin_tool_unavailable"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := context.Background()
			st := storetest.New(t)
			cfg := config.NewManager(t.TempDir())
			if err := cfg.Prepare(); err != nil {
				t.Fatal(err)
			}
			plugins := plugin.NewService(t.TempDir(), cfg)
			e := New(st, event.NewHub(), nil, st, WithPlugins(plugins))
			var loaded []string
			if scenario.loaded {
				loaded = []string{plugin.BuiltinPageAuthoringID}
			}
			if err := st.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock", LoadedPluginIDs: loaded}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "s", TurnID: "turn", UserMessageID: "u", ClientMessageID: "i", UserText: "Create a document"}); err != nil {
				t.Fatal(err)
			}
			defs, err := e.toolDefinitions(ctx, "s", store.ModeChat)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.disabled {
				if _, err := plugins.SetEnabled(ctx, plugin.BuiltinPageAuthoringID, false); err != nil {
					t.Fatal(err)
				}
			}
			if scenario.omitDefinition {
				defs = nil
			}
			parts := &turnPartAccumulator{}
			for index, name := range []string{tool.PageCreate, "builtin_doc_create", "builtin_doc_read", "builtin_doc_edit"} {
				parts.AppendTool(provider.ToolCallChunk{Index: index, CallID: name, Name: name, ArgsDelta: `{"name":"Blocked","body":"must not write","item_id":"missing"}`})
			}
			status, message, _, _ := e.executePendingTools(ctx, "s", "turn", store.ModeChat, defs, parts)
			if status != store.TurnRunning {
				t.Fatal(status, message)
			}
			count := 0
			for _, part := range parts.Parts() {
				if part.Type != store.ContentPartToolResult {
					continue
				}
				count++
				if part.Ok || part.Name != part.CallID || !strings.Contains(part.Content, `"reason":"`+scenario.reason+`"`) {
					t.Fatalf("alias bypassed canonical availability: %+v", part)
				}
			}
			if count != 4 {
				t.Fatal("missing results", count)
			}
			items, err := st.ListStudioItems(ctx, store.StudioItemsActive)
			if err != nil || len(items) != 0 {
				t.Fatal("blocked aliases changed data", items, err)
			}
		})
	}
}

func TestLegacySheetAliasesRespectPluginAvailability(t *testing.T) {
	for _, scenario := range []struct {
		name, reason                     string
		loaded, disabled, omitDefinition bool
	}{
		{name: "not loaded", reason: "plugin_not_loaded"},
		{name: "disabled after request", loaded: true, disabled: true, reason: "plugin_disabled"},
		{name: "not offered", loaded: true, omitDefinition: true, reason: "plugin_tool_unavailable"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := context.Background()
			st := storetest.New(t)
			cfg := config.NewManager(t.TempDir())
			if err := cfg.Prepare(); err != nil {
				t.Fatal(err)
			}
			plugins := plugin.NewService(t.TempDir(), cfg)
			e := New(st, event.NewHub(), nil, st, WithPlugins(plugins))
			var loaded []string
			if scenario.loaded {
				loaded = []string{plugin.BuiltinSheetAuthoringID}
			}
			if err := st.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock", LoadedPluginIDs: loaded}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "s", TurnID: "turn", UserMessageID: "u", ClientMessageID: "i", UserText: "Create a document"}); err != nil {
				t.Fatal(err)
			}
			defs, err := e.toolDefinitions(ctx, "s", store.ModeChat)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.disabled {
				if _, err := plugins.SetEnabled(ctx, plugin.BuiltinSheetAuthoringID, false); err != nil {
					t.Fatal(err)
				}
			}
			if scenario.omitDefinition {
				defs = nil
			}
			parts := &turnPartAccumulator{}
			for index, name := range []string{tool.SheetCreate, "builtin_table_create", "builtin_table_read", "builtin_table_update"} {
				parts.AppendTool(provider.ToolCallChunk{Index: index, CallID: name, Name: name, ArgsDelta: `{"name":"Blocked","body":"must not write","item_id":"missing"}`})
			}
			status, message, _, _ := e.executePendingTools(ctx, "s", "turn", store.ModeChat, defs, parts)
			if status != store.TurnRunning {
				t.Fatal(status, message)
			}
			count := 0
			for _, part := range parts.Parts() {
				if part.Type != store.ContentPartToolResult {
					continue
				}
				count++
				if part.Ok || part.Name != part.CallID || !strings.Contains(part.Content, `"reason":"`+scenario.reason+`"`) {
					t.Fatalf("alias bypassed canonical availability: %+v", part)
				}
			}
			if count != 4 {
				t.Fatal("missing results", count)
			}
			items, err := st.ListStudioItems(ctx, store.StudioItemsActive)
			if err != nil || len(items) != 0 {
				t.Fatal("blocked aliases changed data", items, err)
			}
		})
	}
}
