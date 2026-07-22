package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/teatak/pudding-core/internal/home"
)

type fakeConnectionStore struct {
	items map[string]*Connection
}

func (f fakeConnectionStore) ListAppConnections(context.Context) ([]*Connection, error) {
	out := make([]*Connection, 0, len(f.items))
	for _, item := range f.items {
		out = append(out, CloneConnection(item))
	}
	return out, nil
}

type fakeAppConfig struct {
	fakeConnectionStore
	enabled       map[string]bool
	enablementErr error
}

type fakeRuntimeSource struct{}

func (fakeRuntimeSource) ListRuntimeDefinitions(_ context.Context, runtimeID string) ([]*Definition, error) {
	if runtimeID != "desktop_a" {
		return nil, nil
	}
	return []*Definition{{
		ID:             "canvas",
		Name:           "Canvas",
		Runtime:        "desktop",
		RequiredMode:   "chat",
		DefaultSkillID: "canvas",
		Skills:         []SkillRef{{ID: "canvas", Name: "Canvas", Path: "skills/canvas/SKILL.md"}},
		Tools:          []ToolRef{{Name: "canvas_markdown"}},
	}}, nil
}

func (fakeRuntimeSource) ReadRuntimeSkill(_ context.Context, runtimeID, appID, skillID string) (*SkillDetail, error) {
	if runtimeID != "desktop_a" || appID != "canvas" || skillID != "canvas" {
		return nil, ErrNotFound
	}
	return &SkillDetail{ID: "canvas", Name: "Canvas", Path: "skills/canvas/SKILL.md", Content: "# Canvas"}, nil
}

func (f *fakeAppConfig) ListAppEnablement(context.Context) (map[string]bool, error) {
	if f.enablementErr != nil {
		return nil, f.enablementErr
	}
	out := make(map[string]bool, len(f.enabled))
	for id, enabled := range f.enabled {
		out[id] = enabled
	}
	return out, nil
}

func (f *fakeAppConfig) SetAppEnabled(_ context.Context, id string, enabled bool) error {
	if f.enabled == nil {
		f.enabled = make(map[string]bool)
	}
	f.enabled[id] = enabled
	return nil
}

func TestBuiltinAppsMergeEnablementAndSkills(t *testing.T) {
	config := &fakeAppConfig{}
	svc := NewService(t.TempDir(), config)

	defs, err := svc.ListDefinitions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 7 {
		t.Fatalf("unexpected builtin definitions: %+v", defs)
	}
	browser := definitionByID(defs, BuiltinBrowserID)
	if browser == nil || browser.Source != SourceBuiltin || !browser.Enabled || browser.CanUninstall || browser.RequiredMode != "work" || browser.DefaultSkillID != BuiltinBrowserID {
		t.Fatalf("unexpected browser definition: %+v", browser)
	}
	if len(browser.Tools) != 11 || browser.Tools[0].Name != toolBrowserStatus {
		t.Fatalf("unexpected builtin tools: browser=%+v", browser.Tools)
	}
	for _, tc := range []struct {
		appID, skillID, toolName string
	}{
		{BuiltinSkillAuthoringID, "skill-creator", toolSkillValidate},
		{BuiltinAppAuthoringID, "app-creator", toolAppSave},
	} {
		def := definitionByID(defs, tc.appID)
		if def == nil || def.RequiredMode != "code" || def.DefaultSkillID != tc.skillID || len(def.Tools) != 1 || def.Tools[0].Name != tc.toolName {
			t.Fatalf("unexpected authoring app %s: %+v", tc.appID, def)
		}
		detail, err := svc.ReadSkill(context.Background(), tc.appID, tc.skillID)
		if err != nil || detail.Content == "" {
			t.Fatalf("read authoring skill %s: detail=%+v err=%v", tc.skillID, detail, err)
		}
	}
	for _, tc := range []struct {
		appID, requiredMode string
		toolCount           int
	}{
		{BuiltinProjectFilesID, "code", 9},
		{BuiltinSourceControlID, "code", 6},
		{BuiltinCodeIntelID, "code", 5},
		{BuiltinCaptureID, "chat", 2},
	} {
		def := definitionByID(defs, tc.appID)
		if def == nil || def.RequiredMode != tc.requiredMode || def.DefaultSkillID != "" || len(def.Skills) != 0 || len(def.Tools) != tc.toolCount {
			t.Fatalf("unexpected tool-only builtin app %s: %+v", tc.appID, def)
		}
	}

	updated, err := svc.SetEnabled(context.Background(), BuiltinBrowserID, false)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled || config.enabled[BuiltinBrowserID] {
		t.Fatalf("browser enablement was not updated: %+v", updated)
	}
	if _, err := svc.ReadSkill(context.Background(), BuiltinBrowserID, BuiltinBrowserID); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled builtin skill err = %v", err)
	}
	if _, err := svc.SetEnabled(context.Background(), BuiltinBrowserID, true); err != nil {
		t.Fatal(err)
	}
	detail, err := svc.ReadSkill(context.Background(), BuiltinBrowserID, BuiltinBrowserID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ID != BuiltinBrowserID || detail.Content == "" {
		t.Fatalf("unexpected builtin skill: %+v", detail)
	}
	if err := svc.DeleteDefinition(context.Background(), BuiltinBrowserID); !errors.Is(err, ErrBuiltinApp) {
		t.Fatalf("delete builtin err = %v", err)
	}
}

