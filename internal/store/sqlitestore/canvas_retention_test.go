package sqlitestore

import (
	"context"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestCanvasClosedSnapshotsMigrateToRetainedContent(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "retain-a")
	createTestSession(t, st, "retain-b")
	for _, sessionID := range []string{"retain-a", "retain-b"} {
		if _, err := st.PutCanvasItem(ctx, store.CanvasItemInput{ID: "same-id", ActorSessionID: sessionID, Kind: "markdown", Title: sessionID, Item: []byte(`{"content":"current"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := st.SaveCanvasItem(ctx, "retain-a", "same-id", "saved-retained")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`
  CREATE TABLE canvas_closed_items (
   session_id TEXT NOT NULL, id TEXT NOT NULL, source_item_id TEXT NOT NULL,
   actor_session_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL, title TEXT NOT NULL,
   item_json TEXT NOT NULL, window_json TEXT NOT NULL DEFAULT '', closed_at INTEGER NOT NULL,
   created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY(session_id,id)
  );
  INSERT INTO canvas_closed_items VALUES
   ('retain-a','old-a','same-id','retain-a','markdown','stale snapshot','{"content":"stale"}','',200,100,200),
   ('retain-a','old-b','closed-id','retain-a','table','Recover me','{"rows":[1,2]}','{"x":20}',300,150,300),
   ('retain-b','old-c','closed-id','retain-b','markdown','Other session','{"content":"other"}','',400,200,400);
  PRAGMA user_version=13;
 `); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	items, err := reopened.ListCanvasItems(ctx, "retain-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("retained items: %+v", items)
	}
	var closed *store.CanvasItem
	for _, item := range items {
		if item.ID == "same-id" {
			if item.Title != "retain-a" || item.SourceSavedItemID != saved.SavedItem.ID || item.BaseSavedRevision != 1 || item.SavedDirty {
				t.Fatalf("canonical or saved identity overwritten: %+v", item)
			}
		} else {
			closed = item
		}
	}
	if closed == nil || closed.ID != "closed-id" || closed.Visible || closed.Title != "Recover me" || closed.UpdatedAt.UnixMilli() != 300 || string(closed.Window) != `{"x":20}` {
		t.Fatalf("closed snapshot not retained with initial closed state: %+v", closed)
	}
	others, err := reopened.ListCanvasItems(ctx, "retain-b")
	if err != nil || len(others) != 2 || others[0].Title != "Other session" {
		t.Fatalf("session isolation: items=%+v err=%v", others, err)
	}
	var oldTables int
	if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='canvas_closed_items'`).Scan(&oldTables); err != nil || oldTables != 0 {
		t.Fatalf("old content source still exists: %d, %v", oldTables, err)
	}
	// Reopening the process cannot migrate/duplicate content again.
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	items, err = again.ListCanvasItems(ctx, "retain-a")
	if err != nil || len(items) != 2 {
		t.Fatalf("restart lost retained content: %+v %v", items, err)
	}
	// Only an explicit deletion removes the working content; saved versions stay.
	if err := again.DeleteCanvasItem(ctx, "retain-a", "same-id"); err != nil {
		t.Fatal(err)
	}
	favorites, err := again.ListSavedCanvasItems(ctx, "retain-a")
	if err != nil || len(favorites) != 1 || favorites[0].Revision != 1 {
		t.Fatalf("deletion changed saved version: %+v %v", favorites, err)
	}
}
