package sqlitestore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func TestProjectCreationReceipt(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	create := func(key, name string) (*store.Project, error) {
		p := &store.Project{ID: store.NewID("proj"), Name: name}
		err := st.CreateProject(ctx, p, key)
		return p, err
	}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); p, e := create("same", "Initial"); ids <- p.ID; errs <- e }()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var id string
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("duplicate project")
		}
		id = got
	}
	later := "Later name"
	if _, err := st.UpdateProject(ctx, id, store.ProjectUpdate{Name: &later}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p, err := create("same", "Initial")
	if err != nil || p.ID != id || p.Name != later {
		t.Fatalf("replay %+v %v", p, err)
	}
	if _, err = create("same", "Different"); !errors.Is(err, store.ErrProjectRequestConflict) {
		t.Fatalf("reuse: %v", err)
	}
	if _, err = create("has spaces", "Invalid"); !errors.Is(err, store.ErrInvalidProject) {
		t.Fatalf("invalid key: %v", err)
	}
	other, err := create("new", "Initial")
	if err != nil || other.ID == id {
		t.Fatal("new intent must be independent", err)
	}
	if err = st.DeleteProject(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err = create("same", "Initial"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted result revived: %v", err)
	}
	projects, _ := st.ListProjects(ctx)
	if len(projects) != 1 {
		t.Fatal("unexpected projects", len(projects))
	}
}

func TestProjectRequestTransactionRollback(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	if _, err := st.db.Exec(`CREATE TRIGGER fail_receipt BEFORE INSERT ON project_requests BEGIN SELECT RAISE(ABORT,'test rollback'); END`); err != nil {
		t.Fatal(err)
	}
	p := &store.Project{ID: "rollback", Name: "Rollback"}
	if err := st.CreateProject(ctx, p, "retry"); err == nil {
		t.Fatal("expected rollback")
	}
	if _, err := st.GetProject(ctx, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("partial create: %v", err)
	}
	if _, err := st.db.Exec(`DROP TRIGGER fail_receipt`); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateProject(ctx, p, "retry"); err != nil {
		t.Fatal(err)
	}
}

func TestProjectMergeReceipt(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	target := &store.Project{ID: "target", Name: "Target"}
	source := &store.Project{ID: "source", Name: "Source", RootDirs: []string{"/shared"}}
	for _, p := range []*store.Project{target, source} {
		if err := st.CreateProject(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	archived := time.Now()
	for _, sess := range []*store.Session{{ID: "active", Provider: "mock", Model: "mock", ProjectID: source.ID}, {ID: "archived", Provider: "mock", Model: "mock", ProjectID: source.ID, ArchivedAt: &archived}} {
		if err := st.CreateSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	name := "Merged"
	roots := source.RootDirs
	upd := store.ProjectUpdate{Name: &name, RootDirs: &roots}
	option := store.ProjectMergeOptions{ClientRequestID: "same", ExpectedTargetUpdatedAt: &target.UpdatedAt, ExpectedSourceUpdatedAt: &source.UpdatedAt}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := st.MergeProjects(ctx, target.ID, source.ID, upd, option)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"active", "archived"} {
		sess, err := st.GetSession(ctx, id)
		if err != nil || sess.ProjectID != target.ID {
			t.Fatalf("history moved: %+v %v", sess, err)
		}
	}
	newer := "Changed after merge"
	newRoots := []string{"/new-root"}
	_, err := st.UpdateProject(ctx, target.ID, store.ProjectUpdate{Name: &newer, RootDirs: &newRoots})
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p, err := st.MergeProjects(ctx, target.ID, source.ID, upd, option)
	if err != nil || p.Name != newer || !store.SameProjectDirs(p.RootDirs, newRoots) {
		t.Fatalf("replay overwrote edits: %+v %v", p, err)
	}
	changed := upd
	badName := "Wrong"
	changed.Name = &badName
	if _, err = st.MergeProjects(ctx, target.ID, source.ID, changed, option); !errors.Is(err, store.ErrProjectRequestConflict) {
		t.Fatalf("changed intent: %v", err)
	}
	if _, err = st.MergeProjects(ctx, target.ID, "never-existed", upd, store.ProjectMergeOptions{ClientRequestID: "unrelated"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing source is not success: %v", err)
	}
	st.DeleteProject(ctx, target.ID)
	if _, err = st.MergeProjects(ctx, target.ID, source.ID, upd, option); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted target revived: %v", err)
	}
}

func TestProjectMergeConflictDoesNotConsumeReceipt(t *testing.T) {
	for _, side := range []string{"source", "target"} {
		t.Run(side, func(t *testing.T) {
			st, _ := openTestStore(t)
			ctx := context.Background()
			target := &store.Project{ID: "target", Name: "Target"}
			source := &store.Project{ID: "source", Name: "Source", RootDirs: []string{"/shared"}}
			for _, p := range []*store.Project{target, source} {
				if err := st.CreateProject(ctx, p); err != nil {
					t.Fatal(err)
				}
			}
			old := time.UnixMilli(target.UpdatedAt.UnixMilli() - 1)
			option := store.ProjectMergeOptions{ClientRequestID: "retry", ExpectedTargetUpdatedAt: &target.UpdatedAt, ExpectedSourceUpdatedAt: &source.UpdatedAt}
			if side == "source" {
				option.ExpectedSourceUpdatedAt = &old
			} else {
				option.ExpectedTargetUpdatedAt = &old
			}
			name := "Merged"
			roots := source.RootDirs
			upd := store.ProjectUpdate{Name: &name, RootDirs: &roots}
			if _, err := st.MergeProjects(ctx, target.ID, source.ID, upd, option); !errors.Is(err, store.ErrProjectMergeConflict) {
				t.Fatalf("stale merge: %v", err)
			}
			if _, err := st.GetProject(ctx, source.ID); err != nil {
				t.Fatal("source lost", err)
			}
			option.ExpectedTargetUpdatedAt = &target.UpdatedAt
			option.ExpectedSourceUpdatedAt = &source.UpdatedAt
			if _, err := st.MergeProjects(ctx, target.ID, source.ID, upd, option); err != nil {
				t.Fatal("failed merge consumed key", err)
			}
		})
	}
}

func TestProjectRequestsMigrationFrom48(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	if err := st.CreateProject(ctx, &store.Project{ID: "existing", Name: "Existing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`DROP TABLE project_requests; PRAGMA user_version=48`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	next, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err = next.GetProject(ctx, "existing"); err != nil {
		t.Fatal(err)
	}
	if err = next.CreateProject(ctx, &store.Project{ID: "new", Name: "New"}, "request"); err != nil {
		t.Fatal(err)
	}
}
