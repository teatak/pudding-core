package app

import (
	"encoding/json"
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
endpoints:
  github_rest:
    kind: rest
    url: https://api.github.com
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
	if def.Connection == nil || len(def.Connection.Fields) != 1 || def.Connection.Fields[0].ID != "hotelCode" {
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
