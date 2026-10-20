package config

import (
	"context"
	"testing"
)

func TestWebToolsSaveAndSelectionAreIndependent(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	m := NewManager(home)
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	patch := func(update WebToolsUpdate, search, fetch string) *WebToolsView {
		t.Helper()
		view, err := m.PatchWebTools(ctx, update)
		if err != nil {
			t.Fatal(err)
		}
		if view.SearchProvider != search || view.FetchProvider != fetch {
			t.Fatalf("want search=%q fetch=%q, got %+v", search, fetch, view)
		}
		return view
	}
	keyUpdate := func(name, key string) WebToolsUpdate {
		return WebToolsUpdate{Providers: map[string]WebToolProviderUpdate{name: {APIKey: &key}}}
	}
	selectUpdate := func(search, fetch string) WebToolsUpdate {
		return WebToolsUpdate{SearchProvider: &search, FetchProvider: &fetch}
	}
	patch(keyUpdate("tavily", "tvly-first"), "", "")
	patch(selectUpdate("tavily", "tavily"), "tavily", "tavily")
	patch(keyUpdate("deepseek", "  ds-key  "), "tavily", "tavily")
	patch(keyUpdate("tavily", "tvly-updated"), "tavily", "tavily")
	patch(selectUpdate("deepseek", "tavily"), "deepseek", "tavily")

	// Restarting retains both credentials and independent feature selections.
	m = NewManager(home)
	for _, feature := range []string{"search", "fetch"} {
		name, key, err := m.ResolveWebTool(ctx, feature)
		wantName, wantKey := "deepseek", "ds-key"
		if feature == "fetch" {
			wantName, wantKey = "tavily", "tvly-updated"
		}
		if err != nil || name != wantName || key != wantKey {
			t.Fatalf("resolve %s: provider=%q key=%q err=%v", feature, name, key, err)
		}
	}
	// Clearing an inactive provider never changes the selected one.
	patch(selectUpdate("deepseek", "deepseek"), "deepseek", "deepseek")
	view := patch(keyUpdate("tavily", ""), "deepseek", "deepseek")
	if !view.Providers[1].APIKeySet || view.Providers[1].APIKey != "ds-key" {
		t.Fatalf("clearing Tavily must retain DeepSeek credentials: %+v", view)
	}
	patch(keyUpdate("tavily", "tvly-kept"), "deepseek", "deepseek")
	patch(keyUpdate("deepseek", ""), "", "")
	name, key, err := m.ResolveWebTool(ctx, "search")
	if err != nil || name != "" || key != "" {
		t.Fatalf("disabled search must not fall back to saved Tavily key: %q %q %v", name, key, err)
	}
	patch(keyUpdate("deepseek", "ds-new"), "", "")
	view = patch(selectUpdate("", ""), "", "")
	if !view.Providers[0].APIKeySet || !view.Providers[1].APIKeySet {
		t.Fatalf("disabling tools must retain both keys: %+v", view)
	}
}

func TestWebToolsLegacyTavilyConfig(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	// Existing web.yaml files need no migration and retain their selection.
	err := m.writeWebTools(webToolsYAML{
		Version: 1, Search: webToolFeatureYAML{Provider: "tavily"}, Fetch: webToolFeatureYAML{Provider: "tavily"},
		Providers: map[string]webToolProviderYAML{"tavily": {APIKey: "legacy-key"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, feature := range []string{"search", "fetch"} {
		name, key, err := m.ResolveWebTool(context.Background(), feature)
		if err != nil || name != "tavily" || key != "legacy-key" {
			t.Fatalf("legacy %s config changed: %q %q %v", feature, name, key, err)
		}
	}
}
