package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUserDefinitions(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "github")
	if err := os.MkdirAll(filepath.Join(appDir, "skills", "issues"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, AppFileName), []byte(`
id: github
name: GitHub
description: GitHub API access
connection:
  fields:
    - id: hotelCode
      label: Hotel code
      required: true
      inject:
        - target: query
          methods: [GET, DELETE]
        - target: body
          methods: [POST, PUT, PATCH]
    - id: apiKey
      label: API key
      required: true
      secret: true
      inject:
        - target: env
          name: GITHUB_API_KEY
endpoints:
  github_rest:
    kind: rest
    url: https://api.github.com
  github_mcp:
    kind: mcp
    transport: stdio
    command: npx
    args: ["-y", "@example/github-mcp"]
    env:
      BASE_ENV: base
    platforms:
      windows:
        command: cmd
        args: ["/c", "npx", "-y", "@example/github-mcp"]
        env:
          BASE_ENV: windows
          WINDOWS_ENV: "1"
skills:
  - skills/issues/SKILL.md
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "skills", "issues", "SKILL.md"), []byte(`---
name: github-issues
description: Read GitHub issues.
---

Use builtin_rest_request with github_rest.
`), 0o600); err != nil {
		t.Fatal(err)
	}

	defs, err := LoadUserDefinitions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected one app, got %+v", defs)
	}
	def := defs[0]
	if def.ID != "github" || def.Endpoints["github_rest"].Kind != EndpointKindREST {
		t.Fatalf("unexpected definition: %+v", def)
	}
	win := ResolveEndpointPlatformForGOOS(def.Endpoints["github_mcp"], "windows")
	if win.Command != "cmd" || len(win.Args) != 4 || win.Args[0] != "/c" || win.Env["BASE_ENV"] != "windows" || win.Env["WINDOWS_ENV"] != "1" {
		t.Fatalf("windows platform override not applied: %+v", win)
	}
	if def.Connection == nil || len(def.Connection.Fields) != 2 || def.Connection.Fields[1].ID != "apiKey" {
		t.Fatalf("connection fields not loaded: %+v", def.Connection)
	}
	if len(def.Skills) != 1 || def.Skills[0].ID != "github-issues" || def.Skills[0].Description == "" {
		t.Fatalf("skill frontmatter not loaded: %+v", def.Skills)
	}
}

func TestInstallPackageDefinition(t *testing.T) {
	root := t.TempDir()
	pkg := Package{
		Kind:          AppPackageKind,
		SchemaVersion: AppPackageSchemaVersion,
		App: PackageApp{
			ID:      "github",
			Name:    "GitHub",
			Version: "1.0.0",
		},
		Files: []PackageFile{
			{
				Path: AppFileName,
				Content: `
id: github
name: GitHub
version: 1.0.0
description: GitHub API access
icon:
  svg: assets/icon.svg
  color:
    light: "#111111"
    dark: "#ffffff"
  background:
    light: "#ffffff"
    dark: "#111111"
endpoints:
  github_rest:
    kind: rest
    url: https://api.github.com
skills:
  - skills/issues/SKILL.md
`,
			},
			{
				Path: "skills/issues/SKILL.md",
				Content: `---
name: github-issues
description: Read GitHub issues.
---

Use builtin_rest_request with github_rest.
`,
			},
			{
				Path:    "assets/icon.svg",
				Content: `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
			},
		},
	}
	data, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	installed, err := InstallPackage(root, data, sha256Bytes(data), "https://example.test/apps/registry.json")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Path != filepath.Join(root, "github", AppFileName) {
		t.Fatalf("unexpected installed path: %s", installed.Path)
	}
	if installed.Version != "1.0.0" || installed.Icon == nil || installed.Icon.SVG != "assets/icon.svg" {
		t.Fatalf("package metadata not loaded: %+v", installed)
	}
	if installed.Icon.Color == nil || installed.Icon.Color.Light != "#111111" || installed.Icon.Color.Dark != "#ffffff" {
		t.Fatalf("icon color metadata not loaded: %+v", installed.Icon)
	}
	if installed.Icon.Background == nil || installed.Icon.Background.Light != "#ffffff" || installed.Icon.Background.Dark != "#111111" {
		t.Fatalf("icon background metadata not loaded: %+v", installed.Icon)
	}
	if installed.PackageSHA256 == "" || installed.SourceURL == "" {
		t.Fatalf("package lock metadata not loaded: %+v", installed)
	}
	if _, err := os.Stat(filepath.Join(root, "github", "skills", "issues", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestInstallPackageUpdatePreservesPreviousVersionOnValidationFailure(t *testing.T) {
	root := t.TempDir()
	initial := Package{
		Kind:          AppPackageKind,
		SchemaVersion: AppPackageSchemaVersion,
		App:           PackageApp{ID: "example", Version: "1.0.0"},
		Files: []PackageFile{{
			Path:    AppFileName,
			Content: "id: example\nname: Original\nversion: 1.0.0\nendpoints:\n  example_rest:\n    kind: rest\n    url: https://example.test\n",
		}},
	}
	if _, err := InstallPackage(root, marshalTestAppPackage(t, initial), "", ""); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "example", AppFileName)
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	invalid := Package{
		Kind:          AppPackageKind,
		SchemaVersion: AppPackageSchemaVersion,
		App:           PackageApp{ID: "example", Version: "2.0.0"},
		Files: []PackageFile{{
			Path:    AppFileName,
			Content: "id: wrong-id\nname: Broken\nversion: 2.0.0\n",
		}},
	}
	if _, err := InstallPackage(root, marshalTestAppPackage(t, invalid), "", ""); err == nil {
		t.Fatal("invalid update should fail")
	}
	after, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("failed update changed installed App: %q", after)
	}
}

