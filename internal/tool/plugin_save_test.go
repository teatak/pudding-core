package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/plugin"
)

func TestBuiltinPluginSaveCreatesAndUpdatesValidatedPackage(t *testing.T) {
	homeDir := t.TempDir()
	plugins := plugin.NewService(homeDir, nil)
	runner := NewBuiltinRunner(WithPluginAuthoring(plugins))

	create := callPluginSave(t, runner, pluginSaveRequest{
		Operation: "create",
		PluginID:  "example-service",
		Version:   "0.1.0",
		Files: []pluginSaveRequestFile{
			{Path: "plugin.yaml", Content: testAuthoredPluginManifest("0.1.0", "Example Service")},
			{Path: "skills/example/SKILL.md", Content: "---\nname: example-records\ndescription: Read Example Service records.\n---\n\nUse example_rest.\n"},
			{Path: "assets/icon.svg", Content: `<svg xmlns="http://www.w3.org/2000/svg"></svg>`},
		},
	})
	if !create.Ok {
		t.Fatalf("create plugin: %+v", create)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(create.Content), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["operation"] != "created" || payload["pluginID"] != "example-service" || payload["connectionRequired"] != true {
		t.Fatalf("unexpected create payload: %+v", payload)
	}

	conflict := callPluginSave(t, runner, pluginSaveRequest{
		Operation: "create",
		PluginID:  "example-service",
		Version:   "0.1.0",
		Files:     []pluginSaveRequestFile{{Path: "plugin.yaml", Content: testAuthoredPluginManifest("0.1.0", "Replacement")}},
	})
	if conflict.Ok || !jsonReasonIs(conflict.Content, "plugin_exists") {
		t.Fatalf("create should refuse replacement: %+v", conflict)
	}

	update := callPluginSave(t, runner, pluginSaveRequest{
		Operation: "update",
		PluginID:  "example-service",
		Version:   "0.2.0",
		Files: []pluginSaveRequestFile{
			{Path: "plugin.yaml", Content: testAuthoredPluginManifest("0.2.0", "Example Service Updated")},
			{Path: "skills/example/SKILL.md", Content: "---\nname: example-records\ndescription: Read and update Example Service records.\n---\n\nUse example_rest.\n"},
			{Path: "assets/icon.svg", Content: `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`},
		},
	})
	if !update.Ok {
		t.Fatalf("update plugin: %+v", update)
	}
	definition, err := findInstalledPlugin(context.Background(), plugins, "example-service")
	if err != nil {
		t.Fatal(err)
	}
	if definition.Version != "0.2.0" || definition.Name != "Example Service Updated" {
		t.Fatalf("unexpected updated plugin: %+v", definition)
	}
}

func TestBuiltinPluginSaveInvalidUpdatePreservesInstalledPlugin(t *testing.T) {
	homeDir := t.TempDir()
	plugins := plugin.NewService(homeDir, nil)
	runner := NewBuiltinRunner(WithPluginAuthoring(plugins))
	initial := pluginSaveRequest{
		Operation: "create",
		PluginID:  "example-service",
		Version:   "0.1.0",
		Files: []pluginSaveRequestFile{
			{Path: "plugin.yaml", Content: testAuthoredPluginManifest("0.1.0", "Original")},
			{Path: "skills/example/SKILL.md", Content: "---\nname: example-records\ndescription: Read Example Service records.\n---\n\nUse example_rest.\n"},
			{Path: "assets/icon.svg", Content: `<svg xmlns="http://www.w3.org/2000/svg"></svg>`},
		},
	}
	if result := callPluginSave(t, runner, initial); !result.Ok {
		t.Fatalf("create plugin: %+v", result)
	}
	manifestPath := filepath.Join(homeDir, "plugins", "example-service", "plugin.yaml")
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	invalid := callPluginSave(t, runner, pluginSaveRequest{
		Operation: "update",
		PluginID:  "example-service",
		Version:   "0.2.0",
		Files: []pluginSaveRequestFile{{
			Path:    "plugin.yaml",
			Content: "id: another-plugin\nname: Broken\nversion: 0.2.0\n",
		}},
	})
	if invalid.Ok || !jsonReasonIs(invalid.Content, "plugin_save_failed") {
		t.Fatalf("invalid update should fail: %+v", invalid)
	}
	after, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("invalid update changed installed manifest:\n%s", after)
	}
}

func callPluginSave(t *testing.T, runner *BuiltinRunner, request pluginSaveRequest) Result {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return runner.Call(context.Background(), Call{Name: PluginSave, CallID: "plugin-save", Args: raw})
}

func testAuthoredPluginManifest(version, name string) string {
	return "id: example-service\n" +
		"name: " + name + "\n" +
		"version: " + version + "\n" +
		"icon:\n  svg: assets/icon.svg\n" +
		"auth:\n  required: true\n  methods:\n    - id: bearer\n      type: bearer\n" +
		"endpoints:\n  example_rest:\n    kind: rest\n    url: https://api.example.test\n" +
		"skills:\n  - skills/example/SKILL.md\n"
}

func findInstalledPlugin(ctx context.Context, plugins *plugin.Service, id string) (*plugin.Definition, error) {
	definitions, err := plugins.ListDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	for _, definition := range definitions {
		if definition != nil && definition.ID == id {
			return definition, nil
		}
	}
	return nil, plugin.ErrNotFound
}

func jsonReasonIs(content, reason string) bool {
	var payload map[string]any
	return json.Unmarshal([]byte(content), &payload) == nil && payload["reason"] == reason
}
