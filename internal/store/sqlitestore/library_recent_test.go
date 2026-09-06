package sqlitestore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestLibraryRecentReferencesAndLifetimes(t *testing.T) {
	factories := map[string]func(*testing.T) store.Store{"sqlite": func(t *testing.T) store.Store { s, _ := openTestStore(t); return s }, "memory": func(t *testing.T) store.Store { return memstore.New() }}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			st := factory(t)
			ctx := context.Background()
			createTestSession(t, st, "a")
			createTestSession(t, st, "b")
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			list := func() []*store.LibraryRecentOpen {
				t.Helper()
				e, err := st.ListLibraryRecentOpens(ctx, "b")
				must(err)
				return e
			}
			canvas := store.CanvasItemInput{ActorSessionID: "a", ID: "c", Kind: "table", Title: "Before", Item: []byte(`{"rows":[1]}`)}
			_, err := st.PutCanvasItem(ctx, canvas)
			must(err)
			saved, err := st.SaveCanvasItem(ctx, "a", "c", "saved")
			must(err)
			if len(list()) != 0 {
				t.Fatal("creating/saving content fabricated open history")
			}
			before := time.Now().Truncate(time.Millisecond)
			must(st.RecordLibraryRecentOpen(ctx, "a", store.LibraryRecentOpen{Kind: "canvas", ItemID: "c"}))
			first := list()[0]
			if first.OpenedAt.Before(before) || first.Title != "Before" {
				t.Fatalf("wrong open metadata: %+v", first)
			}
			// Canonical updates change the label, not the opening time.
			canvas.Title = "After"
			_, err = st.PutCanvasItem(ctx, canvas)
			must(err)
			if e := list()[0]; e.Title != "After" || !e.OpenedAt.Equal(first.OpenedAt) {
				t.Fatal("copied title or edit counted as open")
			}
			canvas.ActorSessionID = "b"
			_, err = st.PutCanvasItem(ctx, canvas)
			must(err)
			must(st.RecordLibraryRecentOpen(ctx, "b", store.LibraryRecentOpen{Kind: "canvas", ItemID: "c"}))
			file := store.LibraryRecentOpen{Kind: "file", RootPath: "/root", Path: "docs/same.md"}
			must(st.RecordLibraryRecentOpen(ctx, "a", file))
			fileID := ""
			for _, e := range list() {
				if e.Kind == "file" {
					fileID = e.ID
				}
			}
			time.Sleep(2 * time.Millisecond)
			must(st.RecordLibraryRecentOpen(ctx, "b", file))
			if e := list()[0]; e.ID != fileID || e.SourceSessionID != "b" || len(list()) != 3 {
				t.Fatal("same file duplicated or canvas sessions merged")
			}
			must(st.PutLibraryFavorite(ctx, "b", store.LibraryFavorite{ID: "web-star", Kind: "web", URL: "https://example.com/"}))
			must(st.MoveLibraryFileReferences(ctx, "b", "/root", "docs", "/moved", "renamed"))
			if e := list()[0]; e.Path != "renamed/same.md" || e.RootPath != "/moved" {
				t.Fatal("history lost renamed path")
			}
			must(st.DeleteLibraryRecentOpen(ctx, "b", "file", fileID))
			if len(list()) != 2 {
				t.Fatal("single history removal deleted other resources")
			}
			_, err = st.PutBrowserHistory(ctx, store.BrowserHistoryInput{URL: "https://example.com/", VisitedAt: time.Now()})
			must(err)
			must(st.ClearLibraryRecentOpens(ctx, "b", "canvas"))
			browser, err := st.ListBrowserHistory(ctx, "", 100)
			must(err)
			if len(list()) != 0 || len(browser) != 1 {
				t.Fatal("filtered clear removed browser history")
			}
			bodies, err := st.ListCanvasItems(ctx, "a")
			must(err)
			versions, err := st.ListSavedCanvasItems(ctx, "b")
			must(err)
			favorites, err := st.ListLibraryFavorites(ctx, "b")
			must(err)
			if len(bodies) != 1 || len(versions) != 1 || len(favorites) != 2 || string(versions[0].Item) != string(saved.SavedItem.Item) {
				t.Fatal("clearing history touched content/favorites")
			}
			must(st.RecordLibraryRecentOpen(ctx, "a", store.LibraryRecentOpen{Kind: "canvas", ItemID: "c"}))
			must(st.RecordLibraryRecentOpen(ctx, "b", store.LibraryRecentOpen{Kind: "canvas", ItemID: "c"}))
			must(st.DeleteCanvasItem(ctx, "a", "c"))
			if e := list(); len(e) != 1 || e[0].SourceSessionID != "b" {
				t.Fatal("canvas delete left orphan or crossed session")
			}
			must(st.RecordLibraryRecentOpen(ctx, "a", file))
			must(st.DeleteSession(ctx, "a"))
			if len(list()) != 2 {
				t.Fatal("file history should retain unavailable source reference")
			}
			must(st.ClearLibraryRecentOpens(ctx, "b", ""))
			browser, err = st.ListBrowserHistory(ctx, "", 100)
			must(err)
			if len(list()) != 0 || len(browser) != 0 {
				t.Fatal("combined clear incomplete")
			}
			if err := st.RecordLibraryRecentOpen(ctx, "b", store.LibraryRecentOpen{Kind: "canvas", ItemID: "missing"}); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("missing canvas accepted")
			}
			if err := st.RecordLibraryRecentOpen(ctx, "missing", file); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("missing actor accepted")
			}
			if _, err := st.ListLibraryRecentOpens(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("missing read actor accepted")
			}
		})
	}
}

func TestLibraryRecentPersistenceAndBound(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "source")
	_, err := st.PutCanvasItem(ctx, store.CanvasItemInput{ActorSessionID: "source", ID: "c", Kind: "table", Item: []byte(`{"rows":[]}`)})
	if err != nil {
		t.Fatal(err)
	}
	e, err := st.ListLibraryRecentOpens(ctx, "source")
	if err != nil || len(e) != 0 {
		t.Fatal("creating a canvas invented open times", err)
	}
	for i := 0; i < store.LibraryRecentRetainLimit+1; i++ {
		if err := st.RecordLibraryRecentOpen(ctx, "source", store.LibraryRecentOpen{Kind: "file", RootPath: "/root", Path: fmt.Sprintf("%04d.md", i)}); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if _, err := st.db.Exec(`UPDATE library_recent_opens SET opened_at=?`, time.Now().Add(-time.Hour).UnixMilli()); err != nil {
				t.Fatal(err)
			}
		}
	}
	e, err = st.ListLibraryRecentOpens(ctx, "source")
	if err != nil || len(e) != 1000 {
		t.Fatal("unbounded history", err)
	}
	for _, entry := range e {
		if entry.Path == "0000.md" {
			t.Fatal("retention removed newer entry")
		}
	}
	latest := *e[0]
	st.Close()
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	e, err = st.ListLibraryRecentOpens(ctx, "source")
	if err != nil || e[0].ID != latest.ID || !e[0].OpenedAt.Equal(latest.OpenedAt) {
		t.Fatal("restart changed actual history", err)
	}
}