func TestRuntimeAppIsScopedToOriginRuntime(t *testing.T) {
	config := &fakeAppConfig{}
	svc := NewService(t.TempDir(), config).WithRuntimeSource(fakeRuntimeSource{})

	defs, err := svc.ListDefinitions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 7 {
		t.Fatalf("runtime app leaked without runtime identity: %+v", defs)
	}

	ctx := WithRuntimeID(context.Background(), "desktop_a")
	defs, err = svc.ListDefinitions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	canvas := definitionByID(defs, "canvas")
	if len(defs) != 8 || canvas == nil || canvas.Source != SourceBuiltin || canvas.Runtime != "desktop" || canvas.CanUninstall {
		t.Fatalf("unexpected runtime app definition: %+v", defs)
	}
	if _, err := svc.SetEnabled(ctx, "canvas", false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadSkill(ctx, "canvas", "canvas"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled runtime skill err = %v", err)
	}
	if _, err := svc.SetEnabled(ctx, "canvas", true); err != nil {
		t.Fatal(err)
	}
	detail, err := svc.ReadSkill(ctx, "canvas", "canvas")
	if err != nil || detail.Content != "# Canvas" {
		t.Fatalf("unexpected runtime skill: detail=%+v err=%v", detail, err)
	}
}

func definitionByID(defs []*Definition, id string) *Definition {
	for _, def := range defs {
		if def != nil && def.ID == id {
			return def
		}
	}
	return nil
}

func TestDecorateInstalledDefinitionInfersAPITools(t *testing.T) {
	def := &Definition{Endpoints: map[string]Endpoint{
		"rest":    {Kind: EndpointKindREST},
		"graphql": {Kind: EndpointKindGraphQL},
		"mcp":     {Kind: EndpointKindMCP},
	}}
	decorateInstalledDefinition(def)
	want := []string{toolRESTRequest, toolGraphQLRequest, toolGraphQLIntrospect, toolGraphQLSearch}
	if len(def.Tools) != len(want) {
		t.Fatalf("tools = %+v, want %v", def.Tools, want)
	}
	for i, name := range want {
		if def.Tools[i].Name != name {
			t.Fatalf("tools[%d] = %q, want %q", i, def.Tools[i].Name, name)
		}
	}
}

func TestInstalledAppCanBeTemporarilyDisabled(t *testing.T) {
	config := &fakeAppConfig{}
	svc := NewService(writeTestApp(t), config)

	updated, err := svc.SetEnabled(context.Background(), "github", false)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled || config.enabled["github"] {
		t.Fatalf("installed app enablement was not updated: %+v", updated)
	}
	defs, err := svc.ListDefinitions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range defs {
		if def.ID == "github" && def.Enabled {
			t.Fatalf("installed app was enabled after reload: %+v", def)
		}
	}
}

func TestInstallPackageRejectsReservedAppIDs(t *testing.T) {
	for _, appID := range []string{
		BuiltinBrowserID, BuiltinSkillAuthoringID, BuiltinAppAuthoringID,
		BuiltinProjectFilesID, BuiltinSourceControlID, BuiltinCodeIntelID, BuiltinCaptureID,
		RuntimeCanvasID,
	} {
		t.Run(appID, func(t *testing.T) {
			packageJSON := []byte(`{"kind":"pudding.app.package","schema_version":1,"app":{"id":"` + appID + `"}}`)
			if _, err := InstallPackage(t.TempDir(), packageJSON, "", ""); !errors.Is(err, ErrBuiltinApp) {
				t.Fatalf("install reserved app id err = %v", err)
			}
		})
	}
}

func TestSaveAuthoredPackageEnforcesCreateAndUpdate(t *testing.T) {
	svc := NewService(t.TempDir(), nil)
	pkg := Package{
		Kind:          AppPackageKind,
		SchemaVersion: AppPackageSchemaVersion,
		App:           PackageApp{ID: "example", Version: "0.1.0"},
		Files: []PackageFile{{
			Path:    AppFileName,
			Content: "id: example\nname: Example\nversion: 0.1.0\n",
		}},
	}
	raw := marshalTestAppPackage(t, pkg)
	if _, err := svc.SaveAuthoredPackage(context.Background(), raw, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveAuthoredPackage(context.Background(), raw, false); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second create err = %v", err)
	}
	missing := pkg
	missing.App.ID = "missing"
	missing.Files = []PackageFile{{Path: AppFileName, Content: "id: missing\nname: Missing\nversion: 0.1.0\n"}}
	if _, err := svc.SaveAuthoredPackage(context.Background(), marshalTestAppPackage(t, missing), true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing update err = %v", err)
	}
}

func TestSaveAuthoredPackageRejectsRuntimeAppID(t *testing.T) {
	svc := NewService(t.TempDir(), nil)
	pkg := Package{
		Kind:          AppPackageKind,
		SchemaVersion: AppPackageSchemaVersion,
		App:           PackageApp{ID: RuntimeCanvasID, Version: "0.1.0"},
	}
	if _, err := svc.SaveAuthoredPackage(context.Background(), marshalTestAppPackage(t, pkg), false); !errors.Is(err, ErrBuiltinApp) {
		t.Fatalf("save runtime app id err = %v", err)
	}
	if err := svc.DeleteDefinition(context.Background(), RuntimeCanvasID); !errors.Is(err, ErrBuiltinApp) {
		t.Fatalf("delete runtime app id err = %v", err)
	}
}

func TestSaveAuthoredPackageDoesNotCommitWhenEnablementReadFails(t *testing.T) {
	homeDir := t.TempDir()
	svc := NewService(homeDir, &fakeAppConfig{enablementErr: errors.New("enablement unavailable")})
	pkg := Package{
		Kind:          AppPackageKind,
		SchemaVersion: AppPackageSchemaVersion,
		App:           PackageApp{ID: "example", Version: "0.1.0"},
		Files: []PackageFile{{
			Path:    AppFileName,
			Content: "id: example\nname: Example\nversion: 0.1.0\n",
		}},
	}
	if _, err := svc.SaveAuthoredPackage(context.Background(), marshalTestAppPackage(t, pkg), false); err == nil || err.Error() != "enablement unavailable" {
		t.Fatalf("save err = %v, want enablement unavailable", err)
	}
	if _, err := os.Stat(filepath.Join(home.AppsPath(homeDir), "example")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("App was committed after enablement failure: %v", err)
	}
}

func TestResolveEndpointUsesOnlyConfiguredConnection(t *testing.T) {
	homeDir := writeTestApp(t)
	svc := NewService(homeDir, fakeConnectionStore{items: map[string]*Connection{
		"github-main": {
			ID:           "github-main",
			Name:         "GitHub",
			AppID:        "github",
			Auth:         Auth{Type: "bearer", Token: "secret"},
			Fields:       map[string]string{"hotelCode": "H001"},
			EndpointURLs: map[string]string{"github_rest": "https://github.example.com/api/v3"},
		},
	}})

	binding, err := svc.ResolveEndpoint(context.Background(), "session-1", "github_rest", "")
	if err != nil {
		t.Fatal(err)
	}
	if binding.ConnectionID != "github-main" || binding.Auth.Token != "secret" {
		t.Fatalf("unexpected binding: %+v", binding)
	}
	if binding.ConnectionFields["hotelCode"] != "H001" || len(binding.ConnectionFieldDefs) != 1 {
		t.Fatalf("connection fields not resolved: %+v", binding)
	}
	if binding.Endpoint.URL != "https://github.example.com/api/v3" {
		t.Fatalf("connection endpoint URL not resolved: %+v", binding.Endpoint)
	}
}

func TestResolveEndpointRequiresConnectionWhenMultipleConfigured(t *testing.T) {
	homeDir := writeTestApp(t)
	svc := NewService(homeDir, fakeConnectionStore{items: map[string]*Connection{
		"github-work":     {ID: "github-work", Name: "Work", AppID: "github"},
		"github-personal": {ID: "github-personal", Name: "Personal", AppID: "github"},
	}})

	_, err := svc.ResolveEndpoint(context.Background(), "session-1", "github_rest", "")
	var resolveErr *EndpointResolveError
	if !errors.As(err, &resolveErr) || resolveErr.Reason != "connection_required" || len(resolveErr.Connections) != 2 {
		t.Fatalf("expected connection choice error, got %#v", err)
	}

	binding, err := svc.ResolveEndpoint(context.Background(), "session-1", "github_rest", "Personal")
	if err != nil {
		t.Fatal(err)
	}
	if binding.ConnectionID != "github-personal" {
		t.Fatalf("unexpected binding: %+v", binding)
	}
}

func TestListEndpointBindingsFiltersKind(t *testing.T) {
	homeDir := writeTestApp(t)
	svc := NewService(homeDir, fakeConnectionStore{items: map[string]*Connection{
		"github-main": {ID: "github-main", Name: "GitHub", AppID: "github", Auth: Auth{Type: "bearer", Token: "secret"}},
	}})

	bindings, err := svc.ListEndpointBindings(context.Background(), EndpointKindREST)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].EndpointName != "github_rest" || bindings[0].ConnectionID != "github-main" {
		t.Fatalf("unexpected bindings: %+v", bindings)
	}

	bindings, err = svc.ListEndpointBindings(context.Background(), EndpointKindMCP)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 0 {
		t.Fatalf("expected no mcp bindings, got %+v", bindings)
	}
}

