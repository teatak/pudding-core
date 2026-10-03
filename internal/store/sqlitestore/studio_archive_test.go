package sqlitestore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func TestStudioArchiveRetainsContentAndRejectsStaleCleanup(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "owner")
	created := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	_, err := st.CreateDocument(ctx, &store.StudioItem{ID: "doc", Kind: "doc", Name: "Kept", CreatedAt: created, UpdatedAt: created}, "Keep my text", store.ContentAuthor{Kind: "user"})
	if err != nil {
		t.Fatal(err)
	}
	item, _ := st.GetStudioItem(ctx, "doc")
	body, _ := st.GetDocument(ctx, "doc")
	if _, err := st.OpenStudioItem(ctx, "owner", "doc", "view"); err != nil {
		t.Fatal(err)
	}
	if err := st.PutLibraryFavorite(ctx, "owner", store.LibraryFavorite{ID: "fav", Kind: "studio", SavedItemID: "doc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetStudioItemArchived(ctx, "doc", item.Revision-1, true); !errors.Is(err, store.ErrStudioItemConflict) {
		t.Fatal(err)
	}
	archived, err := st.SetStudioItemArchived(ctx, "doc", item.Revision, true)
	if err != nil || archived.ArchivedAt == nil || !archived.UpdatedAt.Equal(item.UpdatedAt) {
		t.Fatal(archived, err)
	}
	retry, err := st.SetStudioItemArchived(ctx, "doc", item.Revision, true)
	if err != nil || retry.Revision != archived.Revision || !retry.ArchivedAt.Equal(*archived.ArchivedAt) {
		t.Fatal("archive retry changed retention", retry, err)
	}
	if _, err := st.GetDocument(ctx, "doc"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("archive remained readable", err)
	}
	late := "late"
	if _, err := st.WriteDocument(ctx, "doc", store.DocumentWrite{ClientRequestID: "late", ExpectedHash: &body.ContentHash, Body: &late, Author: store.ContentAuthor{Kind: "user"}}); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("archive accepted a write", err)
	}
	active, _ := st.ListStudioItems(ctx, store.StudioItemsActive)
	mounts, _ := st.ListStudioMounts(ctx, "owner")
	if len(active) != 0 || len(mounts) != 0 {
		t.Fatal("archive leaked into active views")
	}
	before, _ := st.ListStudioItemsForCleanup(ctx, archived.ArchivedAt.Add(-time.Millisecond))
	at, _ := st.ListStudioItemsForCleanup(ctx, *archived.ArchivedAt)
	if len(before) != 0 || len(at) != 1 {
		t.Fatal("wrong expiry boundary", before, at)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	list, err := st.ListStudioItems(ctx, store.StudioItemsArchived)
	if err != nil || len(list) != 1 || !list[0].ArchivedAt.Equal(*archived.ArchivedAt) {
		t.Fatal("archive lost after reopen", list, err)
	}
	restored, err := st.SetStudioItemArchived(ctx, "doc", archived.Revision, false)
	if err != nil || restored.ArchivedAt != nil {
		t.Fatal(restored, err)
	}
	if _, err := st.MarkStudioItemDeleted(ctx, "doc", archived.Revision); !errors.Is(err, store.ErrStudioItemConflict) {
		t.Fatal("stale cleanup deleted restored item", err)
	}
	after, err := st.GetDocument(ctx, "doc")
	if err != nil || *after != *body {
		t.Fatal("restoration changed content", after, err)
	}
	mounts, _ = st.ListStudioMounts(ctx, "owner")
	favorites, _ := st.ListLibraryFavorites(ctx, "owner")
	if len(mounts) != 1 || len(favorites) != 1 {
		t.Fatal("restoration lost resource references")
	}
	if _, err := st.MarkStudioItemDeleted(ctx, "doc", restored.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetStudioItemArchived(ctx, "doc", restored.Revision, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("permanent deletion remained restorable", err)
	}
	if err := st.PurgeDeletedStudioItem(ctx, "doc"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"studio_items", "studio_item_content", "studio_item_revisions", "studio_item_saves", "studio_mounts", "library_favorites"} {
		assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM "+table, "0")
	}
}
