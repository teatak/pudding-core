package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

func TestResolveEndpointUsesOnlyConfiguredConnection(t *testing.T) {
	homeDir := writeTestApp(t)
	svc := NewService(homeDir, fakeConnectionStore{items: map[string]*Connection{
		"github-main": {
			ID:     "github-main",
			Name:   "GitHub",
			AppID:  "github",
			Auth:   Auth{Type: "bearer", Token: "secret"},
			Fields: map[string]string{"hotelCode": "H001"},
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
`), 0o600); err != nil {
		t.Fatal(err)
	}
	return homeDir
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