func TestResolveEndpointUsesConnectionlessAppWhenAuthNotRequired(t *testing.T) {
	homeDir := writeConnectionlessTestApp(t)
	svc := NewService(homeDir, nil)

	binding, err := svc.ResolveEndpoint(context.Background(), "session-1", "local_mcp", "")
	if err != nil {
		t.Fatal(err)
	}
	if binding.AppID != "sequential-thinking" || binding.ConnectionID != "" || binding.Auth.Type != "" {
		t.Fatalf("unexpected connectionless binding: %+v", binding)
	}
	if binding.Endpoint.Kind != EndpointKindMCP || binding.Endpoint.Transport != EndpointTransportStdio {
		t.Fatalf("unexpected endpoint: %+v", binding.Endpoint)
	}
	if binding.Endpoint.Command != "platform-command" || len(binding.Endpoint.Args) != 2 || binding.Endpoint.Args[0] != "platform-arg" || binding.Endpoint.Env["PLATFORM_ENV"] != runtime.GOOS {
		t.Fatalf("platform endpoint override not applied: %+v", binding.Endpoint)
	}
}

func TestListEndpointBindingsIncludesConnectionlessAppWhenAuthNotRequired(t *testing.T) {
	homeDir := writeConnectionlessTestApp(t)
	svc := NewService(homeDir, nil)

	bindings, err := svc.ListEndpointBindings(context.Background(), EndpointKindMCP)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].AppID != "sequential-thinking" || bindings[0].ConnectionID != "" || bindings[0].EndpointName != "local_mcp" {
		t.Fatalf("unexpected bindings: %+v", bindings)
	}
}

