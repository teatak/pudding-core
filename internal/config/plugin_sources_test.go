package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginSourcesPersistAndRemainIndependent(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	m := NewManager(home)
	first, err := m.PluginSources(ctx)
	if err != nil || len(first) != 1 || !first[0].Official {
		t.Fatalf("initial %+v %v", first, err)
	}
	added, err := m.AddPluginSource(ctx, " https://EXAMPLE.com:443/plugins/registry.json ")
	if err != nil || added.URL != "https://example.com/plugins/registry.json" {
		t.Fatalf("add %+v %v", added, err)
	}
	duplicate, err := m.AddPluginSource(ctx, added.URL)
	if err != nil || duplicate.ID != added.ID {
		t.Fatal("duplicate", duplicate, err)
	}
	restarted := NewManager(home)
	saved, err := restarted.PluginSources(ctx)
	if err != nil || len(saved) != 2 || saved[1] != added {
		t.Fatalf("restart %+v %v", saved, err)
	}
	if err := restarted.RemovePluginSource(ctx, "official"); err == nil {
		t.Fatal("removed official")
	}
	if err := restarted.RemovePluginSource(ctx, added.ID); err != nil {
		t.Fatal(err)
	}
	saved, err = NewManager(home).PluginSources(ctx)
	if err != nil || len(saved) != 1 {
		t.Fatal("remove not persisted", saved, err)
	}
	for _, raw := range []string{"file:///tmp/registry.json", "http://example.com/x", "https://user:pass@example.com/x", "https://example.com/x#fragment", "https://"} {
		if _, err := m.AddPluginSource(ctx, raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	path := filepath.Join(home, "config", pluginSourcesFile)
	if err := os.WriteFile(path, []byte("version: 99\nurls: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddPluginSource(ctx, added.URL); err == nil {
		t.Fatal("overwrote unsupported config")
	}
}
