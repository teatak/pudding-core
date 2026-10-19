package sqlitestore

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestPageNamesFollowContentAndPreserveManualNames(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "names.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	user := store.ContentAuthor{Kind: "user"}
	doc, err := s.CreatePage(ctx, &store.StudioItem{ID: "page", Kind: "page", Name: "未命名 Page", NameFallback: "未命名 Page"}, "", user)
	if err != nil {
		t.Fatal(err)
	}
	check := func(name, mode string) *store.StudioItem {
		t.Helper()
		item, err := s.GetStudioItem(ctx, "page")
		if err != nil || item.Name != name || item.NameMode != mode {
			t.Fatalf("item=%+v err=%v", item, err)
		}
		return item
	}
	write := func(body string) {
		t.Helper()
		doc, err = s.WritePage(ctx, "page", store.PageWrite{ClientRequestID: store.NewID("save"), ExpectedHash: &doc.ContentHash, Body: &body, Author: user})
		if err != nil {
			t.Fatal(err)
		}
	}
	check("未命名 Page", "auto")
	write("# First\n\nBody\n")
	first := doc.RevisionID
	check("First", "auto")
	write("# Second\n\nBody\n")
	check("Second", "auto")
	doc, err = s.WritePage(ctx, "page", store.PageWrite{ClientRequestID: "restore", ExpectedHash: &doc.ContentHash, RestoreRevision: first, Author: user})
	if err != nil {
		t.Fatal(err)
	}
	check("First", "auto")
	item := check("First", "auto")
	item.NameMode = store.PageNameManual // Same visible name still pins it.
	if _, err = s.UpdateStudioItem(ctx, item, item.Revision); err != nil {
		t.Fatal(err)
	}
	write("A paragraph without a heading.\n")
	check("First", "manual")
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	check("First", "manual")
	item = check("First", "manual")
	item.NameMode = store.PageNameAuto
	if _, err = s.UpdateStudioItem(ctx, item, item.Revision); err != nil {
		t.Fatal(err)
	}
	check("A paragraph without a heading.", "auto")
	write("")
	check("未命名 Page", "auto")
	// Preserving a conflict draft never renames the visible document.
	unsaved := "# Hidden draft"
	if _, err = s.WritePage(ctx, "page", store.PageWrite{ClientRequestID: "preserve", ExpectedHash: &doc.ContentHash, Body: &unsaved, PreserveOnly: true, Author: user}); err != nil {
		t.Fatal(err)
	}
	check("未命名 Page", "auto")
	// A rejected stale metadata write cannot reset an automatically updated name.
	item = check("未命名 Page", "auto")
	write("# Latest")
	item.Name = "Stale"
	item.NameMode = store.PageNameManual
	if _, err = s.UpdateStudioItem(ctx, item, item.Revision); err != store.ErrStudioItemConflict {
		t.Fatal(err)
	}
	check("Latest", "auto")
}

func TestPageNameMigrationPreservesContentHistoryAndIndependentNames(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"Heading", "Independent", "未命名 Page"}
	bodies := []string{"# Heading\r\n\r\nBody\r\n", "# Body title\n", ""}
	before := map[string]*store.PageContent{}
	history := map[string][]*store.StudioItemRevision{}
	for i, name := range names {
		id := name
		d, err := s.CreatePage(ctx, &store.StudioItem{ID: id, Kind: "page", Name: name, NameMode: store.PageNameManual}, bodies[i], store.ContentAuthor{Kind: "user"})
		if err != nil {
			t.Fatal(err)
		}
		before[id] = d
		history[id], _ = s.ListStudioItemRevisions(ctx, id)
	}
	s.Close()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`ALTER TABLE studio_items DROP COLUMN name_mode; ALTER TABLE studio_items DROP COLUMN name_fallback; PRAGMA user_version=45;`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i, name := range names {
		item, _ := s.GetStudioItem(ctx, name)
		mode := store.PageNameAuto
		if i == 1 {
			mode = store.PageNameManual
		}
		if item.Name != name || item.NameMode != mode {
			t.Fatalf("%+v", item)
		}
		d, _ := s.GetPage(ctx, name)
		revisions, _ := s.ListStudioItemRevisions(ctx, name)
		if !reflect.DeepEqual(before[name], d) || !reflect.DeepEqual(history[name], revisions) {
			t.Fatal("migration changed content or history")
		}
		old, _ := s.GetStudioItemRevision(ctx, name, d.RevisionID)
		if old.Body == nil || *old.Body != bodies[i] {
			t.Fatal("old revision changed")
		}
	}
}