func TestInstallPackageUpdatePreservesUnmanagedFilesAndRemovesOldManagedFiles(t *testing.T) {
	root := t.TempDir()
	initial := Package{
		Kind:          AppPackageKind,
		SchemaVersion: AppPackageSchemaVersion,
		App:           PackageApp{ID: "example", Version: "1.0.0"},
		Files: []PackageFile{
			{Path: AppFileName, Content: "id: example\nname: Example\nversion: 1.0.0\nskills:\n  - skills/old/SKILL.md\n"},
			{Path: "skills/old/SKILL.md", Content: "---\nname: old\ndescription: Old App instructions.\n---\n\nOld.\n"},
		},
	}
	if _, err := InstallPackage(root, marshalTestAppPackage(t, initial), "", ""); err != nil {
		t.Fatal(err)
	}
	unmanagedPath := filepath.Join(root, "example", "local-notes.txt")
	if err := os.WriteFile(unmanagedPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := Package{
		Kind:          AppPackageKind,
		SchemaVersion: AppPackageSchemaVersion,
		App:           PackageApp{ID: "example", Version: "2.0.0"},
		Files: []PackageFile{
			{Path: AppFileName, Content: "id: example\nname: Example\nversion: 2.0.0\nskills:\n  - skills/new/SKILL.md\n"},
			{Path: "skills/new/SKILL.md", Content: "---\nname: new\ndescription: New App instructions.\n---\n\nNew.\n"},
		},
	}
	definition, err := InstallPackage(root, marshalTestAppPackage(t, updated), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if definition.Version != "2.0.0" {
		t.Fatalf("version = %q", definition.Version)
	}
	if _, err := os.Stat(filepath.Join(root, "example", "skills", "old", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("old managed file should be removed, err=%v", err)
	}
	if data, err := os.ReadFile(unmanagedPath); err != nil || string(data) != "keep" {
		t.Fatalf("unmanaged file was not preserved: %q %v", data, err)
	}
}

func TestLoadDefinitionRequiresReferencedIconFile(t *testing.T) {
	appDir := filepath.Join(t.TempDir(), "example")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, AppFileName), []byte("id: example\nname: Example\nicon:\n  svg: assets/icon.svg\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDefinitionDir(appDir); err == nil {
		t.Fatal("expected missing referenced icon to fail validation")
	}
}

func TestLoadDefinitionRejectsSkillSymlinkOutsideApp(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "example")
	skillDir := filepath.Join(appDir, "skills", "example")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside-skill.md")
	if err := os.WriteFile(outside, []byte("---\nname: outside\ndescription: Outside.\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, AppFileName), []byte("id: example\nname: Example\nskills:\n  - skills/example/SKILL.md\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDefinitionDir(appDir); err == nil {
		t.Fatal("expected skill symlink outside App to be rejected")
	}
}

func TestAppRootSymlinkIsRejected(t *testing.T) {
	parent := t.TempDir()
	outside := t.TempDir()
	root := filepath.Join(parent, "apps")
	if err := os.Symlink(outside, root); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := LoadUserDefinitions(root); err == nil {
		t.Fatal("symlinked App root should not be loaded")
	}
	pkg := Package{
		Kind:          AppPackageKind,
		SchemaVersion: AppPackageSchemaVersion,
		App:           PackageApp{ID: "example", Version: "1.0.0"},
		Files: []PackageFile{{
			Path:    AppFileName,
			Content: "id: example\nname: Example\nversion: 1.0.0\n",
		}},
	}
	if _, err := InstallPackage(root, marshalTestAppPackage(t, pkg), "", ""); err == nil {
		t.Fatal("package install through symlinked App root should fail")
	}
	if _, err := os.Stat(filepath.Join(outside, "example")); !os.IsNotExist(err) {
		t.Fatalf("package escaped App root: %v", err)
	}
}

func marshalTestAppPackage(t *testing.T, pkg Package) []byte {
	t.Helper()
	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLoadMCPAppEndpoints(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "mcpapp")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, AppFileName), []byte(`
id: mcpapp
name: MCP App
endpoints:
  remote_mcp:
    kind: mcp
    transport: streamable_http
    url: https://example.test/mcp
  local_mcp:
    kind: mcp
    transport: stdio
    command: npx
    args: ["-y", "example-mcp"]
    env:
      EXAMPLE_MODE: test
`), 0o600); err != nil {
		t.Fatal(err)
	}
	defs, err := LoadUserDefinitions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected one app, got %+v", defs)
	}
	remote := defs[0].Endpoints["remote_mcp"]
	if remote.Kind != EndpointKindMCP || remote.Transport != EndpointTransportStreamableHTTP || remote.URL != "https://example.test/mcp" {
		t.Fatalf("remote mcp endpoint not loaded: %+v", remote)
	}
	local := defs[0].Endpoints["local_mcp"]
	if local.Kind != EndpointKindMCP || local.Transport != EndpointTransportStdio || local.Command != "npx" || len(local.Args) != 2 || local.Env["EXAMPLE_MODE"] != "test" {
		t.Fatalf("local mcp endpoint not loaded: %+v", local)
	}
}