func TestListDefinitionsAppliesMCPOverride(t *testing.T) {
	homeDir := writeConnectionlessTestApp(t)
	writeMCPOverride(t, homeDir, "sequential-thinking", `
mcp:
  local_mcp:
    command: docker
    args: ["run", "--rm", "-i", "mcp/sequential-thinking"]
    env:
      BASE_ENV: custom
      CUSTOM_ENV: "1"
`)
	svc := NewService(homeDir, nil)

	binding, err := svc.ResolveEndpoint(context.Background(), "session-1", "local_mcp", "")
	if err != nil {
		t.Fatal(err)
	}
	if binding.Endpoint.Command != "docker" || len(binding.Endpoint.Args) != 4 || binding.Endpoint.Args[0] != "run" {
		t.Fatalf("mcp override not applied: %+v", binding.Endpoint)
	}
	if binding.Endpoint.Env["BASE_ENV"] != "custom" || binding.Endpoint.Env["PLATFORM_ENV"] != runtime.GOOS || binding.Endpoint.Env["CUSTOM_ENV"] != "1" {
		t.Fatalf("mcp override env not merged: %+v", binding.Endpoint.Env)
	}
}

func TestListDefinitionsRejectsMCPOverrideForNonMCPEndpoint(t *testing.T) {
	homeDir := writeTestApp(t)
	writeMCPOverride(t, homeDir, "github", `
mcp:
  github_rest:
    command: docker
`)
	svc := NewService(homeDir, nil)

	if _, err := svc.ListDefinitions(context.Background()); err == nil {
		t.Fatal("expected mcp override for non-mcp endpoint to fail")
	}
}

