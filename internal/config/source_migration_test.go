package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWidgetSourcesMigrateHistoricalAliases(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "config")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, widgetSourcesFile)
	original := []byte(`version: 1
urls:
  - https://teatak.github.io/pudding-hub/extensions/widgets/registry.json
  - https://EXAMPLE.com:443/widgets/registry.json
  - https://teatak.github.io/pudding-hub/widgets/registry.json
  - https://teatak.github.io/pudding-hub/index.json?source=teatak%2Fpudding-hub
  - https://example.com/widgets/registry.json
  - https://teatak.github.io/pudding-hub/widgets/registry-v2.json
  - https://other.test/widgets/registry.json
`)
	if err := os.WriteFile(file, original, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	m := NewManager(home)
	sources, err := m.WidgetSources(ctx)
	if err != nil || len(sources) != 3 {
		t.Fatalf("migration: %+v %v", sources, err)
	}
	if !sources[0].Official || sources[1].URL != "https://example.com/widgets/registry.json" || sources[2].URL != "https://other.test/widgets/registry.json" {
		t.Fatal(sources)
	}
	before, err := os.ReadFile(file)
	if err != nil || string(before) != string(original) {
		t.Fatal("listing rewrote configuration", err)
	}
	restarted, err := NewManager(home).WidgetSources(ctx)
	if err != nil || len(restarted) != 3 || restarted[1] != sources[1] {
		t.Fatal(restarted, err)
	}
	added, err := m.AddWidgetSource(ctx, "https://new.test/widgets/registry.json")
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := NewManager(home).WidgetSources(ctx)
	if err != nil || len(persisted) != 4 || persisted[3] != added {
		t.Fatal(persisted, err)
	}
	if err := m.RemoveWidgetSource(ctx, sources[1].ID); err != nil {
		t.Fatal(err)
	}
	removed, err := NewManager(home).WidgetSources(ctx)
	if err != nil || len(removed) != 3 || removed[1] != sources[2] {
		t.Fatal(removed, err)
	}
	// A bad entry must still fail without rewriting or discarding the user's file.
	invalid := append(append([]byte{}, original...), []byte("  - http://invalid.test/registry.json\n")...)
	if err := os.WriteFile(file, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.WidgetSources(ctx); err == nil {
		t.Fatal("accepted an invalid saved source")
	}
	after, err := os.ReadFile(file)
	if err != nil || string(after) != string(invalid) {
		t.Fatal("failed read changed the configuration", err)
	}
}

func TestPluginSourcesMigrateHistoricalAliases(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "config")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, pluginSourcesFile)
	original := []byte(`version: 1
urls:
  - https://teatak.github.io/pudding-hub/extensions/plugins/registry.json
  - https://EXAMPLE.com:443/plugins/registry.json
  - https://teatak.github.io/pudding-hub/plugins/registry.json
  - https://teatak.github.io/pudding-hub/index.json?source=teatak%2Fpudding-hub
  - https://example.com/plugins/registry.json
  - https://teatak.github.io/pudding-hub/plugins/registry-v2.json
  - https://other.test/plugins/registry.json
`)
	if err := os.WriteFile(file, original, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	m := NewManager(home)
	sources, err := m.PluginSources(ctx)
	if err != nil || len(sources) != 3 {
		t.Fatalf("migration: %+v %v", sources, err)
	}
	if !sources[0].Official || sources[1].URL != "https://example.com/plugins/registry.json" || sources[2].URL != "https://other.test/plugins/registry.json" {
		t.Fatal(sources)
	}
	before, err := os.ReadFile(file)
	if err != nil || string(before) != string(original) {
		t.Fatal("listing rewrote configuration", err)
	}
	restarted, err := NewManager(home).PluginSources(ctx)
	if err != nil || len(restarted) != 3 || restarted[1] != sources[1] {
		t.Fatal(restarted, err)
	}
	added, err := m.AddPluginSource(ctx, "https://new.test/plugins/registry.json")
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := NewManager(home).PluginSources(ctx)
	if err != nil || len(persisted) != 4 || persisted[3] != added {
		t.Fatal(persisted, err)
	}
	if err := m.RemovePluginSource(ctx, sources[1].ID); err != nil {
		t.Fatal(err)
	}
	removed, err := NewManager(home).PluginSources(ctx)
	if err != nil || len(removed) != 3 || removed[1] != sources[2] {
		t.Fatal(removed, err)
	}
	// A bad entry must still fail without rewriting or discarding the user's file.
	invalid := append(append([]byte{}, original...), []byte("  - http://invalid.test/registry.json\n")...)
	if err := os.WriteFile(file, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PluginSources(ctx); err == nil {
		t.Fatal("accepted an invalid saved source")
	}
	after, err := os.ReadFile(file)
	if err != nil || string(after) != string(invalid) {
		t.Fatal("failed read changed the configuration", err)
	}
}