func TestEndpointEnvironmentValidationMatchesProcessEnvironment(t *testing.T) {
	for _, invalid := range []string{"1TOKEN", "BAD-NAME", "BAD.NAME", "BAD=NAME"} {
		if validEndpointEnvName(invalid) {
			t.Fatalf("environment name %q should be invalid", invalid)
		}
	}
	for _, valid := range []string{"TOKEN", "_TOKEN", "TOKEN_2"} {
		if !validEndpointEnvName(valid) {
			t.Fatalf("environment name %q should be valid", valid)
		}
	}
	if err := ValidateConnectionConfig(&ConnectionConfig{Fields: []ConnectionField{{
		ID: "token",
		Inject: []ConnectionFieldInject{{
			Target: "env",
			Name:   "BAD-NAME",
		}},
	}}}); err == nil {
		t.Fatal("connection env injection with an invalid name should fail")
	}
	if err := validateMCPEndpoint(Endpoint{
		Kind:      EndpointKindMCP,
		Transport: EndpointTransportStdio,
		Command:   "example",
		Env:       map[string]string{"VALID_NAME": "bad\x00value"},
	}); err == nil {
		t.Fatal("endpoint environment value containing NUL should fail")
	}
	if err := validateMCPEndpoint(Endpoint{
		Kind:      EndpointKindMCP,
		Transport: EndpointTransportStreamableHTTP,
		URL:       "https://example.test/mcp",
		Headers:   map[string]string{"X-Token": "bad\r\nvalue"},
	}); err == nil {
		t.Fatal("endpoint header value containing a newline should fail")
	}
}