func TestPutGetDeleteMCPOverride(t *testing.T) {
	homeDir := writeConnectionlessTestApp(t)
	svc := NewService(homeDir, nil)

	_, configured, err := svc.GetMCPOverride(context.Background(), "sequential-thinking", "local_mcp")
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		t.Fatal("expected no initial mcp override")
	}

	override, err := svc.PutMCPOverride(context.Background(), "sequential-thinking", "local_mcp", MCPEndpointOverride{
		Command: "docker",
		Args:    stringSlicePtr("run", "--rm", "-i", "mcp/sequential-thinking"),
		Env:     map[string]string{"BASE_ENV": "custom"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if override.Command != "docker" {
		t.Fatalf("unexpected override: %+v", override)
	}
	got, configured, err := svc.GetMCPOverride(context.Background(), "sequential-thinking", "local_mcp")
	if err != nil {
		t.Fatal(err)
	}
	if !configured || got.Command != "docker" || got.Env["BASE_ENV"] != "custom" {
		t.Fatalf("unexpected stored override configured=%v override=%+v", configured, got)
	}

	if err := svc.DeleteMCPOverride(context.Background(), "sequential-thinking", "local_mcp"); err != nil {
		t.Fatal(err)
	}
	_, configured, err = svc.GetMCPOverride(context.Background(), "sequential-thinking", "local_mcp")
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		t.Fatal("expected mcp override to be deleted")
	}
}

func TestMCPOverrideRejectsSymlinkWithoutTouchingTarget(t *testing.T) {
	homeDir := writeConnectionlessTestApp(t)
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	original := []byte("outside content")
	if err := os.WriteFile(outside, original, 0o600); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(home.AppsPath(homeDir), "sequential-thinking", MCPOverrideFileName)
	if err := os.Symlink(outside, overridePath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	svc := NewService(homeDir, nil)

	if _, _, err := svc.GetMCPOverride(context.Background(), "sequential-thinking", "local_mcp"); err == nil {
		t.Fatal("expected symlinked mcp override read to fail")
	}
	if _, err := svc.PutMCPOverride(context.Background(), "sequential-thinking", "local_mcp", MCPEndpointOverride{Command: "docker"}); err == nil {
		t.Fatal("expected symlinked mcp override write to fail")
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("outside target changed: %q", data)
	}
}

func TestReadSkillUsesSkillID(t *testing.T) {
	homeDir := writeTestAppWithSkill(t)
	svc := NewService(homeDir, nil)

	detail, err := svc.ReadSkill(context.Background(), "github", "github-issues")
	if err != nil {
		t.Fatal(err)
	}
	if detail.ID != "github-issues" || detail.Path != "skills/issues/SKILL.md" || detail.Content != "---\nname: github-issues\ndescription: Read issues.\n---\n\n# GitHub Issues\n" {
		t.Fatalf("unexpected skill detail: %+v", detail)
	}

	detail, err = svc.ReadSkill(context.Background(), "github", "skills/issues/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if detail.ID != "github-issues" {
		t.Fatalf("expected path fallback to read same skill, got %+v", detail)
	}
}

func writeTestApp(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	appDir := filepath.Join(home.AppsPath(homeDir), "github")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, AppFileName), []byte(`
id: github
name: GitHub
connection:
  fields:
    - id: hotelCode
      label: Hotel code
      required: true
      inject:
        - target: query
          methods: [GET, DELETE]
endpoints:
  github_rest:
    kind: rest
    url: https://api.github.com
    url_config:
      label: GitHub address
`), 0o600); err != nil {
		t.Fatal(err)
	}
	return homeDir
}

