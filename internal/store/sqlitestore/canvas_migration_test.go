package sqlitestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/canvasarchive"
	"github.com/teatak/pudding-core/internal/store"
)

func canvasV25Fixture(t *testing.T) string {
	t.Helper()
	schema, err := os.ReadFile("testdata/schema-v25.sql")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(schema)) != "9ac7648b15cc883652f8deb4a2a9b28d5ad9bee51ab956309eed11646309cde0" {
		t.Fatal("v25 fixture changed")
	}
	path := filepath.Join(t.TempDir(), "canvas.db")
	db := openMigrationTestDB(t, path)
	defer db.Close()
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
 INSERT INTO sessions(id,provider,model,created_at,updated_at,last_activity_at) VALUES('a','mock','m',1,1,1),('b','mock','m',1,1,1);
 UPDATE sessions SET loaded_app_ids='["browser","workbench-authoring","canvas"]' WHERE id='a';
 INSERT INTO canvas_saved_items(id,source_session_id,kind,title,item_json,revision,created_at,updated_at) VALUES('saved','a','markdown','Report','{"content":"saved"}',3,1,2);
 INSERT INTO canvas_items(session_id,id,kind,title,item_json,source_saved_item_id,base_saved_revision,saved_dirty,window_json,created_at,updated_at) VALUES
 ('a','dirty','markdown','Report','{"content":"unsaved"}','saved',3,1,'{"x":20}',1,4),
 ('b','clean','markdown','Report','{"content":"saved"}','saved',3,0,'{"x":40}',1,3),
 ('a','local','table','Local','{"rows":[1]}','',0,0,'',1,3);
 INSERT INTO workbenches VALUES('app','App','a',7,'hash','hash','{"mail":"account"}','{}',2,0,1,4);
 INSERT INTO workbench_revisions VALUES('app','hash','','save',4,'{"ok":true}');
 INSERT INTO workbench_actions VALUES('action','app','request','digest','succeeded','{}','{"ok":true}',4);
 INSERT INTO workbench_links VALUES('link','app','{"entityID":"left"}','{"entityID":"right"}',4);
 INSERT INTO library_favorites(id,kind,saved_item_id,created_at) VALUES('canvas:saved','canvas','saved',2);
 INSERT INTO library_recent_opens(id,kind,source_session_id,canvas_item_id,opened_at) VALUES('recent','canvas','a','dirty',5);
 PRAGMA user_version=25;
 `); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestUnifiedCanvasMigrationPreservesIdentityContentAndAppState(t *testing.T) {
	path := canvasV25Fixture(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		st, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		resources, err := st.ListWorkbenches(ctx)
		if err != nil || len(resources) != 1 {
			t.Fatalf("resources %+v %v", resources, err)
		}
		archives, err := canvasarchive.List(filepath.Dir(path))
		if err != nil || len(archives) != 3 {
			t.Fatalf("archives %+v %v", archives, err)
		}
		var contents string
		for _, entry := range archives {
			b, e := os.ReadFile(filepath.Join(entry.Path, "original.json"))
			if e != nil {
				t.Fatal(e)
			}
			contents += string(b)
		}
		if !strings.Contains(contents, "unsaved") || !strings.Contains(contents, "saved") {
			t.Fatal("lost dirty or saved content")
		}
		views, err := st.ListCanvasItems(ctx, "a")
		if err != nil || len(views) != 0 {
			t.Fatalf("legacy mounts remain: %+v %v", views, err)
		}
		app, err := st.GetWorkbench(ctx, "app")
		if err != nil || app.Revision != 7 || app.HeadRevision != "hash" || app.Bindings["mail"] != "account" {
			t.Fatalf("App changed %+v %v", app, err)
		}
		for _, check := range [][2]string{{"SELECT loaded_app_ids FROM sessions WHERE id='a'", `["browser","canvas"]`}, {"SELECT state FROM canvas_actions WHERE id='action'", "succeeded"}, {"SELECT id FROM canvas_links WHERE id='link'", "link"}, {"SELECT COUNT(*) FROM library_recent_opens", "0"}, {"SELECT COUNT(*) FROM library_favorites", "0"}, {"SELECT COUNT(*) FROM pragma_foreign_key_check", "0"}, {"SELECT COUNT(*) FROM sqlite_master WHERE name IN ('canvas_saved_items','canvas_items','workbenches')", "0"}} {
			assertWorkspaceMigrationValue(t, st.db, check[0], check[1])
		}
		st.Close()
	}
}
func TestUnifiedCanvasMigrationFailureRollsBack(t *testing.T) {
	path := canvasV25Fixture(t)
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`UPDATE canvas_items SET item_json='invalid' WHERE id='dirty'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if st, err := Open(path); err == nil {
		st.Close()
		t.Fatal("invalid historical JSON did not fail migration")
	}
	db = openMigrationTestDB(t, path)
	defer db.Close()
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "25")
	assertWorkspaceMigrationValue(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE name='canvas_resources'", "0")
	assertWorkspaceMigrationValue(t, db, "SELECT item_json FROM canvas_saved_items WHERE id='saved'", `{"content":"saved"}`)
	if _, err := db.Exec(`UPDATE canvas_items SET item_json='{"content":"unsaved"}' WHERE id='dirty'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.GetWorkbench(context.Background(), store.CanvasResourceID("a", "dirty")); err == nil {
		t.Fatal(err)
	}
}

// Older migration tests deliberately remove later columns from a fresh store.
// Restore the frozen pre-unification canvas layout before constructing those
// historical fixtures. Never downgrade a database containing canvas resources.
func useV25CanvasFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM canvas_resources`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("historical fixture contains canvas data: %d %v", count, err)
	}
	schema, err := os.ReadFile("testdata/schema-v25.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF;
 DROP TABLE library_recent_opens;DROP TABLE library_favorites;DROP TABLE canvas_mounts;
 DROP TABLE canvas_actions;DROP TABLE canvas_links;DROP TABLE canvas_saves;DROP TABLE canvas_revisions;DROP TABLE canvas_resources;`); err != nil {
		t.Fatal(err)
	}
	source := string(schema)
	for _, pair := range [][2]string{{"CREATE TABLE IF NOT EXISTS canvas_items", "CREATE TABLE IF NOT EXISTS session_browser_tabs"}, {"CREATE TABLE IF NOT EXISTS library_favorites", "CREATE TABLE IF NOT EXISTS session_dispatches"}, {"CREATE TABLE workbenches", ""}} {
		part := source[strings.Index(source, pair[0]):]
		if pair[1] != "" {
			part = part[:strings.Index(part, pair[1])]
		}
		if _, err := db.Exec(part); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
}
