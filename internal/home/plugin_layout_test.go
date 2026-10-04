package home

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLayoutFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readLayoutFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPrepareMovesAppLayoutToPlugins(t *testing.T) {
	dir := t.TempDir()
	writeLayoutFile(t, filepath.Join(dir, "apps", "github", "app.yaml"), "id: github\nkind: app\nname: GitHub\n")
	writeLayoutFile(t, filepath.Join(dir, "apps", "github", ".pudding-app-lock.json"), `{"kind":"pudding.app.lock","version":"1.0.5"}`)
	writeLayoutFile(t, filepath.Join(dir, "apps", "github", "skills", "issues", "SKILL.md"), "# Issues\n")
	writeLayoutFile(t, filepath.Join(dir, "apps", "local-tools", "app.yaml"), "id: local-tools\nkind: mcp\n")
	writeLayoutFile(t, filepath.Join(dir, "apps", "local-tools", ".pudding-mcp-overrides.yaml"), "endpoints: {}\n")
	writeLayoutFile(t, filepath.Join(dir, "config", "app-connections.yaml"), "version: 1\nconnections:\n    conn_1:\n        name: GitHub\n        app: github\n        auth:\n            type: token\n")
	writeLayoutFile(t, filepath.Join(dir, "config", "settings.yaml"), "version: 1\n# Display preferences\ndisplay:\n    reasoning: true\napps:\n    show_preview_versions: true\n    enabled:\n        app-authoring: false\n        canvas: true\n        github: false\n")
	for range 2 {
		if err := Prepare(dir); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "apps")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("apps directory remains: %v", err)
	}
	plugins := PluginsPath(dir)
	if got := readLayoutFile(t, filepath.Join(plugins, "github", "plugin.yaml")); !strings.Contains(got, "kind: plugin") || !strings.Contains(got, "id: github") {
		t.Fatalf("plugin manifest: %q", got)
	}
	if got := readLayoutFile(t, filepath.Join(plugins, "github", ".pudding-plugin-lock.json")); got != `{"kind":"pudding.plugin.lock","version":"1.0.5"}` {
		t.Fatalf("plugin lock: %q", got)
	}
	if got := readLayoutFile(t, filepath.Join(plugins, "local-tools", "plugin.yaml")); got != "id: local-tools\nkind: mcp\n" {
		t.Fatalf("MCP manifest changed: %q", got)
	}
	for _, path := range []string{"github/skills/issues/SKILL.md", "local-tools/.pudding-mcp-overrides.yaml"} {
		readLayoutFile(t, filepath.Join(plugins, path))
	}
	for _, path := range []string{"github/app.yaml", "github/.pudding-app-lock.json"} {
		if _, err := os.Stat(filepath.Join(plugins, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s remains: %v", path, err)
		}
	}
	modePath := filepath.Join(t.TempDir(), "mode")
	writeLayoutFile(t, modePath, "")
	wantMode, err := os.Stat(modePath)
	if err != nil {
		t.Fatal(err)
	}
	connections := filepath.Join(dir, "config", "plugin-connections.yaml")
	if got := readLayoutFile(t, connections); !strings.Contains(got, "plugin: github") || strings.Contains(got, "app:") || !strings.Contains(got, "type: token") {
		t.Fatalf("connections: %q", got)
	}
	if info, err := os.Stat(connections); err != nil || info.Mode().Perm() != wantMode.Mode().Perm() {
		t.Fatalf("connections mode: %v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config", "app-connections.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old connections remain: %v", err)
	}
	settings := readLayoutFile(t, filepath.Join(dir, "config", "settings.yaml"))
	for _, want := range []string{"# Display preferences", "reasoning: true", "plugins:", "show_preview_versions: true", "plugin-authoring: false", "widget-authoring: true", "github: false"} {
		if !strings.Contains(settings, want) {
			t.Fatalf("settings missing %q:\n%s", want, settings)
		}
	}
	if strings.Contains(settings, "apps:") || strings.Contains(settings, "app-authoring") || strings.Contains(settings, "canvas:") {
		t.Fatalf("settings kept App names:\n%s", settings)
	}
}

func TestPrepareFinishesInterruptedPluginConnectionsMove(t *testing.T) {
	dir := t.TempDir()
	writeLayoutFile(t, filepath.Join(dir, "config", "plugin-connections.yaml"), "version: 1\nconnections: {}\n")
	writeLayoutFile(t, filepath.Join(dir, "config", "app-connections.yaml"), "version: 1\nconnections:\n    stale:\n        app: github\n")
	if err := Prepare(dir); err != nil {
		t.Fatal(err)
	}
	if got := readLayoutFile(t, filepath.Join(dir, "config", "plugin-connections.yaml")); got != "version: 1\nconnections: {}\n" {
		t.Fatalf("completed move was rewritten: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "config", "app-connections.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old connections remain: %v", err)
	}
}

func TestPrepareKeepsPluginNameConflicts(t *testing.T) {
	dir := t.TempDir()
	writeLayoutFile(t, filepath.Join(dir, "apps", "github", "app.yaml"), "id: github\n")
	writeLayoutFile(t, filepath.Join(dir, "plugins", "github", "plugin.yaml"), "id: github\nname: Newer\n")
	if err := Prepare(dir); err != nil {
		t.Fatal(err)
	}
	if got := readLayoutFile(t, filepath.Join(dir, "plugins", "github", "plugin.yaml")); got != "id: github\nname: Newer\n" {
		t.Fatalf("existing plugin overwritten: %q", got)
	}
	readLayoutFile(t, filepath.Join(dir, "apps", "github", "app.yaml"))
}