func writeConnectionlessTestApp(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	appDir := filepath.Join(home.AppsPath(homeDir), "sequential-thinking")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, AppFileName), []byte(`
id: sequential-thinking
name: Sequential Thinking
auth:
  required: false
endpoints:
  local_mcp:
    kind: mcp
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-sequential-thinking"]
    env:
      BASE_ENV: base
    platforms:
      `+runtime.GOOS+`:
        command: platform-command
        args: ["platform-arg", "@modelcontextprotocol/server-sequential-thinking"]
        env:
          PLATFORM_ENV: `+runtime.GOOS+`
`), 0o600); err != nil {
		t.Fatal(err)
	}
	return homeDir
}

func writeMCPOverride(t *testing.T, homeDir, appID, content string) {
	t.Helper()
	dir := filepath.Join(home.AppsPath(homeDir), appID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, MCPOverrideFileName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func stringSlicePtr(values ...string) *[]string {
	out := append([]string(nil), values...)
	return &out
}

func writeTestAppWithSkill(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	appDir := filepath.Join(home.AppsPath(homeDir), "github")
	skillDir := filepath.Join(appDir, "skills", "issues")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, AppFileName), []byte(`
id: github
name: GitHub
endpoints:
  github_rest:
    kind: rest
    url: https://api.github.com
skills:
  - skills/issues/SKILL.md
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: github-issues\ndescription: Read issues.\n---\n\n# GitHub Issues\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return homeDir
}
