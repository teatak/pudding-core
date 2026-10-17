package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

// Build the exact v41 shape from today's schema for isolated legacy fixtures.
func sheetLegacySchema() string {
	return strings.ReplaceAll(strings.Replace(store.SchemaSQL, "'page','sheet','widget'", "'note','table','widget'", 1), "studio_sheet_ids", "studio_table_ids")
}

func TestSheetMigrationV41PreservesContentAndDependents(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pudding.db")
	db := openMigrationTestDB(t, path)
	body := `{"columns":[{"id":"c","name":"名称","type":"text"}],"rows":[{"id":"r","cells":{"c":"中文😀"}}]}`
	hash := store.ContentHash(body)
	if _, err := db.Exec(sheetLegacySchema()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
 INSERT INTO sessions(id,provider,model,created_at,updated_at,last_activity_at)VALUES('s','mock','m',1,1,1);
 INSERT INTO turns(id,session_id,client_message_id,status,created_at,updated_at)VALUES('t','s','i','completed',1,2);
 INSERT INTO messages(id,session_id,turn_id,role,text,created_at)VALUES('m','s','t','assistant','builtin_table_update @table/table_saved',2);
 INSERT INTO studio_items(id,kind,name,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at,archived_at)VALUES
 ('table_saved','table','保留',2,'rev','','{}',1,0,1,2,0),('archived','table','归档',1,'','','{}',1,0,1,2,99),('deleted','table','删除',1,'','','{}',1,1,1,2,0),('note','note','笔记',1,'','','{}',1,0,1,2,0);
 INSERT INTO studio_item_content(item_id,body,content_hash)VALUES('table_saved',?,?),('note','| a | b |','note-hash');
 INSERT INTO studio_item_revisions(item_id,hash,parent_revision,client_request_id,created_at,build_receipt,content,content_hash,author_kind)VALUES('table_saved','rev','','create',2,'',?,?,'user');
 INSERT INTO studio_item_saves(item_id,client_request_id,hash,request_hash)VALUES('table_saved','create','rev','keep-request-hash');
 INSERT INTO studio_table_ids(item_id,entity_kind,entity_id)VALUES('table_saved','row','r'),('table_saved','column','c'),('table_saved','row','deleted-row');
 INSERT INTO studio_mounts(session_id,id,item_id,visible,created_at)VALUES('s','mount','table_saved',1,1);
 INSERT INTO library_favorites(id,kind,source_session_id,saved_item_id,url,title,created_at)VALUES('favorite','studio','','table_saved','','',3);
 PRAGMA user_version=41;`, body, hash, body, hash); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	checks := [][2]string{
		{"PRAGMA user_version", "44"}, {"PRAGMA foreign_keys", "1"}, {"PRAGMA integrity_check", "ok"},
		{"SELECT count(*) FROM pragma_foreign_key_check", "0"},
		{"SELECT count(*) FROM sqlite_master WHERE name='studio_table_ids'", "0"},
		{"SELECT count(*) FROM studio_sheet_ids", "3"},
		{"SELECT count(*) FROM studio_items WHERE kind='sheet'", "3"},
		{"SELECT kind||'|'||archived_at FROM studio_items WHERE id='archived'", "sheet|99"},
		{"SELECT kind||'|'||deleted FROM studio_items WHERE id='deleted'", "sheet|1"},
		{"SELECT revision||'|'||updated_at||'|'||head_revision FROM studio_items WHERE id='table_saved'", "2|2|rev"},
		{"SELECT request_hash FROM studio_item_saves WHERE item_id='table_saved'", "keep-request-hash"},
		{"SELECT text FROM messages WHERE id='m'", "builtin_table_update @table/table_saved"},
		{"SELECT body FROM studio_item_content WHERE item_id='note'", "| a | b |"},
		{"SELECT item_id FROM studio_mounts WHERE id='mount'", "table_saved"},
		{"SELECT saved_item_id FROM library_favorites WHERE id='favorite'", "table_saved"},
	}
	for _, check := range checks {
		assertWorkspaceMigrationValue(t, s.db, check[0], check[1])
	}
	content, err := s.GetSheet(ctx, "table_saved")
	if err != nil || content.ContentHash != hash || content.RevisionID != "rev" || content.Body.Rows[0].Cells["c"] != "中文😀" {
		t.Fatal(content, err)
	}
	rev, err := s.GetStudioItemRevision(ctx, "table_saved", "rev")
	if err != nil || rev.Body == nil || *rev.Body != body || rev.ContentHash != hash {
		t.Fatal("revision changed", rev, err)
	}
	before := *content
	saved, err := s.WriteSheet(ctx, "table_saved", store.SheetWrite{ClientRequestID: "new-edit", Operations: []store.SheetOperation{sheetCell("r", "c", "更新")}, Author: store.ContentAuthor{Kind: "user"}})
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.WriteSheet(ctx, "table_saved", store.SheetWrite{ClientRequestID: "new-edit", Operations: []store.SheetOperation{sheetCell("r", "c", "更新")}, Author: store.ContentAuthor{Kind: "user"}})
	if err != nil || !reflect.DeepEqual(saved, again) {
		t.Fatal("retry changed", err)
	}
	if _, err := s.WriteSheet(ctx, "table_saved", store.SheetWrite{ClientRequestID: "reuse", Operations: []store.SheetOperation{{Kind: "add_row", Row: &store.SheetRow{ID: "deleted-row", Cells: map[string]any{}}}}, Author: store.ContentAuthor{Kind: "user"}}); err == nil {
		t.Fatal("tombstone lost")
	}
	restored, err := s.WriteSheet(ctx, "table_saved", store.SheetWrite{ClientRequestID: "restore", RestoreRevision: before.RevisionID, ExpectedHash: &saved.ContentHash, Author: store.ContentAuthor{Kind: "user"}})
	if err != nil || restored.ContentHash != hash {
		t.Fatal("restore failed", err)
	}
	backups, _ := filepath.Glob(path + ".backup-v41-*")
	if len(backups) != 1 {
		t.Fatal("missing upgrade backup", backups)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	reopened, err := s.GetSheet(ctx, "table_saved")
	if err != nil || !reflect.DeepEqual(restored, reopened) {
		t.Fatal("reopen changed content", err)
	}
	if _, err := s.db.Exec(`DELETE FROM studio_items WHERE id='table_saved'`); err != nil {
		t.Fatal(err)
	}
	assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM studio_sheet_ids", "0")
	assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM studio_item_revisions WHERE item_id='table_saved'", "0")
}

func TestSheetMigrationFailureRollsBackSchemaAndVersion(t *testing.T) {
	db := openMigrationTestDB(t, filepath.Join(t.TempDir(), "rollback.db"))
	defer db.Close()
	if _, err := db.Exec(sheetLegacySchema() + `PRAGMA user_version=41; PRAGMA foreign_keys=ON;`); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("injected after rename")
	err := runSchemaMigration(db, 42, func(tx *sql.Tx) error {
		if err := migrateSheets(tx); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	for _, check := range [][2]string{{"PRAGMA user_version", "41"}, {"PRAGMA foreign_keys", "1"}, {"SELECT count(*) FROM sqlite_master WHERE name='studio_table_ids'", "1"}, {"SELECT count(*) FROM sqlite_master WHERE name IN ('studio_sheet_ids','studio_items_sheet')", "0"}} {
		assertWorkspaceMigrationValue(t, db, check[0], check[1])
	}
	var ddl string
	db.QueryRow(`SELECT sql FROM sqlite_master WHERE name='studio_items'`).Scan(&ddl)
	if !strings.Contains(ddl, "'table'") || strings.Contains(ddl, "'sheet'") {
		t.Fatal("DDL was not rolled back", ddl)
	}
	if err := runSchemaMigration(db, 42, migrateSheets); err != nil {
		t.Fatal("retry", err)
	}
}

func TestSheetMigrationRetainsEarlyV41DocumentRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "early-v41.db")
	db := openMigrationTestDB(t, path)
	old := strings.Replace(sheetLegacySchema(), "'note','table','widget'", "'doc','table','widget'", 1)
	if _, err := db.Exec(old + `INSERT INTO studio_items(id,kind,name,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at)VALUES('old','doc','Old',1,'','','{}',1,0,1,1);PRAGMA user_version=41;`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertWorkspaceMigrationValue(t, s.db, "SELECT kind FROM studio_items WHERE id='old'", "page")
	assertWorkspaceMigrationValue(t, s.db, "PRAGMA user_version", "44")
}

// Earlier migration tests need an actual pre-sheet schema, not a v42 database
// whose user_version alone has been lowered. Call before creating any content.
func useLegacySheetFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM studio_items`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("legacy schema setup requires empty Studio: %d, %v", count, err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
			t.Fatal(err)
		}
	}()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var ddl string
	if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE name='studio_items'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(`SELECT sql FROM sqlite_master WHERE tbl_name='studio_items' AND type IN ('index','trigger') AND sql IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	var objects []string
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		objects = append(objects, text)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	ddl = strings.Replace(ddl, "'page','sheet','widget'", "'note','table','widget'", 1)
	if _, err := tx.Exec(`DROP TABLE studio_items`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	for _, object := range objects {
		if _, err := tx.Exec(object); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`ALTER TABLE studio_sheet_ids RENAME TO studio_table_ids; PRAGMA user_version=41`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
