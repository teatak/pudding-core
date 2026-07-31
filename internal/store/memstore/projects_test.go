package memstore

import (
	"context"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestProjectsIncludeLatestSessionActivity(t *testing.T) {
	mem := New()
	ctx := context.Background()
	project := &store.Project{ID: "project", Name: "Project"}
	if err := mem.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	session := &store.Session{
		ID:        "session",
		Provider:  "mock",
		Model:     "mock",
		ProjectID: project.ID,
	}
	if err := mem.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}

	projects, err := mem.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].LastActivityAt == nil {
		t.Fatalf("project activity missing: %+v", projects)
	}
	if !projects[0].LastActivityAt.Equal(session.LastActivityAt) {
		t.Fatalf("last activity = %v, want %v", projects[0].LastActivityAt, session.LastActivityAt)
	}
}
