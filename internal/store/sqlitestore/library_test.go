package sqlitestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestLibraryFavoritesRetainVersionsAndReferences(t *testing.T) {
	factories := map[string]func(*testing.T) store.Store{
		"sqlite": func(t *testing.T) store.Store { s, _ := openTestStore(t); return s },
		"memory": func(t *testing.T) store.Store { return memstore.New() },
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			st := factory(t)
			ctx := context.Background()
			createTestSession(t, st, "a")
			createTestSession(t, st, "b")
			_, err := st.PutCanvasItem(ctx, store.CanvasItemInput{ActorSessionID: "a", ID: "c", Kind: "table", Title: "same", Item: []byte(`{"rows":[1]}`)})
			if err != nil {
				t.Fatal(err)
			}
			saved, err := st.SaveCanvasItem(ctx, "a", "c", "s")
			if err != nil {
				t.Fatal(err)
			}
			list := func() []*store.LibraryFavorite {
				t.Helper()
				f, e := st.ListLibraryFavorites(ctx, "b")
				if e != nil {
					t.Fatal(e)
				}
				return f
			}
			if len(list()) != 1 {
				t.Fatal("first save must create favorite")
			}
			working, err := st.OpenSavedCanvasItem(ctx, "b", "s", "b-copy")
			if err != nil {
				t.Fatal(err)
			}
			if err := st.DeleteLibraryFavorite(ctx, "b", "canvas:s"); err != nil {
				t.Fatal(err)
			}
			versions, err := st.ListSavedCanvasItems(ctx, "b")
			if err != nil || len(versions) != 1 || string(versions[0].Item) != string(saved.SavedItem.Item) {
				t.Fatalf("unfavorite lost body: %v %+v", err, versions)
			}
			// Updating an unstarred version must not silently re-add its relationship.
			_, err = st.PutCanvasItem(ctx, store.CanvasItemInput{ActorSessionID: "a", ID: "c", Kind: "table", Title: "changed", Item: []byte(`{"rows":[2]}`)})
			if err != nil {
				t.Fatal(err)
			}
			updated, err := st.SaveCanvasItem(ctx, "a", "c", "unused")
			if err != nil || updated.SavedItem.Revision != 2 || len(list()) != 0 {
				t.Fatalf("update changed favorite: %v %+v", err, updated)
			}
			_, err = st.PutCanvasItem(ctx, store.CanvasItemInput{ActorSessionID: "b", ID: working.ID, Kind: "table", Title: "conflict", Item: []byte(`{"rows":[3]}`)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = st.SaveCanvasItem(ctx, "b", working.ID, "unused"); !errors.Is(err, store.ErrCanvasConflict) {
				t.Fatalf("missing conflict: %v", err)
			}
			for _, f := range []store.LibraryFavorite{
				{ID: "canvas:s", Kind: "canvas", SavedItemID: "s", SourceSessionID: "a"},
				{ID: "w", Kind: "web", URL: "https://example.com/", Title: "same", SourceSessionID: "a"},
			} {
				if err := st.PutLibraryFavorite(ctx, "a", f); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.PutLibraryFavorite(ctx, "b", store.LibraryFavorite{ID: "w-duplicate", Kind: "web", URL: "https://example.com/", SourceSessionID: "b"}); err != nil {
				t.Fatal(err)
			}
			if len(list()) != 2 {
				t.Fatal("same URL duplicated or different resource merged")
			}
			if err := st.ClearBrowserHistory(ctx); err != nil {
				t.Fatal(err)
			}
			if len(list()) != 2 {
				t.Fatal("history clear removed favorite")
			}
			if err := st.DeleteSavedCanvasItem(ctx, "b", "s"); err != nil {
				t.Fatal(err)
			}
			if len(list()) != 1 {
				t.Fatal("saved deletion left orphan favorite")
			}
			remaining, err := st.ListCanvasItems(ctx, "b")
			if err != nil || len(remaining) != 1 || remaining[0].SourceSavedItemID != "" || string(remaining[0].Item) != `{"rows":[3]}` {
				t.Fatalf("deleting version lost local content: %+v %v", remaining, err)
			}
			if _, err := st.ListLibraryFavorites(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("missing actor accepted")
			}
		})
	}
}

func TestLibraryMigratesSavedVersionsOnce(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "source")
	for _, id := range []string{"one", "two"} {
		_, err := st.PutCanvasItem(ctx, store.CanvasItemInput{ActorSessionID: "source", ID: id, Kind: "markdown", Title: "same title", Item: []byte(`{"content":"keep me"}`)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.SaveCanvasItem(ctx, "source", id, id); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`DROP TABLE library_favorites; PRAGMA user_version=13`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := st.ListLibraryFavorites(ctx, "source")
	if err != nil || len(f) != 2 {
		t.Fatalf("migration missing same-title identities: %v %+v", err, f)
	}
	if err := st.DeleteLibraryFavorite(ctx, "source", f[0].ID); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	f, err = st.ListLibraryFavorites(ctx, "source")
	if err != nil || len(f) != 1 {
		t.Fatal("restart re-starred migrated content")
	}
	versions, err := st.ListSavedCanvasItems(ctx, "source")
	if err != nil || len(versions) != 2 {
		t.Fatal("unfavorite removed saved version")
	}
	for _, v := range versions {
		if v.Revision != 1 || v.SourceSessionID != "source" || string(v.Item) != `{"content":"keep me"}` {
			t.Fatalf("version mutated: %+v", v)
		}
	}
}

func TestCurrentWorkspaceSchemaReopenPreservesContentAndHistory(t *testing.T) {
	st, dbPath := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "source")
	root := t.TempDir()
	file := filepath.Join(root, "keep.md")
	if err := os.WriteFile(file, []byte("keep file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutCanvasItem(ctx, store.CanvasItemInput{ActorSessionID: "source", ID: "c", Kind: "markdown", Item: []byte(`{"markdown":"keep canvas"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveCanvasItem(ctx, "source", "c", "s"); err != nil {
		t.Fatal(err)
	}
	// Historical records remain on disk, but are no longer read or written by the product.
	if _, err := st.db.Exec(`INSERT INTO library_recent_opens(id,kind,source_session_id,root_path,path,opened_at) VALUES('old','file','source',?,'keep.md',101)`, root); err != nil {
		t.Fatal(err)
	}

	if _, err := st.db.Exec(`INSERT INTO library_favorites(id,kind,url,title,created_at) VALUES('w','web','https://example.com/','Keep web',101)`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	favorites, err := st.ListLibraryFavorites(ctx, "source")
	if err != nil || len(favorites) != 2 {
		t.Fatalf("lost canvas/web favorites: %v %+v", err, favorites)
	}
	assertWorkspaceMigrationValue(t, st.db, "SELECT title||':'||created_at FROM library_favorites WHERE id='w'", "Keep web:101")
	if backups := migrationBackupFiles(t, dbPath); len(backups) != 0 {
		t.Fatalf("v17 database was migrated again: %v", backups)
	}
	assertWorkspaceMigrationValue(t, st.db, "SELECT path FROM library_recent_opens WHERE id='old'", "keep.md")

	saved, err := st.ListSavedCanvasItems(ctx, "source")
	if err != nil || len(saved) != 1 || saved[0].Revision != 1 || string(saved[0].Item) != `{"markdown":"keep canvas"}` {
		t.Fatalf("changed saved canvas: %v %+v", err, saved)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "keep file" {
		t.Fatal("reopen touched file")
	}
}
