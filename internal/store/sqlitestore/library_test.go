package sqlitestore

import (
	"context"
	"errors"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestCanvasesShareContentRetainHistoryAndFavorites(t *testing.T) {
	ctx := context.Background()
	st, _ := openTestStore(t)
	createTestSession(t, st, "a")
	createTestSession(t, st, "b")
	first, err := seedCanvasMount(st, "a", "c", "Report")
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
	w0, _ := st.GetCanvas(ctx, first.ResourceID)
	changed, err := st.SaveCanvasRevision(ctx, &store.CanvasRevision{CanvasID: w0.ID, Hash: "second", ClientRequestID: "second"}, w0.HeadRevision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveCanvasRevision(ctx, &store.CanvasRevision{CanvasID: w0.ID, Hash: "stale", ClientRequestID: "stale"}, w0.HeadRevision); !errors.Is(err, store.ErrCanvasConflict) {
		t.Fatalf("stale write accepted: %v", err)
	}
	views, err := st.ListCanvasItems(ctx, "b")
	if err != nil || len(views) != 1 || views[0].Revision != changed.Revision {
		t.Fatalf("stale mount: %+v %v", views, err)
	}

	favorites, err := st.ListLibraryFavorites(ctx, "b")
	if err != nil || len(favorites) != 0 {
		t.Fatal("edit re-starred resource")
	}
	versions, err := st.ListCanvasRevisions(ctx, first.ResourceID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("missing versions: %+v %v", versions, err)
	}
	if err := st.DeleteSession(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	w, err := st.GetCanvas(ctx, first.ResourceID)
	if err != nil || w.SourceSessionID != "" {
		t.Fatalf("source deletion removed resource: %+v %v", w, err)
	}
	if err := st.DeleteCanvasItem(ctx, "b", other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetCanvas(ctx, first.ResourceID); err != nil {
		t.Fatal("closing view deleted resource")
	}
	reopened, err := st.OpenCanvasResource(ctx, "b", first.ResourceID, "reopened")
	if err != nil || reopened.ResourceID != first.ResourceID {
		t.Fatalf("lost reopened data: %+v %v", reopened, err)
	}
	if err := st.PutLibraryFavorite(ctx, "b", favorite); err != nil {
		t.Fatal(err)
	}
	w.Deleted = true
	if _, err := st.UpdateCanvas(ctx, w, w.Revision); err != nil {
		t.Fatal(err)
	}
	views, err = st.ListCanvasItems(ctx, "b")
	if err != nil || len(views) != 0 {
		t.Fatal("deleted canvas still open")
	}
	if _, err := seedCanvasMount(st, "b", "reopened", "New after deletion"); err != nil {
		t.Fatalf("deleted resource kept its local ID reserved: %v", err)
	}
	if _, err := st.OpenCanvasResource(ctx, "missing", first.ResourceID, "v"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("missing actor accepted")
	}
}

func TestCurrentCanvasReopenPreservesVersions(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "s")
	item, err := seedCanvasMount(st, "s", "c", "Keep")
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
	if err != nil || len(views) != 1 || views[0].ResourceID != item.ResourceID {
		t.Fatal("restart changed resource")
	}
	if backups := migrationBackupFiles(t, path); len(backups) != 0 {
		t.Fatal("current database migrated again")
	}
}

func TestRemovedCanvasLocalIDCanBeReusedWithoutOverwritingResource(t *testing.T) {
	ctx := context.Background()
	st, _ := openTestStore(t)
	createTestSession(t, st, "s")
	first, err := seedCanvasMount(st, "s", "report", "Original")
	if err != nil {
		t.Fatal(err)
	}
	if err = st.DeleteCanvasItem(ctx, "s", "report"); err != nil {
		t.Fatal(err)
	}
	next, err := seedCanvasMount(st, "s", "report", "New")
	if err != nil {
		t.Fatal(err)
	}
	if next.ResourceID == first.ResourceID {
		t.Fatal("reused local ID overwrote an independent resource")
	}
	old, err := st.OpenCanvasResource(ctx, "s", first.ResourceID, "original")
	if err != nil || old.Title != "Original" {
		t.Fatalf("lost old canvas: %+v %v", old, err)
	}
}

func seedCanvasMount(st store.Store, session, id, name string) (*store.CanvasItem, error) {
	ctx := context.Background()
	w, err := st.CreateCanvas(ctx, &store.Canvas{ID: session + "-" + id + "-" + name, Name: name, SourceSessionID: session})
	if err != nil {
		return nil, err
	}
	if _, err = st.SaveCanvasRevision(ctx, &store.CanvasRevision{CanvasID: w.ID, Hash: "first", ClientRequestID: "first"}, ""); err != nil {
		return nil, err
	}
	return st.OpenCanvasResource(ctx, session, w.ID, id)
}
