package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestPluginIDRenamePreservesLoadUnloadAndAuthorization(t *testing.T) {
	for old, current := range map[string]string{"artifacts": "artifact", "markdown": "page-authoring", "note-authoring": "page-authoring", "tables": "sheet-authoring", "collaboration": "subagent", "web": "web-search-fetch"} {
		t.Run(old, func(t *testing.T) {
			ctx := context.Background()
			st := storetest.New(t)
			plugins := plugin.NewService(t.TempDir(), st.Manager)
			e := New(st, event.NewHub(), nil, st, WithPlugins(plugins))
			if err := st.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock"}); err != nil {
				t.Fatal(err)
			}
			call := func(id, skill string) tool.Call {
				raw, _ := json.Marshal(map[string]string{"plugin_id": id, "skill_id": skill})
				return tool.Call{Name: tool.PluginLoad, Args: raw}
			}
			mode := store.ModeChat
			if current == "subagent" {
				mode = store.ModeWork
			}
			out, changed := e.loadPlugin(ctx, "s", call(old, old), mode)
			if !out.Ok || !changed {
				t.Fatal(out, changed)
			}
			var payload struct {
				PluginID string `json:"pluginID"`
				SkillID  string `json:"skillID"`
			}
			if err := json.Unmarshal([]byte(out.Content), &payload); err != nil || payload.PluginID != current || payload.SkillID != current {
				t.Fatal(out, err)
			}
			out, changed = e.loadPlugin(ctx, "s", call(current, current), mode)
			if !out.Ok || changed {
				t.Fatal("duplicate load", out, changed)
			}
			sess, err := st.GetSession(ctx, "s")
			if err != nil || len(sess.LoadedPluginIDs) != 1 || sess.LoadedPluginIDs[0] != current {
				t.Fatal(sess, err)
			}
			raw, _ := json.Marshal(map[string]string{"plugin_id": old})
			out, changed = e.unloadPlugin(ctx, "s", tool.Call{Name: tool.PluginUnload, Args: raw})
			if !out.Ok || !changed {
				t.Fatal("legacy unload", out, changed)
			}
			if _, err := plugins.SetEnabled(ctx, old, false); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{old, current} {
				out, _ = e.loadPlugin(ctx, "s", call(id, old), mode)
				if out.Ok || !strings.Contains(out.Content, `"reason":"plugin_disabled"`) {
					t.Fatal("disabled alias bypass", out)
				}
			}
		})
	}
}

func TestSubagentPluginAliasCannotBypassChildOrModeRestrictions(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	e := New(st, event.NewHub(), nil, st, WithPlugins(plugin.NewService(t.TempDir(), st.Manager)))
	if err := st.CreateSession(ctx, &store.Session{ID: "parent", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateChildSession(ctx, "parent", &store.Session{ID: "child", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"collaboration", "subagent"} {
		raw, _ := json.Marshal(map[string]string{"plugin_id": id})
		call := tool.Call{Name: tool.PluginLoad, Args: raw}
		out, _ := e.loadPlugin(ctx, "child", call, store.ModeWork)
		if out.Ok || !strings.Contains(out.Content, `"reason":"child_delegation_unavailable"`) {
			t.Fatal(out)
		}
		out, _ = e.loadPlugin(ctx, "parent", call, store.ModeChat)
		if out.Ok || !strings.Contains(out.Content, `"reason":"capability_required"`) {
			t.Fatal(out)
		}
	}
}
