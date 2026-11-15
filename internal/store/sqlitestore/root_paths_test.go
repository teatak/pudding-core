package sqlitestore

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestProjectAndSessionRootsPreserveWhitespaceAcrossRestart(t *testing.T) {
	st, database := openTestStore(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), " project ")
	neighbor := root[:len(root)-1]
	project := &store.Project{ID: "project", Name: "Project", RootDirs: []string{root}}
	if err := st.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	if got, err := st.GetProject(ctx, project.ID); err != nil || !reflect.DeepEqual(got.RootDirs, []string{root}) {
		t.Fatalf("project creation changed the root: %+v %v", got, err)
	}
	roots := []string{root, neighbor}
	if _, err := st.UpdateProject(ctx, project.ID, store.ProjectUpdate{RootDirs: &roots}); err != nil {
		t.Fatal(err)
	}
	createTestSession(t, st, "session")
	if _, err := st.UpdateSession(ctx, "session", store.SessionUpdate{DirectoryGrants: &roots}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(database)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, err := reopened.GetProject(ctx, project.ID); err != nil || !reflect.DeepEqual(got.RootDirs, roots) {
		t.Fatalf("project roots changed after restart: %+v %v", got, err)
	}
	if got, err := reopened.GetSessionDirectories(ctx, "session"); err != nil || !reflect.DeepEqual(got, roots) {
		t.Fatalf("session roots changed after restart: %q %v", got, err)
	}
}

func TestTurnInputPreservesLocalFolderAndProjectReferencePaths(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "session")
	root := filepath.Join(t.TempDir(), " project ")
	relative := " child "
	source := filepath.Join(root, relative)
	parts := []store.ContentPart{
		store.LocalFolderPart(store.LocalFolder{ID: "folder", Path: root}),
		store.ProjectReferencePart(store.ProjectReference{ID: "reference", Name: "Child", Path: relative, SourcePath: source, RootID: "root", Kind: "dir"}),
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "session", TurnID: "turn", UserMessageID: "user", ClientMessageID: "client",
		UserText: "inspect these folders", UserParts: parts,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "turn", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	turn, err := st.GetConversationTurn(ctx, "session", "turn")
	if err != nil {
		t.Fatal(err)
	}
	var user *store.Message
	for _, message := range turn.Messages {
		if message.Role == store.RoleUser {
			user = message
			break
		}
	}
	if user == nil {
		t.Fatal("stored turn has no user input")
	}
	if folders := store.LocalFoldersFromParts(user.Parts); len(folders) != 1 || folders[0].Path != root {
		t.Fatalf("turn input changed the local directory: %+v", folders)
	}
	if references := store.ProjectReferencesFromParts(user.Parts); len(references) != 1 || references[0].Path != relative || references[0].SourcePath != source {
		t.Fatalf("turn input changed the referenced directory: %+v", references)
	}
	if roots, err := st.GetSessionDirectories(ctx, "session"); err != nil || len(roots) != 0 {
		t.Fatalf("folder metadata unexpectedly granted access: %q %v", roots, err)
	}
}