func TestRequestHeaderValidationMatchesRuntime(t *testing.T) {
	for _, invalid := range []string{"Host", "Content-Length", "Bad Header", "X-测试"} {
		if IsAllowedRequestHeaderName(invalid) {
			t.Fatalf("request header %q should be rejected", invalid)
		}
	}
	for _, valid := range []string{"Authorization", "X-API-Key", "X_Custom"} {
		if !IsAllowedRequestHeaderName(valid) {
			t.Fatalf("request header %q should be allowed", valid)
		}
	}
	for _, invalid := range []string{"bad\rvalue", "bad\nvalue", "bad\x00value"} {
		if IsAllowedRequestHeaderValue(invalid) {
			t.Fatalf("request header value %q should be rejected", invalid)
		}
	}
	if !IsAllowedRequestHeaderValue("Bearer valid-token") {
		t.Fatal("ordinary request header value should be allowed")
	}
	if err := ValidateAuthConfig(&AuthConfig{Methods: []AuthMethod{{
		ID:     "custom",
		Type:   AuthTypeHeader,
		Header: "Host",
	}}}); err == nil {
		t.Fatal("auth using a forbidden header should fail")
	}
	if err := ValidateConnectionConfig(&ConnectionConfig{Fields: []ConnectionField{{
		ID: "token",
		Inject: []ConnectionFieldInject{{
			Target: "header",
			Name:   "Content-Length",
		}},
	}}}); err == nil {
		t.Fatal("connection injection using a forbidden header should fail")
	}
	if err := validateMCPEndpoint(Endpoint{
		Kind:      EndpointKindMCP,
		Transport: EndpointTransportStreamableHTTP,
		URL:       "https://example.test/mcp",
		Headers:   map[string]string{"Host": "example.test"},
	}); err == nil {
		t.Fatal("endpoint using a forbidden header should fail")
	}
	if err := ValidateAuthConfig(&AuthConfig{Methods: []AuthMethod{{
		ID:     "token",
		Type:   AuthTypeToken,
		Prefix: "bad\r\nprefix",
	}}}); err == nil {
		t.Fatal("auth using an invalid token prefix should fail")
	}
}

func TestInstallPackageRejectsResourceAmplification(t *testing.T) {
	if _, err := InstallPackage(t.TempDir(), make([]byte, MaxPackageJSONBytes+1), "", ""); !errors.Is(err, ErrPackageTooLarge) {
		t.Fatalf("oversized package error = %v", err)
	}
	files := make([]PackageFile, MaxPackageFiles+1)
	for index := range files {
		files[index] = PackageFile{Path: fmt.Sprintf("files/%d", index)}
	}
	if _, err := packageFiles(files); err == nil {
		t.Fatal("package with too many files should fail")
	}
	if _, err := packageFiles([]PackageFile{{Path: "ambiguous", Content: "text", ContentBase64: "dGV4dA=="}}); err == nil {
		t.Fatal("package file with two content encodings should fail")
	}
}
