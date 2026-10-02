package sqlitestore

import (
	"context"
	"errors"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestStudioItemsShareContentRetainHistoryAndFavorites(t *testing.T) {
	ctx := context.Background()
	st, _ := openTestStore(t)
	createTestSession(t, st, "a")
	createTestSession(t, st, "b")
	first, err := seedStudioMount(st, "a", "c", "Report")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.OpenStudioItem(ctx, "b", first.ItemID, "b-view")
	if err != nil {
		t.Fatal(err)
	}
	favorite := store.LibraryFavorite{ID: "studio:" + first.ItemID, Kind: "studio", SavedItemID: first.ItemID}
	if err := st.PutLibraryFavorite(ctx, "a", favorite); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteLibraryFavorite(ctx, "b", favorite.ID); err != nil {
		t.Fatal(err)
	}
	w0, _ := st.GetStudioItem(ctx, first.ItemID)
	changed, err := st.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: w0.ID, Hash: "second", ClientRequestID: "second"}, w0.HeadRevision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: w0.ID, Hash: "stale", ClientRequestID: "stale"}, w0.HeadRevision); !errors.Is(err, store.ErrStudioItemConflict) {
		t.Fatalf("stale write accepted: %v", err)
	}
	views, err := st.ListStudioMounts(ctx, "b")
	if err != nil || len(views) != 1 || views[0].Revision != changed.Revision {
		t.Fatalf("stale mount: %+v %v", views, err)
	}

	favorites, err := st.ListLibraryFavorites(ctx, "b")
	if err != nil || len(favorites) != 0 {
		t.Fatal("edit re-starred resource")
	}
	versions, err := st.ListStudioItemRevisions(ctx, first.ItemID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("missing versions: %+v %v", versions, err)
	}
	if err := st.DeleteSession(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	w, err := st.GetStudioItem(ctx, first.ItemID)
	if err != nil || w.SourceSessionID != "" {
		t.Fatalf("source deletion removed resource: %+v %v", w, err)
	}
	if err := st.DeleteStudioMount(ctx, "b", other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetStudioItem(ctx, first.ItemID); err != nil {
		t.Fatal("closing view deleted resource")
	}
	reopened, err := st.OpenStudioItem(ctx, "b", first.ItemID, "reopened")
	if err != nil || reopened.ItemID != first.ItemID {
		t.Fatalf("lost reopened data: %+v %v", reopened, err)
	}
	if err := st.PutLibraryFavorite(ctx, "b", favorite); err != nil {
		t.Fatal(err)
	}
	w.Deleted = true
	if _, err := st.UpdateStudioItem(ctx, w, w.Revision); err != nil {
		t.Fatal(err)
	}
	views, err = st.ListStudioMounts(ctx, "b")
	if err != nil || len(views) != 0 {
		t.Fatal("deleted widget still open")
	}
	if _, err := seedStudioMount(st, "b", "reopened", "New after deletion"); err != nil {
		t.Fatalf("deleted resource kept its local ID reserved: %v", err)
	}
	if _, err := st.OpenStudioItem(ctx, "missing", first.ItemID, "v"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("missing actor accepted")
	}
}

func TestCurrentStudioItemReopenPreservesVersions(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "s")
	item, err := seedStudioMount(st, "s", "c", "Keep")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutLibraryFavorite(ctx, "s", store.LibraryFavorite{ID: "f", Kind: "studio", SavedItemID: item.ItemID}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	views, err := st.ListStudioMounts(ctx, "s")
	if err != nil || len(views) != 1 || views[0].ItemID != item.ItemID {
		t.Fatal("restart changed resource")
	}
	if backups := migrationBackupFiles(t, path); len(backups) != 0 {
		t.Fatal("current database migrated again")
	}
}

func TestRemovedStudioMountLocalIDCanBeReusedWithoutOverwritingItem(t *testing.T) {
	ctx := context.Background()
	st, _ := openTestStore(t)
	createTestSession(t, st, "s")
	first, err := seedStudioMount(st, "s", "report", "Original")
	if err != nil {
		t.Fatal(err)
	}
	if err = st.DeleteStudioMount(ctx, "s", "report"); err != nil {
		t.Fatal(err)
	}
	next, err := seedStudioMount(st, "s", "report", "New")
	if err != nil {
		t.Fatal(err)
	}
	if next.ItemID == first.ItemID {
		t.Fatal("reused local ID overwrote an independent resource")
	}
	old, err := st.OpenStudioItem(ctx, "s", first.ItemID, "original")
	if err != nil || old.Title != "Original" {
		t.Fatalf("lost old widget: %+v %v", old, err)
	}
}

func seedStudioMount(st store.Store, session, id, name string) (*store.StudioMount, error) {
	ctx := context.Background()
	w, err := st.CreateStudioItem(ctx, &store.StudioItem{Kind: store.StudioItemKindWidget, ID: session + "-" + id + "-" + name, Name: name, SourceSessionID: session})
	if err != nil {
		return nil, err
	}
	if _, err = st.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: w.ID, Hash: "first", ClientRequestID: "first"}, ""); err != nil {
		return nil, err
	}
	return st.OpenStudioItem(ctx, session, w.ID, id)
}
