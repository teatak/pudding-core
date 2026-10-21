package config

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/plugin"
)

func TestWebConnectionsMigrationPreservesSelectionsAndDoesNotResurrectDeletedKeys(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	m := NewManager(home)
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := m.writeWebTools(webToolsYAML{Version: 1, Search: webToolFeatureYAML{Provider: "tavily"}, Fetch: webToolFeatureYAML{Provider: "tavily"}, Providers: map[string]webToolProviderYAML{"tavily": {APIKey: "tvly-old"}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPluginEnabled(ctx, "web", false); err != nil {
		t.Fatal(err)
	}
	connections, err := m.ListPluginConnections(ctx)
	if err != nil || len(connections) != 1 {
		t.Fatal(connections, err)
	}
	bindings, err := m.PluginConnectionBindings(ctx, "web")
	if err != nil {
		t.Fatal(err)
	}
	for purpose, want := range map[string]string{"active": "tavily"} {
		c, err := m.GetPluginConnection(ctx, bindings[purpose])
		if err != nil || c.PluginID != plugin.BuiltinWebSearchFetchID || c.Auth.MethodID != want || c.Auth.Token == "" {
			t.Fatal(purpose, c, err)
		}
	}
	if _, err := os.Stat(m.path(webToolsFile)); !os.IsNotExist(err) {
		t.Fatal("legacy credential file still active", err)
	}
	enabled, err := m.ListPluginEnablement(ctx)
	if err != nil || enabled[plugin.BuiltinWebSearchFetchID] {
		t.Fatal("migration enabled plugin", enabled, err)
	}
	info, err := os.Stat(m.path(pluginConnectionsFile))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential file permissions", err)
	}
	// Deleting a selected connection clears only its bindings. Restart never reimports it.
	if err := m.DeletePluginConnection(ctx, bindings["active"]); err != nil {
		t.Fatal(err)
	}
	m = NewManager(home)
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	got, err := m.PluginConnectionBindings(ctx, "web")
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	list, err := m.ListPluginConnections(ctx)
	if err != nil || len(list) != 0 {
		t.Fatal("deleted connection resurrected", list, err)
	}
}

func TestStandardConnectionsAndBindingsKeepSavingSelectionAndEnablementIndependent(t *testing.T) {
	ctx := context.Background()
	m := NewManager(t.TempDir())
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	service := plugin.NewService(m.homeDir, m)
	for _, c := range []*plugin.Connection{{ID: "one", PluginID: plugin.BuiltinWebSearchFetchID, Auth: plugin.Auth{MethodID: "tavily", Type: "bearer", Token: "key-one"}}, {ID: "two", PluginID: plugin.BuiltinWebSearchFetchID, Auth: plugin.Auth{MethodID: "tavily", Type: "bearer", Token: "key-two"}}, {ID: "foreign", PluginID: "custom", Auth: plugin.Auth{Type: "bearer", Token: "foreign"}}} {
		if err := m.PutPluginConnection(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := m.PluginConnectionBindings(ctx, "web")
	if len(b) != 0 {
		t.Fatal("saving selected a connection", b)
	}
	if name, key, err := m.ResolveWebTool(ctx, "search"); err != nil || name != "" || key != "" {
		t.Fatal("unselected connection was used", name, key, err)
	}
	if _, err := service.SetEnabled(ctx, "web", false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PatchConnectionBindings(ctx, "web", map[string]string{"active": "one"}); err != nil {
		t.Fatal(err)
	}
	saved, _ := m.PluginConnectionBindings(ctx, "web")
	for _, bad := range []map[string]string{{"search": "foreign"}, {"unknown": "one"}, {"search": "missing"}} {
		if _, err := service.PatchConnectionBindings(ctx, "web", bad); err == nil {
			t.Fatal("invalid binding accepted", bad)
		}
	}
	got, _ := m.PluginConnectionBindings(ctx, "web")
	if !reflect.DeepEqual(got, saved) {
		t.Fatal("invalid patch changed bindings", got)
	}
	if err := m.PutPluginConnection(ctx, &plugin.Connection{ID: "one", PluginID: plugin.BuiltinWebSearchFetchID, Auth: plugin.Auth{MethodID: "deepseek", Type: "bearer", Token: "replacement"}}); err == nil {
		t.Fatal("saving retargeted a bound connection")
	}
	if err := m.PutPluginConnection(ctx, &plugin.Connection{ID: "one", PluginID: plugin.BuiltinWebSearchFetchID, Auth: plugin.Auth{MethodID: "tavily", Type: "bearer", Token: "updated"}}); err != nil {
		t.Fatal(err)
	}
	enabled, _ := m.ListPluginEnablement(ctx)
	if enabled[plugin.BuiltinWebSearchFetchID] {
		t.Fatal("binding or saving enabled plugin")
	}
	if _, err := service.SetEnabled(ctx, "web", true); err != nil {
		t.Fatal(err)
	}
	for purpose, want := range map[string]string{"search": "updated", "fetch": "updated"} {
		name, key, err := m.ResolveWebTool(ctx, purpose)
		if err != nil || name != "tavily" || key != want {
			t.Fatal(purpose, name, key, err)
		}
	}
	if err := m.DeletePluginConnection(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	if name, key, err := m.ResolveWebTool(ctx, "search"); err != nil || name != "" || key != "" {
		t.Fatal("deleted selection fell back to another account", name, key, err)
	}
	view, err := m.WebTools(ctx)
	if err != nil || view.FetchProvider != "" || view.SearchProvider != "" {
		t.Fatal("legacy view did not reflect bindings", view, err)
	}
}

func TestWebMigrationKeepsLegacyKeyWhenStandardProviderAlreadyExists(t *testing.T) {
	ctx := context.Background()
	m := NewManager(t.TempDir())
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := m.writePluginConnections(pluginConnectionsYAML{Version: 1, Connections: map[string]*plugin.Connection{"existing": {PluginID: plugin.BuiltinWebSearchFetchID, Auth: plugin.Auth{MethodID: "tavily", Type: "bearer", Token: "standard-key"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.writeWebTools(webToolsYAML{Version: 1, Search: webToolFeatureYAML{Provider: "tavily"}, Fetch: webToolFeatureYAML{Provider: "tavily"}, Providers: map[string]webToolProviderYAML{"tavily": {APIKey: "release-key"}}}); err != nil {
		t.Fatal(err)
	}
	for _, feature := range []string{"search", "fetch"} {
		name, key, err := m.ResolveWebTool(ctx, feature)
		if err != nil || name != "tavily" || key != "release-key" {
			t.Fatal("previous release selection lost", name, key, err)
		}
	}
	connections, err := m.ListPluginConnections(ctx)
	if err != nil || len(connections) != 2 {
		t.Fatal(connections, err)
	}
	existing, err := m.GetPluginConnection(ctx, "existing")
	if err != nil || existing.Auth.Token != "standard-key" {
		t.Fatal("existing standard credential overwritten", existing, err)
	}
}
