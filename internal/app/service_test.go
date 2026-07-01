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

type fakeGrantStore struct{}

func (fakeGrantStore) ListSessionAppGrants(context.Context, string) ([]*store.SessionAppGrant, error) {
	return nil, nil
}

func TestResolveEndpointUsesOnlyConfiguredConnection(t *testing.T) {
	homeDir := writeTestApp(t)
	svc := NewService(homeDir, fakeGrantStore{}, fakeConnectionStore{items: map[string]*Connection{
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
	svc := NewService(homeDir, fakeGrantStore{}, fakeConnectionStore{items: map[string]*Connection{
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
