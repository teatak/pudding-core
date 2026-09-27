package sqlitestore

import (
	"context"
	"errors"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestCanvasesShareContentRetainHistoryAndFavorites(t *testing.T) {
	factories := map[string]func(*testing.T) store.Store{"sqlite": func(t *testing.T) store.Store { s, _ := openTestStore(t); return s }, "memory": func(t *testing.T) store.Store { return memstore.New() }}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			st := factory(t)
			createTestSession(t, st, "a")
			createTestSession(t, st, "b")
			first, err := st.PutCanvasItem(ctx, store.CanvasItemInput{ActorSessionID: "a", ID: "c", Kind: "table", Title: "Report", Item: []byte(`{"rows":[1]}`)})
			if err != nil {
				t.Fatal(err)
			}
			other, err := st.OpenCanvasResource(ctx, "b", first.ResourceID, "b-view")
			if err != nil {
				t.Fatal(err)
			}
			favorite := store.LibraryFavorite{ID: "canvas:" + first.ResourceID, Kind: "canvas", SavedItemID: first.ResourceID}
			if err := st.PutLibraryFavorite(ctx, "a", favorite); err != nil {
				t.Fatal(err)
			}
			if err := st.DeleteLibraryFavorite(ctx, "b", favorite.ID); err != nil {
				t.Fatal(err)
			}
			changed, err := st.PutCanvasItem(ctx, store.CanvasItemInput{ActorSessionID: "a", ID: "c", ExpectedRevision: first.Revision, Kind: "table", Title: "Changed", Item: []byte(`{"rows":[2]}`)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.PutCanvasItem(ctx, store.CanvasItemInput{ActorSessionID: "b", ID: other.ID, ExpectedRevision: other.Revision, Kind: "table", Title: "Stale", Item: []byte(`{"rows":[3]}`)}); !errors.Is(err, store.ErrWorkbenchConflict) {
				t.Fatalf("stale overwrite accepted: %v", err)
			}
			views, err := st.ListCanvasItems(ctx, "b")
			if err != nil || len(views) != 1 || views[0].Revision != changed.Revision || string(views[0].Item) != `{"rows":[2]}` {
				t.Fatalf("view copied canonical data: %+v %v", views, err)
			}
			favorites, err := st.ListLibraryFavorites(ctx, "b")
			if err != nil || len(favorites) != 0 {
				t.Fatal("edit re-starred resource")
			}
			versions, err := st.ListWorkbenchRevisions(ctx, first.ResourceID)
			if err != nil || len(versions) != 2 {
				t.Fatalf("missing versions: %+v %v", versions, err)
			}
			if err := st.DeleteSession(ctx, "a"); err != nil {
				t.Fatal(err)
			}
			w, err := st.GetWorkbench(ctx, first.ResourceID)
			if err != nil || w.SourceSessionID != "" {
				t.Fatalf("source deletion removed resource: %+v %v", w, err)
			}
			if err := st.DeleteCanvasItem(ctx, "b", other.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := st.GetWorkbench(ctx, first.ResourceID); err != nil {
				t.Fatal("closing view deleted resource")
			}
			reopened, err := st.OpenCanvasResource(ctx, "b", first.ResourceID, "reopened")
			if err != nil || string(reopened.Item) != `{"rows":[2]}` {
				t.Fatalf("lost reopened data: %+v %v", reopened, err)
			}
			if err := st.PutLibraryFavorite(ctx, "b", favorite); err != nil {
				t.Fatal(err)
			}
			w.Deleted = true
			if _, err := st.UpdateWorkbench(ctx, w, w.Revision); err != nil {
				t.Fatal(err)
			}
			views, err = st.ListCanvasItems(ctx, "b")
			if err != nil || len(views) != 0 {
				t.Fatal("deleted canvas still open")
			}
			if _, err := st.PutCanvasItem(ctx, store.CanvasItemInput{ActorSessionID: "b", ID: "reopened", Kind: "markdown", Title: "New after deletion", Item: []byte(`{"markdown":"new"}`)}); err != nil {
				t.Fatalf("deleted resource kept its local ID reserved: %v", err)
			}
			if _, err := st.OpenCanvasResource(ctx, "missing", first.ResourceID, "v"); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("missing actor accepted")
			}
		})
	}
}

func TestCurrentCanvasReopenPreservesVersions(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "s")
	item, err := st.PutCanvasItem(ctx, store.CanvasItemInput{ActorSessionID: "s", ID: "c", Kind: "markdown", Title: "Keep", Item: []byte(`{"content":"keep"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutLibraryFavorite(ctx, "s", store.LibraryFavorite{ID: "f", Kind: "canvas", SavedItemID: item.ResourceID}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	views, err := st.ListCanvasItems(ctx, "s")
	if err != nil || len(views) != 1 || views[0].ResourceID != item.ResourceID || string(views[0].Item) != string(item.Item) {
		t.Fatal("restart changed resource")
	}
	if backups := migrationBackupFiles(t, path); len(backups) != 0 {
		t.Fatal("current database migrated again")
	}
}

func TestRemovedCanvasLocalIDCanBeReusedWithoutOverwritingResource(t *testing.T) {
	for name, factory := range map[string]func(*testing.T) store.Store{"sqlite": func(t *testing.T) store.Store { s, _ := openTestStore(t); return s }, "memory": func(t *testing.T) store.Store { return memstore.New() }} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			st := factory(t)
			createTestSession(t, st, "s")
			in := store.CanvasItemInput{ActorSessionID: "s", ID: "report", Kind: "markdown", Title: "Original", Item: []byte(`{"markdown":"keep"}`)}
			first, err := st.PutCanvasItem(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			if err = st.DeleteCanvasItem(ctx, "s", "report"); err != nil {
				t.Fatal(err)
			}
			in.Title = "New"
			in.Item = []byte(`{"markdown":"new"}`)
			next, err := st.PutCanvasItem(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			if next.ResourceID == first.ResourceID {
				t.Fatal("reused local ID overwrote an independent resource")
			}
			old, err := st.OpenCanvasResource(ctx, "s", first.ResourceID, "original")
			if err != nil || string(old.Item) != `{"markdown":"keep"}` {
				t.Fatalf("lost old canvas: %+v %v", old, err)
			}
		})
	}
}
