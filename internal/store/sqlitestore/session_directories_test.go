package sqlitestore

import (
	"context"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestSessionDirectoriesPersistWithoutProjectAndDoNotPropagate(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "owner")
	dirs := []string{t.TempDir()}
	if _, err := st.UpdateSession(ctx, "owner", store.SessionUpdate{DirectoryGrants: &dirs}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateChildSession(ctx, "owner", &store.Session{ID: "child", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if got, err := st.GetSessionDirectories(ctx, "child"); err != nil || len(got) != 0 {
		t.Fatalf("child inherited directory consent: %v %v", got, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, err := reopened.GetSessionDirectories(ctx, "owner"); err != nil || !reflect.DeepEqual(got, dirs) {
		t.Fatalf("restart lost consent: %v %v", got, err)
	}
	if projects, err := reopened.ListProjects(ctx); err != nil || len(projects) != 0 {
		t.Fatalf("directory consent created a project: %+v %v", projects, err)
	}
	if err := reopened.DeleteSession(ctx, "owner"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := reopened.db.QueryRow(`SELECT count(*) FROM session_directory_grants`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted session retained consent: %d %v", count, err)
	}
}

func TestSessionDirectoryApprovalRollsBackWithMode(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "owner")
	if _, err := st.db.Exec(`CREATE TRIGGER reject_mode_update BEFORE UPDATE ON sessions BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
		t.Fatal(err)
	}
	dirs := []string{t.TempDir()}
	mode, lease := store.ModeCode, store.ModeLeaseSession
	if _, err := st.UpdateSession(ctx, "owner", store.SessionUpdate{DirectoryGrants: &dirs, ActiveMode: &mode, ModeLease: &lease}); err == nil {
		t.Fatal("expected failure")
	}
	if got, err := st.GetSessionDirectories(ctx, "owner"); err != nil || len(got) != 0 {
		t.Fatalf("failed approval retained directories: %v %v", got, err)
	}
}
