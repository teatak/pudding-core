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

	"github.com/teatak/pudding-core/internal/widget"
)

// canvasV25AppearanceFixture builds the released v26 layout and removes the
// appearance columns that v26 added.
func canvasV25AppearanceFixture(t *testing.T, keepColorColumn bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "canvas.db")
	db := openMigrationTestDB(t, path)
	schema, err := os.ReadFile("testdata/schema-v26.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO canvas_resources(id,name,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at) VALUES('old','Existing canvas',1,'','','{}',1,0,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE canvas_resources DROP COLUMN icon`); err != nil {
		t.Fatal(err)
	}
	if !keepColorColumn {
		if _, err := db.Exec(`ALTER TABLE canvas_resources DROP COLUMN icon_color`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`PRAGMA user_version=25`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCanvasAppearanceMigrationPreservesResourcesAndRestarts(t *testing.T) {
	path := canvasV25AppearanceFixture(t, false)
	for range 2 {
		st, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		resource, err := st.GetStudioItem(context.Background(), "old")
		if err != nil || resource.Name != "Existing canvas" || resource.Icon != "" || resource.IconColor != "" {
			t.Fatalf("migrated resource: %+v %v", resource, err)
		}
		assertWorkspaceMigrationValue(t, st.db, "PRAGMA user_version", fmt.Sprint(currentSchemaVersion))
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCanvasAppearanceMigrationFailureRollsBack(t *testing.T) {
	path := canvasV25AppearanceFixture(t, true)
	if st, err := Open(path); err == nil {
		st.Close()
		t.Fatal("duplicate icon_color column did not fail migration")
	}
	db := openMigrationTestDB(t, path)
	defer db.Close()
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "25")
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM pragma_table_info('canvas_resources') WHERE name='icon'", "0")
	assertWorkspaceMigrationValue(t, db, "SELECT name FROM canvas_resources WHERE id='old'", "Existing canvas")
}

func releaseV24Schema(t *testing.T) string {
	t.Helper()
	schema, err := os.ReadFile("testdata/schema-v24.sql")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(schema)); got != "03c3bff523a3b796998107413c902cb92b70b1af57792f8cbb9c4fca46f25c28" {
		t.Fatalf("v24 release fixture changed: %s", got)
	}
	return string(schema)
}

func canvasV24Fixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "canvas.db")
	db := openMigrationTestDB(t, path)
	defer db.Close()
	if _, err := db.Exec(releaseV24Schema(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
 INSERT INTO sessions(id,provider,model,created_at,updated_at,last_activity_at) VALUES('a','mock','m',1,1,1),('b','mock','m',1,1,1);
 INSERT INTO canvas_saved_items(id,source_session_id,kind,title,item_json,revision,created_at,updated_at) VALUES('saved','a','markdown','Report','{"content":"saved"}',3,1,2);
 INSERT INTO canvas_items(session_id,id,kind,title,item_json,source_saved_item_id,base_saved_revision,saved_dirty,window_json,created_at,updated_at) VALUES
 ('a','dirty','markdown','Report','{"content":"unsaved"}','saved',3,1,'{"x":20}',1,4),
 ('b','clean','markdown','Report','{"content":"saved"}','saved',3,0,'{"x":40}',1,3),
 ('a','local','table','Local','{"rows":[1]}','',0,0,'',1,3);
 INSERT INTO library_favorites(id,kind,saved_item_id,created_at) VALUES('canvas:saved','canvas','saved',2);
 INSERT INTO library_recent_opens(id,kind,source_session_id,canvas_item_id,opened_at) VALUES('recent','canvas','a','dirty',5);
 PRAGMA user_version=24;
 `); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFinalCanvasMigrationConvertsReleaseDataAndRestart(t *testing.T) {
	path := canvasV24Fixture(t)
	for i := 0; i < 2; i++ {
		st, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		assertWorkspaceMigrationValue(t, st.db, "PRAGMA user_version", fmt.Sprint(currentSchemaVersion))
		assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM studio_items WHERE kind='widget'", "3")
		assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM pragma_foreign_key_check", "0")
		assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM pragma_table_info('studio_item_revisions') WHERE name='item_id'", "1")
		assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM pragma_table_info('studio_item_revisions') WHERE name='workbench_id'", "0")
		assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM studio_mounts", "3")
		assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM library_favorites", "1")
		assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM library_recent_opens", "1")
		assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM studio_items WHERE active_revision=head_revision AND active_revision<>''", "3")
		assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM studio_item_revisions WHERE build_receipt<>''", "0")
		var hash string
		if err := st.db.QueryRow("SELECT head_revision FROM studio_items WHERE id='saved'").Scan(&hash); err != nil {
			t.Fatal(err)
		}
		pkg, err := widget.ReadPackage(filepath.Dir(path), "saved", hash)
		if err != nil || !strings.Contains(pkg.Files["src/App.tsx"], "saved") {
			t.Fatal(pkg, err)
		}
		assertConvertedCanvasesContain(t, path, "unsaved", "saved", "migration-v25")
		st.Close()
	}
}

func TestFinalCanvasMigrationFailureRollsBack(t *testing.T) {
	path := canvasV24Fixture(t)
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
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "24")
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM sqlite_master WHERE name='canvas_resources'", "0")
	assertWorkspaceMigrationValue(t, db, "SELECT item_json FROM canvas_saved_items WHERE id='saved'", `{"content":"saved"}`)
	if _, err := db.Exec(`UPDATE canvas_items SET item_json='{"content":"unsaved"}' WHERE id='dirty'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
}

// Older tests remove later tables from a fresh store before constructing
// historical fixtures. The v24 release is the sole pre-upgrade canvas layout.
func useV24CanvasFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM studio_items`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("historical fixture contains studio data: %d %v", count, err)
	}
	// Schema v30 renames the session plugin column; earlier layouts keep loaded_app_ids.
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF;
 DROP TABLE widget_data;
 DROP TABLE remote_identity; DROP TABLE remote_pairings; DROP TABLE remote_devices;
 DROP TABLE library_recent_opens;DROP TABLE library_favorites;DROP TABLE studio_mounts;
 DROP TABLE studio_table_ids;DROP TABLE studio_item_content;DROP TABLE widget_actions;DROP TABLE widget_links;DROP TABLE studio_item_saves;DROP TABLE studio_item_revisions;DROP TABLE studio_items;
 ALTER TABLE sessions RENAME COLUMN loaded_plugin_ids TO loaded_app_ids;`); err != nil {
		t.Fatal(err)
	}
	source := releaseV24Schema(t)
	for _, pair := range [][2]string{{"CREATE TABLE IF NOT EXISTS canvas_items", "CREATE TABLE IF NOT EXISTS session_browser_tabs"}, {"CREATE TABLE IF NOT EXISTS library_favorites", "CREATE TABLE IF NOT EXISTS session_dispatches"}} {
		part := source[strings.Index(source, pair[0]):]
		part = part[:strings.Index(part, pair[1])]
		if _, err := db.Exec(part); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
}
