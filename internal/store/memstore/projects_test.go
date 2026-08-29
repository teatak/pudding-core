package memstore

import (
	"context"
	"errors"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestMergeProjectsMovesSessionsAndDeletesSource(t *testing.T) {
	mem := New()
	ctx := context.Background()
	target := &store.Project{ID: "target", Name: "Target", RootDirs: []string{"/target"}}
	source := &store.Project{ID: "source", Name: "Source", RootDirs: []string{"/shared"}}
	for _, project := range []*store.Project{target, source} {
		if err := mem.CreateProject(ctx, project); err != nil {
			t.Fatal(err)
		}
	}
	for _, session := range []*store.Session{
		{ID: "target-session", Provider: "mock", Model: "mock", ProjectID: target.ID},
		{ID: "source-session", Provider: "mock", Model: "mock", ProjectID: source.ID},
	} {
		if err := mem.CreateSession(ctx, session); err != nil {
			t.Fatal(err)
		}
	}
	name := "Merged"
	dirs := []string{"/shared"}
	merged, err := mem.MergeProjects(ctx, target.ID, source.ID, store.ProjectUpdate{Name: &name, RootDirs: &dirs})
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID != target.ID || merged.Name != name || !store.SameProjectDirs(merged.RootDirs, dirs) {
		t.Fatalf("merged project = %+v", merged)
	}
	if _, err := mem.GetProject(ctx, source.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("source project still exists: %v", err)
	}
	for _, sessionID := range []string{"target-session", "source-session"} {
		session, err := mem.GetSession(ctx, sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if session.ProjectID != target.ID {
			t.Fatalf("session %s project = %q", sessionID, session.ProjectID)
		}
	}
}

func TestMergeProjectsRejectsChangedSourceDirectories(t *testing.T) {
	mem := New()
	ctx := context.Background()
	for _, project := range []*store.Project{
		{ID: "target", Name: "Target", RootDirs: []string{"/target"}},
		{ID: "source", Name: "Source", RootDirs: []string{"/source"}},
	} {
		if err := mem.CreateProject(ctx, project); err != nil {
			t.Fatal(err)
		}
	}
	name := "Target"
	dirs := []string{"/changed"}
	if _, err := mem.MergeProjects(ctx, "target", "source", store.ProjectUpdate{Name: &name, RootDirs: &dirs}); !errors.Is(err, store.ErrProjectMergeConflict) {
		t.Fatalf("merge error = %v", err)
	}
	if _, err := mem.GetProject(ctx, "source"); err != nil {
		t.Fatalf("source project removed after rejected merge: %v", err)
	}
}

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
