package sqlitestore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/teatak/pudding-core/internal/canvasarchive"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertArchivesContain(t *testing.T, path string, values ...string) {
	t.Helper()
	entries, err := canvasarchive.List(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var body string
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(e.Path, "original.json"))
		if err != nil {
			t.Fatal(err)
		}
		body += string(b)
	}
	for _, value := range values {
		if !strings.Contains(body, value) {
			t.Fatalf("archive missing %q", value)
		}
	}
}
func canvasV26Fixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pudding.db")
	schema, err := os.ReadFile("testdata/schema-v26.sql")
	if fmt.Sprintf("%x", sha256.Sum256(schema)) != "062f41cc8376d4d210122e2c4203e67721b144c002ae070f86854cb4e9030eb5" {
		t.Fatal("v26 fixture changed")
	}
	if err != nil {
		t.Fatal(err)
	}
	db := openMigrationTestDB(t, path)
	defer db.Close()
	if _, err = db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`
 INSERT INTO sessions(id,title,provider,model,created_at,updated_at,last_activity_at) VALUES('s','Source','mock','m',1,1,1);
 INSERT INTO canvas_resources(id,name,source_session_id,revision,active_revision,head_revision,bindings,grants,binding_version,deleted,created_at,updated_at) VALUES
 ('old','Old','s',3,'old2','old2','{}','{}',1,0,1,5),
 ('mixed','Mixed','s',7,'legacy','legacy','{"mail":"account"}','{}',4,0,1,6),
 ('app','App','s',8,'code','code','{"mail":"account"}','{}',4,0,1,7);
 INSERT INTO canvas_revisions(workbench_id,hash,parent_revision,client_request_id,created_at,build_receipt,content_json) VALUES
 ('old','old1','','a',1,'','{"kind":"markdown","title":"First","item":{"content":"before"}}'),
 ('old','old2','old1','b',2,'','{"kind":"table","title":"Second","item":{"columns":["value"],"rows":[{"value":"after"}]}}'),
 ('mixed','source','','a',1,'{"ok":true}',''),
 ('mixed','legacy','source','b',2,'','{"kind":"markdown","title":"Legacy","item":{"content":"keep legacy"}}'),
 ('mixed','newcode','legacy','c',3,'',''),
 ('app','code','','a',1,'{"ok":true}','');
 INSERT INTO canvas_mounts(session_id,id,resource_id,window_json,created_at) VALUES('s','old-view','old','{"x":20}',1),('s','mixed-view','mixed','',1),('s','app-view','app','',1);
 INSERT INTO canvas_actions VALUES('action','app','req','digest','succeeded','{}','{}',1);
 INSERT INTO canvas_links VALUES('link','app','{}','{}',1);
 INSERT INTO library_favorites(id,kind,saved_item_id,created_at) VALUES('old-fav','canvas','old',1),('app-fav','canvas','app',1);
 PRAGMA user_version=26;
 `); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestCanvasRetirementPreservesVersionsAppBindingsAndRestart(t *testing.T) {
	path := canvasV26Fixture(t)
	home := filepath.Dir(path)
	st, err := OpenWithHome(path, home)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"PRAGMA user_version", "28"}, {"SELECT count(*) FROM canvas_resources", "2"}, {"SELECT count(*) FROM canvas_mounts", "2"}, {"SELECT count(*) FROM library_favorites", "1"}, {"SELECT count(*) FROM canvas_actions", "1"}, {"SELECT count(*) FROM canvas_links", "1"}, {"SELECT count(*) FROM pragma_foreign_key_check", "0"}, {"SELECT count(*) FROM pragma_table_info('canvas_revisions') WHERE name='content_json'", "0"}, {"SELECT count(*) FROM pragma_table_info('canvas_mounts') WHERE name='window_json'", "0"}, {"SELECT count(*) FROM pragma_table_info('canvas_resources') WHERE name='grants'", "0"}} {
		assertWorkspaceMigrationValue(t, st.db, pair[0], pair[1])
	}
	ctx := context.Background()
	app, err := st.GetCanvas(ctx, "app")
	if err != nil || app.Revision != 8 || app.Bindings["mail"] != "account" || app.BindingVersion != 4 {
		t.Fatalf("App changed: %+v %v", app, err)
	}
	mixed, err := st.GetCanvas(ctx, "mixed")
	if err != nil || mixed.HeadRevision != "newcode" || mixed.ActiveRevision != "source" || mixed.Revision != 8 {
		t.Fatalf("mixed canvas: %+v %v", mixed, err)
	}
	assertArchivesContain(t, path, "before", "after", "keep legacy", "old-fav", "Source")
	entries, err := canvasarchive.List(home)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries %+v %v", entries, err)
	}
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(e.Path, "original.json"))
		var snap canvasarchive.Snapshot
		if err = json.Unmarshal(b, &snap); err != nil {
			t.Fatal(err)
		}
		if e.CanvasID == "old" && len(snap.Revisions) != 2 {
			t.Fatal("lost versions")
		}
	}
	st.Close()
	ids := []string{}
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	if err = canvasarchive.Remove(home, ids); err != nil {
		t.Fatal(err)
	}
	st, err = OpenWithHome(path, home)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	entries, err = canvasarchive.List(home)
	if err != nil || len(entries) != 0 {
		t.Fatal("restart resurrected archives")
	}
}
func TestCanvasRetirementArchiveFailureRollsBackAndRetryReusesFiles(t *testing.T) {
	path := canvasV26Fixture(t)
	home := filepath.Dir(path)
	// A late SQL failure occurs after archives were safely written.
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`CREATE TRIGGER block_retirement BEFORE DELETE ON canvas_resources BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := OpenWithHome(path, home)
	if err == nil {
		st.Close()
		t.Fatal("expected failure")
	}
	entries, err := canvasarchive.List(home)
	if err != nil || len(entries) != 2 {
		t.Fatalf("verified archives missing: %+v %v", entries, err)
	}
	db = openMigrationTestDB(t, path)
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "26")
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM canvas_revisions WHERE content_json<>''", "3")
	db.Exec(`DROP TRIGGER block_retirement`)
	db.Close()
	st, err = OpenWithHome(path, home)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	again, _ := canvasarchive.List(home)
	if len(again) != len(entries) || again[0].ID != entries[0].ID {
		t.Fatal("retry duplicated archive")
	}
}
func TestCanvasRetirementCannotWriteArchivesKeepsDatabase(t *testing.T) {
	path := canvasV26Fixture(t)
	home := filepath.Dir(path)
	if err := os.WriteFile(filepath.Join(home, "archives"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := OpenWithHome(path, home)
	if err == nil {
		st.Close()
		t.Fatal("expected failure")
	}
	db := openMigrationTestDB(t, path)
	defer db.Close()
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "26")
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM canvas_resources", "3")
}
func TestFreshStoreCreatesNoArchiveEntry(t *testing.T) {
	st, path := openTestStore(t)
	defer st.Close()
	entries, err := canvasarchive.List(filepath.Dir(path))
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	if _, err = os.Stat(canvasarchive.Root(filepath.Dir(path))); !os.IsNotExist(err) {
		t.Fatal("fresh store created archive directory")
	}
}
