package sqlitestore

import (
	"context"
	"testing"
)

func TestCanvasClosedSnapshotsMigrateToRetainedContent(t *testing.T) {
	db, path := openWorkspaceV13Database(t)
	ctx := context.Background()
	if _, err := db.Exec(`
 INSERT INTO sessions(id,title,provider,model,created_at,updated_at,last_activity_at) VALUES('retain-a','A','mock','mock',1,1,1),('retain-b','B','mock','mock',1,1,1);
 INSERT INTO canvas_items(session_id,id,kind,title,item_json,created_at,updated_at) VALUES('retain-a','same-id','markdown','retain-a','{"content":"current"}',100,200),('retain-b','same-id','markdown','retain-b','{"content":"current"}',100,200);
 `); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
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
	assertConvertedCanvasesContain(t, path, "current", "Recover me", "Other session", "closed-id")
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
	assertConvertedCanvasesContain(t, path, "current", "Recover me", "Other session")
}
