package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/store"
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

func (f fakeConnectionStore) GetAppConnection(_ context.Context, id string) (*Connection, error) {
	item := f.items[id]
	if item == nil {
		return nil, store.ErrNotFound
	}
	return CloneConnection(item), nil
}

type fakeGrantStore struct {
	grants []*store.SessionAppGrant
	err    error
}

func (f fakeGrantStore) ListSessionAppGrants(context.Context, string) ([]*store.SessionAppGrant, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]*store.SessionAppGrant, 0, len(f.grants))
	for _, grant := range f.grants {
		cp := *grant
		cp.AllowedEndpoints = append([]string(nil), grant.AllowedEndpoints...)
		out = append(out, &cp)
	}
	return out, nil
}

func TestResolveEndpointUsesOnlyConfiguredConnection(t *testing.T) {
	homeDir := writeTestApp(t)
	svc := NewService(homeDir, nil, fakeConnectionStore{items: map[string]*Connection{
		"github-main": {ID: "github-main", Name: "GitHub", AppID: "github", Auth: Auth{Type: "bearer", Token: "secret"}},
	}})

	binding, err := svc.ResolveEndpoint(context.Background(), "session-1", "github_rest", "")
	if err != nil {
		t.Fatal(err)
	}
	if binding.ConnectionID != "github-main" || binding.Auth.Token != "secret" {
		t.Fatalf("unexpected binding: %+v", binding)
	}
}

func TestResolveEndpointRequiresConnectionWhenMultipleConfigured(t *testing.T) {
	homeDir := writeTestApp(t)
	svc := NewService(homeDir, nil, fakeConnectionStore{items: map[string]*Connection{
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

func TestResolveEndpointRequiresSessionGrantWhenGrantSourcePresent(t *testing.T) {
	homeDir := writeTestApp(t)
	svc := NewService(homeDir, fakeGrantStore{}, fakeConnectionStore{items: map[string]*Connection{
		"github-main": {ID: "github-main", Name: "GitHub", AppID: "github", Auth: Auth{Type: "bearer", Token: "secret"}},
	}})

	_, err := svc.ResolveEndpoint(context.Background(), "session-1", "github_rest", "")
	var resolveErr *EndpointResolveError
	if !errors.As(err, &resolveErr) || resolveErr.Reason != "endpoint_not_granted" {
		t.Fatalf("expected endpoint_not_granted, got %#v", err)
	}
}

func TestResolveEndpointUsesSessionGrants(t *testing.T) {
	homeDir := writeTestApp(t)
	grants := fakeGrantStore{grants: []*store.SessionAppGrant{
		{SessionID: "session-1", AppID: "github", ConnectionID: "github-work", AllowedEndpoints: []string{"github_rest"}},
		{SessionID: "session-1", AppID: "github", ConnectionID: "github-personal", AllowedEndpoints: []string{"github_rest"}},
	}}
	svc := NewService(homeDir, grants, fakeConnectionStore{items: map[string]*Connection{
		"github-work":     {ID: "github-work", Name: "Work", AppID: "github"},
		"github-personal": {ID: "github-personal", Name: "Personal", AppID: "github"},
		"github-other":    {ID: "github-other", Name: "Other", AppID: "github"},
	}})

	_, err := svc.ResolveEndpoint(context.Background(), "session-1", "github_rest", "")
	var resolveErr *EndpointResolveError
	if !errors.As(err, &resolveErr) || resolveErr.Reason != "connection_required" || len(resolveErr.Connections) != 2 {
		t.Fatalf("expected granted connection choices, got %#v", err)
	}

	binding, err := svc.ResolveEndpoint(context.Background(), "session-1", "github_rest", "Personal")
	if err != nil {
		t.Fatal(err)
	}
	if binding.ConnectionID != "github-personal" {
		t.Fatalf("unexpected binding: %+v", binding)
	}

	_, err = svc.ResolveEndpoint(context.Background(), "session-1", "github_rest", "Other")
	if !errors.As(err, &resolveErr) || resolveErr.Reason != "connection_not_found" {
		t.Fatalf("expected ungranted connection not found, got %#v", err)
	}
}

func TestReadSkillUsesSkillID(t *testing.T) {
	homeDir := writeTestAppWithSkill(t)
	svc := NewService(homeDir, nil, nil)

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
endpoints:
  github_rest:
    kind: rest
    url: https://api.github.com
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
