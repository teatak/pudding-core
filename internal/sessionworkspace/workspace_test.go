package sessionworkspace

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestResolveKeepsScratchAlongsideProjectRoots(t *testing.T) {
	ctx := context.Background()
	st := memstore.New()
	homeDir := t.TempDir()
	projectRoot := t.TempDir()
	const sessionID = "sess_workspace_roots"
	if err := st.CreateProject(ctx, &store.Project{ID: "proj_workspace_roots", RootDirs: []string{projectRoot}}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, &store.Session{ID: sessionID, ProjectID: "proj_workspace_roots", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	scratch, err := home.PrepareCodeScratch(homeDir, sessionID)
	if err != nil {
		t.Fatal(err)
	}

	workspace, err := Resolve(ctx, st, homeDir, sessionID, ScratchExisting)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Project == nil || workspace.Temporary || workspace.ScratchRoot != scratch || len(workspace.RootDirs) != 2 {
		t.Fatalf("unexpected workspace: %+v", workspace)
	}
	if workspace.RootDirs[0] != filepath.Clean(projectRoot) || workspace.RootDirs[1] != scratch {
		t.Fatalf("unexpected root order: %+v", workspace.RootDirs)
	}
}

func TestResolveDoesNotCreateScratchWhileInspecting(t *testing.T) {
	ctx := context.Background()
	st := memstore.New()
	homeDir := t.TempDir()
	const sessionID = "sess_workspace_empty"
	if err := st.CreateSession(ctx, &store.Session{ID: sessionID, Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}

	workspace, err := Resolve(ctx, st, homeDir, sessionID, ScratchExisting)
	if err != nil {
		t.Fatal(err)
	}
	if !workspace.Temporary || len(workspace.RootDirs) != 0 {
		t.Fatalf("unexpected empty workspace: %+v", workspace)
	}
	if path := home.CodeScratchPath(homeDir, sessionID); path == "" {
		t.Fatal("scratch path should be derivable")
	} else if _, exists, err := home.ExistingCodeScratch(homeDir, sessionID); err != nil || exists {
		t.Fatalf("inspection created scratch %q: exists=%t err=%v", path, exists, err)
	}
}
