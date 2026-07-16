package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/home"
)

func TestImportAndUpdateMCPApps(t *testing.T) {
	homeDir := t.TempDir()
	svc := NewService(homeDir, nil)
	config := `{
  "mcpServers": {
    "Local tools": {
      "command": "node",
      "args": ["server.js"],
      "env": {"API_TOKEN": "secret"}
    },
    "Remote tools": {
      "url": "https://example.test/mcp",
      "headers": {"Authorization": "Bearer secret"}
    }
  }
}`
	defs, err := svc.ImportMCPApps(context.Background(), []byte(config), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 2 {
		t.Fatalf("definitions = %d, want 2", len(defs))
	}
	byName := map[string]*Definition{}
	for _, def := range defs {
		byName[def.Name] = def
	}
	local := byName["Local tools"]
	remote := byName["Remote tools"]
	if local == nil || local.Kind != KindMCP || local.RequiredMode != "code" {
		t.Fatalf("unexpected local MCP App: %+v", local)
	}
	if remote == nil || remote.Kind != KindMCP || remote.RequiredMode != "work" {
		t.Fatalf("unexpected remote MCP App: %+v", remote)
	}
	bindings, err := svc.ListEndpointBindings(context.Background(), EndpointKindMCP)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 2 {
		t.Fatalf("MCP App bindings = %d, want 2", len(bindings))
	}
	for _, binding := range bindings {
		if binding.ConnectionID != "" {
			t.Fatalf("MCP App unexpectedly requires a connection: %+v", binding)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(home.AppsPath(homeDir), local.ID, AppFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "secret") || strings.Contains(string(manifest), "API_TOKEN") {
		t.Fatalf("secret data leaked into app manifest: %s", manifest)
	}
	exported, err := svc.GetMCPAppConfig(context.Background(), local.ID)
	if err != nil {
		t.Fatal(err)
	}
	var decoded mcpServersConfig
	if err := json.Unmarshal(exported, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := decoded.MCPServers["Local tools"].Env["API_TOKEN"]; got != "secret" {
		t.Fatalf("exported API_TOKEN = %q", got)
	}

	updated, err := svc.UpdateMCPApp(context.Background(), local.ID, []byte(`{
  "mcpServers": {
    "Local tools": {"url": "https://example.test/new-mcp"}
  }
}`), "Renamed tools")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := updated.Endpoints[mcpAppEndpointName]
	if updated.ID != local.ID || updated.Name != "Renamed tools" || updated.RequiredMode != "work" || endpoint.Transport != EndpointTransportStreamableHTTP {
		t.Fatalf("unexpected updated MCP App: %+v", updated)
	}
	if len(endpoint.Env) != 0 {
		t.Fatalf("old secret env survived transport update: %+v", endpoint.Env)
	}
}

func TestMCPAppConfigValidation(t *testing.T) {
	svc := NewService(t.TempDir(), nil)
	for _, raw := range []string{
		`{}`,
		`{"mcpServers": {}}`,
		`{"mcpServers": {"bad": {"command": "node", "url": "https://example.test/mcp"}}}`,
		`{"mcpServers": {"bad": {"url": "https://example.test/mcp", "env": {"TOKEN": "x"}}}}`,
		`{"mcpServers": {"same": {"command": "node"}, " SAME ": {"command": "node"}}}`,
	} {
		if _, err := svc.ImportMCPApps(context.Background(), []byte(raw), ""); err == nil {
			t.Fatalf("config unexpectedly accepted: %s", raw)
		}
	}
	if _, err := svc.ImportMCPApps(context.Background(), []byte(`{"mcpServers":{"one":{"command":"node"},"two":{"command":"node"}}}`), "Custom"); err == nil {
		t.Fatal("custom name unexpectedly accepted for multiple servers")
	}
}

func TestImportMCPAppCustomName(t *testing.T) {
	svc := NewService(t.TempDir(), nil)
	defs, err := svc.ImportMCPApps(context.Background(), []byte(`{"mcpServers":{"filesystem":{"command":"node"}}}`), "Local Files")
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Name != "Local Files" || defs[0].ID != "mcp-local-files" {
		t.Fatalf("unexpected custom-named MCP App: %+v", defs)
	}
}

func TestDefinitionKindDefaultsAndMCPConstraints(t *testing.T) {
	ordinary := &Definition{ID: "ordinary", Name: "Ordinary"}
	if err := ValidateDefinition(ordinary); err != nil {
		t.Fatal(err)
	}
	if ordinary.Kind != KindApp {
		t.Fatalf("default kind = %q", ordinary.Kind)
	}
	invalid := &Definition{
		Kind: KindMCP,
		ID:   "invalid-mcp",
		Name: "Invalid MCP",
		Endpoints: map[string]Endpoint{
			"api": {Kind: EndpointKindREST, URL: "https://example.test"},
		},
	}
	if err := ValidateDefinition(invalid); err == nil {
		t.Fatal("non-MCP endpoint accepted by MCP App")
	}
}
