package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/app"
)

func TestBuiltinAppSaveCreatesAndUpdatesValidatedPackage(t *testing.T) {
	homeDir := t.TempDir()
	apps := app.NewService(homeDir, nil)
	runner := NewBuiltinRunner(WithAppAuthoring(apps))

	create := callAppSave(t, runner, appSaveRequest{
		Operation: "create",
		AppID:     "example-service",
		Version:   "0.1.0",
		Files: []appSaveRequestFile{
			{Path: "app.yaml", Content: testAuthoredAppManifest("0.1.0", "Example Service")},
			{Path: "skills/example/SKILL.md", Content: "---\nname: example-records\ndescription: Read Example Service records.\n---\n\nUse example_rest.\n"},
			{Path: "assets/icon.svg", Content: `<svg xmlns="http://www.w3.org/2000/svg"></svg>`},
		},
	})
	if !create.Ok {
		t.Fatalf("create App: %+v", create)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(create.Content), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["operation"] != "created" || payload["appID"] != "example-service" || payload["connectionRequired"] != true {
		t.Fatalf("unexpected create payload: %+v", payload)
	}

	conflict := callAppSave(t, runner, appSaveRequest{
		Operation: "create",
		AppID:     "example-service",
		Version:   "0.1.0",
		Files:     []appSaveRequestFile{{Path: "app.yaml", Content: testAuthoredAppManifest("0.1.0", "Replacement")}},
	})
	if conflict.Ok || !jsonReasonIs(conflict.Content, "app_exists") {
		t.Fatalf("create should refuse replacement: %+v", conflict)
	}

	update := callAppSave(t, runner, appSaveRequest{
		Operation: "update",
		AppID:     "example-service",
		Version:   "0.2.0",
		Files: []appSaveRequestFile{
			{Path: "app.yaml", Content: testAuthoredAppManifest("0.2.0", "Example Service Updated")},
			{Path: "skills/example/SKILL.md", Content: "---\nname: example-records\ndescription: Read and update Example Service records.\n---\n\nUse example_rest.\n"},
			{Path: "assets/icon.svg", Content: `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`},
		},
	})
	if !update.Ok {
		t.Fatalf("update App: %+v", update)
	}
	definition, err := findInstalledApp(context.Background(), apps, "example-service")
	if err != nil {
		t.Fatal(err)
	}
	if definition.Version != "0.2.0" || definition.Name != "Example Service Updated" {
		t.Fatalf("unexpected updated App: %+v", definition)
	}
}

func TestBuiltinAppSaveInvalidUpdatePreservesInstalledApp(t *testing.T) {
	homeDir := t.TempDir()
	apps := app.NewService(homeDir, nil)
	runner := NewBuiltinRunner(WithAppAuthoring(apps))
	initial := appSaveRequest{
		Operation: "create",
		AppID:     "example-service",
		Version:   "0.1.0",
		Files: []appSaveRequestFile{
			{Path: "app.yaml", Content: testAuthoredAppManifest("0.1.0", "Original")},
			{Path: "skills/example/SKILL.md", Content: "---\nname: example-records\ndescription: Read Example Service records.\n---\n\nUse example_rest.\n"},
			{Path: "assets/icon.svg", Content: `<svg xmlns="http://www.w3.org/2000/svg"></svg>`},
		},
	}
	if result := callAppSave(t, runner, initial); !result.Ok {
		t.Fatalf("create App: %+v", result)
	}
	manifestPath := filepath.Join(homeDir, "apps", "example-service", "app.yaml")
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	invalid := callAppSave(t, runner, appSaveRequest{
		Operation: "update",
		AppID:     "example-service",
		Version:   "0.2.0",
		Files: []appSaveRequestFile{{
			Path:    "app.yaml",
			Content: "id: another-app\nname: Broken\nversion: 0.2.0\n",
		}},
	})
	if invalid.Ok || !jsonReasonIs(invalid.Content, "app_save_failed") {
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

func callAppSave(t *testing.T, runner *BuiltinRunner, request appSaveRequest) Result {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return runner.Call(context.Background(), Call{Name: AppSave, CallID: "app-save", Args: raw})
}

func testAuthoredAppManifest(version, name string) string {
	return "id: example-service\n" +
		"name: " + name + "\n" +
		"version: " + version + "\n" +
		"icon:\n  svg: assets/icon.svg\n" +
		"auth:\n  required: true\n  methods:\n    - id: bearer\n      type: bearer\n" +
		"endpoints:\n  example_rest:\n    kind: rest\n    url: https://api.example.test\n" +
		"skills:\n  - skills/example/SKILL.md\n"
}

func findInstalledApp(ctx context.Context, apps *app.Service, id string) (*app.Definition, error) {
	definitions, err := apps.ListDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	for _, definition := range definitions {
		if definition != nil && definition.ID == id {
			return definition, nil
		}
	}
	return nil, app.ErrNotFound
}

func jsonReasonIs(content, reason string) bool {
	var payload map[string]any
	return json.Unmarshal([]byte(content), &payload) == nil && payload["reason"] == reason
}
